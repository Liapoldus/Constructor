package infrastructure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Liapoldus/Constructor/internal/domain"
)

type HTTPGatewayClient struct {
	baseURL string
	bearer  string
	client  *http.Client
}

func NewHTTPGatewayClient(baseURL, bearer string) *HTTPGatewayClient {
	return &HTTPGatewayClient{baseURL: strings.TrimRight(baseURL, "/"), bearer: bearer, client: &http.Client{Timeout: 2 * time.Minute}}
}

func (c *HTTPGatewayClient) CurrentRevision(ctx context.Context, siteID string) (*string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cursor := ""
	seenCursors := map[string]struct{}{}
	for {
		endpoint := c.baseURL + "/api/sites?limit=100"
		if cursor != "" {
			endpoint += "&cursor=" + url.QueryEscape(cursor)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		c.authorize(request)
		response, err := c.client.Do(request)
		if err != nil {
			return nil, err
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			_ = response.Body.Close()
			return nil, fmt.Errorf("gateway site status returned HTTP %d", response.StatusCode)
		}
		var page struct {
			Items []struct {
				Slug            *string `json:"slug"`
				CurrentRevision *string `json:"currentRevision"`
			} `json:"items"`
			NextCursor *string `json:"nextCursor"`
		}
		decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&page)
		_ = response.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("gateway site status returned an invalid response")
		}
		for _, item := range page.Items {
			if item.Slug != nil && *item.Slug == siteID {
				return item.CurrentRevision, nil
			}
		}
		if page.NextCursor == nil || *page.NextCursor == "" {
			return nil, fmt.Errorf("gateway site %q was not found", siteID)
		}
		if _, seen := seenCursors[*page.NextCursor]; seen {
			return nil, fmt.Errorf("gateway site listing returned a repeated cursor")
		}
		seenCursors[*page.NextCursor] = struct{}{}
		cursor = *page.NextCursor
	}
}

func (c *HTTPGatewayClient) Publish(ctx context.Context, deployment domain.Deployment, build domain.Build, expected *string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	body, err := json.Marshal(struct {
		Source                  string  `json:"source"`
		IdempotencyKey          string  `json:"idempotencyKey"`
		ExpectedCurrentRevision *string `json:"expectedCurrentRevision"`
	}{build.ArtifactPath, deployment.ID, expected})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/sites/"+url.PathEscape(deployment.SiteID)+"/publish", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	c.authorize(request)
	request.Header.Set("Idempotency-Key", deployment.ID)
	return c.execute(request)
}

func (c *HTTPGatewayClient) Rollback(ctx context.Context, deployment domain.Deployment, _ domain.Build, expected *string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	body, err := json.Marshal(struct {
		IdempotencyKey          string  `json:"idempotencyKey"`
		ExpectedCurrentRevision *string `json:"expectedCurrentRevision"`
	}{deployment.ID, expected})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/sites/"+url.PathEscape(deployment.SiteID)+"/rollback", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	c.authorize(request)
	request.Header.Set("Idempotency-Key", deployment.ID)
	return c.execute(request)
}

func (c *HTTPGatewayClient) authorize(request *http.Request) {
	if c.bearer != "" {
		request.Header.Set("Authorization", "Bearer "+c.bearer)
	}
}

func (c *HTTPGatewayClient) execute(request *http.Request) (string, error) {
	response, err := c.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("gateway operation returned HTTP %d", response.StatusCode)
	}
	var operation struct {
		ID     string `json:"operationId"`
		State  string `json:"state"`
		Result struct {
			Revision string `json:"revision"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&operation); err != nil {
		return "", fmt.Errorf("gateway operation returned an invalid response")
	}
	if operation.State == "succeeded" {
		return requireGatewayRevision(operation.Result.Revision)
	}
	if (operation.State == "pending" || operation.State == "running") && operation.ID != "" {
		return c.waitForOperation(request.Context(), operation.ID)
	}
	return "", fmt.Errorf("gateway operation did not succeed")
}

func requireGatewayRevision(value string) (string, error) {
	if len(value) != 64 {
		return "", fmt.Errorf("gateway operation returned an invalid release revision")
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return "", fmt.Errorf("gateway operation returned an invalid release revision")
		}
	}
	return value, nil
}

func (c *HTTPGatewayClient) waitForOperation(ctx context.Context, id string) (string, error) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	path := c.baseURL + "/api/operations/" + url.PathEscape(id)
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
		if err != nil {
			return "", err
		}
		c.authorize(request)
		response, err := c.client.Do(request)
		if err != nil {
			return "", err
		}
		var operation struct {
			State  string `json:"state"`
			Result struct {
				Revision string `json:"revision"`
			} `json:"result"`
		}
		decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&operation)
		_ = response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 || decodeErr != nil {
			return "", fmt.Errorf("gateway operation status could not be read")
		}
		switch operation.State {
		case "succeeded":
			return requireGatewayRevision(operation.Result.Revision)
		case "failed", "cancelled":
			return "", fmt.Errorf("gateway operation did not succeed")
		case "pending", "running":
		default:
			return "", fmt.Errorf("gateway operation returned an unknown state")
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}
