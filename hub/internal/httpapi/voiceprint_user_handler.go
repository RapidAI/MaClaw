package httpapi

import (
	"io"
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/voiceprint"
)

// UserVoiceprintEnrollHandler allows authenticated users to enroll their own voiceprint.
// Expects multipart form: file=<wav>, label (optional).
func UserVoiceprintEnrollHandler(identity *auth.IdentityService, svc *voiceprint.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10MB hard cap
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart form"})
			return
		}

		label := r.FormValue("label")
		if label == "" {
			label = "default"
		}
		if len(label) > 64 {
			label = label[:64]
		}

		file, _, err := r.FormFile("file")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing 'file' field (WAV audio)"})
			return
		}
		defer file.Close()

		wavData, err := io.ReadAll(file)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "read file: " + err.Error()})
			return
		}

		vp, err := svc.Enroll(r.Context(), principal.UserID, principal.Email, label, wavData)
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

// UserVoiceprintListHandler returns the authenticated user's own voiceprints.
func UserVoiceprintListHandler(identity *auth.IdentityService, svc *voiceprint.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
			return
		}

		items, err := svc.ListByUser(r.Context(), principal.UserID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		type vpInfo struct {
			ID        string `json:"id"`
			Label     string `json:"label"`
			Dim       int    `json:"dim"`
			CreatedAt string `json:"created_at"`
		}
		out := make([]vpInfo, 0, len(items))
		for _, vp := range items {
			out = append(out, vpInfo{
				ID:        vp.ID,
				Label:     vp.Label,
				Dim:       len(vp.Embedding),
				CreatedAt: vp.CreatedAt.Format("2006-01-02 15:04:05"),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"voiceprints": out, "count": len(out)})
	}
}

// UserVoiceprintDeleteHandler allows a user to delete their own voiceprint by ID.
func UserVoiceprintDeleteHandler(identity *auth.IdentityService, svc *voiceprint.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
			return
		}

		id := r.URL.Query().Get("id")
		if id == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
			return
		}

		// Verify ownership: list user's voiceprints and check the ID belongs to them.
		items, err := svc.ListByUser(r.Context(), principal.UserID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		owned := false
		for _, vp := range items {
			if vp.ID == id {
				owned = true
				break
			}
		}
		if !owned {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "voiceprint not found or not owned by you"})
			return
		}

		if err := svc.Delete(r.Context(), id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}
