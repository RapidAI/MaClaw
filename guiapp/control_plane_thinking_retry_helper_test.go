package guiapp

import (
	"encoding/json"
	"net/http"
	"testing"
)

func decodeJSONBody(t *testing.T, w http.ResponseWriter, r *http.Request, dest *map[string]interface{}) bool {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		t.Errorf("decode request: %v", err)
		http.Error(w, "bad json", http.StatusBadRequest)
		return false
	}
	return true
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
