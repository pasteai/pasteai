package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/pasteai/pasteai/server"
	"github.com/pasteai/pasteai/store"
)

// Options configures the MCP server. All fields are optional.
type Options struct {
	// URL of the pasteai HTTP server. If empty, an embedded server is started
	// automatically on EmbeddedPort using the default database path.
	URL string

	// APIKey is sent as a Bearer token on all API requests. Optional.
	APIKey string

	// EmbeddedPort is the port for the embedded HTTP server when URL is empty.
	// Defaults to "18080".
	EmbeddedPort string

	// Logger for diagnostic output. Defaults to log.Default() with an [pasteai-mcp] prefix.
	Logger *log.Logger

	// HTTPClient is used for all requests to the pasteai HTTP server. If nil,
	// a default client with a 30s timeout is used. Provide a custom client to
	// use alternative auth mechanisms (cookies, mTLS, OAuth) via http.RoundTripper.
	HTTPClient *http.Client
}

// Server is an MCP stdio server that forwards tool calls to a pasteai HTTP server.
type Server struct {
	baseURL    string
	apiKey     string
	logger     *log.Logger
	httpClient *http.Client
	cleanup    func() // called on Run return; non-nil only for embedded servers
}

// NewHTTPHandler builds a stateless streamable-HTTP MCP handler that forwards
// tool calls to opts.URL. Unlike New, it does not start an embedded server and
// does not dial the target on creation — the target only needs to be reachable
// when tool calls arrive. Intended for mounting on an existing HTTP mux.
func NewHTTPHandler(opts Options) (http.Handler, error) {
	if opts.URL == "" {
		return nil, fmt.Errorf("NewHTTPHandler requires a non-empty URL")
	}
	u, err := url.Parse(opts.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("NewHTTPHandler: invalid URL %q", opts.URL)
	}
	u.Path, u.RawQuery, u.Fragment = "", "", ""

	logger := opts.Logger
	if logger == nil {
		logger = log.New(os.Stderr, "[pasteai-mcp] ", log.LstdFlags)
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	s := &Server{
		baseURL:    u.String(),
		apiKey:     opts.APIKey,
		logger:     logger,
		httpClient: httpClient,
	}
	return s.Handler(), nil
}

// New creates a new MCP Server. If opts.URL is empty and no pasteai server is
// responding on the embedded port, an embedded HTTP server is started in-process.
func New(opts Options) *Server {
	logger := opts.Logger
	if logger == nil {
		logger = log.New(os.Stderr, "[pasteai-mcp] ", log.LstdFlags)
	}

	embeddedPort := opts.EmbeddedPort
	if embeddedPort == "" {
		embeddedPort = "18080"
	}

	rawURL := opts.URL
	embedded := rawURL == ""
	if embedded {
		rawURL = "http://localhost:" + embeddedPort
	}

	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		fmt.Fprintf(os.Stderr, "[pasteai-mcp] URL must be http or https, got: %q\n", rawURL)
		os.Exit(1)
	}
	u.Path, u.RawQuery, u.Fragment = "", "", ""
	baseURL := u.String()

	var cleanup func()
	if embedded && !isResponding(baseURL) {
		var err error
		cleanup, err = startEmbedded(embeddedPort, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[pasteai-mcp] failed to start embedded server: %v\n", err)
			os.Exit(1)
		}
		if !waitForServer(baseURL, 5*time.Second) {
			fmt.Fprintf(os.Stderr, "[pasteai-mcp] embedded server did not become ready within 5s\n")
			os.Exit(1)
		}
		logger.Printf("started embedded server, documents at ~/.pasteai/documents.db")
	} else if embedded {
		logger.Printf("using existing server at %s", baseURL)
	} else {
		logger.Printf("using remote server at %s", baseURL)
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Server{
		baseURL:    baseURL,
		apiKey:     opts.APIKey,
		logger:     logger,
		httpClient: httpClient,
		cleanup:    cleanup,
	}
}

func (s *Server) Run() error {
	if s.cleanup != nil {
		defer s.cleanup()
	}
	srv := mcpserver.NewMCPServer("pasteai", "1.0.0",
		mcpserver.WithToolCapabilities(false),
	)
	s.registerTools(srv)
	return mcpserver.ServeStdio(srv)
}

// Handler returns a streamable-HTTP MCP handler that exposes the same tools as
// the stdio server. Mount it at /mcp on an existing HTTP mux. The handler is
// stateless — each POST is a self-contained request/response; no session state
// is kept between calls.
func (s *Server) Handler() http.Handler {
	srv := mcpserver.NewMCPServer("pasteai", "1.0.0",
		mcpserver.WithToolCapabilities(false),
	)
	s.registerTools(srv)
	return mcpserver.NewStreamableHTTPServer(srv, mcpserver.WithStateLess(true))
}

func (s *Server) registerTools(srv *mcpserver.MCPServer) {
	publishTool := mcpgo.NewTool("publish_document",
		mcpgo.WithDescription("Publish a markdown document to PasteAI and get back a shareable URL"),
		mcpgo.WithString("title",
			mcpgo.Required(),
			mcpgo.Description("The title of the document"),
		),
		mcpgo.WithString("content",
			mcpgo.Required(),
			mcpgo.Description("The document content in markdown format"),
		),
		mcpgo.WithString("author",
			mcpgo.Description("Optional author name (e.g. the AI model name)"),
		),
		mcpgo.WithString("visibility",
			mcpgo.Description("Visibility: public (default, appears in listings) or unlisted (link-only, not listed)"),
		),
	)
	srv.AddTool(publishTool, s.handlePublish)

	listTool := mcpgo.NewTool("list_documents",
		mcpgo.WithDescription("List recent documents published to PasteAI"),
	)
	srv.AddTool(listTool, s.handleList)

	getTool := mcpgo.NewTool("get_document",
		mcpgo.WithDescription("Retrieve a PasteAI document by ID, including its full markdown content"),
		mcpgo.WithString("id",
			mcpgo.Required(),
			mcpgo.Description("The document ID"),
		),
	)
	srv.AddTool(getTool, s.handleGet)

	updateTool := mcpgo.NewTool("update_document",
		mcpgo.WithDescription("Update the title or content of an existing PasteAI document. Provide at least one of title or content."),
		mcpgo.WithString("id",
			mcpgo.Required(),
			mcpgo.Description("The document ID to update"),
		),
		mcpgo.WithString("title",
			mcpgo.Description("New title (omit to keep existing)"),
		),
		mcpgo.WithString("content",
			mcpgo.Description("New markdown content (omit to keep existing)"),
		),
	)
	srv.AddTool(updateTool, s.handleUpdate)

	deleteTool := mcpgo.NewTool("delete_document",
		mcpgo.WithDescription("Permanently delete a PasteAI document by ID"),
		mcpgo.WithString("id",
			mcpgo.Required(),
			mcpgo.Description("The document ID to delete"),
		),
	)
	srv.AddTool(deleteTool, s.handleDelete)

	searchTool := mcpgo.NewTool("search_documents",
		mcpgo.WithDescription("Search PasteAI documents by title keyword"),
		mcpgo.WithString("query",
			mcpgo.Required(),
			mcpgo.Description("Keyword to search for in document titles"),
		),
	)
	srv.AddTool(searchTool, s.handleSearch)

	visibilityTool := mcpgo.NewTool("set_visibility",
		mcpgo.WithDescription("Change the visibility of an existing PasteAI document"),
		mcpgo.WithString("id",
			mcpgo.Required(),
			mcpgo.Description("The document ID"),
		),
		mcpgo.WithString("visibility",
			mcpgo.Required(),
			mcpgo.Description("New visibility: public, unlisted, or private"),
		),
	)
	srv.AddTool(visibilityTool, s.handleSetVisibility)
}

func (s *Server) handlePublish(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	title := req.GetString("title", "")
	content := req.GetString("content", "")
	author := req.GetString("author", "")
	visibility := req.GetString("visibility", "public")

	if title == "" || content == "" {
		return mcpgo.NewToolResultError("title and content are required"), nil
	}

	payload := map[string]string{
		"title":      title,
		"content":    content,
		"author":     author,
		"visibility": visibility,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("failed to serialise request: %v", err)), nil
	}

	httpReq, err := http.NewRequest(http.MethodPost, s.baseURL+"/api/documents", bytes.NewReader(body))
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("failed to build request: %v", err)), nil
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("failed to reach PasteAI server: %v", err)), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		var errBody struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(resp.Body).Decode(&errBody) == nil && errBody.Error != "" {
			return mcpgo.NewToolResultError(fmt.Sprintf("server error (%d): %s", resp.StatusCode, errBody.Error)), nil
		}
		return mcpgo.NewToolResultError(fmt.Sprintf("server returned %d", resp.StatusCode)), nil
	}

	var result struct {
		URL string `json:"url"`
		ID  string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return mcpgo.NewToolResultError("failed to parse server response"), nil
	}

	return mcpgo.NewToolResultText(fmt.Sprintf("Document published successfully.\nURL: %s\nID: %s", result.URL, result.ID)), nil
}

func (s *Server) handleList(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	httpReq, err := http.NewRequest(http.MethodGet, s.baseURL+"/api/documents", nil)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("failed to build request: %v", err)), nil
	}
	if s.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("failed to reach PasteAI server: %v", err)), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(resp.Body).Decode(&errBody) == nil && errBody.Error != "" {
			return mcpgo.NewToolResultError(fmt.Sprintf("server error (%d): %s", resp.StatusCode, errBody.Error)), nil
		}
		return mcpgo.NewToolResultError(fmt.Sprintf("server returned %d", resp.StatusCode)), nil
	}

	var listResp struct {
		Documents []struct {
			ID         string `json:"id"`
			Title      string `json:"title"`
			Author     string `json:"author"`
			Visibility string `json:"visibility"`
			CreatedAt  string `json:"created_at"`
			URL        string `json:"url"`
		} `json:"documents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return mcpgo.NewToolResultError("failed to parse server response"), nil
	}
	docs := listResp.Documents

	if len(docs) == 0 {
		return mcpgo.NewToolResultText("No documents found."), nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d document(s):\n\n", len(docs))
	for _, d := range docs {
		fmt.Fprintf(&sb, "- [%s](%s) (ID: %s", d.Title, d.URL, d.ID)
		if d.Author != "" {
			fmt.Fprintf(&sb, ", by %s", d.Author)
		}
		fmt.Fprintf(&sb, ")\n")
	}
	return mcpgo.NewToolResultText(sb.String()), nil
}

func (s *Server) handleGet(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	id := req.GetString("id", "")
	if id == "" {
		return mcpgo.NewToolResultError("id is required"), nil
	}

	httpReq, err := http.NewRequest(http.MethodGet, s.baseURL+"/api/documents/"+id, nil)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("failed to build request: %v", err)), nil
	}
	if s.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("failed to reach PasteAI server: %v", err)), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return mcpgo.NewToolResultError(fmt.Sprintf("document %q not found", id)), nil
	}
	if resp.StatusCode != http.StatusOK {
		return mcpgo.NewToolResultError(fmt.Sprintf("server returned %d", resp.StatusCode)), nil
	}

	var result struct {
		ID         string `json:"id"`
		Title      string `json:"title"`
		Content    string `json:"content"`
		Author     string `json:"author"`
		Visibility string `json:"visibility"`
		CreatedAt  string `json:"created_at"`
		URL        string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return mcpgo.NewToolResultError("failed to parse server response"), nil
	}

	return mcpgo.NewToolResultText(fmt.Sprintf(
		"Title: %s\nID: %s\nURL: %s\nVisibility: %s\nCreated: %s\n\n---\n\n%s",
		result.Title, result.ID, result.URL, result.Visibility, result.CreatedAt, result.Content,
	)), nil
}

func (s *Server) handleUpdate(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	id := req.GetString("id", "")
	title := req.GetString("title", "")
	content := req.GetString("content", "")

	if id == "" {
		return mcpgo.NewToolResultError("id is required"), nil
	}
	if title == "" && content == "" {
		return mcpgo.NewToolResultError("at least one of title or content is required"), nil
	}

	payload := map[string]string{"title": title, "content": content}
	body, err := json.Marshal(payload)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("failed to serialise request: %v", err)), nil
	}

	httpReq, err := http.NewRequest(http.MethodPut, s.baseURL+"/api/documents/"+id, bytes.NewReader(body))
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("failed to build request: %v", err)), nil
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("failed to reach PasteAI server: %v", err)), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return mcpgo.NewToolResultError(fmt.Sprintf("document %q not found", id)), nil
	}
	if resp.StatusCode != http.StatusOK {
		var errBody struct{ Error string `json:"error"` }
		if json.NewDecoder(resp.Body).Decode(&errBody) == nil && errBody.Error != "" {
			return mcpgo.NewToolResultError(fmt.Sprintf("server error (%d): %s", resp.StatusCode, errBody.Error)), nil
		}
		return mcpgo.NewToolResultError(fmt.Sprintf("server returned %d", resp.StatusCode)), nil
	}

	var result struct {
		URL string `json:"url"`
		ID  string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return mcpgo.NewToolResultError("failed to parse server response"), nil
	}
	return mcpgo.NewToolResultText(fmt.Sprintf("Document updated.\nURL: %s\nID: %s", result.URL, result.ID)), nil
}

func (s *Server) handleDelete(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	id := req.GetString("id", "")
	if id == "" {
		return mcpgo.NewToolResultError("id is required"), nil
	}

	httpReq, err := http.NewRequest(http.MethodDelete, s.baseURL+"/api/documents/"+id, nil)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("failed to build request: %v", err)), nil
	}
	if s.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("failed to reach PasteAI server: %v", err)), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return mcpgo.NewToolResultError(fmt.Sprintf("document %q not found", id)), nil
	}
	if resp.StatusCode != http.StatusNoContent {
		return mcpgo.NewToolResultError(fmt.Sprintf("server returned %d", resp.StatusCode)), nil
	}

	return mcpgo.NewToolResultText(fmt.Sprintf("Document %q deleted.", id)), nil
}

func (s *Server) handleSearch(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	query := req.GetString("query", "")
	if query == "" {
		return mcpgo.NewToolResultError("query is required"), nil
	}

	httpReq, err := http.NewRequest(http.MethodGet, s.baseURL+"/api/search?q="+url.QueryEscape(query), nil)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("failed to build request: %v", err)), nil
	}
	if s.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("failed to reach PasteAI server: %v", err)), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(resp.Body).Decode(&errBody) == nil && errBody.Error != "" {
			return mcpgo.NewToolResultError(fmt.Sprintf("server error (%d): %s", resp.StatusCode, errBody.Error)), nil
		}
		return mcpgo.NewToolResultError(fmt.Sprintf("server returned %d", resp.StatusCode)), nil
	}

	var searchResp struct {
		Documents []struct {
			ID         string `json:"id"`
			Title      string `json:"title"`
			Author     string `json:"author"`
			Visibility string `json:"visibility"`
			CreatedAt  string `json:"created_at"`
			URL        string `json:"url"`
		} `json:"documents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return mcpgo.NewToolResultError("failed to parse server response"), nil
	}
	docs := searchResp.Documents

	if len(docs) == 0 {
		return mcpgo.NewToolResultText(fmt.Sprintf("No documents found matching %q.", query)), nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d document(s) matching %q:\n\n", len(docs), query)
	for _, d := range docs {
		fmt.Fprintf(&sb, "- [%s](%s) (ID: %s", d.Title, d.URL, d.ID)
		if d.Author != "" {
			fmt.Fprintf(&sb, ", by %s", d.Author)
		}
		fmt.Fprintf(&sb, ")\n")
	}
	return mcpgo.NewToolResultText(sb.String()), nil
}

func (s *Server) handleSetVisibility(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	id := req.GetString("id", "")
	visibility := req.GetString("visibility", "")

	if id == "" {
		return mcpgo.NewToolResultError("id is required"), nil
	}
	if visibility != "public" && visibility != "unlisted" && visibility != "private" {
		return mcpgo.NewToolResultError("visibility must be public, unlisted, or private"), nil
	}

	body, err := json.Marshal(map[string]string{"visibility": visibility})
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("failed to serialise request: %v", err)), nil
	}

	httpReq, err := http.NewRequest(http.MethodPatch, s.baseURL+"/api/documents/"+id+"/visibility", bytes.NewReader(body))
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("failed to build request: %v", err)), nil
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("failed to reach PasteAI server: %v", err)), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return mcpgo.NewToolResultError(fmt.Sprintf("document %q not found", id)), nil
	}
	if resp.StatusCode != http.StatusOK {
		var errBody struct{ Error string `json:"error"` }
		if json.NewDecoder(resp.Body).Decode(&errBody) == nil && errBody.Error != "" {
			return mcpgo.NewToolResultError(fmt.Sprintf("server error (%d): %s", resp.StatusCode, errBody.Error)), nil
		}
		return mcpgo.NewToolResultError(fmt.Sprintf("server returned %d", resp.StatusCode)), nil
	}

	return mcpgo.NewToolResultText(fmt.Sprintf("Document %q visibility set to %s.", id, visibility)), nil
}

// ── Embedded server helpers ────────────────────────────────

// isResponding does a quick GET to confirm a pasteai server is already up.
func isResponding(baseURL string) bool {
	c := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := c.Get(baseURL + "/api/documents")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

// startEmbedded opens the db, binds the given port, and starts the HTTP server
// in a goroutine. Binding synchronously means a port conflict is caught
// immediately rather than discovered later when forwarding tool calls to the
// wrong service.
func startEmbedded(port string, logger *log.Logger) (func(), error) {
	dbPath := embeddedDBPath()
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	boltStore, err := store.NewBolt(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	diskContent, err := store.NewDiskContent(store.DirFromDBPath(dbPath))
	if err != nil {
		boltStore.Close()
		return nil, fmt.Errorf("open content dir: %w", err)
	}
	addr := ":" + port
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		boltStore.Close()
		return nil, fmt.Errorf("port %s is already in use by another process (not pasteai): %w", port, err)
	}
	handler := server.NewServer(boltStore, diskContent, server.Options{
		Logger: logger,
	})
	httpSrv := &http.Server{Handler: handler}
	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "[pasteai] embedded server stopped: %v\n", err)
		}
	}()
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpSrv.Shutdown(ctx)
		boltStore.Close()
	}, nil
}

// waitForServer polls until the server responds or the timeout elapses.
func waitForServer(baseURL string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isResponding(baseURL) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// embeddedDBPath returns the default path for the embedded database.
func embeddedDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "pasteai.db"
	}
	return filepath.Join(home, ".pasteai", "documents.db")
}
