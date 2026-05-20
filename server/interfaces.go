package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
)

var ErrNotFound = errors.New("document not found")
var ErrContentMissing = errors.New("document content file missing")

type Store interface {
	Create(ctx context.Context, doc Document) (*Document, error)
	List(ctx context.Context, opts ListOptions) (*ListResult, error)
	Get(ctx context.Context, id string) (*Document, error)
	// Update overwrites non-empty title only. Content is managed by ContentBackend.
	Update(ctx context.Context, id, title string) (*Document, error)
	UpdateVisibility(ctx context.Context, id string, vis Visibility) (*Document, error)
	Delete(ctx context.Context, id string) error
	Close() error
}

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

var errUnauthorized = &authError{"invalid API key"}

type authError struct{ msg string }

func (e *authError) Error() string { return e.msg }
