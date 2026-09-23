package infrastructure

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

const previewSessionQuery = "__liapoldus_preview_session"

type previewProxy struct {
	host      string
	sessionID string
	upstream  *httputil.ReverseProxy
}

func newPreviewProxy(host, sessionID string, target *url.URL) *previewProxy {
	reverse := httputil.NewSingleHostReverseProxy(target)
	direct := reverse.Director
	reverse.Director = func(request *http.Request) {
		direct(request)
		// Vite's own CORS policy must not decide access for the opaque sandbox
		// origin. This proxy grants it only after validating the preview session.
		request.Header.Del("Origin")
	}
	return &previewProxy{host: host, sessionID: sessionID, upstream: reverse}
}

func (p *previewProxy) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request.Host != p.host {
		http.Error(w, "preview host is invalid", http.StatusMisdirectedRequest)
		return
	}
	if !p.authorizedRequest(request) {
		http.Error(w, "preview session is invalid", http.StatusForbidden)
		return
	}
	w.Header().Set("Referrer-Policy", "no-referrer")

	if request.Header.Get("Origin") == "null" {
		w.Header().Set("Access-Control-Allow-Origin", "null")
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.Header().Add("Vary", "Origin")
	}
	if request.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	p.upstream.ServeHTTP(w, request)
}

func (p *previewProxy) authorizedRequest(request *http.Request) bool {
	if request.Method != http.MethodGet && request.Method != http.MethodHead && request.Method != http.MethodOptions {
		return false
	}
	origin := request.Header.Get("Origin")
	if origin != "" && origin != "null" {
		return false
	}
	websocketUpgrade := strings.EqualFold(request.Header.Get("Upgrade"), "websocket")
	if request.URL.Path == "/" && request.URL.Query().Get(previewSessionQuery) != p.sessionID && !websocketUpgrade {
		return false
	}
	return true
}
