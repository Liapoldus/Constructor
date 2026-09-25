package presentation

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Liapoldus/Constructor/internal/domain"
)

func (h *Handler) gatewayGroupsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if h.gatewayGroups == nil {
		writeProblem(w, http.StatusServiceUnavailable, "gateway_groups_unavailable", "Gateway group catalog is not configured.", nil)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/gateway/groups")
	if path == "" {
		groups, err := h.gatewayGroups.ListGroups(r.Context())
		if err != nil {
			writeGatewayGroupError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, groups)
		return
	}
	if !strings.HasPrefix(path, "/") || !strings.HasSuffix(path, "/releases") {
		http.NotFound(w, r)
		return
	}
	groupID := strings.TrimSuffix(strings.TrimPrefix(path, "/"), "/releases")
	if groupID == "" || strings.Contains(groupID, "/") {
		http.NotFound(w, r)
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeProblem(w, http.StatusBadRequest, "gateway_group_query_invalid", "Release page limit must be between 1 and 100.", nil)
			return
		}
		limit = parsed
	}
	cursor := r.URL.Query().Get("cursor")
	releases, err := h.gatewayGroups.ListReleases(r.Context(), groupID, cursor, limit)
	if err != nil {
		writeGatewayGroupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, releases)
}

func writeGatewayGroupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrGatewayGroupQueryInvalid):
		writeProblem(w, http.StatusBadRequest, "gateway_group_query_invalid", "Gateway group query is invalid.", nil)
	case errors.Is(err, domain.ErrGatewayGroupsUnavailable):
		writeProblem(w, http.StatusServiceUnavailable, "gateway_groups_unavailable", "Gateway group catalog is unavailable.", nil)
	default:
		writeProblem(w, http.StatusBadGateway, "gateway_groups_upstream_failed", "Could not read Gateway groups.", nil)
	}
}
