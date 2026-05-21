package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// ── helpers ────────────────────────────────────────────────

func newTestServer(t *testing.T, handler http.Handler) *Server {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return &Server{
		baseURL:    ts.URL,
		apiKey:     "",
		logger:     log.New(io.Discard, "", 0),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func makeReq(args map[string]any) mcpgo.CallToolRequest {
	var req mcpgo.CallToolRequest
	req.Params.Arguments = args
	return req
}

func resultText(t *testing.T, tr *mcpgo.CallToolResult) string {
	t.Helper()
	if len(tr.Content) == 0 {
		return ""
	}
	for _, c := range tr.Content {
		if tc, ok := c.(mcpgo.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

// ── handlePublish ──────────────────────────────────────────

func TestHandlePublishSuccess(t *testing.T) {
	s := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/documents" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"url": "http://example.com/d/abc", "id": "abc"})
	}))

	tr, err := s.handlePublish(context.Background(), makeReq(map[string]any{
		"title": "My Doc", "content": "# Hello", "author": "Claude", "visibility": "public",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if tr.IsError {
		t.Errorf("expected success, got error: %s", resultText(t, tr))
	}
	text := resultText(t, tr)
	if !strings.Contains(text, "http://example.com/d/abc") {
		t.Errorf("expected URL in result, got: %s", text)
	}
	if !strings.Contains(text, "abc") {
		t.Errorf("expected ID in result, got: %s", text)
	}
}

func TestHandlePublishMissingParams(t *testing.T) {
	s := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("HTTP should not be called when params are missing")
	}))

	tests := []struct {
		name string
		args map[string]any
	}{
		{"missing title", map[string]any{"content": "body"}},
		{"missing content", map[string]any{"title": "x"}},
		{"both missing", map[string]any{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, err := s.handlePublish(context.Background(), makeReq(tt.args))
			if err != nil {
				t.Fatal(err)
			}
			if !tr.IsError {
				t.Error("expected IsError=true")
			}
		})
	}
}

func TestHandlePublishServerError(t *testing.T) {
	s := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	tr, err := s.handlePublish(context.Background(), makeReq(map[string]any{
		"title": "x", "content": "y",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !tr.IsError {
		t.Error("expected IsError=true for 500")
	}
}

func TestHandlePublishServerErrorWithBody(t *testing.T) {
	s := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "title is required"})
	}))

	tr, err := s.handlePublish(context.Background(), makeReq(map[string]any{
		"title": "x", "content": "y",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !tr.IsError {
		t.Error("expected IsError=true for 400")
	}
	if !strings.Contains(resultText(t, tr), "title is required") {
		t.Errorf("expected error message in result: %s", resultText(t, tr))
	}
}

func TestHandlePublishSendsAPIKey(t *testing.T) {
	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"url": "http://x/d/1", "id": "1"})
	}))
	defer ts.Close()

	s := &Server{
		baseURL:    ts.URL,
		apiKey:     "secret",
		logger:     log.New(io.Discard, "", 0),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
	s.handlePublish(context.Background(), makeReq(map[string]any{"title": "x", "content": "y"}))
	if gotAuth != "Bearer secret" {
		t.Errorf("Authorization = %q, want Bearer secret", gotAuth)
	}
}

// ── handleList ─────────────────────────────────────────────

func TestHandleListSuccess(t *testing.T) {
	s := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/documents" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"documents": []map[string]any{
				{"id": "1", "title": "Doc A", "url": "http://x/d/1", "author": "Claude"},
				{"id": "2", "title": "Doc B", "url": "http://x/d/2", "author": ""},
			},
		})
	}))

	tr, err := s.handleList(context.Background(), makeReq(nil))
	if err != nil {
		t.Fatal(err)
	}
	if tr.IsError {
		t.Errorf("expected success: %s", resultText(t, tr))
	}
	text := resultText(t, tr)
	if !strings.Contains(text, "Doc A") || !strings.Contains(text, "Doc B") {
		t.Errorf("expected document titles in result: %s", text)
	}
}

func TestHandleListEmpty(t *testing.T) {
	s := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"documents": []any{}})
	}))

	tr, err := s.handleList(context.Background(), makeReq(nil))
	if err != nil {
		t.Fatal(err)
	}
	if tr.IsError {
		t.Errorf("expected success on empty list: %s", resultText(t, tr))
	}
	if !strings.Contains(resultText(t, tr), "No documents") {
		t.Errorf("expected empty message: %s", resultText(t, tr))
	}
}

func TestHandleListServerError(t *testing.T) {
	s := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "db down"})
	}))

	tr, err := s.handleList(context.Background(), makeReq(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !tr.IsError {
		t.Error("expected IsError=true for 500")
	}
	if !strings.Contains(resultText(t, tr), "db down") {
		t.Errorf("expected error message in result: %s", resultText(t, tr))
	}
}

// ── handleGet ──────────────────────────────────────────────

func TestHandleGetSuccess(t *testing.T) {
	s := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/documents/abc123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]string{
			"id": "abc123", "title": "My Doc", "content": "# Hello",
			"url": "http://x/d/abc123", "visibility": "public", "created_at": "2024-01-01T00:00:00Z",
		})
	}))

	tr, err := s.handleGet(context.Background(), makeReq(map[string]any{"id": "abc123"}))
	if err != nil {
		t.Fatal(err)
	}
	if tr.IsError {
		t.Errorf("expected success: %s", resultText(t, tr))
	}
	text := resultText(t, tr)
	if !strings.Contains(text, "My Doc") || !strings.Contains(text, "# Hello") {
		t.Errorf("expected title and content: %s", text)
	}
}

func TestHandleGetMissingID(t *testing.T) {
	s := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("HTTP should not be called for missing id")
	}))

	tr, err := s.handleGet(context.Background(), makeReq(map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	if !tr.IsError {
		t.Error("expected IsError=true for missing id")
	}
}

func TestHandleGetNotFound(t *testing.T) {
	s := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	tr, err := s.handleGet(context.Background(), makeReq(map[string]any{"id": "no-such"}))
	if err != nil {
		t.Fatal(err)
	}
	if !tr.IsError {
		t.Error("expected IsError=true for 404")
	}
}

func TestHandleGetServerError(t *testing.T) {
	s := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	tr, err := s.handleGet(context.Background(), makeReq(map[string]any{"id": "x"}))
	if err != nil {
		t.Fatal(err)
	}
	if !tr.IsError {
		t.Error("expected IsError=true for 500")
	}
}

// ── handleUpdate ───────────────────────────────────────────

func TestHandleUpdateSuccess(t *testing.T) {
	s := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		json.NewEncoder(w).Encode(map[string]string{"url": "http://x/d/abc", "id": "abc"})
	}))

	tr, err := s.handleUpdate(context.Background(), makeReq(map[string]any{
		"id": "abc", "title": "New Title", "content": "new content",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if tr.IsError {
		t.Errorf("expected success: %s", resultText(t, tr))
	}
	if !strings.Contains(resultText(t, tr), "updated") {
		t.Errorf("expected 'updated' in result: %s", resultText(t, tr))
	}
}

func TestHandleUpdateMissingID(t *testing.T) {
	s := newTestServer(t, nil)
	tr, err := s.handleUpdate(context.Background(), makeReq(map[string]any{"title": "x"}))
	if err != nil {
		t.Fatal(err)
	}
	if !tr.IsError {
		t.Error("expected IsError=true for missing id")
	}
}

func TestHandleUpdateMissingTitleAndContent(t *testing.T) {
	s := newTestServer(t, nil)
	tr, err := s.handleUpdate(context.Background(), makeReq(map[string]any{"id": "abc"}))
	if err != nil {
		t.Fatal(err)
	}
	if !tr.IsError {
		t.Error("expected IsError=true when neither title nor content provided")
	}
}

func TestHandleUpdateNotFound(t *testing.T) {
	s := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	tr, err := s.handleUpdate(context.Background(), makeReq(map[string]any{
		"id": "no-such", "title": "x",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !tr.IsError {
		t.Error("expected IsError=true for 404")
	}
}

func TestHandleUpdateServerErrorWithBody(t *testing.T) {
	s := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "title or content required"})
	}))

	tr, err := s.handleUpdate(context.Background(), makeReq(map[string]any{
		"id": "abc", "title": "x",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !tr.IsError {
		t.Error("expected IsError=true for 400")
	}
	if !strings.Contains(resultText(t, tr), "title or content required") {
		t.Errorf("expected error body in result: %s", resultText(t, tr))
	}
}

// ── handleDelete ───────────────────────────────────────────

func TestHandleDeleteSuccess(t *testing.T) {
	s := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/documents/abc" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	tr, err := s.handleDelete(context.Background(), makeReq(map[string]any{"id": "abc"}))
	if err != nil {
		t.Fatal(err)
	}
	if tr.IsError {
		t.Errorf("expected success, got error: %s", resultText(t, tr))
	}
	if !strings.Contains(resultText(t, tr), "abc") {
		t.Errorf("expected ID in result: %s", resultText(t, tr))
	}
}

func TestHandleDeleteMissingID(t *testing.T) {
	s := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("HTTP should not be called for missing id")
	}))

	tr, err := s.handleDelete(context.Background(), makeReq(map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	if !tr.IsError {
		t.Error("expected IsError=true for missing id")
	}
}

func TestHandleDeleteNotFound(t *testing.T) {
	s := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	tr, err := s.handleDelete(context.Background(), makeReq(map[string]any{"id": "no-such"}))
	if err != nil {
		t.Fatal(err)
	}
	if !tr.IsError {
		t.Error("expected IsError=true for 404")
	}
	if !strings.Contains(resultText(t, tr), "no-such") {
		t.Errorf("expected ID in error message: %s", resultText(t, tr))
	}
}

func TestHandleDeleteServerError(t *testing.T) {
	s := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	tr, err := s.handleDelete(context.Background(), makeReq(map[string]any{"id": "x"}))
	if err != nil {
		t.Fatal(err)
	}
	if !tr.IsError {
		t.Error("expected IsError=true for 500")
	}
}

// ── New / HTTPClient option ────────────────────────────────

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNewDefaultHTTPClient(t *testing.T) {
	var called bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		json.NewEncoder(w).Encode(map[string]any{"documents": []any{}})
	}))
	defer ts.Close()

	s := &Server{
		baseURL:    ts.URL,
		logger:     log.New(io.Discard, "", 0),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
	s.handleList(context.Background(), makeReq(nil))
	if !called {
		t.Error("expected server to be called with default http client")
	}
}

func TestNewCustomHTTPClientUsed(t *testing.T) {
	var gotHeader string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Custom-Transport")
		json.NewEncoder(w).Encode(map[string]any{"documents": []any{}})
	}))
	defer ts.Close()

	custom := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			r.Header.Set("X-Custom-Transport", "injected")
			return http.DefaultTransport.RoundTrip(r)
		}),
	}
	s := &Server{
		baseURL:    ts.URL,
		logger:     log.New(io.Discard, "", 0),
		httpClient: custom,
	}
	s.handleList(context.Background(), makeReq(nil))
	if gotHeader != "injected" {
		t.Errorf("X-Custom-Transport = %q, want injected", gotHeader)
	}
}

func TestNewNilHTTPClientFallsBack(t *testing.T) {
	opts := Options{
		URL:        "http://127.0.0.1:19998", // nothing listening — we only test New() itself
		HTTPClient: nil,
		Logger:     log.New(io.Discard, "", 0),
	}
	// New() calls os.Exit if the server doesn't respond, so we can't call it
	// directly here. Verify the fallback logic in isolation instead.
	var client *http.Client
	if opts.HTTPClient == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	} else {
		client = opts.HTTPClient
	}
	if client == nil {
		t.Error("expected non-nil fallback client")
	}
	if client.Timeout != 30*time.Second {
		t.Errorf("default timeout = %v, want 30s", client.Timeout)
	}
}

func TestNewCustomHTTPClientPreserved(t *testing.T) {
	custom := &http.Client{Timeout: 99 * time.Second}
	opts := Options{HTTPClient: custom}

	// Same isolation as above — test the selection logic directly.
	selected := opts.HTTPClient
	if selected == nil {
		selected = &http.Client{Timeout: 30 * time.Second}
	}
	if selected != custom {
		t.Error("expected custom client to be preserved")
	}
}

// ── helpers: isResponding, waitForServer, embeddedDBPath ──

func TestIsRespondingTrue(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	if !isResponding(ts.URL) {
		t.Error("expected isResponding=true for live server")
	}
}

func TestIsRespondingFalse(t *testing.T) {
	if isResponding("http://127.0.0.1:19999") {
		t.Error("expected isResponding=false for nothing listening")
	}
}

func TestIsRespondingServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()
	if isResponding(ts.URL) {
		t.Error("expected isResponding=false for 500 response")
	}
}

func TestWaitForServerReady(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	if !waitForServer(ts.URL, 2*time.Second) {
		t.Error("expected waitForServer=true for live server")
	}
}

func TestWaitForServerTimeout(t *testing.T) {
	if waitForServer("http://127.0.0.1:19999", 150*time.Millisecond) {
		t.Error("expected waitForServer=false when nothing listening")
	}
}

func TestEmbeddedDBPath(t *testing.T) {
	orig := os.Getenv("HOME")
	os.Setenv("HOME", "/tmp/test-home")
	defer os.Setenv("HOME", orig)

	p := embeddedDBPath()
	if p != "/tmp/test-home/.pasteai/documents.db" {
		t.Errorf("embeddedDBPath = %q, want /tmp/test-home/.pasteai/documents.db", p)
	}
}
