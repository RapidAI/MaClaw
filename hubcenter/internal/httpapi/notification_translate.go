package httpapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/notification"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

// serviceGroupTranslator translates an admin notice through the system free
// group. A missing group or a failed call is returned as an error so the
// notice still publishes and that language is omitted.
type serviceGroupTranslator struct {
	settings store.SystemSettingsRepository
	llm      *llmservice.Service
}

func newServiceGroupTranslator(settings store.SystemSettingsRepository, llm *llmservice.Service) notification.Translator {
	return &serviceGroupTranslator{settings: settings, llm: llm}
}

func (t *serviceGroupTranslator) TranslateFields(ctx context.Context, from, to string, fields notification.LocaleCopy) (notification.LocaleCopy, error) {
	if t == nil || t.llm == nil {
		return notification.LocaleCopy{}, fmt.Errorf("system free group translator is not configured")
	}
	groupID, err := LoadSystemFreeGroupID(ctx, t.settings)
	if err != nil {
		return notification.LocaleCopy{}, err
	}
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return notification.LocaleCopy{}, fmt.Errorf("system free group is not configured")
	}
	reply, err := t.llm.CompleteServiceGroupChat(ctx, groupID, []llmservice.ChatTurn{
		{Role: "system", Content: notification.TranslationSystemPrompt},
		{Role: "user", Content: notification.TranslationUserPrompt(from, to, fields)},
	}, 3072)
	if err != nil {
		return notification.LocaleCopy{}, err
	}
	return notification.ParseTranslatedLocale(reply)
}
