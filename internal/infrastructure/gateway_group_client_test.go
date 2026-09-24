package infrastructure

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGatewayGroupClientListsGroupsAndUsesBearer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/groups" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer group-token" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = io.WriteString(w, `{"items":[{"id":"frontend","kind":"application","active":true,"currentRevision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","previousRevision":null,"state":"ready"}],"requestId":"request-1"}`)
	}))
	defer server.Close()

	groups, err := NewHTTPGatewayGroupClient(server.URL, "group-token").ListGroups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(groups.Items) != 1 || groups.Items[0].ID != "frontend" || groups.Items[0].CurrentRevision == nil || *groups.Items[0].CurrentRevision != strings.Repeat("a", 64) {
		t.Fatalf("groups = %#v", groups)
	}
}

func TestGatewayGroupClientPublishesMultipartAndReturnsOperation(t *testing.T) {
	wantCaddyfile := "example.test {\n  respond 200\n}\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/groups/app/releases" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer group-token" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "multipart/form-data" {
			t.Fatalf("content-type = %q, err=%v", r.Header.Get("Content-Type"), err)
		}
		reader := multipart.NewReader(r.Body, params["boundary"])
		parts := map[string]string{}
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(part)
			if err != nil {
				t.Fatal(err)
			}
			parts[part.FormName()] = string(data)
			if part.FormName() == "artifact" {
				if part.FileName() != "artifact.tar.gz" || part.Header.Get("Content-Type") != "application/gzip" {
					t.Errorf("artifact headers = %#v, filename=%q", part.Header, part.FileName())
				}
			}
		}
		var metadata struct {
			IdempotencyKey          string  `json:"idempotencyKey"`
			ExpectedCurrentRevision *string `json:"expectedCurrentRevision"`
		}
		if err := json.Unmarshal([]byte(parts["metadata"]), &metadata); err != nil {
			t.Fatal(err)
		}
		if metadata.IdempotencyKey != "release-key-00000001" || metadata.ExpectedCurrentRevision == nil || *metadata.ExpectedCurrentRevision != strings.Repeat("a", 64) {
			t.Errorf("metadata = %#v", metadata)
		}
		if parts["caddyfile"] != wantCaddyfile || parts["artifact"] != "compressed-frontend" {
			t.Errorf("multipart parts = %#v", parts)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"operationId":"operation-1","state":"pending","requestId":"request-1"}`)
	}))
	defer server.Close()

	expected := strings.Repeat("a", 64)
	operation, err := NewHTTPGatewayGroupClient(server.URL, "group-token").PublishRelease(context.Background(), "app", GroupReleaseInput{
		IdempotencyKey: "release-key-00000001", ExpectedCurrentRevision: &expected,
		Caddyfile: wantCaddyfile, Artifact: strings.NewReader("compressed-frontend"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if operation.OperationID != "operation-1" || operation.State != "pending" || operation.RequestID != "request-1" {
		t.Fatalf("operation = %#v", operation)
	}
}

func TestGatewayGroupClientReadsReleaseAndOperationAsTypedResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/groups/app/releases/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa":
			_, _ = io.WriteString(w, `{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","groupId":"app","caddyfile":"example.test {}","caddyfileDigest":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","artifactDigest":null,"frontends":[],"createdAt":"2026-09-24T10:00:00Z","actor":"operator"}`)
		case "/api/operations/op-1":
			_, _ = io.WriteString(w, `{"id":"op-1","kind":"group-release","state":"succeeded","createdAt":"2026-09-24T10:00:00Z","updatedAt":"2026-09-24T10:00:01Z","result":{"revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"requestId":"request-2"}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewHTTPGatewayGroupClient(server.URL, "")
	release, err := client.GetRelease(context.Background(), "app", strings.Repeat("a", 64))
	if err != nil || release.GroupID != "app" || release.Caddyfile != "example.test {}" || release.Actor != "operator" {
		t.Fatalf("release=%#v err=%v", release, err)
	}
	operation, err := client.GetOperation(context.Background(), "op-1")
	if err != nil || operation.State != "succeeded" || operation.Result == nil {
		t.Fatalf("operation=%#v err=%v", operation, err)
	}
}

func TestGatewayGroupClientImplementsGroupControlMutationsAndReleasePaging(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/groups/app/releases":
			if r.URL.Query().Get("cursor") != "next page" || r.URL.Query().Get("limit") != "25" {
				t.Errorf("release query = %v", r.URL.Query())
			}
			_, _ = io.WriteString(w, `{"items":[],"nextCursor":null,"requestId":"page-request"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/groups/app":
			_, _ = io.WriteString(w, `{"id":"app","kind":"application","active":true,"currentRevision":null,"previousRevision":null,"state":"empty"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/groups":
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request["id"] != "app" || request["idempotencyKey"] != "create-key-0000001" {
				t.Errorf("create request = %#v, err=%v", request, err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"app","kind":"application","active":false,"currentRevision":null,"previousRevision":null,"state":"empty"}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/groups/app":
			assertIfMatchAndIdempotency(t, r, strings.Repeat("d", 64), "archive-key-0000001")
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"operationId":"archive-op","state":"pending","requestId":"archive-request"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/groups/app/activate":
			assertIfMatchAndIdempotency(t, r, strings.Repeat("d", 64), "activate-key-000001")
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"operationId":"activate-op","state":"pending","requestId":"activate-request"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/groups/app/rollback":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["idempotencyKey"] != "rollback-key-000001" || body["expectedCurrentRevision"] != strings.Repeat("a", 64) {
				t.Errorf("rollback request = %#v, err=%v", body, err)
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"operationId":"rollback-op","state":"running","requestId":"rollback-request"}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.RequestURI())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewHTTPGatewayGroupClient(server.URL, "token")

	if _, err := client.CreateGroup(context.Background(), GatewayGroupCreate{ID: "app", IdempotencyKey: "create-key-0000001"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetGroup(context.Background(), "app"); err != nil {
		t.Fatal(err)
	}
	page, err := client.ListReleases(context.Background(), "app", "next page", 25)
	if err != nil || page.RequestID != "page-request" {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	if _, err := client.ArchiveGroup(context.Background(), "app", strings.Repeat("d", 64), "archive-key-0000001"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ActivateGroup(context.Background(), "app", strings.Repeat("d", 64), "activate-key-000001"); err != nil {
		t.Fatal(err)
	}
	expected := strings.Repeat("a", 64)
	if _, err := client.RollbackGroup(context.Background(), "app", GatewayRevisionMutation{IdempotencyKey: "rollback-key-000001", ExpectedCurrentRevision: &expected}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"POST /api/groups", "GET /api/groups/app", "GET /api/groups/app/releases?cursor=next+page&limit=25",
		"DELETE /api/groups/app", "POST /api/groups/app/activate", "POST /api/groups/app/rollback",
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %#v", calls)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Errorf("call[%d] = %q, want %q", i, calls[i], want[i])
		}
	}
}

func assertIfMatchAndIdempotency(t *testing.T, r *http.Request, digest, idempotencyKey string) {
	t.Helper()
	if got := r.Header.Get("If-Match"); got != digest {
		t.Errorf("If-Match = %q", got)
	}
	var request map[string]any
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request["idempotencyKey"] != idempotencyKey || len(request) != 1 {
		t.Errorf("mutation request = %#v, err=%v", request, err)
	}
}
