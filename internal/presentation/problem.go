package presentation

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Liapoldus/Constructor/internal/domain"
)

type requestMetadataWriter struct {
	http.ResponseWriter
	requestID string
	instance  string
}

type problemDetail struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Status      int    `json:"status"`
	Detail      string `json:"detail,omitempty"`
	Instance    string `json:"instance,omitempty"`
	Code        string `json:"code"`
	RequestID   string `json:"requestId,omitempty"`
	LegacyError string `json:"error,omitempty"`
}

var requestFallbackCounter atomic.Uint64

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set("X-Request-ID", id)
		writer := &requestMetadataWriter{ResponseWriter: w, requestID: id, instance: r.URL.Path}
		next.ServeHTTP(writer, r)
	})
}

func newRequestID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}
	return fmt.Sprintf("fallback-%x-%x", time.Now().UnixNano(), requestFallbackCounter.Add(1))
}

func writeProblem(w http.ResponseWriter, status int, code, detail string, extensions map[string]any) {
	problem := problemDetail{
		Type:   "about:blank",
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
		Code:   code,
	}
	if metadata, ok := w.(*requestMetadataWriter); ok {
		problem.RequestID = metadata.requestID
		problem.Instance = metadata.instance
	}
	body, _ := json.Marshal(problem)
	var document map[string]json.RawMessage
	_ = json.Unmarshal(body, &document)
	for key, value := range extensions {
		encoded, err := json.Marshal(value)
		if err == nil {
			document[key] = encoded
		}
	}
	body, _ = json.Marshal(document)
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}

func writeValidationProblem(w http.ResponseWriter, diagnostics []domain.Diagnostic) {
	writeProblem(w, http.StatusUnprocessableEntity, "project_validation_failed", "The project model violates a schema or cross-reference invariant.", map[string]any{
		"valid":       false,
		"diagnostics": diagnostics,
	})
}

func requestIDFromWriter(w http.ResponseWriter) string {
	if metadata, ok := w.(*requestMetadataWriter); ok {
		return metadata.requestID
	}
	return strings.TrimSpace(w.Header().Get("X-Request-ID"))
}

func addRequestID(w http.ResponseWriter, body []byte) []byte {
	id := requestIDFromWriter(w)
	if id == "" {
		return body
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(body, &document); err != nil || document == nil {
		return body
	}
	if _, exists := document["requestId"]; !exists {
		if encoded, err := json.Marshal(id); err == nil {
			document["requestId"] = encoded
		}
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return body
	}
	return encoded
}
