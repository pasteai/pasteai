package server

import (
	"log"
	"net/http"
)

type Options struct {
	BaseURL              string
	AuthProvider         AuthProvider // optional; nil means open writes
	AllowAnonymousWrites bool         // when true, unauthenticated users may write public/unlisted docs
	HomeHandler          http.Handler // optional; replaces the default GET /{$} document-list handler
	Logger               *log.Logger // optional; nil = log.Default()
}
