package server

import "log"

type Options struct {
	BaseURL      string
	AuthProvider AuthProvider // optional; nil means open writes
	Logger       *log.Logger  // optional; nil = log.Default()
}
