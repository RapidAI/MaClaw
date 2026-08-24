package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestExtractCodingSubAgentBugSignal(t *testing.T) {
	s := extractCodingSubAgentBugSignal(`panic: "database connection failed" at gui/app.go:42 expected response, actual crash; 复现：点击保存`)
	if len(s.StackFrames) != 1 || s.StackFrames[0] != "gui/app.go:42" {
		t.Fatalf("stack=%v", s.StackFrames)
	}
	if len(s.ErrorStrings) == 0 || s.Reproduction == "" {
		t.Fatalf("signal=%+v", s)
	}
}

func TestRankLocalizationCandidates(t *testing.T) {
	got := rankLocalizationCandidates([]CodingSubAgentLocalizationCandidate{
		{File: "weak.go", Score: 1, ContradictingEvidence: []string{"not on path"}},
		{File: "root.go", Symbol: "Run", Score: 2, SupportingEvidence: []string{"stack", "caller"}},
	})
	if got[0].File != "root.go" {
		t.Fatalf("ranked=%+v", got)
	}
}

func TestLocalizationCandidatesFromOutputFindsAllPathsDeterministically(t *testing.T) {
	output := "caller C:\\repo\\a.go:12 invokes pkg/b.go:34\nother pkg/c.go:9"
	first := localizationCandidatesFromOutput(output)
	second := localizationCandidatesFromOutput(output)
	if len(first) != 3 || len(second) != 3 {
		t.Fatalf("expected every path on each line, first=%+v second=%+v", first, second)
	}
	for i := range first {
		if first[i].File != second[i].File {
			t.Fatalf("candidate ordering must be deterministic: first=%+v second=%+v", first, second)
		}
	}
	foundWindows := false
	for _, candidate := range first {
		if candidate.File == "C:/repo/a.go" {
			foundWindows = true
		}
	}
	if !foundWindows {
		t.Fatalf("Windows drive-qualified path was not preserved: %+v", first)
	}
}

func TestRankLocalizationCandidatesIsIdempotent(t *testing.T) {
	in := []CodingSubAgentLocalizationCandidate{
		{File: "root.go", Symbol: "Run", Score: 2, SupportingEvidence: []string{"stack", "caller"}},
		{File: "weak.go", Score: 3, ContradictingEvidence: []string{"not on path"}},
	}
	once := rankLocalizationCandidates(in)
	twice := rankLocalizationCandidates(once)
	if len(once) != len(twice) || once[0].File != twice[0].File || once[0].Score != twice[0].Score {
		t.Fatalf("ranking changed after second pass: once=%+v twice=%+v", once, twice)
	}
}

func TestValidateLocalizationEvidenceRejectsBlankAndNonFiniteValues(t *testing.T) {
	valid := CodingSubAgentLocalizationEvidence{
		RootCauseFile: "root.go", CausalPath: []string{"request -> root.Run"},
		Reproduction: "focused test fails", SupportingEvidence: []string{"stack points to root.go"},
		ResearchDecision: "not_needed", ResearchReason: "repository control flow is sufficient", Confidence: .8,
	}
	tests := []struct {
		name   string
		mutate func(*CodingSubAgentLocalizationEvidence)
	}{
		{"blank causal path", func(e *CodingSubAgentLocalizationEvidence) { e.CausalPath = []string{"  "} }},
		{"blank supporting evidence", func(e *CodingSubAgentLocalizationEvidence) { e.SupportingEvidence = []string{"\t"} }},
		{"blank external source", func(e *CodingSubAgentLocalizationEvidence) {
			e.ResearchDecision, e.ResearchReason, e.ExternalSources = "searched", "checked vendor docs", []string{" "}
		}},
		{"nan confidence", func(e *CodingSubAgentLocalizationEvidence) { e.Confidence = math.NaN() }},
		{"infinite confidence", func(e *CodingSubAgentLocalizationEvidence) { e.Confidence = math.Inf(1) }},
		{"zero confidence", func(e *CodingSubAgentLocalizationEvidence) { e.Confidence = 0 }},
		{"negative confidence", func(e *CodingSubAgentLocalizationEvidence) { e.Confidence = -.1 }},
		{"blank candidate file", func(e *CodingSubAgentLocalizationEvidence) {
			e.Candidates = []CodingSubAgentLocalizationCandidate{{File: " ", Score: .5, SupportingEvidence: []string{"caller"}}}
		}},
		{"dot root path", func(e *CodingSubAgentLocalizationEvidence) { e.RootCauseFile = "." }},
		{"root trailing separator", func(e *CodingSubAgentLocalizationEvidence) { e.RootCauseFile = "pkg/" }},
		{"root parent traversal", func(e *CodingSubAgentLocalizationEvidence) { e.RootCauseFile = "pkg/../root.go" }},
		{"dot candidate path", func(e *CodingSubAgentLocalizationEvidence) {
			e.Candidates = []CodingSubAgentLocalizationCandidate{{File: ".", Score: .5, SupportingEvidence: []string{"caller"}}}
		}},
		{"candidate parent traversal", func(e *CodingSubAgentLocalizationEvidence) {
			e.Candidates = []CodingSubAgentLocalizationCandidate{{File: `pkg\..\candidate.go`, Score: .5, SupportingEvidence: []string{"caller"}}}
		}},
		{"duplicate candidate path", func(e *CodingSubAgentLocalizationEvidence) {
			e.Candidates = []CodingSubAgentLocalizationCandidate{
				{File: "pkg/candidate.go", Score: .5, SupportingEvidence: []string{"caller"}},
				{File: "pkg/candidate.go", Score: .4, SupportingEvidence: []string{"stack"}},
			}
		}},
		{"blank candidate evidence", func(e *CodingSubAgentLocalizationEvidence) {
			e.Candidates = []CodingSubAgentLocalizationCandidate{{File: "candidate.go", Score: .5, SupportingEvidence: []string{" "}}}
		}},
		{"nan candidate score", func(e *CodingSubAgentLocalizationEvidence) {
			e.Candidates = []CodingSubAgentLocalizationCandidate{{File: "candidate.go", Score: math.NaN(), SupportingEvidence: []string{"caller"}}}
		}},
		{"zero supported candidate score", func(e *CodingSubAgentLocalizationEvidence) {
			e.Candidates = []CodingSubAgentLocalizationCandidate{{File: "candidate.go", SupportingEvidence: []string{"caller"}}}
		}},
		{"negative supported candidate score", func(e *CodingSubAgentLocalizationEvidence) {
			e.Candidates = []CodingSubAgentLocalizationCandidate{{File: "candidate.go", Score: -.1, SupportingEvidence: []string{"caller"}}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := valid
			tt.mutate(&e)
			if err := validateLocalizationEvidence(&e, ""); err == nil {
				t.Fatal("expected malformed evidence to be rejected")
			}
		})
	}
}

func TestReportLocalizationRejectsMissingConfidence(t *testing.T) {
	_, err := localizationEvidenceFromArgs(map[string]interface{}{
		"root_cause_file": "root.go", "causal_path": []interface{}{"request -> root.Run"},
		"reproduction": "focused test fails", "supporting_evidence": []interface{}{"stack points to root.go"},
		"research_decision": "not_needed", "research_reason": "repository-only",
	}, CodingSubAgentBugSignal{})
	if err == nil || !strings.Contains(err.Error(), "confidence") {
		t.Fatalf("missing confidence must not silently become an edit-authorizing zero value, got %v", err)
	}
}

func TestValidateLocalizationEvidenceAllowsRejectedCandidate(t *testing.T) {
	e := &CodingSubAgentLocalizationEvidence{
		RootCauseFile: "root.go", CausalPath: []string{"request -> root.Run"},
		Reproduction: "focused test fails", SupportingEvidence: []string{"stack points to root.go"},
		ResearchDecision: "not_needed", ResearchReason: "repository control flow is sufficient", Confidence: .8,
		Candidates: []CodingSubAgentLocalizationCandidate{{File: "rejected.go", Score: .2, ContradictingEvidence: []string{"not reachable from failing path"}}},
	}
	if err := validateLocalizationEvidence(e, ""); err != nil {
		t.Fatalf("a candidate retained to document falsification should be valid: %v", err)
	}
}

func TestLocalizationStateDeepCopiesAndNormalizesEvidence(t *testing.T) {
	e := CodingSubAgentLocalizationEvidence{
		Signal:        CodingSubAgentBugSignal{EntryPoints: []string{" handler.Run ", "handler.Run"}},
		RootCauseFile: " root.go ", CausalPath: []string{" request -> root.Run ", "request -> root.Run"},
		Reproduction: " focused test fails ", SupportingEvidence: []string{" stack ", "stack"},
		ResearchDecision: " NOT_NEEDED ", ResearchReason: " repository-only ", Confidence: .8,
		Candidates: []CodingSubAgentLocalizationCandidate{{File: " candidate.go ", Score: .5, SupportingEvidence: []string{" caller "}}},
	}
	var state codingSubAgentLocalizationState
	state.set(e)
	e.CausalPath[0] = "mutated"
	e.Signal.EntryPoints[0] = "mutated"
	e.Candidates[0].SupportingEvidence[0] = "mutated"
	first := state.snapshot()
	if first.RootCauseFile != "root.go" || len(first.CausalPath) != 1 || first.CausalPath[0] != "request -> root.Run" || len(first.Signal.EntryPoints) != 1 || first.Signal.EntryPoints[0] != "handler.Run" {
		t.Fatalf("stored evidence was not normalized/deep-copied: %+v", first)
	}
	first.CausalPath[0] = "snapshot mutation"
	first.Signal.EntryPoints[0] = "snapshot mutation"
	first.Candidates[0].SupportingEvidence[0] = "snapshot mutation"
	second := state.snapshot()
	if second.CausalPath[0] == "snapshot mutation" || second.Signal.EntryPoints[0] == "snapshot mutation" || second.Candidates[0].SupportingEvidence[0] == "snapshot mutation" {
		t.Fatalf("snapshot leaked nested slices: %+v", second)
	}
}

func TestBugFixExistingEditRequiresLocalization(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bug.go")
	if err := os.WriteFile(path, []byte("package bug\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: dir}, task: &TaskItem{Title: "fix crash bug", Description: "panic on save"}}
	if got := cb.requireLocalizationBeforeExistingBugEdit(path, false); !strings.Contains(got, "report_localization") {
		t.Fatalf("expected gate, got %q", got)
	}
	cb.localization.set(CodingSubAgentLocalizationEvidence{RootCauseFile: "bug.go", CausalPath: []string{"save -> bug.Run"}, Reproduction: "focused test fails before fix", SupportingEvidence: []string{"stack points to bug.go"}, ResearchDecision: "not_needed", ResearchReason: "root cause is fully proven by repository control flow", Confidence: .9})
	if got := cb.requireLocalizationBeforeExistingBugEdit(path, false); got != "" {
		t.Fatalf("unexpected gate: %s", got)
	}
}

func TestHorizonCLIEditSkipsLocalizationGate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bug.go")
	if err := os.WriteFile(path, []byte("package bug\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: dir, horizonPosture: true},
		task:     &TaskItem{Title: "fix crash bug", Description: "panic on save"},
	}
	if got := cb.requireLocalizationBeforeExistingBugEdit(path, false); got != "" {
		t.Fatalf("horizon CLI must skip localization gate, got %q", got)
	}
}

func TestCodeNavigationFallbackFindsDefinition(t *testing.T) {
	if _, err := os.Stat(filepath.Join("..", ".codegraph")); err != nil {
		t.Skip("workspace CodeGraph state unavailable")
	}
	backend, output, err := runLocalCodeNavigation("..", "definition", "summarizeSubAgentExploration", 2)
	if err != nil {
		t.Fatal(err)
	}
	if backend == "" || !strings.Contains(output, "summarizeSubAgentExploration") {
		t.Fatalf("backend=%q output=%q", backend, output)
	}
}

func TestLocalizationFocusedTestSuggestions(t *testing.T) {
	got := localizationFocusedTestSuggestions("gui/coding_subagent.go", "TestBug")
	if len(got) == 0 || !strings.Contains(got[0], "go test") {
		t.Fatalf("got=%v", got)
	}
}

func TestExternalResearchTriggerIsNarrowAndVersionAware(t *testing.T) {
	for _, text := range []string{
		"fix unknown vendor SDK error after upgrade",
		"第三方依赖升级后出现陌生协议错误",
	} {
		if !codingTaskNeedsExternalResearch(text) {
			t.Fatalf("expected external research trigger for %q", text)
		}
	}
	if codingTaskNeedsExternalResearch("fix local nil pointer in parser") {
		t.Fatal("pure repository logic bug should not force web research")
	}
	for _, text := range []string{
		"fix internal API handler nil pointer",
		"repair local package parser",
		"修复仓内 provider 的空指针",
	} {
		if codingTaskNeedsExternalResearch(text) {
			t.Fatalf("repository-owned surface must not force web research: %q", text)
		}
	}
	for _, text := range []string{
		"fix local wrapper handling Stripe API error",
		"repair internal adapter after React framework behavior changed",
		"fix local parser and Stripe API timeout",
	} {
		if !codingTaskNeedsExternalResearch(text) {
			t.Fatalf("named external surface must override a local-wrapper qualifier: %q", text)
		}
	}
	if codingTaskNeedsExternalResearch("fix parser in our internal payment API") {
		t.Fatal("a directly qualified repository-owned API should remain local")
	}
	if codingTaskNeedsExternalResearch("fix our internal Acme API request parser") {
		t.Fatal("an internally qualified named API should remain local")
	}
}

func TestLocalizationTriggerUsesEnglishWordBoundaries(t *testing.T) {
	for _, text := range []string{"fix Stripe API error", "React framework behavior changed", "插件更新后报错", "云服务商返回错误码"} {
		if !codingTaskNeedsExternalResearch(text) {
			t.Fatalf("third-party surface should trigger research: %q", text)
		}
	}

	for _, text := range []string{"configure feature flags", "parse fixture metadata", "update prefix table"} {
		if codingTaskNeedsLocalization(text) {
			t.Fatalf("ordinary task %q must not be classified as a bug merely because it contains the letters fix", text)
		}
	}
	for _, text := range []string{
		"fix parser crash", "parser regression", "request fails intermittently",
		"compiler error in parser", "test failed on Windows", "service hangs during shutdown", "request timeout",
	} {
		if !codingTaskNeedsLocalization(text) {
			t.Fatalf("bug task %q should require localization", text)
		}
	}
}

func TestExternalResearchTriggerRecognizesExplicitVersionFact(t *testing.T) {
	if !codingTaskNeedsExternalResearch("fix behavior in framework version 4.2") {
		t.Fatal("an explicit version-sensitive bug should require external research")
	}
}

func TestLocalizationResearchGateRequiresAuditedSearch(t *testing.T) {
	task := "fix unknown third-party SDK error after upgrade"
	e := &CodingSubAgentLocalizationEvidence{
		RootCauseFile: "client.go", CausalPath: []string{"request -> SDK"},
		Reproduction: "focused test fails", SupportingEvidence: []string{"exact error"},
		ResearchDecision: "not_needed", ResearchReason: "guessed", Confidence: .7,
	}
	if err := validateLocalizationResearchEvidence(task, e, nil); err == nil {
		t.Fatal("unknown third-party fact should reject not_needed")
	}
	e.ResearchDecision = "searched"
	e.ResearchReason = "version-sensitive SDK behavior"
	e.ExternalSources = []string{"https://vendor.example/docs"}
	if err := validateLocalizationResearchEvidence(task, e, nil); err == nil {
		t.Fatal("searched decision should require audited web_search")
	}
	searches := []CodingSubAgentSearchResult{
		{Tool: "web_search", Query: "exact SDK error v2", Summary: "https://vendor.example/docs official documentation", Succeeded: true},
		{Tool: "web_fetch", Query: "https://vendor.example/docs", Summary: "Official SDK exact error v2 documentation body", Succeeded: true},
	}
	if err := validateLocalizationResearchEvidence(task, e, searches); err != nil {
		t.Fatalf("unexpected research gate: %v", err)
	}
}

func TestLocalizationResearchGateUsesDiscoveredEvidence(t *testing.T) {
	task := "fix request failure"
	e := &CodingSubAgentLocalizationEvidence{
		RootCauseFile: "client.go", CausalPath: []string{"request -> third-party AcmeClient v2"},
		Reproduction: "focused test fails", SupportingEvidence: []string{"third-party AcmeClient v2 returns code X917"},
		ResearchDecision: "not_needed", ResearchReason: "repository-only", Confidence: .8,
	}
	if err := validateLocalizationResearchEvidence(task, e, nil); err == nil {
		t.Fatal("third-party/version facts discovered during localization must require research even when absent from the original task")
	}
	e.ResearchDecision = "searched"
	e.ResearchReason = "discovered version-sensitive SDK behavior"
	e.ExternalSources = []string{"https://vendor.example/x917"}
	searches := []CodingSubAgentSearchResult{{
		Tool: "web_search", Query: "AcmeClient v2 X917", Succeeded: true,
		Summary: "Search AcmeClient v2 X917 found 1 result\nhttps://vendor.example/x917",
	}, {Tool: "web_fetch", Query: "https://vendor.example/x917", Summary: "AcmeClient v2 X917 official reference", Succeeded: true}}
	if err := validateLocalizationResearchEvidence(task, e, searches); err != nil {
		t.Fatalf("a query relevant to facts discovered in localization evidence should pass: %v", err)
	}
}

func TestLocalizationResearchContextExcludesRejectedAndPathNoise(t *testing.T) {
	e := &CodingSubAgentLocalizationEvidence{
		RootCauseFile:      "pkg/client_v9.go",
		CausalPath:         []string{"request -> AcmeClient v2"},
		SupportingEvidence: []string{"AcmeClient returns X917"},
		RejectedHypotheses: []string{"old vendor v1 theory rejected"},
		Candidates: []CodingSubAgentLocalizationCandidate{{
			File: "legacy_v7.go", Symbol: "fallbackV3", ContradictingEvidence: []string{"not reachable"},
		}},
	}
	context := localizationResearchContext("fix request failure", e)
	if strings.Contains(context, "v9") || strings.Contains(context, "v7") || strings.Contains(context, "fallbackV3") || strings.Contains(context, "v1") {
		t.Fatalf("research query requirements must not inherit path or rejected-hypothesis noise: %q", context)
	}
	if !strings.Contains(context, "AcmeClient v2") || !strings.Contains(context, "X917") {
		t.Fatalf("confirmed discovered component/version/error must remain in research context: %q", context)
	}
}

func TestLocalizationResearchGateRejectsUnrelatedSearch(t *testing.T) {
	task := "fix AcmeSDK v2 compatibility error after upgrade"
	e := &CodingSubAgentLocalizationEvidence{
		RootCauseFile: "client.go", CausalPath: []string{"request -> AcmeSDK"},
		Reproduction: "focused test fails", SupportingEvidence: []string{"exact error"},
		ResearchDecision: "searched", ResearchReason: "version-sensitive SDK behavior",
		ExternalSources: []string{"https://unrelated.example/weather"}, Confidence: .7,
	}
	unrelated := []CodingSubAgentSearchResult{{
		Tool: "web_search", Query: "weather forecast tomorrow", Summary: "https://unrelated.example/weather", Succeeded: true,
	}}
	if err := validateLocalizationResearchEvidence(task, e, unrelated); err == nil {
		t.Fatal("an unrelated successful search must not satisfy bug research")
	}
	related := []CodingSubAgentSearchResult{
		{Tool: "web_search", Query: "AcmeSDK v2 compatibility", Summary: "https://vendor.example/acmesdk-v2", Succeeded: true},
		{Tool: "web_fetch", Query: "https://vendor.example/acmesdk-v2", Summary: "AcmeSDK v2 compatibility guide", Succeeded: true},
	}
	e.ExternalSources = []string{"https://vendor.example/acmesdk-v2"}
	if err := validateLocalizationResearchEvidence(task, e, related); err != nil {
		t.Fatalf("a task-specific search should satisfy the research gate: %v", err)
	}
}

func TestLocalizationResearchQueryRejectsGenericTaskOverlap(t *testing.T) {
	task := "fix AcmeSDK compatibility error after upgrade"
	for _, query := range []string{
		"upgrade compatibility guide",
		"how to fix SDK error",
		"latest vendor dependency version",
	} {
		if localizationResearchQueryRelevant(task, query) {
			t.Fatalf("generic query %q must not count as task-specific research", query)
		}
	}
	if !localizationResearchQueryRelevant(task, "AcmeSDK migration behavior") {
		t.Fatal("query retaining the task's product identifier should be relevant")
	}
	if localizationResearchQueryRelevant("fix AcmeSDK v2 compatibility error", "weather API v2 forecast") {
		t.Fatal("a shared version token alone must not make an unrelated query relevant")
	}
	if localizationResearchQueryRelevant("fix framework 4.2 compatibility error", "weather forecast 4.2") {
		t.Fatal("a shared numeric version alone must not make an unrelated query relevant")
	}
	if !localizationResearchQueryRelevant("fix framework 4.2 compatibility error", "framework 4.2 migration guide") {
		t.Fatal("a domain-specific version query should remain relevant when no product identifier is available")
	}
	if localizationResearchQueryRelevant("fix AcmeSDK v2 error X917", "AcmeSDK migration guide") {
		t.Fatal("component-only query must retain discovered version and diagnostic code")
	}
	if localizationResearchQueryRelevant("fix AcmeSDK v2 error X917", "AcmeSDK v2 migration guide") {
		t.Fatal("query missing the precise diagnostic code must not satisfy research")
	}
	if !localizationResearchQueryRelevant("fix AcmeSDK v2 error X917", "AcmeSDK v2 X917 official docs") {
		t.Fatal("query retaining component, version, and diagnostic code should pass")
	}
}

func TestLocalizationResearchPrecisionTokensStayBoundedAndRelevant(t *testing.T) {
	tokens := localizationResearchTokenRE.FindAllString(strings.ToLower(
		"failed 2026.07.22 on port8080 build abc123def456 AcmeSDK v2 v3 X917 Y918 Z919 CVE-2025-1234 ERR_CONNECTION_RESET"), -1)
	got := localizationResearchRequiredPrecisionTokens(tokens)
	if len(got) > 4 {
		t.Fatalf("precision requirements must stay bounded, got %v", got)
	}
	if containsString(got, "2026.07.22") || containsString(got, "port8080") || containsString(got, "abc123def456") {
		t.Fatalf("dates, ports and hash-like noise must not become mandatory search tokens: %v", got)
	}
	if !containsString(got, "v2") || !containsString(got, "v3") || !containsString(got, "err_connection_reset") || !containsString(got, "cve-2025-1234") {
		t.Fatalf("bounded selection should retain versions and prioritize canonical diagnostics: %v", got)
	}
	if !localizationResearchDiagnosticToken("ERR_CONNECTION_RESET") || !localizationResearchDiagnosticToken("CVE-2025-1234") {
		t.Fatal("canonical named error and CVE identifiers should be precise diagnostics")
	}
}

func TestLocalizationResearchPrecisionPrioritizesCanonicalDiagnostics(t *testing.T) {
	tokens := localizationResearchTokenRE.FindAllString(strings.ToLower(
		"AcmeSDK X917 Y918 ERR_CONNECTION_RESET CVE-2025-1234"), -1)
	got := localizationResearchRequiredPrecisionTokens(tokens)
	if !containsString(got, "err_connection_reset") || !containsString(got, "cve-2025-1234") {
		t.Fatalf("canonical diagnostics should outrank earlier vendor codes: %v", got)
	}
	if len(got) > 2 {
		t.Fatalf("diagnostic selection should remain bounded without versions: %v", got)
	}
}

func TestLocalizationResearchPrecisionRequiresQualifiedHTTPStatus(t *testing.T) {
	if localizationResearchQueryRelevant("fix AcmeSDK v2 HTTP status 429", "AcmeSDK v2 rate limit") {
		t.Fatal("query must retain a qualified HTTP failure status")
	}
	if !localizationResearchQueryRelevant("fix AcmeSDK v2 HTTP status 429", "AcmeSDK v2 HTTP 429 official docs") {
		t.Fatal("query retaining the qualified HTTP status should pass")
	}
	if !localizationResearchQueryRelevant("fix AcmeSDK v2 issue 429", "AcmeSDK v2 issue guide") {
		t.Fatal("an unqualified issue number must not become a mandatory HTTP status")
	}
}

func TestLocalizationWebAuditTruncationPreservesHeadAndTail(t *testing.T) {
	head := "Search AcmeSDK found 1 result\n"
	tail := "\nhttps://vendor.example/official/reference"
	result := head + strings.Repeat("x", localizationWebAuditMaxRunes) + tail
	got := truncateLocalizationWebAudit(result)
	if utf8.RuneCountInString(got) > localizationWebAuditMaxRunes || !strings.HasPrefix(got, head) || !strings.HasSuffix(got, tail) {
		t.Fatalf("web audit truncation must preserve bounded head and source-bearing tail; len=%d", utf8.RuneCountInString(got))
	}
}

func TestUnavailableResearchRequiresRelevantAttempt(t *testing.T) {
	task := "fix AcmeSDK v2 error after upgrade"
	e := &CodingSubAgentLocalizationEvidence{ResearchDecision: "unavailable", ResearchReason: "provider failed"}
	searches := []CodingSubAgentSearchResult{{Tool: "web_search", Query: "weather", Summary: "provider timed out", Succeeded: false}}
	if err := validateLocalizationResearchEvidence(task, e, searches); err == nil {
		t.Fatal("an unrelated failed query must not justify unavailable research")
	}
	searches[0].Query = "AcmeSDK v2 exact error"
	if err := validateLocalizationResearchEvidence(task, e, searches); err != nil {
		t.Fatalf("a relevant provider failure should justify unavailable research: %v", err)
	}
}

func TestUnavailableResearchRetriesDistinctNoResultQuery(t *testing.T) {
	task := "fix AcmeSDK v2 error X917 after upgrade"
	e := &CodingSubAgentLocalizationEvidence{ResearchDecision: "unavailable", ResearchReason: "no useful results"}
	searches := []CodingSubAgentSearchResult{{Tool: "web_search", Query: "AcmeSDK v2 X917", Summary: "no relevant results", Succeeded: false}}
	if err := validateLocalizationResearchEvidence(task, e, searches); err == nil {
		t.Fatal("one no-result query should require a distinct retry")
	}
	searches = append(searches, CodingSubAgentSearchResult{Tool: "web_search", Query: "AcmeSDK v2 error X917 official", Summary: "no results", Succeeded: false})
	if err := validateLocalizationResearchEvidence(task, e, searches); err != nil {
		t.Fatalf("two distinct precise no-result queries should justify unavailable: %v", err)
	}
	searches[1].Query = searches[0].Query
	if err := validateLocalizationResearchEvidence(task, e, searches); err == nil {
		t.Fatal("repeating the same query must not satisfy retry evidence")
	}
	searches[1].Query = `"X917" AcmeSDK v2`
	if err := validateLocalizationResearchEvidence(task, e, searches); err == nil {
		t.Fatal("reordering or quoting the same query tokens must not satisfy retry evidence")
	}
	searches[1].Query = `please search AcmeSDK v2 X917 for the error`
	if err := validateLocalizationResearchEvidence(task, e, searches); err == nil {
		t.Fatal("adding only search filler must not satisfy retry evidence")
	}
}

func TestLocalizationResearchQueryFingerprint(t *testing.T) {
	first := localizationResearchQueryFingerprint(`AcmeSDK v2 "X917"`)
	if second := localizationResearchQueryFingerprint("X917, AcmeSDK v2"); first != second {
		t.Fatalf("punctuation/order-only variants should share a fingerprint: %q vs %q", first, second)
	}
	if second := localizationResearchQueryFingerprint("AcmeSDK v2 X917 official migration"); first == second {
		t.Fatal("a substantively broadened query should have a different fingerprint")
	}
}

func TestSameLocalizationResearchOriginRejectsHTTPSDowngrade(t *testing.T) {
	if sameLocalizationResearchOrigin("http://vendor.example/docs", "https://vendor.example/docs") {
		t.Fatal("an HTTPS source redirected to HTTP must not be accepted")
	}
	if !sameLocalizationResearchOrigin("https://vendor.example/docs", "http://vendor.example/start") {
		t.Fatal("an HTTP source upgraded to HTTPS on the same host should be accepted")
	}
	if !sameLocalizationResearchOrigin("https://vendor.example:443/docs", "https://vendor.example/start") {
		t.Fatal("explicit and implicit default ports should share an origin")
	}
	if sameLocalizationResearchOrigin("https://vendor.example:8443/docs", "https://vendor.example/start") {
		t.Fatal("different non-default ports must not share an origin")
	}
	if sameLocalizationResearchOrigin("https://vendor.example@evil.example/docs", "https://evil.example/start") {
		t.Fatal("userinfo-bearing URLs must not be accepted as research origins")
	}
}

func TestLocalizationResearchSourceRejectsURLUserinfoSpoof(t *testing.T) {
	for _, source := range []string{
		"https://vendor.example@evil.example/docs",
		"https://user:secret@vendor.example/docs",
	} {
		if localizationResearchSourceIsHTTPURL(source) {
			t.Fatalf("userinfo-bearing source must be rejected: %q", source)
		}
		if localizationAuditContainsSource(source, source) {
			t.Fatalf("userinfo-bearing source must not match search audit: %q", source)
		}
	}
}

func TestLocalizationResearchSourceRejectsInvalidPortsAndIPv6Zones(t *testing.T) {
	for _, source := range []string{
		"https://vendor.example:0/docs",
		"https://vendor.example:65536/docs",
		"https://[fe80::1%25eth0]/docs",
	} {
		if localizationResearchSourceIsHTTPURL(source) {
			t.Fatalf("invalid/nonportable source URL must be rejected: %q", source)
		}
	}
	if !sameLocalizationResearchURL("https://vendor.example:443/docs", "https://vendor.example/docs/") {
		t.Fatal("explicit and implicit default ports should identify the same URL")
	}
	if !sameLocalizationResearchURL("http://vendor.example:80/docs", "http://vendor.example/docs") {
		t.Fatal("explicit and implicit HTTP default ports should identify the same URL")
	}
}

func TestLocalizationResearchDebugSummaryIsCompactAndSecretSafe(t *testing.T) {
	e := &CodingSubAgentLocalizationEvidence{ResearchDecision: "searched", ExternalSources: []string{"https://vendor.example/docs"}}
	searches := []CodingSubAgentSearchResult{
		{Tool: "web_search", Query: "AcmeSDK v2 X917 api_key=super-secret", Summary: "no results", Succeeded: false, seq: 2},
		{Tool: "web_fetch", Query: "https://user:secret@vendor.example/docs", FetchResolvedURL: "https://vendor.example/docs", Summary: "Documentation body with enough content for a successful audited fetch.", Succeeded: true, FetchAuditKnown: true, FetchRangeKnown: true, FetchNextOffset: 66, FetchTotalChars: 66, seq: 3},
	}
	got := localizationResearchDebugSummary("fix AcmeSDK v2 X917", e, searches)
	for _, want := range []string{"decision=searched", "searches_total=2", "fetches=1", "last_seq=3"} {
		if !strings.Contains(got, want) {
			t.Fatalf("debug summary missing %q: %s", want, got)
		}
	}
	for _, search := range searches {
		toolSummary := localizationResearchToolDebugSummary(search)
		for _, sensitive := range []string{"secret", "user", "AcmeSDK", "X917", "api_key"} {
			if strings.Contains(toolSummary, sensitive) {
				t.Fatalf("research debug summary must not log query or URL secrets (%q): %s", sensitive, toolSummary)
			}
		}
	}
	if got := localizationResearchToolDebugSummary(searches[0]); !strings.Contains(got, "failure_kind=no_results") {
		t.Fatalf("search debug summary should classify no-result failures: %s", got)
	}
	if got := localizationResearchToolDebugSummary(searches[1]); !strings.Contains(got, "failure_kind=none") {
		t.Fatalf("fetch debug summary should classify successful fetches: %s", got)
	}
}

func TestRemoteLocalizationLoggingIsSafeWithoutAgent(t *testing.T) {
	c := &remoteCodingCallbacks{task: "fix AcmeSDK v2 error X917"}
	if got := remoteLocalizationLogProject(c); got != "" {
		t.Fatalf("nil-agent project log context = %q, want empty", got)
	}
	result := c.executeRemoteReportLocalization(map[string]interface{}{})
	if !strings.Contains(result, "invalid localization evidence") {
		t.Fatalf("nil-agent localization report should reject evidence without panicking: %s", result)
	}
}

func TestUnavailableResearchRetriesAmbiguousFailure(t *testing.T) {
	task := "fix AcmeSDK v2 error X917 after upgrade"
	e := &CodingSubAgentLocalizationEvidence{ResearchDecision: "unavailable", ResearchReason: "empty provider response"}
	searches := []CodingSubAgentSearchResult{{Tool: "web_search", Query: "AcmeSDK v2 X917", Summary: "", Succeeded: false}}
	if err := validateLocalizationResearchEvidence(task, e, searches); err == nil {
		t.Fatal("one ambiguous failure must not prove search is unavailable")
	}
	searches = append(searches, CodingSubAgentSearchResult{Tool: "web_search", Query: "AcmeSDK v2 X917 official", Summary: "unexpected empty response", Succeeded: false})
	if err := validateLocalizationResearchEvidence(task, e, searches); err != nil {
		t.Fatalf("two distinct ambiguous failures should justify unavailable: %v", err)
	}
}

func TestLocalizationResearchRechecksAuditedResultBody(t *testing.T) {
	task := "fix AcmeSDK v2 error after upgrade"
	e := &CodingSubAgentLocalizationEvidence{
		ResearchDecision: "searched", ResearchReason: "version-sensitive behavior",
		ExternalSources: []string{"https://vendor.example/acmesdk"},
	}
	for _, summary := range []string{"", "no relevant results", "搜索失败: provider timeout"} {
		searches := []CodingSubAgentSearchResult{{Tool: "web_search", Query: "AcmeSDK v2 error", Summary: summary, Succeeded: true}}
		if err := validateLocalizationResearchEvidence(task, e, searches); err == nil {
			t.Fatalf("Succeeded=true with unusable body %q must not satisfy research", summary)
		}
	}
	searches := []CodingSubAgentSearchResult{{
		Tool: "web_search", Query: "AcmeSDK v2 error",
		Summary: "搜索 \"AcmeSDK v2 error\" 找到 1 条结果:\nhttps://vendor.example/acmesdk", Succeeded: true,
	}}
	searches = append(searches, CodingSubAgentSearchResult{Tool: "web_fetch", Query: "https://vendor.example/acmesdk", Summary: "AcmeSDK v2 official error reference documentation body", Succeeded: true})
	if err := validateLocalizationResearchEvidence(task, e, searches); err != nil {
		t.Fatalf("usable audited result should pass: %v", err)
	}
}

func TestLocalizationResearchSourceMustComeFromUsableSearch(t *testing.T) {
	task := "fix AcmeSDK v2 error after upgrade"
	e := &CodingSubAgentLocalizationEvidence{
		ResearchDecision: "searched", ResearchReason: "version-sensitive behavior",
		ExternalSources: []string{"https://fabricated.example/acmesdk"},
	}
	searches := []CodingSubAgentSearchResult{
		{
			Tool: "web_search", Query: "AcmeSDK v2 error", Succeeded: true,
			Summary: "Search AcmeSDK v2 error found 1 result\nhttps://vendor.example/acmesdk",
		},
		{
			Tool: "web_search", Query: "AcmeSDK v2 migration", Succeeded: true,
			Summary: "search failed: provider timeout https://fabricated.example/acmesdk",
		},
	}
	if err := validateLocalizationResearchEvidence(task, e, searches); err == nil {
		t.Fatal("a source mentioned only by a failed-looking search must not borrow success from another query")
	}
	e.ExternalSources = []string{"https://vendor.example/acmesdk"}
	searches = append(searches, CodingSubAgentSearchResult{Tool: "web_fetch", Query: "https://vendor.example/acmesdk", Summary: "AcmeSDK v2 official documentation page body", Succeeded: true})
	if err := validateLocalizationResearchEvidence(task, e, searches); err != nil {
		t.Fatalf("source from the usable relevant search should pass: %v", err)
	}
}

func TestLocalizationResearchRequiresSuccessfulFetchForExternalFacts(t *testing.T) {
	task := "fix AcmeSDK v2 error after upgrade"
	e := &CodingSubAgentLocalizationEvidence{
		ResearchDecision: "searched", ResearchReason: "version-sensitive behavior",
		ExternalSources: []string{"https://vendor.example/acmesdk"},
	}
	searches := []CodingSubAgentSearchResult{{
		Tool: "web_search", Query: "AcmeSDK v2 error", Succeeded: true,
		Summary: "Search AcmeSDK v2 error found 1 result\nhttps://vendor.example/acmesdk",
	}}
	if err := validateLocalizationResearchEvidence(task, e, searches); err == nil {
		t.Fatal("a search snippet alone must not count as reading the declared source")
	}
	searches = append(searches, CodingSubAgentSearchResult{
		Tool: "web_fetch", Query: "https://vendor.example/acmesdk", Succeeded: true,
		Summary: "web_fetch failed: provider timeout",
	})
	if err := validateLocalizationResearchEvidence(task, e, searches); err == nil {
		t.Fatal("a failed-looking fetch must not satisfy source verification even when marked successful")
	}
	searches[1].Summary = "AcmeSDK v2 official error reference documentation body"
	if err := validateLocalizationResearchEvidence(task, e, searches); err != nil {
		t.Fatalf("successfully fetched declared source should satisfy verification: %v", err)
	}
	searches[1].Query = "HTTPS://VENDOR.EXAMPLE/acmesdk/#overview"
	if err := validateLocalizationResearchEvidence(task, e, searches); err != nil {
		t.Fatalf("harmless URL case, trailing slash, and fragment variants should match: %v", err)
	}
	searches[1].Summary = "ok"
	if err := validateLocalizationResearchEvidence(task, e, searches); err == nil {
		t.Fatal("a tiny fetch acknowledgement must not prove that the source was read")
	}
}

func TestLocalizationResearchFetchMustFollowSourceDiscovery(t *testing.T) {
	task := "fix AcmeSDK v2 error after upgrade"
	e := &CodingSubAgentLocalizationEvidence{
		ResearchDecision: "searched", ResearchReason: "version-sensitive behavior",
		ExternalSources: []string{"https://vendor.example/acmesdk"},
	}
	searches := []CodingSubAgentSearchResult{
		{Tool: "web_fetch", Query: "https://vendor.example/acmesdk", Succeeded: true, Summary: "AcmeSDK v2 official documentation body with error details", seq: 1},
		{Tool: "web_search", Query: "AcmeSDK v2 error", Succeeded: true, Summary: "Search AcmeSDK v2 error found 1 result\nhttps://vendor.example/acmesdk", seq: 2},
	}
	if err := validateLocalizationResearchEvidence(task, e, searches); err == nil {
		t.Fatal("a stale fetch performed before the relevant source discovery must not satisfy research")
	}
	searches[0].seq = 3
	if err := validateLocalizationResearchEvidence(task, e, searches); err != nil {
		t.Fatalf("fetch after the relevant source discovery should pass: %v", err)
	}
}

func TestLocalizationResearchFetchedBodyMustRelateToFailure(t *testing.T) {
	task := "fix AcmeSDK v2 error X917 after upgrade"
	e := &CodingSubAgentLocalizationEvidence{
		ResearchDecision: "searched", ResearchReason: "version-sensitive behavior",
		ExternalSources: []string{"https://vendor.example/acmesdk"},
	}
	searches := []CodingSubAgentSearchResult{
		{Tool: "web_search", Query: "AcmeSDK v2 X917", Succeeded: true, Summary: "Search AcmeSDK v2 X917 found 1 result\nhttps://vendor.example/acmesdk", seq: 1},
		{Tool: "web_fetch", Query: "https://vendor.example/acmesdk", Succeeded: true, Summary: "Generic documentation page with navigation and copyright text", seq: 2},
	}
	if err := validateLocalizationResearchEvidence(task, e, searches); err == nil {
		t.Fatal("a fetched page with no component, version, or diagnostic overlap must not validate research")
	}
	searches[1].Summary = "AcmeSDK v2 generic release documentation without the reported diagnostic"
	if err := validateLocalizationResearchEvidence(task, e, searches); err == nil {
		t.Fatal("when precise diagnostics exist, component/version-only fetch content must not pass")
	}
	searches[1].Summary = "AcmeSDK error X917 documentation for another release"
	if err := validateLocalizationResearchEvidence(task, e, searches); err == nil {
		t.Fatal("when precise versions exist, diagnostic-only fetch content must not pass")
	}
	searches[1].Summary = "AcmeSDK v2 error X917 official troubleshooting reference"
	if err := validateLocalizationResearchEvidence(task, e, searches); err != nil {
		t.Fatalf("fetched content tied to the concrete failure should pass: %v", err)
	}
}

func TestLocalizationResearchCombinesRelevantFetchPages(t *testing.T) {
	task := "fix AcmeSDK v2 error X917 after upgrade"
	e := &CodingSubAgentLocalizationEvidence{
		ResearchDecision: "searched", ResearchReason: "version-sensitive behavior",
		ExternalSources: []string{"https://vendor.example/acmesdk"},
	}
	searches := []CodingSubAgentSearchResult{
		{Tool: "web_search", Query: "AcmeSDK v2 X917", Succeeded: true, Summary: "Search AcmeSDK v2 X917 found 1 result\nhttps://vendor.example/acmesdk", seq: 1},
		{Tool: "web_fetch", Query: "https://vendor.example/acmesdk", Succeeded: true, Summary: "AcmeSDK v2 migration reference page one", FetchOffset: 0, FetchNextOffset: 100, FetchTotalChars: 200, FetchHasMore: true, FetchRangeKnown: true, seq: 2},
		{Tool: "web_fetch", Query: "https://vendor.example/acmesdk", Succeeded: true, Summary: "Troubleshooting diagnostic X917 on page two", FetchOffset: 100, FetchNextOffset: 200, FetchTotalChars: 200, FetchRangeKnown: true, seq: 3},
	}
	if err := validateLocalizationResearchEvidence(task, e, searches); err != nil {
		t.Fatalf("precision evidence split across post-discovery pages of one source should pass: %v", err)
	}
	searches[2].Query = "https://other.example/troubleshooting"
	if err := validateLocalizationResearchEvidence(task, e, searches); err == nil {
		t.Fatal("precision evidence must not be combined across different fetched sources")
	}
}

func TestLocalizationResearchRejectsDuplicateAndGappedFetchPages(t *testing.T) {
	task := "fix AcmeSDK v2 error X917 after upgrade"
	e := &CodingSubAgentLocalizationEvidence{ResearchDecision: "searched", ResearchReason: "version-sensitive behavior", ExternalSources: []string{"https://vendor.example/acmesdk"}}
	base := []CodingSubAgentSearchResult{
		{Tool: "web_search", Query: "AcmeSDK v2 X917", Succeeded: true, Summary: "https://vendor.example/acmesdk", seq: 1},
		{Tool: "web_fetch", Query: "https://vendor.example/acmesdk", Succeeded: true, Summary: "AcmeSDK v2 migration", FetchOffset: 0, FetchNextOffset: 100, FetchTotalChars: 300, FetchHasMore: true, FetchRangeKnown: true, seq: 2},
	}
	duplicate := append(append([]CodingSubAgentSearchResult{}, base...), CodingSubAgentSearchResult{Tool: "web_fetch", Query: "https://vendor.example/acmesdk", Succeeded: true, Summary: "diagnostic X917", FetchOffset: 0, FetchNextOffset: 100, FetchTotalChars: 300, FetchHasMore: true, FetchRangeKnown: true, seq: 3})
	if err := validateLocalizationResearchEvidence(task, e, duplicate); err == nil {
		t.Fatal("duplicate offset-zero pages must not be combined")
	}
	gapped := append(append([]CodingSubAgentSearchResult{}, base...), CodingSubAgentSearchResult{Tool: "web_fetch", Query: "https://vendor.example/acmesdk", Succeeded: true, Summary: "diagnostic X917", FetchOffset: 200, FetchNextOffset: 300, FetchTotalChars: 300, FetchRangeKnown: true, seq: 3})
	if err := validateLocalizationResearchEvidence(task, e, gapped); err == nil {
		t.Fatal("gapped pages must not be combined")
	}
}

func TestLocalizationWebFetchPaginationAudit(t *testing.T) {
	result := "标题: docs\nURL: https://vendor.example/docs\n已读取: 100-200 / 300 字符\ntruncated: true | has_more: true | next_offset: 200\n\nbody"
	offset, next, total, more, known := localizationWebFetchPagination("web_fetch", map[string]interface{}{"offset": float64(100)}, result)
	if !known || offset != 100 || next != 200 || !more {
		t.Fatalf("pagination = (%d,%d,%d,%t,%t)", offset, next, total, more, known)
	}
}

func TestLocalizationWebFetchPaginationUsesReturnedRange(t *testing.T) {
	result := "标题: docs\nURL: https://vendor.example/docs\n已读取: 100-200 / 300 字符\ntruncated: true | has_more: true | next_offset: 200\n\nbody"
	offset, next, total, more, known := localizationWebFetchPagination("web_fetch", map[string]interface{}{"offset": "100"}, result)
	if !known || offset != 100 || next != 200 || !more {
		t.Fatalf("pagination = (%d,%d,%d,%t,%t)", offset, next, total, more, known)
	}
	bad := strings.Replace(result, "已读取: 100-200", "已读取: 0-100", 1)
	if _, _, _, _, known := localizationWebFetchPagination("web_fetch", nil, bad); known {
		t.Fatal("inconsistent returned range and next_offset must not be auditable")
	}
}

func TestLocalizationWebFetchAuditIgnoresBodyMetadata(t *testing.T) {
	result := "download completed\n\nURL: https://vendor.example/docs\n已读取: 0-100 / 200 字符\ntruncated: true | has_more: true | next_offset: 100"
	if _, _, _, _, known := localizationWebFetchPagination("web_fetch", nil, result); known {
		t.Fatal("page body must not be able to manufacture fetch pagination metadata")
	}
	if got := localizationWebFetchResolvedURL(result); got != "" {
		t.Fatalf("body URL must not be treated as tool metadata, got %q", got)
	}
}

func TestLocalizationResearchRejectsFetchRedirectToUndeclaredSource(t *testing.T) {
	task := "fix AcmeSDK v2 error X917 after upgrade"
	e := &CodingSubAgentLocalizationEvidence{ResearchDecision: "searched", ResearchReason: "version-sensitive behavior", ExternalSources: []string{"https://vendor.example/acmesdk"}}
	searches := []CodingSubAgentSearchResult{
		{Tool: "web_search", Query: "AcmeSDK v2 X917", Succeeded: true, Summary: "https://vendor.example/acmesdk", seq: 1},
		{Tool: "web_fetch", Query: "https://vendor.example/acmesdk", Succeeded: true, Summary: "AcmeSDK v2 official error X917 troubleshooting reference", FetchOffset: 0, FetchNextOffset: 100, FetchTotalChars: 100, FetchRangeKnown: true, FetchAuditKnown: true, FetchResolvedURL: "https://other.example/copied", seq: 2},
	}
	if err := validateLocalizationResearchEvidence(task, e, searches); err == nil {
		t.Fatal("content reached through an undeclared redirect must not verify the declared source")
	}
	searches[1].FetchResolvedURL = "HTTPS://VENDOR.EXAMPLE/acmesdk/#reference"
	if !sameLocalizationResearchURL(searches[1].FetchResolvedURL, e.ExternalSources[0]) {
		t.Fatalf("canonical URL mismatch: %q vs %q", searches[1].FetchResolvedURL, e.ExternalSources[0])
	}
	if err := validateLocalizationResearchEvidence(task, e, searches); err != nil {
		t.Fatalf("canonical variants of the declared final URL should pass: %v", err)
	}
	searches[1].FetchResolvedURL = "https://vendor.example/docs/en/acmesdk"
	if err := validateLocalizationResearchEvidence(task, e, searches); err != nil {
		t.Fatalf("same-origin documentation redirect should pass: %v", err)
	}
}

func TestLocalizationResearchRejectsPaginationTotalChange(t *testing.T) {
	fetches := []CodingSubAgentSearchResult{
		{Summary: "AcmeSDK v2", FetchOffset: 0, FetchNextOffset: 100, FetchTotalChars: 200, FetchHasMore: true, FetchRangeKnown: true, seq: 1},
		{Summary: "error X917", FetchOffset: 100, FetchNextOffset: 250, FetchTotalChars: 250, FetchRangeKnown: true, seq: 2},
	}
	if localizationResearchContinuousFetchesRelevant("AcmeSDK v2 error X917", fetches) {
		t.Fatal("pages from different source revisions must not be combined")
	}
}

func TestLocalizationResearchRejectsDownloadOnlyFetchAudit(t *testing.T) {
	task := "fix AcmeSDK v2 error X917 after upgrade"
	e := &CodingSubAgentLocalizationEvidence{ResearchDecision: "searched", ResearchReason: "version-sensitive behavior", ExternalSources: []string{"https://vendor.example/acmesdk-v2-X917.pdf"}}
	searches := []CodingSubAgentSearchResult{
		{Tool: "web_search", Query: "AcmeSDK v2 X917", Succeeded: true, Summary: "https://vendor.example/acmesdk-v2-X917.pdf", seq: 1},
		{Tool: "web_fetch", Query: "https://vendor.example/acmesdk-v2-X917.pdf", Succeeded: true, Summary: "已保存到: AcmeSDK-v2-X917.pdf", FetchAuditKnown: true, seq: 2},
	}
	if err := validateLocalizationResearchEvidence(task, e, searches); err == nil {
		t.Fatal("downloading a source without reading its body must not satisfy research")
	}
}

func TestLocalizationResearchIgnoresWebFetchMetadataForBodyRelevance(t *testing.T) {
	summary := "标题: Generic docs\nURL: https://vendor.example/AcmeSDK\n类型: text/html | 大小: 120 字节\n已读取: 0-60 / 60 字符\ntruncated: false | has_more: false | next_offset: 60\n\nNavigation copyright and generic documentation text"
	if localizationResearchFetchedBodyRelevant("fix AcmeSDK compatibility", localizationWebFetchResultBody(summary)) {
		t.Fatal("component identity present only in fetch metadata must not validate generic page content")
	}
	summary = strings.Replace(summary, "generic documentation text", "AcmeSDK compatibility reference", 1)
	if !localizationResearchFetchedBodyRelevant("fix AcmeSDK compatibility", localizationWebFetchResultBody(summary)) {
		t.Fatal("component identity in the extracted page body should validate relevance")
	}
}

func TestLocalizationResearchQueryRelevantSupportsTranslatedChineseTask(t *testing.T) {
	if !localizationResearchQueryRelevant("修复陌生的第三方依赖错误", "vendor dependency compatibility error") {
		t.Fatal("a substantive translated query should be accepted for a task with no ASCII component identifier")
	}
	if localizationResearchQueryRelevant("修复 AcmeSDK 第三方错误", "vendor dependency compatibility error") {
		t.Fatal("an ASCII product identifier in the task must be retained in the query")
	}
}

func TestReportLocalizationSchemaRequiresResearchDecision(t *testing.T) {
	def := buildReportLocalizationToolDefinition()
	fn, _ := def["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	required, _ := params["required"].([]string)
	if !containsString(required, "research_decision") || !containsString(required, "research_reason") {
		t.Fatalf("required=%v", required)
	}
	props, _ := params["properties"].(map[string]interface{})
	if props["external_sources"] == nil {
		t.Fatal("external_sources schema missing")
	}
}

func TestSummarizeLocalizationQualityRejectsClaimedUnauditedResearch(t *testing.T) {
	e := &CodingSubAgentLocalizationEvidence{
		RootCauseFile: "client.go", CausalPath: []string{"request -> SDK"},
		Reproduction: "focused test fails", SupportingEvidence: []string{"exact error"},
		ResearchDecision: "searched", ResearchReason: "SDK version changed",
		ExternalSources: []string{"https://vendor.example/docs"}, Confidence: .8,
	}
	got := summarizeLocalizationQuality("fix unknown SDK error", []string{"client.go"}, e, nil)
	if !strings.Contains(got, "research gate failed") {
		t.Fatalf("got=%q", got)
	}
}

func TestWebResearchEmptyResultsAreNotSuccessful(t *testing.T) {
	for _, result := range []string{"", "未找到相关结果", "no relevant results"} {
		if !codingWebResearchResultLooksFailed(result) {
			t.Fatalf("expected failed research result for %q", result)
		}
	}
}

func TestWebResearchSuccessfulHeaderWinsOverSnippetFailureWords(t *testing.T) {
	result := "搜索 \"SDK no results behavior\" 找到 2 条结果:\n\n1. Handling no results and search failed states\n   https://vendor.example/docs"
	if codingWebResearchResultLooksFailed(result) {
		t.Fatal("a non-empty provider result must not fail because an article title contains failure words")
	}
	if !codingWebResearchResultLooksFailed("搜索失败: provider timed out") {
		t.Fatal("an actual provider failure must remain failed")
	}
}

func TestReportLocalizationReturnsStoredNormalizedEvidence(t *testing.T) {
	cb := &codingSubAgentCallbacks{task: &TaskItem{Title: "fix parser bug"}}
	result := cb.executeReportLocalization(map[string]interface{}{
		"root_cause_file": " root.go ", "root_cause_symbol": " Run ",
		"causal_path":  []interface{}{" request -> Run ", "request -> Run"},
		"reproduction": " focused test fails ", "supporting_evidence": []interface{}{" stack ", "stack"},
		"research_decision": " NOT_NEEDED ", "research_reason": " repository-only ", "confidence": .8,
	})
	if result.Outcome != codingToolOutcomeSuccess {
		t.Fatalf("report failed: %s", result.Text)
	}
	if !strings.Contains(result.Text, `"root_cause_file": "root.go"`) || !strings.Contains(result.Text, `"reported_at":`) {
		t.Fatalf("accepted response should reflect normalized stored evidence: %s", result.Text)
	}
	if strings.Count(result.Text, `request -\u003e Run`) != 1 {
		t.Fatalf("accepted response should contain deduplicated evidence: %s", result.Text)
	}
}

func TestLocalizationEvidenceCannotAuthorizeEditAfterStaticSurfaceReplacement(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: t.TempDir()},
		task:     &TaskItem{Title: "fix parser bug", Description: "existing parser crashes"},
	}
	_ = cb.BuildToolsForModelRequest("fix parser bug", 0)
	report := cb.executeReportLocalization(map[string]interface{}{
		"root_cause_file": "root.go", "causal_path": []interface{}{"request -> root"},
		"reproduction": "focused test fails", "supporting_evidence": []interface{}{"stack trace"},
		"research_decision": "not_needed", "research_reason": "repository-only", "confidence": .8,
	})
	if report.Outcome != codingToolOutcomeSuccess || !strings.Contains(report.Text, "control_plane_revision=1") {
		t.Fatalf("report=%#v", report)
	}
	if blocked := cb.requireLocalizationBeforeExistingBugEdit("root.go", false); blocked != "" {
		t.Fatalf("current revision evidence should authorize root edit: %s", blocked)
	}
	_ = cb.BuildToolsForModelRequest("fix parser bug again", 1)
	if blocked := cb.requireLocalizationBeforeExistingBugEdit("root.go", false); !strings.Contains(blocked, "submit report_localization") {
		t.Fatalf("replacement must invalidate old localization evidence, got %q", blocked)
	}
}

func TestRemoteLocalizationEvidenceCannotAuthorizeEditAfterStaticSurfaceReplacement(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{}, task: "fix parser bug", taskContext: "existing parser crashes"}
	_ = cb.BuildToolsForModelRequest("fix parser bug", 0)
	report := cb.executeRemoteReportLocalization(map[string]interface{}{
		"root_cause_file": "root.go", "causal_path": []interface{}{"request -> root"},
		"reproduction": "focused test fails", "supporting_evidence": []interface{}{"stack trace"},
		"research_decision": "not_needed", "research_reason": "repository-only", "confidence": .8,
	})
	if !strings.Contains(report, "control_plane_revision=1") {
		t.Fatalf("report=%q", report)
	}
	if blocked := cb.requireRemoteLocalizationBeforeBugEdit(map[string]interface{}{"path": "root.go"}, true); blocked != "" {
		t.Fatalf("current remote revision evidence should authorize root edit: %s", blocked)
	}
	_ = cb.BuildToolsForModelRequest("fix parser bug again", 1)
	if blocked := cb.requireRemoteLocalizationBeforeBugEdit(map[string]interface{}{"path": "root.go"}, true); !strings.Contains(blocked, "submit report_localization") {
		t.Fatalf("remote replacement must invalidate old localization evidence, got %q", blocked)
	}
}

func TestResearchSourceMustAppearInAuditedResult(t *testing.T) {
	searches := []CodingSubAgentSearchResult{{Tool: "web_search", Query: "SDK error v2", Summary: "https://vendor.example/docs", Succeeded: true}}
	if !codingWebResearchSourceCoversAudit("https://vendor.example/docs", searches) {
		t.Fatal("real audited source was not recognized")
	}
	if codingWebResearchSourceCoversAudit("SDK error v2 fabricated source", searches) {
		t.Fatal("query text alone must not validate an external source")
	}
}

func TestFocusedTestCoverageRequiresCompletePathToken(t *testing.T) {
	e := &CodingSubAgentLocalizationEvidence{FocusedTests: []string{"go test ./gui -run TestBug"}}
	if localizationEvidenceCoversPath(e, "gui/x.go") {
		t.Fatal("short basename substring must not authorize an unrelated edit")
	}
	e.FocusedTests = []string{"go test ./gui/coding_subagent_test.go -run TestBug"}
	if !localizationEvidenceCoversPath(e, "gui/coding_subagent_test.go") {
		t.Fatal("explicitly named focused test file should be covered")
	}
	e.FocusedTests = []string{"go test ./gui/unrelated.go -run TestBug"}
	if localizationEvidenceCoversPath(e, "gui/unrelated.go") {
		t.Fatal("focused_tests must not authorize edits to a non-test production file")
	}
	e.FocusedTests = []string{"pytest tests/test_client.py"}
	if !localizationEvidenceCoversPath(e, "tests/test_client.py") {
		t.Fatal("explicitly named test-directory source should be covered")
	}
}

func TestLocalizationQualityChecksEveryExistingEdit(t *testing.T) {
	e := &CodingSubAgentLocalizationEvidence{
		RootCauseFile: "root.go", CausalPath: []string{"entry -> root"},
		Reproduction: "focused test fails", SupportingEvidence: []string{"stack"},
		ResearchDecision: "not_needed", ResearchReason: "repository-local logic", Confidence: .9,
	}
	got := summarizeLocalizationQuality("fix crash bug", []string{"root.go", "unrelated.go"}, e, nil)
	if !strings.Contains(got, "unrelated.go") {
		t.Fatalf("expected uncovered secondary edit failure, got %q", got)
	}
	e.Candidates = []CodingSubAgentLocalizationCandidate{{File: "related.go", Score: .7, SupportingEvidence: []string{"caller path"}}}
	got = summarizeLocalizationQuality("fix crash bug", []string{"root.go", "related.go"}, e, nil)
	if got != "" {
		t.Fatalf("supported secondary edit should pass, got %q", got)
	}
}

func TestCandidateCoverageRequiresPositiveExplicitScore(t *testing.T) {
	e := &CodingSubAgentLocalizationEvidence{Candidates: []CodingSubAgentLocalizationCandidate{{
		File: "secondary.go", SupportingEvidence: []string{"caller path"},
	}}}
	if localizationEvidenceCoversPath(e, "secondary.go") {
		t.Fatal("a missing/default candidate score must not authorize an edit")
	}
	e.Candidates[0].Score = -.1
	if localizationEvidenceCoversPath(e, "secondary.go") {
		t.Fatal("a negatively ranked candidate must not authorize an edit")
	}
	e.Candidates[0].Score = .1
	if !localizationEvidenceCoversPath(e, "secondary.go") {
		t.Fatal("a positively scored candidate with supporting evidence should authorize its path")
	}
}

func TestUnavailableResearchRejectsSuccessfulSearch(t *testing.T) {
	e := &CodingSubAgentLocalizationEvidence{ResearchDecision: "unavailable", ResearchReason: "provider failed"}
	searches := []CodingSubAgentSearchResult{{Tool: "web_search", Succeeded: true, Summary: "usable result"}}
	if err := validateLocalizationResearchEvidence("unknown SDK error", e, searches); err == nil {
		t.Fatal("unavailable must not pass when an audited search succeeded")
	}
}

func TestSameLocalizationPathDoesNotCollapseDifferentRelativeFiles(t *testing.T) {
	if sameLocalizationPath("pkg/a/config.go", "pkg/b/config.go") {
		t.Fatal("different relative files with the same basename must not match")
	}
	if sameLocalizationPath("config.go", "pkg/b/config.go") {
		t.Fatal("bare relative basename must not authorize a nested path")
	}
	abs := filepath.Join(t.TempDir(), "pkg", "config.go")
	if !sameLocalizationPath(abs, "pkg/config.go") {
		t.Fatal("absolute and project-relative forms of the same path should match")
	}
	if !sameLocalizationPath("/srv/repo/pkg/config.go", "pkg/config.go") {
		t.Fatal("remote POSIX absolute and project-relative forms should match on every host OS")
	}
	if !sameLocalizationPath("C:/Repo/Pkg/Config.go", "pkg/config.go") {
		t.Fatal("Windows paths should compare case-insensitively even when slash style differs")
	}
	if sameLocalizationPath("/srv/repo/Pkg/Config.go", "pkg/config.go") {
		t.Fatal("remote POSIX paths must remain case-sensitive")
	}
}

func TestReportLocalizationSchemaDefinesCandidateShape(t *testing.T) {
	def := buildReportLocalizationToolDefinition()
	fn, _ := def["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	candidates, _ := props["candidates"].(map[string]interface{})
	items, _ := candidates["items"].(map[string]interface{})
	candidateProps, _ := items["properties"].(map[string]interface{})
	required, _ := items["required"].([]string)
	if candidateProps["supporting_evidence"] == nil || candidateProps["contradicting_evidence"] == nil || !containsString(required, "file") || !containsString(required, "score") {
		t.Fatalf("candidate schema is incomplete: %#v", items)
	}
}

func TestBugFixEditRevalidatesLocalizationResearchAudit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.go")
	if err := os.WriteFile(path, []byte("package client\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: dir}, task: &TaskItem{Title: "fix unknown SDK error after upgrade"}}
	cb.localization.set(CodingSubAgentLocalizationEvidence{
		RootCauseFile: "client.go", CausalPath: []string{"request -> SDK"}, Reproduction: "focused test fails",
		SupportingEvidence: []string{"exact SDK error"}, ResearchDecision: "searched", ResearchReason: "version-sensitive SDK behavior",
		ExternalSources: []string{"https://vendor.example/docs"}, Confidence: .8,
	})
	if got := cb.requireLocalizationBeforeExistingBugEdit(path, false); !strings.Contains(got, "research evidence") {
		t.Fatalf("stale/directly injected research evidence should not authorize an edit, got %q", got)
	}
	cb.trackSearchResult("web_search", map[string]interface{}{"query": "SDK error"}, "https://vendor.example/docs", true)
	if got := cb.requireLocalizationBeforeExistingBugEdit(path, false); got != "" {
		t.Fatalf("audited research should authorize the covered edit: %s", got)
	}
}

func TestExternalResearchTriggerUsesEnglishWordBoundaries(t *testing.T) {
	for _, text := range []string{
		"fix latest SDK compatibility bug",
		"unknown driver error after upgrade",
	} {
		if !codingTaskNeedsExternalResearch(text) {
			t.Fatalf("expected external research for %q", text)
		}
	}
	for _, text := range []string{
		"update the knowledge graph",
		"the driverless local parser fails",
	} {
		if codingTaskNeedsExternalResearch(text) {
			t.Fatalf("substring false positive should not force research for %q", text)
		}
	}
}

func TestResearchSourceAuditRequiresExactURLOrMeaningfulTitle(t *testing.T) {
	searches := []CodingSubAgentSearchResult{{
		Tool: "web_search", Succeeded: true,
		Summary: "Vendor SDK migration guide\nhttps://vendor.example/docs/v2/migration",
	}}
	if !codingWebResearchSourceCoversAudit("https://vendor.example/docs/v2/migration", searches) {
		t.Fatal("exact URL should be accepted")
	}
	if codingWebResearchSourceCoversAudit("https://vendor.example/docs", searches) {
		t.Fatal("URL prefix must not validate a different source")
	}
	if codingWebResearchSourceCoversAudit("docs", searches) {
		t.Fatal("tiny generic title fragment must not validate a source")
	}
	if !codingWebResearchSourceCoversAudit("Vendor SDK migration guide", searches) {
		t.Fatal("meaningful exact source title should be accepted")
	}
	if codingWebResearchSourceCoversAudit("file://vendor.example/docs", searches) {
		t.Fatal("non-HTTP URL schemes must not be accepted as external web sources")
	}
}

func TestResearchSourceURLAuditUsesCanonicalBoundedTokens(t *testing.T) {
	searches := []CodingSubAgentSearchResult{{
		Tool: "web_search", Succeeded: true,
		Summary: "Official docs: HTTPS://VENDOR.EXAMPLE/docs/v2/?lang=en#install).",
	}}
	if !codingWebResearchSourceCoversAudit("https://vendor.example/docs/v2?lang=en", searches) {
		t.Fatal("URL audit should tolerate scheme/host case, trailing slash, fragment, and prose punctuation")
	}
	if codingWebResearchSourceCoversAudit("https://vendor.example/docs/v2?lang=zh", searches) {
		t.Fatal("different query parameters must remain distinct sources")
	}
	if codingWebResearchSourceCoversAudit("https://vendor.example/docs/v2", searches) {
		t.Fatal("a URL without the audited query must not validate a different resource")
	}
}

func TestResearchSourceAuditRejectsEchoedQueryAsTitle(t *testing.T) {
	searches := []CodingSubAgentSearchResult{{
		Tool: "web_search", Query: "AcmeSDK exact error migration",
		Summary:   "搜索 \"AcmeSDK exact error migration\" 找到 1 条结果:\n\n1. Official migration notes\n   https://vendor.example/migration",
		Succeeded: true,
	}}
	if codingWebResearchSourceCoversAudit("AcmeSDK exact error migration", searches) {
		t.Fatal("the echoed search query must not validate itself as a source title")
	}
	if !codingWebResearchSourceCoversAudit("Official migration notes", searches) {
		t.Fatal("a real result title should remain valid")
	}
	if !codingWebResearchSourceCoversAudit("https://vendor.example/migration", searches) {
		t.Fatal("a real result URL should remain valid")
	}
	searches[0].Summary = "搜索 \"AcmeSDK exact error migration\" 找到 1 条结果:\n\n1. AcmeSDK exact error migration\n   https://vendor.example/exact-title"
	if !codingWebResearchSourceCoversAudit("AcmeSDK exact error migration", searches) {
		t.Fatal("a genuine result title equal to the query should be accepted when it appears in the result body")
	}
}
