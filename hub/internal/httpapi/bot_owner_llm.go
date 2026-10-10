package httpapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
)

// botOwnerLLM mints a viewer token for the Hub user who owns a bot.
// MaClawSrv calls Hub LLM with that token, so usage statistics stay on
// that user. A service-group API key would file the same calls under sys_user.
type botOwnerLLM struct {
	identity *auth.IdentityService
	center   *center.Service
}

func (b botOwnerLLM) IssueViewerTokenForUser(ctx context.Context, userID string) (string, error) {
	if b.identity == nil {
		return "", fmt.Errorf("hub identity is not configured")
	}
	return b.identity.IssueViewerTokenForUser(ctx, userID)
}

func (b botOwnerLLM) PublicLLMBaseURL(ctx context.Context) string {
	if b.center == nil {
		return ""
	}
	return hubPublicLLMEndpoint(b.center.GetPublicBaseURL(ctx))
}

func hubPublicLLMEndpoint(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return ""
	}
	const suffix = "/api/llm/v1"
	if strings.HasSuffix(base, suffix) {
		return base
	}
	return base + suffix
}
