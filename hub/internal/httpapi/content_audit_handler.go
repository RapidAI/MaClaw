package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const contentAuditConfigKey = "content_audit_config"

// GetContentAuditConfigHandler reads the content audit config from SystemSettings.
func GetContentAuditConfigHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		raw, err := system.Get(r.Context(), contentAuditConfigKey)
		if err != nil || raw == "" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(im.ContentAuditDynamicConfig{})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(raw))
	}
}

// UpdateContentAuditConfigHandler writes the content audit config to SystemSettings.
func UpdateContentAuditConfigHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		var cfg im.ContentAuditDynamicConfig
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB limit
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		data, err := json.Marshal(cfg)
		if err != nil {
			http.Error(w, "marshal error", http.StatusInternalServerError)
			return
		}
		if err := system.Set(r.Context(), contentAuditConfigKey, string(data)); err != nil {
			http.Error(w, "save failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}
}
