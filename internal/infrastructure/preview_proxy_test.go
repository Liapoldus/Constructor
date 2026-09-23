package infrastructure

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestPreviewProxyRestrictsOpaqueOriginCORSToSession(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "" {
			t.Errorf("upstream should not receive the opaque browser origin")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("preview"))
	}))
	defer targetServer.Close()
	target, err := url.Parse(targetServer.URL)
	if err != nil {
		t.Fatal(err)
	}

	const session = "0123456789abcdef0123456789abcdef"
	const host = session + ".localhost:43210"
	proxy := newPreviewProxy(host, session, target)

	page := httptest.NewRequest(http.MethodGet, "http://"+host+"/?"+previewSessionQuery+"="+session, nil)
	page.Host = host
	pageResponse := httptest.NewRecorder()
	proxy.ServeHTTP(pageResponse, page)
	if pageResponse.Code != http.StatusOK || pageResponse.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("valid session page was not served with a no-referrer policy: status=%d headers=%v", pageResponse.Code, pageResponse.Header())
	}

	asset := httptest.NewRequest(http.MethodGet, "http://"+host+"/src/app.tsx", nil)
	asset.Host = host
	asset.Header.Set("Origin", "null")
	assetResponse := httptest.NewRecorder()
	proxy.ServeHTTP(assetResponse, asset)
	if assetResponse.Code != http.StatusOK || assetResponse.Header().Get("Access-Control-Allow-Origin") != "null" {
		t.Fatalf("active sandbox session could not load a module: status=%d headers=%v", assetResponse.Code, assetResponse.Header())
	}
	websocket := httptest.NewRequest(http.MethodGet, "http://"+host+"/?token=vite-hmr", nil)
	websocket.Host = host
	websocket.Header.Set("Origin", "null")
	websocket.Header.Set("Upgrade", "websocket")
	if !proxy.authorizedRequest(websocket) {
		t.Fatal("active preview host must authorize its HMR websocket upgrade")
	}

	unauthorized := httptest.NewRequest(http.MethodGet, "http://"+host+"/src/app.tsx", nil)
	unauthorized.Host = host
	unauthorized.Header.Set("Origin", "null")
	unauthorizedResponse := httptest.NewRecorder()
	newPreviewProxy("other-session.localhost:43210", session, target).ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusMisdirectedRequest {
		t.Fatalf("opaque origin without this session host was allowed: %d", unauthorizedResponse.Code)
	}

	external := httptest.NewRequest(http.MethodGet, "http://"+host+"/src/app.tsx", nil)
	external.Host = host
	external.Header.Set("Origin", "https://attacker.example")
	externalResponse := httptest.NewRecorder()
	proxy.ServeHTTP(externalResponse, external)
	if externalResponse.Code != http.StatusForbidden {
		t.Fatalf("non-opaque origin was allowed to read preview assets: %d", externalResponse.Code)
	}
}
