package api_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/pasteai/pasteai/internal/store"
)

func newTestServer(t *testing.T) (*httptest.Server, store.Store) {
	t.Helper()
	return newServerWithBaseURL(t, "")
}

func mustGet(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

func mustPost(t *testing.T, url, contentType, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, contentType, strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	return buf.String()
}


// ── /api/documents ─────────────────────────────────────────

func TestCreateDocument(t *testing.T) {
	ts, _ := newTestServer(t)

	body := `{"title":"My Report","content":"# Hello","author":"Claude"}`
	resp := mustPost(t, ts.URL+"/api/documents", "application/json", body)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want 201", resp.StatusCode)
	}

	var result map[string]any
	decodeJSON(t, resp.Body, &result)

	if result["id"] == "" {
		t.Error("expected non-empty id")
	}
	if result["url"] == nil {
		t.Error("expected url in response")
	}
	if result["content"] == nil {
		t.Error("expected content in response")
	}
	if result["title"] != "My Report" {
		t.Errorf("title = %v, want My Report", result["title"])
	}
}

func TestCreateDocumentMissingTitle(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := mustPost(t, ts.URL+"/api/documents", "application/json", `{"content":"body"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestCreateDocumentMissingContent(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := mustPost(t, ts.URL+"/api/documents", "application/json", `{"title":"title"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestCreateDocumentInvalidJSON(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := mustPost(t, ts.URL+"/api/documents", "application/json", `not-json`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestListDocuments(t *testing.T) {
	ts, s := newTestServer(t)
	ctx := context.Background()

	s.Create(ctx, store.Document{Title: "Doc A", Content: "a", Visibility: store.VisibilityPublic})
	s.Create(ctx, store.Document{Title: "Doc B", Content: "b", Visibility: store.VisibilityPublic})

	resp := mustGet(t, ts.URL+"/api/documents")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var listResp struct {
		Documents []map[string]any `json:"documents"`
		NextToken string           `json:"next_token"`
	}
	decodeJSON(t, resp.Body, &listResp)

	if len(listResp.Documents) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(listResp.Documents))
	}
	// content field should NOT be present in list response
	if _, ok := listResp.Documents[0]["content"]; ok {
		t.Error("list response should not include content field")
	}
}

func TestListDocumentsEmpty(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := mustGet(t, ts.URL+"/api/documents")
	defer resp.Body.Close()

	var listResp struct {
		Documents []any  `json:"documents"`
		NextToken string `json:"next_token"`
	}
	decodeJSON(t, resp.Body, &listResp)
	if len(listResp.Documents) != 0 {
		t.Errorf("expected empty list, got %d items", len(listResp.Documents))
	}
}

func TestGetDocument(t *testing.T) {
	ts, s := newTestServer(t)
	doc, _ := s.Create(context.Background(), store.Document{Title: "Test", Content: "# body"})

	resp := mustGet(t, fmt.Sprintf("%s/api/documents/%s", ts.URL, doc.ID))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]any
	decodeJSON(t, resp.Body, &result)
	if result["content"] != "# body" {
		t.Errorf("content = %v, want '# body'", result["content"])
	}
}

func TestGetDocumentNotFound(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := mustGet(t, ts.URL+"/api/documents/does-not-exist")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestDeleteDocument(t *testing.T) {
	ts, s := newTestServer(t)
	doc, _ := s.Create(context.Background(), store.Document{Title: "To Delete", Content: "bye"})

	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/documents/%s", ts.URL, doc.ID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("DELETE status = %d, want 204", resp.StatusCode)
	}

	// Verify the document is gone from the API
	get := mustGet(t, fmt.Sprintf("%s/api/documents/%s", ts.URL, doc.ID))
	defer get.Body.Close()
	if get.StatusCode != http.StatusNotFound {
		t.Errorf("GET after DELETE status = %d, want 404", get.StatusCode)
	}

	// Verify it no longer appears in the list
	list := mustGet(t, ts.URL+"/api/documents")
	defer list.Body.Close()
	var listResp struct {
		Documents []map[string]any `json:"documents"`
	}
	decodeJSON(t, list.Body, &listResp)
	for _, d := range listResp.Documents {
		if d["id"] == doc.ID {
			t.Error("deleted document still appears in list")
		}
	}
}

func TestDeleteDocumentNotFound(t *testing.T) {
	ts, _ := newTestServer(t)
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/documents/does-not-exist", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("DELETE status = %d, want 404", resp.StatusCode)
	}
}

func TestAPIResponseContentType(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := mustGet(t, ts.URL+"/api/documents")
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

// ── Web UI pages ───────────────────────────────────────────

func TestHomePageReturnsHTML(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := mustGet(t, ts.URL+"/")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}

func TestHomePageStructure(t *testing.T) {
	ts, s := newTestServer(t)
	s.Create(context.Background(), store.Document{
		Title:      "Hello Report",
		Content:    "body",
		Author:     "Claude",
		Visibility: store.VisibilityPublic,
	})

	resp := mustGet(t, ts.URL+"/")
	defer resp.Body.Close()
	html := readBody(t, resp)

	checks := []struct {
		name string
		want string
	}{
		{"nav logo", `class="nav-logo"`},
		{"theme picker", `class="theme-picker"`},
		{"document list", `class="doc-list"`},
		{"document card", `class="doc-card"`},
		{"document title", "Hello Report"},
		{"document author", "Claude"},
		{"anti-FOUC script", "localStorage.getItem('pasteai-theme')"},
		{"all 6 themes present", "catppuccin-frappe"},
	}

	for _, c := range checks {
		if !strings.Contains(html, c.want) {
			t.Errorf("home page missing %s: %q not found", c.name, c.want)
		}
	}
}

func TestHomePageEmptyState(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := mustGet(t, ts.URL+"/")
	defer resp.Body.Close()
	html := readBody(t, resp)

	if !strings.Contains(html, "empty-state") {
		t.Error("expected empty-state element when no documents")
	}
}

func TestDocumentPageRendersMarkdown(t *testing.T) {
	ts, s := newTestServer(t)
	doc, _ := s.Create(context.Background(), store.Document{
		Title:      "My Report",
		Content:    "# Heading One\n\nSome **bold** text.\n\n## Heading Two\n\nMore content.",
		Author:     "Claude",
		Visibility: store.VisibilityPublic,
	})

	resp := mustGet(t, fmt.Sprintf("%s/d/%s", ts.URL, doc.ID))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	html := readBody(t, resp)

	checks := []struct {
		name string
		want string
	}{
		{"page title", "My Report — PasteAI"},
		{"doc header", `class="doc-header"`},
		{"document title in header", "My Report"},
		{"author", "Claude"},
		{"markdown body", `class="markdown-body"`},
		{"rendered h1", `<h1 id="heading-one">`},
		{"rendered bold", "<strong>bold</strong>"},
		{"rendered h2", `<h2 id="heading-two">`},
		{"toc panel", `class="toc-panel"`},
		{"toc heading 1", `toc-h1`},
		{"toc heading 2", `toc-h2`},
		{"toc link", `href="#heading-one"`},
	}

	for _, c := range checks {
		if !strings.Contains(html, c.want) {
			t.Errorf("document page missing %s: %q not found in HTML", c.name, c.want)
		}
	}
}

func TestDocumentPageNoTOCWhenNoHeadings(t *testing.T) {
	ts, s := newTestServer(t)
	doc, _ := s.Create(context.Background(), store.Document{
		Title:   "Plain",
		Content: "Just a paragraph, no headings.",
	})

	resp := mustGet(t, fmt.Sprintf("%s/d/%s", ts.URL, doc.ID))
	defer resp.Body.Close()
	html := readBody(t, resp)

	if strings.Contains(html, `class="toc-panel"`) {
		t.Error("toc-panel should not appear when document has no headings")
	}
}

func TestDocumentPageNotFound(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := mustGet(t, ts.URL+"/d/does-not-exist")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestDocumentPageSyntaxHighlighting(t *testing.T) {
	ts, s := newTestServer(t)
	doc, _ := s.Create(context.Background(), store.Document{
		Title:   "Code",
		Content: "```go\nfmt.Println(\"hello\")\n```",
	})

	resp := mustGet(t, fmt.Sprintf("%s/d/%s", ts.URL, doc.ID))
	defer resp.Body.Close()
	html := readBody(t, resp)

	if !strings.Contains(html, `<pre class="chroma"`) {
		t.Error("expected Chroma-highlighted <pre class=\"chroma\"> for code block")
	}
	if !strings.Contains(html, `class="chroma"`) {
		t.Error("expected class-based Chroma output")
	}
}

// TestDocumentPageCodeBlockThemeCSS is the self-serve regression test for the
// "code blocks don't change with theme" bug. It asserts two invariants that
// together guarantee the CSS variable controls the background:
//
//  1. The injected <style> block has NO hardcoded background-color on the
//     PreWrapper rule — if it did, it would fight (and potentially beat) our
//     CSS variable depending on cascade order.
//
//  2. style.css contains `.markdown-body pre.chroma { background: var(--color-surface-card-strong) }`
//     which is the rule that reads the per-theme CSS variable.
func TestDocumentPageCodeBlockThemeCSS(t *testing.T) {
	ts, s := newTestServer(t)
	doc, _ := s.Create(context.Background(), store.Document{
		Title:   "Theme Test",
		Content: "```go\nfmt.Println(\"hello\")\n```",
	})

	// ── 1. Inspect the injected <style> block ──────────────────
	resp := mustGet(t, fmt.Sprintf("%s/d/%s", ts.URL, doc.ID))
	defer resp.Body.Close()
	html := readBody(t, resp)

	styleStart := strings.Index(html, "<style>")
	styleEnd := strings.Index(html, "</style>")
	if styleStart == -1 || styleEnd == -1 {
		t.Fatal("document page must inject a <style> block for Chroma CSS")
	}
	injectedCSS := html[styleStart : styleEnd+8]

	// No unscoped .bg rule — it would bleed onto arbitrary page elements.
	if strings.Contains(injectedCSS, "/* Background */") {
		t.Error("injected Chroma CSS must not contain the unscoped .bg Background rule")
	}

	// PreWrapper must not hardcode background-color — that fights the CSS variable.
	for _, line := range strings.Split(injectedCSS, "\n") {
		if strings.Contains(line, "/* PreWrapper */") && strings.Contains(line, "background-color") {
			t.Errorf("PreWrapper rule must not set background-color (overrides CSS variable): %s", line)
		}
	}

	// Both theme-group scopes must be present so tokens colour correctly.
	for _, scope := range []string{`data-theme="light"`, `data-theme="dark"`} {
		if !strings.Contains(injectedCSS, scope) {
			t.Errorf("injected CSS missing theme scope: %s", scope)
		}
	}

	// ── 2. Confirm style.css has the CSS-variable rule ─────────
	cssResp := mustGet(t, ts.URL+"/static/style.css")
	defer cssResp.Body.Close()
	cssBody := readBody(t, cssResp)

	if !strings.Contains(cssBody, "pre.chroma") {
		t.Error("style.css must contain a pre.chroma rule to control code block background")
	}
	if !strings.Contains(cssBody, "var(--color-surface-card-strong)") {
		t.Error("style.css pre.chroma rule must use var(--color-surface-card-strong) so background tracks the active theme")
	}
}

func TestStaticFileServed(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := mustGet(t, ts.URL+"/static/style.css")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/css") {
		t.Errorf("Content-Type = %q, want text/css", ct)
	}
}

func TestUnknownPathReturns404(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := mustGet(t, ts.URL+"/does-not-exist")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
