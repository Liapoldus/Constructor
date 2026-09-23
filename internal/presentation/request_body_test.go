package presentation

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeRequestJSONAcceptsExactLimitAndRejectsOversize(t *testing.T) {
	valid := []byte(`{"name":"test"}`)
	exactLimit := append(valid, bytes.Repeat([]byte(" "), int(smallJSONRequestLimit)-len(valid))...)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/test", bytes.NewReader(exactLimit))
	response := httptest.NewRecorder()
	var value struct {
		Name string `json:"name"`
	}
	if !decodeRequestJSON(response, request, smallJSONRequestLimit, &value) {
		t.Fatalf("request at the limit was rejected: %s", response.Body.String())
	}
	if value.Name != "test" {
		t.Fatalf("unexpected decoded request: %#v", value)
	}

	oversized := append(exactLimit, ' ')
	request = httptest.NewRequest(http.MethodPost, "/api/v1/test", bytes.NewReader(oversized))
	response = httptest.NewRecorder()
	value = struct {
		Name string `json:"name"`
	}{}
	if decodeRequestJSON(response, request, smallJSONRequestLimit, &value) {
		t.Fatal("oversized request was accepted")
	}
	if response.Code != http.StatusRequestEntityTooLarge || response.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("oversized request did not return RFC 9457 413: status=%d body=%s", response.Code, response.Body.String())
	}
	var problem map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if problem["code"] != "payload_too_large" || problem["status"] != float64(http.StatusRequestEntityTooLarge) {
		t.Fatalf("unexpected size-limit problem: %#v", problem)
	}
}

func TestDecodeRequestJSONRejectsMultipleValues(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/test", strings.NewReader(`{"name":"one"}{"name":"two"}`))
	response := httptest.NewRecorder()
	var value map[string]string
	if decodeRequestJSON(response, request, smallJSONRequestLimit, &value) {
		t.Fatal("multiple JSON values were accepted")
	}
	if response.Code != http.StatusBadRequest {
		t.Fatalf("multiple JSON values returned %d, want 400", response.Code)
	}
}

func TestDecodeRequestJSONRejectsUnknownFields(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/test", strings.NewReader(`{"name":"one","typo":true}`))
	response := httptest.NewRecorder()
	var value struct {
		Name string `json:"name"`
	}
	if decodeRequestJSON(response, request, smallJSONRequestLimit, &value) {
		t.Fatal("unknown request field was accepted")
	}
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown field returned %d, want 400", response.Code)
	}
}
