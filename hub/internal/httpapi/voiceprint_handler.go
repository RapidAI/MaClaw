package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/voiceprint"
)

// GetVoiceprintConfigHandler returns the current voiceprint configuration.
func GetVoiceprintConfigHandler(svc *voiceprint.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := svc.LoadConfig(r.Context())
		writeJSON(w, http.StatusOK, cfg)
	}
}

// UpdateVoiceprintConfigHandler saves voiceprint configuration.
func UpdateVoiceprintConfigHandler(svc *voiceprint.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var cfg voiceprint.Config
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := svc.SaveConfig(r.Context(), cfg); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, svc.LoadConfig(r.Context()))
	}
}

// VoiceprintEnrollHandler enrolls a voiceprint from uploaded WAV audio.
// Expects multipart form: file=<wav>, user_id, email, label (optional).
func VoiceprintEnrollHandler(svc *voiceprint.Service, users store.UserRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil { // 10MB max
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart form"})
			return
		}

		email := r.FormValue("email")
		label := r.FormValue("label")
		if label == "" {
			label = "default"
		}

		// Look up user by email
		if email == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email is required"})
			return
		}
		user, err := users.GetByEmail(r.Context(), email)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if user == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
			return
		}

		file, _, err := r.FormFile("file")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing 'file' field (WAV audio)"})
			return
		}
		defer file.Close()

		wavData, err := io.ReadAll(io.LimitReader(file, 10<<20)) // 10MB hard cap
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "read file: " + err.Error()})
			return
		}

		vp, err := svc.Enroll(r.Context(), user.ID, email, label, wavData)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"id":         vp.ID,
			"user_id":    vp.UserID,
			"email":      vp.Email,
			"label":      vp.Label,
			"dim":        len(vp.Embedding),
			"created_at": vp.CreatedAt,
		})
	}
}

// VoiceprintIdentifyHandler identifies a speaker from uploaded WAV audio.
// Expects multipart form: file=<wav>
func VoiceprintIdentifyHandler(svc *voiceprint.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart form"})
			return
		}

		file, _, err := r.FormFile("file")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing 'file' field (WAV audio)"})
			return
		}
		defer file.Close()

		wavData, err := io.ReadAll(io.LimitReader(file, 10<<20)) // 10MB hard cap
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "read file: " + err.Error()})
			return
		}

		matches, err := svc.Identify(r.Context(), wavData)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"matches": matches,
			"count":   len(matches),
		})
	}
}

// ListVoiceprintsHandler lists all enrolled voiceprints.
func ListVoiceprintsHandler(svc *voiceprint.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		all, err := svc.ListAll(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		type vpInfo struct {
			ID        string `json:"id"`
			UserID    string `json:"user_id"`
			Email     string `json:"email"`
			Label     string `json:"label"`
			Dim       int    `json:"dim"`
			CreatedAt string `json:"created_at"`
		}
		items := make([]vpInfo, 0, len(all))
		for _, vp := range all {
			items = append(items, vpInfo{
				ID:        vp.ID,
				UserID:    vp.UserID,
				Email:     vp.Email,
				Label:     vp.Label,
				Dim:       len(vp.Embedding),
				CreatedAt: vp.CreatedAt.Format("2006-01-02 15:04:05"),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"voiceprints": items, "count": len(items)})
	}
}

// DeleteVoiceprintHandler deletes a single voiceprint by ID.
func DeleteVoiceprintHandler(svc *voiceprint.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
			return
		}
		if err := svc.Delete(r.Context(), id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}
