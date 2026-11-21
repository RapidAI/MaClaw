package httpapi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/notification"
)

// systemFreeTranslator translates an admin notice with the Hub system-free
// group. Cascade notices never call it: they already carry locales.
type systemFreeTranslator struct{}

func (systemFreeTranslator) TranslateFields(ctx context.Context, from, to string, fields notification.LocaleCopy) (notification.LocaleCopy, error) {
	resolver := im.DefaultSystemLLMResolver()
	if resolver == nil {
		return notification.LocaleCopy{}, fmt.Errorf("system-free LLM is not configured")
	}
	messages := []interface{}{
		map[string]string{"role": "system", "content": notification.TranslationSystemPrompt},
		map[string]string{"role": "user", "content": notification.TranslationUserPrompt(from, to, fields)},
	}
	resp, err := resolver.Call(ctx, messages, 45*time.Second)
	if err != nil {
		return notification.LocaleCopy{}, err
	}
	if resp == nil || strings.TrimSpace(resp.Content) == "" {
		return notification.LocaleCopy{}, fmt.Errorf("system-free LLM returned an empty translation")
	}
	return notification.ParseTranslatedLocale(resp.Content)
}
