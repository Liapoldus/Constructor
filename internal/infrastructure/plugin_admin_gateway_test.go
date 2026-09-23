package infrastructure

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPPluginAdminGatewayFetchesSurfaceWithoutExposingCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/plugins/forms-db/admin/surface" {
			t.Errorf("unexpected Gateway request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer gateway-secret" {
			t.Error("configured Gateway credential was not applied server-side")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"`+repeatDigest('a')+`"`)
		w.Header().Set("Set-Cookie", "must-not-be-forwarded")
		_, _ = io.WriteString(w, `{"version":1,"plugin":"forms-db"}`)
	}))
	defer server.Close()

	response, err := NewHTTPPluginAdminGateway(server.URL, "gateway-secret").Surface(context.Background(), "forms-db")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || response.ETag != `"`+repeatDigest('a')+`"` || string(response.Body) != `{"version":1,"plugin":"forms-db"}` {
		t.Fatalf("unexpected surface response: %#v", response)
	}
}

func TestHTTPPluginAdminGatewayBindsQueryToSurfaceDigest(t *testing.T) {
	digest := repeatDigest('b')
	body := []byte(`{"cursor":"opaque","limit":20}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/plugins/forms-db/admin/pages/submissions/query" {
			t.Errorf("unexpected query route: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("If-Match"); got != `"`+digest+`"` {
			t.Errorf("If-Match=%q", got)
		}
		if got, _ := io.ReadAll(r.Body); string(got) != string(body) {
			t.Errorf("body=%s", got)
		}
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"code":"plugin_surface_changed"}`)
	}))
	defer server.Close()

	response, err := NewHTTPPluginAdminGateway(server.URL, "").Query(context.Background(), "forms-db", "submissions", digest, body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusConflict || string(response.Body) != `{"code":"plugin_surface_changed"}` {
		t.Fatalf("stale surface response was not preserved: %#v", response)
	}
}

func TestHTTPPluginAdminGatewayUsesTwoPhaseDangerousActionHeaders(t *testing.T) {
	digest := repeatDigest('c')
	key := "action-0123456789abcdef"
	body := []byte(`{"recordId":"record-1"}`)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/api/plugins/forms-db/admin/pages/submissions/actions/delete" {
			t.Errorf("unexpected action route: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("If-Match") != `"`+digest+`"` || r.Header.Get("Idempotency-Key") != key {
			t.Errorf("action precondition headers differ: %#v", r.Header)
		}
		if got, _ := io.ReadAll(r.Body); string(got) != string(body) {
			t.Errorf("action input changed on retry: %s", got)
		}
		w.Header().Set("Content-Type", "application/problem+json")
		if requests == 1 {
			if got := r.Header.Get("X-Admin-Confirmation"); got != "" {
				t.Errorf("initial request unexpectedly carried a confirmation: %q", got)
			}
			w.WriteHeader(http.StatusPreconditionRequired)
			_, _ = io.WriteString(w, `{"code":"confirmation_required","confirmationToken":"opaque-token"}`)
			return
		}
		if got := r.Header.Get("X-Admin-Confirmation"); got != "opaque-token" {
			t.Errorf("confirmation token=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"deleted"}`)
	}))
	defer server.Close()

	client := NewHTTPPluginAdminGateway(server.URL, "")
	first, err := client.Action(context.Background(), "forms-db", "submissions", "delete", digest, key, "", body)
	if err != nil || first.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("initial challenge response=%#v err=%v", first, err)
	}
	second, err := client.Action(context.Background(), "forms-db", "submissions", "delete", digest, key, "opaque-token", body)
	if err != nil || second.StatusCode != http.StatusOK || requests != 2 {
		t.Fatalf("confirmed response=%#v requests=%d err=%v", second, requests, err)
	}
}

func TestHTTPPluginAdminGatewayDoesNotFollowRedirects(t *testing.T) {
	targetReached := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetReached = true }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirect.Close()

	response, err := NewHTTPPluginAdminGateway(redirect.URL, "secret").Surface(context.Background(), "forms-db")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusFound || targetReached {
		t.Fatalf("redirect was followed: status=%d targetReached=%v", response.StatusCode, targetReached)
	}
}

func repeatDigest(char byte) string {
	value := make([]byte, 64)
	for index := range value {
		value[index] = char
	}
	return string(value)
}
