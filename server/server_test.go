package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pasteai/pasteai/server"
	"github.com/pasteai/pasteai/store"
)

// ── In-memory backends ─────────────────────────────────────

type memStore struct {
	mu   sync.Mutex
	docs map[string]*server.Document
	seq  int
}

func newMemStore() *memStore { return &memStore{docs: make(map[string]*server.Document)} }

func (m *memStore) Create(_ context.Context, doc server.Document) (*server.Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	doc.ID = fmt.Sprintf("test-%d", m.seq)
	doc.CreatedAt = time.Now()
	if doc.Visibility == "" {
		doc.Visibility = server.VisibilityPublic
	}
	doc.Content = ""
	cp := doc
	m.docs[doc.ID] = &cp
	return &cp, nil
}

func (m *memStore) List(_ context.Context, opts server.ListOptions) (*server.ListResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var docs []server.Document
	for _, d := range m.docs {
		if opts.OwnerID != "" {
			if d.OwnerID == opts.OwnerID {
				docs = append(docs, *d)
			}
		} else if d.Visibility == server.VisibilityPublic {
			docs = append(docs, *d)
		}
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].CreatedAt.After(docs[j].CreatedAt) })
	return &server.ListResult{Documents: docs}, nil
}

func (m *memStore) Get(_ context.Context, id string) (*server.Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.docs[id]
	if !ok {
		return nil, server.ErrNotFound
	}
	cp := *d
	cp.Content = ""
	return &cp, nil
}

func (m *memStore) Update(_ context.Context, id, title string) (*server.Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.docs[id]
	if !ok {
		return nil, server.ErrNotFound
	}
	if title != "" {
		d.Title = title
	}
	cp := *d
	cp.Content = ""
	return &cp, nil
}

func (m *memStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.docs[id]; !ok {
		return server.ErrNotFound
	}
	delete(m.docs, id)
	return nil
}

func (m *memStore) Close() error { return nil }

type memContent struct {
	mu      sync.Mutex
	content map[string][]byte
}

func newMemContent() *memContent { return &memContent{content: make(map[string][]byte)} }

func (m *memContent) Put(_ context.Context, id string, content []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(content))
	copy(cp, content)
	m.content[id] = cp
	return nil
}

func (m *memContent) Get(_ context.Context, id string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.content[id]
	if !ok {
		return nil, server.ErrNotFound
	}
	cp := make([]byte, len(c))
	copy(cp, c)
	return cp, nil
}

func (m *memContent) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.content, id)
	return nil
}

// testDB wraps both backends and provides a Create helper that seeds both.
type testDB struct {
	store   *memStore
	content *memContent
}

func (tb *testDB) Create(ctx context.Context, doc server.Document) (*server.Document, error) {
	raw := doc.Content
	created, err := tb.store.Create(ctx, doc)
	if err != nil {
		return nil, err
	}
	if raw != "" {
		if err := tb.content.Put(ctx, created.ID, []byte(raw)); err != nil {
			return nil, err
		}
		created.Content = raw
	}
	return created, nil
}

// ── Error injection backends ───────────────────────────────

var errInjected = errors.New("injected error")

type alwaysFailStore struct{}

func (*alwaysFailStore) Create(_ context.Context, _ server.Document) (*server.Document, error) {
	return nil, errInjected
}
func (*alwaysFailStore) List(_ context.Context, _ server.ListOptions) (*server.ListResult, error) {
	return nil, errInjected
}
func (*alwaysFailStore) Get(_ context.Context, _ string) (*server.Document, error) {
	return nil, errInjected
}
func (*alwaysFailStore) Update(_ context.Context, _, _ string) (*server.Document, error) {
	return nil, errInjected
}
func (*alwaysFailStore) Delete(_ context.Context, _ string) error { return errInjected }
func (*alwaysFailStore) Close() error                             { return nil }

type alwaysFailContent struct{}

func (*alwaysFailContent) Put(_ context.Context, _ string, _ []byte) error { return errInjected }
func (*alwaysFailContent) Get(_ context.Context, _ string) ([]byte, error) {
	return nil, errInjected
}
func (*alwaysFailContent) Delete(_ context.Context, _ string) error { return errInjected }

// failPutContent fails only on Put; Get/Delete delegate to the embedded memContent.
type failPutContent struct{ *memContent }

func (*failPutContent) Put(_ context.Context, _ string, _ []byte) error { return errInjected }

// failDeleteStore fails only on Delete; all other operations delegate to embedded memStore.
type failDeleteStore struct{ *memStore }

func (*failDeleteStore) Delete(_ context.Context, _ string) error { return errInjected }

// ── Test helpers ───────────────────────────────────────────

func newTestServer(t *testing.T) (*httptest.Server, *testDB) {
	t.Helper()
	return newServerWithBaseURL(t, "")
}

func newServerWithBaseURL(t *testing.T, baseURL string) (*httptest.Server, *testDB) {
	t.Helper()
	db := &testDB{store: newMemStore(), content: newMemContent()}
	handler := server.NewServer(db.store, db.content, server.Options{
		BaseURL: baseURL,
		Logger:  log.New(io.Discard, "", 0),
	})
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts, db
}

func newServerWith(t *testing.T, st server.Store, ct server.ContentBackend) *httptest.Server {
	t.Helper()
	handler := server.NewServer(st, ct, server.Options{Logger: log.New(io.Discard, "", 0)})
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts
}

func newServerWithAuth(t *testing.T, apiKey string) (*httptest.Server, *testDB) {
	t.Helper()
	db := &testDB{store: newMemStore(), content: newMemContent()}
	handler := server.NewServer(db.store, db.content, server.Options{
		Logger:       log.New(io.Discard, "", 0),
		AuthProvider: server.NewStaticKeyAuth(map[string]string{apiKey: "owner"}),
	})
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts, db
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

func decodeJSON(t *testing.T, r io.Reader, v any) {
	t.Helper()
	if err := json.NewDecoder(r).Decode(v); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
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
		{name: "title too long", body: `{"title":"` + strings.Repeat("x", 501) + `","content":"c"}`, wantStatus: http.StatusBadRequest},
		{name: "author too long", body: `{"title":"t","content":"c","author":"` + strings.Repeat("a", 201) + `"}`, wantStatus: http.StatusBadRequest},
		{name: "invalid visibility", body: `{"title":"t","content":"c","visibility":"secret"}`, wantStatus: http.StatusBadRequest},
		{name: "unlisted visibility", body: `{"title":"t","content":"c","visibility":"unlisted"}`, wantStatus: http.StatusCreated},
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
				if tt.name == "valid" && result["title"] != "My Report" {
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
			ts, db := newTestServer(t)
			ctx := context.Background()
			for _, title := range tt.seedTitles {
				db.Create(ctx, server.Document{Title: title, Content: "c", Visibility: server.VisibilityPublic})
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
	ts, db := newTestServer(t)
	doc, _ := db.Create(context.Background(), server.Document{Title: "Test", Content: "# body"})

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
			ts, db := newTestServer(t)
			var id string
			if tt.createFirst {
				doc, _ := db.Create(context.Background(), server.Document{Title: "To Delete", Content: "bye"})
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
			ts, db := newTestServer(t)
			var id string
			if tt.createFirst {
				doc, _ := db.Create(context.Background(), server.Document{Title: "Original", Content: "old"})
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
	ts, db := newTestServer(t)
	doc, _ := db.Create(context.Background(), server.Document{
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
	ts, db := newTestServer(t)
	db.Create(context.Background(), server.Document{
		Title:      "Hello Report",
		Content:    "body",
		Author:     "Claude",
		Visibility: server.VisibilityPublic,
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
			ts, db := newTestServer(t)
			if tt.hasDocs {
				db.Create(context.Background(), server.Document{
					Title:      "Doc",
					Content:    "content",
					Visibility: server.VisibilityPublic,
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
			ts, db := newTestServer(t)
			title := tt.title
			if title == "" {
				title = "Test Doc"
			}
			doc, _ := db.Create(context.Background(), server.Document{
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

func TestDocumentPageCodeBlockThemeCSS(t *testing.T) {
	ts, db := newTestServer(t)
	doc, _ := db.Create(context.Background(), server.Document{
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

func TestDocURLFromBaseURL(t *testing.T) {
	ts, _ := newServerWithBaseURL(t, "https://pasteai.io")
	body := `{"title":"Test","content":"body"}`
	resp, err := http.Post(ts.URL+"/api/documents", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result map[string]any
	decodeJSON(t, resp.Body, &result)
	url, _ := result["url"].(string)
	if !strings.HasPrefix(url, "https://pasteai.io/d/") {
		t.Errorf("url = %q, want prefix https://pasteai.io/d/", url)
	}
}

func TestDocURLDerivedFromRequest(t *testing.T) {
	ts, _ := newServerWithBaseURL(t, "")
	body := `{"title":"Test","content":"body"}`
	resp, err := http.Post(ts.URL+"/api/documents", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result map[string]any
	decodeJSON(t, resp.Body, &result)
	url, _ := result["url"].(string)
	if !strings.Contains(url, "/d/") {
		t.Errorf("url = %q, expected /d/ path", url)
	}
	if !strings.HasPrefix(url, "http://127.0.0.1") {
		t.Errorf("url = %q, expected http://127.0.0.1 prefix", url)
	}
}

// ── Error path tests ───────────────────────────────────────

func TestStoreListError(t *testing.T) {
	ts := newServerWith(t, &alwaysFailStore{}, newMemContent())
	for _, path := range []string{"/api/documents", "/"} {
		resp := mustGet(t, ts.URL+path)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("GET %s: status = %d, want 500", path, resp.StatusCode)
		}
	}
}

func TestStoreGetError(t *testing.T) {
	ts := newServerWith(t, &alwaysFailStore{}, newMemContent())
	for _, path := range []string{"/api/documents/x", "/d/x", "/d/x/raw"} {
		resp := mustGet(t, ts.URL+path)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("GET %s: status = %d, want 500", path, resp.StatusCode)
		}
	}
}

func TestGetDocumentContentMissing(t *testing.T) {
	ms := newMemStore()
	doc, _ := ms.Create(context.Background(), server.Document{Title: "test"})
	ts := newServerWith(t, ms, newMemContent()) // content not stored → ErrNotFound
	resp := mustGet(t, fmt.Sprintf("%s/api/documents/%s", ts.URL, doc.ID))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for missing content", resp.StatusCode)
	}
}

func TestGetDocumentContentError(t *testing.T) {
	ms := newMemStore()
	doc, _ := ms.Create(context.Background(), server.Document{Title: "test"})
	ts := newServerWith(t, ms, &alwaysFailContent{})
	resp := mustGet(t, fmt.Sprintf("%s/api/documents/%s", ts.URL, doc.ID))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 for content error", resp.StatusCode)
	}
}

func TestViewRawContentError(t *testing.T) {
	ms := newMemStore()
	doc, _ := ms.Create(context.Background(), server.Document{Title: "test"})
	ts := newServerWith(t, ms, &alwaysFailContent{})
	resp := mustGet(t, fmt.Sprintf("%s/d/%s/raw", ts.URL, doc.ID))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 for content error", resp.StatusCode)
	}
}

func TestViewDocumentContentError(t *testing.T) {
	ms := newMemStore()
	doc, _ := ms.Create(context.Background(), server.Document{Title: "test"})
	ts := newServerWith(t, ms, &alwaysFailContent{})
	resp := mustGet(t, fmt.Sprintf("%s/d/%s", ts.URL, doc.ID))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 for content error", resp.StatusCode)
	}
}

func TestCreateDocumentStoreError(t *testing.T) {
	ts := newServerWith(t, &alwaysFailStore{}, newMemContent())
	resp := mustPost(t, ts.URL+"/api/documents", "application/json", `{"title":"x","content":"y"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

func TestCreateDocumentRollback(t *testing.T) {
	ms := newMemStore()
	ts := newServerWith(t, ms, &failPutContent{newMemContent()})
	resp := mustPost(t, ts.URL+"/api/documents", "application/json", `{"title":"x","content":"y"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 after content failure", resp.StatusCode)
	}
	result, _ := ms.List(context.Background(), server.ListOptions{Limit: 10})
	if len(result.Documents) != 0 {
		t.Errorf("store rollback: got %d docs, want 0", len(result.Documents))
	}
}

func TestUpdateDocumentStoreError(t *testing.T) {
	ts := newServerWith(t, &alwaysFailStore{}, newMemContent())
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/documents/any",
		strings.NewReader(`{"title":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

func TestUpdateDocumentContentPutError(t *testing.T) {
	ms := newMemStore()
	doc, _ := ms.Create(context.Background(), server.Document{Title: "test"})
	ts := newServerWith(t, ms, &failPutContent{newMemContent()})
	req, _ := http.NewRequest(http.MethodPut,
		fmt.Sprintf("%s/api/documents/%s", ts.URL, doc.ID),
		strings.NewReader(`{"title":"x","content":"new"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

func TestUpdateDocumentTitleOnly(t *testing.T) {
	ts, db := newTestServer(t)
	doc, _ := db.Create(context.Background(), server.Document{Title: "Original", Content: "original content"})
	req, _ := http.NewRequest(http.MethodPut,
		fmt.Sprintf("%s/api/documents/%s", ts.URL, doc.ID),
		strings.NewReader(`{"title":"New Title"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var result map[string]any
	decodeJSON(t, resp.Body, &result)
	if result["title"] != "New Title" {
		t.Errorf("title = %v, want New Title", result["title"])
	}
	if result["content"] != "original content" {
		t.Errorf("content = %v, want original content", result["content"])
	}
}

func TestDeleteDocumentStoreError(t *testing.T) {
	ts := newServerWith(t, &alwaysFailStore{}, newMemContent())
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/documents/any", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

func TestCreateDocumentRollbackFailure(t *testing.T) {
	var logBuf bytes.Buffer
	fds := &failDeleteStore{newMemStore()}
	handler := server.NewServer(fds, &failPutContent{newMemContent()}, server.Options{
		Logger: log.New(&logBuf, "", 0),
	})
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	resp := mustPost(t, ts.URL+"/api/documents", "application/json", `{"title":"x","content":"y"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
	if !strings.Contains(logBuf.String(), "rollback") {
		t.Errorf("expected rollback failure in log, got: %s", logBuf.String())
	}
}

func TestUpdateDocumentContentGetError(t *testing.T) {
	ms := newMemStore()
	doc, _ := ms.Create(context.Background(), server.Document{Title: "test"})
	// content not seeded — content.Get returns ErrNotFound
	ts := newServerWith(t, ms, newMemContent())
	req, _ := http.NewRequest(http.MethodPut,
		fmt.Sprintf("%s/api/documents/%s", ts.URL, doc.ID),
		strings.NewReader(`{"title":"New Title"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 when content cannot be fetched", resp.StatusCode)
	}
}

func TestServerWithRealStore(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	boltStore, err := store.NewBolt(dbPath)
	if err != nil {
		t.Fatalf("NewBolt: %v", err)
	}
	t.Cleanup(func() { boltStore.Close() })

	diskContent, err := store.NewDiskContent(store.DirFromDBPath(dbPath))
	if err != nil {
		t.Fatalf("NewDiskContent: %v", err)
	}

	handler := server.NewServer(boltStore, diskContent, server.Options{
		Logger: log.New(io.Discard, "", 0),
	})
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	// Create
	body := `{"title":"Real Test","content":"# Hello\n\nWorld","author":"Go test"}`
	resp := mustPost(t, ts.URL+"/api/documents", "application/json", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status = %d", resp.StatusCode)
	}
	var created map[string]any
	decodeJSON(t, resp.Body, &created)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("expected non-empty id")
	}

	// Get — verifies BoltStore returns empty Content and handler fetches from DiskContent
	resp2 := mustGet(t, fmt.Sprintf("%s/api/documents/%s", ts.URL, id))
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("get: status = %d", resp2.StatusCode)
	}
	var got map[string]any
	decodeJSON(t, resp2.Body, &got)
	if got["title"] != "Real Test" {
		t.Errorf("title = %v, want Real Test", got["title"])
	}
	if got["content"] != "# Hello\n\nWorld" {
		t.Errorf("content = %v, want '# Hello\\n\\nWorld'", got["content"])
	}

	// Update
	req, _ := http.NewRequest(http.MethodPut,
		fmt.Sprintf("%s/api/documents/%s", ts.URL, id),
		strings.NewReader(`{"title":"Updated","content":"new body"}`))
	req.Header.Set("Content-Type", "application/json")
	resp3, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("update: status = %d", resp3.StatusCode)
	}

	// Delete
	req2, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/documents/%s", ts.URL, id), nil)
	resp4, _ := http.DefaultClient.Do(req2)
	resp4.Body.Close()
	if resp4.StatusCode != http.StatusNoContent {
		t.Errorf("delete: status = %d, want 204", resp4.StatusCode)
	}

	// Get after delete — DiskContent file should be gone
	resp5 := mustGet(t, fmt.Sprintf("%s/api/documents/%s", ts.URL, id))
	resp5.Body.Close()
	if resp5.StatusCode != http.StatusNotFound {
		t.Errorf("get after delete: status = %d, want 404", resp5.StatusCode)
	}
}

func TestShowDeleteWithAuth(t *testing.T) {
	ts, db := newServerWithAuth(t, "secret")
	doc, _ := db.Create(context.Background(), server.Document{Title: "Auth Test", Content: "body"})
	docURL := fmt.Sprintf("%s/d/%s", ts.URL, doc.ID)

	// Without auth: delete button must be absent (ShowDelete = false)
	resp := mustGet(t, docURL)
	defer resp.Body.Close()
	html := readBody(t, resp)
	if strings.Contains(html, `class="delete-btn"`) {
		t.Error("delete button must be absent without auth")
	}

	// With valid auth: delete button must be present (ShowDelete = true)
	req, _ := http.NewRequest(http.MethodGet, docURL, nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	html2 := readBody(t, resp2)
	if !strings.Contains(html2, `class="delete-btn"`) {
		t.Error("delete button must be present with valid auth")
	}
}

func TestAllowAnonymousWrites(t *testing.T) {
	db := &testDB{store: newMemStore(), content: newMemContent()}
	handler := server.NewServer(db.store, db.content, server.Options{
		Logger:               log.New(io.Discard, "", 0),
		AuthProvider:         server.NewStaticKeyAuth(map[string]string{"key": "owner"}),
		AllowAnonymousWrites: true,
	})
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	// Anonymous create of public doc — should succeed
	resp := mustPost(t, ts.URL+"/api/documents", "application/json",
		`{"title":"anon","content":"hello","visibility":"public"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("anonymous public create: got %d, want 201", resp.StatusCode)
	}

	// Anonymous create of unlisted doc — should succeed
	resp2 := mustPost(t, ts.URL+"/api/documents", "application/json",
		`{"title":"anon","content":"hello","visibility":"unlisted"}`)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusCreated {
		t.Errorf("anonymous unlisted create: got %d, want 201", resp2.StatusCode)
	}

	// Anonymous create of private doc — should be rejected with 403
	resp3 := mustPost(t, ts.URL+"/api/documents", "application/json",
		`{"title":"anon","content":"hello","visibility":"private"}`)
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusForbidden {
		t.Errorf("anonymous private create: got %d, want 403", resp3.StatusCode)
	}
}

func TestAnonymousWritesBlockedByDefault(t *testing.T) {
	ts, _ := newServerWithAuth(t, "key")

	// Without AllowAnonymousWrites, anonymous creates must be rejected
	resp := mustPost(t, ts.URL+"/api/documents", "application/json",
		`{"title":"anon","content":"hello","visibility":"public"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous write without flag: got %d, want 401", resp.StatusCode)
	}
}

func TestCustomHomeHandler(t *testing.T) {
	db := &testDB{store: newMemStore(), content: newMemContent()}
	customHome := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("custom home"))
	})
	handler := server.NewServer(db.store, db.content, server.Options{
		Logger:      log.New(io.Discard, "", 0),
		HomeHandler: customHome,
	})
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	resp := mustGet(t, ts.URL+"/")
	defer resp.Body.Close()
	body := readBody(t, resp)
	if !strings.Contains(body, "custom home") {
		t.Errorf("expected custom home handler response, got: %s", body)
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
