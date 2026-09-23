package infrastructure

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Liapoldus/Constructor/internal/domain"
)

func TestHTTPGatewayClientPublishesBuildSourceAndWaitsForOperation(t *testing.T) {
	var publishCalls, pollCalls int
	revision := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gateway-secret" {
			t.Errorf("authorization header missing")
		}
		switch r.URL.Path {
		case "/api/sites/site-one/publish":
			publishCalls++
			if r.Method != http.MethodPost || r.Header.Get("Idempotency-Key") != "deployment-00000001" {
				t.Errorf("publish request method/headers = %s %#v", r.Method, r.Header)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode publish body: %v", err)
			}
			if body["source"] != "/artifacts/site" || body["idempotencyKey"] != "deployment-00000001" || body["expectedCurrentRevision"] != nil || len(body) != 3 {
				t.Errorf("publish body = %#v", body)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"operationId":"operation-1","state":"pending"}`))
		case "/api/operations/operation-1":
			pollCalls++
			_, _ = w.Write([]byte(`{"id":"operation-1","state":"succeeded","result":{"revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`))
		default:
			t.Errorf("unexpected Gateway path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewHTTPGatewayClient(server.URL, "gateway-secret")
	deployment := domain.Deployment{ID: "deployment-00000001", SiteID: "site-one"}
	build := domain.Build{ArtifactPath: "/artifacts/site"}
	if got, err := client.Publish(context.Background(), deployment, build, nil); err != nil || got != revision {
		t.Fatalf("revision=%q err=%v", got, err)
	}
	if publishCalls != 1 || pollCalls != 1 {
		t.Fatalf("Gateway publish/poll count = %d/%d", publishCalls, pollCalls)
	}
}

func TestHTTPGatewayClientRollbackUsesIdempotencyAndWaits(t *testing.T) {
	var rollbackCalls, pollCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/sites/site-one/rollback":
			rollbackCalls++
			if r.Method != http.MethodPost || r.Header.Get("Idempotency-Key") != "rollback-0000000000000000000000000000000000000000000000000000000000000000" {
				t.Errorf("rollback method/header = %s %q", r.Method, r.Header.Get("Idempotency-Key"))
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"operationId":"rollback-op","state":"running"}`))
		case "/api/operations/rollback-op":
			pollCalls++
			_, _ = w.Write([]byte(`{"state":"succeeded","result":{"revision":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}`))
		default:
			t.Errorf("unexpected Gateway path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewHTTPGatewayClient(server.URL, "")
	deployment := domain.Deployment{ID: "rollback-0000000000000000000000000000000000000000000000000000000000000000", SiteID: "site-one"}
	expected := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if got, err := client.Rollback(context.Background(), deployment, domain.Build{}, &expected); err != nil || got != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("revision=%q err=%v", got, err)
	}
	if rollbackCalls != 1 || pollCalls != 1 {
		t.Fatalf("Gateway rollback/poll count = %d/%d", rollbackCalls, pollCalls)
	}
}

func TestHTTPGatewayClientRejectsUnsuccessfulOperation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"operationId":"failed","state":"failed"}`))
	}))
	defer server.Close()
	client := NewHTTPGatewayClient(server.URL, "")
	_, err := client.Publish(context.Background(), domain.Deployment{ID: "deployment-00000001", SiteID: "site-one"}, domain.Build{ArtifactPath: "/artifact"}, nil)
	if err == nil {
		t.Fatal("Gateway failed state was accepted")
	}
}

func TestHTTPGatewayClientReadsCurrentRevisionAcrossPages(t *testing.T) {
	var requests int
	want := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			if r.URL.Query().Get("limit") != "100" {
				t.Errorf("query=%v", r.URL.Query())
			}
			_, _ = w.Write([]byte(`{"items":[],"nextCursor":"page-2"}`))
			return
		}
		if r.URL.Query().Get("cursor") != "page-2" {
			t.Errorf("query=%v", r.URL.Query())
		}
		payload, _ := json.Marshal(map[string]any{"items": []any{map[string]any{"slug": "site-one", "currentRevision": want}}, "nextCursor": nil})
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	got, err := NewHTTPGatewayClient(server.URL, "").CurrentRevision(context.Background(), "site-one")
	if err != nil || got == nil || *got != want || requests != 2 {
		t.Fatalf("revision=%v requests=%d err=%v", got, requests, err)
	}
}
