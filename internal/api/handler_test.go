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
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "valid",
			body:       `{"title":"My Report","content":"# Hello","author":"Claude"}`,
			wantStatus: http.StatusCreated,
		},
		{name: "missing title", body: `{"content":"body"}`, wantStatus: http.StatusBadRequest},
		{name: "missing content", body: `{"title":"title"}`, wantStatus: http.StatusBadRequest},
		{name: "invalid JSON", body: `not-json`, wantStatus: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := mustPost(t, ts.URL+"/api/documents", "application/json", tt.body)
			defer resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusCreated {
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
		})
	}
}

func TestListDocuments(t *testing.T) {
	tests := []struct {
		name          string
		seedTitles    []string
		wantCount     int
		wantNoContent bool
	}{
		{name: "empty", seedTitles: nil, wantCount: 0},
		{name: "with documents", seedTitles: []string{"Doc A", "Doc B"}, wantCount: 2, wantNoContent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, s := newTestServer(t)
			ctx := context.Background()
			for _, title := range tt.seedTitles {
				s.Create(ctx, store.Document{Title: title, Content: "c", Visibility: store.VisibilityPublic})
			}
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
			if len(listResp.Documents) != tt.wantCount {
				t.Errorf("got %d documents, want %d", len(listResp.Documents), tt.wantCount)
			}
			if tt.wantNoContent && len(listResp.Documents) > 0 {
				if _, ok := listResp.Documents[0]["content"]; ok {
					t.Error("list response must not include content field")
				}
			}
		})
	}
}

func TestGetDocument(t *testing.T) {
	ts, s := newTestServer(t)
	doc, _ := s.Create(context.Background(), store.Document{Title: "Test", Content: "# body"})

	tests := []struct {
		name        string
		id          string
		wantStatus  int
		wantContent string
	}{
		{name: "found", id: doc.ID, wantStatus: http.StatusOK, wantContent: "# body"},
		{name: "not found", id: "does-not-exist", wantStatus: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := mustGet(t, fmt.Sprintf("%s/api/documents/%s", ts.URL, tt.id))
			defer resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.wantContent != "" {
				var result map[string]any
				decodeJSON(t, resp.Body, &result)
				if result["content"] != tt.wantContent {
					t.Errorf("content = %v, want %q", result["content"], tt.wantContent)
				}
			}
		})
	}
}

func TestDeleteDocument(t *testing.T) {
	tests := []struct {
		name        string
		createFirst bool
		wantStatus  int
	}{
		{name: "success", createFirst: true, wantStatus: http.StatusNoContent},
		{name: "not found", createFirst: false, wantStatus: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, s := newTestServer(t)
			var id string
			if tt.createFirst {
				doc, _ := s.Create(context.Background(), store.Document{Title: "To Delete", Content: "bye"})
				id = doc.ID
			} else {
				id = "does-not-exist"
			}
			req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/documents/%s", ts.URL, id), nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("DELETE: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.createFirst {
				get := mustGet(t, fmt.Sprintf("%s/api/documents/%s", ts.URL, id))
				defer get.Body.Close()
				if get.StatusCode != http.StatusNotFound {
					t.Errorf("GET after DELETE status = %d, want 404", get.StatusCode)
				}
				list := mustGet(t, ts.URL+"/api/documents")
				defer list.Body.Close()
				var listResp struct {
					Documents []map[string]any `json:"documents"`
				}
				decodeJSON(t, list.Body, &listResp)
				for _, d := range listResp.Documents {
					if d["id"] == id {
						t.Error("deleted document still appears in list")
					}
				}
			}
		})
	}
}

func TestUpdateDocument(t *testing.T) {
	tests := []struct {
		name        string
		createFirst bool
		body        string
		wantStatus  int
	}{
		{
			name:        "success",
			createFirst: true,
			body:        `{"title":"Updated Title","content":"new content"}`,
			wantStatus:  http.StatusOK,
		},
		{
			name:        "not found",
			createFirst: false,
			body:        `{"title":"x","content":"y"}`,
			wantStatus:  http.StatusNotFound,
		},
		{
			name:        "empty body",
			createFirst: true,
			body:        `{}`,
			wantStatus:  http.StatusBadRequest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, s := newTestServer(t)
			var id string
			if tt.createFirst {
				doc, _ := s.Create(context.Background(), store.Document{Title: "Original", Content: "old"})
				id = doc.ID
			} else {
				id = "does-not-exist"
			}
			req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/api/documents/%s", ts.URL, id), strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("PUT: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusOK {
				var result map[string]any
				decodeJSON(t, resp.Body, &result)
				if result["title"] != "Updated Title" {
					t.Errorf("title = %v, want Updated Title", result["title"])
				}
				if result["content"] != "new content" {
					t.Errorf("content = %v, want new content", result["content"])
				}
			}
		})
	}
}

func TestRawDocument(t *testing.T) {
	ts, s := newTestServer(t)
	doc, _ := s.Create(context.Background(), store.Document{
		Title:   "Raw Test",
		Content: "# Hello\n\nworld",
	})

	tests := []struct {
		name       string
		id         string
		wantStatus int
		wantCT     string
		wantBody   string
	}{
		{
			name:       "found",
			id:         doc.ID,
			wantStatus: http.StatusOK,
			wantCT:     "text/plain",
			wantBody:   "# Hello\n\nworld",
		},
		{
			name:       "not found",
			id:         "does-not-exist",
			wantStatus: http.StatusNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := mustGet(t, fmt.Sprintf("%s/d/%s/raw", ts.URL, tt.id))
			defer resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.wantCT != "" {
				ct := resp.Header.Get("Content-Type")
				if !strings.Contains(ct, tt.wantCT) {
					t.Errorf("Content-Type = %q, want %s", ct, tt.wantCT)
				}
			}
			if tt.wantBody != "" {
				body := readBody(t, resp)
				if body != tt.wantBody {
					t.Errorf("body = %q, want %q", body, tt.wantBody)
				}
			}
		})
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

func TestHomePageHero(t *testing.T) {
	tests := []struct {
		name    string
		hasDocs bool
		wantIn  string
		wantOut string
	}{
		{name: "compact with documents", hasDocs: true, wantIn: "hero--compact"},
		{name: "full when empty", hasDocs: false, wantOut: "hero--compact"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, s := newTestServer(t)
			if tt.hasDocs {
				s.Create(context.Background(), store.Document{
					Title:      "Doc",
					Content:    "content",
					Visibility: store.VisibilityPublic,
				})
			}
			resp := mustGet(t, ts.URL+"/")
			defer resp.Body.Close()
			html := readBody(t, resp)
			if tt.wantIn != "" && !strings.Contains(html, tt.wantIn) {
				t.Errorf("want %q in HTML", tt.wantIn)
			}
			if tt.wantOut != "" && strings.Contains(html, tt.wantOut) {
				t.Errorf("want %q absent from HTML", tt.wantOut)
			}
		})
	}
}

func TestDocumentPage(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		author  string
		content string
		wantIn  []string
		wantOut []string
	}{
		{
			name:    "renders markdown with TOC",
			title:   "My Report",
			author:  "Claude",
			content: "# Heading One\n\nSome **bold** text.\n\n## Heading Two\n\nMore content.",
			wantIn: []string{
				"My Report — PasteAI",
				`class="doc-header"`,
				"My Report",
				"Claude",
				`class="markdown-body"`,
				`<h1 id="heading-one">`,
				"<strong>bold</strong>",
				`<h2 id="heading-two">`,
				`class="toc-panel"`,
				`toc-h1`,
				`toc-h2`,
				`href="#heading-one"`,
			},
		},
		{
			name:    "no TOC when no headings",
			content: "Just a paragraph, no headings.",
			wantOut: []string{`class="toc-panel"`, `class="toc-mobile"`},
		},
		{
			name:    "OG tags with description",
			content: "# Heading\n\nFirst paragraph of the document.",
			wantIn:  []string{`og:title`, `og:type`, `First paragraph of the document`, `twitter:card`, `name="description"`},
		},
		{
			name:    "OG tags omit description for heading-only content",
			content: "# Just A Heading",
			wantOut: []string{`name="description"`},
		},
		{
			name:    "breadcrumb",
			title:   "My Analysis",
			content: "content",
			wantIn:  []string{`class="doc-breadcrumb"`, `href="/"`, "My Analysis"},
		},
		{
			name:    "mobile TOC when headings present",
			content: "# Section One\n\nParagraph.\n\n## Section Two\n\nMore.",
			wantIn:  []string{`class="toc-mobile"`, `class="toc-mobile-toggle"`},
		},
		{
			name:    "no mobile TOC when no headings",
			content: "Just prose, no headings.",
			wantOut: []string{`class="toc-mobile"`},
		},
		{
			name:    "delete modal",
			content: "content",
			wantIn:  []string{`id="delete-modal"`, `modal-btn--danger`, `modal-btn--cancel`},
		},
		{
			name:    "syntax highlighting",
			content: "```go\nfmt.Println(\"hello\")\n```",
			wantIn:  []string{`<pre class="chroma"`, `class="chroma"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, s := newTestServer(t)
			title := tt.title
			if title == "" {
				title = "Test Doc"
			}
			doc, _ := s.Create(context.Background(), store.Document{
				Title:   title,
				Author:  tt.author,
				Content: tt.content,
			})
			resp := mustGet(t, fmt.Sprintf("%s/d/%s", ts.URL, doc.ID))
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want 200", resp.StatusCode)
			}
			html := readBody(t, resp)
			for _, want := range tt.wantIn {
				if !strings.Contains(html, want) {
					t.Errorf("want %q in HTML", want)
				}
			}
			for _, notWant := range tt.wantOut {
				if strings.Contains(html, notWant) {
					t.Errorf("want %q absent from HTML", notWant)
				}
			}
		})
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

// TestDocumentPageCodeBlockThemeCSS verifies two invariants that together
// guarantee CSS variables control code block backgrounds per-theme:
//  1. The injected <style> block has no hardcoded background-color on PreWrapper.
//  2. style.css uses var(--color-surface-card-strong) on pre.chroma.
func TestDocumentPageCodeBlockThemeCSS(t *testing.T) {
	ts, s := newTestServer(t)
	doc, _ := s.Create(context.Background(), store.Document{
		Title:   "Theme Test",
		Content: "```go\nfmt.Println(\"hello\")\n```",
	})

	resp := mustGet(t, fmt.Sprintf("%s/d/%s", ts.URL, doc.ID))
	defer resp.Body.Close()
	html := readBody(t, resp)

	styleStart := strings.Index(html, "<style>")
	styleEnd := strings.Index(html, "</style>")
	if styleStart == -1 || styleEnd == -1 {
		t.Fatal("document page must inject a <style> block for Chroma CSS")
	}
	injectedCSS := html[styleStart : styleEnd+8]

	if strings.Contains(injectedCSS, "/* Background */") {
		t.Error("injected Chroma CSS must not contain the unscoped .bg Background rule")
	}
	for _, line := range strings.Split(injectedCSS, "\n") {
		if strings.Contains(line, "/* PreWrapper */") && strings.Contains(line, "background-color") {
			t.Errorf("PreWrapper must not set background-color (overrides CSS variable): %s", line)
		}
	}
	for _, scope := range []string{`data-theme="light"`, `data-theme="dark"`} {
		if !strings.Contains(injectedCSS, scope) {
			t.Errorf("injected CSS missing theme scope: %s", scope)
		}
	}

	cssResp := mustGet(t, ts.URL+"/static/style.css")
	defer cssResp.Body.Close()
	cssBody := readBody(t, cssResp)

	if !strings.Contains(cssBody, "pre.chroma") {
		t.Error("style.css must contain a pre.chroma rule")
	}
	if !strings.Contains(cssBody, "var(--color-surface-card-strong)") {
		t.Error("style.css pre.chroma rule must use var(--color-surface-card-strong)")
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
