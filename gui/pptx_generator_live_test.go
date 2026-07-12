package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestLivePPTXGeneratorSkillRunner(t *testing.T) {
	if os.Getenv("RUN_PPTX_LIVE_TEST") != "1" {
		t.Skip("set RUN_PPTX_LIVE_TEST=1 to run live pptx-generator verification")
	}

	skillRoot := "C:/Users/ma139/.maclaw/data/skills"
	skillDir := filepath.Join(skillRoot, "pptx-generator")
	if _, err := os.Stat(skillDir); err != nil {
		t.Skipf("pptx-generator skill not available: %v", err)
	}

	apiKey := firstNonEmptyEnv("MACLAW_LIVE_API_KEY", "DEEPSEEK_API_KEY", "OPENAI_API_KEY")
	apiURL := firstNonEmptyEnv("MACLAW_LIVE_API_URL", "DEEPSEEK_API_URL")
	if strings.TrimSpace(apiURL) == "" {
		apiURL = "https://api.deepseek.com/v1"
	}
	model := firstNonEmptyEnv("MACLAW_LIVE_MODEL", "DEEPSEEK_MODEL")
	if strings.TrimSpace(model) == "" {
		model = "deepseek-chat"
	}
	if strings.TrimSpace(apiKey) == "" {
		t.Skip("MACLAW_LIVE_API_KEY / DEEPSEEK_API_KEY / OPENAI_API_KEY is required for live pptx verification")
	}

	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	outputDir := filepath.Join(tempHome, "pptx-live-output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(outputDir) error = %v", err)
	}
	before, err := filepath.Glob(filepath.Join(outputDir, "*.pptx"))
	if err != nil {
		t.Fatalf("Glob(before) error = %v", err)
	}

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.ExternalSkillDirs = []string{skillRoot}
	cfg.MaclawLLMProviders = []corelib.MaclawLLMProvider{{
		Name:       "Custom1",
		URL:        strings.TrimRight(strings.TrimSpace(apiURL), "/"),
		Key:        strings.TrimSpace(apiKey),
		Model:      strings.TrimSpace(model),
		Protocol:   "openai",
		AgentType:  "codex",
		IsCustom:   true,
		TimeoutSec: 300,
	}}
	cfg.MaclawLLMCurrentProvider = "Custom1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.skillRunner = NewSkillRunner(app.skillExecutor)
	app.ensureInteractionInfra()

	runArgs := map[string]interface{}{
		"args": map[string]interface{}{
			"output": filepath.Join(outputDir, "live_test_deck.pptx"),
			"topic":  "Quarterly product review",
		},
	}
	runID, err := app.skillRunner.StartRun("pptx-generator", runArgs)
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	var status *SkillRunStatus
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		status, err = app.skillRunner.GetRunStatus(runID)
		if err != nil {
			t.Fatalf("GetRunStatus() error = %v", err)
		}
		if status != nil && status.Status != "running" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if status == nil {
		t.Fatalf("expected status for run %s", runID)
	}
	if status.Status == "running" {
		t.Fatalf("run %s still running after timeout", runID)
	}

	after, err := filepath.Glob(filepath.Join(outputDir, "*.pptx"))
	if err != nil {
		t.Fatalf("Glob(after) error = %v", err)
	}
	newFiles := diffStringSets(after, before)
	if len(status.Steps) == 0 {
		t.Fatalf("expected step output, got %#v", status)
	}
	stepOutput := status.Steps[0].Output
	if !strings.Contains(stepOutput, "脚本路径:") {
		t.Fatalf("expected craft tool script path in output, got %s", stepOutput)
	}
	generatedPaths := append([]string{}, newFiles...)
	if len(generatedPaths) == 0 {
		if generatedPath := extractGeneratedPath(stepOutput); generatedPath != "" {
			generatedPaths = append(generatedPaths, generatedPath)
		}
	}
	if len(generatedPaths) == 0 {
		t.Fatalf("pptx-generator did not create .pptx; run=%s status=%s output=%s", runID, status.Status, stepOutput)
	}
	for _, path := range generatedPaths {
		assertGeneratedPPTX(t, path)
	}
}

func assertGeneratedPPTX(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	if info.Size() <= 0 {
		t.Fatalf("generated pptx is empty: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open(%s) error = %v", path, err)
	}
	defer f.Close()
	head := make([]byte, 2)
	_, _ = f.Read(head)
	if string(head) != "PK" {
		t.Fatalf("generated file does not look like pptx zip: %s", path)
	}
}

func extractGeneratedPath(stepOutput string) string {
	for _, line := range strings.Split(stepOutput, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Generated:") {
			continue
		}
		path := strings.TrimSpace(strings.TrimPrefix(line, "Generated:"))
		if strings.EqualFold(filepath.Ext(path), ".pptx") {
			return path
		}
	}
	return ""
}

func diffStringSets(after, before []string) []string {
	seen := make(map[string]struct{}, len(before))
	for _, item := range before {
		seen[item] = struct{}{}
	}
	var diff []string
	for _, item := range after {
		if _, ok := seen[item]; ok {
			continue
		}
		diff = append(diff, item)
	}
	return diff
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}
