package commands

import (
	"log"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

// tuiRepairHTTPClient is a shared HTTP client for skill repair LLM calls.
var tuiRepairHTTPClient = &http.Client{Timeout: 60 * time.Second}

// tuiSkillRepairer adapts the TUI's LLM calling to skill.LLMRepairer.
type tuiSkillRepairer struct {
	cfg corelib.MaclawLLMConfig
}

func (r *tuiSkillRepairer) ChatCall(messages []map[string]string) (string, error) {
	ifaces := make([]interface{}, len(messages))
	for i, m := range messages {
		ifaces[i] = m
	}
	resp, err := agent.DoSimpleLLMRequest(r.cfg, ifaces, tuiRepairHTTPClient, 60*time.Second)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func (r *tuiSkillRepairer) IsConfigured() bool {
	return r.cfg.URL != "" && r.cfg.Key != ""
}

// ConfigStore abstracts config load/save for skill repair.
type ConfigStore interface {
	LoadConfig() (corelib.AppConfig, error)
	SaveConfig(cfg corelib.AppConfig) error
}

// maybeRepairSkillTUI checks if a skill is eligible for self-repair and
// attempts an LLM-driven repair. Runs in the background to avoid blocking.
func maybeRepairSkillTUI(entry *corelib.NLSkillEntry, cfg corelib.AppConfig, store ConfigStore) {
	if !skill.ShouldAttemptRepair(entry) {
		return
	}

	// Find the LLM config from the app config.
	llmCfg := corelib.MaclawLLMConfig{}
	if len(cfg.MaclawLLMProviders) > 0 {
		p := cfg.MaclawLLMProviders[0]
		llmCfg = corelib.MaclawLLMConfig{
			URL:   p.URL,
			Key:   p.Key,
			Model: p.Model,
		}
	}

	repairer := &tuiSkillRepairer{cfg: llmCfg}
	if !repairer.IsConfigured() {
		return
	}

	go func() {
		log.Printf("[skill-repair-tui] attempting repair for %q", entry.Name)
		result, err := skill.AttemptRepair(repairer, entry)
		if err != nil {
			log.Printf("[skill-repair-tui] repair failed for %q: %v", entry.Name, err)
			return
		}
		if skill.ApplyRepair(entry, result) {
			// Reload config and write back.
			freshCfg, err := store.LoadConfig()
			if err != nil {
				log.Printf("[skill-repair-tui] cannot reload config: %v", err)
				return
			}
			for i, s := range freshCfg.NLSkills {
				if s.Name == entry.Name {
					freshCfg.NLSkills[i].Steps = entry.Steps
					freshCfg.NLSkills[i].Status = entry.Status
					freshCfg.NLSkills[i].LastError = entry.LastError
					break
				}
			}
			_ = store.SaveConfig(freshCfg)
			log.Printf("[skill-repair-tui] repaired skill %q", entry.Name)
		}
	}()
}
