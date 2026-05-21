package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// httpMCPClient sends JSON-RPC requests to the streamable-HTTP MCP endpoint.
type httpMCPClient struct {
	url    string
	apiKey string
	client *http.Client
}

func newHTTPMCPClient(addr, apiKey string) *httpMCPClient {
	return &httpMCPClient{
		url:    "http://" + addr + "/mcp",
		apiKey: apiKey,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// post sends a JSON-RPC message and returns (decoded response, HTTP status).
// Notifications return (zero mcpMsg, 202).
func (c *httpMCPClient) post(t *testing.T, msg any) (mcpMsg, int) {
	t.Helper()
	body, _ := json.Marshal(msg)
	req, err := http.NewRequest(http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatalf("MCP HTTP post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted {
		return mcpMsg{}, http.StatusAccepted
	}
	var m mcpMsg
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode MCP HTTP response (status %d): %v", resp.StatusCode, err)
	}
	return m, resp.StatusCode
}

func (c *httpMCPClient) initialize(t *testing.T) {
	t.Helper()
	id := 1
	resp, status := c.post(t, mcpMsg{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1"},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("initialize: HTTP %d", status)
	}
	if resp.Error != nil {
		t.Fatalf("initialize error: %d %s", resp.Error.Code, resp.Error.Message)
	}
	c.post(t, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
}

func (c *httpMCPClient) callTool(t *testing.T, id int, name string, args map[string]any) toolResult {
	t.Helper()
	mid := id
	resp, status := c.post(t, mcpMsg{
		JSONRPC: "2.0",
		ID:      &mid,
		Method:  "tools/call",
		Params:  map[string]any{"name": name, "arguments": args},
	})
	if status != http.StatusOK {
		t.Fatalf("tools/call %s: HTTP %d", name, status)
	}
	if resp.Error != nil {
		t.Fatalf("tools/call %s: JSON-RPC error %d: %s", name, resp.Error.Code, resp.Error.Message)
	}
	var tr toolResult
	if err := json.Unmarshal(resp.Result, &tr); err != nil {
		t.Fatalf("parse tool result for %s: %v", name, err)
	}
	return tr
}

// ── Tests ──────────────────────────────────────────────────

func TestMCPHTTPInitialize(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	addr := startMCPHTTPServer(t)
	c := newHTTPMCPClient(addr, "")
	c.initialize(t)

	id := 2
	resp, _ := c.post(t, mcpMsg{JSONRPC: "2.0", ID: &id, Method: "tools/list"})
	if resp.Error != nil {
		t.Fatalf("tools/list error: %v", resp.Error)
	}
	raw := string(resp.Result)
	for _, tool := range []string{
		"publish_document", "list_documents", "get_document",
		"update_document", "delete_document",
	} {
		if !strings.Contains(raw, tool) {
			t.Errorf("%s not in tools/list response", tool)
		}
	}
}

func TestMCPHTTPPublishAndList(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	addr := startMCPHTTPServer(t)
	c := newHTTPMCPClient(addr, "")
	c.initialize(t)

	tr := c.callTool(t, 2, "publish_document", map[string]any{
		"title":   "HTTP MCP Doc",
		"content": "# Hello\n\nPublished via HTTP MCP.",
	})
	if tr.IsError {
		t.Fatalf("publish failed: %s", tr.Content[0].Text)
	}
	if !strings.Contains(tr.Content[0].Text, "published successfully") {
		t.Errorf("unexpected publish response: %s", tr.Content[0].Text)
	}

	tr2 := c.callTool(t, 3, "list_documents", map[string]any{})
	if tr2.IsError {
		t.Fatalf("list failed: %s", tr2.Content[0].Text)
	}
	if !strings.Contains(tr2.Content[0].Text, "HTTP MCP Doc") {
		t.Errorf("published doc not in list: %s", tr2.Content[0].Text)
	}
}

func TestMCPHTTPGetDocument(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	addr := startMCPHTTPServer(t)
	c := newHTTPMCPClient(addr, "")
	c.initialize(t)

	publish := c.callTool(t, 2, "publish_document", map[string]any{
		"title":   "Get Me",
		"content": "# Get Me\n\nSome content.",
	})
	if publish.IsError {
		t.Fatalf("publish failed: %s", publish.Content[0].Text)
	}
	docID := parseID(t, publish.Content[0].Text)

	tr := c.callTool(t, 3, "get_document", map[string]any{"id": docID})
	if tr.IsError {
		t.Fatalf("get_document failed: %s", tr.Content[0].Text)
	}
	got := tr.Content[0].Text
	if !strings.Contains(got, "Get Me") {
		t.Errorf("title missing from get response: %s", got)
	}
	if !strings.Contains(got, "# Get Me") {
		t.Errorf("content missing from get response: %s", got)
	}
}

func TestMCPHTTPUpdateDocument(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	addr := startMCPHTTPServer(t)
	c := newHTTPMCPClient(addr, "")
	c.initialize(t)

	publish := c.callTool(t, 2, "publish_document", map[string]any{
		"title":   "Before Update",
		"content": "original",
	})
	if publish.IsError {
		t.Fatalf("publish failed: %s", publish.Content[0].Text)
	}
	docID := parseID(t, publish.Content[0].Text)

	update := c.callTool(t, 3, "update_document", map[string]any{
		"id":      docID,
		"title":   "After Update",
		"content": "updated content",
	})
	if update.IsError {
		t.Fatalf("update_document failed: %s", update.Content[0].Text)
	}

	got := c.callTool(t, 4, "get_document", map[string]any{"id": docID})
	if got.IsError {
		t.Fatalf("get after update failed: %s", got.Content[0].Text)
	}
	text := got.Content[0].Text
	if !strings.Contains(text, "After Update") {
		t.Errorf("updated title not found: %s", text)
	}
	if !strings.Contains(text, "updated content") {
		t.Errorf("updated content not found: %s", text)
	}
}

func TestMCPHTTPDeleteDocument(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	addr := startMCPHTTPServer(t)
	c := newHTTPMCPClient(addr, "")
	c.initialize(t)

	publish := c.callTool(t, 2, "publish_document", map[string]any{
		"title":   "Delete Me",
		"content": "bye",
	})
	if publish.IsError {
		t.Fatalf("publish failed: %s", publish.Content[0].Text)
	}
	docID := parseID(t, publish.Content[0].Text)

	del := c.callTool(t, 3, "delete_document", map[string]any{"id": docID})
	if del.IsError {
		t.Fatalf("delete_document failed: %s", del.Content[0].Text)
	}
	if !strings.Contains(del.Content[0].Text, "deleted") {
		t.Errorf("unexpected delete response: %s", del.Content[0].Text)
	}

	// Document should be gone
	gone := c.callTool(t, 4, "get_document", map[string]any{"id": docID})
	if !gone.IsError {
		t.Errorf("expected error fetching deleted document, got: %s", gone.Content[0].Text)
	}
}

func TestMCPHTTPAuthRequired(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	addr := startMCPHTTPServerWithKey(t, "secret-key")

	// Request without auth should be rejected by the server's auth middleware.
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1"},
		},
	})
	req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth, got %d", resp.StatusCode)
	}

	// With the correct key, initialize should succeed.
	c := newHTTPMCPClient(addr, "secret-key")
	c.initialize(t)
}

// parseID extracts "ID: <value>" from a publish_document response.
func parseID(t *testing.T, text string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "ID: ") {
			return strings.TrimPrefix(line, "ID: ")
		}
	}
	t.Fatalf("could not parse ID from: %q", text)
	return ""
}
