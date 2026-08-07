package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// SkillEvolutionLLMAdapter adapts the GUI's LLM calling to cskill.LLMRepairer
// for use by the EvolutionPipeline's optimizer and promoter.
//
// Unlike a snapshot-based adapter, this reads LLM config lazily via cfgFn
// on each call, so mid-session LLM provider changes are picked up automatically.
type SkillEvolutionLLMAdapter struct {
	cfgFn   func() corelib.MaclawLLMConfig
	client  *http.Client
	timeout time.Duration
}

// NewSkillEvolutionLLMAdapter creates an adapter that reads config lazily.
func NewSkillEvolutionLLMAdapter(cfgFn func() corelib.MaclawLLMConfig) *SkillEvolutionLLMAdapter {
	return &SkillEvolutionLLMAdapter{
		cfgFn:   cfgFn,
		client:  &http.Client{Timeout: 90 * time.Second},
		timeout: 90 * time.Second,
	}
}

// WithTimeout returns a copy of the adapter using a shorter request timeout,
// for auxiliary calls (e.g. skill-recording metadata) where a long stall
// hurts UX and a fast heuristic fallback exists.
func (a *SkillEvolutionLLMAdapter) WithTimeout(d time.Duration) *SkillEvolutionLLMAdapter {
	return &SkillEvolutionLLMAdapter{
		cfgFn:   a.cfgFn,
		client:  &http.Client{Timeout: d},
		timeout: d,
	}
}

func (a *SkillEvolutionLLMAdapter) ChatCall(messages []map[string]string) (string, error) {
	cfg := a.cfgFn()
	ifaces := make([]interface{}, len(messages))
	for i, m := range messages {
		ifaces[i] = m
	}
	ctx := llm.WithRequestTrace(context.Background(), llm.RequestTrace{Caller: "skill-evolution"})
	resp, err := doSimpleLLMRequest(ctx, cfg, ifaces, a.client, a.timeout)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func (a *SkillEvolutionLLMAdapter) IsConfigured() bool {
	if a == nil || a.cfgFn == nil {
		return false
	}
	cfg := a.cfgFn()
	return cfg.URL != "" && (cfg.Key != "" || cfg.Model != "")
}

// Compile-time interface check.
var _ cskill.LLMRepairer = (*SkillEvolutionLLMAdapter)(nil)

// skillExecutorRegistrar implements skill.SkillRegistrar by adding the entry
// to the SkillExecutor's managed skill list.
type skillExecutorRegistrar struct {
	app *App
}

func (r *skillExecutorRegistrar) RegisterSkill(entry *corelib.NLSkillEntry) error {
	if r.app == nil || r.app.skillExecutor == nil || entry == nil {
		return fmt.Errorf("skill executor not available")
	}
	return r.app.skillExecutor.withSkillListMutate(func() error {
		skills := r.app.skillExecutor.loadSkills()
		// Check for duplicates.
		for _, s := range skills {
			if s.Name == entry.Name {
				return fmt.Errorf("skill %q already registered", entry.Name)
			}
		}
		skills = append(skills, *entry)
		return r.app.skillExecutor.saveSkills(skills)
	})
}
