package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/pasteai/pasteai/internal/renderer"
	"github.com/pasteai/pasteai/internal/store"
	"github.com/pasteai/pasteai/web"
)

type Server struct {
	mux          *http.ServeMux
	store        store.Store
	homeTmpl     *template.Template
	documentTmpl *template.Template
	errorTmpl    *template.Template
	baseURL      string
	logger       *log.Logger
	chromaCSS    template.CSS
}

type Config struct {
	Addr         string
	BaseURL      string
	AuthProvider AuthProvider // optional; if set, Bearer token auth is enforced on writes
	Logger       *log.Logger
}

func NewServer(s store.Store, cfg Config) *http.Server {
	srv := &Server{
		mux:       http.NewServeMux(),
		store:     s,
		baseURL:   cfg.BaseURL,
		logger:    cfg.Logger,
		chromaCSS: renderer.ThemeCSS(),
	}
	srv.loadTemplates()
	srv.registerRoutes()

	var handler http.Handler = srv.mux
	if cfg.AuthProvider != nil {
		handler = authMiddleware(cfg.AuthProvider, srv.mux)
	}
	handler = securityHeaders(handler)

	return &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

func (s *Server) loadTemplates() {
	// Each page gets its own template set so {{define "body"}} doesn't conflict.
	s.homeTmpl = template.Must(template.New("").ParseFS(web.FS,
		"templates/base.html", "templates/home.html"))
	s.documentTmpl = template.Must(template.New("").ParseFS(web.FS,
		"templates/base.html", "templates/document.html"))
	s.errorTmpl = template.Must(template.New("").ParseFS(web.FS,
		"templates/base.html", "templates/error.html"))
}

func (s *Server) registerRoutes() {
	staticFS, err := fs.Sub(web.FS, "static")
	if err != nil {
		panic("web.FS missing static directory: " + err.Error())
	}
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))

	s.mux.HandleFunc("GET /{$}", s.handleHome)
	s.mux.HandleFunc("GET /d/{id}", s.handleViewDocument)

	s.mux.HandleFunc("POST /api/documents", s.handleCreateDocument)
	s.mux.HandleFunc("GET /api/documents", s.handleListDocuments)
	s.mux.HandleFunc("GET /api/documents/{id}", s.handleGetDocument)
	s.mux.HandleFunc("DELETE /api/documents/{id}", s.handleDeleteDocument)
}

// ── Web UI handlers ────────────────────────────────────────

type homeData struct {
	Documents []store.Document
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	result, err := s.store.List(r.Context(), store.ListOptions{
		OwnerID: ownerFromCtx(r.Context()),
		Limit:   50,
	})
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderWith(w, s.homeTmpl, homeData{Documents: result.Documents})
}

type documentData struct {
	Document     store.Document
	RenderedHTML template.HTML
	Headings     []renderer.Heading
	ChromaCSS    template.CSS
}

func (s *Server) handleViewDocument(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	doc, err := s.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.renderNotFound(w)
			return
		}
		s.serverError(w, err)
		return
	}

	result, err := renderer.Render(doc.Content)
	if err != nil {
		s.serverError(w, err)
		return
	}

	s.renderWith(w, s.documentTmpl, documentData{
		Document:     *doc,
		RenderedHTML: result.HTML,
		Headings:     result.Headings,
		ChromaCSS:    s.chromaCSS,
	})
}

// ── API handlers ───────────────────────────────────────────

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

func (s *Server) handleCreateDocument(w http.ResponseWriter, r *http.Request) {
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

	vis := store.Visibility(req.Visibility)
	switch vis {
	case "", store.VisibilityPublic:
		vis = store.VisibilityPublic
	case store.VisibilityUnlisted:
		// valid
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "visibility must be public or unlisted"})
		return
	}

	doc, err := s.store.Create(r.Context(), store.Document{
		Title:      req.Title,
		Content:    req.Content,
		Author:     req.Author,
		OwnerID:    ownerFromCtx(r.Context()),
		Visibility: vis,
	})
	if err != nil {
		s.serverError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, documentDetailResponse{
		documentResponse: s.toResponse(r, *doc),
		Content:          doc.Content,
	})
}

type listResponse struct {
	Documents []documentResponse `json:"documents"`
	NextToken string             `json:"next_token,omitempty"`
}

func (s *Server) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	result, err := s.store.List(r.Context(), store.ListOptions{
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

func (s *Server) handleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.Delete(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetDocument(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	doc, err := s.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		s.serverError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, documentDetailResponse{
		documentResponse: s.toResponse(r, *doc),
		Content:          doc.Content,
	})
}

// ── Helpers ────────────────────────────────────────────────

func (s *Server) toResponse(r *http.Request, d store.Document) documentResponse {
	return documentResponse{
		ID:         d.ID,
		Title:      d.Title,
		Author:     d.Author,
		Visibility: string(d.Visibility),
		CreatedAt:  d.CreatedAt.UTC().Format(time.RFC3339),
		URL:        s.docURL(r, d.ID),
	}
}

func (s *Server) renderWith(w http.ResponseWriter, tmpl *template.Template, data any) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "base.html", data); err != nil {
		s.logger.Printf("template error: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

func (s *Server) renderNotFound(w http.ResponseWriter) {
	var buf bytes.Buffer
	data := map[string]string{"Message": "Document not found or has been removed."}
	if err := s.errorTmpl.ExecuteTemplate(&buf, "base.html", data); err != nil {
		s.logger.Printf("template error: %v", err)
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	buf.WriteTo(w)
}

func (s *Server) serverError(w http.ResponseWriter, err error) {
	s.logger.Printf("internal error: %v", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
}

func (s *Server) docURL(r *http.Request, id string) string {
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
