package server

import (
	"html/template"
	"log"
	"net/http"
)

// Options configures the behaviour of a server created by NewServer.
type Options struct {
	BaseURL              string
	AuthProvider         AuthProvider // optional; nil means open writes
	AllowAnonymousWrites bool         // when true, unauthenticated users may write public/unlisted docs
	DefaultVisibility    Visibility   // default visibility for authenticated creates; empty means public
	HomeHandler          http.Handler // optional; replaces the default GET /{$} document-list handler
	Logger               *log.Logger  // optional; nil = log.Default()
	EventListener        EventListener // optional; called after successful document operations
	MCPHandler           http.Handler  // optional; when set, mounted at /mcp (streamable-HTTP MCP transport)
	NavExtrasFunc        func(*http.Request) template.HTML // optional; called per HTML page to populate NavExtras
	Footer               template.HTML                     // optional; injected into HTML pages that don't define a footer block
}
