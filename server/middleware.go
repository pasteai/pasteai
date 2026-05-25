package server

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
)

type contextKey struct{}

var ownerKey contextKey

func ownerFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ownerKey).(string)
	return v
}

// authMiddleware enforces authentication using an AuthProvider.
// GET and HEAD requests always pass through (public read access).
// Write requests require a valid owner identity unless allowAnonymousWrites is true,
// in which case anonymous users may write (handlers enforce visibility restrictions).
func authMiddleware(provider AuthProvider, allowAnonymousWrites bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ownerID, err := provider.Authenticate(r)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("WWW-Authenticate", `Bearer realm="pasteai"`)
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		isWrite := r.Method != http.MethodGet && r.Method != http.MethodHead
		if ownerID == "" && isWrite && !allowAnonymousWrites {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("WWW-Authenticate", `Bearer realm="pasteai"`)
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		if ownerID != "" {
			r = r.WithContext(context.WithValue(r.Context(), ownerKey, ownerID)) // metadata: request-scoped owner identity
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
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"style-src 'self' 'unsafe-inline' https://api.fontshare.com; "+
				"font-src https://api.fontshare.com; "+
				"script-src 'self'; "+
				"img-src 'self' data:; "+
				"frame-ancestors 'none'")
		if r.Header.Get("X-Forwarded-Proto") == "https" {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

var gzipPool = sync.Pool{New: func() any { w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed); return w }}

type gzipResponseWriter struct {
	http.ResponseWriter
	w *gzip.Writer
}

func (g gzipResponseWriter) Write(b []byte) (int, error) { return g.w.Write(b) }

// gzipHandler compresses responses for clients that accept gzip encoding.
func gzipHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		gz := gzipPool.Get().(*gzip.Writer)
		gz.Reset(w)
		defer func() {
			gz.Close()
			gzipPool.Put(gz)
		}()
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		next.ServeHTTP(gzipResponseWriter{ResponseWriter: w, w: gz}, r)
	})
}
