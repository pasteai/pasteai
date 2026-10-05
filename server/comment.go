package server

import (
	"context"
	"time"
)

// Comment is a text-anchored annotation on a Document.
//
// StartChar and EndChar are offsets into the concatenated text of the RENDERED
// document as the browser sees it — not byte offsets into the markdown source.
// They are a disambiguation hint for repeated text, not the primary anchor:
// QuotedText is, and the client falls back to searching for it when the offsets
// no longer line up. A caller computing offsets from markdown source will not
// match, and degrades to that search.
//
// RevisionNum records how many revisions existed when the comment was written,
// so a comment stranded by a later rewrite can still say which version of the
// document it was made against.
type Comment struct {
	ID          string    `json:"id"`
	DocID       string    `json:"doc_id"`
	Author      string    `json:"author"`
	OwnerID     string    `json:"owner_id,omitempty"`
	Body        string    `json:"body"`
	StartChar   int       `json:"start_char"`
	EndChar     int       `json:"end_char"`
	QuotedText  string    `json:"quoted_text"`
	ParentID    string    `json:"parent_id,omitempty"`
	RevisionNum int       `json:"revision_num,omitempty"`
	Resolved    bool      `json:"resolved"`
	CreatedAt   time.Time `json:"created_at"`
}

// CommentStore is optionally implemented by Store backends that support
// document comments. The server detects support via type assertion.
type CommentStore interface {
	Store
	// AddComment creates a new comment. Implementations assign ID and CreatedAt.
	AddComment(ctx context.Context, c Comment) (*Comment, error)
	// ListComments returns all comments for docID ordered by CreatedAt ascending.
	ListComments(ctx context.Context, docID string) ([]Comment, error)
	// GetComment returns the comment with the given ID, or ErrNotFound.
	GetComment(ctx context.Context, docID, commentID string) (*Comment, error)
	// ResolveComment sets the Resolved field and returns the updated comment.
	ResolveComment(ctx context.Context, docID, commentID string, resolved bool) (*Comment, error)
	// DeleteComment permanently removes a comment. Returns ErrNotFound if missing.
	DeleteComment(ctx context.Context, docID, commentID string) error
}
