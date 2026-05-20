package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStaticKeyAuthValidKey(t *testing.T) {
	auth := NewStaticKeyAuth(map[string]string{"secret-key": "owner-alice"})
	handler := authMiddleware(auth, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner := ownerFromCtx(r.Context())
		if owner != "owner-alice" {
			t.Errorf("ownerID = %q, want owner-alice", owner)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer secret-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
}

func TestStaticKeyAuthInvalidKey(t *testing.T) {
	auth := NewStaticKeyAuth(map[string]string{"secret-key": "owner-alice"})
	handler := authMiddleware(auth, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for invalid key, got %d", rr.Code)
	}
}

func TestStaticKeyAuthNoHeader(t *testing.T) {
	auth := NewStaticKeyAuth(map[string]string{"secret-key": "owner-alice"})
	handler := authMiddleware(auth, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner := ownerFromCtx(r.Context())
		if owner != "" {
			t.Errorf("expected empty ownerID with no auth header, got %q", owner)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for unauthenticated GET, got %d", rr.Code)
	}
}

func TestOwnerFromCtxEmpty(t *testing.T) {
	owner := ownerFromCtx(context.Background())
	if owner != "" {
		t.Errorf("expected empty owner from bare context, got %q", owner)
	}
}

func TestAuthErrorMessage(t *testing.T) {
	err := errUnauthorized
	if err.Error() != "invalid API key" {
		t.Errorf("Error() = %q, want %q", err.Error(), "invalid API key")
	}
}
