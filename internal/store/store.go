package store

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("document not found")
var ErrContentMissing = errors.New("document content file missing")

type Visibility string

const (
	VisibilityPublic   Visibility = "public"
	VisibilityUnlisted Visibility = "unlisted"
	VisibilityPrivate  Visibility = "private" // reserved for hosted auth tier; not accepted by the v1 API
)

type Document struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Content    string     `json:"content"`
	Author     string     `json:"author"`
	OwnerID    string     `json:"owner_id"`
	Visibility Visibility `json:"visibility"`
	Slug       string     `json:"slug,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

// ListOptions controls what List returns.
// If OwnerID is non-empty, returns all docs for that owner (any visibility).
// If OwnerID is empty, returns public docs only.
// NextToken is an opaque cursor for the next page; empty means start from the beginning.
type ListOptions struct {
	OwnerID   string
	Limit     int
	NextToken string
}

// ListResult holds a page of documents and an optional cursor for the next page.
type ListResult struct {
	Documents []Document
	NextToken string // empty when there are no more pages
}

type Store interface {
	Create(ctx context.Context, doc Document) (*Document, error)
	List(ctx context.Context, opts ListOptions) (*ListResult, error)
	Get(ctx context.Context, id string) (*Document, error)
	Delete(ctx context.Context, id string) error
	Close() error
}
