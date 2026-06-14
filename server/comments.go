package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// hasComments reports whether the configured store supports document comments.
func (s *srv) hasComments() bool {
	_, ok := s.store.(CommentStore)
	return ok
}

// canModifyComment reports whether requesterOwnerID may resolve or delete c.
// In OSS mode (no AuthProvider) all requests are permitted.
// In cloud mode the requester must own the comment or the document.
func (s *srv) canModifyComment(requesterOwnerID string, c *Comment, doc *Document) bool {
	if s.authProvider == nil {
		return true
	}
	if requesterOwnerID == "" {
		return false
	}
	return requesterOwnerID == c.OwnerID || requesterOwnerID == doc.OwnerID
}

// commentResponse is the API-visible shape of a Comment. OwnerID is intentionally excluded.
type commentResponse struct {
	ID         string `json:"id"`
	DocID      string `json:"doc_id"`
	Author     string `json:"author"`
	Body       string `json:"body"`
	StartChar  int    `json:"start_char"`
	EndChar    int    `json:"end_char"`
	QuotedText string `json:"quoted_text"`
	Resolved   bool   `json:"resolved"`
	CreatedAt  string `json:"created_at"`
}

func toCommentResponse(c Comment) commentResponse {
	return commentResponse{
		ID:         c.ID,
		DocID:      c.DocID,
		Author:     c.Author,
		Body:       c.Body,
		StartChar:  c.StartChar,
		EndChar:    c.EndChar,
		QuotedText: c.QuotedText,
		Resolved:   c.Resolved,
		CreatedAt:  c.CreatedAt.UTC().Format(time.RFC3339),
	}
}

type createCommentRequest struct {
	Author     string `json:"author"`
	Body       string `json:"body"`
	StartChar  int    `json:"start_char"`
	EndChar    int    `json:"end_char"`
	QuotedText string `json:"quoted_text"`
}

type resolveCommentRequest struct {
	Resolved bool `json:"resolved"`
}

func (s *srv) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cs := s.store.(CommentStore)

	var req createCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if req.Body == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body is required"})
		return
	}
	if req.QuotedText == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "quoted_text is required"})
		return
	}
	if req.StartChar >= req.EndChar {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "start_char must be less than end_char"})
		return
	}

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
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	ownerID := ownerFromCtx(r.Context())
	c, err := cs.AddComment(r.Context(), Comment{
		DocID:      id,
		Author:     req.Author,
		OwnerID:    ownerID,
		Body:       req.Body,
		StartChar:  req.StartChar,
		EndChar:    req.EndChar,
		QuotedText: req.QuotedText,
	})
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toCommentResponse(*c))
}

func (s *srv) handleListComments(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cs := s.store.(CommentStore)

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
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	comments, err := cs.ListComments(r.Context(), id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	resp := make([]commentResponse, len(comments))
	for i, c := range comments {
		resp[i] = toCommentResponse(c)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *srv) handleResolveComment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cid := r.PathValue("cid")
	cs := s.store.(CommentStore)

	var req resolveCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	doc, err := s.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		s.serverError(w, err)
		return
	}

	c, err := cs.GetComment(r.Context(), id, cid)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		s.serverError(w, err)
		return
	}

	if !s.canModifyComment(ownerFromCtx(r.Context()), c, doc) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	updated, err := cs.ResolveComment(r.Context(), id, cid, req.Resolved)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toCommentResponse(*updated))
}

func (s *srv) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cid := r.PathValue("cid")
	cs := s.store.(CommentStore)

	doc, err := s.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		s.serverError(w, err)
		return
	}

	c, err := cs.GetComment(r.Context(), id, cid)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		s.serverError(w, err)
		return
	}

	if !s.canModifyComment(ownerFromCtx(r.Context()), c, doc) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	if err := cs.DeleteComment(r.Context(), id, cid); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
