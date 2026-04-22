package httpapi

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]any{
		"ok":      false,
		"code":    code,
		"message": message,
	})
}

func writeClientAwareError(w http.ResponseWriter, status int, code string, message string, retryable bool, retryOtherNode bool) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":             code,
			"message":          message,
			"retryable":        retryable,
			"retry_other_node": retryOtherNode,
		},
	})
}
