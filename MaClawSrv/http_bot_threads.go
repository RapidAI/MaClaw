package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// GET /api/v1/instances/{instanceId}/bot-threads?bot_id=…: the owner's
// registry of one bot's tasks (main conversation and per-run threads),
// bounded to 7 days and 50 rows; max_age_hours, limit, exclude_running and
// threads_only narrow it further.
func (s *HTTPServer) handleListBotThreads(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	q := agentservice.BotThreadQuery{}
	if v := strings.TrimSpace(r.URL.Query().Get("max_age_hours")); v != "" {
		hours, err := strconv.Atoi(v)
		if err != nil || hours <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "max_age_hours must be a positive integer"})
			return
		}
		q.MaxAge = time.Duration(hours) * time.Hour
	}
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be a positive integer"})
			return
		}
		q.Limit = n
	}
	q.ExcludeRunning = r.URL.Query().Get("exclude_running") == "true" || r.URL.Query().Get("exclude_running") == "1"
	q.ThreadsOnly = r.URL.Query().Get("threads_only") == "true" || r.URL.Query().Get("threads_only") == "1"
	items, err := s.svc.ListBotThreads(p, r.PathValue("instanceId"), r.URL.Query().Get("bot_id"), q)
	if errors.Is(err, agentservice.ErrBotThreadsBadBot) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	if items == nil {
		items = []agentservice.BotThread{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// POST /api/v1/instances/{instanceId}/bot-threads/route: which of the bot's
// conversations a message with no explicit hint belongs to. Hub calls it
// before admitting the message; clients only show the answer.
func (s *HTTPServer) handleRouteBotMessage(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.BotRouteInput
	if !decodeJSON(w, r, &in) {
		return
	}
	route, err := s.svc.RouteBotMessage(r.Context(), p, r.PathValue("instanceId"), in, s.botRouteClassifier(p))
	if errors.Is(err, agentservice.ErrBotThreadsBadBot) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, route)
}

// POST /api/v1/instances/{instanceId}/bot-intake: what a message does
// (new task, supplement, stop, pause, continue, reply, ask, schedule,
// status). Hub applies the result. On timeout the body is a new task.
func (s *HTTPServer) handleBotIntake(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.BotIntakeInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.IntakeBotMessage(r.Context(), p, r.PathValue("instanceId"), in, s.botIntakeClassifier(p))
	if errors.Is(err, agentservice.ErrBotThreadsBadBot) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	persistDesktopBotSchedule(p, r.PathValue("instanceId"), in, out)
	writeJSON(w, http.StatusOK, out)
}

// botIntakeClassifierOverride lets tests answer for the model.
var botIntakeClassifierOverride agentservice.BotIntakeClassifier

// botRouteClassifierOverride lets tests answer for the model.
var botRouteClassifierOverride agentservice.BotRouteClassifier

func (s *HTTPServer) botIntakeClassifier(p agentservice.Principal) agentservice.BotIntakeClassifier {
	if botIntakeClassifierOverride != nil {
		return botIntakeClassifierOverride
	}
	return func(ctx context.Context, system, user string) (string, error) {
		cfg, err := s.svc.DemoSkillLLMConfig(p)
		if err != nil {
			return "", err
		}
		cfg = srvIntentLLMConfig(cfg)
		if cfg.MaxOutputTokens == 0 || cfg.MaxOutputTokens > 500 {
			cfg.MaxOutputTokens = 500
		}
		messages := []interface{}{
			map[string]string{"role": "system", "content": system},
			map[string]string{"role": "user", "content": user},
		}
		client := &http.Client{Timeout: agentservice.BotIntakeBudget + time.Second}
		resp, err := agent.DoSimpleLLMRequestContext(ctx, cfg, messages, client, agentservice.BotIntakeBudget)
		if err != nil {
			return "", err
		}
		return resp.Content, nil
	}
}

// botRouteClassifier asks the owner's bot model (the bot run path's config,
// with the lightweight hints and thinking off, as the intent classifier
// does) within the routing budget.
func (s *HTTPServer) botRouteClassifier(p agentservice.Principal) agentservice.BotRouteClassifier {
	if botRouteClassifierOverride != nil {
		return botRouteClassifierOverride
	}
	return func(ctx context.Context, system, user string) (string, error) {
		cfg, err := s.svc.DemoSkillLLMConfig(p)
		if err != nil {
			return "", err
		}
		cfg = srvIntentLLMConfig(cfg)
		if cfg.MaxOutputTokens == 0 || cfg.MaxOutputTokens > 200 {
			cfg.MaxOutputTokens = 200
		}
		messages := []interface{}{
			map[string]string{"role": "system", "content": system},
			map[string]string{"role": "user", "content": user},
		}
		client := &http.Client{Timeout: agentservice.BotRouteBudget + time.Second}
		resp, err := agent.DoSimpleLLMRequestContext(ctx, cfg, messages, client, agentservice.BotRouteBudget)
		if err != nil {
			return "", err
		}
		return resp.Content, nil
	}
}
