package infrastructure

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Liapoldus/Constructor/internal/domain"
)

const pluginAdminResponseLimit = 4 << 20

type HTTPPluginAdminGateway struct {
	baseURL string
	bearer  string
	client  *http.Client
}

func NewHTTPPluginAdminGateway(baseURL, bearer string) *HTTPPluginAdminGateway {
	return &HTTPPluginAdminGateway{
		baseURL: strings.TrimRight(baseURL, "/"),
		bearer:  bearer,
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (c *HTTPPluginAdminGateway) Plugins(ctx context.Context) (domain.PluginAdminResponse, error) {
	return c.do(ctx, http.MethodGet, "/api/plugins", "", "", "", nil)
}

func (c *HTTPPluginAdminGateway) Surface(ctx context.Context, instance string) (domain.PluginAdminResponse, error) {
	return c.do(ctx, http.MethodGet, "/api/plugins/"+instance+"/admin/surface", "", "", "", nil)
}

func (c *HTTPPluginAdminGateway) Query(ctx context.Context, instance, page, digest string, body []byte) (domain.PluginAdminResponse, error) {
	return c.do(ctx, http.MethodPost, "/api/plugins/"+instance+"/admin/pages/"+page+"/query", digest, "", "", body)
}

func (c *HTTPPluginAdminGateway) Action(ctx context.Context, instance, page, action, digest, idempotencyKey, confirmation string, body []byte) (domain.PluginAdminResponse, error) {
	path := "/api/plugins/" + instance + "/admin/pages/" + page + "/actions/" + action
	return c.do(ctx, http.MethodPost, path, digest, idempotencyKey, confirmation, body)
}

func (c *HTTPPluginAdminGateway) Health(ctx context.Context, instance, page string) (domain.PluginAdminResponse, error) {
	return c.do(ctx, http.MethodGet, "/api/plugins/"+instance+"/admin/pages/"+page+"/health", "", "", "", nil)
}

func (c *HTTPPluginAdminGateway) do(ctx context.Context, method, path, digest, idempotencyKey, confirmation string, body []byte) (domain.PluginAdminResponse, error) {
	if c == nil || c.baseURL == "" {
		return domain.PluginAdminResponse{}, domain.ErrPluginAdminGatewayUnavailable
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return domain.PluginAdminResponse{}, domain.ErrPluginAdminGatewayUnavailable
	}
	if c.bearer != "" {
		request.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	if digest != "" {
		request.Header.Set("If-Match", strconv.Quote(digest))
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	if confirmation != "" {
		request.Header.Set("X-Admin-Confirmation", confirmation)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return domain.PluginAdminResponse{}, domain.ErrPluginAdminGatewayUnavailable
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, pluginAdminResponseLimit+1))
	if err != nil || len(responseBody) > pluginAdminResponseLimit {
		return domain.PluginAdminResponse{}, fmt.Errorf("Gateway Plugin Admin response is unavailable or exceeds its size limit")
	}
	return domain.PluginAdminResponse{
		StatusCode:  response.StatusCode,
		ContentType: response.Header.Get("Content-Type"),
		ETag:        response.Header.Get("ETag"),
		RequestID:   response.Header.Get("X-Request-ID"),
		RetryAfter:  response.Header.Get("Retry-After"),
		Body:        responseBody,
	}, nil
}

var _ domain.PluginAdminGateway = (*HTTPPluginAdminGateway)(nil)
