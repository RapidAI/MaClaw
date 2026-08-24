package longhorizon

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseManagerNextFailClosed(t *testing.T) {
	if got := ParseManagerNext("let us finish later"); got != NextInvalid {
		t.Fatalf("next = %q, want invalid", got)
	}
	plan := ParseManagerPlan("please keep going")
	if plan.Next != NextAsk {
		t.Fatalf("invalid next must become ask, got %q", plan.Next)
	}
	if plan.Question == "" {
		t.Fatal("ask question missing")
	}
}

func TestParseManagerNextCLI(t *testing.T) {
	if got := ParseManagerNext("Next: cli — write the tests"); got != NextCLI {
		t.Fatalf("next = %q, want cli", got)
	}
}

func TestCanCompleteRequiresRealCleanAudit(t *testing.T) {
	state := &TaskState{ManagerNext: NextDone}
	if CanComplete(state) {
		t.Fatal("done without audit must not complete")
	}
	state.Rounds = []ManagedRound{{
		Audit: &AuditReport{Status: "complete", Integrity: "clean", Alignment: "aligned", Synthetic: true},
	}}
	if CanComplete(state) {
		t.Fatal("synthetic audit must not complete")
	}
	state.Rounds[0].Audit.Synthetic = false
	if !CanComplete(state) {
		t.Fatal("real clean complete aligned should complete")
	}
	if MarkCompleted(&TaskState{ManagerNext: NextDone}) {
		t.Fatal("mark without audit must fail")
	}
}

func TestExperienceEligibleRejectsAttemptTerminal(t *testing.T) {
	audit := &AuditReport{Status: "complete", Integrity: "clean", Alignment: "aligned", Digest: "abc"}
	ok := ExperienceEligible(EligibilityInput{
		HorizonTaskID:   "t1",
		RoundIndex:      1,
		AuditDigest:     "abc",
		Audit:           audit,
		AttemptTerminal: true,
	})
	if ok {
		t.Fatal("attempt terminal must not be knowledge-eligible")
	}
	ok = ExperienceEligible(EligibilityInput{
		HorizonTaskID: "t1",
		RoundIndex:    1,
		AuditDigest:   "abc",
		Audit:         audit,
	})
	if !ok {
		t.Fatal("complete+clean+aligned persist should be eligible")
	}
	if ExperienceEligible(EligibilityInput{HorizonTaskID: "t1", RoundIndex: 1, AuditDigest: "abc", Audit: audit, Cancelled: true}) {
		t.Fatal("cancelled must not be eligible")
	}
	incomplete := &AuditReport{Status: "incomplete", Integrity: "suspect", Alignment: "drifted", Digest: "abc"}
	if ExperienceEligible(EligibilityInput{HorizonTaskID: "t1", RoundIndex: 1, AuditDigest: "abc", Audit: incomplete}) {
		t.Fatal("ordinary incomplete audit must not be eligible")
	}
	blocked := &AuditReport{Status: "blocked", Integrity: "violation", Alignment: "drifted", Digest: "abc"}
	if !ExperienceEligible(EligibilityInput{HorizonTaskID: "t1", RoundIndex: 1, AuditDigest: "abc", Audit: blocked}) {
		t.Fatal("blocked/violation pitfall should be eligible")
	}
	mechanical := &AuditReport{Status: "complete", Integrity: "clean", Alignment: "aligned", Digest: "abc", Mechanical: true}
	if ExperienceEligible(EligibilityInput{HorizonTaskID: "t1", RoundIndex: 1, AuditDigest: "abc", Audit: mechanical}) {
		t.Fatal("mechanical GUI pass must not burn experience slots")
	}
	state := &TaskState{ManagerNext: NextDone, Rounds: []ManagedRound{{Audit: mechanical}}}
	if !CanComplete(state) {
		t.Fatal("mechanical real audit must still be able to complete the outer task")
	}
}

func TestClampMaxRounds(t *testing.T) {
	if got := ClampMaxRounds(0); got != DefaultMaxRounds {
		t.Fatalf("zero = %d", got)
	}
	if got := ClampMaxRounds(MaxRounds + 8); got != MaxRounds {
		t.Fatalf("cap = %d", got)
	}
}

func TestAssembleEpisodeContextRejectsPollution(t *testing.T) {
	plan := ManagerPlan{Next: NextCLI, Goal: "add tests", Acceptance: "go test passes"}
	ep := AssembleEpisodeContext(RoleCLIExecutor, plan, nil, "", PolicySnapshot{HorizonTaskID: "t1", RoundIndex: 1})
	if !AssembleIsClean(ep) {
		t.Fatalf("clean assembler output marked dirty: %+v", ep)
	}
	if ContainsForbiddenPrompt(ep.SystemPrompt) {
		t.Fatal("executor prompt contains forbidden memory text")
	}
	if RejectForbiddenExecutorTools(ep.ToolSurface) != nil {
		t.Fatalf("forbidden tools in surface: %v", ep.ToolSurface)
	}
	dirty := ep
	dirty.Goal = "see ConversationMemory and coding_knowledge_search"
	if AssembleIsClean(dirty) {
		t.Fatal("polluted goal must fail cleanliness")
	}
}

func TestAssembleManagerContextDropsDirtyEvidence(t *testing.T) {
	plan := ManagerPlan{Goal: "add tests"}
	state := &TaskState{UserGoal: "add tests"}
	ep, ok := AssembleManagerContext(plan, state, "see coding_knowledge_search in prior notes", PolicySnapshot{HorizonTaskID: "t1"})
	if !ok || !AssembleIsClean(ep) {
		t.Fatalf("manager must drop dirty retrieved evidence, ok=%v evidence=%q", ok, ep.Evidence)
	}
	if strings.Contains(strings.ToLower(ep.Evidence), "coding_knowledge_search") {
		t.Fatal("dirty evidence leaked into manager context")
	}
	_, ok = AssembleManagerContext(plan, state, "", PolicySnapshot{})
	if !ok {
		t.Fatal("clean manager context without evidence must assemble")
	}
}

func TestAssembleGUIAndBrowserSurfaces(t *testing.T) {
	plan := ManagerPlan{Next: NextGUI, Goal: "open notepad", Acceptance: "window visible"}
	gui := AssembleEpisodeContext(RoleGUIExecutor, plan, nil, "", PolicySnapshot{HorizonTaskID: "t1"})
	if gui.SystemPrompt == CLIExecutorSystemPrompt() {
		t.Fatal("gui executor must not reuse CLI prompt")
	}
	if !AssembleIsClean(gui) {
		t.Fatalf("gui surface dirty: %v", gui.ToolSurface)
	}
	if SurfaceViolatesRole(RoleGUIExecutor, gui.ToolSurface) {
		t.Fatal("gui surface flagged forbidden")
	}
	if gui.Budget.MaxIterations != GUIMaxIterations {
		t.Fatalf("gui budget = %d", gui.Budget.MaxIterations)
	}
	browser := AssembleEpisodeContext(RoleBrowserExecutor, plan, nil, "", PolicySnapshot{HorizonTaskID: "t1"})
	if browser.SystemPrompt == CLIExecutorSystemPrompt() || browser.SystemPrompt == GUIExecutorSystemPrompt() {
		t.Fatal("browser executor must use its own prompt")
	}
	if !AssembleIsClean(browser) {
		t.Fatalf("browser surface dirty: %v", browser.ToolSurface)
	}
	auditor := AssembleEpisodeContext(RoleGUIAuditor, plan, nil, "observe digest", PolicySnapshot{})
	if len(auditor.ToolSurface) != 0 || !AssembleIsClean(auditor) {
		t.Fatalf("gui auditor must be tool-free clean: %+v", auditor)
	}
	probe := AssembleEpisodeContext(RoleGUIAuditor, plan, nil, "computer_observe: notepad visible", PolicySnapshot{})
	if !AssembleIsClean(probe) {
		t.Fatal("gui auditor evidence may mention computer_observe")
	}
	if !ToolForbiddenForRole(RoleGUIAuditor, "computer_focus") {
		t.Fatal("gui auditor must forbid computer_focus")
	}
	dirtyAuditor := auditor
	dirtyAuditor.ToolSurface = []string{"computer_focus"}
	if AssembleIsClean(dirtyAuditor) || !SurfaceViolatesRole(RoleGUIAuditor, dirtyAuditor.ToolSurface) {
		t.Fatal("gui auditor with computer_focus must be dirty")
	}
	paragraph := AssembleEpisodeContext(RoleGUIAuditor, ManagerPlan{
		Goal:       "open notepad",
		Acceptance: "The notepad window is in the foreground and hello.txt is saved.",
	}, nil, "observe digest", PolicySnapshot{})
	if paragraph.Acceptance != "The notepad window is in the foreground and hello.txt is saved." {
		t.Fatalf("auditor must keep paragraph acceptance, got %q", paragraph.Acceptance)
	}
	if paragraph.SystemPrompt != GUIAuditorSystemPrompt() || paragraph.SystemPrompt == CLIAuditorSystemPrompt() {
		t.Fatal("gui auditor must use LLM auditor prompt, not CLI")
	}
}

func TestProjectTaskState(t *testing.T) {
	proj := ProjectTaskState(&TaskState{
		TaskID:      "hz-1",
		Status:      StatusExecuting,
		RoundIndex:  3,
		MaxRounds:   25,
		ManagerNext: NextGUI,
		Completed:   false,
		Policy:      PolicySnapshot{OwnerID: "u1", EventScopeID: "tab-1"},
	})
	if proj.TaskID != "hz-1" || proj.SessionKey != "u1" || proj.EventScopeID != "tab-1" || proj.ManagerNext != NextGUI {
		t.Fatalf("projection = %+v", proj)
	}
}

func TestAssembleTruncatesCaps(t *testing.T) {
	longGoal := strings.Repeat("g", GoalCap+50)
	plan := ManagerPlan{Goal: longGoal, Acceptance: "ok"}
	ep := AssembleEpisodeContext(RoleCLIExecutor, plan, nil, "", PolicySnapshot{})
	if utf8.RuneCountInString(ep.Goal) > GoalCap {
		t.Fatalf("goal len %d exceeds cap", utf8.RuneCountInString(ep.Goal))
	}
}

func TestToolAllowedRejectsComputerUse(t *testing.T) {
	if ToolAllowed(CLIExecutorTools, "computer_observe") {
		t.Fatal("computer_observe must be rejected")
	}
	if !ToolAllowed(CLIExecutorTools, "bash") {
		t.Fatal("bash must be allowed")
	}
	if ToolAllowed(nil, "bash") {
		t.Fatal("empty surface must reject")
	}
}

func TestParseAuditWithoutProbeCannotBeClean(t *testing.T) {
	report := ParseAuditReport("Status: complete\nIntegrity: clean\nAlignment: aligned\nSummary: ok", ProbeResult{})
	if report.Integrity == "clean" {
		t.Fatal("no probe digest must not stay clean")
	}
	report = ParseAuditReport("Status: complete\nIntegrity: clean\nAlignment: aligned\nSummary: ok", ProbeResult{Digest: "tests passed", OK: true})
	if report.Integrity != "clean" || report.Status != "complete" {
		t.Fatalf("unexpected audit %+v", report)
	}
	if report.Summary != "ok" {
		t.Fatalf("labeled summary = %q", report.Summary)
	}
	report = ParseAuditReport("Status: complete\nIntegrity: clean\nAlignment: aligned\nSummary: logged in", ProbeResult{Digest: "url=https://example.com\nflags=captcha_or_login\n", OK: true})
	if report.Integrity == "clean" || report.Status == "complete" {
		t.Fatalf("captcha/login probe must not complete cleanly: %+v", report)
	}
}

func TestSaveTaskStateRequiresRoot(t *testing.T) {
	err := SaveTaskState("", &TaskState{TaskID: "t1"})
	if err == nil {
		t.Fatal("empty root must fail")
	}
}

func TestParseAdmitCommand(t *testing.T) {
	body, ok := ParseAdmitCommand("@horizon add a test")
	if !ok || body != "add a test" {
		t.Fatalf("admit = %q %v", body, ok)
	}
	if _, ok := ParseAdmitCommand("please add a test"); ok {
		t.Fatal("plain chat must not admit")
	}
	body, ok = ParseAdmitCommand("@Horizon add a test")
	if !ok || body != "add a test" {
		t.Fatalf("mixed-case admit = %q %v", body, ok)
	}
	if _, ok := ParseAdmitCommand("@horizontal layout"); ok {
		t.Fatal("@horizontal must not admit")
	}
	if _, ok := ParseAdmitCommand("@horizonally"); ok {
		t.Fatal("@horizonally must not admit")
	}
	body, ok = ParseAdmitCommand("@horizon\tadd a test")
	if !ok || body != "add a test" {
		t.Fatalf("tab-separated admit = %q %v", body, ok)
	}
	if _, ok := ParseAdmitCommand("@horizon"); !ok {
		t.Fatal("bare @horizon should admit with empty body")
	}
}

func TestClipCarryoverKeepsNewestUnderCaps(t *testing.T) {
	items := make([]string, 0, CarryoverMaxItems+5)
	for i := 0; i < CarryoverMaxItems+5; i++ {
		items = append(items, "item-"+strconv.Itoa(i))
	}
	got := ClipCarryover(items)
	if len(got) != CarryoverMaxItems {
		t.Fatalf("len=%d want %d", len(got), CarryoverMaxItems)
	}
	if got[0] != "item-5" || got[len(got)-1] != "item-"+strconv.Itoa(CarryoverMaxItems+4) {
		t.Fatalf("kept wrong window: %q .. %q", got[0], got[len(got)-1])
	}
	got = ClipCarryover([]string{
		strings.Repeat("a", CarryoverItemCap),
		strings.Repeat("b", CarryoverItemCap),
		strings.Repeat("c", CarryoverItemCap),
		strings.Repeat("d", CarryoverItemCap),
		"newest",
	})
	if len(got) != 4 || got[0] != strings.Repeat("b", CarryoverItemCap) || got[len(got)-1] != "newest" {
		t.Fatalf("rune budget should drop oldest, got len=%d last=%q", len(got), got[len(got)-1])
	}
}

func TestResumableSkipsExhaustedRounds(t *testing.T) {
	state := &TaskState{Status: StatusBlocked, RoundIndex: DefaultMaxRounds, MaxRounds: DefaultMaxRounds}
	if Resumable(state) {
		t.Fatal("round-limit blocked task must not be resumable")
	}
	state.RoundIndex = DefaultMaxRounds - 1
	if !Resumable(state) {
		t.Fatal("blocked task under the round cap should be resumable")
	}
	state.Status = StatusCancelled
	if Resumable(state) {
		t.Fatal("cancelled must not be resumable")
	}
}

func TestCloneTaskStateIsolatesCarryover(t *testing.T) {
	orig := &TaskState{
		TaskID:    "t1",
		Carryover: []string{"one"},
		Rounds:    []ManagedRound{{Goal: "g", Audit: &AuditReport{Summary: "s"}}},
	}
	cp := CloneTaskState(orig)
	orig.Carryover[0] = "mutated"
	orig.Rounds[0].Goal = "other"
	orig.Rounds[0].Audit.Summary = "changed"
	if cp.Carryover[0] != "one" || cp.Rounds[0].Goal != "g" || cp.Rounds[0].Audit.Summary != "s" {
		t.Fatalf("clone shared backing data: %+v", cp)
	}
}

func TestSanitizeExperienceTextDropsImages(t *testing.T) {
	if got := SanitizeExperienceText("data:image/png;base64,AAAA"); got != "" {
		t.Fatalf("image payload leaked: %q", got)
	}
	if got := SanitizeExperienceText("tests failed on foo_test.go"); got == "" {
		t.Fatal("plain summary should remain")
	}
}

func TestStripUntrustedMediaKeepsSurroundingText(t *testing.T) {
	got := StripUntrustedMedia("ocr=notepad data:image/png;base64," + strings.Repeat("A", 40) + " title=Notes")
	if !strings.Contains(got, "ocr=notepad") || !strings.Contains(got, "title=Notes") {
		t.Fatalf("surrounding text dropped: %q", got)
	}
	if strings.Contains(got, strings.Repeat("A", 40)) {
		t.Fatalf("base64 payload leaked: %q", got)
	}
}

func TestFindIncompleteTaskSkipsExhausted(t *testing.T) {
	root := t.TempDir()
	state := &TaskState{
		TaskID:     "hz-exhausted",
		Status:     StatusBlocked,
		RoundIndex: DefaultMaxRounds,
		MaxRounds:  DefaultMaxRounds,
		Policy:     PolicySnapshot{OwnerID: "u1", HorizonTaskID: "hz-exhausted"},
	}
	if err := SaveTaskState(root, state); err != nil {
		t.Fatal(err)
	}
	got, err := FindIncompleteTask(root, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("exhausted task should not resume, got %+v", got)
	}
}

func TestSaveTaskStatePersistsCarryoverAndExperienceWrites(t *testing.T) {
	root := t.TempDir()
	state := &TaskState{
		TaskID:           "hz-keep",
		Status:           StatusAsking,
		RoundIndex:       1,
		MaxRounds:        DefaultMaxRounds,
		Carryover:        []string{"also format"},
		ExperienceWrites: 1,
		Policy:           PolicySnapshot{OwnerID: "u1", HorizonTaskID: "hz-keep"},
	}
	if err := SaveTaskState(root, state); err != nil {
		t.Fatal(err)
	}
	got, err := FindIncompleteTask(root, "u1")
	if err != nil || got == nil {
		t.Fatalf("resumable asking task got=%+v err=%v", got, err)
	}
	if len(got.Carryover) != 1 || got.Carryover[0] != "also format" || got.ExperienceWrites != 1 {
		t.Fatalf("carryover/experience not restored: %+v", got)
	}
}
