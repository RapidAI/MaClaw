package guiapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func TestCodingExperienceEvidenceDigestBindsMaterialLedgerEvents(t *testing.T) {
	now := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	store := codingruntime.NewMemoryStore()
	task, err := store.CreateTask(codingruntime.Task{TaskID: "task-experience", ProjectRef: "repo", Mode: "local", PolicyDigest: "policy-digest"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "runner", time.Minute, codingruntime.PolicySnapshot{Digest: "policy-digest", ProjectRoot: "repo", Mode: "local", ReadOnly: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(attempt.AttemptID, "runner", "verification", "sha256:verification", now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(attempt.AttemptID, "runner", codingruntime.FinishInput{Status: codingruntime.TaskCompleted, SideEffectState: codingruntime.SideEffectNone}, now); err != nil {
		t.Fatal(err)
	}
	digest, err := codingExperienceEvidenceDigest(store, task, attempt)
	if err != nil || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("digest=%q err=%v", digest, err)
	}
	// Changing any material Ledger fact must change the provenance digest.
	store2 := codingruntime.NewMemoryStore()
	task2, _ := store2.CreateTask(*task)
	attempt2, _ := store2.StartAttempt(task2.TaskID, "runner", time.Minute, codingruntime.PolicySnapshot{Digest: "policy-digest", ProjectRoot: "repo", Mode: "local", ReadOnly: true}, now)
	_, _ = store2.AppendEvent(attempt2.AttemptID, "runner", "verification", "sha256:different", now)
	_, _ = store2.FinishAttempt(attempt2.AttemptID, "runner", codingruntime.FinishInput{Status: codingruntime.TaskCompleted, SideEffectState: codingruntime.SideEffectNone}, now)
	changed, err := codingExperienceEvidenceDigest(store2, task2, attempt2)
	if err != nil || digest == changed {
		t.Fatalf("digest=%q changed=%q err=%v", digest, changed, err)
	}
}

func TestCodingExperienceEvidenceDigestRejectsLifecycleOnlyAttempt(t *testing.T) {
	now := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	store := codingruntime.NewMemoryStore()
	task, err := store.CreateTask(codingruntime.Task{TaskID: "task-lifecycle-only", ProjectRef: "repo", Mode: "local", PolicyDigest: "policy"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "runner", time.Minute, codingruntime.PolicySnapshot{Digest: "policy", ProjectRoot: "repo", Mode: "local", ReadOnly: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(attempt.AttemptID, "runner", "attempt_started", "policy", now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(attempt.AttemptID, "runner", codingruntime.FinishInput{Status: codingruntime.TaskCompleted, SideEffectState: codingruntime.SideEffectNone}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := codingExperienceEvidenceDigest(store, task, attempt); err == nil || !strings.Contains(err.Error(), "no material execution evidence") {
		t.Fatalf("expected lifecycle-only evidence rejection, err=%v", err)
	}
}

func TestCodingExperienceEvidenceDigestRejectsUnknownEvidenceEvent(t *testing.T) {
	now := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	store := codingruntime.NewMemoryStore()
	task, err := store.CreateTask(codingruntime.Task{TaskID: "task-unknown-evidence", ProjectRef: "repo", Mode: "local", PolicyDigest: "policy"})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.StartAttempt(task.TaskID, "runner", time.Minute, codingruntime.PolicySnapshot{Digest: "policy", ProjectRoot: "repo", Mode: "local", ReadOnly: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(attempt.AttemptID, "runner", "future_adapter_claim", "sha256:unreviewed", now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(attempt.AttemptID, "runner", codingruntime.FinishInput{Status: codingruntime.TaskCompleted, SideEffectState: codingruntime.SideEffectNone}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := codingExperienceEvidenceDigest(store, task, attempt); err == nil || !strings.Contains(err.Error(), "no material execution evidence") {
		t.Fatalf("expected unknown evidence rejection, err=%v", err)
	}
}

func TestParseExtractedExperienceResponseMapsStructuredFields(t *testing.T) {
	experiences, err := parseExtractedExperienceResponse(`{"experiences":[{"title":"Nil client","category":"pitfall","scope":"universal","trigger_condition":"nil client","content":"Check the client.","failed_attempts":["retried blindly"],"contraindications":["not for owned clients"],"frameworks":["net/http"],"labels":["http"],"project_specific":false}]}`)
	if err != nil {
		t.Fatalf("parseExtractedExperienceResponse: %v", err)
	}
	if len(experiences) != 1 {
		t.Fatalf("experiences = %d, want 1", len(experiences))
	}
	exp := experiences[0]
	if len(exp.FailedAttempts) != 1 || exp.FailedAttempts[0] != "retried blindly" {
		t.Fatalf("failed attempts = %v", exp.FailedAttempts)
	}
	if len(exp.Contraindications) != 1 || exp.Contraindications[0] != "not for owned clients" {
		t.Fatalf("contraindications = %v", exp.Contraindications)
	}
	if len(exp.Frameworks) != 1 || exp.Frameworks[0] != "net/http" {
		t.Fatalf("frameworks = %v", exp.Frameworks)
	}
	if len(exp.Labels) != 1 || exp.Labels[0] != "http" {
		t.Fatalf("labels = %v", exp.Labels)
	}
}

func TestParseExtractedExperienceResponseBoundsModelOutput(t *testing.T) {
	repeatList := func(prefix string, count int) string {
		items := make([]string, count)
		for i := range items {
			items[i] = fmt.Sprintf("%s-%d", prefix, i)
		}
		return strings.Join(items, `","`)
	}
	payload := fmt.Sprintf(`{"experiences":[{"title":"%s","category":"pitfall","scope":"universal","trigger_condition":"%s","content":"%s","code_snippet":"%s","failed_attempts":["%s"],"contraindications":["%s"],"frameworks":["%s"],"labels":["%s"]}]}`,
		strings.Repeat("t", 500),
		strings.Repeat("g", 200),
		strings.Repeat("c", 6000),
		strings.Repeat("s", 3000),
		repeatList("fail", 12),
		repeatList("contra", 12),
		repeatList("fw", 20),
		repeatList("label", 30),
	)
	experiences, err := parseExtractedExperienceResponse(payload)
	if err != nil {
		t.Fatalf("parseExtractedExperienceResponse: %v", err)
	}
	if len(experiences) != 1 {
		t.Fatalf("experiences = %d, want 1", len(experiences))
	}
	exp := experiences[0]
	if got := len([]rune(exp.Title)); got != maxExtractedExperienceTitleRunes {
		t.Fatalf("title runes = %d, want %d", got, maxExtractedExperienceTitleRunes)
	}
	if got := len([]rune(exp.TriggerCondition)); got != maxExtractedExperienceTriggerRunes {
		t.Fatalf("trigger runes = %d, want %d", got, maxExtractedExperienceTriggerRunes)
	}
	if got := len([]rune(exp.Content)); got != maxExtractedExperienceContentRunes {
		t.Fatalf("content runes = %d, want %d", got, maxExtractedExperienceContentRunes)
	}
	if got := len([]rune(exp.CodeSnippet)); got != maxExtractedExperienceCodeSnippetRunes {
		t.Fatalf("snippet runes = %d, want %d", got, maxExtractedExperienceCodeSnippetRunes)
	}
	if len(exp.FailedAttempts) != maxExtractedExperienceListItems || len(exp.Contraindications) != maxExtractedExperienceListItems {
		t.Fatalf("attempts/contraindications = %d/%d, want %d each", len(exp.FailedAttempts), len(exp.Contraindications), maxExtractedExperienceListItems)
	}
	if len(exp.Frameworks) != maxExtractedExperienceFrameworks || len(exp.Labels) != maxExtractedExperienceLabels {
		t.Fatalf("frameworks/labels = %d/%d, want %d/%d", len(exp.Frameworks), len(exp.Labels), maxExtractedExperienceFrameworks, maxExtractedExperienceLabels)
	}
}

func TestBuildExperienceExtractionPromptBoundsInputs(t *testing.T) {
	files := make([]string, 30)
	for i := range files {
		files[i] = fmt.Sprintf("pkg/file-%d.go", i)
	}
	prompt := buildExperienceExtractionPrompt(
		&TaskItem{Title: strings.Repeat("T", 400), Files: files},
		&CodingSubAgentResult{Status: TaskExecFailed, Error: strings.Repeat("E", 2000)},
		t.TempDir(),
	)
	if strings.Contains(prompt, strings.Repeat("T", 250)) {
		t.Fatal("task title was not bounded in the extraction prompt")
	}
	if !strings.Contains(prompt, "还有") || strings.Contains(prompt, "pkg/file-29.go") {
		t.Fatalf("task files were not compacted: %q", prompt)
	}
	if strings.Contains(prompt, strings.Repeat("E", 700)) {
		t.Fatal("result error was not bounded in the extraction prompt")
	}
}

func TestCompactCodingFailureSummaryPrefersErrorLines(t *testing.T) {
	summary := compactCodingFailureSummary("build started\ncompiling...\nmain.go:12: undefined: foo\nFAIL\texample\t0.1s", 160)
	if !strings.Contains(summary, "undefined: foo") || !strings.Contains(summary, "FAIL") {
		t.Fatalf("failure summary = %q, want error lines", summary)
	}
	if strings.Contains(summary, "build started") {
		t.Fatalf("failure summary kept boilerplate: %q", summary)
	}

	cases := []struct {
		name string
		text string
		want []string
	}{
		{
			// The line after --- FAIL carries the actual got/want values.
			name: "go test assertion detail",
			text: "--- FAIL: TestFoo (0.00s)\n    foo_test.go:12: got 1 want 2\nFAIL\nexit status 1",
			want: []string{"foo_test.go:12: got 1 want 2"},
		},
		{
			name: "pytest assertion",
			text: "FAILED tests/test_x.py::test_y\nE   assert 1 == 2",
			want: []string{"E   assert 1 == 2"},
		},
		{
			name: "jest expectation",
			text: "Test suite failed to run\nExpected: 2\nReceived: 1",
			want: []string{"Expected: 2", "Received: 1"},
		},
		{
			name: "rust compiler location",
			text: "error[E0308]: mismatched types\n --> src/main.rs:3:5",
			want: []string{" --> src/main.rs:3:5"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := compactCodingFailureSummary(tc.text, 240)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("failure summary = %q, want %q", got, want)
				}
			}
		})
	}
}

func TestBuildExperienceExtractionPromptIncludesStructuredGuidance(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "package.json"), []byte(`{"dependencies":{"react":"^18"}}`), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	prompt := buildExperienceExtractionPrompt(&TaskItem{Title: "fix client", Files: []string{"main.go"}}, &CodingSubAgentResult{
		Status:   TaskExecFailed,
		DiffStat: &SubAgentDiffStat{FilesChanged: 1, Insertions: 2, Deletions: 1},
		CommandsRun: []CodingSubAgentCommandResult{
			{Command: "go test ./...", Succeeded: false, Summary: "undefined: foo"},
		},
	}, projectDir)
	for _, want := range []string{"contraindications", "frameworks", "undefined: foo", "1 files changed (+2 -1)", "Detected frameworks: react"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("extraction prompt missing %q: %s", want, prompt)
		}
	}
}

func TestCodingExperienceShouldExtract(t *testing.T) {
	passed := &CodingSubAgentResult{Status: TaskExecPassed}
	failed := &CodingSubAgentResult{Status: TaskExecFailed}
	horizon := &CodingSubAgentResult{Status: TaskExecPassed, HorizonOwned: true}

	if codingExperienceShouldExtract(ExperienceSaveModeOff, ExperienceStrategyOnSuccess, passed, false) {
		t.Fatal("off mode must not extract")
	}
	if codingExperienceShouldExtract(ExperienceSaveModeObserve, ExperienceStrategyOff, passed, false) {
		t.Fatal("off strategy must not extract")
	}
	if codingExperienceShouldExtract(ExperienceSaveModeAuto, ExperienceStrategyOnSuccess, horizon, false) {
		t.Fatal("horizon-owned results must not extract")
	}
	asked := &CodingSubAgentResult{Status: TaskExecSkipped, AskQuestion: "Which database should I use?"}
	if codingExperienceShouldExtract(ExperienceSaveModeObserve, ExperienceStrategyAlways, asked, false) {
		t.Fatal("a user-input pause must not extract")
	}
	if codingExperienceShouldExtract(ExperienceSaveModeObserve, ExperienceStrategyOnRetry, passed, false) {
		t.Fatal("on_retry_success must wait for a retry")
	}
	if !codingExperienceShouldExtract(ExperienceSaveModeObserve, ExperienceStrategyOnRetry, passed, true) {
		t.Fatal("on_retry_success should extract after a retry")
	}
	if !codingExperienceShouldExtract(ExperienceSaveModeAuto, ExperienceStrategyOnSuccess, passed, false) {
		t.Fatal("auto/on_success should extract a passing task")
	}
	if codingExperienceShouldExtract(ExperienceSaveModeAuto, ExperienceStrategyOnSuccess, failed, false) {
		t.Fatal("on_success must not extract a failed task")
	}
	if !codingExperienceShouldExtract(ExperienceSaveModeObserve, ExperienceStrategyAlways, failed, false) {
		t.Fatal("always should extract failures")
	}
}

func TestCodingResultLooksLikeRetrySuccess(t *testing.T) {
	if codingResultLooksLikeRetrySuccess(&CodingSubAgentResult{
		CommandsRun: []CodingSubAgentCommandResult{{Command: "go test", Succeeded: true}},
	}) {
		t.Fatal("first-try success is not a retry")
	}
	if !codingResultLooksLikeRetrySuccess(&CodingSubAgentResult{
		CommandsRun: []CodingSubAgentCommandResult{
			{Command: "go test", Succeeded: false},
			{Command: "go test", Succeeded: true},
		},
	}) {
		t.Fatal("failed then succeeded commands should count as retry success")
	}
	if codingResultLooksLikeRetrySuccess(&CodingSubAgentResult{
		CommandsRun: []CodingSubAgentCommandResult{
			{Command: "ls", Succeeded: false},
			{Command: "go test", Succeeded: true},
		},
	}) {
		t.Fatal("an unrelated failed command must not count as retry success")
	}
}

func TestIsSimilarExperienceNormalizesTriggerTokens(t *testing.T) {
	existing := knowledge.CodingExperience{Title: "Alpha", TriggerCondition: "HTTP client, reuse"}
	if !isSimilarExperience(existing, knowledge.CodingExperience{Title: "Beta", TriggerCondition: "reuse http-client"}) {
		t.Fatal("reordered/punctuated trigger should identify the same experience")
	}
	if isSimilarExperience(existing, knowledge.CodingExperience{Title: "Gamma", TriggerCondition: "reuse database-client"}) {
		t.Fatal("different trigger tokens must not be treated as the same experience")
	}
}

func TestIsSimilarExperienceUsesContentWhenTriggerMissing(t *testing.T) {
	body := strings.Repeat("always set http client timeouts in production. ", 3)
	existing := knowledge.CodingExperience{Title: "timeouts", Content: body}
	dup := knowledge.CodingExperience{Title: "http timeouts", Content: body}
	if !isSimilarExperience(existing, dup) {
		t.Fatal("identical content should be treated as a duplicate")
	}
	if isSimilarExperience(existing, knowledge.CodingExperience{Title: "other", Content: "unrelated guidance about maps"}) {
		t.Fatal("unrelated content must not match")
	}
	short := strings.Repeat("timeout ", 8)
	long := short + strings.Repeat("and many extra unrelated details about maps and interfaces. ", 8)
	if isSimilarExperience(knowledge.CodingExperience{Title: "a", Content: short}, knowledge.CodingExperience{Title: "b", Content: long}) {
		t.Fatal("a much longer experience must not be treated as a duplicate of a short fragment")
	}
}

func TestInferRemoteCodingLanguageDoesNotHardcodePython(t *testing.T) {
	if got := inferRemoteCodingLanguage(&remoteCodingCallbacks{}); got != "" {
		t.Fatalf("empty files should leave language unset, got %q", got)
	}
	if got := inferRemoteCodingLanguage(&remoteCodingCallbacks{filesModified: []string{"pkg/foo.go"}}); got != "go" {
		t.Fatalf("go files should infer go, got %q", got)
	}
}

func TestRecalledExperienceIDsIgnoresCandidates(t *testing.T) {
	ids := recalledExperienceIDs([]knowledge.CodingExperience{
		{ID: "active-1", Status: knowledge.CodingStatusActive},
		{ID: "verified-1", Status: knowledge.CodingStatusVerified},
		{ID: "candidate-1", Status: knowledge.CodingStatusCandidate},
		{ID: "empty-status"},
	})
	if len(ids) != 2 || ids[0] != "active-1" || ids[1] != "verified-1" {
		t.Fatalf("recalled IDs = %#v", ids)
	}
}

func TestFinishCodingKnowledgeSearchPreservesLocalError(t *testing.T) {
	localErr := fmt.Errorf("db closed")
	_, err := finishCodingKnowledgeSearch(nil, context.Background(), nil, localErr, "query", "", "", 5)
	if err != localErr {
		t.Fatalf("nil app should keep local search error, got %v", err)
	}
	_, err = finishCodingKnowledgeSearch(&App{}, context.Background(), nil, localErr, "query", "", "", 5)
	if err == nil || !strings.Contains(err.Error(), "db closed") {
		t.Fatalf("empty enterprise merge should not hide local error, got %v", err)
	}
}

func TestRemoteResultAsCodingSubAgentResultMapsSuccess(t *testing.T) {
	got := remoteResultAsCodingSubAgentResult(&RemoteCodingSubAgentResult{
		Status:                "success",
		Summary:               "done",
		RuntimeTaskID:         "task-1",
		RecalledExperienceIDs: []string{"exp-1"},
	})
	if got.Status != TaskExecPassed || got.RuntimeTaskID != "task-1" || got.RecalledExperienceIDs[0] != "exp-1" {
		t.Fatalf("mapped result = %+v", got)
	}
}

func createTestRuntimeProvenance(t *testing.T, app *App, prefix string) codingExperienceProvenance {
	t.Helper()
	ledger := app.ensureCodingRuntimeStore()
	if ledger == nil {
		t.Fatal("coding runtime ledger unavailable")
	}
	now := time.Now().UTC()
	taskID := fmt.Sprintf("%s-%d", prefix, now.UnixNano())
	task, err := ledger.CreateTask(codingruntime.Task{TaskID: taskID, ProjectRef: "repo", Mode: "local", PolicyDigest: "policy"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	attempt, err := ledger.StartAttempt(task.TaskID, "test", time.Minute, codingruntime.PolicySnapshot{Digest: "policy", ProjectRoot: "repo", Mode: "local"}, now)
	if err != nil {
		t.Fatalf("StartAttempt: %v", err)
	}
	if _, err := ledger.AppendEvent(attempt.AttemptID, "test", "verification", "sha256:"+taskID, now); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if _, err := ledger.FinishAttempt(attempt.AttemptID, "test", codingruntime.FinishInput{Status: codingruntime.TaskCompleted, SideEffectState: codingruntime.SideEffectConfirmed}, now); err != nil {
		t.Fatalf("FinishAttempt: %v", err)
	}
	provenance, err := codingruntime.ResolveExperienceProvenance(ledger, task.TaskID)
	if err != nil {
		t.Fatalf("ResolveExperienceProvenance: %v", err)
	}
	return codingExperienceProvenance{TaskID: provenance.TaskID, AttemptID: provenance.AttemptID, EvidenceDigest: provenance.EvidenceDigest}
}

func TestResolveCodingKnowledgeAutoPromoteObservations(t *testing.T) {
	cases := []struct {
		configured int
		want       int
	}{
		{0, 2},
		{1, 2},
		{2, 2},
		{3, 3},
		{99, 20},
	}
	for _, tc := range cases {
		got := resolveCodingKnowledgeAutoPromoteObservations(corelib.AppConfig{CodingKnowledgeAutoPromoteObservations: tc.configured})
		if got != tc.want {
			t.Fatalf("configured %d resolved to %d, want %d", tc.configured, got, tc.want)
		}
	}
}

func TestCodingKnowledgeFinalizationStatus(t *testing.T) {
	cases := []struct {
		name   string
		result *CodingSubAgentResult
		want   TaskExecStatus
		ok     bool
	}{
		{"passed", &CodingSubAgentResult{Status: TaskExecPassed, RuntimeTaskID: "task"}, TaskExecPassed, true},
		{"failed", &CodingSubAgentResult{Status: TaskExecFailed, RuntimeTaskID: "task"}, TaskExecFailed, true},
		{"skipped", &CodingSubAgentResult{Status: TaskExecSkipped, RuntimeTaskID: "task"}, TaskExecSkipped, true},
		{"waiting approval", &CodingSubAgentResult{Status: TaskExecWaitingApproval, RuntimeTaskID: "task"}, TaskExecWaitingApproval, false},
		{"waiting child", &CodingSubAgentResult{Status: TaskExecWaitingChild, RuntimeTaskID: "task"}, TaskExecWaitingChild, false},
		{"horizon", &CodingSubAgentResult{Status: TaskExecPassed, RuntimeTaskID: "task", HorizonOwned: true}, "", false},
		{"no ledger", &CodingSubAgentResult{Status: TaskExecPassed}, "", false},
		{"quality failed", &CodingSubAgentResult{Status: TaskExecPassed, RuntimeTaskID: "task", QualityStatus: codingSubAgentQualityFailed}, TaskExecFailed, true},
	}
	for _, tc := range cases {
		status, ok := codingKnowledgeFinalizationStatus(tc.result)
		if status != tc.want || ok != tc.ok {
			t.Fatalf("%s: status=%q ok=%v, want %q/%v", tc.name, status, ok, tc.want, tc.ok)
		}
	}
}

func TestFindDuplicateExperienceScopesByProjectAndLanguage(t *testing.T) {
	store, err := knowledge.NewCodingKnowledgeStore(filepath.Join(t.TempDir(), "coding_knowledge.db"))
	if err != nil {
		t.Fatalf("NewCodingKnowledgeStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	base := knowledge.CodingExperience{
		Title:            "Shared rule",
		Category:         knowledge.CodingCategoryPattern,
		TriggerCondition: "shared rule",
		Content:          "Always do the shared thing.",
	}
	projectA := base
	projectA.Scope = knowledge.CodingScopeProject
	projectA.ProjectPath = "/repo/a"
	projectA.Status = knowledge.CodingStatusActive
	if _, err := store.SaveExperience(ctx, projectA); err != nil {
		t.Fatalf("SaveExperience project A: %v", err)
	}
	if _, ok := findDuplicateExperience(ctx, store, projectA); !ok {
		t.Fatal("same project rule should be a duplicate")
	}
	projectB := projectA
	projectB.ProjectPath = "/repo/b"
	if _, ok := findDuplicateExperience(ctx, store, projectB); ok {
		t.Fatal("another project's rule must not be a duplicate")
	}

	goRule := base
	goRule.Scope = knowledge.CodingScopeLanguage
	goRule.Language = "go"
	goRule.Status = knowledge.CodingStatusActive
	if _, err := store.SaveExperience(ctx, goRule); err != nil {
		t.Fatalf("SaveExperience go: %v", err)
	}
	pythonRule := goRule
	pythonRule.Language = "python"
	if _, ok := findDuplicateExperience(ctx, store, pythonRule); ok {
		t.Fatal("another language's rule must not be a duplicate")
	}
	if _, ok := findDuplicateExperience(ctx, store, goRule); !ok {
		t.Fatal("same language rule should be a duplicate")
	}
}

func TestFindDuplicateExperienceFallsBackToContentQuery(t *testing.T) {
	store, err := knowledge.NewCodingKnowledgeStore(filepath.Join(t.TempDir(), "coding_knowledge.db"))
	if err != nil {
		t.Fatalf("NewCodingKnowledgeStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	saved, err := store.SaveRuntimeExperience(ctx, knowledge.CodingExperience{
		Title:                  "Alpha guidance",
		Category:               knowledge.CodingCategoryPattern,
		Scope:                  knowledge.CodingScopeUniversal,
		TriggerCondition:       "alpha trigger",
		Content:                "Check the pooled HTTP client before reusing a connection.",
		SourceRuntimeTaskID:    "task-alpha",
		SourceRuntimeAttemptID: "attempt-alpha",
		EvidenceDigest:         "sha256:alpha",
	})
	if err != nil {
		t.Fatalf("SaveRuntimeExperience: %v", err)
	}
	got, ok := findDuplicateExperience(ctx, store, knowledge.CodingExperience{
		Title:            "Beta guidance",
		Category:         knowledge.CodingCategoryPattern,
		Scope:            knowledge.CodingScopeUniversal,
		TriggerCondition: "beta trigger",
		Content:          saved.Content,
	})
	if !ok || got.ID != saved.ID {
		t.Fatalf("content-based dedup = %+v/%v, want id %s", got, ok, saved.ID)
	}
}

func TestRecordMainAgentCodingKnowledgeOutcomesKeepsTransientFailureIDs(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { closeCodingKnowledgeStore(t, app) })
	loop := NewLoopContext("main-agent-transient-test", 1, nil)
	loop.noteCodingKnowledgeRecallIDs(loop.SemanticTurnGeneration(), []string{"exp-1"})
	handler := &IMMessageHandler{app: app}

	recordMainAgentCodingKnowledgeOutcomes(handler, loop, &CodingSubAgentResult{
		Status:        TaskExecFailed,
		RuntimeTaskID: "task-1",
		Error:         "LLM provider returned HTTP 502 bad gateway",
	}, loop.SemanticTurnGeneration())

	ids := loop.snapshotCodingKnowledgeRecallIDs()
	if len(ids) != 1 || ids[0] != "exp-1" {
		t.Fatalf("transient failure consumed recalled IDs: %v", ids)
	}
}

func TestRecordMainAgentCodingKnowledgeOutcomes(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { closeCodingKnowledgeStore(t, app) })
	store := app.ensureCodingKnowledgeStore()
	ctx := context.Background()
	exp, err := store.SaveExperience(ctx, knowledge.CodingExperience{
		Title:            "Main agent recalled rule",
		Category:         knowledge.CodingCategoryPattern,
		Scope:            knowledge.CodingScopeUniversal,
		TriggerCondition: "main agent recall",
		Content:          "Guidance recalled by the main agent.",
		Status:           knowledge.CodingStatusActive,
	})
	if err != nil {
		t.Fatalf("SaveExperience: %v", err)
	}
	provenance := createTestRuntimeProvenance(t, app, "main-agent-outcome")
	loop := NewLoopContext("main-agent-recall-test", 1, nil)
	loop.noteCodingKnowledgeRecallIDs(loop.SemanticTurnGeneration(), []string{exp.ID})

	handler := &IMMessageHandler{app: app}
	recordMainAgentCodingKnowledgeOutcomes(handler, loop, &CodingSubAgentResult{
		Status:        TaskExecPassed,
		RuntimeTaskID: provenance.TaskID,
	}, loop.SemanticTurnGeneration())
	if ids := loop.snapshotCodingKnowledgeRecallIDs(); len(ids) != 0 {
		t.Fatalf("recalled IDs were not consumed: %v", ids)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := store.GetExperience(ctx, exp.ID)
		if err == nil && got.RecallCount >= 1 && got.SuccessCount >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("recall outcome was not recorded: exp=%+v err=%v", got, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRecordDuplicateExperienceObservationPromotesInAutoMode(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { closeCodingKnowledgeStore(t, app) })
	store := app.ensureCodingKnowledgeStore()
	ctx := context.Background()
	source := createTestRuntimeProvenance(t, app, "observation-source")
	saved, err := store.SaveRuntimeExperience(ctx, knowledge.CodingExperience{
		Title:                  "Nil HTTP client",
		Category:               knowledge.CodingCategoryPitfall,
		Scope:                  knowledge.CodingScopeUniversal,
		TriggerCondition:       "nil http client",
		Content:                "Check the pooled client before reuse.",
		SourceTaskTitle:        "first task",
		SourceRuntimeTaskID:    source.TaskID,
		SourceRuntimeAttemptID: source.AttemptID,
		EvidenceDigest:         source.EvidenceDigest,
	})
	if err != nil {
		t.Fatalf("SaveRuntimeExperience: %v", err)
	}
	runner := &SubAgentTaskRunner{handler: &IMMessageHandler{app: app}}
	incoming := knowledge.CodingExperience{
		Title:          "Nil HTTP client",
		FailedAttempts: []string{"Retried without checking the client."},
		Labels:         []string{"http"},
	}

	observeProvenance := createTestRuntimeProvenance(t, app, "observation-observe")
	runner.recordDuplicateExperienceObservation(ctx, store, saved, incoming, observeProvenance, ExperienceSaveModeObserve, corelib.AppConfig{})
	got, err := store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatalf("GetExperience: %v", err)
	}
	if got.Status != knowledge.CodingStatusCandidate || got.ObservationCount != 2 {
		t.Fatalf("observe mode = status %q observations %d, want candidate/2", got.Status, got.ObservationCount)
	}

	autoProvenance := createTestRuntimeProvenance(t, app, "observation-auto")
	runner.recordDuplicateExperienceObservation(ctx, store, got, incoming, autoProvenance, ExperienceSaveModeAuto, corelib.AppConfig{})
	got, err = store.GetExperience(ctx, saved.ID)
	if err != nil {
		t.Fatalf("GetExperience after auto: %v", err)
	}
	if got.Status != knowledge.CodingStatusActive {
		t.Fatalf("auto mode status = %q, want active", got.Status)
	}
	if got.ObservationCount != 3 {
		t.Fatalf("auto mode observations = %d, want 3", got.ObservationCount)
	}
}

func TestLocalCodingKnowledgePromptInjectsActiveExperience(t *testing.T) {
	store, err := knowledge.NewCodingKnowledgeStore(filepath.Join(t.TempDir(), "coding_knowledge.db"))
	if err != nil {
		t.Fatalf("NewCodingKnowledgeStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	if _, err := store.SaveExperience(ctx, knowledge.CodingExperience{
		Title:            "Nil HTTP client",
		Category:         knowledge.CodingCategoryPitfall,
		Scope:            knowledge.CodingScopeUniversal,
		TriggerCondition: "nil http client",
		Content:          "Check the pooled HTTP client before reusing a connection.",
		FailedAttempts:   []string{"Retried without checking the client."},
		Status:           knowledge.CodingStatusActive,
	}); err != nil {
		t.Fatalf("SaveExperience: %v", err)
	}
	callbacks := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{codingKB: store, projectPath: "/repo"},
		task:     &TaskItem{Title: "nil http client panic", Files: []string{"main.go"}},
	}
	sections := callbacks.buildKnowledgePromptSections()
	if !strings.Contains(sections, "相关编码经验") ||
		!strings.Contains(sections, "Nil HTTP client") ||
		!strings.Contains(sections, "避免:") {
		t.Fatalf("local knowledge prompt = %q", sections)
	}
	if ids := callbacks.snapshotRecalledExperienceIDs(); len(ids) != 1 {
		t.Fatalf("recalled experience IDs = %v, want one", ids)
	}
}

func TestCodingKnowledgeExtractionAllowedSkipsInquiry(t *testing.T) {
	if codingKnowledgeExtractionAllowed(&TaskItem{RequestKind: codingRequestInquiry}) {
		t.Fatal("inquiry turns must not extract execution experience")
	}
	if !codingKnowledgeExtractionAllowed(&TaskItem{RequestKind: codingRequestImplementation}) {
		t.Fatal("implementation turns should extract experience")
	}
	if !codingKnowledgeExtractionAllowed(nil) {
		t.Fatal("legacy callers without a task keep extraction")
	}
}

func TestCodingKnowledgeProjectPathUsesMainProjectForWorktree(t *testing.T) {
	if got := codingKnowledgeProjectPath("/main", nil); got != "/main" {
		t.Fatalf("plain project path = %q, want /main", got)
	}
	if got := codingKnowledgeProjectPath("/worktree", &guiCodingRuntimeOptions{PolicyProjectRoot: "/main"}); got != "/main" {
		t.Fatalf("worktree project path = %q, want /main", got)
	}
	if got := codingKnowledgeProjectPath("", &guiCodingRuntimeOptions{}); got != "" {
		t.Fatalf("empty project path = %q, want empty", got)
	}
}

func TestCodingKnowledgeResultIsTransientFailure(t *testing.T) {
	cases := []struct {
		name   string
		result *CodingSubAgentResult
		want   bool
	}{
		{"provider 502", &CodingSubAgentResult{Status: TaskExecFailed, Error: "LLM provider returned HTTP 502 bad gateway"}, true},
		{"provider timeout", &CodingSubAgentResult{Status: TaskExecFailed, Error: "LLM API context deadline exceeded"}, true},
		{"rate limit", &CodingSubAgentResult{Status: TaskExecFailed, Error: "rate limit exceeded"}, true},
		{"unknown status with provider failure", &CodingSubAgentResult{Status: "bogus", Error: "LLM provider HTTP 503"}, true},
		{"code failure", &CodingSubAgentResult{Status: TaskExecFailed, Error: "go test ./... failed: exit status 1"}, false},
		{"passed with provider error text", &CodingSubAgentResult{Status: TaskExecPassed, Error: "LLM provider HTTP 502"}, false},
		{"skipped", &CodingSubAgentResult{Status: TaskExecSkipped, Error: "LLM provider HTTP 502"}, false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		if got := codingKnowledgeResultIsTransientFailure(tc.result); got != tc.want {
			t.Fatalf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestFinalizeLocalCodingKnowledgeMarksTerminalResult(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { closeCodingKnowledgeStore(t, app) })
	handler := &IMMessageHandler{app: app}

	terminal := &CodingSubAgentResult{Status: TaskExecFailed, RuntimeTaskID: "missing-task"}
	finalizeLocalCodingKnowledge(handler, corelib.MaclawLLMConfig{}, nil, &TaskItem{Title: "task"}, terminal, "/repo", "/repo")
	if !terminal.knowledgeFinalized {
		t.Fatal("terminal result should be marked finalized")
	}

	waiting := &CodingSubAgentResult{Status: TaskExecWaitingApproval, RuntimeTaskID: "missing-task"}
	finalizeLocalCodingKnowledge(handler, corelib.MaclawLLMConfig{}, nil, &TaskItem{Title: "task"}, waiting, "/repo", "/repo")
	if waiting.knowledgeFinalized {
		t.Fatal("waiting result must not be marked finalized")
	}
}

func TestLocalCodingKnowledgePromptUsesPreviousFailureFallback(t *testing.T) {
	store, err := knowledge.NewCodingKnowledgeStore(filepath.Join(t.TempDir(), "coding_knowledge.db"))
	if err != nil {
		t.Fatalf("NewCodingKnowledgeStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	if _, err := store.SaveExperience(ctx, knowledge.CodingExperience{
		Title:            "Nil HTTP client",
		Category:         knowledge.CodingCategoryPitfall,
		Scope:            knowledge.CodingScopeUniversal,
		TriggerCondition: "nil http client",
		Content:          "Check the pooled HTTP client before reusing a connection.",
		Status:           knowledge.CodingStatusActive,
	}); err != nil {
		t.Fatalf("SaveExperience: %v", err)
	}
	callbacks := &codingSubAgentCallbacks{
		subagent:    &CodingSubAgent{codingKB: store, projectPath: "/repo"},
		task:        &TaskItem{Title: "unrelated maintenance task", Files: []string{"main.go"}},
		prevOutputs: []string{"Previous attempt failed: nil http client panic when reusing the pool."},
	}
	sections := callbacks.buildKnowledgePromptSections()
	if !strings.Contains(sections, "Nil HTTP client") {
		t.Fatalf("previous-failure fallback prompt = %q", sections)
	}
	if ids := callbacks.snapshotRecalledExperienceIDs(); len(ids) != 1 {
		t.Fatalf("recalled experience IDs = %v, want one", ids)
	}
}

func TestCodingExperienceKnowledgeTerminalStatus(t *testing.T) {
	for _, status := range []codingruntime.TaskStatus{codingruntime.TaskCompleted, codingruntime.TaskFailed, codingruntime.TaskBlocked} {
		if !codingExperienceKnowledgeTerminalStatus(status) {
			t.Fatalf("status %q should be knowledge eligible", status)
		}
	}
	for _, status := range []codingruntime.TaskStatus{codingruntime.TaskCancelled, codingruntime.TaskInterrupted, codingruntime.TaskRunning} {
		if codingExperienceKnowledgeTerminalStatus(status) {
			t.Fatalf("status %q should not be knowledge eligible", status)
		}
	}
}
