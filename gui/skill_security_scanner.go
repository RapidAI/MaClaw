package main

// skill_security_scanner.go is the GUI-specific adapter for the shared
// security scanner in corelib/skill. It provides the LLMCaller implementation
// that bridges to the GUI's LLM infrastructure.

import (
	"context"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/skill"
)

// ── GUI LLM caller ─────────────────────────────────────────────────────

// skillScanLLMCaller implements skill.LLMCaller using the GUI's LLM infrastructure.
type skillScanLLMCaller struct {
	app        *App
	httpClient *http.Client
}

func (c *skillScanLLMCaller) Available() bool {
	if c.app == nil {
		return false
	}
	cfg := c.app.GetMaclawLLMConfig()
	return cfg.URL != "" && cfg.Key != "" && cfg.Model != ""
}

func (c *skillScanLLMCaller) Call(ctx context.Context, prompt string) (string, error) {
	cfg := c.app.GetMaclawLLMConfig()
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": prompt},
	}
	resp, err := doSimpleLLMRequest(ctx, cfg, messages, c.httpClient, 30*time.Second)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// ── Factory ─────────────────────────────────────────────────────────────

// NewSkillSecurityScanner creates a SecurityScanner wired to the GUI's LLM.
// Returns the shared corelib scanner — all logic lives there.
func NewSkillSecurityScanner(app *App, httpClient *http.Client) *skill.SecurityScanner {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 35 * time.Second}
	}
	var llm skill.LLMCaller
	if app != nil {
		llm = &skillScanLLMCaller{app: app, httpClient: httpClient}
	}
	return skill.NewSecurityScanner(llm)
}

// ── Re-export for call sites that use the old name ──────────────────────

// FormatScanReportForUser delegates to the shared implementation.
func FormatScanReportForUser(report *skill.ScanReport, skillName string) string {
	return skill.FormatScanReportForUser(report, skillName)
}

// Ensure guiLLMCaller satisfies the interface at compile time.
var _ skill.LLMCaller = (*skillScanLLMCaller)(nil)
