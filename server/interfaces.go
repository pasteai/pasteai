package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
)

// ErrNotFound is returned when a requested document does not exist.
var ErrNotFound = errors.New("document not found")

// DocumentEvent identifies which document lifecycle operation just succeeded.
type DocumentEvent string

const (
	DocumentCreated DocumentEvent = "created"
	DocumentViewed  DocumentEvent = "viewed"
	DocumentUpdated DocumentEvent = "updated"
	DocumentDeleted DocumentEvent = "deleted"
)

// EventListener receives notifications after successful document operations.
// All methods are called after the operation succeeds; failures are not reported.
// Implementations must be safe for concurrent use.
type EventListener interface {
	OnDocumentEvent(ctx context.Context, typ DocumentEvent, ownerID, docID string)
}

// Store persists documents and supports listing, searching, and deletion.
type Store interface {
	Create(ctx context.Context, doc Document) (*Document, error)
	List(ctx context.Context, opts ListOptions) (*ListResult, error)
	Search(ctx context.Context, opts SearchOptions) ([]Document, error)
	Get(ctx context.Context, id string) (*Document, error)
	// Update overwrites non-empty title only. Content is managed by ContentBackend.
	Update(ctx context.Context, id, title string) (*Document, error)
	UpdateVisibility(ctx context.Context, id string, vis Visibility) (*Document, error)
	Delete(ctx context.Context, id string) error
	Close() error
}

// ContentBackend stores and retrieves raw document content by document ID.
type ContentBackend interface {
	Put(ctx context.Context, id string, content []byte) error
	Get(ctx context.Context, id string) ([]byte, error)
	Delete(ctx context.Context, id string) error
}

// AuthProvider resolves a request to an owner identity.
// Implementations: StaticKeyAuth (self-hosted), custom (hosted).
type AuthProvider interface {
	// Authenticate returns the ownerID for the request, or an error if
	// credentials were present but invalid. Empty ownerID means anonymous.
	Authenticate(r *http.Request) (ownerID string, err error)
}

// StaticKeyAuth is the self-hosted AuthProvider: a fixed map of key → ownerID.
// Timing-safe comparison prevents key extraction via response timing.
type StaticKeyAuth struct {
	entries []struct{ key, ownerID []byte }
}

// NewStaticKeyAuth creates a StaticKeyAuth from a map of API key → ownerID.
func NewStaticKeyAuth(keys map[string]string) *StaticKeyAuth {
	a := &StaticKeyAuth{}
	for k, v := range keys {
		a.entries = append(a.entries, struct{ key, ownerID []byte }{[]byte(k), []byte(v)})
	}
	return a
}

func (a *StaticKeyAuth) Authenticate(r *http.Request) (string, error) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return "", nil
	}
	token := []byte(strings.TrimPrefix(auth, "Bearer "))
	for _, e := range a.entries {
		if subtle.ConstantTimeCompare(token, e.key) == 1 {
			return string(e.ownerID), nil
		}
	}
	return "", errUnauthorized
}

var errUnauthorized = errors.New("invalid API key")
