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
	cfgFn  func() corelib.MaclawLLMConfig
	client *http.Client
}

// NewSkillEvolutionLLMAdapter creates an adapter that reads config lazily.
func NewSkillEvolutionLLMAdapter(cfgFn func() corelib.MaclawLLMConfig) *SkillEvolutionLLMAdapter {
	return &SkillEvolutionLLMAdapter{
		cfgFn:  cfgFn,
		client: &http.Client{Timeout: 90 * time.Second},
	}
}

func (a *SkillEvolutionLLMAdapter) ChatCall(messages []map[string]string) (string, error) {
	cfg := a.cfgFn()
	ifaces := make([]interface{}, len(messages))
	for i, m := range messages {
		ifaces[i] = m
	}
	ctx := llm.WithRequestTrace(context.Background(), llm.RequestTrace{Caller: "skill-evolution"})
	resp, err := doSimpleLLMRequest(ctx, cfg, ifaces, a.client, 90*time.Second)
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
	r.app.skillExecutor.mu.Lock()
	defer r.app.skillExecutor.mu.Unlock()

	skills := r.app.skillExecutor.loadSkills()
	// Check for duplicates.
	for _, s := range skills {
		if s.Name == entry.Name {
			return fmt.Errorf("skill %q already registered", entry.Name)
		}
	}
	skills = append(skills, *entry)
	return r.app.skillExecutor.saveSkills(skills)
}
