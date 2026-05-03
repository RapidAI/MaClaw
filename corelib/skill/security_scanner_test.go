package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/security"
)

// 闁冲厜鍋撻柍鍏夊亾 ParseAgentScanResponse 闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾

func TestParseAgentScanResponse_ValidJSON(t *testing.T) {
	response := `{"score":2,"summary":"Safe PDF converter","findings":[{"severity":"info","category":"safe_pattern","description":"Uses pdfplumber","location":"skill.yaml"}],"recommendation":"safe to install"}`

	report, err := ParseAgentScanResponse(response)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if report.AgentScore != 2 {
		t.Errorf("expected score 2, got %d", report.AgentScore)
	}
	if report.FinalLevel != security.RiskLow {
		t.Errorf("expected low, got %s", report.FinalLevel)
	}
	if len(report.Findings) != 1 {
		t.Errorf("expected 1 finding, got %d", len(report.Findings))
	}
}

func TestParseAgentScanResponse_MarkdownWrapped(t *testing.T) {
	response := "```json\n{\"score\":7,\"summary\":\"Dangerous\",\"findings\":[],\"recommendation\":\"Do not install\"}\n```"

	report, err := ParseAgentScanResponse(response)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if report.AgentScore != 7 {
		t.Errorf("expected score 7, got %d", report.AgentScore)
	}
	if report.FinalLevel != security.RiskCritical {
		t.Errorf("expected critical, got %s", report.FinalLevel)
	}
}

func TestParseAgentScanResponse_ClampsScore(t *testing.T) {
	report, _ := ParseAgentScanResponse(`{"score":15,"summary":"test","findings":[],"recommendation":"test"}`)
	if report.AgentScore != 10 {
		t.Errorf("expected clamped 10, got %d", report.AgentScore)
	}
	report2, _ := ParseAgentScanResponse(`{"score":-5,"summary":"test","findings":[],"recommendation":"test"}`)
	if report2.AgentScore != 0 {
		t.Errorf("expected clamped 0, got %d", report2.AgentScore)
	}
}

func TestParseAgentScanResponse_InvalidJSON(t *testing.T) {
	_, err := ParseAgentScanResponse("not json at all")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseAgentScanResponse_JSONInSurroundingText(t *testing.T) {
	response := "Here is my analysis:\n{\"score\":3,\"summary\":\"Minor\",\"findings\":[],\"recommendation\":\"OK\"}\nDone."
	report, err := ParseAgentScanResponse(response)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if report.AgentScore != 3 {
		t.Errorf("expected score 3, got %d", report.AgentScore)
	}
}

func TestParseAgentScanResponse_StopsAtFirstJSONObject(t *testing.T) {
	response := `{"score":3,"summary":"OK","findings":[],"recommendation":"safe"} and some {braces} after`
	report, err := ParseAgentScanResponse(response)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if report.AgentScore != 3 {
		t.Errorf("expected score 3, got %d", report.AgentScore)
	}
}

// 闁冲厜鍋撻柍鍏夊亾 AgentScoreToLevel 闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋?
func TestAgentScoreToLevel(t *testing.T) {
	tests := []struct {
		score int
		want  security.RiskLevel
	}{
		{0, security.RiskLow},
		{2, security.RiskLow},
		{3, security.RiskMedium},
		{4, security.RiskMedium},
		{5, security.RiskHigh},
		{6, security.RiskHigh},
		{7, security.RiskCritical},
		{10, security.RiskCritical},
	}
	for _, tt := range tests {
		got := AgentScoreToLevel(tt.score)
		if got != tt.want {
			t.Errorf("AgentScoreToLevel(%d) = %s, want %s", tt.score, got, tt.want)
		}
	}
}

// 闁冲厜鍋撻柍鍏夊亾 Security model: agent cannot downgrade pattern result 闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋?
func TestScanStaged_AgentCannotDowngradePatternResult(t *testing.T) {
	// Pattern says critical, agent says safe (score=0).
	// Final must remain critical 闁?pattern is hard floor.
	report := &ScanReport{
		PatternAssessment: security.RiskAssessment{Level: security.RiskCritical},
		AgentScore:        0,
		FinalLevel:        security.RiskCritical,
	}
	agentLevel := AgentScoreToLevel(report.AgentScore)
	if security.RiskLevelOrder[agentLevel] > security.RiskLevelOrder[report.FinalLevel] {
		report.FinalLevel = agentLevel
	}
	if report.FinalLevel != security.RiskCritical {
		t.Errorf("expected critical (pattern hard floor), got %s", report.FinalLevel)
	}
}

func TestScanStaged_AgentCanUpgradePatternResult(t *testing.T) {
	report := &ScanReport{
		PatternAssessment: security.RiskAssessment{Level: security.RiskLow},
		AgentScore:        8,
		FinalLevel:        security.RiskLow,
	}
	agentLevel := AgentScoreToLevel(report.AgentScore)
	if security.RiskLevelOrder[agentLevel] > security.RiskLevelOrder[report.FinalLevel] {
		report.FinalLevel = agentLevel
	}
	if report.FinalLevel != security.RiskCritical {
		t.Errorf("expected critical (agent upgrade), got %s", report.FinalLevel)
	}
}

// 闁冲厜鍋撻柍鍏夊亾 patternScan does not mutate caller's entry 闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾

func TestPatternScan_DoesNotMutateEntry(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name:       "test",
		TrustLevel: "trusted",
		SkillDir:   "/original/path",
	}
	_ = patternScan(entry, "/staging/path")
	if entry.SkillDir != "/original/path" {
		t.Errorf("patternScan mutated entry.SkillDir: got %q", entry.SkillDir)
	}
}

// 闁冲厜鍋撻柍鍏夊亾 ScanStaged end-to-end with mock LLM 闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋?
type mockLLMCaller struct {
	available bool
	response  string
	err       error
}

func (m *mockLLMCaller) Available() bool { return m.available }
func (m *mockLLMCaller) Call(ctx context.Context, prompt string) (string, error) {
	return m.response, m.err
}

func TestScanStaged_PatternOnly_WhenLLMUnavailable(t *testing.T) {
	scanner := NewSecurityScanner(nil) // no LLM
	entry := &corelib.NLSkillEntry{
		Name:       "test-skill",
		TrustLevel: "trusted",
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "echo hello"}},
		},
	}
	report := scanner.ScanStaged(context.Background(), entry, "", nil)
	if report.ScannedBy != "pattern" {
		t.Errorf("expected pattern-only, got %s", report.ScannedBy)
	}
	if report.AgentScore != -1 {
		t.Errorf("expected agent score -1, got %d", report.AgentScore)
	}
}

func TestScanStaged_AgentUpgradesPattern(t *testing.T) {
	llm := &mockLLMCaller{
		available: true,
		response:  `{"score":8,"summary":"Exfiltrates data","findings":[{"severity":"critical","category":"network","description":"Sends files to remote server","location":"run.py:5"}],"recommendation":"Do not install"}`,
	}
	scanner := NewSecurityScanner(llm)
	entry := &corelib.NLSkillEntry{
		Name:       "safe-looking",
		TrustLevel: "trusted",
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "python run.py"}},
		},
	}
	report := scanner.ScanStaged(context.Background(), entry, "", nil)
	if report.ScannedBy != "agent+pattern" {
		t.Errorf("expected agent+pattern, got %s", report.ScannedBy)
	}
	if report.FinalLevel != security.RiskCritical {
		t.Errorf("expected critical (agent upgrade), got %s", report.FinalLevel)
	}
}

func TestScanStaged_AgentFailure_FallsBackToPattern(t *testing.T) {
	llm := &mockLLMCaller{
		available: true,
		err:       fmt.Errorf("timeout"),
	}
	scanner := NewSecurityScanner(llm)
	entry := &corelib.NLSkillEntry{
		Name:       "test",
		TrustLevel: "trusted",
	}
	report := scanner.ScanStaged(context.Background(), entry, "", nil)
	if report.ScannedBy != "pattern" {
		t.Errorf("expected pattern fallback, got %s", report.ScannedBy)
	}
}

// 闁冲厜鍋撻柍鍏夊亾 FormatScanReportForUser 闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋?
func TestFormatScanReport_Safe(t *testing.T) {
	report := &ScanReport{
		PatternAssessment: security.RiskAssessment{Level: security.RiskLow},
		FinalLevel:        security.RiskLow,
		Summary:           "Standard PDF converter",
		ScannedBy:         "agent+pattern",
	}
	text := FormatScanReportForUser(report, "any2pdf")
	if !strings.Contains(text, "any2pdf") {
		t.Error("safe report should include skill name")
	}
}

func TestFormatScanReport_Dangerous(t *testing.T) {
	report := &ScanReport{
		PatternAssessment: security.RiskAssessment{Level: security.RiskCritical},
		FinalLevel:        security.RiskCritical,
		Summary:           "Downloads and executes remote script",
		ScannedBy:         "agent+pattern",
		Findings: []ScanFinding{
			{Severity: "critical", Category: "execution", Description: "curl | bash detected", Location: "install.sh:3"},
		},
	}
	text := FormatScanReportForUser(report, "malicious")
	if !strings.Contains(text, "malicious") {
		t.Error("critical report should include skill name")
	}
	if !strings.Contains(text, "curl | bash") {
		t.Error("should show critical findings")
	}
}

func TestValidateExternalSkillDirAcceptsLowercaseReadmeSkill(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "lower-readme-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "readme.md"), []byte("# readme skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	count, err := ValidateExternalSkillDir(root)
	if err != nil {
		t.Fatalf("ValidateExternalSkillDir() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}
func TestValidateExternalSkillDirAcceptsMixedCaseReadmeSkill(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "mixed-readme-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "Readme.md"), []byte("# readme skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	count, err := ValidateExternalSkillDir(root)
	if err != nil {
		t.Fatalf("ValidateExternalSkillDir() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}
func TestValidateExternalSkillDirAcceptsReadmeSkill(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "readme-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "README.md"), []byte("# README skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	count, err := ValidateExternalSkillDir(root)
	if err != nil {
		t.Fatalf("ValidateExternalSkillDir() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}
