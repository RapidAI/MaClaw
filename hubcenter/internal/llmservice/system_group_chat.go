package llmservice

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

// ChatTurn is one message in a server-side completion. Moderation and
// notification translation use this instead of a separately stored endpoint.
type ChatTurn struct {
	Role    string
	Content string
}

// CompleteServiceGroupChat sends a non-streaming chat through one configured
// service group. It tries that group's providers in order and does not bill
// a tenant: the caller is HubCenter itself.
func (s *Service) CompleteServiceGroupChat(ctx context.Context, groupID string, messages []ChatTurn, maxTokens int) (string, error) {
	if s == nil {
		return "", fmt.Errorf("llm service is required")
	}
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return "", fmt.Errorf("service group is required")
	}
	if len(messages) == 0 {
		return "", fmt.Errorf("messages are required")
	}
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return "", err
	}
	group := findServiceGroup(reg, groupID)
	if group == nil {
		return "", fmt.Errorf("service group %s was not found", groupID)
	}
	targets := serviceGroupChatTargets(reg, group)
	if len(targets) == 0 {
		return "", fmt.Errorf("service group %s has no provider", groupID)
	}
	bodyMessages := make([]any, 0, len(messages))
	for _, message := range messages {
		role := strings.TrimSpace(message.Role)
		if role == "" {
			role = "user"
		}
		bodyMessages = append(bodyMessages, map[string]string{
			"role":    role,
			"content": message.Content,
		})
	}
	client := &http.Client{Timeout: 45 * time.Second}
	var lastErr error
	for _, target := range targets {
		body := map[string]any{
			"model":       target.model,
			"messages":    bodyMessages,
			"temperature": 0,
		}
		if maxTokens > 0 {
			body["max_tokens"] = maxTokens
		}
		resp, err := forwardToProviderAttempts(ctx, client, target.provider, body, target.model, target.model, false)
		if err != nil {
			lastErr = err
			continue
		}
		if resp == nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
			status := 0
			snippet := ""
			if resp != nil {
				status = resp.StatusCode
				snippet = strings.TrimSpace(string(resp.Body))
				if len(snippet) > 180 {
					snippet = snippet[:180]
				}
			}
			lastErr = fmt.Errorf("provider %s HTTP %d %s", target.provider.ID, status, snippet)
			continue
		}
		reply, errMsg, _ := hopTestReply(resp.Body)
		if errMsg != "" {
			lastErr = fmt.Errorf("provider %s: %s", target.provider.ID, errMsg)
			continue
		}
		reply = strings.TrimSpace(reply)
		if reply == "" {
			lastErr = fmt.Errorf("provider %s returned an empty reply", target.provider.ID)
			continue
		}
		return reply, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("service group %s did not answer", groupID)
	}
	return "", lastErr
}

type serviceGroupChatTarget struct {
	provider *llmpool.ProviderConfig
	model    string
}

func findServiceGroup(reg *Registry, id string) *llmpool.ServiceGroup {
	if reg == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	for i := range reg.ServiceGroups {
		if strings.EqualFold(strings.TrimSpace(reg.ServiceGroups[i].ID), id) {
			return &reg.ServiceGroups[i]
		}
	}
	return nil
}

func serviceGroupChatTargets(reg *Registry, group *llmpool.ServiceGroup) []serviceGroupChatTarget {
	if reg == nil || group == nil {
		return nil
	}
	var targets []serviceGroupChatTarget
	seen := map[string]struct{}{}
	add := func(provider *llmpool.ProviderConfig, model string) {
		if provider == nil || provider.Paused {
			return
		}
		if strings.TrimSpace(provider.APIURL) == "" {
			return
		}
		model = strings.TrimSpace(model)
		if model == "" && len(provider.Models) > 0 {
			model = strings.TrimSpace(provider.Models[0])
		}
		if model == "" {
			return
		}
		key := strings.ToLower(strings.TrimSpace(provider.ID)) + "\x00" + strings.ToLower(model)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		targets = append(targets, serviceGroupChatTarget{provider: provider, model: model})
	}
	for _, model := range group.Models {
		logical := strings.TrimSpace(model.Name)
		type route struct {
			id    string
			model string
		}
		var routes []route
		for _, id := range model.ProviderIDs {
			routes = append(routes, route{id: id})
		}
		for _, cfg := range model.ProviderConfigs {
			routes = append(routes, route{id: cfg.ProviderID, model: cfg.Model})
		}
		for _, route := range routes {
			id := strings.TrimSpace(route.id)
			if id == "" {
				continue
			}
			modelName := firstChatModel(route.model, logical)
			if provider := findProvider(reg, id); provider != nil {
				add(provider, modelName)
				continue
			}
			array := findProviderArray(reg, id)
			if array == nil {
				continue
			}
			for _, memberID := range array.MemberIDs {
				if provider := findProvider(reg, memberID); provider != nil {
					add(provider, modelName)
				}
			}
		}
	}
	return targets
}

func firstChatModel(override, logical string) string {
	override = strings.TrimSpace(override)
	if override != "" && !strings.EqualFold(override, "auto") {
		return override
	}
	logical = strings.TrimSpace(logical)
	if logical != "" && !strings.EqualFold(logical, "auto") {
		return logical
	}
	if override != "" {
		return override
	}
	return logical
}
