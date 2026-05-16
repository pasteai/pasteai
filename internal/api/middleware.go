package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

type contextKey struct{}

var ownerKey contextKey

func ownerFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ownerKey).(string)
	return v
}

// AuthProvider resolves a request to an owner identity.
// Implementations: StaticKeyAuth (self-hosted), DynamoKeyAuth (hosted).
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

// authMiddleware enforces authentication using an AuthProvider.
// GET and HEAD requests always pass through (public read access).
// Write requests require a valid owner identity from the provider.
func authMiddleware(provider AuthProvider, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ownerID, err := provider.Authenticate(r)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("WWW-Authenticate", `Bearer realm="pasteai"`)
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		if ownerID == "" && r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("WWW-Authenticate", `Bearer realm="pasteai"`)
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		if ownerID != "" {
			r = r.WithContext(context.WithValue(r.Context(), ownerKey, ownerID))
		}
		next.ServeHTTP(w, r)
	})
}

// securityHeaders adds security-related HTTP headers to every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// unsafe-inline for script-src is required for the inline anti-FOUC and theme scripts.
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"style-src 'self' 'unsafe-inline' https://api.fontshare.com; "+
				"font-src https://api.fontshare.com; "+
				"script-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; "+
				"frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}
