package http

import (
	"encoding/json"
	"net/http"
)

// respondError sends a structured JSON error response.
// Shared across all HTTP handlers to avoid code duplication.
func respondError(w http.ResponseWriter, statusCode int, code string, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := map[string]interface{}{
		"error": message,
		"code":  code,
	}

	_ = json.NewEncoder(w).Encode(response)
}
