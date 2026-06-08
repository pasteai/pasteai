package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestSecurityHeadersHSTS_SetOverHTTPS(t *testing.T) {
	handler := securityHeaders("", "", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	hsts := rr.Header().Get("Strict-Transport-Security")
	if hsts == "" {
		t.Fatal("Strict-Transport-Security not set for HTTPS request")
	}
	if !strings.Contains(hsts, "max-age=") {
		t.Errorf("Strict-Transport-Security missing max-age: %s", hsts)
	}
}

func TestSecurityHeadersHSTS_NotSetOverHTTP(t *testing.T) {
	handler := securityHeaders("", "", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if hsts := rr.Header().Get("Strict-Transport-Security"); hsts != "" {
		t.Errorf("Strict-Transport-Security should not be set for plain HTTP, got: %s", hsts)
	}
}

func TestSecurityHeadersCSPScriptSrcUnsafeInline(t *testing.T) {
	// Inline scripts in Go templates use template variables (e.g. document ID),
	// so 'unsafe-inline' is required in script-src.
	handler := securityHeaders("", "", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	csp := rr.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("Content-Security-Policy header not set")
	}
	var hasUnsafeInline bool
	for _, directive := range strings.Split(csp, ";") {
		directive = strings.TrimSpace(directive)
		if strings.HasPrefix(directive, "script-src") && strings.Contains(directive, "'unsafe-inline'") {
			hasUnsafeInline = true
		}
	}
	if !hasUnsafeInline {
		t.Errorf("script-src should contain 'unsafe-inline' to allow template inline scripts: %s", csp)
	}
}
