package presentation

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBrowserOriginBoundaryProtectsReadAndWriteRequests(t *testing.T) {
	called := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusNoContent)
	})
	handler := requestIDMiddleware(browserOriginBoundary(next))
	for _, test := range []struct {
		name   string
		method string
		origin string
		want   int
	}{
		{name: "same-site read", method: http.MethodGet, origin: "https://attacker.example", want: http.StatusForbidden},
		{name: "cross-site write", method: http.MethodPost, origin: "http://attacker.localhost", want: http.StatusForbidden},
		{name: "opaque origin", method: http.MethodGet, origin: "null", want: http.StatusForbidden},
		{name: "loopback browser", method: http.MethodGet, origin: "http://localhost:5173", want: http.StatusNoContent},
		{name: "native client", method: http.MethodPost, want: http.StatusNoContent},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "/api/v1/health", nil)
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.want, response.Body.String())
			}
			if response.Header().Get("X-Request-ID") == "" {
				t.Fatal("response has no request ID")
			}
		})
	}
	if called != 2 {
		t.Fatalf("next handler called %d times; want only the loopback and native requests", called)
	}
}

func TestLoopbackHostBoundaryRejectsRebindingHost(t *testing.T) {
	called := false
	handler := LoopbackHostBoundary(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, test := range []struct {
		host string
		want int
	}{
		{host: "attacker.example", want: http.StatusForbidden},
		{host: "constructor.localhost:8787", want: http.StatusForbidden},
		{host: "localhost:8787", want: http.StatusNoContent},
		{host: "127.0.0.1:8787", want: http.StatusNoContent},
		{host: "[::1]:8787", want: http.StatusNoContent},
	} {
		called = false
		request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		request.Host = test.host
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.want || called != (test.want == http.StatusNoContent) {
			t.Errorf("host=%q status=%d called=%v wantStatus=%d", test.host, response.Code, called, test.want)
		}
	}
}

func TestLoopbackHostBoundaryRejectsInvalidPortsAndHosts(t *testing.T) {
	for _, host := range []string{"", "0.0.0.0:8787", "127.0.0.1:0", "127.0.0.1:abc", "localhost:65536", "[fe80::1%en0]:8787"} {
		if isLoopbackRequestHost(host) {
			t.Errorf("host %q should not be considered loopback", host)
		}
	}
}
