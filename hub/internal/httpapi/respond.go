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
	writeErrorWithFields(w, status, code, message, nil)
}

func writeErrorWithFields(w http.ResponseWriter, status int, code string, message string, fields map[string]any) {
	payload := map[string]any{
		"ok":      false,
		"code":    code,
		"message": message,
	}
	for key, value := range fields {
		payload[key] = value
	}
	writeJSON(w, status, payload)
}
