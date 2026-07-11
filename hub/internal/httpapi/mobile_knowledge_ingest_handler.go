package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// MobileAgentKnowledgeIngestHandler accepts free-form notes from Mobile and
// indexes them into the Hub knowledge store for the official full agent.
//
//	POST /api/mobile/agent/knowledge/ingest
//	body: {"text":"...","title":"..."}  (title optional)
func MobileAgentKnowledgeIngestHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		_, _, err = mobileEnsureCoreAgent()
		if err != nil || mobileKnowledgeStore == nil {
			writeError(w, http.StatusServiceUnavailable, "KNOWLEDGE_UNAVAILABLE", "knowledge store is not available")
			return
		}

		var body struct {
			Text  string `json:"text"`
			Title string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid JSON body")
			return
		}
		text := strings.TrimSpace(body.Text)
		if text == "" {
			writeError(w, http.StatusBadRequest, "TEXT_REQUIRED", "text is required")
			return
		}
		if utf8.RuneCountInString(text) > mobileKnowledgeMaxRunes {
			runes := []rune(text)
			text = string(runes[:mobileKnowledgeMaxRunes]) + "\n\n…(truncated for knowledge ingest)"
		}
		title := strings.TrimSpace(body.Title)
		if title == "" {
			title = "mobile-note"
		}
		if utf8.RuneCountInString(title) > 200 {
			title = string([]rune(title)[:200])
		}

		tenantID := strings.TrimSpace(principal.TenantID)
		if tenantID == "" {
			tenantID = "default"
		}
		ownerID := strings.TrimSpace(principal.UserID)
		if ownerID == "" {
			ownerID = strings.TrimSpace(principal.Email)
		}

		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		src, err := mobileKnowledgeStore.SaveText(ctx, knowledge.TextSaveRequest{
			Text:      text,
			Title:     title,
			Kind:      "mobile_note",
			OwnerID:   ownerID,
			TenantID:  tenantID,
			TopicHint: "mobile_note",
			Labels:    []string{"mobile", "note", "user_ingest"},
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INGEST_FAILED", "failed to index note into knowledge store")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         true,
			"source_id":  src.ID,
			"title":      title,
			"rune_count": utf8.RuneCountInString(text),
			"mode":       mobileKnowledgeMode,
		})
	}
}
