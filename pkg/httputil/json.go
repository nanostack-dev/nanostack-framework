// Package httputil holds small HTTP response helpers shared across services.
package httputil

import (
	"encoding/json"
	"net/http"
)

// WriteJSON sets the JSON content type, writes status, and encodes value as
// the response body, in that order. Every JSON response handler in the
// framework and its consuming services builds this same three-step sequence
// by hand; WriteJSON gives them one call instead.
func WriteJSON(w http.ResponseWriter, status int, value any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(value)
}
