package experience

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

type fakeLLM struct {
	content string
	calls   int
}

func (f *fakeLLM) Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	f.calls++
	return f.content, nil
}

type fakeStore struct {
	skills      []corelib.NLSkillEntry
	registered  []corelib.NLSkillEntry
	updated     []corelib.NLSkillEntry
	registerErr error
	updateErr   error
}

func (s *fakeStore) List() []corelib.NLSkillEntry {
	return append([]corelib.NLSkillEntry(nil), s.skills...)
}

func (s *fakeStore) Register(entry corelib.NLSkillEntry) error {
	if s.registerErr != nil {
		return s.registerErr
	}
	s.registered = append(s.registered, entry)
	s.skills = append(s.skills, entry)
	return nil
}

func (s *fakeStore) Update(entry corelib.NLSkillEntry) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	s.updated = append(s.updated, entry)
	for i := range s.skills {
		if s.skills[i].Name == entry.Name {
			s.skills[i] = entry
			return nil
		}
	}
	s.skills = append(s.skills, entry)
	return nil
}

func TestExtractorRegistersValidPattern(t *testing.T) {
	llm := &fakeLLM{content: `[{"name":"run-coverage-tests","description":"Run the project test suite with coverage reporting when validating code changes.","triggers":["coverage","tests","coverage"],"steps":[{"action":"bash","params":{"command":"go test ./... -cover"}}]}]`}
	store := &fakeStore{}
	ext := NewExtractor(llm, store)
	ext.now = func() time.Time { return time.Date(2026, 5, 2, 1, 2, 3, 0, time.UTC) }

	entries, err := ext.Extract(context.Background(), SessionSnapshot{
		Tool:        "codex",
		Title:       "add tests",
		ProjectPath: "D:/workprj/aicoder",
		Events:      []ImportantEvent{{Type: "command", Title: "tests", Summary: "ran go test ./... -cover coverage tests"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || len(store.registered) != 1 {
		t.Fatalf("expected one registered skill, entries=%d registered=%d", len(entries), len(store.registered))
	}
	got := store.registered[0]
	if got.Name != "run-coverage-tests" || got.Source != "learned" || got.SourceProject != "D:/workprj/aicoder" {
		t.Fatalf("unexpected skill metadata: %#v", got)
	}
	if got.Steps[0].OnError != "stop" {
		t.Fatalf("default on_error should be stop, got %q", got.Steps[0].OnError)
	}
	if len(got.Triggers) != 2 {
		t.Fatalf("expected duplicate triggers to be removed, got %#v", got.Triggers)
	}
}

func TestExtractorSkipsIneligibleFailedSession(t *testing.T) {
	llm := &fakeLLM{content: `[]`}
	code := 2
	_, err := NewExtractor(llm, &fakeStore{}).Extract(context.Background(), SessionSnapshot{
		ExitCode: &code,
		Events:   []ImportantEvent{{Type: "error", Title: "failed", Summary: "command failed"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if llm.calls != 0 {
		t.Fatalf("LLM should not be called for ineligible failed sessions")
	}
}

func TestExtractorRejectsUnsafePatternShape(t *testing.T) {
	llm := &fakeLLM{content: `[{"name":"Bad Name","description":"This should not be accepted because its name is invalid.","triggers":["bad","name"],"steps":[{"action":"unknown","params":{}}]}]`}
	store := &fakeStore{}
	entries, err := NewExtractor(llm, store).Extract(context.Background(), SessionSnapshot{
		Events: []ImportantEvent{{Type: "summary", Title: "work", Summary: "did work"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 || len(store.registered) != 0 {
		t.Fatalf("invalid pattern should not be registered")
	}
}

func TestExtractorUpdatesOnlyWhenPatternImproves(t *testing.T) {
	store := &fakeStore{skills: []corelib.NLSkillEntry{{
		Name:        "deploy-service",
		Description: "Deploy the service.",
		Triggers:    []string{"deploy", "service"},
		Steps:       []corelib.NLSkillStep{{Action: "bash"}},
		CreatedAt:   "old-time",
		UsageCount:  4,
	}}}
	llm := &fakeLLM{content: `[{"name":"deploy-service","description":"Deploy the service with build, migration, restart, and smoke-test verification for repeatable release work.","triggers":["deploy","service","release"],"steps":[{"action":"bash","params":{"command":"make build"}},{"action":"bash","params":{"command":"make migrate"}},{"action":"bash","params":{"command":"make smoke"}}]}]`}

	entries, err := NewExtractor(llm, store).Extract(context.Background(), SessionSnapshot{
		Events: []ImportantEvent{{Type: "deploy", Title: "release", Summary: "released service with make build, make migrate, and make smoke"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || len(store.updated) != 1 {
		t.Fatalf("expected one updated skill, entries=%d updated=%d", len(entries), len(store.updated))
	}
	if store.updated[0].CreatedAt != "old-time" || store.updated[0].UsageCount != 4 {
		t.Fatalf("update should preserve existing stats: %#v", store.updated[0])
	}
}

func TestExtractorRejectsTrivialSingleCommand(t *testing.T) {
	llm := &fakeLLM{content: `[{"name":"pull-latest-code","description":"Pull latest code from git when a repository needs to be updated.","triggers":["git","pull","update"],"steps":[{"action":"bash","params":{"command":"git pull"}}]}]`}
	store := &fakeStore{}
	entries, err := NewExtractor(llm, store).Extract(context.Background(), SessionSnapshot{
		Events: []ImportantEvent{{Type: "command", Title: "git", Summary: "ran git pull"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 || len(store.registered) != 0 {
		t.Fatalf("trivial one-command pattern should not be registered")
	}
}

func TestExtractorRejectsOneOffAbsolutePath(t *testing.T) {
	llm := &fakeLLM{content: `[{"name":"patch-local-file","description":"Patch a specific temporary local file with the exact command from one session.","triggers":["patch","local","file"],"steps":[{"action":"bash","params":{"command":"sed -i s/foo/bar/ D:\\work\\tmp\\one-off.txt"}},{"action":"bash","params":{"command":"cat D:\\work\\tmp\\one-off.txt"}}]}]`}
	store := &fakeStore{}
	entries, err := NewExtractor(llm, store).Extract(context.Background(), SessionSnapshot{
		Events: []ImportantEvent{{Type: "command", Title: "patch", Summary: "patched a local temp file"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 || len(store.registered) != 0 {
		t.Fatalf("one-off absolute-path pattern should not be registered")
	}
}

func TestExtractorCapturesRequiredArgs(t *testing.T) {
	llm := &fakeLLM{content: `[{"name":"deploy-service-env","description":"Deploy a named service to a target environment with a smoke-test verification step.","triggers":["deploy","service","environment"],"steps":[{"action":"bash","params":{"command":"deploy {{service}} --env {{env}}"},"on_error":"stop"},{"action":"bash","params":{"command":"smoke-test {{service}} --env {{env}}"},"on_error":"continue"}]}]`}
	store := &fakeStore{}
	entries, err := NewExtractor(llm, store).Extract(context.Background(), SessionSnapshot{
		Events: []ImportantEvent{{Type: "deploy", Title: "service", Summary: "deployed a service with deploy service-name --env staging and smoke-test service-name --env staging"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || len(store.registered) != 1 {
		t.Fatalf("expected parameterized skill to be registered")
	}
	got := store.registered[0].RequiredArgs
	if len(got) != 2 || got[0] != "env" || got[1] != "service" {
		t.Fatalf("unexpected required args: %#v", got)
	}
	params := store.registered[0].Params
	if len(params) != 2 || params[0].Name != "env" || !params[0].Required || !params[0].Synthetic {
		t.Fatalf("unexpected synthesized params: %#v", params)
	}
}

func TestEvaluatePatternQualityExplainsDecision(t *testing.T) {
	report := EvaluatePatternQuality(Pattern{
		Name:        "deploy-service-env",
		Description: "Deploy a named service to an environment with verification.",
		Triggers:    []string{"deploy", "service", "environment"},
		Steps: []Step{
			{Action: "bash", Params: map[string]interface{}{"command": "deploy {{service}} --env {{env}}"}, OnError: "stop"},
			{Action: "bash", Params: map[string]interface{}{"command": "smoke-test {{service}} --env {{env}}"}, OnError: "continue"},
		},
	})
	if !report.Passes() || report.Score < minPatternQualityScore || len(report.Reasons) == 0 {
		t.Fatalf("expected passing quality report with reasons, got %#v", report)
	}
}
func TestExtractorConsolidatesSimilarPatternNames(t *testing.T) {
	store := &fakeStore{skills: []corelib.NLSkillEntry{{
		Name:        "deploy-service-env",
		Description: "Deploy a service to an environment.",
		Triggers:    []string{"deploy", "service", "environment"},
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "deploy {{service}} --env {{env}}"}},
			{Action: "bash", Params: map[string]interface{}{"command": "smoke-test {{service}} --env {{env}}"}},
		},
		CreatedAt:    "old-time",
		UsageCount:   7,
		SuccessCount: 6,
	}}}
	llm := &fakeLLM{content: `[{"name":"release-service-env","description":"Release a named service to a target environment with deploy and smoke-test verification steps.","triggers":["release","deploy","service","environment"],"steps":[{"action":"bash","params":{"command":"deploy {{service}} --env {{env}}"},"on_error":"stop"},{"action":"bash","params":{"command":"smoke-test {{service}} --env {{env}}"},"on_error":"continue"}]}]`}

	entries, err := NewExtractor(llm, store).Extract(context.Background(), SessionSnapshot{
		Events: []ImportantEvent{{Type: "deploy", Title: "release", Summary: "released a service with deploy service-name --env staging and smoke-test service-name --env staging"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || len(store.updated) != 1 || len(store.registered) != 0 {
		t.Fatalf("expected similar pattern to update existing skill, entries=%d updated=%d registered=%d", len(entries), len(store.updated), len(store.registered))
	}
	updated := store.updated[0]
	if updated.Name != "deploy-service-env" {
		t.Fatalf("expected existing skill identity to be preserved, got %q", updated.Name)
	}
	if updated.CreatedAt != "old-time" || updated.UsageCount != 7 || updated.SuccessCount != 6 {
		t.Fatalf("expected existing usage stats to be preserved, got %#v", updated)
	}
}

func TestSimilarSkillRequiresStepShapeMatch(t *testing.T) {
	a := corelib.NLSkillEntry{
		Name:     "deploy-service-env",
		Triggers: []string{"deploy", "service", "environment"},
		Steps:    []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "deploy {{service}} --env {{env}}"}}},
	}
	b := corelib.NLSkillEntry{
		Name:     "deploy-service-with-db",
		Triggers: []string{"deploy", "service", "environment"},
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "migrate {{service}} --env {{env}}"}},
			{Action: "bash", Params: map[string]interface{}{"command": "deploy {{service}} --env {{env}}"}},
		},
	}
	if similarSkill(a, b) {
		t.Fatalf("different step shapes should not be consolidated")
	}
}
func TestExtractorDetailedReportsSkippedQuality(t *testing.T) {
	llm := &fakeLLM{content: `[{"name":"pull-latest-code","description":"Pull latest code from git when a repository needs to be updated.","triggers":["git","pull","update"],"steps":[{"action":"bash","params":{"command":"git pull"}}]}]`}
	result, err := NewExtractor(llm, &fakeStore{}).ExtractDetailed(context.Background(), SessionSnapshot{
		Events: []ImportantEvent{{Type: "command", Title: "git", Summary: "ran git pull"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Upserted) != 0 || len(result.Decisions) != 1 {
		t.Fatalf("expected one skipped decision, got %#v", result)
	}
	decision := result.Decisions[0]
	if decision.Action != DecisionSkipped || decision.Reason != "quality score below threshold" {
		t.Fatalf("unexpected decision: %#v", decision)
	}
	if decision.Quality.Score >= minPatternQualityScore || len(decision.Quality.Reasons) == 0 {
		t.Fatalf("expected low quality report with reasons, got %#v", decision.Quality)
	}
}

func TestExtractorDetailedReportsMatchedUpdate(t *testing.T) {
	store := &fakeStore{skills: []corelib.NLSkillEntry{{
		Name:        "deploy-service",
		Description: "Deploy the service.",
		Triggers:    []string{"deploy", "service"},
		Steps:       []corelib.NLSkillStep{{Action: "bash"}},
		CreatedAt:   "old-time",
		UsageCount:  4,
	}}}
	llm := &fakeLLM{content: `[{"name":"deploy-service","description":"Deploy the service with build, migration, restart, and smoke-test verification for repeatable release work.","triggers":["deploy","service","release"],"steps":[{"action":"bash","params":{"command":"make build"}},{"action":"bash","params":{"command":"make migrate"}},{"action":"bash","params":{"command":"make smoke"}}]}]`}
	result, err := NewExtractor(llm, store).ExtractDetailed(context.Background(), SessionSnapshot{
		Events: []ImportantEvent{{Type: "deploy", Title: "release", Summary: "released service with make build, make migrate, and make smoke"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Upserted) != 1 || len(result.Decisions) != 1 {
		t.Fatalf("unexpected detailed result: %#v", result)
	}
	decision := result.Decisions[0]
	if decision.Action != DecisionUpdated || decision.MatchedSkillName != "deploy-service" || !decision.Quality.Passes() {
		t.Fatalf("unexpected update decision: %#v", decision)
	}
}
func TestBuildSessionHistoryRedactsSecrets(t *testing.T) {
	history := BuildSessionHistory(SessionSnapshot{
		Tool:        "codex",
		Title:       "debug auth",
		ProjectPath: "D:/workprj/aicoder",
		Events: []ImportantEvent{{
			Type:    "command",
			Title:   "env",
			Summary: "configured password=supersecret and api key sk-12345678901234567890",
		}},
		RawOutputLines: []string{"Authorization: Bearer eyJabc.eyJdef.signature"},
	})
	if strings.Contains(history, "supersecret") || strings.Contains(history, "sk-12345678901234567890") || strings.Contains(history, "eyJabc") {
		t.Fatalf("history should redact secrets, got: %s", history)
	}
	if !strings.Contains(history, "[REDACTED]") {
		t.Fatalf("expected redaction marker in history, got: %s", history)
	}
}

func TestExtractorRejectsRedactedSecretPattern(t *testing.T) {
	llm := &fakeLLM{content: `[{"name":"configure-api-key","description":"Configure an API key for a service using the captured session value.","triggers":["api","key","configure"],"steps":[{"action":"bash","params":{"command":"export API_KEY=sk-12345678901234567890"},"on_error":"stop"},{"action":"bash","params":{"command":"service restart"},"on_error":"continue"}]}]`}
	result, err := NewExtractor(llm, &fakeStore{}).ExtractDetailed(context.Background(), SessionSnapshot{
		Events: []ImportantEvent{{Type: "command", Title: "auth", Summary: "configured api key"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Upserted) != 0 || len(result.Decisions) != 1 {
		t.Fatalf("expected redacted-secret pattern to be skipped, got %#v", result)
	}
	decision := result.Decisions[0]
	if decision.Action != DecisionSkipped || decision.Reason != "quality score below threshold" {
		t.Fatalf("unexpected decision: %#v", decision)
	}
	foundReason := false
	for _, reason := range decision.Quality.Reasons {
		if strings.Contains(reason, "redacted secret") {
			foundReason = true
			break
		}
	}
	if !foundReason {
		t.Fatalf("expected quality report to mention redacted secret, got %#v", decision.Quality.Reasons)
	}
}
func TestExtractorRejectsUngroundedPattern(t *testing.T) {
	llm := &fakeLLM{content: `[{"name":"deploy-service-env","description":"Deploy a named service to a target environment with verification.","triggers":["deploy","service","environment"],"steps":[{"action":"bash","params":{"command":"deploy {{service}} --env {{env}}"},"on_error":"stop"},{"action":"bash","params":{"command":"smoke-test {{service}} --env {{env}}"},"on_error":"continue"}]}]`}
	result, err := NewExtractor(llm, &fakeStore{}).ExtractDetailed(context.Background(), SessionSnapshot{
		Events: []ImportantEvent{{Type: "test", Title: "coverage", Summary: "ran go test ./... -cover and fixed assertions"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Upserted) != 0 || len(result.Decisions) != 1 {
		t.Fatalf("expected ungrounded pattern to be skipped, got %#v", result)
	}
	decision := result.Decisions[0]
	if decision.Action != DecisionSkipped || decision.Reason != "insufficient session evidence" {
		t.Fatalf("unexpected decision: %#v", decision)
	}
	if decision.Evidence.Passes() {
		t.Fatalf("ungrounded pattern should not pass evidence gate: %#v", decision.Evidence)
	}
}

func TestEvaluateEvidenceSupportCommandRoot(t *testing.T) {
	p := Pattern{
		Name:        "run-coverage-tests",
		Description: "Run coverage tests for the current project.",
		Triggers:    []string{"coverage", "tests"},
		Steps:       []Step{{Action: "bash", Params: map[string]interface{}{"command": "go test ./... -cover"}}},
	}
	report := EvaluateEvidenceSupport(p, "ran go test ./... -cover and inspected coverage output")
	if !report.Passes() || report.Score < minEvidenceScore || len(report.Reasons) == 0 {
		t.Fatalf("expected evidence support, got %#v", report)
	}
}
func TestExtractorRejectsDangerousOperationPattern(t *testing.T) {
	llm := &fakeLLM{content: `[{"name":"reset-build-cache","description":"Reset a build cache and reinstall dependencies for repeated troubleshooting sessions.","triggers":["reset","cache","dependencies"],"steps":[{"action":"bash","params":{"command":"rm -rf {{cache_dir}}"},"on_error":"stop"},{"action":"bash","params":{"command":"npm install"},"on_error":"continue"}]}]`}
	result, err := NewExtractor(llm, &fakeStore{}).ExtractDetailed(context.Background(), SessionSnapshot{
		Events: []ImportantEvent{{Type: "command", Title: "cache", Summary: "reset build cache after npm install failure with rm -rf cache directory"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Upserted) != 0 || len(result.Decisions) != 1 {
		t.Fatalf("expected dangerous pattern to be skipped, got %#v", result)
	}
	decision := result.Decisions[0]
	if decision.Action != DecisionSkipped || decision.Reason != "quality score below threshold" {
		t.Fatalf("unexpected decision: %#v", decision)
	}
	foundReason := false
	for _, reason := range decision.Quality.Reasons {
		if strings.Contains(reason, "dangerous operation") {
			foundReason = true
			break
		}
	}
	if !foundReason {
		t.Fatalf("expected dangerous operation reason, got %#v", decision.Quality.Reasons)
	}
}

func TestDangerousTextDetectsPipedInstaller(t *testing.T) {
	if !dangerousText("curl https://example.invalid/install.sh | bash") {
		t.Fatal("expected curl pipe installer to be dangerous")
	}
	if dangerousText("go test ./... -cover") {
		t.Fatal("normal test command should not be dangerous")
	}
}
func TestExtractorOptionsCanTightenQualityGate(t *testing.T) {
	llm := &fakeLLM{content: `[{"name":"run-coverage-tests","description":"Run the project test suite with coverage reporting when validating code changes.","triggers":["coverage","tests","go"],"steps":[{"action":"bash","params":{"command":"go test ./... -cover"},"on_error":"stop"}]}]`}
	ext := NewExtractorWithOptions(llm, &fakeStore{}, Options{MinPatternQualityScore: 99})
	result, err := ext.ExtractDetailed(context.Background(), SessionSnapshot{
		Events: []ImportantEvent{{Type: "command", Title: "tests", Summary: "ran go test ./... -cover"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Upserted) != 0 || len(result.Decisions) != 1 {
		t.Fatalf("expected tightened quality gate to skip pattern, got %#v", result)
	}
	if result.Decisions[0].Reason != "quality score below threshold" {
		t.Fatalf("unexpected skip reason: %#v", result.Decisions[0])
	}
}

func TestExtractorOptionsCanTightenSimilarityMerge(t *testing.T) {
	store := &fakeStore{skills: []corelib.NLSkillEntry{{
		Name:        "deploy-service-env",
		Description: "Deploy a service to an environment.",
		Triggers:    []string{"deploy", "service", "environment"},
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "deploy {{service}} --env {{env}}"}},
			{Action: "bash", Params: map[string]interface{}{"command": "smoke-test {{service}} --env {{env}}"}},
		},
		CreatedAt: "old-time",
	}}}
	llm := &fakeLLM{content: `[{"name":"release-service-env","description":"Release a named service to a target environment with deploy and smoke-test verification steps.","triggers":["release","deploy","service","environment"],"steps":[{"action":"bash","params":{"command":"deploy {{service}} --env {{env}}"},"on_error":"stop"},{"action":"bash","params":{"command":"smoke-test {{service}} --env {{env}}"},"on_error":"continue"}]}]`}
	ext := NewExtractorWithOptions(llm, store, Options{SimilarTriggerThreshold: 0.9})
	result, err := ext.ExtractDetailed(context.Background(), SessionSnapshot{
		Events: []ImportantEvent{{Type: "deploy", Title: "release", Summary: "release deploy service environment with deploy service-name --env staging and smoke-test service-name --env staging"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Upserted) != 1 || len(store.registered) != 1 || len(store.updated) != 0 {
		t.Fatalf("expected strict similarity threshold to register separately, result=%#v registered=%d updated=%d", result, len(store.registered), len(store.updated))
	}
	if store.registered[0].Name != "release-service-env" {
		t.Fatalf("expected new skill identity, got %q", store.registered[0].Name)
	}
}
func TestExtractorNormalizesPatternName(t *testing.T) {
	llm := &fakeLLM{content: `[{"name":"Run Coverage Tests!","description":"Run the project test suite with coverage reporting when validating code changes.","triggers":["coverage","tests","go"],"steps":[{"action":"bash","params":{"command":"go test ./... -cover"},"on_error":"stop"}]}]`}
	store := &fakeStore{}
	entries, err := NewExtractor(llm, store).Extract(context.Background(), SessionSnapshot{
		Events: []ImportantEvent{{Type: "command", Title: "tests", Summary: "ran go test ./... -cover and reviewed coverage"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || len(store.registered) != 1 {
		t.Fatalf("expected normalized pattern to register, got entries=%d registered=%d", len(entries), len(store.registered))
	}
	if store.registered[0].Name != "run-coverage-tests" {
		t.Fatalf("unexpected normalized name: %q", store.registered[0].Name)
	}
}

func TestNormalizePatternName(t *testing.T) {
	tests := map[string]string{
		"Run Coverage Tests!":   "run-coverage-tests",
		" deploy__service env ": "deploy-service-env",
		"---bad---name---":      "bad-name",
		"non-ascii-name":        "non-ascii-name",
	}
	for in, want := range tests {
		if got := NormalizePatternName(in); got != want {
			t.Fatalf("NormalizePatternName(%q) = %q, want %q", in, got, want)
		}
	}
}
func TestExtractorGeneralizesProjectPath(t *testing.T) {
	projectPath := `D:\workprj\aicoder`
	llm := &fakeLLM{content: `[{"name":"Run Project Tests","description":"Run this project test suite from its repository root with coverage output.","triggers":["project","tests","coverage"],"steps":[{"action":"bash","params":{"command":"cd D:\\workprj\\aicoder && go test ./... -cover"},"on_error":"stop"}]}]`}
	store := &fakeStore{}
	entries, err := NewExtractor(llm, store).Extract(context.Background(), SessionSnapshot{
		ProjectPath: projectPath,
		Events:      []ImportantEvent{{Type: "command", Title: "tests", Summary: `ran cd D:\workprj\aicoder && go test ./... -cover`}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || len(store.registered) != 1 {
		t.Fatalf("expected project-path pattern to register after generalization, entries=%d registered=%d", len(entries), len(store.registered))
	}
	cmd, _ := store.registered[0].Steps[0].Params["command"].(string)
	if strings.Contains(cmd, projectPath) || !strings.Contains(cmd, projectPathTemplate) {
		t.Fatalf("expected command to contain generalized project path, got %q", cmd)
	}
	if len(store.registered[0].RequiredArgs) != 1 || store.registered[0].RequiredArgs[0] != projectPathArg {
		t.Fatalf("expected project_path required arg, got %#v", store.registered[0].RequiredArgs)
	}
	params := store.registered[0].Params
	if len(params) != 1 || params[0].Name != projectPathArg || !strings.Contains(params[0].Description, "Project root") {
		t.Fatalf("expected synthesized project_path param, got %#v", params)
	}
}

func TestGeneralizePatternNestedParams(t *testing.T) {
	p := Pattern{Steps: []Step{{Action: "call_mcp_tool", Params: map[string]interface{}{
		"path":   `D:\workprj\aicoder\README.md`,
		"nested": map[string]interface{}{"cwd": `D:/workprj/aicoder`},
	}}}}
	got := GeneralizePattern(p, `D:\workprj\aicoder`)
	if got.Steps[0].Params["path"] != projectPathTemplate+`\README.md` {
		t.Fatalf("unexpected generalized path: %#v", got.Steps[0].Params["path"])
	}
	nested := got.Steps[0].Params["nested"].(map[string]interface{})
	if nested["cwd"] != projectPathTemplate {
		t.Fatalf("unexpected nested cwd: %#v", nested["cwd"])
	}
}
func TestSynthesizeSkillParamsDescriptions(t *testing.T) {
	steps := []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "deploy {{service}} --env {{env}} --value {{unknown_value}}"}}}
	params := synthesizeSkillParams(steps, []string{"env", "service", "unknown_value"})
	if len(params) != 3 {
		t.Fatalf("expected 3 params, got %#v", params)
	}
	if !strings.Contains(params[0].Description, "environment") {
		t.Fatalf("expected env description, got %#v", params[0])
	}
	if len(params[0].Aliases) == 0 {
		t.Fatalf("expected env aliases, got %#v", params[0])
	}
	if !params[2].Required || !params[2].Synthetic {
		t.Fatalf("expected synthetic required param, got %#v", params[2])
	}
}
func TestExtractorOptionsLimitPatternsPerExtraction(t *testing.T) {
	llm := &fakeLLM{content: `[
		{"name":"run-coverage-tests","description":"Run the project test suite with coverage reporting when validating code changes.","triggers":["coverage","tests","go"],"steps":[{"action":"bash","params":{"command":"go test ./... -cover"},"on_error":"stop"}]},
		{"name":"run-unit-tests","description":"Run the project unit test suite when validating ordinary code changes.","triggers":["unit","tests","go"],"steps":[{"action":"bash","params":{"command":"go test ./..."},"on_error":"stop"}]}
	]`}
	store := &fakeStore{}
	ext := NewExtractorWithOptions(llm, store, Options{MaxPatternsPerExtraction: 1})
	result, err := ext.ExtractDetailed(context.Background(), SessionSnapshot{
		Events: []ImportantEvent{{Type: "command", Title: "tests", Summary: "ran go test ./... -cover and go test ./..."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Upserted) != 1 || len(store.registered) != 1 || len(result.Decisions) != 2 {
		t.Fatalf("expected one upsert and two decisions, got result=%#v registered=%d", result, len(store.registered))
	}
	if result.Decisions[1].Action != DecisionSkipped || result.Decisions[1].Reason != "pattern budget exceeded" {
		t.Fatalf("expected budget skip for second pattern, got %#v", result.Decisions[1])
	}
}
func TestParsePatternsAcceptsCodeFence(t *testing.T) {
	patterns, err := ParsePatterns("```json\n[]\n```")
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != 0 {
		t.Fatalf("expected empty pattern list")
	}
}

func TestParsePatternsAcceptsWrappedPatterns(t *testing.T) {
	patterns, err := ParsePatterns(`{"patterns":[{"name":"run-tests","description":"Run tests","triggers":["test"],"steps":[]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != 1 || patterns[0].Name != "run-tests" {
		t.Fatalf("unexpected wrapped patterns: %#v", patterns)
	}
}

func TestEvaluateEvidenceSupportRejectsInventedCommandStep(t *testing.T) {
	p := Pattern{
		Name:        "validate-go-project",
		Description: "Run observed Go validation commands for the current project.",
		Triggers:    []string{"go", "test", "validate"},
		Steps: []Step{
			{Action: "bash", Params: map[string]interface{}{"command": "go test ./..."}},
			{Action: "bash", Params: map[string]interface{}{"command": "go vet ./..."}},
		},
	}
	report := EvaluateEvidenceSupport(p, "ran go test ./... and reviewed failing test output")
	if report.Passes() {
		t.Fatalf("pattern with invented go vet step should not pass evidence gate: %#v", report)
	}
	if len(report.UnsupportedSteps) != 1 || report.UnsupportedSteps[0] != "go:vet" {
		t.Fatalf("expected unsupported go vet signature, got %#v", report.UnsupportedSteps)
	}
}

func TestEvaluateEvidenceSupportNonBashAction(t *testing.T) {
	p := Pattern{
		Name:        "fetch-issue-context",
		Description: "Fetch issue context through an MCP tool before planning code changes.",
		Triggers:    []string{"issue", "mcp", "context"},
		Steps: []Step{{Action: "call_mcp_tool", Params: map[string]interface{}{
			"tool": "github_issue_fetch",
			"repo": "CodeClaw",
		}}},
	}
	report := EvaluateEvidenceSupport(p, "called call_mcp_tool github_issue_fetch for the CodeClaw repo and used the issue context")
	if !report.Passes() {
		t.Fatalf("non-bash action with observed tool evidence should pass, got %#v", report)
	}
}

func TestResultSummaryAggregatesDecisions(t *testing.T) {
	result := Result{Decisions: []Decision{
		{PatternName: "new-skill", Action: DecisionRegistered},
		{PatternName: "better-skill", Action: DecisionUpdated},
		{PatternName: "weak-skill", Action: DecisionSkipped, Reason: "quality score below threshold"},
		{PatternName: "invented-skill", Action: DecisionSkipped, Reason: "insufficient session evidence", Evidence: EvidenceReport{UnsupportedSteps: []string{"go:vet", "make:deploy"}}},
		{PatternName: "invented-again", Action: DecisionSkipped, Reason: "insufficient session evidence", Evidence: EvidenceReport{UnsupportedSteps: []string{"go:vet"}}},
	}}

	summary := result.Summary()
	if summary.TotalCandidates != 5 || summary.Registered != 1 || summary.Updated != 1 || summary.Skipped != 3 {
		t.Fatalf("unexpected summary counts: %#v", summary)
	}
	if summary.SkipReasons["insufficient session evidence"] != 2 || summary.SkipReasons["quality score below threshold"] != 1 {
		t.Fatalf("unexpected skip reasons: %#v", summary.SkipReasons)
	}
	if summary.UnsupportedSteps["go:vet"] != 2 || summary.UnsupportedSteps["make:deploy"] != 1 {
		t.Fatalf("unexpected unsupported steps: %#v", summary.UnsupportedSteps)
	}
}

func TestSimilarSkillDistinguishesCommandSubcommands(t *testing.T) {
	a := corelib.NLSkillEntry{
		Name:     "validate-go-tests",
		Triggers: []string{"go", "validate", "test"},
		Steps:    []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "go test ./..."}}},
	}
	b := corelib.NLSkillEntry{
		Name:     "validate-go-vet",
		Triggers: []string{"go", "validate", "test"},
		Steps:    []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "go vet ./..."}}},
	}
	if similarSkill(a, b) {
		t.Fatalf("different go subcommands should not be consolidated")
	}
}

func TestSimilarSkillMatchesEquivalentCommandShape(t *testing.T) {
	a := corelib.NLSkillEntry{
		Name:     "run-go-tests",
		Triggers: []string{"go", "test", "coverage"},
		Steps:    []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "cd {{project_path}} && go test ./... -cover"}}},
	}
	b := corelib.NLSkillEntry{
		Name:     "validate-go-tests",
		Triggers: []string{"go", "test", "coverage"},
		Steps:    []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "go test ./... -race"}}},
	}
	if !similarSkill(a, b) {
		t.Fatalf("equivalent go test command shape should be consolidated")
	}
}

func TestIsPatternBetterPromotesParameterizedWorkflow(t *testing.T) {
	existing := corelib.NLSkillEntry{
		Name:        "deploy-service-env",
		Description: "Deploy service to staging.",
		Triggers:    []string{"deploy", "service", "environment"},
		Steps:       []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "deploy api --env staging"}}},
	}
	candidate := Pattern{
		Name:        "deploy-service-env",
		Description: "Deploy a named service to a selected environment with an explicit reusable template.",
		Triggers:    []string{"deploy", "service", "environment"},
		Steps:       []Step{{Action: "bash", Params: map[string]interface{}{"command": "deploy {{service}} --env {{env}}"}, OnError: "stop"}},
	}
	if !IsPatternBetter(candidate, existing) {
		t.Fatalf("parameterized workflow should improve hardcoded existing skill")
	}
}

func TestIsPatternBetterKeepsEquivalentExistingSkill(t *testing.T) {
	existing := corelib.NLSkillEntry{
		Name:        "run-go-tests",
		Description: "Run the Go project test suite with coverage when validating code changes.",
		Triggers:    []string{"go", "test", "coverage"},
		Steps:       []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "go test ./... -cover"}, OnError: "stop"}},
	}
	candidate := Pattern{
		Name:        "run-go-tests",
		Description: "Run the Go project test suite with coverage when validating code changes.",
		Triggers:    []string{"go", "test", "coverage"},
		Steps:       []Step{{Action: "bash", Params: map[string]interface{}{"command": "go test ./... -cover"}, OnError: "stop"}},
	}
	if IsPatternBetter(candidate, existing) {
		t.Fatalf("equivalent candidate should not churn existing skill")
	}
}

func TestExtractorDetailedReportsRegisterFailure(t *testing.T) {
	llm := &fakeLLM{content: `[{"name":"run-coverage-tests","description":"Run the project test suite with coverage reporting when validating code changes.","triggers":["coverage","tests","go"],"steps":[{"action":"bash","params":{"command":"go test ./... -cover"},"on_error":"stop"}]}]`}
	store := &fakeStore{registerErr: errors.New("disk unavailable")}
	result, err := NewExtractor(llm, store).ExtractDetailed(context.Background(), SessionSnapshot{
		Events: []ImportantEvent{{Type: "command", Title: "tests", Summary: "ran go test ./... -cover and reviewed coverage"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Upserted) != 0 || len(result.Decisions) != 1 {
		t.Fatalf("expected failed register to produce one skipped decision, got %#v", result)
	}
	decision := result.Decisions[0]
	if decision.Action != DecisionSkipped || !strings.Contains(decision.Reason, "register failed") {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestExtractorDetailedReportsUpdateFailure(t *testing.T) {
	store := &fakeStore{
		skills: []corelib.NLSkillEntry{{
			Name:        "deploy-service",
			Description: "Deploy the service.",
			Triggers:    []string{"deploy", "service"},
			Steps:       []corelib.NLSkillStep{{Action: "bash"}},
			CreatedAt:   "old-time",
			UsageCount:  4,
		}},
		updateErr: errors.New("write denied"),
	}
	llm := &fakeLLM{content: `[{"name":"deploy-service","description":"Deploy the service with build, migration, restart, and smoke-test verification for repeatable release work.","triggers":["deploy","service","release"],"steps":[{"action":"bash","params":{"command":"make build"}},{"action":"bash","params":{"command":"make migrate"}},{"action":"bash","params":{"command":"make smoke"}}]}]`}
	result, err := NewExtractor(llm, store).ExtractDetailed(context.Background(), SessionSnapshot{
		Events: []ImportantEvent{{Type: "deploy", Title: "release", Summary: "released service with make build, make migrate, and make smoke"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Upserted) != 0 || len(result.Decisions) != 1 {
		t.Fatalf("expected failed update to produce one skipped decision, got %#v", result)
	}
	decision := result.Decisions[0]
	if decision.Action != DecisionSkipped || !strings.Contains(decision.Reason, "update failed") || decision.MatchedSkillName != "deploy-service" {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestResultAuditViewBoundsAndRedactsDetails(t *testing.T) {
	longReason := strings.Repeat("x", 80) + " sk-12345678901234567890"
	result := Result{Decisions: []Decision{
		{
			PatternName: "first-pattern",
			Action:      DecisionSkipped,
			Reason:      longReason,
			Quality:     QualityReport{Reasons: []string{"quality one", "quality two", "quality three"}},
			Evidence:    EvidenceReport{Reasons: []string{"evidence one", "evidence two"}, UnsupportedSteps: []string{"go:vet", "make:deploy", "npm:audit"}},
		},
		{PatternName: "second-pattern", Action: DecisionRegistered},
	}}

	summary := result.AuditSummary(AuditOptions{MaxSummaryItems: 1, MaxStringLength: 24})
	decisions := result.AuditDecisions(AuditOptions{MaxDecisions: 1, MaxReasonsPerDecision: 2, MaxUnsupportedPerReport: 1, MaxStringLength: 24})

	if summary.TotalCandidates != 2 || len(decisions) != 1 {
		t.Fatalf("expected complete summary and bounded decisions, summary=%#v decisions=%#v", summary, decisions)
	}
	if strings.Contains(decisions[0].Reason, "sk-12345678901234567890") || !strings.Contains(decisions[0].Reason, "...") {
		t.Fatalf("expected redacted truncated reason, got %q", decisions[0].Reason)
	}
	if len(decisions[0].Quality.Reasons) != 2 || len(decisions[0].Evidence.UnsupportedSteps) != 1 {
		t.Fatalf("expected bounded reports, got %#v", decisions[0])
	}
}

func TestResultSummaryAuditViewKeepsTopReasons(t *testing.T) {
	summary := ResultSummary{
		TotalCandidates:  5,
		Skipped:          5,
		SkipReasons:      map[string]int{"rare": 1, "common": 3, "medium": 2},
		UnsupportedSteps: map[string]int{"go:vet": 2, "secret:sk-12345678901234567890": 1},
	}

	view := summary.AuditView(AuditOptions{MaxSummaryItems: 2, MaxStringLength: 32})
	if view.TotalCandidates != 5 || view.Skipped != 5 {
		t.Fatalf("counts should be preserved: %#v", view)
	}
	if len(view.SkipReasons) != 2 || view.SkipReasons["common"] != 3 || view.SkipReasons["medium"] != 2 {
		t.Fatalf("expected top skip reasons, got %#v", view.SkipReasons)
	}
	for key := range view.UnsupportedSteps {
		if strings.Contains(key, "sk-12345678901234567890") {
			t.Fatalf("unsupported step keys should be redacted, got %#v", view.UnsupportedSteps)
		}
	}
}

func TestAuditTrailBoundsNewestFirstAndDeepCopies(t *testing.T) {
	trail := NewAuditTrail(2)
	trail.Append(AuditEntry{SessionID: "old", Summary: ResultSummary{SkipReasons: map[string]int{"old": 1}}})
	trail.Append(AuditEntry{SessionID: "middle", Decisions: []Decision{{Quality: QualityReport{Reasons: []string{"stable"}}}}})
	trail.Append(AuditEntry{SessionID: "new", Upserted: []string{"run-tests"}})

	first := trail.List()
	if len(first) != 2 || first[0].SessionID != "new" || first[1].SessionID != "middle" {
		t.Fatalf("expected newest-first bounded audit entries, got %#v", first)
	}
	first[0].Upserted[0] = "mutated"
	first[1].Decisions[0].Quality.Reasons[0] = "mutated"

	second := trail.List()
	if second[0].Upserted[0] == "mutated" || second[1].Decisions[0].Quality.Reasons[0] == "mutated" {
		t.Fatalf("audit trail should return deep copies, got %#v", second)
	}
}

func TestAuditStatus(t *testing.T) {
	if AuditStatus(ResultSummary{}) != AuditStatusNoCandidates {
		t.Fatalf("empty summary should be no_candidates")
	}
	if AuditStatus(ResultSummary{TotalCandidates: 1}) != AuditStatusCompleted {
		t.Fatalf("candidate summary should be completed")
	}
}

func TestNewResultAuditEntryBuildsSafeEntry(t *testing.T) {
	result := Result{
		Upserted: []corelib.NLSkillEntry{{Name: "first-skill"}, {Name: "second-skill"}},
		Decisions: []Decision{
			{PatternName: "first-skill", Action: DecisionRegistered},
			{PatternName: "second-skill", Action: DecisionSkipped, Reason: "too weak"},
		},
	}
	entry := NewResultAuditEntry(AuditContext{
		Timestamp:  "2026-05-03T01:02:03Z",
		SessionID:  "sess-1",
		Snapshot:   SessionSnapshot{Tool: "codex", Title: "auth sk-12345678901234567890", ProjectPath: "D:/workprj/aicoder"},
		DurationMS: 123,
	}, result, AuditOptions{MaxDecisions: 1, MaxStringLength: 40})

	if entry.Timestamp != "2026-05-03T01:02:03Z" || entry.SessionID != "sess-1" || entry.DurationMS != 123 {
		t.Fatalf("unexpected audit metadata: %#v", entry)
	}
	if entry.Status != AuditStatusCompleted || entry.Summary.TotalCandidates != 2 {
		t.Fatalf("unexpected audit status/summary: %#v", entry)
	}
	if len(entry.Decisions) != 1 || len(entry.Upserted) != 1 || entry.Upserted[0] != "first-skill" {
		t.Fatalf("expected bounded decisions/upserted, got %#v", entry)
	}
	if strings.Contains(entry.Title, "sk-12345678901234567890") || !strings.Contains(entry.Title, "[REDACTED]") {
		t.Fatalf("expected redacted title, got %q", entry.Title)
	}
}

func TestNewErrorAuditEntryBuildsSafeEntry(t *testing.T) {
	entry := NewErrorAuditEntry(AuditContext{
		Timestamp:  "2026-05-03T01:02:03Z",
		SessionID:  "sess-error",
		Snapshot:   SessionSnapshot{Tool: "codex", Title: "extract"},
		DurationMS: 77,
	}, errors.New("failed with token sk-12345678901234567890"), AuditOptions{MaxStringLength: 64})

	if entry.Status != AuditStatusFailed || entry.DurationMS != 77 || entry.Summary.TotalCandidates != 0 {
		t.Fatalf("unexpected error audit entry: %#v", entry)
	}
	if strings.Contains(entry.Error, "sk-12345678901234567890") || !strings.Contains(entry.Error, "[REDACTED]") {
		t.Fatalf("expected redacted error, got %q", entry.Error)
	}
}

func TestAuditTrailRecordResultUsesConfiguredOptions(t *testing.T) {
	trail := NewAuditTrailWithOptions(5, AuditOptions{MaxDecisions: 1, MaxStringLength: 24})
	trail.RecordResult(AuditContext{Timestamp: "2026-05-03T02:00:00Z", SessionID: "sess-options"}, Result{
		Upserted: []corelib.NLSkillEntry{{Name: "first-skill"}, {Name: "second-skill"}},
		Decisions: []Decision{
			{PatternName: "first-skill", Action: DecisionRegistered},
			{PatternName: "second-skill", Action: DecisionSkipped, Reason: "second reason"},
		},
	})

	entries := trail.List()
	if len(entries) != 1 {
		t.Fatalf("expected one audit entry, got %#v", entries)
	}
	entry := entries[0]
	if entry.Status != AuditStatusCompleted || entry.Summary.TotalCandidates != 2 {
		t.Fatalf("unexpected result audit entry: %#v", entry)
	}
	if len(entry.Decisions) != 1 || len(entry.Upserted) != 1 || entry.Upserted[0] != "first-skill" {
		t.Fatalf("configured audit options should bound details, got %#v", entry)
	}
}

func TestAuditTrailRecordErrorUsesConfiguredOptions(t *testing.T) {
	trail := NewAuditTrailWithOptions(5, AuditOptions{MaxStringLength: 24})
	trail.RecordError(AuditContext{Timestamp: "2026-05-03T02:00:00Z", SessionID: "sess-error"}, errors.New("failed with api key sk-12345678901234567890 and extra diagnostic text"))

	entries := trail.List()
	if len(entries) != 1 || entries[0].Status != AuditStatusFailed {
		t.Fatalf("unexpected error audit entries: %#v", entries)
	}
	if strings.Contains(entries[0].Error, "sk-12345678901234567890") || len([]rune(entries[0].Error)) > 24 {
		t.Fatalf("expected bounded redacted error, got %q", entries[0].Error)
	}
}

func TestAuditTrailHealthAggregatesRecentEntries(t *testing.T) {
	trail := NewAuditTrailWithOptions(10, AuditOptions{MaxSummaryItems: 2, MaxStringLength: 32})
	trail.Append(AuditEntry{
		Timestamp:  "2026-05-03T03:00:00Z",
		Status:     AuditStatusFailed,
		DurationMS: 100,
	})
	trail.Append(AuditEntry{
		Timestamp:  "2026-05-03T03:01:00Z",
		Status:     AuditStatusNoCandidates,
		DurationMS: 200,
	})
	trail.Append(AuditEntry{
		Timestamp:  "2026-05-03T03:02:00Z",
		Status:     AuditStatusCompleted,
		DurationMS: 300,
		Summary: ResultSummary{
			TotalCandidates:  3,
			Registered:       1,
			Updated:          1,
			Skipped:          1,
			SkipReasons:      map[string]int{"rare": 1, "common": 3, "medium": 2},
			UnsupportedSteps: map[string]int{"go:vet": 2},
		},
	})

	health := trail.Health()
	if health.Runs != 3 || health.Completed != 1 || health.NoCandidates != 1 || health.Failed != 1 {
		t.Fatalf("unexpected run health: %#v", health)
	}
	if health.TotalCandidates != 3 || health.Registered != 1 || health.Updated != 1 || health.Skipped != 1 || health.AvgDurationMS != 200 {
		t.Fatalf("unexpected aggregate health: %#v", health)
	}
	if health.LatestTimestamp != "2026-05-03T03:02:00Z" {
		t.Fatalf("expected newest timestamp, got %q", health.LatestTimestamp)
	}
	if len(health.SkipReasons) != 2 || health.SkipReasons["common"] != 3 || health.SkipReasons["medium"] != 2 {
		t.Fatalf("expected bounded top skip reasons, got %#v", health.SkipReasons)
	}
}

func TestAuditHealthDiagnosesActionableStatus(t *testing.T) {
	cases := []struct {
		name       string
		entries    []AuditEntry
		wantStatus AuditHealthStatusKind
		wantIssue  string
		wantCode   string
		wantAction string
	}{
		{name: "empty", wantStatus: AuditHealthStatusEmpty, wantCode: AuditIssueNoRuns, wantAction: "eligible successful session"},
		{
			name:       "failing",
			entries:    []AuditEntry{{Status: AuditStatusFailed}},
			wantStatus: AuditHealthStatusFailing,
			wantIssue:  "failed",
			wantCode:   AuditIssueExtractionFailed,
			wantAction: "LLM connectivity",
		},
		{
			name:       "no signal from skips",
			entries:    []AuditEntry{{Status: AuditStatusCompleted, Summary: ResultSummary{TotalCandidates: 2, Skipped: 2, SkipReasons: map[string]int{"quality score below threshold": 2}}}},
			wantStatus: AuditHealthStatusNoSignal,
			wantIssue:  "quality score below threshold",
			wantCode:   AuditIssueQualityBelowThreshold,
			wantAction: "broader repeatable workflows",
		},
		{
			name:       "needs attention",
			entries:    []AuditEntry{{Status: AuditStatusCompleted, Summary: ResultSummary{TotalCandidates: 4, Registered: 1, Skipped: 3, SkipReasons: map[string]int{"insufficient session evidence": 3}}}},
			wantStatus: AuditHealthStatusNeedsAttention,
			wantIssue:  "insufficient session evidence",
			wantCode:   AuditIssueInsufficientEvidence,
			wantAction: "command output",
		},
		{
			name:       "healthy",
			entries:    []AuditEntry{{Status: AuditStatusCompleted, Summary: ResultSummary{TotalCandidates: 2, Registered: 1, Updated: 1}}},
			wantStatus: AuditHealthStatusHealthy,
			wantCode:   AuditIssueNone,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			health := SummarizeAuditEntries(tc.entries, AuditOptions{})
			if health.Status != tc.wantStatus {
				t.Fatalf("expected status %q, got %#v", tc.wantStatus, health)
			}
			if tc.wantCode != "" && health.IssueCode != tc.wantCode {
				t.Fatalf("expected issue code %q, got %#v", tc.wantCode, health)
			}
			if tc.wantIssue != "" && !strings.Contains(health.PrimaryIssue, tc.wantIssue) {
				t.Fatalf("expected issue to contain %q, got %#v", tc.wantIssue, health)
			}
			if tc.wantAction != "" && !strings.Contains(health.SuggestedAction, tc.wantAction) {
				t.Fatalf("expected action to contain %q, got %#v", tc.wantAction, health)
			}
		})
	}
}

func TestAuditHealthIssueCodeUsesRawReasonBeforeTruncation(t *testing.T) {
	health := SummarizeAuditEntries([]AuditEntry{{
		Status: AuditStatusCompleted,
		Summary: ResultSummary{
			TotalCandidates: 1,
			Skipped:         1,
			SkipReasons:     map[string]int{"quality score below threshold with secret sk-12345678901234567890": 1},
		},
	}}, AuditOptions{MaxStringLength: 18})
	if health.IssueCode != AuditIssueQualityBelowThreshold {
		t.Fatalf("expected raw reason to drive issue code, got %#v", health)
	}
	if strings.Contains(health.PrimaryIssue, "sk-12345678901234567890") || len([]rune(health.PrimaryIssue)) > len("no skills learned: ")+18 {
		t.Fatalf("primary issue should be redacted and bounded, got %q", health.PrimaryIssue)
	}
}

func TestSummarizeAuditEntriesRedactsHealthKeys(t *testing.T) {
	health := SummarizeAuditEntries([]AuditEntry{{
		Status:  AuditStatusCompleted,
		Summary: ResultSummary{SkipReasons: map[string]int{"failed with sk-12345678901234567890": 1}},
	}}, AuditOptions{MaxStringLength: 32})
	for key := range health.SkipReasons {
		if strings.Contains(key, "sk-12345678901234567890") || !strings.Contains(key, "[REDACTED]") {
			t.Fatalf("expected redacted health key, got %#v", health.SkipReasons)
		}
	}
}

func TestAuditTrailAppendSanitizesDirectEntries(t *testing.T) {
	trail := NewAuditTrailWithOptions(5, AuditOptions{MaxDecisions: 1, MaxReasonsPerDecision: 1, MaxStringLength: 32})
	trail.Append(AuditEntry{
		SessionID: "sess-sk-12345678901234567890",
		Title:     "debug sk-12345678901234567890",
		Error:     "failed with sk-12345678901234567890 and extra diagnostic text",
		Decisions: []Decision{
			{PatternName: "first", Reason: "reason sk-12345678901234567890", Quality: QualityReport{Reasons: []string{"one", "two"}}},
			{PatternName: "second"},
		},
		Upserted: []string{"first", "second"},
		Summary:  ResultSummary{SkipReasons: map[string]int{"secret sk-12345678901234567890": 1}},
	})

	entries := trail.List()
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %#v", entries)
	}
	entry := entries[0]
	if entry.Status != AuditStatusFailed {
		t.Fatalf("entry with error should be failed, got %#v", entry)
	}
	if strings.Contains(entry.SessionID+entry.Title+entry.Error, "sk-12345678901234567890") {
		t.Fatalf("direct append should redact text fields, got %#v", entry)
	}
	if len(entry.Decisions) != 1 || len(entry.Decisions[0].Quality.Reasons) != 1 || len(entry.Upserted) != 1 {
		t.Fatalf("direct append should bound slices, got %#v", entry)
	}
	for key := range entry.Summary.SkipReasons {
		if strings.Contains(key, "sk-12345678901234567890") {
			t.Fatalf("direct append should redact summary keys, got %#v", entry.Summary.SkipReasons)
		}
	}
}

func TestEmptyAuditHealthIsConsistent(t *testing.T) {
	var nilTrail *AuditTrail
	zeroTrail := NewAuditTrail(5)
	healths := []AuditHealth{
		EmptyAuditHealth(),
		nilTrail.Health(),
		zeroTrail.Health(),
		SummarizeAuditEntries(nil, AuditOptions{}),
	}
	for _, health := range healths {
		if health.Runs != 0 || health.Completed != 0 || health.Failed != 0 || health.Status != AuditHealthStatusEmpty || health.IssueCode != AuditIssueNoRuns || health.SuggestedAction == "" {
			t.Fatalf("empty health should be consistent, got %#v", health)
		}
	}
}

func TestSummarizeAuditEntriesInfersMissingStatus(t *testing.T) {
	health := SummarizeAuditEntries([]AuditEntry{
		{Summary: ResultSummary{}},
		{Error: "failed"},
		{Summary: ResultSummary{TotalCandidates: 1}},
	}, AuditOptions{})
	if health.NoCandidates != 1 || health.Failed != 1 || health.Completed != 1 {
		t.Fatalf("expected inferred statuses, got %#v", health)
	}
}

func TestZeroValueAuditTrailAppendUsesDefaultLimit(t *testing.T) {
	var trail AuditTrail
	trail.Append(AuditEntry{SessionID: "zero"})
	entries := trail.List()
	if len(entries) != 1 || entries[0].SessionID != "zero" {
		t.Fatalf("zero-value audit trail should keep appended entries, got %#v", entries)
	}
}

func TestSummarizeAuditEntriesFindsLatestTimestamp(t *testing.T) {
	health := SummarizeAuditEntries([]AuditEntry{
		{Timestamp: "2026-05-03T03:00:00Z", Status: AuditStatusCompleted, Summary: ResultSummary{TotalCandidates: 1}},
		{Timestamp: "2026-05-03T03:02:00Z", Status: AuditStatusFailed},
		{Timestamp: "2026-05-03T03:01:00Z", Status: AuditStatusNoCandidates},
	}, AuditOptions{})
	if health.LatestTimestamp != "2026-05-03T03:02:00Z" {
		t.Fatalf("expected max timestamp, got %#v", health)
	}
}

func TestSummarizeAuditEntriesComparesRFC3339Offsets(t *testing.T) {
	health := SummarizeAuditEntries([]AuditEntry{
		{Timestamp: "2026-05-03T10:00:00+08:00", Status: AuditStatusCompleted, Summary: ResultSummary{TotalCandidates: 1}},
		{Timestamp: "2026-05-03T03:00:00Z", Status: AuditStatusFailed},
	}, AuditOptions{})
	if health.LatestTimestamp != "2026-05-03T03:00:00Z" {
		t.Fatalf("expected chronological latest timestamp across offsets, got %#v", health)
	}
}

func TestAuditTrailListReturnsSanitizedDeepCopies(t *testing.T) {
	trail := NewAuditTrailWithOptions(5, AuditOptions{MaxStringLength: 32})
	trail.Append(AuditEntry{
		SessionID: "sess-sk-12345678901234567890",
		Summary:   ResultSummary{SkipReasons: map[string]int{"secret sk-12345678901234567890": 1}},
		Decisions: []Decision{{Quality: QualityReport{Reasons: []string{"stable"}}}},
	})

	first := trail.List()
	if len(first) != 1 {
		t.Fatalf("expected one entry, got %#v", first)
	}
	if strings.Contains(first[0].SessionID, "sk-12345678901234567890") {
		t.Fatalf("list should return sanitized entries, got %#v", first[0])
	}
	first[0].Decisions[0].Quality.Reasons[0] = "mutated"
	for key := range first[0].Summary.SkipReasons {
		delete(first[0].Summary.SkipReasons, key)
	}

	second := trail.List()
	if second[0].Decisions[0].Quality.Reasons[0] == "mutated" || len(second[0].Summary.SkipReasons) == 0 {
		t.Fatalf("list should return deep copies, got %#v", second[0])
	}
}
