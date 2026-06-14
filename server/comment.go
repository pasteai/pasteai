package server

import (
	"context"
	"time"
)

// Comment is a text-anchored annotation on a Document.
// StartChar and EndChar are byte offsets into the document content at creation
// time. QuotedText is a snapshot of the selected passage used to re-anchor
// the comment after the document is updated.
type Comment struct {
	ID         string    `json:"id"`
	DocID      string    `json:"doc_id"`
	Author     string    `json:"author"`
	OwnerID    string    `json:"owner_id,omitempty"`
	Body       string    `json:"body"`
	StartChar  int       `json:"start_char"`
	EndChar    int       `json:"end_char"`
	QuotedText string    `json:"quoted_text"`
	Resolved   bool      `json:"resolved"`
	CreatedAt  time.Time `json:"created_at"`
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
