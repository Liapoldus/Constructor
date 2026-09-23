package presentation

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/Liapoldus/Constructor/internal/domain"
)

const pluginAdminJSONLimit int64 = 4 << 20

func (h *Handler) pluginAdminAPI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/plugins" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		if h.pluginAdmin == nil {
			writeProblem(w, http.StatusServiceUnavailable, "plugin_admin_unavailable", "Gateway Plugin Admin API is not configured.", nil)
			return
		}
		response, err := h.pluginAdmin.Plugins(r.Context())
		if err != nil {
			writeProblem(w, http.StatusServiceUnavailable, "plugin_admin_gateway_unavailable", "Gateway Plugin Admin API is unavailable.", nil)
			return
		}
		writePluginAdminResponse(w, response)
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/plugins/"), "/"), "/")
	if len(parts) < 3 || parts[1] != "admin" {
		writeProblem(w, http.StatusNotFound, "not_found", "Plugin Admin resource not found.", nil)
		return
	}
	if h.pluginAdmin == nil {
		writeProblem(w, http.StatusServiceUnavailable, "plugin_admin_unavailable", "Gateway Plugin Admin API is not configured.", nil)
		return
	}
	instance := parts[0]
	var response domain.PluginAdminResponse
	var err error
	switch {
	case len(parts) == 3 && parts[2] == "surface":
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		response, err = h.pluginAdmin.Surface(r.Context(), instance)
	case len(parts) == 5 && parts[2] == "pages" && parts[4] == "query":
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		body, ok := pluginAdminJSONBody(w, r)
		if !ok {
			return
		}
		digest, ok := pluginAdminDigest(w, r)
		if !ok {
			return
		}
		response, err = h.pluginAdmin.Query(r.Context(), instance, parts[3], digest, body)
	case len(parts) == 6 && parts[2] == "pages" && parts[4] == "actions":
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		body, ok := pluginAdminJSONBody(w, r)
		if !ok {
			return
		}
		digest, ok := pluginAdminDigest(w, r)
		if !ok {
			return
		}
		response, err = h.pluginAdmin.Action(r.Context(), instance, parts[3], parts[5], digest, r.Header.Get("Idempotency-Key"), r.Header.Get("X-Admin-Confirmation"), body)
	case len(parts) == 5 && parts[2] == "pages" && parts[4] == "health":
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		response, err = h.pluginAdmin.Health(r.Context(), instance, parts[3])
	default:
		writeProblem(w, http.StatusNotFound, "not_found", "Plugin Admin resource not found.", nil)
		return
	}
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrPluginAdminInvalidRequest):
			writeProblem(w, http.StatusBadRequest, "plugin_admin_request_invalid", "Plugin Admin request parameters are invalid.", nil)
		case errors.Is(err, domain.ErrPluginAdminGatewayUnavailable):
			writeProblem(w, http.StatusServiceUnavailable, "plugin_admin_gateway_unavailable", "Gateway Plugin Admin API is unavailable.", nil)
		default:
			writeProblem(w, http.StatusBadGateway, "plugin_admin_gateway_failed", "Gateway Plugin Admin request failed.", nil)
		}
		return
	}
	writePluginAdminResponse(w, response)
}

func pluginAdminJSONBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, ok := readLimitedRequestBody(w, r, pluginAdminJSONLimit)
	if !ok {
		return nil, false
	}
	var object map[string]json.RawMessage
	if len(body) == 0 || json.Unmarshal(body, &object) != nil || object == nil {
		writeProblem(w, http.StatusBadRequest, "invalid_input", "Plugin Admin input must be one JSON object.", nil)
		return nil, false
	}
	return body, true
}

func pluginAdminDigest(w http.ResponseWriter, r *http.Request) (string, bool) {
	value := r.Header.Get("If-Match")
	digest, err := strconv.Unquote(value)
	if err != nil || digest == "" {
		writeProblem(w, http.StatusPreconditionRequired, "plugin_surface_digest_required", "Send the active Admin Surface digest in If-Match.", nil)
		return "", false
	}
	return digest, true
}

func writePluginAdminResponse(w http.ResponseWriter, response domain.PluginAdminResponse) {
	if response.StatusCode < 100 || response.StatusCode > 599 {
		writeProblem(w, http.StatusBadGateway, "plugin_admin_gateway_invalid_response", "Gateway returned an invalid Plugin Admin response.", nil)
		return
	}
	mediaType, _, err := mime.ParseMediaType(response.ContentType)
	if err != nil || (mediaType != "application/json" && mediaType != "application/problem+json") {
		writeProblem(w, http.StatusBadGateway, "plugin_admin_gateway_invalid_response", "Gateway returned a non-JSON Plugin Admin response.", nil)
		return
	}
	w.Header().Set("Content-Type", response.ContentType)
	if response.ETag != "" {
		w.Header().Set("ETag", response.ETag)
	}
	if response.RequestID != "" {
		w.Header().Set("X-Gateway-Request-ID", response.RequestID)
	}
	if response.RetryAfter != "" {
		w.Header().Set("Retry-After", response.RetryAfter)
	}
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(response.Body)
}
