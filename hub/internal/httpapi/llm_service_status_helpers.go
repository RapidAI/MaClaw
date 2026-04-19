package httpapi

import (
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func attachLLMServiceStatus(r *http.Request, payload map[string]any, system store.SystemSettingsRepository, securitySvc *security.SecurityService, email string) {
	if payload == nil || system == nil || strings.TrimSpace(email) == "" {
		return
	}
	status, err := llmservice.ResolveServiceStatus(r.Context(), system, securitySvc, email, externalLLMBaseURL(r))
	if err != nil || status == nil {
		return
	}
	payload["service_status"] = status
}
