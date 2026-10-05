package server

import (
	"context"
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
	ID          string `json:"id"`
	DocID       string `json:"doc_id"`
	Author      string `json:"author"`
	Body        string `json:"body"`
	StartChar   int    `json:"start_char"`
	EndChar     int    `json:"end_char"`
	QuotedText  string `json:"quoted_text"`
	ParentID    string `json:"parent_id,omitempty"`
	RevisionNum int    `json:"revision_num"`
	IsMine      bool   `json:"is_mine"`
	Resolved    bool   `json:"resolved"`
	CreatedAt   string `json:"created_at"`
}

// toCommentResponseForUser builds a commentResponse from c, populating
// is_mine according to whether the requester (identified by ownerID)
// owns the comment. When authEnabled is false (OSS/self-hosted mode),
// is_mine is always true so the local user has full control.
func toCommentResponseForUser(c Comment, ownerID string, authEnabled bool) commentResponse {
	isMine := true
	if authEnabled {
		isMine = ownerID != "" && ownerID == c.OwnerID
	}
	return commentResponse{
		ID:          c.ID,
		DocID:       c.DocID,
		Author:      c.Author,
		Body:        c.Body,
		StartChar:   c.StartChar,
		EndChar:     c.EndChar,
		QuotedText:  c.QuotedText,
		ParentID:    c.ParentID,
		RevisionNum: c.RevisionNum,
		IsMine:      isMine,
		Resolved:    c.Resolved,
		CreatedAt:   c.CreatedAt.UTC().Format(time.RFC3339),
	}
}

type createCommentRequest struct {
	Author     string `json:"author"`
	Body       string `json:"body"`
	StartChar  int    `json:"start_char"`
	EndChar    int    `json:"end_char"`
	QuotedText string `json:"quoted_text"`
	ParentID   string `json:"parent_id"`
}

type patchCommentRequest struct {
	Resolved *bool  `json:"resolved"`
	Body     string `json:"body"`
}

// userNamer is a consumer-site interface for AuthProviders that can resolve a
// display name for an authenticated user. The hosted service implements it from
// the session's GitHub login; the self-hosted server has no concept of a user
// name and does not.
type userNamer interface {
	DisplayName(ctx context.Context, ownerID string) string
}

// commentAuthor decides the name a comment is attributed to. When the server
// can identify the user it wins, so the browser need not ask for a name and a
// caller cannot post under someone else's. Otherwise the client's value is
// used, which keeps the API usable for self-hosted and script callers.
func (s *srv) commentAuthor(ctx context.Context, ownerID, requested string) string {
	if ownerID == "" {
		return requested
	}
	namer, ok := s.authProvider.(userNamer)
	if !ok {
		return requested
	}
	if name := namer.DisplayName(ctx, ownerID); name != "" {
		return name
	}
	return requested
}

// currentRevisionNum reports how many revisions the document has, which is the
// version a comment written now is anchored against. Zero when the store keeps
// no history, which is the self-hosted default.
func (s *srv) currentRevisionNum(ctx context.Context, docID string) int {
	rs, ok := s.store.(RevisionStore)
	if !ok {
		return 0
	}
	revs, err := rs.ListRevisions(ctx, docID)
	if err != nil {
		s.logger.Printf("comment revision number: %v", err)
		return 0
	}
	return len(revs)
}

// commentBodyUpdater is a consumer-site interface for stores that support
// updating the body of a comment. The server type-asserts CommentStore
// implementations to this interface to enable comment editing.
type commentBodyUpdater interface {
	UpdateCommentBody(ctx context.Context, docID, commentID, body string) (*Comment, error)
}

func (s *srv) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cs := s.store.(CommentStore)

	var req createCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if err := validateCreateCommentRequest(req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
		DocID:       id,
		Author:      s.commentAuthor(r.Context(), ownerID, req.Author),
		OwnerID:     ownerID,
		Body:        req.Body,
		StartChar:   req.StartChar,
		EndChar:     req.EndChar,
		QuotedText:  req.QuotedText,
		ParentID:    req.ParentID,
		RevisionNum: s.currentRevisionNum(r.Context(), id),
	})
	if err != nil {
		s.serverError(w, err)
		return
	}
	authEnabled := s.authProvider != nil
	writeJSON(w, http.StatusCreated, toCommentResponseForUser(*c, ownerID, authEnabled))
}

// validateCreateCommentRequest returns a user-facing error for invalid input.
func validateCreateCommentRequest(req createCommentRequest) error {
	if req.Body == "" {
		return errors.New("body is required")
	}
	if req.QuotedText == "" {
		return errors.New("quoted_text is required")
	}
	if req.StartChar >= req.EndChar {
		return errors.New("start_char must be less than end_char")
	}
	return nil
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
	ownerID := ownerFromCtx(r.Context())
	authEnabled := s.authProvider != nil
	resp := make([]commentResponse, len(comments))
	for i, c := range comments {
		resp[i] = toCommentResponseForUser(c, ownerID, authEnabled)
	}
	writeJSON(w, http.StatusOK, resp)
}

// handlePatchComment updates a comment's body and/or resolved status.
// Either or both fields may be provided; at least one is required.
func (s *srv) handlePatchComment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cid := r.PathValue("cid")
	cs := s.store.(CommentStore)

	var req patchCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if req.Body == "" && req.Resolved == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body or resolved is required"})
		return
	}

	doc, c, ok := s.loadCommentForPatch(w, r, id, cid)
	if !ok {
		return
	}
	if !s.canModifyComment(ownerFromCtx(r.Context()), c, doc) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	final := c
	if req.Body != "" {
		updated, err := s.updateCommentBody(r.Context(), cs, id, cid, req.Body)
		if err != nil {
			s.writeCommentUpdateError(w, err)
			return
		}
		final = updated
	}
	if req.Resolved != nil {
		updated, err := cs.ResolveComment(r.Context(), id, cid, *req.Resolved)
		if err != nil {
			s.writeCommentUpdateError(w, err)
			return
		}
		final = updated
	}
	ownerID := ownerFromCtx(r.Context())
	writeJSON(w, http.StatusOK, toCommentResponseForUser(*final, ownerID, s.authProvider != nil))
}

// loadCommentForPatch fetches the document and comment, writing errors to w.
// Returns ok=false when the caller should stop processing.
func (s *srv) loadCommentForPatch(w http.ResponseWriter, r *http.Request, id, cid string) (*Document, *Comment, bool) {
	cs := s.store.(CommentStore)
	doc, err := s.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return nil, nil, false
		}
		s.serverError(w, err)
		return nil, nil, false
	}
	c, err := cs.GetComment(r.Context(), id, cid)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return nil, nil, false
		}
		s.serverError(w, err)
		return nil, nil, false
	}
	return doc, c, true
}

// updateCommentBody type-asserts the CommentStore to commentBodyUpdater and
// calls UpdateCommentBody. Returns ErrNotFound if body updates are unsupported.
func (s *srv) updateCommentBody(ctx context.Context, cs CommentStore, docID, commentID, body string) (*Comment, error) {
	updater, ok := cs.(commentBodyUpdater)
	if !ok {
		return nil, ErrNotFound
	}
	return updater.UpdateCommentBody(ctx, docID, commentID, body)
}

// writeCommentUpdateError writes the correct HTTP response for a comment update error.
func (s *srv) writeCommentUpdateError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	s.serverError(w, err)
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
