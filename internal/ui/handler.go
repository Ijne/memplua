package ui

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
)

//go:embed assets/*
var embedded embed.FS

// Options supplies non-secret metadata displayed by the contributor panel.
type Options struct {
	Version   string
	TokenFile string
}

type handler struct {
	options Options
	files   http.Handler
}

// New returns the embedded memplua control panel. The static UI is
// public on the loopback listener, while every data request still requires the
// local API bearer token.
func New(options Options) http.Handler {
	assets, err := fs.Sub(embedded, "assets")
	if err != nil {
		panic(err)
	}
	return &handler{
		options: options,
		files:   http.StripPrefix("/ui/", http.FileServer(http.FS(assets))),
	}
}

// ServeHTTP serves stable embedded assets, redirects roots, and exposes a
// no-store bootstrap document that contains no bearer token.
func (h *handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	setSecurityHeaders(response)
	switch request.URL.Path {
	case "/":
		http.Redirect(response, request, "/ui/", http.StatusTemporaryRedirect)
	case "/ui":
		http.Redirect(response, request, "/ui/", http.StatusTemporaryRedirect)
	case "/ui/bootstrap.json":
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(response).Encode(map[string]string{
			"version":    h.options.Version,
			"token_file": h.options.TokenFile,
		})
	default:
		// UI assets are embedded into the executable but keep stable URLs. Do not
		// let an already open browser combine a new API with an older JS bundle.
		response.Header().Set("Cache-Control", "no-store")
		h.files.ServeHTTP(response, request)
	}
}

func setSecurityHeaders(response http.ResponseWriter) {
	response.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	response.Header().Set("Referrer-Policy", "no-referrer")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("X-Frame-Options", "DENY")
}
