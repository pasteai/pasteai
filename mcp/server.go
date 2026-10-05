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
			mcpgo.Description("The document content in markdown format. Mermaid diagram blocks (```mermaid) are rendered as diagrams."),
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

	resolveReviewTool := mcpgo.NewTool("resolve_review",
		mcpgo.WithDescription("Mark a review (comment) resolved once you have acted on it. Use the review ID from list_reviews."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("The document ID")),
		mcpgo.WithString("review_id", mcpgo.Required(), mcpgo.Description("The review ID from list_reviews")),
		mcpgo.WithBoolean("resolved", mcpgo.Description("Set false to reopen a resolved review. Defaults to true.")),
	)
	srv.AddTool(resolveReviewTool, s.handleResolveReview)

	replyReviewTool := mcpgo.NewTool("reply_to_review",
		mcpgo.WithDescription("Reply to a review (comment), for example to say how you addressed it. Use the review ID from list_reviews."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("The document ID")),
		mcpgo.WithString("review_id", mcpgo.Required(), mcpgo.Description("The review ID from list_reviews")),
		mcpgo.WithString("body", mcpgo.Required(), mcpgo.Description("The reply text")),
	)
	srv.AddTool(replyReviewTool, s.handleReplyToReview)

	listReviewsTool := mcpgo.NewTool("list_reviews",
		mcpgo.WithDescription("List human reviews (comments) on a PasteAI document. Use this to read feedback before revising with update_document."),
		mcpgo.WithString("id",
			mcpgo.Required(),
			mcpgo.Description("The document ID"),
		),
		mcpgo.WithBoolean("include_resolved",
			mcpgo.Description("Include resolved comments in the response (default false)"),
		),
	)
	srv.AddTool(listReviewsTool, s.handleListReviews)

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

	// Only send what the caller actually supplied. Sending an empty title asks
	// the server to blank it, which is not what "omit to keep existing" means.
	payload := map[string]string{}
	if title != "" {
		payload["title"] = title
	}
	if content != "" {
		payload["content"] = content
	}
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
		var errBody struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(resp.Body).Decode(&errBody) == nil && errBody.Error != "" {
			return mcpgo.NewToolResultError(fmt.Sprintf("server error (%d): %s", resp.StatusCode, errBody.Error)), nil
		}
		return mcpgo.NewToolResultError(fmt.Sprintf("server returned %d", resp.StatusCode)), nil
	}

	return mcpgo.NewToolResultText(fmt.Sprintf("Document %q visibility set to %s.", id, visibility)), nil
}

// reviewComment is the subset of the comments API the MCP tools need.
type reviewComment struct {
	ID          string `json:"id"`
	Author      string `json:"author"`
	Body        string `json:"body"`
	QuotedText  string `json:"quoted_text"`
	StartChar   int    `json:"start_char"`
	EndChar     int    `json:"end_char"`
	ParentID    string `json:"parent_id"`
	RevisionNum int    `json:"revision_num"`
	Resolved    bool   `json:"resolved"`
	CreatedAt   string `json:"created_at"`
}

// writeReview renders one review and its replies. The id is included so the
// agent can resolve or reply to this exact review afterwards.
func writeReview(sb *strings.Builder, c reviewComment, replies []reviewComment) {
	status := "open"
	if c.Resolved {
		status = "resolved"
	}
	fmt.Fprintf(sb, "### Review %s\n", c.ID)
	fmt.Fprintf(sb, "[%s] %s (%s", status, reviewAuthor(c.Author), c.CreatedAt)
	if c.RevisionNum > 0 {
		fmt.Fprintf(sb, ", written against v%d", c.RevisionNum)
	}
	fmt.Fprintf(sb, ")\n")
	fmt.Fprintf(sb, "> %q\n", c.QuotedText)
	fmt.Fprintf(sb, "%s\n", c.Body)
	for _, r := range replies {
		fmt.Fprintf(sb, "  - reply from %s (%s): %s\n", reviewAuthor(r.Author), r.CreatedAt, r.Body)
	}
	fmt.Fprintf(sb, "\n---\n\n")
}

func reviewAuthor(a string) string {
	if a == "" {
		return "anonymous"
	}
	return a
}

// fetchReviews returns every comment on a document, replies included.
func (s *Server) fetchReviews(docID string) ([]reviewComment, error) {
	httpReq, err := http.NewRequest(http.MethodGet, s.baseURL+"/api/documents/"+docID+"/comments", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if s.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)
	}
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("reach PasteAI server: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}
	var comments []reviewComment
	if err := json.NewDecoder(resp.Body).Decode(&comments); err != nil {
		return nil, fmt.Errorf("parse server response: %w", err)
	}
	return comments, nil
}

// handleResolveReview marks a review resolved, so an agent can retire feedback
// it has acted on instead of leaving every document permanently open.
func (s *Server) handleResolveReview(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	docID := req.GetString("id", "")
	reviewID := req.GetString("review_id", "")
	if docID == "" || reviewID == "" {
		return mcpgo.NewToolResultError("id and review_id are required"), nil
	}
	resolved := req.GetBool("resolved", true)

	body, err := json.Marshal(map[string]bool{"resolved": resolved})
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("failed to serialise request: %v", err)), nil
	}
	httpReq, err := http.NewRequest(http.MethodPatch,
		s.baseURL+"/api/documents/"+docID+"/comments/"+reviewID, bytes.NewReader(body))
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
	if msg := reviewHTTPError(resp, reviewID); msg != "" {
		return mcpgo.NewToolResultError(msg), nil
	}
	verb := "resolved"
	if !resolved {
		verb = "reopened"
	}
	return mcpgo.NewToolResultText(fmt.Sprintf("Review %s %s.", reviewID, verb)), nil
}

// handleReplyToReview posts a reply to a review. The reply inherits the
// parent's anchor, so the server keeps it attached to the same passage.
func (s *Server) handleReplyToReview(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	docID := req.GetString("id", "")
	reviewID := req.GetString("review_id", "")
	replyBody := req.GetString("body", "")
	if docID == "" || reviewID == "" || replyBody == "" {
		return mcpgo.NewToolResultError("id, review_id and body are required"), nil
	}

	comments, err := s.fetchReviews(docID)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("failed to read reviews: %v", err)), nil
	}
	var parent *reviewComment
	for i := range comments {
		if comments[i].ID == reviewID {
			parent = &comments[i]
			break
		}
	}
	if parent == nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("review %q not found on document %q", reviewID, docID)), nil
	}

	body, err := json.Marshal(map[string]any{
		"body":        replyBody,
		"parent_id":   parent.ID,
		"quoted_text": parent.QuotedText,
		"start_char":  parent.StartChar,
		"end_char":    parent.EndChar,
	})
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("failed to serialise request: %v", err)), nil
	}
	httpReq, err := http.NewRequest(http.MethodPost,
		s.baseURL+"/api/documents/"+docID+"/comments", bytes.NewReader(body))
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
	if msg := reviewHTTPError(resp, reviewID); msg != "" {
		return mcpgo.NewToolResultError(msg), nil
	}
	return mcpgo.NewToolResultText(fmt.Sprintf("Replied to review %s.", reviewID)), nil
}

// reviewHTTPError maps a non-success response to a user-facing message,
// returning "" when the response was a success.
func reviewHTTPError(resp *http.Response, reviewID string) string {
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Sprintf("review %q not found", reviewID)
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized:
		return "not authorised to modify this review"
	case resp.StatusCode >= 300:
		return fmt.Sprintf("server returned %d", resp.StatusCode)
	}
	return ""
}

func (s *Server) handleListReviews(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	id := req.GetString("id", "")
	if id == "" {
		return mcpgo.NewToolResultError("id is required"), nil
	}
	includeResolved := req.GetBool("include_resolved", false)

	httpReq, err := http.NewRequest(http.MethodGet, s.baseURL+"/api/documents/"+id+"/comments", nil)
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
	if resp.StatusCode == http.StatusNotImplemented || resp.StatusCode == http.StatusMethodNotAllowed {
		return mcpgo.NewToolResultError("review system not available on this server"), nil
	}
	if resp.StatusCode != http.StatusOK {
		return mcpgo.NewToolResultError(fmt.Sprintf("server returned %d", resp.StatusCode)), nil
	}

	var comments []reviewComment
	if err := json.NewDecoder(resp.Body).Decode(&comments); err != nil {
		return mcpgo.NewToolResultError("failed to parse server response"), nil
	}

	var sb strings.Builder
	count := 0
	byParent := map[string][]reviewComment{}
	for _, c := range comments {
		if c.ParentID != "" {
			byParent[c.ParentID] = append(byParent[c.ParentID], c)
		}
	}
	for _, c := range comments {
		// Replies are rendered under their parent, never as reviews of their own:
		// they inherit the parent's quoted text and would read as duplicates.
		if c.ParentID != "" {
			continue
		}
		if !includeResolved && c.Resolved {
			continue
		}
		count++
		if count == 1 {
			fmt.Fprintf(&sb, "## Reviews for document %s\n\n", id)
		}
		writeReview(&sb, c, byParent[c.ID])
	}

	if count == 0 {
		if includeResolved {
			return mcpgo.NewToolResultText(fmt.Sprintf("No reviews found for document %q.", id)), nil
		}
		return mcpgo.NewToolResultText(fmt.Sprintf("No open reviews for document %q.", id)), nil
	}
	return mcpgo.NewToolResultText(sb.String()), nil
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
