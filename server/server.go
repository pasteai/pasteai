package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/pasteai/pasteai/internal/diff"
	"github.com/pasteai/pasteai/internal/renderer"
	"github.com/pasteai/pasteai/web"
)

type srv struct {
	mux               *http.ServeMux
	store             Store
	content           ContentBackend
	homeTmpl          *template.Template
	documentTmpl      *template.Template
	errorTmpl         *template.Template
	baseURL           string
	logger            *log.Logger
	chromaCSS         template.CSS
	authProvider      AuthProvider
	defaultVisibility Visibility
	eventListener     EventListener
	mcpHandler        http.Handler
}

// NewServer constructs the PasteAI HTTP handler. The caller is responsible for
// wrapping it in an *http.Server with appropriate timeouts and calling ListenAndServe.
func NewServer(store Store, content ContentBackend, opts Options) http.Handler {
	logger := opts.Logger
	if logger == nil {
		logger = log.Default()
	}

	s := &srv{
		mux:               http.NewServeMux(),
		store:             store,
		content:           content,
		baseURL:           opts.BaseURL,
		logger:            logger,
		chromaCSS:         renderer.ThemeCSS(),
		authProvider:      opts.AuthProvider,
		defaultVisibility: opts.DefaultVisibility,
		eventListener:     opts.EventListener,
		mcpHandler:        opts.MCPHandler,
	}
	s.loadTemplates()
	s.registerRoutes(opts.HomeHandler)

	var handler http.Handler = s.mux
	if opts.AuthProvider != nil {
		handler = authMiddleware(opts.AuthProvider, opts.AllowAnonymousWrites, s.mux)
	}
	handler = gzipHandler(handler)
	handler = securityHeaders(handler)
	return handler
}

func (s *srv) loadTemplates() {
	s.homeTmpl = template.Must(template.New("").ParseFS(web.FS,
		"templates/base.html", "templates/home.html"))
	s.documentTmpl = template.Must(template.New("").ParseFS(web.FS,
		"templates/base.html", "templates/document.html"))
	s.errorTmpl = template.Must(template.New("").ParseFS(web.FS,
		"templates/base.html", "templates/error.html"))
}

func (s *srv) registerRoutes(homeHandler http.Handler) {
	staticFS, err := fs.Sub(web.FS, "static")
	if err != nil {
		panic("web.FS missing static directory: " + err.Error()) // panic: build invariant — static dir is baked into the binary
	}
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", staticCacheHandler(http.FileServerFS(staticFS))))

	if homeHandler != nil {
		s.mux.Handle("GET /{$}", homeHandler)
	} else {
		s.mux.HandleFunc("GET /{$}", s.handleHome)
	}
	s.mux.HandleFunc("GET /d/{id}", s.handleViewDocument)
	s.mux.HandleFunc("GET /d/{id}/raw", s.handleViewRaw)

	s.mux.HandleFunc("POST /api/documents", s.handleCreateDocument)
	s.mux.HandleFunc("GET /api/documents", s.handleListDocuments)
	s.mux.HandleFunc("GET /api/documents/{id}", s.handleGetDocument)
	s.mux.HandleFunc("PUT /api/documents/{id}", s.handleUpdateDocument)
	s.mux.HandleFunc("DELETE /api/documents/{id}", s.handleDeleteDocument)
	s.mux.HandleFunc("GET /api/search", s.handleSearch)

	if s.mcpHandler != nil {
		s.mux.Handle("/mcp", s.mcpHandler)
	}
}

// ── Web UI handlers ────────────────────────────────────────

// baseData is embedded in all template data structs to provide the NavExtras slot.
// NavExtras is empty in the OSS server; the hosted tier injects login/logout HTML.
type baseData struct {
	NavExtras template.HTML
}

type homeData struct {
	baseData
	Documents []Document
	NextToken string
	Query     string
	IsSearch  bool
}

type errorData struct {
	baseData
	Message string
}

func (s *srv) handleHome(w http.ResponseWriter, r *http.Request) {
	if q := r.URL.Query().Get("q"); q != "" {
		docs, err := s.store.Search(r.Context(), SearchOptions{
			Query:   q,
			OwnerID: ownerFromCtx(r.Context()),
			Limit:   20,
		})
		if err != nil {
			s.serverError(w, err)
			return
		}
		s.renderWith(w, s.homeTmpl, homeData{
			Documents: docs,
			Query:     q,
			IsSearch:  true,
		})
		return
	}
	result, err := s.store.List(r.Context(), ListOptions{
		OwnerID:   ownerFromCtx(r.Context()),
		Limit:     20,
		NextToken: r.URL.Query().Get("token"),
	})
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderWith(w, s.homeTmpl, homeData{
		Documents: result.Documents,
		NextToken: result.NextToken,
	})
}

func (s *srv) handleViewRaw(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	doc, err := s.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.serverError(w, err)
		return
	}
	if doc.Visibility == VisibilityPrivate && ownerFromCtx(r.Context()) != doc.OwnerID {
		http.NotFound(w, r)
		return
	}
	raw, err := s.content.Get(r.Context(), id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	doc.Content = string(raw)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s.md"`, id))
	fmt.Fprint(w, doc.Content)
}

type documentData struct {
	baseData
	Document              Document
	RenderedHTML          template.HTML
	Headings              []renderer.Heading
	ChromaCSS             template.CSS
	Description           string
	PageURL               string
	OGImageURL            string
	RawURL                string
	ShowDelete            bool
	ShowVisibilityToggle  bool
}

func (s *srv) handleViewDocument(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	doc, err := s.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			s.renderNotFound(w)
			return
		}
		s.serverError(w, err)
		return
	}
	raw, err := s.content.Get(r.Context(), id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	doc.Content = string(raw)

	result, err := renderer.Render(doc.Content)
	if err != nil {
		s.serverError(w, err)
		return
	}

	ownerID := ownerFromCtx(r.Context())
	if doc.Visibility == VisibilityPrivate && ownerID != doc.OwnerID {
		s.renderNotFound(w)
		return
	}
	s.notify(r.Context(), DocumentViewed, ownerID, doc.ID)
	s.renderWith(w, s.documentTmpl, documentData{
		Document:             *doc,
		RenderedHTML:         result.HTML,
		Headings:             result.Headings,
		ChromaCSS:            s.chromaCSS,
		Description:          docDescription(doc.Content),
		PageURL:              s.baseURL + "/d/" + doc.ID,
		OGImageURL:           s.baseURL + "/static/og-image.svg",
		RawURL:               "/d/" + doc.ID + "/raw",
		ShowDelete:           s.canModify(ownerID, doc),
		ShowVisibilityToggle: ownerID != "" && doc.OwnerID != "" && ownerID == doc.OwnerID,
	})
}

func (s *srv) notify(ctx context.Context, typ DocumentEvent, ownerID, docID string) {
	if s.eventListener != nil {
		s.eventListener.OnDocumentEvent(ctx, typ, ownerID, docID)
	}
}

func (s *srv) canModify(ownerID string, doc *Document) bool {
	if s.authProvider == nil {
		return true
	}
	if doc.OwnerID == "" {
		return false
	}
	return ownerID == doc.OwnerID
}

func docDescription(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		const max = 160
		if len(line) > max {
			return line[:max] + "…"
		}
		return line
	}
	return ""
}

// ── API handlers ───────────────────────────────────────────

type updateRequest struct {
	Title      string `json:"title"`
	Content    string `json:"content"`
	Visibility string `json:"visibility"`
}

func (s *srv) handleUpdateDocument(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if req.Title == "" && req.Content == "" && req.Visibility == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title, content, or visibility required"})
		return
	}

	s.saveRevision(r.Context(), id, req.Content)

	doc, err := s.store.Update(r.Context(), id, req.Title)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		s.serverError(w, err)
		return
	}
	if req.Visibility != "" {
		vis := Visibility(req.Visibility)
		switch vis {
		case VisibilityPublic, VisibilityUnlisted, VisibilityPrivate:
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "visibility must be public, unlisted, or private"})
			return
		}
		doc, err = s.store.UpdateVisibility(r.Context(), id, vis)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
				return
			}
			s.serverError(w, err)
			return
		}
	}
	if req.Content != "" {
		if err := s.content.Put(r.Context(), id, []byte(req.Content)); err != nil {
			s.serverError(w, err)
			return
		}
		doc.Content = req.Content
	} else {
		raw, err := s.content.Get(r.Context(), id)
		if err != nil {
			s.serverError(w, err)
			return
		}
		doc.Content = string(raw)
	}
	s.notify(r.Context(), DocumentUpdated, ownerFromCtx(r.Context()), id)
	writeJSON(w, http.StatusOK, documentDetailResponse{
		documentResponse: s.toResponse(r, *doc),
		Content:          doc.Content,
	})
}

type createRequest struct {
	Title      string `json:"title"`
	Content    string `json:"content"`
	Author     string `json:"author"`
	Visibility string `json:"visibility"`
}

type documentResponse struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Author     string `json:"author"`
	Visibility string `json:"visibility"`
	CreatedAt  string `json:"created_at"`
	URL        string `json:"url"`
}

type documentDetailResponse struct {
	documentResponse
	Content string `json:"content"`
}

const (
	maxBodyBytes    = 1 << 20  // 1 MB total
	maxTitleBytes   = 500
	maxAuthorBytes  = 200
	maxContentBytes = 512 * 1024 // 512 KB
)

func (s *srv) handleCreateDocument(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large (max 1 MB)"})
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		}
		return
	}
	if req.Title == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title is required"})
		return
	}
	if req.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
		return
	}
	if len(req.Title) > maxTitleBytes {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title too long (max 500 bytes)"})
		return
	}
	if len(req.Author) > maxAuthorBytes {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "author too long (max 200 bytes)"})
		return
	}
	if len(req.Content) > maxContentBytes {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content too long (max 512 KB)"})
		return
	}

	vis := Visibility(req.Visibility)
	switch vis {
	case "":
		if ownerFromCtx(r.Context()) != "" && s.defaultVisibility != "" {
			vis = s.defaultVisibility
		} else {
			vis = VisibilityPublic
		}
	case VisibilityPublic:
		// explicit public — always allowed
	case VisibilityUnlisted:
		// valid
	case VisibilityPrivate:
		if ownerFromCtx(r.Context()) == "" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "private documents require authentication"})
			return
		}
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "visibility must be public, unlisted, or private"})
		return
	}

	doc, err := s.store.Create(r.Context(), Document{
		Title:      req.Title,
		Author:     req.Author,
		OwnerID:    ownerFromCtx(r.Context()),
		Visibility: vis,
	})
	if err != nil {
		s.serverError(w, err)
		return
	}
	if err := s.content.Put(r.Context(), doc.ID, []byte(req.Content)); err != nil {
		// best-effort rollback — content write failed after metadata was stored
		if rerr := s.store.Delete(r.Context(), doc.ID); rerr != nil {
			s.logger.Printf("rollback failed after content.Put error (id=%s): %v", doc.ID, rerr)
		}
		s.serverError(w, err)
		return
	}
	doc.Content = req.Content
	s.notify(r.Context(), DocumentCreated, doc.OwnerID, doc.ID)
	writeJSON(w, http.StatusCreated, documentDetailResponse{
		documentResponse: s.toResponse(r, *doc),
		Content:          doc.Content,
	})
}

type listResponse struct {
	Documents []documentResponse `json:"documents"`
	NextToken string             `json:"next_token,omitempty"`
}

func (s *srv) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "q parameter is required"})
		return
	}
	docs, err := s.store.Search(r.Context(), SearchOptions{
		Query:   q,
		OwnerID: ownerFromCtx(r.Context()),
		Limit:   20,
	})
	if err != nil {
		s.serverError(w, err)
		return
	}
	resp := make([]documentResponse, len(docs))
	for i, d := range docs {
		resp[i] = s.toResponse(r, d)
	}
	writeJSON(w, http.StatusOK, listResponse{Documents: resp})
}

func (s *srv) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	result, err := s.store.List(r.Context(), ListOptions{
		OwnerID:   ownerFromCtx(r.Context()),
		Limit:     50,
		NextToken: r.URL.Query().Get("next_token"),
	})
	if err != nil {
		s.serverError(w, err)
		return
	}

	docs := make([]documentResponse, len(result.Documents))
	for i, d := range result.Documents {
		docs[i] = s.toResponse(r, d)
	}
	writeJSON(w, http.StatusOK, listResponse{Documents: docs, NextToken: result.NextToken})
}

func (s *srv) handleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.Delete(r.Context(), id); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		s.serverError(w, err)
		return
	}
	// non-fatal: content file may already be gone
	s.content.Delete(r.Context(), id)
	if rs, ok := s.store.(RevisionStore); ok {
		if err := rs.DeleteRevisions(r.Context(), id); err != nil {
			s.logger.Printf("delete revisions: %v", err)
		}
	}
	if rcb, ok := s.content.(RevisionContentBackend); ok {
		if err := rcb.DeleteRevisions(r.Context(), id); err != nil {
			s.logger.Printf("delete revision content: %v", err)
		}
	}
	s.notify(r.Context(), DocumentDeleted, ownerFromCtx(r.Context()), id)
	w.WriteHeader(http.StatusNoContent)
}

// saveRevision captures a snapshot of the document's current state before an update.
// newContent is the incoming content (may be empty if only title/visibility changed).
// All errors are logged but do not abort the update — revision saving is best-effort.
func (s *srv) saveRevision(ctx context.Context, docID, newContent string) {
	rs, ok := s.store.(RevisionStore)
	if !ok {
		return
	}
	doc, err := s.store.Get(ctx, docID)
	if err != nil {
		s.logger.Printf("saveRevision: get doc: %v", err)
		return
	}
	var currentContent string
	raw, err := s.content.Get(ctx, docID)
	if err != nil {
		s.logger.Printf("saveRevision: get content: %v", err)
	} else {
		currentContent = string(raw)
	}

	added, removed := diff.CountLines(currentContent, newContent)
	saved, err := rs.SaveRevision(ctx, Revision{
		DocID:        docID,
		Title:        doc.Title,
		Author:       doc.Author,
		Visibility:   doc.Visibility,
		SavedAt:      time.Now().UTC(),
		AddedLines:   added,
		RemovedLines: removed,
	})
	if err != nil {
		s.logger.Printf("saveRevision: save metadata: %v", err)
		return
	}
	if rcb, ok := s.content.(RevisionContentBackend); ok {
		if err := rcb.PutRevision(ctx, docID, saved.Num, []byte(currentContent)); err != nil {
			s.logger.Printf("saveRevision: store content: %v", err)
		}
	}
}

func (s *srv) handleGetDocument(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	doc, err := s.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		s.serverError(w, err)
		return
	}
	if doc.Visibility == VisibilityPrivate && ownerFromCtx(r.Context()) != doc.OwnerID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	raw, err := s.content.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		s.serverError(w, err)
		return
	}
	doc.Content = string(raw)

	writeJSON(w, http.StatusOK, documentDetailResponse{
		documentResponse: s.toResponse(r, *doc),
		Content:          doc.Content,
	})
}

// ── Helpers ────────────────────────────────────────────────

func (s *srv) toResponse(r *http.Request, d Document) documentResponse {
	return documentResponse{
		ID:         d.ID,
		Title:      d.Title,
		Author:     d.Author,
		Visibility: string(d.Visibility),
		CreatedAt:  d.CreatedAt.UTC().Format(time.RFC3339),
		URL:        s.docURL(r, d.ID),
	}
}

func (s *srv) renderWith(w http.ResponseWriter, tmpl *template.Template, data any) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "base.html", data); err != nil {
		s.logger.Printf("template error: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

func (s *srv) renderNotFound(w http.ResponseWriter) {
	var buf bytes.Buffer
	data := errorData{Message: "Document not found or has been removed."}
	if err := s.errorTmpl.ExecuteTemplate(&buf, "base.html", data); err != nil {
		s.logger.Printf("template error: %v", err)
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	buf.WriteTo(w)
}

func (s *srv) serverError(w http.ResponseWriter, err error) {
	s.logger.Printf("internal error: %v", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
}

func (s *srv) docURL(r *http.Request, id string) string {
	if s.baseURL != "" {
		return s.baseURL + "/d/" + id
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	// Sanitise r.Host: keep only "hostname" or "hostname:port" to prevent
	// newline injection and path-component injection in the returned URL.
	host := strings.SplitN(r.Host, "/", 2)[0]
	host = strings.TrimSpace(host)
	return fmt.Sprintf("%s://%s/d/%s", scheme, host, id)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// staticCacheHandler wraps a handler serving embedded static files with a
// one-year Cache-Control header. Files are content-addressed by the embed
// hash so stale caches are never an issue.
func staticCacheHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		next.ServeHTTP(w, r)
	})
}
