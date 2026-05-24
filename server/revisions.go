package server

import (
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"github.com/pasteai/pasteai/internal/diff"
	"github.com/pasteai/pasteai/internal/renderer"
)

// revisionAccess returns the RevisionStore and checks owner access.
// Returns nil and writes a 403/404 response on failure.
func (s *srv) revisionAccess(w http.ResponseWriter, r *http.Request) RevisionStore {
	rs, ok := s.store.(RevisionStore)
	if !ok {
		http.NotFound(w, r)
		return nil
	}
	if s.authProvider != nil {
		ownerID := ownerFromCtx(r.Context())
		id := r.PathValue("id")
		doc, err := s.store.Get(r.Context(), id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				s.renderNotFound(w)
				return nil
			}
			s.serverError(w, err)
			return nil
		}
		if ownerID == "" || ownerID != doc.OwnerID {
			http.Error(w, "forbidden", http.StatusForbidden)
			return nil
		}
	}
	return rs
}

// ── HTML handlers ──────────────────────────────────────────

type revisionsData struct {
	baseData
	DocID     string
	DocTitle  string
	Revisions []Revision
}

// handleListRevisions serves the split-panel revision browser.
func (s *srv) handleListRevisions(w http.ResponseWriter, r *http.Request) {
	rs := s.revisionAccess(w, r)
	if rs == nil {
		return
	}
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
	revs, err := rs.ListRevisions(r.Context(), id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderWith(w, s.revisionsTmpl, revisionsData{
		DocID:     id,
		DocTitle:  doc.Title,
		Revisions: revs,
	})
}

type revisionViewData struct {
	baseData
	DocID     string
	DocTitle  string
	Revision  Revision
	HTML      renderer.Result
	ChromaCSS template.CSS
}

// handleViewRevision renders a specific revision's content.
func (s *srv) handleViewRevision(w http.ResponseWriter, r *http.Request) {
	rs := s.revisionAccess(w, r)
	if rs == nil {
		return
	}
	id := r.PathValue("id")
	num, err := strconv.Atoi(r.PathValue("num"))
	if err != nil {
		http.Error(w, "invalid revision number", http.StatusBadRequest)
		return
	}
	doc, err := s.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			s.renderNotFound(w)
			return
		}
		s.serverError(w, err)
		return
	}
	rev, err := rs.GetRevision(r.Context(), id, num)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			s.renderNotFound(w)
			return
		}
		s.serverError(w, err)
		return
	}
	var content string
	if rcb, ok := s.content.(RevisionContentBackend); ok {
		raw, err := rcb.GetRevision(r.Context(), id, num)
		if err == nil {
			content = string(raw)
		}
	}
	result, _ := renderer.Render(content)
	var renderResult renderer.Result
	if result != nil {
		renderResult = *result
	}
	s.renderWith(w, s.revisionTmpl, revisionViewData{
		DocID:     id,
		DocTitle:  doc.Title,
		Revision:  *rev,
		HTML:      renderResult,
		ChromaCSS: s.chromaCSS,
	})
}

type diffData struct {
	baseData
	DocID    string
	DocTitle string
	From     int
	ToLabel  string
	DiffText string
}

// handleDiffHTML renders a full-page unified diff.
func (s *srv) handleDiffHTML(w http.ResponseWriter, r *http.Request) {
	rs := s.revisionAccess(w, r)
	if rs == nil {
		return
	}
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
	fromNum, toNum, toLabel, ok := s.parseDiffParams(w, r, rs, id)
	if !ok {
		return
	}
	contentA, contentB, ok := s.fetchDiffContents(w, r, id, fromNum, toNum)
	if !ok {
		return
	}
	labelA := "revision " + strconv.Itoa(fromNum)
	unified := diff.Unified(labelA, toLabel, contentA, contentB)
	s.renderWith(w, s.diffTmpl, diffData{
		DocID:    id,
		DocTitle: doc.Title,
		From:     fromNum,
		ToLabel:  toLabel,
		DiffText: unified,
	})
}

// ── API handlers ───────────────────────────────────────────

type revisionAPIResponse struct {
	Num          int    `json:"num"`
	Title        string `json:"title"`
	Author       string `json:"author"`
	Visibility   string `json:"visibility"`
	SavedAt      string `json:"saved_at"`
	AddedLines   int    `json:"added_lines"`
	RemovedLines int    `json:"removed_lines"`
}

type revisionDetailAPIResponse struct {
	revisionAPIResponse
	Content string `json:"content"`
}

func toRevisionResponse(rev Revision) revisionAPIResponse {
	return revisionAPIResponse{
		Num:          rev.Num,
		Title:        rev.Title,
		Author:       rev.Author,
		Visibility:   string(rev.Visibility),
		SavedAt:      rev.SavedAt.UTC().Format(time.RFC3339),
		AddedLines:   rev.AddedLines,
		RemovedLines: rev.RemovedLines,
	}
}

// handleListRevisionsAPI returns revision metadata as JSON.
func (s *srv) handleListRevisionsAPI(w http.ResponseWriter, r *http.Request) {
	rs := s.revisionAccess(w, r)
	if rs == nil {
		return
	}
	id := r.PathValue("id")
	revs, err := rs.ListRevisions(r.Context(), id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	resp := make([]revisionAPIResponse, len(revs))
	for i, rev := range revs {
		resp[i] = toRevisionResponse(rev)
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": resp})
}

// handleGetRevisionAPI returns a single revision with content as JSON.
func (s *srv) handleGetRevisionAPI(w http.ResponseWriter, r *http.Request) {
	rs := s.revisionAccess(w, r)
	if rs == nil {
		return
	}
	id := r.PathValue("id")
	num, err := strconv.Atoi(r.PathValue("num"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid revision number"})
		return
	}
	rev, err := rs.GetRevision(r.Context(), id, num)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		s.serverError(w, err)
		return
	}
	var content string
	if rcb, ok := s.content.(RevisionContentBackend); ok {
		raw, err := rcb.GetRevision(r.Context(), id, num)
		if err == nil {
			content = string(raw)
		}
	}
	writeJSON(w, http.StatusOK, revisionDetailAPIResponse{
		revisionAPIResponse: toRevisionResponse(*rev),
		Content:             content,
	})
}

// handleDiffAPI returns a unified diff as JSON.
func (s *srv) handleDiffAPI(w http.ResponseWriter, r *http.Request) {
	rs := s.revisionAccess(w, r)
	if rs == nil {
		return
	}
	id := r.PathValue("id")
	fromNum, toNum, toLabel, ok := s.parseDiffParams(w, r, rs, id)
	if !ok {
		return
	}
	contentA, contentB, ok := s.fetchDiffContents(w, r, id, fromNum, toNum)
	if !ok {
		return
	}
	labelA := "revision " + strconv.Itoa(fromNum)
	unified := diff.Unified(labelA, toLabel, contentA, contentB)

	var to any = toLabel
	if toNum > 0 {
		to = toNum
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from": fromNum,
		"to":   to,
		"diff": unified,
	})
}

// ── Shared helpers ─────────────────────────────────────────

// parseDiffParams parses and validates ?from=N[&to=M] query parameters.
// Returns (fromNum, toNum, toLabel, ok). toNum==0 means "current".
func (s *srv) parseDiffParams(w http.ResponseWriter, r *http.Request, rs RevisionStore, docID string) (fromNum, toNum int, toLabel string, ok bool) {
	fromStr := r.URL.Query().Get("from")
	if fromStr == "" {
		http.Error(w, "from parameter is required", http.StatusBadRequest)
		return
	}
	fromNum, err := strconv.Atoi(fromStr)
	if err != nil || fromNum < 1 {
		http.Error(w, "invalid from parameter", http.StatusBadRequest)
		return 0, 0, "", false
	}
	if _, err := rs.GetRevision(r.Context(), docID, fromNum); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "revision not found"})
			return 0, 0, "", false
		}
		s.serverError(w, err)
		return 0, 0, "", false
	}

	toStr := r.URL.Query().Get("to")
	if toStr != "" {
		toNum, err = strconv.Atoi(toStr)
		if err != nil || toNum < 1 {
			http.Error(w, "invalid to parameter", http.StatusBadRequest)
			return 0, 0, "", false
		}
		if _, err := rs.GetRevision(r.Context(), docID, toNum); err != nil {
			if errors.Is(err, ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "to revision not found"})
				return 0, 0, "", false
			}
			s.serverError(w, err)
			return 0, 0, "", false
		}
		toLabel = "revision " + strconv.Itoa(toNum)
	} else {
		toLabel = "current"
	}
	return fromNum, toNum, toLabel, true
}

// fetchDiffContents retrieves content for both sides of a diff.
// toNum==0 means fetch current content.
func (s *srv) fetchDiffContents(w http.ResponseWriter, r *http.Request, docID string, fromNum, toNum int) (contentA, contentB string, ok bool) {
	rcb, hasRCB := s.content.(RevisionContentBackend)

	if hasRCB {
		raw, err := rcb.GetRevision(r.Context(), docID, fromNum)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "revision content not found"})
				return "", "", false
			}
			s.serverError(w, err)
			return "", "", false
		}
		contentA = string(raw)
	}

	if toNum > 0 && hasRCB {
		raw, err := rcb.GetRevision(r.Context(), docID, toNum)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "to revision content not found"})
				return "", "", false
			}
			s.serverError(w, err)
			return "", "", false
		}
		contentB = string(raw)
	} else if toNum == 0 {
		raw, err := s.content.Get(r.Context(), docID)
		if err != nil {
			s.serverError(w, err)
			return "", "", false
		}
		contentB = string(raw)
	}

	return contentA, contentB, true
}

