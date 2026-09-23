package presentation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Liapoldus/Constructor/internal/application"
	"github.com/Liapoldus/Constructor/internal/domain"
)

type pluginAdminGatewayStub struct {
	response     domain.PluginAdminResponse
	instance     string
	page         string
	action       string
	digest       string
	key          string
	confirmation string
	body         []byte
	queries      int
	actions      int
}

func newPluginAdminTestHandler(gateway domain.PluginAdminGateway) http.Handler {
	return NewHandlerWithPluginAdmin(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, application.NewAuthService(previewTestAuthorizer{}), nil, application.NewPluginAdminService(gateway))
}

func (s *pluginAdminGatewayStub) Plugins(context.Context) (domain.PluginAdminResponse, error) {
	return s.response, nil
}
func (s *pluginAdminGatewayStub) Surface(_ context.Context, instance string) (domain.PluginAdminResponse, error) {
	s.instance = instance
	return s.response, nil
}
func (s *pluginAdminGatewayStub) Query(_ context.Context, instance, page, digest string, body []byte) (domain.PluginAdminResponse, error) {
	s.queries++
	s.instance, s.page, s.digest, s.body = instance, page, digest, body
	return s.response, nil
}
func (s *pluginAdminGatewayStub) Action(_ context.Context, instance, page, action, digest, key, confirmation string, body []byte) (domain.PluginAdminResponse, error) {
	s.actions++
	s.instance, s.page, s.action, s.digest, s.key, s.confirmation, s.body = instance, page, action, digest, key, confirmation, body
	return s.response, nil
}
func (s *pluginAdminGatewayStub) Health(_ context.Context, instance, page string) (domain.PluginAdminResponse, error) {
	s.instance, s.page = instance, page
	return s.response, nil
}

func TestPluginAdminSurfaceIsProxiedThroughTheServerBoundary(t *testing.T) {
	gateway := &pluginAdminGatewayStub{response: domain.PluginAdminResponse{StatusCode: http.StatusOK, ContentType: "application/json", ETag: `"surface-digest"`, Body: []byte(`{"version":1,"plugin":"forms-db"}`)}}
	handler := newPluginAdminTestHandler(gateway)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/plugins/forms-db/admin/surface", nil))
	if response.Code != http.StatusOK || response.Header().Get("ETag") != `"surface-digest"` || gateway.instance != "forms-db" {
		t.Fatalf("unexpected Surface proxy response: status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
}

func TestPluginAdminQueryRequiresETagAndForwardsStructuredInput(t *testing.T) {
	gateway := &pluginAdminGatewayStub{response: domain.PluginAdminResponse{StatusCode: http.StatusOK, ContentType: "application/json", Body: []byte(`{"rows":[]}`)}}
	handler := newPluginAdminTestHandler(gateway)
	withoutDigest := httptest.NewRecorder()
	handler.ServeHTTP(withoutDigest, httptest.NewRequest(http.MethodPost, "/api/plugins/forms-db/admin/pages/submissions/query", strings.NewReader(`{"limit":20}`)))
	if withoutDigest.Code != http.StatusPreconditionRequired || gateway.queries != 0 {
		t.Fatalf("query without digest reached Gateway: status=%d calls=%d", withoutDigest.Code, gateway.queries)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/plugins/forms-db/admin/pages/submissions/query", strings.NewReader(`{"limit":20}`))
	request.Header.Set("If-Match", `"`+strings.Repeat("a", 64)+`"`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || gateway.queries != 1 || gateway.instance != "forms-db" || gateway.page != "submissions" || gateway.digest != strings.Repeat("a", 64) || string(gateway.body) != `{"limit":20}` {
		t.Fatalf("unexpected query forwarding: status=%d calls=%d gateway=%#v body=%s", response.Code, gateway.queries, gateway, response.Body.String())
	}
}

func TestPluginAdminActionForwardsTheBoundConfirmationHandshake(t *testing.T) {
	digest := strings.Repeat("b", 64)
	key := "action-idempotency-key-0001"
	gateway := &pluginAdminGatewayStub{response: domain.PluginAdminResponse{StatusCode: http.StatusPreconditionRequired, ContentType: "application/problem+json", Body: []byte(`{"code":"confirmation_required","confirmationToken":"opaque"}`)}}
	handler := newPluginAdminTestHandler(gateway)
	request := httptest.NewRequest(http.MethodPost, "/api/plugins/forms-db/admin/pages/submissions/actions/delete", strings.NewReader(`{"recordId":"r1"}`))
	request.Header.Set("If-Match", `"`+digest+`"`)
	request.Header.Set("Idempotency-Key", key)
	request.Header.Set("X-Admin-Confirmation", "opaque")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusPreconditionRequired || gateway.actions != 1 || gateway.key != key || gateway.confirmation != "opaque" || gateway.digest != digest {
		t.Fatalf("confirmation challenge did not pass through: status=%d call=%#v body=%s", response.Code, gateway, response.Body.String())
	}
	var problem map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil || string(problem["confirmationToken"]) != `"opaque"` {
		t.Fatalf("Gateway challenge body changed: %s err=%v", response.Body.String(), err)
	}
}

func TestPluginAdminProxyRejectsUnknownPathsAndNonJSONResponses(t *testing.T) {
	gateway := &pluginAdminGatewayStub{response: domain.PluginAdminResponse{StatusCode: http.StatusOK, ContentType: "text/html", Body: []byte("<script>alert(1)</script>")}}
	handler := newPluginAdminTestHandler(gateway)
	unknown := httptest.NewRecorder()
	handler.ServeHTTP(unknown, httptest.NewRequest(http.MethodGet, "/api/plugins/forms-db/admin/anything?url=http://127.0.0.1", nil))
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown Gateway path status=%d body=%s", unknown.Code, unknown.Body.String())
	}
	nonJSON := httptest.NewRecorder()
	handler.ServeHTTP(nonJSON, httptest.NewRequest(http.MethodGet, "/api/plugins/forms-db/admin/surface", nil))
	if nonJSON.Code != http.StatusBadGateway || strings.Contains(nonJSON.Body.String(), "<script>") {
		t.Fatalf("non-JSON response was forwarded: status=%d body=%s", nonJSON.Code, nonJSON.Body.String())
	}
}

func TestPluginListUsesTheFixedGatewayEndpoint(t *testing.T) {
	gateway := &pluginAdminGatewayStub{response: domain.PluginAdminResponse{StatusCode: http.StatusOK, ContentType: "application/json", Body: []byte(`{"items":[]}`)}}
	handler := newPluginAdminTestHandler(gateway)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/plugins", nil))
	if response.Code != http.StatusOK || response.Body.String() != `{"items":[]}` {
		t.Fatalf("plugin list response=%d %s", response.Code, response.Body.String())
	}
}
