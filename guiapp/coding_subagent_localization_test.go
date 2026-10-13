package guiapp

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
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

func TestCodegraphIndexReadyRequiresDatabase(t *testing.T) {
	t.Setenv("CODEGRAPH_DIR", "")
	dir := t.TempDir()
	if codegraphIndexReady(dir) || codegraphIndexReady("") || codegraphIndexReady(" ") {
		t.Fatal("missing or blank project must not count as indexed")
	}
	if err := os.Mkdir(filepath.Join(dir, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".codegraph", ".gitignore"), []byte("*\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if codegraphIndexReady(dir) {
		t.Fatal("gitignore-only .codegraph must not skip init")
	}
	db := filepath.Join(dir, ".codegraph", "codegraph.db")
	if err := os.WriteFile(db, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if codegraphIndexReady(dir) {
		t.Fatal("empty codegraph.db must not count as an index")
	}
	if err := os.WriteFile(db, []byte("sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !codegraphIndexReady(dir) {
		t.Fatal("non-empty codegraph.db should count as an index")
	}
}

func TestCodegraphContinueHintStaysOnAfterAHit(t *testing.T) {
	t.Setenv("CODEGRAPH_DIR", "")
	hint := codegraphContinueHint("codegraph")
	if !strings.Contains(hint, "call code_navigation") || !strings.Contains(hint, "evidence") || !strings.Contains(hint, "Do not repeat") || strings.Contains(hint, "again") || strings.Contains(hint, "only") {
		t.Fatalf("hit hint = %q", hint)
	}
	if strings.Contains(hint, "stale") || strings.Contains(hint, "before ripgrep") {
		t.Fatalf("hit hint retracts the lookup: %q", hint)
	}
	fallback := codegraphContinueHint("rg")
	if strings.Contains(fallback, "index is ready") || strings.Contains(fallback, "before ripgrep") || strings.Contains(fallback, "stale") {
		t.Fatalf("a text fallback must not withdraw the lookup: %q", fallback)
	}
	if codingCommandLooksLikeTextSearch("Get-ChildItem F:\\test-prog") || codingCommandLooksLikeTextSearch("git -C F:\\test-prog log --oneline -10") {
		t.Fatal("listing and git log are not text searches")
	}
	if !codingCommandLooksLikeTextSearch("Select-String -Path F:\\test-prog\\snake.cpp -Pattern Cue") || !codingCommandLooksLikeTextSearch("rg -n playTone .") {
		t.Fatal("Select-String and rg should remind the model to stay on codegraph")
	}
	dir := t.TempDir()
	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{projectPath: dir}}
	if got := cb.appendCodegraphSearchHint("match.go:1:playTone"); got != "match.go:1:playTone" {
		t.Fatalf("no index hint = %q", got)
	}
	if err := os.Mkdir(filepath.Join(dir, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".codegraph", "codegraph.db"), []byte("sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := cb.appendCodegraphSearchHint("match.go:1:playTone")
	if !strings.Contains(got, "match.go:1:playTone") || !strings.Contains(got, "already ran and counts") || !strings.Contains(got, "Do not repeat") || strings.Contains(got, "call code_navigation") || strings.Contains(got, "read_file only") || strings.Contains(got, "stale") || strings.Contains(got, "before ripgrep") || strings.Contains(got, "again") {
		t.Fatalf("indexed search hint = %q", got)
	}
	fn, _ := buildCodeNavigationToolDefinition()["function"].(map[string]interface{})
	desc, _ := fn["description"].(string)
	if strings.Contains(desc, "stale") || strings.Contains(desc, "only when that fails") || strings.Contains(desc, "again") {
		t.Fatalf("tool description retracts a finished lookup: %q", desc)
	}
	if strings.Contains(buildFullCodingEnvironmentPromptPreamble(), "更早一次没有索引") || strings.Contains(buildUncorrelatedLocalCodingCompatibilityPrompt(nil), "returns nothing") {
		t.Fatal("prompts still tell the model a finished text search does not count")
	}
	if got := cb.appendCodegraphSearchHint("no matches found (searched 12 files)"); strings.Contains(got, "Do not repeat") || strings.Contains(got, "already ran") {
		t.Fatalf("a miss must stay searchable: %q", got)
	}
	emptyShell := cb.appendCodegraphSearchHint("command exited with code 1 (no stdout/stderr captured). This typically happens when pipeline filters consume all output.")
	if strings.Contains(emptyShell, "Do not repeat") || strings.Contains(emptyShell, "already ran") {
		t.Fatalf("an empty search command must stay searchable: %q", emptyShell)
	}
	quietHit := cb.appendCodegraphSearchHint(codingQuietSearchHitText + "\ncommand exited with code 0")
	if strings.Contains(quietHit, "Do not repeat") || strings.Contains(quietHit, "already ran") || !strings.Contains(quietHit, "quiet mode") {
		t.Fatalf("a quiet hit must stay open so the lines can be shown: %q", quietHit)
	}
	noOutput := cb.appendCodegraphSearchHint("(command completed with no output)\ncommand exited with code 0")
	if strings.Contains(noOutput, "Do not repeat") || strings.Contains(noOutput, "already ran") {
		t.Fatalf("a search that printed nothing must stay searchable: %q", noOutput)
	}
	withStatus := cb.appendCodegraphSearchHint("a.go:1:playTone\ncommand exited with code 0")
	if !strings.Contains(withStatus, "a.go:1:playTone") || !strings.Contains(withStatus, "Do not repeat") {
		t.Fatalf("a hit followed by the exit status still counts: %q", withStatus)
	}
	paged := cb.appendCodegraphSearchHint("match.go:1:playTone\n... reached max_results=100")
	if !strings.Contains(paged, "match.go:1:playTone") || !strings.Contains(paged, "already ran and counts") || !strings.Contains(paged, "offset") || strings.Contains(paged, "Do not repeat") {
		t.Fatalf("a capped search must stay pageable: %q", paged)
	}
	stopped := cb.appendCodegraphSearchHint("early.go:1:tone\n... search stopped before the tree was finished")
	if !strings.Contains(stopped, "early.go:1:tone") || !strings.Contains(stopped, "narrower path") || strings.Contains(stopped, "Do not repeat") {
		t.Fatalf("a walk that stopped early must stay open: %q", stopped)
	}
	onlyStop := cb.appendCodegraphSearchHint("... search stopped before the tree was finished")
	if strings.Contains(onlyStop, "already ran") || strings.Contains(onlyStop, "Do not repeat") {
		t.Fatalf("a stop with no match lines must stay unmarked: %q", onlyStop)
	}
	quoted := cb.appendCodegraphSearchHint("note.go:3:log says reached max_results=100")
	if strings.Contains(quoted, "offset") || !strings.Contains(quoted, "Do not repeat") || !strings.Contains(quoted, "note.go:3:") {
		t.Fatalf("a source mention of the cap is a normal hit: %q", quoted)
	}
	if strings.Join(navigationSearchTokens("sound audio cue beep snake game", 3), ",") != "cue,snake,sound" {
		t.Fatalf("generic words crowded out the project name: %v", navigationSearchTokens("sound audio cue beep snake game", 3))
	}
	cleaned, hit := takeRemoteCodegraphHitMarker("symbol Cue\n" + remoteCodegraphHitMarker + "\n")
	if !hit || strings.Contains(cleaned, remoteCodegraphHitMarker) || !strings.Contains(cleaned, "symbol Cue") {
		t.Fatalf("marker cleaned=%q hit=%v", cleaned, hit)
	}
	mention := "snake.cpp:10:const marker = \"" + remoteCodegraphHitMarker + "\""
	cleaned, hit = takeRemoteCodegraphHitMarker(mention + "\n")
	if hit || cleaned != mention+"\n" {
		t.Fatalf("source mention cleaned=%q hit=%v", cleaned, hit)
	}
}

func TestCodegraphNearestIndexUsesParent(t *testing.T) {
	t.Setenv("CODEGRAPH_DIR", "")
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if codegraphNearestIndexRoot("") != "" || codegraphNearestIndexRoot("child") != "" {
		t.Fatal("blank and relative projects must not climb into another index")
	}
	if err := os.Mkdir(filepath.Join(parent, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, ".codegraph", "codegraph.db"), []byte("sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := codegraphNearestIndexRoot(child); got != filepath.Clean(parent) {
		t.Fatalf("parent index = %q, want %q", got, filepath.Clean(parent))
	}
	if err := os.Mkdir(filepath.Join(child, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, ".codegraph", "codegraph.db"), []byte("sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := codegraphNearestIndexRoot(child); got != filepath.Clean(child) {
		t.Fatalf("child index = %q, want %q", got, filepath.Clean(child))
	}
	codegraphMarkIndexBuilding(child, true)
	t.Cleanup(func() { codegraphMarkIndexBuilding(child, false) })
	if codegraphNearestIndexRoot(child) != "" {
		t.Fatal("an index still being written must not be queried or replaced by the parent")
	}
}

func TestCodeNavigationFallbackFindsDefinition(t *testing.T) {
	if !codegraphIndexReady(filepath.Join("..")) {
		t.Skip("workspace CodeGraph database unavailable")
	}
	backend, output, err := runLocalCodeNavigation("..", "definition", "summarizeSubAgentExploration", 2)
	if err != nil {
		t.Fatal(err)
	}
	if backend == "" || !strings.Contains(output, "summarizeSubAgentExploration") {
		t.Fatalf("backend=%q output=%q", backend, output)
	}
}

func TestRemoteCodeNavigationShellPrefersCodeGraphThenFallsBack(t *testing.T) {
	explore := remoteCodeNavigationShell("explore", "how auth works", "how auth works", "", 0, 0, true)
	for _, want := range []string{
		`_cg_dir=.codegraph`,
		`[ -s "$_cg_dir/codegraph.db" ]`,
		"CODEGRAPH_DIR",
		"command -v codegraph",
		"codegraph.cmd",
		`"$_cg_bin" query`,
		"--limit",
		"-ge 2000",
		"[[:alpha:]]",
		"printf '\\033'",
		"npm install -g '@colbymchenry/codegraph'",
		"npm prefix -g",
		"$_cg_prefix/bin/codegraph",
		`"$_cg_bin" init .`,
		`"$_cg_bin" status`,
		"kill -0",
		"-lt 20",
		"_cg_kill_tree",
		"ppid",
		"/proc/",
		"PPid",
		"_cg_running",
		"_cg_has_db",
		"dirname",
		"MACLAW_CODEGRAPH_HIT",
		`_cg_rc=$?`,
		`[ "$_cg_rc" -eq 0 ]`,
		`"$_cg_bin" unlock .`,
		`rm -f "$_cg_dir/codegraph.db"`,
		"_cg_hit",
		`if [ "$_cg_hit" -eq 0 ]`,
		"rg -n",
		"grep -RIn",
	} {
		if !strings.Contains(explore, want) {
			t.Fatalf("explore shell missing %q: %s", want, explore)
		}
	}
	plain := remoteCodeNavigationShell("explore", "how auth works", "how auth works", "", 0, 0, false)
	if strings.Contains(plain, "npm install") || strings.Contains(plain, "init .") || strings.Contains(plain, ">/dev/null") || strings.Contains(plain, "rm -f") || strings.Contains(plain, "unlock .") {
		t.Fatalf("read-only shell must not install, init, delete the index, or redirect to a file: %s", plain)
	}
	if !strings.Contains(plain, "npm prefix -g") || !strings.Contains(plain, "$_cg_prefix/bin/codegraph") || !strings.Contains(plain, "_cg_has_db") {
		t.Fatalf("plain shell should still resolve an off-PATH install and an existing parent index: %s", plain)
	}
	if msg := rejectDisallowedCodingBashCommand(explore); msg != "" {
		t.Fatalf("bootstrap navigation was rejected as a shell write: %s\n%s", msg, explore)
	}
	if msg := rejectDisallowedCodingBashCommand(plain); msg != "" {
		t.Fatalf("plain navigation was rejected as a shell write: %s\n%s", msg, plain)
	}
	if remoteSSHWaitSeconds(explore) != 300 {
		t.Fatalf("bootstrap navigation should wait for install/init, wait=%d", remoteSSHWaitSeconds(explore))
	}
	if remoteSSHWaitSeconds(plain) != 60 {
		t.Fatalf("plain navigation wait=%d", remoteSSHWaitSeconds(plain))
	}
	node := remoteCodeNavigationShell("node", "guiapp/coding_subagent.go", "ignored", "", 0, 0, false)
	if !strings.Contains(node, `"$_cg_bin" node`) || !strings.Contains(node, "--symbols-only") || strings.Contains(node, `"$_cg_bin" explore`) {
		t.Fatalf("node shell = %s", node)
	}
	def := remoteCodeNavigationShell("definition", "Foo", "Find the definition of Foo", "pkg/foo.go", 10, 2, false)
	if !strings.Contains(def, `"$_cg_bin" node`) || strings.Contains(def, "--symbols-only") || strings.Contains(def, `"$_cg_bin" explore`) || !strings.Contains(def, "gopls definition") || !strings.Contains(def, "lsp-cli") || !strings.Contains(def, "## Foo ") || !strings.Contains(def, `definitions named "Foo"`) {
		t.Fatalf("definition shell = %s", def)
	}
	qualified := codegraphRemoteAcceptNamedNode("Audio::playTone")
	if !strings.Contains(qualified, "## playTone ") || strings.Contains(qualified, "## playTon ") {
		t.Fatalf("qualified node shell = %s", qualified)
	}
	assertRemoteNavigationShellSyntax(t, qualified)
	callers := remoteCodeNavigationShell("callers", "Foo", "Find callers of Foo", "", 0, 0, false)
	if !strings.Contains(callers, `"$_cg_bin" callers`) || !strings.Contains(callers, `"$_cg_bin" node`) || !strings.Contains(callers, "## Foo ") || !strings.Contains(callers, "_cg_named") {
		t.Fatalf("callers shell = %s", callers)
	}
	if remoteSSHWaitSeconds(callers) != 120 || remoteSSHWaitSeconds(def) != 60 {
		t.Fatalf("callers wait=%d definition wait=%d", remoteSSHWaitSeconds(callers), remoteSSHWaitSeconds(def))
	}
	parent, parentCancel := context.WithTimeout(context.Background(), time.Millisecond)
	parentCancel()
	step, stepCancel := codegraphStepContext()
	defer stepCancel()
	if parent.Err() == nil || step.Err() != nil {
		t.Fatal("an expired navigation clock must not cancel the callers query")
	}
	if deadline, ok := step.Deadline(); !ok || time.Until(deadline) < 20*time.Second {
		t.Fatalf("callers budget = %v ok=%v", deadline, ok)
	}
	symbol := remoteCodeNavigationShell("explore", "AuthService", "AuthService", "", 0, 0, false)
	if !strings.Contains(symbol, `"$_cg_bin" node`) || strings.Contains(symbol, `"$_cg_bin" explore`) || strings.Contains(symbol, "--symbols-only") {
		t.Fatalf("symbol explore should use node: %s", symbol)
	}
	fileSym := remoteCodeNavigationShell("file_symbols", "snake.cpp", "snake.cpp", "", 0, 0, false)
	if !strings.Contains(fileSym, "[ -f 'snake.cpp' ]") || !strings.Contains(fileSym, "head -n 40") || !strings.Contains(fileSym, "find . -maxdepth 5") || !strings.Contains(fileSym, "tail -n 40") {
		t.Fatalf("file symbol shell = %s", fileSym)
	}
	outside := remoteCodeNavigationShell("file_symbols", "../secret.cpp", "../secret.cpp", "", 0, 0, false)
	if strings.Contains(outside, "[ -f '../secret.cpp' ]") || strings.Contains(outside, "head -n 40 '../secret.cpp'") || strings.Contains(outside, "grep -nE "+remoteShellQuote(navigationRemoteFileSymbolPattern)+" '../secret.cpp'") {
		t.Fatalf("outline must not open a path outside the project: %s", outside)
	}
	if !strings.Contains(outside, "-iname 'secret.cpp'") {
		t.Fatalf("outside path should search by basename: %s", outside)
	}
	abs := remoteCodeNavigationShell("file_symbols", "/etc/passwd", "/etc/passwd", "", 0, 0, false)
	if strings.Contains(abs, "[ -f '/etc/passwd' ]") || strings.Contains(abs, "head -n 40 '/etc/passwd'") || !strings.Contains(abs, "-iname 'passwd'") {
		t.Fatalf("absolute path shell = %s", abs)
	}
	if !navigationQueryEscapesRoot(`..\secret.cpp`) || navigationQueryEscapesRoot("src/snake.cpp") {
		t.Fatal("path escape check mismatch")
	}
	multi := remoteCodeNavigationShell("explore", "visual polish frame screenshot bmp capture checks", "visual polish frame screenshot bmp capture checks", "", 0, 0, false)
	if !strings.Contains(multi, "for _cg_term in") || !strings.Contains(multi, "'bmp'") || !strings.Contains(multi, "'screenshot'") || !strings.Contains(multi, "'capture'") || !strings.Contains(multi, "_cg_miss") {
		t.Fatalf("multi-token shell = %s", multi)
	}
	for _, script := range []string{explore, plain, node, def, callers, symbol, fileSym, outside, abs, multi} {
		assertRemoteNavigationShellSyntax(t, script)
		if msg := rejectDisallowedCodingBashCommand(script); msg != "" {
			t.Fatalf("navigation shell was rejected: %s\n%s", msg, script)
		}
	}
}

func TestCodegraphRemoteEdgeSkipsNeighbor(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	run := func(symbol, callersText string) string {
		t.Helper()
		script := `cg() { if [ "$1" = node ]; then printf '%s\n' '## playTone (function)'; return 0; fi; printf '%s\n' ` + remoteShellQuote(callersText) + `; }
_cg_bin=cg
_cg_hit=0
` + codegraphRemoteEdgeAfterExactNode("callers", symbol, "") + `
printf '%s\n' "HIT:$_cg_hit"
`
		out, err := exec.Command(bash, "-c", script).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", symbol, err, out)
		}
		return string(out)
	}
	if miss := run("playTon", "CALLERS-RAN"); strings.Contains(miss, "CALLERS-RAN") || strings.Contains(miss, "HIT:1") {
		t.Fatalf("neighbor callers ran: %s", miss)
	}
	if hit := run("playTone", "CALLERS-RAN"); !strings.Contains(hit, "CALLERS-RAN") || !strings.Contains(hit, "HIT:1") {
		t.Fatalf("exact callers did not run: %s", hit)
	}
	if qual := run("Audio::playTone", "CALLERS-RAN"); strings.Contains(qual, "CALLERS-RAN") || strings.Contains(qual, "HIT:1") {
		t.Fatalf("qualified name must not run callers: %s", qual)
	}
	if empty := run("playTone", `No callers found for "playTone"`); strings.Contains(empty, "HIT:1") {
		t.Fatalf("empty caller list counted as a hit: %s", empty)
	}
	trailScript := `cg() { printf '%s\n' '## playTone (function)' '**Location:** snake.cpp:792' '**Called by ←** main (snake.cpp:10)' '**Calls →** beep (snake.cpp:40)' '**Calls the mixer' '## audioShutdown (function)' '**Location:** snake.cpp:800' '**Called by ←** other (snake.cpp:1)'; }
_cg_bin=cg
_cg_hit=0
` + codegraphRemoteEdgeAfterExactNode("callers", "Audio::playTone", "") + `
printf '%s\n' "HIT:$_cg_hit"
`
	trailOut, err := exec.Command(bash, "-c", trailScript).CombinedOutput()
	if err != nil {
		t.Fatalf("qualified trail: %v\n%s", err, trailOut)
	}
	trail := string(trailOut)
	if !strings.Contains(trail, "## playTone") || !strings.Contains(trail, "snake.cpp:792") || !strings.Contains(trail, "main (snake.cpp:10)") || !strings.Contains(trail, "HIT:1") || strings.Contains(trail, "beep (snake.cpp:40)") || strings.Contains(trail, "audioShutdown") || strings.Contains(trail, "other (snake.cpp:1)") || strings.Contains(trail, "Calls the mixer") {
		t.Fatalf("qualified callers = %s", trail)
	}
	full := remoteCodeNavigationShell("callers", "Audio::playTone", "Find callers of Audio::playTone", "", 0, 0, false)
	if strings.Contains(full, `"$_cg_bin" callers`) || !strings.Contains(full, "Called by") {
		t.Fatalf("qualified callers shell = %s", full)
	}
	if msg := rejectDisallowedCodingBashCommand(full); msg != "" {
		t.Fatalf("qualified callers shell rejected: %s", msg)
	}
	assertRemoteNavigationShellSyntax(t, full)
	if remoteSSHWaitSeconds(full) != 60 {
		t.Fatalf("qualified callers is one node lookup, wait=%d", remoteSSHWaitSeconds(full))
	}
	if !codegraphSymbolIsQualified("Audio::playTone") || !codegraphSymbolIsQualified("Session.request") || codegraphSymbolIsQualified("playTone") {
		t.Fatal("qualifier detection")
	}
	probe := "## audioShutdown (function)\n**Location:** snake.cpp:800\n**Called by ←** other (snake.cpp:1)\n## playTone (function)\n**Location:** snake.cpp:792\n792\tvoid playTone() {}\n**Called by ←** main (snake.cpp:10)\n**Calls →** beep (snake.cpp:40)\n**Calls the mixer\n"
	callers := codegraphQualifiedEdges(probe, "Audio::playTone", "callers")
	callees := codegraphQualifiedEdges(probe, "Audio::playTone", "callees")
	if callers != "## playTone (function)\n**Location:** snake.cpp:792\n**Called by ←** main (snake.cpp:10)" || callees != "## playTone (function)\n**Location:** snake.cpp:792\n**Calls →** beep (snake.cpp:40)" {
		t.Fatalf("trail callers=%q callees=%q", callers, callees)
	}
	pinned := codegraphCommandList("callers", "playTone", "callers", "snake.cpp")
	if len(pinned) != 1 || strings.Join(pinned[0], " ") != "callers playTone --file snake.cpp" {
		t.Fatalf("pinned callers = %v", pinned)
	}
	pinnedDef := codegraphCommandList("definition", "playTone", "def", "snake.cpp")
	if len(pinnedDef) != 1 || strings.Join(pinnedDef[0], " ") != "node --file snake.cpp playTone" {
		t.Fatalf("pinned definition = %v", pinnedDef)
	}
	if outside := codegraphCommandList("callers", "playTone", "callers", "../snake.cpp"); len(outside) != 1 || len(outside[0]) != 2 {
		t.Fatalf("outside file must not pin: %v", outside)
	}
	pinnedShell := remoteCodeNavigationShell("callers", "playTone", "Find callers of playTone", "snake.cpp", 0, 0, false)
	if !strings.Contains(pinnedShell, "--file") || !strings.Contains(pinnedShell, "snake.cpp") || strings.Contains(pinnedShell, `"$_cg_bin" callers`) {
		t.Fatalf("pinned callers shell = %s", pinnedShell)
	}
	if msg := rejectDisallowedCodingBashCommand(pinnedShell); msg != "" {
		t.Fatalf("pinned callers shell rejected: %s", msg)
	}
	assertRemoteNavigationShellSyntax(t, pinnedShell)
	if strings.Contains(remoteCodeNavigationShell("callers", "playTone", "Find callers of playTone", "../snake.cpp", 0, 0, false), "--file") {
		t.Fatal("callers must not pin a path outside the project")
	}
}

func TestCodegraphOutputUsefulIgnoresMissBanner(t *testing.T) {
	if codegraphOutputUseful("") || codegraphOutputUseful(" \n\t") {
		t.Fatal("blank output should fall back")
	}
	if codegraphOutputUseful("No callers found for \"playTone\"") || codegraphOutputUseful("No callees found for \"playTone\"") {
		t.Fatal("an empty edge list should fall back to text search")
	}
	if !codegraphOutputUseful("## playTone (function)\nNo callers found in a comment\n") {
		t.Fatal("source that mentions an empty edge list should stay")
	}
	if codegraphOutputUseful("\x1b[34m[i]\x1b[0m No results found for \"missing\"") {
		t.Fatal("query miss should fall back")
	}
	if codegraphOutputUseful("\x1b[34m[i]\x1b[0m Symbol \"missing\" not found") {
		t.Fatal("callers miss should fall back")
	}
	if codegraphOutputUseful("No indexed file matches \"missing.go\". Read it directly.") {
		t.Fatal("missing file should fall back")
	}
	if !codegraphOutputUseful("## AuthService (function)\n\nfunc AuthService() {}") {
		t.Fatal("a real symbol result should be kept")
	}
	if !codegraphNodeNamesSymbol("## playTone (function)\n\n**Location:** snake.cpp:792\n", "playTone") || !codegraphNodeNamesSymbol("## playTone (function)\n", "Audio::playTone") || !codegraphNodeNamesSymbol("## request (function)\n", "Session.request") {
		t.Fatal("exact and qualified node headings should be kept")
	}
	if codegraphNodeNamesSymbol("## playTone (function)\nplayTon is not this symbol\n", "playTon") || codegraphNodeNamesSymbol("## audioShutdown (function)\n", "Audio::playTone") || codegraphNodeNamesSymbol("## playTone (function)\n## playTon (function)\n", "playTon") {
		t.Fatal("the nearest symbol must not count as the name that was asked")
	}
	if !codegraphNodeNamesSymbol("**2 definitions named \"playTone\"**\n", "playTone") {
		t.Fatal("an overload heading should be kept")
	}
	if !codegraphCommandAccepted([]string{"node", "playTone"}, "## playTone (function)\n") || codegraphCommandAccepted([]string{"node", "playTon"}, "## playTone (function)\n") {
		t.Fatal("node acceptance must require the requested name")
	}
	if !codegraphNodeNamesSymbol("## playTone (function)\n", "playTone") || codegraphNodeNamesSymbol("## playTone (function)\n", "playTon") {
		t.Fatal("edge lookups need the same heading check as node")
	}
	if codegraphOutputUseful("Search Results for \"bmp\"\nconstant MNS_CHECKORBMP (1380%)\n  constants.go:3299") {
		t.Fatal("a weak fuzzy score should fall back")
	}
	if !codegraphOutputUseful("## Auth\n\nconst sample = (1380%)\n") {
		t.Fatal("a percentage inside source is not a query ranking")
	}
	if !codegraphOutputUseful("func Example() {\n\tlog(\"Search Results\")\n\tconst sample = (1380%)\n}\n") {
		t.Fatal("source that mentions a query header should stay")
	}
	if codegraphOutputUseful("\x1b[1m\nSearch Results for \"bmp\":\nconstant MNS_CHECKORBMP (1380%)\n") {
		t.Fatal("a weak score after a color-only line should fall back")
	}
	if !codegraphOutputUseful("Search Results for \"screenshot\"\nmethod screenshot (9090%)\n  file.go:1") {
		t.Fatal("a strong query score should be kept")
	}
	if got := strings.Join(codegraphArgs("definition", "Foo", "Find the definition of Foo"), " "); got != "node Foo" {
		t.Fatalf("definition args = %s", got)
	}
	if got := strings.Join(codegraphArgs("explore", "AuthService", "AuthService"), " "); got != "node AuthService" {
		t.Fatalf("symbol explore args = %s", got)
	}
	if got := strings.Join(codegraphArgs("explore", "how auth works", "how auth works"), " "); got != "query --limit 4 auth" {
		t.Fatalf("area explore args = %s", got)
	}
	tokens := navigationSearchTokens("visual polish frame screenshot bmp capture checks", 3)
	if strings.Join(tokens, ",") != "bmp,screenshot,capture" {
		t.Fatalf("search tokens = %v", tokens)
	}
	ranked := navigationSearchTokens("game screenshot capture", 3)
	if strings.Join(ranked, ",") != "screenshot,capture,game" {
		t.Fatalf("short words should not outrank longer ones: %v", ranked)
	}
	noisy := navigationSearchTokens("fix int overflow in screenshot bmp", 3)
	if strings.Join(noisy, ",") != "bmp,screenshot,overflow" {
		t.Fatalf("generic words should not take a query slot: %v", noisy)
	}
	cmds := codegraphCommandList("explore", "visual polish frame screenshot bmp capture checks", "visual polish frame screenshot bmp capture checks", "")
	if len(cmds) != 3 || strings.Join(cmds[0], " ") != "query --limit 4 bmp" {
		t.Fatalf("commands = %v", cmds)
	}
	if got := strings.Join(codegraphArgs("explore", "认证是怎么做的", "认证是怎么做的"), " "); got != "query --limit 4 认证是怎么做的" {
		t.Fatalf("prose explore args = %s", got)
	}
	if got := strings.Join(codegraphArgs("file_symbols", "snake.cpp", "snake.cpp"), " "); got != "node --symbols-only snake.cpp" {
		t.Fatalf("file symbol args = %s", got)
	}
	phrase := navigationFallbackPattern("explore", "visual polish frame screenshot bmp capture checks")
	if strings.Contains(phrase, "visual polish") || !strings.Contains(phrase, "screenshot") || !strings.Contains(phrase, "bmp") {
		t.Fatalf("explore fallback pattern = %s", phrase)
	}
}

func TestNavigationFileSymbolOutlineWithoutIndex(t *testing.T) {
	dir := t.TempDir()
	body := "#include <stdio.h>\n#define CELL_SIZE 20\nstruct Point { int x; int y; };\nPAINTSTRUCT ps;\n// pure function of the state\n\nstatic void playTone(int hz) {\n  if (hz < 1) {\n    return;\n  }\n}\n\nint main(void) {\n  playTone(440);\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "snake.cpp"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	backend, output, err := runLocalNavigationTextFallback(ctx, dir, "file_symbols", "snake.cpp")
	if err != nil {
		t.Fatal(err)
	}
	if backend != "file" || !strings.Contains(output, "playTone") || !strings.Contains(output, "main(") || !strings.Contains(output, "CELL_SIZE") || !strings.Contains(output, "struct Point") {
		t.Fatalf("backend=%s output=%s", backend, output)
	}
	if strings.Contains(output, "if (hz") || strings.Contains(output, "playTone(440)") || strings.Contains(output, "PAINTSTRUCT") || strings.Contains(output, "pure function") {
		t.Fatalf("calls, comments, or substrings leaked into the outline: %s", output)
	}
	outside := filepath.Join(filepath.Dir(dir), "nav-outside.cpp")
	if err := os.WriteFile(outside, []byte("void secret() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	if _, ok := navigationFileSymbolOutline(dir, outside); ok {
		t.Fatal("outline must stay inside the project")
	}
	deep := filepath.Join(dir, "a", "deep")
	shallow := filepath.Join(dir, "shallow")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(shallow, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "widget.cpp"), []byte("void deepFn() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shallow, "widget.cpp"), []byte("void shallowFn() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gotFile := navigationResolveFile(dir, "missing/widget.cpp")
	if !strings.HasSuffix(filepath.ToSlash(gotFile), "shallow/widget.cpp") {
		t.Fatalf("basename search returned %s", gotFile)
	}
	var many strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&many, "void fn%03d() {\n}\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "many.cpp"), []byte(many.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	_, manyOut, err := runLocalNavigationTextFallback(ctx, dir, "file_symbols", "many.cpp")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(manyOut, "fn000") || !strings.Contains(manyOut, "fn099") {
		t.Fatalf("outline dropped one end of the file: %s", manyOut)
	}
	def := regexp.MustCompile(navigationFallbackPattern("definition", "playTone"))
	if !def.MatchString("static void playTone(int freq, int durationMs) {") || def.MatchString("playTone(440);") {
		t.Fatalf("definition pattern = %s", def.String())
	}
	classDef := regexp.MustCompile(navigationFallbackPattern("definition", "Game"))
	if !classDef.MatchString("class Game {") || !classDef.MatchString("Game() { reset(); }") {
		t.Fatal("class and constructor should match")
	}
}

func TestCodegraphIndexFailureKeyAndBootstrapRetry(t *testing.T) {
	dir := t.TempDir()
	codegraphRememberIndexFailure(dir + string(filepath.Separator))
	failed := codegraphIndexAttemptFailed(dir)
	codegraphIndexMu.Lock()
	delete(codegraphIndexFailed, codegraphIndexFailureKey(dir))
	codegraphIndexMu.Unlock()
	if !failed {
		t.Fatal("trailing separator should share the cleaned failure key")
	}
	c := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{projectDir: dir}}
	claimed, release := c.claimRemoteCodegraphBootstrap()
	if !claimed {
		t.Fatal("first claim")
	}
	if remoteCodegraphQueryAllowed(dir, false) {
		t.Fatal("index init in progress must not be queried")
	}
	if !remoteCodegraphQueryAllowed(dir, true) {
		t.Fatal("the call that is initializing may query after init")
	}
	if again, _ := c.claimRemoteCodegraphBootstrap(); again {
		t.Fatal("claim must be exclusive")
	}
	release(false)
	claimed, release = c.claimRemoteCodegraphBootstrap()
	if !claimed {
		t.Fatal("a command that never ran should be retried")
	}
	release(true)
	if again, _ := c.claimRemoteCodegraphBootstrap(); again {
		t.Fatal("a completed bootstrap must not retry")
	}
	if !remoteCodegraphQueryAllowed(dir, false) {
		t.Fatal("a finished init should be queried")
	}
	held := remoteCodeNavigationCommand("explore", "auth", "auth", "", 0, 0, false, false)
	if strings.Contains(held, `"$_cg_bin"`) || strings.Contains(held, "init .") {
		t.Fatal("a call during init must not read or rebuild the partial index")
	}
	unlockA := codegraphLockProject(dir)
	other := t.TempDir()
	done := make(chan struct{})
	go func() {
		unlockB := codegraphLockProject(other)
		unlockB()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("one project's init must not block another")
	}
	unlockA()
	inquiry := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{projectDir: dir + "-inquiry", readOnlyInquiry: true}}
	if claimed, _ := inquiry.claimRemoteCodegraphBootstrap(); claimed {
		t.Fatal("inquiry must not install codegraph")
	}
	explorer := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{projectDir: dir + "-explorer", nestDepth: 1, role: codingRoleExplorer}}
	if claimed, _ := explorer.claimRemoteCodegraphBootstrap(); claimed {
		t.Fatal("explorer must not install codegraph")
	}
	operational := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{projectDir: dir + "-op", operationalRequest: true}}
	if claimed, _ := operational.claimRemoteCodegraphBootstrap(); claimed {
		t.Fatal("operational turn must not install codegraph")
	}
	worker := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{projectDir: dir + "-worker", nestDepth: 1, role: codingRoleWorker}}
	if claimed, release := worker.claimRemoteCodegraphBootstrap(); !claimed {
		t.Fatal("worker should install codegraph when the index is missing")
	} else {
		release(false)
	}
	localInquiry := &codingSubAgentCallbacks{task: &TaskItem{RequestKind: codingRequestInquiry}}
	if localInquiry.codegraphBootstrapAllowed() {
		t.Fatal("local inquiry must not init codegraph")
	}
	localImpl := &codingSubAgentCallbacks{task: &TaskItem{RequestKind: codingRequestImplementation}, subagent: &CodingSubAgent{correlatedLocalExecution: true}}
	if !localImpl.codegraphBootstrapAllowed() {
		t.Fatal("implementation turn should init a missing codegraph index")
	}
	localExplorer := &codingSubAgentCallbacks{task: &TaskItem{RequestKind: codingRequestImplementation}, subagent: &CodingSubAgent{correlatedLocalExecution: true, nestDepth: 1, role: codingRoleExplorer}}
	if localExplorer.codegraphBootstrapAllowed() {
		t.Fatal("local explorer must not init codegraph")
	}
}

func TestCodegraphRemovePartialIndexDropsInterruptedDatabase(t *testing.T) {
	t.Setenv("CODEGRAPH_DIR", "")
	dir := t.TempDir()
	base := filepath.Join(dir, ".codegraph")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "codegraph.db"), []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "daemon.pid"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !codegraphIndexReady(dir) {
		t.Fatal("partial db should look ready before cleanup")
	}
	if !codegraphIndexUsable(dir) {
		t.Fatal("a database nobody is writing should be usable")
	}
	codegraphMarkIndexBuilding(dir, true)
	if codegraphIndexUsable(dir) {
		t.Fatal("a database init is still writing must not be queried")
	}
	codegraphMarkIndexBuilding(dir, false)
	codegraphRemovePartialIndex(dir)
	if codegraphIndexReady(dir) {
		t.Fatal("partial db should be removed")
	}
	if _, err := os.Stat(filepath.Join(base, "daemon.pid")); !os.IsNotExist(err) {
		t.Fatal("stale daemon pid should be removed")
	}
	if codegraphInitResultUsable(nil, true, false) != true {
		t.Fatal("a clean init that left a database should be kept")
	}
	if codegraphInitResultUsable(context.DeadlineExceeded, true, true) != true {
		t.Fatal("a timed-out wait should keep a database status accepts")
	}
	if codegraphInitResultUsable(context.DeadlineExceeded, true, false) {
		t.Fatal("a timed-out wait should drop a database status rejects")
	}
	if codegraphInitResultUsable(nil, false, true) {
		t.Fatal("a missing database is not usable")
	}
	if !codegraphInstallFailureIsFinal(nil) {
		t.Fatal("a finished npm error should not be retried")
	}
	if codegraphInstallFailureIsFinal(context.DeadlineExceeded) {
		t.Fatal("an interrupted npm install should be retried")
	}
	if !codegraphIndexStatusKeepsDatabase(nil, nil) {
		t.Fatal("status success should keep the database")
	}
	if !codegraphIndexStatusKeepsDatabase(context.DeadlineExceeded, context.DeadlineExceeded) {
		t.Fatal("a status timeout should keep a large database")
	}
	if codegraphIndexStatusKeepsDatabase(fmt.Errorf("database disk image is malformed"), nil) {
		t.Fatal("an explicit status failure should drop the fragment")
	}
	longName := strings.Repeat("n", 80)
	if !codegraphDataDirNameSafe(".codegraph-win") || !codegraphDataDirNameSafe(".codegraph win") || !codegraphDataDirNameSafe(longName) {
		t.Fatal("CODEGRAPH_DIR should accept the directory names codegraph accepts")
	}
	if codegraphDataDirNameSafe(".") || codegraphDataDirNameSafe("..") || codegraphDataDirNameSafe(`a/b`) || codegraphDataDirNameSafe(`..\x`) || codegraphDataDirNameSafe("a\"b") {
		t.Fatal("CODEGRAPH_DIR must stay inside the project as one directory name")
	}
	t.Setenv("CODEGRAPH_DIR", "  .codegraph win  ")
	if codegraphDataDirName() != ".codegraph win" {
		t.Fatalf("directory name = %q", codegraphDataDirName())
	}
	spaced := filepath.Join(dir, ".codegraph win")
	if err := os.MkdirAll(spaced, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(spaced, "codegraph.db"), []byte("sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !codegraphIndexReady(dir) {
		t.Fatal("ready check should trim CODEGRAPH_DIR and allow a space")
	}
	codegraphRemovePartialIndex(dir)
	if codegraphIndexReady(dir) {
		t.Fatal("cleanup should remove the spaced directory database")
	}
	t.Setenv("CODEGRAPH_DIR", ".codegraph-win")
	override := filepath.Join(dir, ".codegraph-win")
	if err := os.MkdirAll(override, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(override, "codegraph.db"), []byte("sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(dir, ".codegraph", "codegraph.db")
	if err := os.WriteFile(kept, []byte("default-index"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !codegraphIndexReady(dir) {
		t.Fatal("ready check should follow CODEGRAPH_DIR")
	}
	codegraphRemovePartialIndex(dir)
	if codegraphIndexReady(dir) {
		t.Fatal("cleanup should remove the CODEGRAPH_DIR database")
	}
	keptData, err := os.ReadFile(kept)
	if err != nil || string(keptData) != "default-index" {
		t.Fatal("cleanup of the override directory should leave the default database alone")
	}
}

func assertRemoteNavigationShellSyntax(t *testing.T, script string) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	cmd := exec.Command(bash, "-n", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("navigation shell syntax: %v\n%s\n%s", err, out, script)
	}
}

func TestRemoteCodegraphDirAssignmentTrimsAndKeepsSpaces(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	run := func(value string) string {
		t.Helper()
		cmd := exec.Command(bash, "-c", remoteCodegraphDirAssignment()+`printf '%s' "$_cg_dir"`)
		cmd.Env = []string{"CODEGRAPH_DIR=" + value, "PATH=" + os.Getenv("PATH")}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("CODEGRAPH_DIR=%q: %v\n%s", value, err, out)
		}
		return string(out)
	}
	if got := run("  .codegraph-win  "); got != ".codegraph-win" {
		t.Fatalf("trimmed = %q", got)
	}
	if got := run(".codegraph win"); got != ".codegraph win" {
		t.Fatalf("spaced = %q", got)
	}
	if got := run("../secret"); got != ".codegraph" {
		t.Fatalf("unsafe = %q", got)
	}
	if got := run("a\"b"); got != ".codegraph" {
		t.Fatalf("quote = %q", got)
	}
	if got := run(`a\b`); got != ".codegraph" {
		t.Fatalf("separator = %q", got)
	}
}

func TestRemoteCodegraphStatusKeepsFailedExitAndParentIndex(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	// A status command that exits immediately must not be treated as still
	// running. That mistake keeps a broken database and also spends the
	// whole 20s budget.
	script := remoteCodegraphProcessHelpers() + `
_cg_bin=false
start=$SECONDS
if _cg_db_ok; then echo KEPT; else echo DROPPED; fi
echo ELAPSED:$SECONDS
`
	cmd := exec.Command(bash, "-c", script)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("status wait: %v\n%s", err, out)
	}
	text := string(out)
	if !strings.Contains(text, "DROPPED") || strings.Contains(text, "KEPT") {
		t.Fatalf("failed status should drop the database: %s", text)
	}
	elapsed := -1
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ELAPSED:") {
			fmt.Sscanf(line, "ELAPSED:%d", &elapsed)
		}
	}
	if elapsed < 0 || elapsed > 4 {
		t.Fatalf("failed status should return as soon as the process exits, output: %s", text)
	}

	parent := t.TempDir()
	child := filepath.Join(parent, "pkg")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	walk := remoteCodegraphDirAssignment() + remoteCodegraphHasDBFunc() + `if _cg_has_db; then echo YES; else echo NO; fi`
	runWalk := func(dir string) string {
		t.Helper()
		cmd := exec.Command(bash, "-c", walk)
		cmd.Dir = dir
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "CODEGRAPH_DIR="}
		got, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("has db in %s: %v\n%s", dir, err, got)
		}
		return strings.TrimSpace(string(got))
	}
	if got := runWalk(child); got != "NO" {
		t.Fatalf("missing index = %q", got)
	}
	if err := os.Mkdir(filepath.Join(parent, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, ".codegraph", "codegraph.db"), []byte("sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runWalk(child); got != "YES" {
		t.Fatalf("parent index = %q", got)
	}

	killScript := remoteCodegraphProcessHelpers() + `
_cg_status=$(sleep 30) &
pid=$!
sleep 0.3
child=
for ent in /proc/[0-9]*; do
  ppid=$(sed -n 's/^PPid:[[:space:]]*//p' "$ent/status" 2>/dev/null || true)
  if [ "$ppid" = "$pid" ]; then child=${ent#/proc/}; fi
done
_cg_kill_tree "$pid"
sleep 0.3
if [ -n "$child" ] && [ -r "/proc/$child/status" ]; then echo SURVIVED; kill "$child" 2>/dev/null || true; else echo STOPPED; fi
`
	killCmd := exec.Command(bash, "-c", killScript)
	killCmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	killOut, err := killCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kill tree: %v\n%s", err, killOut)
	}
	if !strings.Contains(string(killOut), "STOPPED") {
		t.Fatalf("status child still running: %s", killOut)
	}
}

func TestCapCodeNavigationOutputKeepsRuneBoundary(t *testing.T) {
	short, truncated := capCodeNavigationOutput("abc")
	if truncated || short != "abc" {
		t.Fatalf("short = %q truncated=%v", short, truncated)
	}
	body := strings.Repeat("a", 23999) + "你"
	got, truncated := capCodeNavigationOutput(body)
	if !truncated || !utf8.ValidString(got) || len(got) > 24000 || strings.Contains(got, "你") {
		t.Fatalf("truncated=%v len=%d valid=%v gotSuffix=%q", truncated, len(got), utf8.ValidString(got), got[len(got)-8:])
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
		"故障定位 parser", "定位根因后修复",
	} {
		if !codingTaskNeedsLocalization(text) {
			t.Fatalf("bug task %q should require localization", text)
		}
	}
	for _, text := range []string{
		"探索项目现状与 CLI 缺口",
		"这次把定位报告的全部必填字段一次写进调用，不再只传文件名。",
		"先定位相关路径与符号，再深入阅读关键文件",
		"开发一个基于ClamAV引擎的C++系统病毒扫描工具",
	} {
		if codingTaskNeedsLocalization(text) {
			t.Fatalf("feature/exploration text %q must not trip the bug-fix localization gate", text)
		}
	}
	poisonedStep := "[Plan step T1/2] 探索项目现状与 CLI 缺口\n\n" +
		"You are executing plan step T1/2: 探索项目现状与 CLI 缺口\n" +
		"Focus on this step only; do not skip ahead.\n\n" +
		"## Overall user request (context only — not a license to finish later steps)\n" +
		"继续完成编程任务\n\n" +
		"Previous turn summary:\n" +
		"Prior plan step T1 (探索项目现状与 CLI 缺口) status=failed.\n" +
		"bug localization quality gate failed: missing localization evidence"
	if codingTaskNeedsLocalization(poisonedStep) {
		t.Fatal("carry-over failed status must not reclassify exploration as a bug-fix")
	}
	crlfPoisoned := strings.ReplaceAll(poisonedStep, "\n", "\r\n")
	crlfPoisoned = strings.ReplaceAll(crlfPoisoned, "Previous turn summary:", "PREVIOUS TURN SUMMARY:")
	if codingTaskNeedsLocalization(crlfPoisoned) {
		t.Fatal("carry-over header matching must be case-insensitive")
	}
}

func TestReportLocalizationSkippedForFeatureWork(t *testing.T) {
	remote := &remoteCodingCallbacks{task: "探索项目现状与 CLI 缺口"}
	got := remote.executeRemoteReportLocalization(map[string]interface{}{"root_cause_file": "src/main.cpp"})
	if !strings.Contains(got, "report_localization skipped") {
		t.Fatalf("feature work should skip localization: %s", got)
	}
	if remoteCodingToolOutcome(got) != "success" {
		t.Fatalf("skip must not look like a tool failure: %s -> %s", got, remoteCodingToolOutcome(got))
	}

	local := &codingSubAgentCallbacks{task: &TaskItem{Title: "开发一个基于ClamAV引擎的C++系统病毒扫描工具"}}
	res := local.executeReportLocalization(map[string]interface{}{"root_cause_file": "src/main.cpp"})
	if res.Outcome != codingToolOutcomeSuccess || !strings.Contains(res.Text, "report_localization skipped") {
		t.Fatalf("local feature work should skip localization: %+v", res)
	}

	bug := &remoteCodingCallbacks{task: "fix parser crash"}
	got = bug.executeRemoteReportLocalization(map[string]interface{}{"root_cause_file": "src/main.cpp"})
	if !strings.Contains(got, "invalid localization evidence") {
		t.Fatalf("bug-fix still needs a complete report: %s", got)
	}
}

func TestRemoteBuildToolsOmitsLocalizationForFeatureWork(t *testing.T) {
	hasTool := func(tools []map[string]interface{}) bool {
		for _, def := range tools {
			fn, _ := def["function"].(map[string]interface{})
			name, _ := fn["name"].(string)
			if name == reportLocalizationToolName {
				return true
			}
		}
		return false
	}
	feature := &remoteCodingCallbacks{task: "探索项目现状与 CLI 缺口"}
	if hasTool(feature.BuildTools("")) {
		t.Fatal("feature exploration must not list report_localization")
	}
	bug := &remoteCodingCallbacks{task: "fix parser crash"}
	if !hasTool(bug.BuildTools("")) {
		t.Fatal("bug-fix work must still list report_localization")
	}
	poisoned := &remoteCodingCallbacks{task: "[Plan step T1/2] 探索项目现状与 CLI 缺口\n\n" +
		"继续完成编程任务\n\nPrevious turn summary:\n" +
		"Prior plan step T1 status=failed.\nbug localization quality gate failed: missing localization evidence"}
	if hasTool(poisoned.BuildTools("")) {
		t.Fatal("failed-step carry summary must not put report_localization back on the surface")
	}
	if !strings.Contains(poisoned.executeRemoteReportLocalization(map[string]interface{}{"root_cause_file": "src/main.cpp"}), "report_localization skipped") {
		t.Fatal("failed-step carry summary must skip localization reports")
	}
}

func TestLocalizationInvalidEvidenceMessageStopsIncompleteRetry(t *testing.T) {
	err := fmt.Errorf("missing required fields: causal_path, reproduction, supporting_evidence, research_decision, research_reason, confidence")
	got := localizationInvalidEvidenceMessage(err)
	if !strings.Contains(got, "invalid localization evidence: missing required fields") {
		t.Fatalf("missing prefix: %q", got)
	}
	if !strings.Contains(got, "stop calling report_localization") {
		t.Fatalf("missing non-bug guidance: %q", got)
	}
	if !strings.Contains(got, `"confidence":0.8`) {
		t.Fatalf("missing example payload: %q", got)
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

func TestLocalizationWebFetchPaginationAcceptsFinishedPage(t *testing.T) {
	result := "标题: docs\nURL: https://vendor.example/docs\n已读取: 0-10 / 10 字符\ntruncated: false | has_more: false | next_offset: 10\n\nbody"
	offset, next, total, more, known := localizationWebFetchPagination("web_fetch", nil, result)
	if !known || offset != 0 || next != 10 || total != 10 || more {
		t.Fatalf("finished page = (%d,%d,%d,%t,%t)", offset, next, total, more, known)
	}
	last := "标题: docs\nURL: https://vendor.example/docs\n已读取: 100-300 / 300 字符\ntruncated: false | has_more: false | next_offset: 300\n\nbody"
	offset, next, total, more, known = localizationWebFetchPagination("web_fetch", nil, last)
	if !known || offset != 100 || next != 300 || total != 300 || more {
		t.Fatalf("last page = (%d,%d,%d,%t,%t)", offset, next, total, more, known)
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
	if cb.currentControlPlaneRevision() != 1 {
		t.Fatalf("identical tool surface must keep control-plane revision, got %d", cb.currentControlPlaneRevision())
	}
	if blocked := cb.requireLocalizationBeforeExistingBugEdit("root.go", false); blocked != "" {
		t.Fatalf("same-surface later turn must still authorize the reported file: %s", blocked)
	}
	if rev := cb.setStaticCompatibilitySurface([]map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "read_file"}},
	}); rev != 2 {
		t.Fatalf("name-set replacement revision=%d", rev)
	}
	if blocked := cb.requireLocalizationBeforeExistingBugEdit("root.go", false); !strings.Contains(blocked, "submit report_localization") || !strings.Contains(blocked, "older control-plane surface") {
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
	if cb.currentControlPlaneRevision() != 1 {
		t.Fatalf("identical remote tool surface must keep control-plane revision, got %d", cb.currentControlPlaneRevision())
	}
	if blocked := cb.requireRemoteLocalizationBeforeBugEdit(map[string]interface{}{"path": "root.go"}, true); blocked != "" {
		t.Fatalf("same-surface later remote turn must still authorize the reported file: %s", blocked)
	}
	if rev := cb.setStaticCompatibilitySurface([]map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "ssh_read_file"}},
	}); rev != 2 {
		t.Fatalf("remote name-set replacement revision=%d", rev)
	}
	if blocked := cb.requireRemoteLocalizationBeforeBugEdit(map[string]interface{}{"path": "root.go"}, true); !strings.Contains(blocked, "submit report_localization") || !strings.Contains(blocked, "older control-plane surface") {
		t.Fatalf("remote replacement must invalidate old localization evidence, got %q", blocked)
	}
}

func TestCorrelatedRemoteLocalizationSurvivesSameSurfaceRerender(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent:       &RemoteCodingSubAgent{correlatedRemoteExecution: true},
		task:        "fix parser bug",
		taskContext: "existing parser crashes",
	}
	_ = cb.BuildToolsForModelRequest("fix parser bug", 0)
	report := cb.executeRemoteReportLocalization(map[string]interface{}{
		"root_cause_file": "root.go", "causal_path": []interface{}{"request -> root"},
		"reproduction": "focused test fails", "supporting_evidence": []interface{}{"stack trace"},
		"research_decision": "not_needed", "research_reason": "repository-only", "confidence": .8,
	})
	if !strings.Contains(report, "control_plane_revision=1") {
		t.Fatalf("report=%q", report)
	}
	_ = cb.BuildToolsForModelRequest("fix parser bug again", 1)
	if cb.currentControlPlaneRevision() != 1 {
		t.Fatalf("correlated same-surface re-render must keep revision, got %d", cb.currentControlPlaneRevision())
	}
	if blocked := cb.requireRemoteLocalizationBeforeBugEdit(map[string]interface{}{"path": "root.go"}, true); blocked != "" {
		t.Fatalf("correlated later turn must still authorize the reported file: %s", blocked)
	}
}

func TestRemoteKnownExistingWriteUsesStaleEvidenceAfterSurfaceReplacement(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent:         &RemoteCodingSubAgent{},
		task:          "fix parser bug",
		taskContext:   "existing parser crashes",
		knownExisting: map[string]bool{"root.go": true},
	}
	_ = cb.BuildToolsForModelRequest("fix parser bug", 0)
	_ = cb.executeRemoteReportLocalization(map[string]interface{}{
		"root_cause_file": "root.go", "causal_path": []interface{}{"request -> root"},
		"reproduction": "focused test fails", "supporting_evidence": []interface{}{"stack trace"},
		"research_decision": "not_needed", "research_reason": "repository-only", "confidence": .8,
	})
	if rev := cb.setStaticCompatibilitySurface([]map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "ssh_read_file"}},
	}); rev != 2 {
		t.Fatalf("replacement revision=%d", rev)
	}
	blocked := cb.requireRemoteLocalizationBeforeBugEdit(map[string]interface{}{"path": "root.go"}, false)
	if strings.Contains(blocked, "determine whether the target exists") {
		t.Fatalf("known existing write must not be treated as existence-unknown after replacement: %q", blocked)
	}
	if !strings.Contains(blocked, "older control-plane surface") {
		t.Fatalf("known existing write after replacement must require a fresh report, got %q", blocked)
	}
}

func TestRemoteNewFileWriteSkipsLocalizationWhenExistenceIsKnownAbsent(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent:         &RemoteCodingSubAgent{},
		task:          "fix parser bug",
		taskContext:   "existing parser crashes",
		knownExisting: map[string]bool{"new_helper.go": false},
	}
	if blocked := cb.requireRemoteLocalizationBeforeBugEdit(map[string]interface{}{"path": "new_helper.go"}, false); blocked != "" {
		t.Fatalf("known-absent file must be exempt from the existing-file gate, got %q", blocked)
	}
}

func TestRemoteUnreadPathIsNotTreatedAsNewFile(t *testing.T) {
	cb := &remoteCodingCallbacks{
		agent:         &RemoteCodingSubAgent{},
		task:          "fix parser bug",
		taskContext:   "existing parser crashes",
		knownExisting: map[string]bool{"root.go": true},
	}
	blocked := cb.requireRemoteLocalizationBeforeBugEdit(map[string]interface{}{"path": "other.go"}, false)
	if !strings.Contains(blocked, "determine whether the target exists") {
		t.Fatalf("an unread path must not bypass the gate as a new file, got %q", blocked)
	}
}

func TestRemoteWriteWithoutExistenceTrackingStaysBlocked(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{}, task: "fix parser bug", taskContext: "existing parser crashes"}
	blocked := cb.requireRemoteLocalizationBeforeBugEdit(map[string]interface{}{"path": "root.go"}, false)
	if !strings.Contains(blocked, "determine whether the target exists") {
		t.Fatalf("write with unknown existence must ask for a read first, got %q", blocked)
	}
}

func TestQuarantineInvalidatesLocalizationEvidence(t *testing.T) {
	cb := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{projectPath: t.TempDir()},
		task:     &TaskItem{Title: "fix parser bug", Description: "existing parser crashes"},
	}
	_ = cb.BuildToolsForModelRequest("fix parser bug", 0)
	_ = cb.executeReportLocalization(map[string]interface{}{
		"root_cause_file": "root.go", "causal_path": []interface{}{"request -> root"},
		"reproduction": "focused test fails", "supporting_evidence": []interface{}{"stack trace"},
		"research_decision": "not_needed", "research_reason": "repository-only", "confidence": .8,
	})
	if blocked := cb.requireLocalizationBeforeExistingBugEdit("root.go", false); blocked != "" {
		t.Fatalf("pre-quarantine evidence should authorize: %s", blocked)
	}
	cb.quarantineStaticCompatibilitySurface()
	if cb.currentControlPlaneRevision() != 2 {
		t.Fatalf("quarantine must replace the name set and advance revision, got %d", cb.currentControlPlaneRevision())
	}
	if blocked := cb.requireLocalizationBeforeExistingBugEdit("root.go", false); !strings.Contains(blocked, "older control-plane surface") {
		t.Fatalf("quarantine must invalidate leftover localization evidence, got %q", blocked)
	}
}

func TestRemoteIncompleteLocalizationReportIsFailedOutcome(t *testing.T) {
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{}, task: "fix parser bug"}
	got := cb.executeRemoteReportLocalization(map[string]interface{}{
		"root_cause_file": "/home/testprj-2/src/main.cpp",
		"confidence":      0.92,
	})
	if remoteCodingToolOutcome(got) != "failed" {
		t.Fatalf("incomplete report must be a failed tool outcome, got %q from %q", remoteCodingToolOutcome(got), got)
	}
	if !strings.Contains(got, "missing required fields") {
		t.Fatalf("incomplete report should list missing fields, got %q", got)
	}
}

func TestLocalizationEvidenceFromArgsReportsMissingRequiredFieldsTogether(t *testing.T) {
	_, err := localizationEvidenceFromArgs(map[string]interface{}{
		"root_cause_file":   "/home/testprj-2/src/main.cpp",
		"root_cause_symbol": "main",
		"confidence":        0.92,
	}, CodingSubAgentBugSignal{})
	if err == nil {
		t.Fatal("incomplete report must be rejected")
	}
	got := err.Error()
	for _, field := range []string{"causal_path", "reproduction", "supporting_evidence", "research_decision", "research_reason"} {
		if !strings.Contains(got, field) {
			t.Fatalf("missing-field error should list %q, got %q", field, got)
		}
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
	// The audit now requires a genuinely relevant query (identity-token overlap
	// with the bug context; "sdk"/"error" are stoplisted) and, for
	// version-sensitive tasks, an audited web_fetch of the declared source with
	// read-range metadata and a resolved URL (see line 982 in the validator).
	cb.trackSearchResult("web_search", map[string]interface{}{"query": "exact SDK error"}, "https://vendor.example/docs", true)
	cb.searchesRun = append(cb.searchesRun, CodingSubAgentSearchResult{
		Tool: "web_fetch", Query: "https://vendor.example/docs", Succeeded: true,
		Summary:          "Vendor documentation body covering the exact SDK error and request handling.",
		FetchAuditKnown:  true,
		FetchRangeKnown:  true,
		FetchNextOffset:  66,
		FetchTotalChars:  66,
		FetchResolvedURL: "https://vendor.example/docs",
		seq:              2,
	})
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
