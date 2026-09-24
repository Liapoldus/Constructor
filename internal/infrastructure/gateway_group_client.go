package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const gatewayGroupResponseLimit = 4 << 20

// HTTPGatewayGroupClient is a typed adapter for the documented Group Releases
// and durable operations endpoints. It is intentionally separate from the
// legacy site deployment client until Constructor's group mapping is defined.
type HTTPGatewayGroupClient struct {
	baseURL string
	bearer  string
	client  *http.Client
}

func NewHTTPGatewayGroupClient(baseURL, bearer string) *HTTPGatewayGroupClient {
	return &HTTPGatewayGroupClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		bearer:  bearer,
		client:  &http.Client{Timeout: 15 * time.Minute},
	}
}

type GatewayGroup struct {
	ID               string  `json:"id"`
	Kind             string  `json:"kind"`
	Active           bool    `json:"active"`
	CurrentRevision  *string `json:"currentRevision"`
	PreviousRevision *string `json:"previousRevision"`
	State            string  `json:"state"`
}

type GatewayGroupList struct {
	Items     []GatewayGroup `json:"items"`
	RequestID string         `json:"requestId"`
}

type GatewayFrontendRevision struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
	Files  int    `json:"files"`
}

type GatewayGroupRevision struct {
	ID              string                    `json:"id"`
	GroupID         string                    `json:"groupId"`
	Caddyfile       string                    `json:"caddyfile"`
	CaddyfileDigest string                    `json:"caddyfileDigest"`
	ArtifactDigest  *string                   `json:"artifactDigest"`
	Frontends       []GatewayFrontendRevision `json:"frontends"`
	CreatedAt       time.Time                 `json:"createdAt"`
	Actor           string                    `json:"actor,omitempty"`
}

type GatewayGroupRevisionList struct {
	Items      []GatewayGroupRevision `json:"items"`
	NextCursor *string                `json:"nextCursor"`
	RequestID  string                 `json:"requestId"`
}

type GatewayOperationReference struct {
	OperationID string `json:"operationId"`
	State       string `json:"state"`
	RequestID   string `json:"requestId"`
}

type GatewayOperation struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	State     string          `json:"state"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
	Result    json.RawMessage `json:"result,omitempty"`
	Problem   *GatewayProblem `json:"problem,omitempty"`
	RequestID string          `json:"requestId"`
}

type GatewayProblem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Code      string `json:"code"`
	Detail    string `json:"detail"`
	Instance  string `json:"instance"`
	RequestID string `json:"requestId"`
}

type GatewayGroupCreate struct {
	ID             string `json:"id"`
	IdempotencyKey string `json:"idempotencyKey"`
}

type GatewayRevisionMutation struct {
	IdempotencyKey          string  `json:"idempotencyKey"`
	ExpectedCurrentRevision *string `json:"expectedCurrentRevision"`
}

type GroupReleaseInput struct {
	IdempotencyKey          string
	ExpectedCurrentRevision *string
	Caddyfile               string
	Artifact                io.Reader
}

func (c *HTTPGatewayGroupClient) ListGroups(ctx context.Context) (GatewayGroupList, error) {
	var result GatewayGroupList
	err := c.doJSON(ctx, http.MethodGet, "/api/groups", nil, nil, http.StatusOK, &result)
	return result, err
}

func (c *HTTPGatewayGroupClient) GetGroup(ctx context.Context, groupID string) (GatewayGroup, error) {
	var result GatewayGroup
	err := c.doJSON(ctx, http.MethodGet, "/api/groups/"+url.PathEscape(groupID), nil, nil, http.StatusOK, &result)
	return result, err
}

func (c *HTTPGatewayGroupClient) CreateGroup(ctx context.Context, input GatewayGroupCreate) (GatewayGroup, error) {
	var result GatewayGroup
	err := c.doJSON(ctx, http.MethodPost, "/api/groups", input, nil, http.StatusCreated, &result)
	return result, err
}

func (c *HTTPGatewayGroupClient) ArchiveGroup(ctx context.Context, groupID, runtimeDigest, idempotencyKey string) (GatewayOperationReference, error) {
	var result GatewayOperationReference
	err := c.doJSON(ctx, http.MethodDelete, "/api/groups/"+url.PathEscape(groupID), struct {
		IdempotencyKey string `json:"idempotencyKey"`
	}{idempotencyKey}, map[string]string{"If-Match": runtimeDigest}, http.StatusAccepted, &result)
	return result, err
}

func (c *HTTPGatewayGroupClient) ActivateGroup(ctx context.Context, groupID, runtimeDigest, idempotencyKey string) (GatewayOperationReference, error) {
	var result GatewayOperationReference
	err := c.doJSON(ctx, http.MethodPost, "/api/groups/"+url.PathEscape(groupID)+"/activate", struct {
		IdempotencyKey string `json:"idempotencyKey"`
	}{idempotencyKey}, map[string]string{"If-Match": runtimeDigest}, http.StatusAccepted, &result)
	return result, err
}

func (c *HTTPGatewayGroupClient) ListReleases(ctx context.Context, groupID, cursor string, limit int) (GatewayGroupRevisionList, error) {
	query := url.Values{}
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	if limit != 0 {
		query.Set("limit", fmt.Sprint(limit))
	}
	endpoint := "/api/groups/" + url.PathEscape(groupID) + "/releases"
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	var result GatewayGroupRevisionList
	err := c.doJSON(ctx, http.MethodGet, endpoint, nil, nil, http.StatusOK, &result)
	return result, err
}

func (c *HTTPGatewayGroupClient) GetRelease(ctx context.Context, groupID, revisionID string) (GatewayGroupRevision, error) {
	var result GatewayGroupRevision
	path := "/api/groups/" + url.PathEscape(groupID) + "/releases/" + url.PathEscape(revisionID)
	err := c.doJSON(ctx, http.MethodGet, path, nil, nil, http.StatusOK, &result)
	return result, err
}

// PublishRelease streams the multipart upload. Artifact may be nil when the
// group release has no frontend archive.
func (c *HTTPGatewayGroupClient) PublishRelease(ctx context.Context, groupID string, input GroupReleaseInput) (GatewayOperationReference, error) {
	if !utf8.ValidString(input.Caddyfile) {
		return GatewayOperationReference{}, fmt.Errorf("group release Caddyfile must be valid UTF-8")
	}
	pipeReader, pipeWriter := io.Pipe()
	form := multipart.NewWriter(pipeWriter)
	multipartType := form.FormDataContentType()
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- writeGroupReleaseMultipart(pipeWriter, form, input)
	}()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/groups/"+url.PathEscape(groupID)+"/releases", pipeReader)
	if err != nil {
		_ = pipeReader.Close()
		_ = pipeWriter.CloseWithError(err)
		<-writeDone
		return GatewayOperationReference{}, err
	}
	request.Header.Set("Content-Type", multipartType)
	c.authorize(request)
	response, requestErr := c.client.Do(request)
	if requestErr != nil {
		_ = pipeReader.CloseWithError(requestErr)
		writeErr := <-writeDone
		if writeErr != nil {
			return GatewayOperationReference{}, fmt.Errorf("could not encode group release multipart request")
		}
		return GatewayOperationReference{}, requestErr
	}
	// A server may reject an upload before consuming the complete request body.
	// Closing the reader unblocks the producer before waiting for it to finish.
	_ = pipeReader.Close()
	writeErr := <-writeDone
	defer response.Body.Close()
	if writeErr != nil {
		return GatewayOperationReference{}, fmt.Errorf("could not encode group release multipart request")
	}
	if response.StatusCode != http.StatusAccepted {
		return GatewayOperationReference{}, gatewayStatusError(response.StatusCode)
	}
	var result GatewayOperationReference
	if err := decodeGatewayJSON(response.Body, &result); err != nil {
		return GatewayOperationReference{}, err
	}
	return result, nil
}

func writeGroupReleaseMultipart(pipe *io.PipeWriter, form *multipart.Writer, input GroupReleaseInput) error {
	writePart := func(name, mediaType, filename, value string) error {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", mimeContentDisposition(name, filename))
		header.Set("Content-Type", mediaType)
		part, err := form.CreatePart(header)
		if err != nil {
			return err
		}
		_, err = io.WriteString(part, value)
		return err
	}
	metadata, err := json.Marshal(struct {
		IdempotencyKey          string  `json:"idempotencyKey"`
		ExpectedCurrentRevision *string `json:"expectedCurrentRevision"`
	}{input.IdempotencyKey, input.ExpectedCurrentRevision})
	if err == nil {
		err = writePart("metadata", "application/json", "", string(metadata))
	}
	if err == nil {
		err = writePart("caddyfile", "text/plain; charset=utf-8", "", input.Caddyfile)
	}
	if err == nil && input.Artifact != nil {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", mimeContentDisposition("artifact", "artifact.tar.gz"))
		header.Set("Content-Type", "application/gzip")
		var part io.Writer
		part, err = form.CreatePart(header)
		if err == nil {
			_, err = io.Copy(part, input.Artifact)
		}
	}
	if closeErr := form.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = pipe.CloseWithError(err)
		return err
	}
	return pipe.Close()
}

func mimeContentDisposition(name, filename string) string {
	if filename == "" {
		return `form-data; name="` + name + `"`
	}
	return `form-data; name="` + name + `"; filename="` + filename + `"`
}

func (c *HTTPGatewayGroupClient) RollbackGroup(ctx context.Context, groupID string, mutation GatewayRevisionMutation) (GatewayOperationReference, error) {
	var result GatewayOperationReference
	path := "/api/groups/" + url.PathEscape(groupID) + "/rollback"
	err := c.doJSON(ctx, http.MethodPost, path, mutation, nil, http.StatusAccepted, &result)
	return result, err
}

func (c *HTTPGatewayGroupClient) GetOperation(ctx context.Context, operationID string) (GatewayOperation, error) {
	var result GatewayOperation
	err := c.doJSON(ctx, http.MethodGet, "/api/operations/"+url.PathEscape(operationID), nil, nil, http.StatusOK, &result)
	return result, err
}

func (c *HTTPGatewayGroupClient) doJSON(ctx context.Context, method, path string, body any, headers map[string]string, expectedStatus int, output any) error {
	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("could not encode Gateway request")
		}
		requestBody = strings.NewReader(string(encoded))
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, requestBody)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	c.authorize(request)
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != expectedStatus {
		return gatewayStatusError(response.StatusCode)
	}
	return decodeGatewayJSON(response.Body, output)
}

func (c *HTTPGatewayGroupClient) authorize(request *http.Request) {
	if c.bearer != "" {
		request.Header.Set("Authorization", "Bearer "+c.bearer)
	}
}

func decodeGatewayJSON(body io.Reader, target any) error {
	data, err := io.ReadAll(io.LimitReader(body, gatewayGroupResponseLimit+1))
	if err != nil || len(data) > gatewayGroupResponseLimit || json.Unmarshal(data, target) != nil {
		return fmt.Errorf("Gateway returned an invalid response")
	}
	return nil
}

func gatewayStatusError(status int) error {
	return fmt.Errorf("Gateway returned HTTP %d", status)
}
