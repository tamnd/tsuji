package gateway

import (
	"encoding/json"
	"net/http"
)

// APIError is the OpenAI-shaped error body: {"error": {...}}.
type APIError struct {
	Code     int            `json:"code"`
	Message  string         `json:"message"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type errorBody struct {
	Error APIError `json:"error"`
}

// WriteError sends an OpenAI-shaped error with the matching HTTP status.
func WriteError(w http.ResponseWriter, status int, msg string, meta map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody{Error: APIError{Code: status, Message: msg, Metadata: meta}})
}
