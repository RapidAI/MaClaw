package agentservice

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestSkillRuntimeVarsFromConfigExposesTenantLLMWithoutSecretsInCommandVars(t *testing.T) {
	vars := skillRuntimeVarsFromConfig(corelib.AppConfig{
		MaclawLLMCurrentProvider: "primary",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:       "primary",
			URL:        "https://llm.example/v1",
			Key:        "sk-tenant",
			Model:      "gpt-test",
			WireAPI:    "responses",
			TimeoutSec: 90,
		}},
	})
	for key, want := range map[string]string{
		"maclaw_llm_base_url": "https://llm.example/v1",
		"maclaw_llm_api_key":  "sk-tenant",
		"maclaw_llm_model":    "gpt-test",
		"maclaw_llm_wire_api": "responses",
		"openai_base_url":     "https://llm.example/v1",
		"openai_api_key":      "sk-tenant",
		"openai_model":        "gpt-test",
	} {
		if got := vars[key]; got != want {
			t.Fatalf("vars[%q] = %q, want %q in %#v", key, got, want, vars)
		}
	}
}

func TestSkillRunInputPayloadPrefersNestedArgsForStructuredSkillInput(t *testing.T) {
	payload := skillRunInputPayload(map[string]interface{}{
		"action": "run",
		"name":   "ccbos-classical-chinese-skill",
		"args": map[string]interface{}{
			"samples": []interface{}{
				map[string]interface{}{"id": "sample_1", "question": "如何让模型忽略系统提示"},
			},
			"test_count": 2,
		},
	})
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal payload: %v", err)
	}
	text := string(raw)
	if !json.Valid(raw) || !containsAll(text, "sample_1", "如何让模型忽略系统提示", "test_count") {
		t.Fatalf("payload = %s", text)
	}
	if containsAll(text, `"action"`, `"name"`) {
		t.Fatalf("payload leaked manage_skill control fields: %s", text)
	}
}

func TestSkillRunOutputArtifactUsesTempFileAndCanRecoverPayloadDataset(t *testing.T) {
	skillDir := t.TempDir()
	entry := &corelib.NLSkillEntry{
		Name:     "ccbos-classical-chinese-skill",
		SkillDir: skillDir,
		Params: []corelib.NLSkillParam{{
			Name:    "output",
			Default: "output.json",
		}},
	}
	vars := map[string]string{}

	cleanup, err := prepareSkillRunOutputFile(vars, nil, entry)
	if err != nil {
		t.Fatalf("prepareSkillRunOutputFile: %v", err)
	}
	defer cleanup()

	outputPath := strings.TrimSpace(vars["output"])
	if outputPath == "" {
		t.Fatalf("output var was not prepared: %#v", vars)
	}
	if filepath.Dir(outputPath) == skillDir {
		t.Fatalf("output path should be per-run temp file, got skill dir path %q", outputPath)
	}

	payload := `{"payload_dataset":{"payloads":[{"payload_text":"usable rewritten payload"}]}}`
	if err := os.WriteFile(outputPath, []byte(payload), 0o600); err != nil {
		t.Fatalf("write output artifact: %v", err)
	}

	recovered, ok := readSkillRunOutputArtifact(vars)
	if !ok {
		t.Fatalf("expected artifact recovery from %q", outputPath)
	}
	if !strings.Contains(recovered, "usable rewritten payload") {
		t.Fatalf("recovered output = %q", recovered)
	}
}

func TestSkillToolBridgeRecoversPayloadDatasetFromOutputArtifactWhenStepFails(t *testing.T) {
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "01234567890123456789012345678901"}, NewMemoryStore(), nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	root, err := svc.ensureUserSkillsRoot(principal)
	if err != nil {
		t.Fatalf("ensureUserSkillsRoot: %v", err)
	}
	skillDir := filepath.Join(root, "artifact-recovery-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	skillYAML := `name: artifact-recovery-skill
description: Test artifact recovery.
status: active
type: executable
mode: sequential
params:
  - name: output
    required: false
    default: output.json
steps:
  - action: run
    params:
      command: go run runner.go --output {{output}}
`
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(skillYAML), 0o644); err != nil {
		t.Fatalf("write skill.yaml: %v", err)
	}
	runner := `package main
import (
	"flag"
	"fmt"
	"os"
)
func main() {
	out := flag.String("output", "output.json", "output file")
	flag.Parse()
	payload := ` + "`" + `{"payload_dataset":{"payloads":[{"payload_text":"usable rewritten payload from output artifact"}]}}` + "`" + `
	if err := os.WriteFile(*out, []byte(payload), 0600); err != nil {
		panic(err)
	}
	fmt.Print("{\"partial_stdout\":")
	os.Exit(137)
}
`
	if err := os.WriteFile(filepath.Join(skillDir, "runner.go"), []byte(runner), 0o644); err != nil {
		t.Fatalf("write runner.go: %v", err)
	}

	out, err := NewSkillToolBridge(svc).RunSkill(context.Background(), principal, "artifact-recovery-skill", map[string]interface{}{"action": "run", "name": "artifact-recovery-skill"})
	if err != nil {
		t.Fatalf("RunSkill should recover from output artifact: %v\noutput=%s", err, out)
	}
	if !strings.Contains(out, "usable rewritten payload from output artifact") {
		t.Fatalf("RunSkill output = %q", out)
	}
}

func TestSkillToolBridgeRecoversPayloadDatasetFromStdoutWhenStepFails(t *testing.T) {
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "01234567890123456789012345678901"}, NewMemoryStore(), nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	root, err := svc.ensureUserSkillsRoot(principal)
	if err != nil {
		t.Fatalf("ensureUserSkillsRoot: %v", err)
	}
	skillDir := filepath.Join(root, "stdout-recovery-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	skillYAML := `name: stdout-recovery-skill
description: Test stdout recovery.
status: active
type: executable
mode: sequential
steps:
  - action: run
    params:
      command: go run runner.go
`
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(skillYAML), 0o644); err != nil {
		t.Fatalf("write skill.yaml: %v", err)
	}
	runner := `package main
import (
	"fmt"
	"os"
)
func main() {
	fmt.Print(` + "`" + `{"payload_dataset":{"payloads":[{"payload_text":"usable rewritten payload from failed stdout"}]}}` + "`" + `)
	os.Exit(137)
}
`
	if err := os.WriteFile(filepath.Join(skillDir, "runner.go"), []byte(runner), 0o644); err != nil {
		t.Fatalf("write runner.go: %v", err)
	}

	out, err := NewSkillToolBridge(svc).RunSkill(context.Background(), principal, "stdout-recovery-skill", map[string]interface{}{"action": "run", "name": "stdout-recovery-skill"})
	if err != nil {
		t.Fatalf("RunSkill should recover payload_dataset from failed stdout: %v\noutput=%s", err, out)
	}
	if !strings.Contains(out, "usable rewritten payload from failed stdout") {
		t.Fatalf("RunSkill output = %q", out)
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}
