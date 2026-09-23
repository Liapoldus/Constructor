package presentation

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const (
	smallJSONRequestLimit  int64 = 4 << 10
	structuredRequestLimit int64 = 10 << 20
)

func decodeRequestJSON(w http.ResponseWriter, r *http.Request, limit int64, destination any) bool {
	body, ok := readLimitedRequestBody(w, r, limit)
	if !ok {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, errors.New("request body must contain exactly one JSON value"))
		return false
	}
	return true
}

func readLimitedRequestBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return nil, false
	}
	if int64(len(body)) > limit {
		writeError(w, http.StatusRequestEntityTooLarge, errors.New("request body exceeds the configured size limit"))
		return nil, false
	}
	return body, true
}
