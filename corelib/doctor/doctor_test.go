package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestRunReportsPrimaryLLMBlocker(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	report := Run(Input{
		Config:     corelib.AppConfig{},
		ConfigPath: cfgPath,
		BaseDir:    dir,
		Now:        time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC),
	})
	if report.OK {
		t.Fatalf("expected not ok, got ok summary=%q", report.Summary)
	}
	if report.Blockers < 1 {
		t.Fatalf("expected blockers, got %d checks=%+v", report.Blockers, report.Checks)
	}
	if !hasCheck(report, "llm.primary", StatusFail) {
		t.Fatalf("missing llm.primary fail: %+v", report.Checks)
	}
	if !hasCheck(report, "config.file", StatusOK) {
		t.Fatalf("expected config.file ok: %+v", report.Checks)
	}
	if !strings.Contains(report.Summary, "blocker") {
		t.Fatalf("summary=%q", report.Summary)
	}
}

func TestRunWarnsWhenRoutingCannotDivert(t *testing.T) {
	dir := t.TempDir()
	report := Run(Input{
		Config: corelib.AppConfig{
			MaclawLLMUrl:   "http://x",
			MaclawLLMModel: "m",
			MaclawLLMKey:   "k",
			OnboardingDone: true,
		},
		BaseDir: dir,
	})
	if !hasCheck(report, "llm.routing", StatusWarn) {
		t.Fatalf("expected llm.routing warn: %+v", report.Checks)
	}
}

func TestRunReadyWhenLLMConfigured(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"maclaw_llm_url":"http://127.0.0.1:1","maclaw_llm_model":"m"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	report := Run(Input{
		Config: corelib.AppConfig{
			MaclawLLMUrl:   "http://127.0.0.1:1/v1",
			MaclawLLMModel: "test-model",
			MaclawLLMKey:   "sk-test",
			OnboardingDone: true,
			AuxiliaryLLM: corelib.AuxiliaryLLMConfig{
				URL:   "http://127.0.0.1:2/v1",
				Key:   "sk-aux",
				Model: "flash",
			},
			ModelRoutes: map[string]corelib.ModelRouteConfig{
				"intent": {Model: "flash"},
			},
		},
		ConfigPath: cfgPath,
		BaseDir:    dir,
		Now:        time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC),
	})
	if !report.OK {
		t.Fatalf("expected ok, summary=%q checks=%+v", report.Summary, report.Checks)
	}
	if !hasCheck(report, "llm.primary", StatusOK) {
		t.Fatalf("llm.primary: %+v", report.Checks)
	}
	if !hasCheck(report, "llm.aux", StatusOK) {
		t.Fatalf("llm.aux: %+v", report.Checks)
	}
	if !hasCheck(report, "llm.model_routes", StatusOK) {
		t.Fatalf("llm.model_routes: %+v", report.Checks)
	}
	if !hasCheck(report, "paths.home", StatusOK) {
		t.Fatalf("paths.home: %+v", report.Checks)
	}
}

func TestRunMergesExtraChecks(t *testing.T) {
	dir := t.TempDir()
	report := Run(Input{
		Config: corelib.AppConfig{
			MaclawLLMUrl:   "http://x",
			MaclawLLMModel: "m",
			MaclawLLMKey:   "k",
			OnboardingDone: true,
		},
		BaseDir: dir,
		ExtraChecks: []Check{
			{ID: "gateway.health", Status: StatusFail, Message: "connection refused", Hint: "start GUI gateway"},
		},
	})
	if report.OK {
		t.Fatal("expected gateway health to fail overall ok")
	}
	if !hasCheck(report, "gateway.health", StatusFail) {
		t.Fatalf("missing gateway.health: %+v", report.Checks)
	}
}

func TestRedactURLHost(t *testing.T) {
	got := redactURLHost("https://user:pass@api.example.com:8443/v1/chat?x=1")
	if got != "https://api.example.com:8443" {
		t.Fatalf("got %q", got)
	}
}

func hasCheck(r Report, id string, status Status) bool {
	for _, c := range r.Checks {
		if c.ID == id && c.Status == status {
			return true
		}
	}
	return false
}
