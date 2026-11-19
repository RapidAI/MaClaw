package guiapp

import (
	"context"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/gorilla/websocket"
)

// acceptanceTestGate serializes tests that replace the package acceptance
// clock, driver, or stop hook. guiapp tests may run in parallel.
var acceptanceTestGate sync.Mutex

func lockAcceptanceHooks(t *testing.T) {
	t.Helper()
	acceptanceTestGate.Lock()
	prevNow := acceptanceNow
	prevWait := acceptanceWait
	prevDriver := acceptanceConsoleDriver
	prevDesktop := acceptanceDesktopDriver
	prevBrowser := acceptanceBrowserDriver
	prevListen := acceptanceListenFinder
	prevProof := acceptanceProveDisplay
	prevRemote := acceptanceRemoteDriver
	prevBinding := acceptanceDisplayBindingForTurn
	prevContainer := acceptanceInContainerExec
	prevForeground := acceptanceReadForeground
	prevClick := acceptanceDesktopClickHook
	prevCU := acceptanceComputerUseForTest
	prevReconnect := acceptanceSSHReconnect
	prevEnabled := acceptanceEnabledForTest
	prevStop := acceptanceStopCancel
	var mu sync.Mutex
	now := time.Unix(1_700_000_000, 0)
	acceptanceNow = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	acceptanceWait = func(d time.Duration) {
		mu.Lock()
		now = now.Add(d)
		mu.Unlock()
	}
	t.Cleanup(func() {
		acceptanceNow = prevNow
		acceptanceWait = prevWait
		acceptanceConsoleDriver = prevDriver
		acceptanceDesktopDriver = prevDesktop
		acceptanceBrowserDriver = prevBrowser
		acceptanceListenFinder = prevListen
		acceptanceProveDisplay = prevProof
		acceptanceRemoteDriver = prevRemote
		acceptanceDisplayBindingForTurn = prevBinding
		acceptanceInContainerExec = prevContainer
		acceptanceReadForeground = prevForeground
		acceptanceDesktopClickHook = prevClick
		acceptanceComputerUseForTest = prevCU
		acceptanceSSHReconnect = prevReconnect
		acceptanceEnabledForTest = prevEnabled
		acceptanceStopCancel = prevStop
		clearAcceptanceDriving()
		acceptanceTestGate.Unlock()
	})
}

func acceptanceFakePE(t *testing.T, subsystem uint16, lfanew int) []byte {
	t.Helper()
	if lfanew < 0x40 {
		t.Fatalf("lfanew %d overlaps the DOS header", lfanew)
	}
	need := lfanew + 4 + 20 + 70
	b := make([]byte, need)
	b[0], b[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(b[0x3C:], uint32(lfanew))
	copy(b[lfanew:], []byte{'P', 'E', 0, 0})
	coff := lfanew + 4
	binary.LittleEndian.PutUint16(b[coff+16:], 96)
	opt := coff + 20
	binary.LittleEndian.PutUint16(b[opt:], 0x20b)
	binary.LittleEndian.PutUint16(b[opt+68:], subsystem)
	return b
}

func acceptanceWritePE(t *testing.T, path string, subsystem uint16) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, acceptanceFakePE(t, subsystem, 0x40), 0o644); err != nil {
		t.Fatal(err)
	}
}

func acceptancePiece(row int, cols ...int) consoleGrid {
	lines := make([]string, row+1)
	buf := []rune(strings.Repeat(" ", 8))
	for _, c := range cols {
		if c >= 0 && c < len(buf) {
			buf[c] = 'X'
		}
	}
	lines[row] = string(buf)
	return gridFromRows(lines)
}

const acceptanceTetrisClosing = "T5 完成，项目逻辑层与端到端边界全部验证通过；交互式终端体验需人工运行 tetris.exe（本环境无法注入按键）。"

type scriptedConsole struct {
	startCount        int
	writes            []string
	reads             []consoleGrid
	readN             int
	exitWithin        bool
	startErr          error
	firstProbeReason  string
	firstProbeWin32   int
	alwaysProbeReason string
	alwaysProbeWin32  int
}

func (s *scriptedConsole) Start(ctx context.Context, exe, dir string) (int, func(time.Duration) bool, func(), error) {
	if err := ctx.Err(); err != nil {
		return 0, nil, nil, err
	}
	s.startCount++
	if s.startErr != nil {
		return 0, nil, nil, s.startErr
	}
	exited := s.exitWithin
	return 4242, func(time.Duration) bool { return exited }, func() {}, nil
}

func (s *scriptedConsole) Probe(ctx context.Context, pid int) (consoleHelperResult, error) {
	reason := s.alwaysProbeReason
	win := s.alwaysProbeWin32
	if reason == "" && s.startCount == 1 {
		reason = s.firstProbeReason
		win = s.firstProbeWin32
	}
	if reason != "" {
		return consoleHelperResult{Reason: reason, Win32: win}, errors.New(reason)
	}
	return consoleHelperResult{OK: true, ConsoleHWND: 1}, nil
}

func (s *scriptedConsole) Read(ctx context.Context, pid int) (consoleGrid, error) {
	if len(s.reads) == 0 {
		return gridFromRows(nil), nil
	}
	if s.readN >= len(s.reads) {
		return s.reads[len(s.reads)-1], nil
	}
	g := s.reads[s.readN]
	s.readN++
	return g, nil
}

func (s *scriptedConsole) Write(ctx context.Context, pid int, key string) error {
	s.writes = append(s.writes, key)
	return nil
}

func TestAcceptanceDirectionClock(t *testing.T) {
	b2 := acceptancePiece(0, 1, 2)
	b3 := acceptancePiece(0, 1, 2)
	left := acceptancePiece(0, 0, 1)
	ok, detail := evaluateAcceptanceCheck("left", b2, b3, left)
	if !ok || strings.Contains(detail, "时钟已有") {
		t.Fatalf("left move against an empty clock should pass: %v %s", ok, detail)
	}

	same := acceptancePiece(0, 1, 2)
	ok, detail = evaluateAcceptanceCheck("right", b2, b3, same)
	if ok || !strings.Contains(detail, "input_ignored") {
		t.Fatalf("a key that returns to the original cells fails: %v %s", ok, detail)
	}

	gravityBefore := acceptancePiece(0, 1, 2)
	gravityAfter := acceptancePiece(1, 1, 2)
	ok, detail = evaluateAcceptanceCheck("down", gravityBefore, gravityAfter, gravityAfter)
	if ok || strings.Contains(detail, "时钟") {
		t.Fatalf("gravity then a still frame is input_ignored, not a clock excuse: %v %s", ok, detail)
	}

	if rowJump(gridFromRows([]string{"   "}), acceptancePiece(3, 1, 2)) != 0 {
		t.Fatal("an empty side has row jump 0")
	}
	ok, _ = evaluateAcceptanceCheck("hard_drop", gravityBefore, gravityAfter, gravityAfter)
	if ok {
		t.Fatal("hard_drop must not pass on gravity alone")
	}
	dropped := acceptancePiece(2, 1, 2)
	ok, detail = evaluateAcceptanceCheck("hard_drop", b2, b3, dropped)
	if !ok {
		t.Fatalf("hard_drop +2 against a still clock should pass: %s", detail)
	}
	oneMore := acceptancePiece(2, 1, 2)
	ok, _ = evaluateAcceptanceCheck("hard_drop", gravityBefore, gravityAfter, oneMore)
	if ok {
		t.Fatal("a key that only matches the clock step fails hard_drop")
	}

	rotated := gridFromRows([]string{"X ", "X "})
	ok, detail = evaluateAcceptanceCheck("rotate", b2, b3, rotated)
	if !ok {
		t.Fatalf("shape change with an empty clock should pass: %s", detail)
	}
	translated := acceptancePiece(0, 3, 4)
	ok, detail = evaluateAcceptanceCheck("rotate", b2, b3, translated)
	if ok || !strings.Contains(detail, "keyed_translation") {
		t.Fatalf("a pure translation is not a rotate: %v %s", ok, detail)
	}
	ambiguous := gridFromRows([]string{"X"})
	ok, detail = evaluateAcceptanceCheck("rotate", b2, ambiguous, rotated)
	if ok || detail != "clock_ambiguous" {
		t.Fatalf("a clock that is neither empty nor a translation fails closed: %v %s", ok, detail)
	}
}

func TestAcceptanceNormalizeExpect(t *testing.T) {
	checks, _ := normalizeAcceptanceChecks([]codingAcceptanceCheck{
		{Keys: []string{"left"}, Expect: "buffer_changed"},
		{Keys: []string{"right"}, Expect: "buffer_changed"},
		{Keys: []string{"down"}, Expect: "buffer_changed"},
		{Keys: []string{"up"}, Expect: "buffer_changed"},
		{Keys: []string{"left", "right"}, Expect: "left"},
		{Keys: []string{"space"}, Expect: "static_change"},
	})
	want := []string{"left", "right", "down", "expect_rejected", "expect_rejected", "expect_rejected"}
	if len(checks) != len(want) {
		t.Fatalf("checks=%d", len(checks))
	}
	for i, expect := range want {
		if checks[i].Expect != expect {
			t.Fatalf("check %d expect %q want %q", i, checks[i].Expect, expect)
		}
	}
	if checks[4].Keys != nil {
		t.Fatalf("a chord must not be written: %#v", checks[4].Keys)
	}
	if acceptanceTextHasSecret("see the footnote") {
		t.Fatal("footnote is not a secret word")
	}
	if !acceptanceTextHasSecret("otp") || !acceptanceTextHasSecret("密码") {
		t.Fatal("password-like words must drop the script")
	}
	brief, text := parseAcceptanceBrief(`{"launch":"tetris.exe","checks":[{"keys":["left"],"expect":"the password"}]}`)
	if text != "accepted: default keys" || brief.Launch != "" || !brief.SecretLike {
		t.Fatalf("raw secret dropped launch: %+v %s", brief, text)
	}
	brief, text = parseAcceptanceBrief("")
	if text != "accepted" || len(brief.Checks) != 0 {
		t.Fatalf("empty brief: %+v %s", brief, text)
	}
	_, text = parseAcceptanceBrief("{")
	if text != "ignored: invalid json" {
		t.Fatalf("invalid json: %s", text)
	}
	x, y := 3, 4
	half, _ := normalizeAcceptanceChecks([]codingAcceptanceCheck{{ID: "half", ClickX: &x, Expect: "click"}})
	if len(half) != 1 || half[0].Expect != "expect_rejected" || half[0].ClickX != nil || half[0].ClickY != nil {
		t.Fatalf("half click must not stay clickable: %+v", half)
	}
	mixed, _ := normalizeAcceptanceChecks([]codingAcceptanceCheck{{ClickX: &x, ClickY: &y, Keys: []string{"tab"}, Expect: "click"}})
	if len(mixed) != 1 || mixed[0].Expect != "expect_rejected" || mixed[0].ClickX != nil || mixed[0].Keys != nil {
		t.Fatalf("click mixed with a key must not stay clickable: %+v", mixed)
	}
	space, _ := normalizeAcceptanceChecks([]codingAcceptanceCheck{{Keys: []string{"space"}, Expect: "static_change"}})
	if len(space) != 1 || space[0].Expect != "expect_rejected" || len(space[0].Keys) != 1 || space[0].Keys[0] != "space" {
		t.Fatalf("disallowed expect must not be rewritten into a send: %+v", space)
	}
}

func TestAcceptanceExecutableNames(t *testing.T) {
	dir := t.TempDir()
	keep := []string{
		filepath.Join(dir, "tetris.EXE"),
		filepath.Join(dir, "Tests", "tetris.EXE"),
		filepath.Join(dir, "mytest.exe"),
	}
	drop := []string{
		filepath.Join(dir, "Test_e2e.exe"),
		filepath.Join(dir, "test_board.exe"),
		filepath.Join(dir, "test_game.exe"),
		filepath.Join(dir, "test_e2e.exe"),
		filepath.Join(dir, "foo_test.exe"),
		filepath.Join(dir, "foo.test.exe"),
	}
	var files []string
	for _, path := range append(append([]string{}, keep...), drop...) {
		acceptanceWritePE(t, path, acceptanceImageSubsystemCUI)
		files = append(files, path)
		if isTestExecutableBase(path) == containsAcceptance(keep, path) {
			t.Fatalf("basename rule mismatch for %s keep=%v", path, containsAcceptance(keep, path))
		}
	}
	cands := findAcceptanceCandidates(dir, files, nil, time.Time{})
	if len(cands) != len(keep) {
		t.Fatalf("candidates=%d want %d: %+v", len(cands), len(keep), cands)
	}

	old := filepath.Join(dir, "ancient.exe")
	acceptanceWritePE(t, old, acceptanceImageSubsystemCUI)
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	scanned := findAcceptanceCandidates(dir, nil, []CodingSubAgentCommandResult{{
		Command:    "cmake --build .",
		WorkingDir: dir,
	}}, time.Time{})
	if len(scanned) != 0 {
		t.Fatalf("a zero turn start must not scan old executables: %+v", scanned)
	}
	quoted := filepath.Join(dir, "build", "game.exe")
	acceptanceWritePE(t, quoted, acceptanceImageSubsystemCUI)
	if err := os.Chtimes(quoted, past, past); err != nil {
		t.Fatal(err)
	}
	fromCommand := findAcceptanceCandidates(dir, nil, []CodingSubAgentCommandResult{{
		Command:    `g++ -o tetris.exe main.cpp && cl /Fe:ancient.exe src.cpp && "` + quoted + `"`,
		WorkingDir: dir,
	}}, time.Now())
	got := map[string]bool{}
	for _, c := range fromCommand {
		got[acceptancePathKey(c.Path)] = true
	}
	for _, path := range []string{filepath.Join(dir, "tetris.EXE"), old, quoted} {
		if !got[acceptancePathKey(path)] {
			t.Fatalf("command token %s missing from %+v", path, fromCommand)
		}
	}
}

func containsAcceptance(list []string, path string) bool {
	key := acceptancePathKey(path)
	for _, item := range list {
		if acceptancePathKey(item) == key {
			return true
		}
	}
	return false
}

func TestAcceptancePESubsystem(t *testing.T) {
	dir := t.TempDir()
	cui := filepath.Join(dir, "cui.exe")
	gui := filepath.Join(dir, "gui.exe")
	far := filepath.Join(dir, "far.exe")
	if err := os.WriteFile(cui, acceptanceFakePE(t, acceptanceImageSubsystemCUI, 0x40), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gui, acceptanceFakePE(t, acceptanceImageSubsystemGUI, 0x40), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(far, acceptanceFakePE(t, acceptanceImageSubsystemCUI, 8192), 0o644); err != nil {
		t.Fatal(err)
	}
	if sub, ok := readPESubsystem(cui); !ok || sub != acceptanceImageSubsystemCUI {
		t.Fatalf("cui subsystem %d ok=%v", sub, ok)
	}
	if sub, ok := readPESubsystem(gui); !ok || sub != acceptanceImageSubsystemGUI {
		t.Fatalf("gui subsystem %d ok=%v", sub, ok)
	}
	if sub, ok := readPESubsystem(far); !ok || sub != acceptanceImageSubsystemCUI {
		t.Fatalf("header past 4096 bytes: subsystem %d ok=%v", sub, ok)
	}
	short := filepath.Join(dir, "short.exe")
	if err := os.WriteFile(short, []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := readPESubsystem(short); ok {
		t.Fatal("a short image is not a PE")
	}
	if _, ok := peSubsystem([]byte("not a pe")); ok {
		t.Fatal("bad magic accepted")
	}
}

func TestAcceptanceSummarySplice(t *testing.T) {
	failed := CodingInteractiveAcceptance{
		Kind:   acceptanceKindConsole,
		Status: acceptanceStatusFailed,
		Reason: "已用 WriteConsoleInput 向 PID 9 的控制台输入缓冲写入按键，画面没有按脚本方向变化；没有把键发给前台窗口。",
		Checks: []CodingAcceptanceCheckResult{{ID: "left", Key: "left", Expect: "left", Detail: "input_ignored"}},
	}
	out := spliceAcceptanceSummary("all board tests passed. "+acceptanceTetrisClosing, failed)
	if strings.Contains(out, "全部验证通过") || strings.Contains(out, "本环境无法注入按键") || strings.Contains(out, "无法注入按键") {
		t.Fatalf("failure left a pass claim or the excuse:\n%s", out)
	}
	if !strings.Contains(out, "all board tests passed") || !strings.Contains(out, "逻辑测试已通过，交互验证未通过") || !strings.Contains(out, "交互验证未通过。") {
		t.Fatalf("failure splice:\n%s", out)
	}
	passed := spliceAcceptanceSummary(acceptanceTetrisClosing, CodingInteractiveAcceptance{Status: acceptanceStatusPassed, Checks: []CodingAcceptanceCheckResult{{ID: "left", Key: "left", Expect: "left", Passed: true, Detail: "列重心差 -2.0，时钟未先发生"}}})
	if !strings.Contains(passed, "端到端边界全部验证通过") || !strings.Contains(passed, "交互验证已通过。") || strings.Contains(passed, "无法注入按键") {
		t.Fatalf("pass splice:\n%s", passed)
	}
	blocked := spliceAcceptanceSummary("需人工运行 tetris.exe（本环境无法注入按键）。", CodingInteractiveAcceptance{Status: acceptanceStatusPlatformBlocked, Reason: acceptanceReasonNoPlatform})
	if strings.Contains(blocked, "无法注入按键") || !strings.Contains(blocked, "需人工运行") || !strings.Contains(blocked, acceptanceReasonNoPlatform) {
		t.Fatalf("platform block:\n%s", blocked)
	}
}

func TestAcceptanceRemoteSplice(t *testing.T) {
	plain := "逻辑测试已通过。"
	if spliceRemoteInteractiveAcceptance(plain) != plain {
		t.Fatal("a remote summary without the excuse stays unchanged")
	}
	out := spliceRemoteInteractiveAcceptance(acceptanceTetrisClosing)
	if strings.Contains(out, "无法注入按键") || strings.Contains(out, "需人工运行") {
		t.Fatalf("remote splice left the excuse:\n%s", out)
	}
	if !strings.Contains(out, "远端会话没有本机可驱动的显示") {
		t.Fatalf("remote splice:\n%s", out)
	}
}

func TestAcceptanceOwnerWindowHeld(t *testing.T) {
	if (*IMMessageHandler)(nil).ownerWindowHeld("user") {
		t.Fatal("nil handler")
	}
	h := &IMMessageHandler{}
	if h.ownerWindowHeld("") || h.ownerWindowHeld("  ") {
		t.Fatal("empty user")
	}
	if h.ownerWindowHeld("missing") {
		t.Fatal("missing record")
	}
	if _, ok := h.sessionLoops.Load("missing"); ok {
		t.Fatal("ownerWindowHeld created a session record")
	}
	state := &sessionLoopState{loopCtx: &LoopContext{}}
	h.sessionLoops.Store("user", state)
	if h.ownerWindowHeld("user") {
		t.Fatal("a live loop without the window marker is not held")
	}
	state.windowHeld = true
	if !h.ownerWindowHeld("user") {
		t.Fatal("windowHeld is the signal")
	}
}

func TestAcceptanceComputerUseStop(t *testing.T) {
	lockAcceptanceHooks(t)
	called := 0
	acceptanceStopCancel = func(*App) { called++ }
	app := &App{}
	err := app.ComputerUseStop()
	if err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("idle stop: %v", err)
	}
	if called != 0 {
		t.Fatal("stop without a drive must not cancel the assistant")
	}
	setAcceptanceDriving(`E:\snake3\tetris.exe`)
	if err := app.ComputerUseStop(); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("acceptance stop calls=%d", called)
	}
	if driving, target := acceptanceDrivingSnapshot(); !driving || !strings.Contains(target, "tetris.exe") {
		t.Fatalf("Stop cancels the turn and leaves the drive flag for the runner: driving=%v target=%q", driving, target)
	}
}

func TestAcceptancePhaseAndBriefTool(t *testing.T) {
	if normalizeCodingAgentEventPhaseKind("verifying_ui") != codingAgentEventPhaseVerifyingUI {
		t.Fatal("verifying_ui did not round-trip")
	}
	event := codingAcceptanceProgressEvent(&TaskItem{Index: 5, Title: "tetris"}, `E:\snake3\tetris.exe`)
	if event.Event != codingAgentEventKindAssistantNote.String() || event.Phase != codingAgentEventPhaseVerifyingUI.String() {
		t.Fatalf("progress event %+v", event)
	}
	if !strings.Contains(event.Detail, `tetris.exe`) {
		t.Fatalf("detail %q", event.Detail)
	}
	if !isCodingOperationalTool(codingAcceptanceBriefToolName) || isCodingInquiryTool(codingAcceptanceBriefToolName) || isGoalPlanPhaseTool(codingAcceptanceBriefToolName) {
		t.Fatal("the brief tool is operational only")
	}
	cb := &codingSubAgentCallbacks{}
	result := cb.executeSubmitInteractiveAcceptance("")
	if result.Text != "accepted" || result.Outcome != codingToolOutcomeSuccess || cb.acceptanceBrief == nil {
		t.Fatalf("empty brief result %+v stored=%v", result, cb.acceptanceBrief != nil)
	}
	result = cb.executeSubmitInteractiveAcceptance("{")
	if result.Text != "ignored: invalid json" || cb.acceptanceBrief == nil {
		t.Fatalf("invalid json must not replace a stored brief: %+v", result)
	}
}

func TestAcceptanceChooseTarget(t *testing.T) {
	dir := t.TempDir()
	cui := filepath.Join(dir, "tetris.exe")
	gui := filepath.Join(dir, "editor.exe")
	other := filepath.Join(dir, "other.exe")
	acceptanceWritePE(t, cui, acceptanceImageSubsystemCUI)
	acceptanceWritePE(t, gui, acceptanceImageSubsystemGUI)
	acceptanceWritePE(t, other, acceptanceImageSubsystemCUI)
	info, err := os.Stat(cui)
	if err != nil {
		t.Fatal(err)
	}
	cuiCand := acceptanceCandidate{Path: cui, Subsystem: acceptanceImageSubsystemCUI, artifact: acceptanceArtifactKey(cui, info)}
	guiCand := acceptanceCandidate{Path: gui, Subsystem: acceptanceImageSubsystemGUI}
	choice := chooseAcceptanceTarget(dir, []acceptanceCandidate{guiCand}, nil)
	if !choice.drive || choice.kind != acceptanceKindDesktop || choice.launch != gui {
		t.Fatalf("gui only: %+v", choice)
	}
	choice = chooseAcceptanceTarget(dir, []acceptanceCandidate{cuiCand, guiCand}, nil)
	if !choice.drive || choice.kind != acceptanceKindDesktop || choice.launch != gui {
		t.Fatalf("gui wins over cui: %+v", choice)
	}
	choice = chooseAcceptanceTarget(dir, []acceptanceCandidate{
		cuiCand,
		{Path: other, Subsystem: acceptanceImageSubsystemCUI},
	}, nil)
	if choice.drive || choice.reason != acceptanceReasonMultiple {
		t.Fatalf("multiple: %+v", choice)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	choice = chooseAcceptanceTarget(dir, []acceptanceCandidate{{Path: self, Subsystem: acceptanceImageSubsystemCUI}}, nil)
	if choice.drive || choice.kind != acceptanceKindRefused || choice.reason != acceptanceReasonMaClaw {
		t.Fatalf("maclaw: %+v", choice)
	}
}

func TestAcceptanceConsoleDrive(t *testing.T) {
	lockAcceptanceHooks(t)
	dir := t.TempDir()
	exe := filepath.Join(dir, "tetris.exe")
	acceptanceWritePE(t, exe, acceptanceImageSubsystemCUI)
	userID := "acceptance-drive"
	agent := &CodingSubAgent{
		handler:             &IMMessageHandler{},
		projectPath:         dir,
		turnCtx:             context.Background(),
		acceptanceTurnStart: time.Now(),
		permissionOwnerID:   userID,
	}
	agent.handler.sessionLoops.Store(userID, &sessionLoopState{windowHeld: true})
	t.Cleanup(func() { agent.handler.clearCodingExecCheckpoint(userID) })
	sess := &scriptedConsole{reads: []consoleGrid{
		acceptancePiece(0, 1, 2),
		acceptancePiece(0, 1, 2),
		acceptancePiece(0, 0, 1),
		acceptancePiece(0, 2, 3),
		acceptancePiece(1, 1, 2),
	}}
	acceptanceConsoleDriver = sess
	var notes []string
	agent.onProgress = func(text string) { notes = append(notes, text) }
	cb := &codingSubAgentCallbacks{commandsRun: []CodingSubAgentCommandResult{{
		Command:    "g++ -std=c++17 -Wall -Wextra -o tetris.exe main.cpp",
		WorkingDir: dir,
		Succeeded:  true,
	}}}
	summary, acc := agent.applyCodingInteractiveAcceptance(nil, acceptanceTetrisClosing, cb, userID, false, false)
	if acc.Status != acceptanceStatusPassed || acc.Kind != acceptanceKindConsole || acc.Attempts != 1 || sess.startCount != 1 {
		t.Fatalf("drive acc=%+v starts=%d", acc, sess.startCount)
	}
	if strings.Join(sess.writes, ",") != "left,right,down" {
		t.Fatalf("keys %v", sess.writes)
	}
	if strings.Contains(summary, "无法注入按键") || !strings.Contains(summary, "交互验证已通过。") || !strings.Contains(summary, "端到端边界全部验证通过") {
		t.Fatalf("summary:\n%s", summary)
	}
	if _, err := os.Stat(filepath.Join(dir, "acceptance", "buffer.txt")); err != nil {
		t.Fatal(err)
	}
	if len(notes) == 0 || !strings.Contains(notes[0], "verifying_ui") || !strings.Contains(notes[0], "tetris.exe") {
		t.Fatalf("progress %v", notes)
	}
	summary, acc = agent.applyCodingInteractiveAcceptance(nil, acceptanceTetrisClosing, cb, userID, false, false)
	if sess.startCount != 1 || acc.Status != acceptanceStatusSkipped || !strings.Contains(acc.Reason, "沿用") || !strings.Contains(acc.Reason, "buffer.txt") {
		t.Fatalf("second drive starts=%d acc=%+v\n%s", sess.startCount, acc, summary)
	}
}

func TestAcceptanceFailedDoesNotSkip(t *testing.T) {
	lockAcceptanceHooks(t)
	dir := t.TempDir()
	acceptanceWritePE(t, filepath.Join(dir, "tetris.exe"), acceptanceImageSubsystemCUI)
	userID := "acceptance-fail"
	agent := &CodingSubAgent{
		handler:             &IMMessageHandler{},
		projectPath:         dir,
		turnCtx:             context.Background(),
		acceptanceTurnStart: time.Now(),
		permissionOwnerID:   userID,
	}
	agent.handler.sessionLoops.Store(userID, &sessionLoopState{windowHeld: true})
	t.Cleanup(func() { agent.handler.clearCodingExecCheckpoint(userID) })
	still := acceptancePiece(0, 1, 2)
	sess := &scriptedConsole{reads: []consoleGrid{still, still, still}}
	acceptanceConsoleDriver = sess
	cb := &codingSubAgentCallbacks{commandsRun: []CodingSubAgentCommandResult{{
		Command: "g++ -o tetris.exe main.cpp", WorkingDir: dir,
	}}}
	_, acc := agent.applyCodingInteractiveAcceptance(nil, acceptanceTetrisClosing, cb, userID, false, false)
	if acc.Status != acceptanceStatusFailed || sess.startCount != 1 || !strings.Contains(acc.Reason, "PID 4242") {
		t.Fatalf("first fail %+v starts=%d", acc, sess.startCount)
	}
	_, acc = agent.applyCodingInteractiveAcceptance(nil, acceptanceTetrisClosing, cb, userID, false, false)
	if sess.startCount != 2 || acc.Status != acceptanceStatusFailed {
		t.Fatalf("failed acceptance must run again: starts=%d %+v", sess.startCount, acc)
	}
}

func TestAcceptanceDriveGates(t *testing.T) {
	lockAcceptanceHooks(t)
	dir := t.TempDir()
	exe := filepath.Join(dir, "tetris.exe")
	acceptanceWritePE(t, exe, acceptanceImageSubsystemCUI)
	gui := filepath.Join(dir, "editor.exe")
	acceptanceWritePE(t, gui, acceptanceImageSubsystemGUI)
	sess := &scriptedConsole{}
	acceptanceConsoleDriver = sess
	cb := &codingSubAgentCallbacks{commandsRun: []CodingSubAgentCommandResult{{
		Command: "g++ -o tetris.exe main.cpp", WorkingDir: dir,
	}}}
	userID := "acceptance-gates"
	base := func() *CodingSubAgent {
		h := &IMMessageHandler{}
		h.sessionLoops.Store(userID, &sessionLoopState{windowHeld: true})
		return &CodingSubAgent{
			handler:             h,
			projectPath:         dir,
			turnCtx:             context.Background(),
			acceptanceTurnStart: time.Now(),
			permissionOwnerID:   userID,
		}
	}
	noWindow := base()
	noWindow.handler.sessionLoops.Store(userID, &sessionLoopState{loopCtx: &LoopContext{}})
	summary, acc := noWindow.applyCodingInteractiveAcceptance(nil, acceptanceTetrisClosing, cb, userID, false, false)
	if acc.Reason != acceptanceReasonNoWindow || sess.startCount != 0 || strings.Contains(summary, "无法注入按键") {
		t.Fatalf("window gate %+v\n%s", acc, summary)
	}
	noCtx := base()
	noCtx.turnCtx = nil
	_, acc = noCtx.applyCodingInteractiveAcceptance(nil, "ok", cb, userID, false, false)
	if acc.Reason != acceptanceReasonNoTurnCtx || sess.startCount != 0 {
		t.Fatalf("context gate %+v", acc)
	}
	off := false
	acceptanceEnabledForTest = &off
	_, acc = base().applyCodingInteractiveAcceptance(nil, "ok", cb, userID, false, false)
	if acc.Reason != acceptanceReasonDisabled || sess.startCount != 0 {
		t.Fatalf("flag gate %+v", acc)
	}
	acceptanceEnabledForTest = nil
	horizon := base()
	horizon.horizonPosture = true
	summary, acc = horizon.applyCodingInteractiveAcceptance(nil, acceptanceTetrisClosing, cb, userID, false, false)
	if strings.Contains(summary, acceptanceReasonNoWindow) || strings.Contains(summary, "无法注入按键") || acc.Status != acceptanceStatusSkipped {
		t.Fatalf("horizon %+v\n%s", acc, summary)
	}
	plain := "逻辑测试已通过。"
	summary, acc = base().applyCodingInteractiveAcceptance(nil, plain, nil, userID, false, false)
	if summary != plain || acc.Kind != "" || acc.Status != "" || acc.Reason != "" {
		t.Fatalf("no target and no excuse changed the summary: %+v\n%s", acc, summary)
	}
	summary, _ = base().applyCodingInteractiveAcceptance(nil, acceptanceTetrisClosing, nil, userID, false, false)
	if !strings.Contains(summary, acceptanceReasonNoCandidate) || strings.Contains(summary, "无法注入按键") {
		t.Fatalf("no candidate:\n%s", summary)
	}
	onlyGUI := &codingSubAgentCallbacks{filesCreated: map[string]bool{gui: true}}
	summary, acc = base().applyCodingInteractiveAcceptance(nil, "ok", onlyGUI, userID, false, false)
	if acc.Status != acceptanceStatusPlatformBlocked || acc.Reason != acceptanceReasonTestNoDesktop || sess.startCount != 0 || !strings.Contains(summary, acceptanceReasonTestNoDesktop) {
		t.Fatalf("gui without a desktop driver %+v\n%s", acc, summary)
	}
}

func TestAcceptanceAttachRetryAndExit(t *testing.T) {
	lockAcceptanceHooks(t)
	ctx := context.Background()
	retry := &scriptedConsole{firstProbeReason: "attach_console_failed", firstProbeWin32: 6}
	acc, _ := runConsoleAcceptance(ctx, retry, "tetris.exe", t.TempDir(), nil)
	if retry.startCount != 2 || acc.Attempts != 2 || acc.Status != acceptanceStatusPassed {
		t.Fatalf("healed attach %+v starts=%d", acc, retry.startCount)
	}
	blocked := &scriptedConsole{alwaysProbeReason: "attached_host_console", alwaysProbeWin32: 5}
	acc, _ = runConsoleAcceptance(ctx, blocked, "tetris.exe", t.TempDir(), nil)
	if blocked.startCount != 2 || acc.Status != acceptanceStatusPlatformBlocked || !strings.Contains(acc.Reason, "没有向其他窗口发送按键") || !strings.Contains(acc.Reason, "错误 5") {
		t.Fatalf("host console %+v starts=%d", acc, blocked.startCount)
	}
	early := &scriptedConsole{exitWithin: true}
	acc, _ = runConsoleAcceptance(ctx, early, "tetris.exe", t.TempDir(), defaultAcceptanceChecks())
	if early.startCount != 1 || len(early.writes) != 0 || acc.Kind != acceptanceKindNone || acc.Status != acceptanceStatusSkipped || !strings.Contains(acc.Reason, "1.5") {
		t.Fatalf("cli exit %+v starts=%d writes=%v", acc, early.startCount, early.writes)
	}
	failedStart := &scriptedConsole{startErr: errors.New("boom")}
	acc, _ = runConsoleAcceptance(ctx, failedStart, "tetris.exe", t.TempDir(), nil)
	if acc.Status != acceptanceStatusPlatformBlocked || !strings.Contains(acc.Reason, "CreateProcess 失败") || !strings.Contains(acc.Reason, "没有向其他窗口发送按键") {
		t.Fatalf("create %+v", acc)
	}
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := &scriptedConsole{}
	acc, _ = runConsoleAcceptance(cancelCtx, cancelled, "tetris.exe", t.TempDir(), defaultAcceptanceChecks())
	if cancelled.startCount != 0 || acc.Status != acceptanceStatusCancelled || acc.Reason != acceptanceReasonCancelled {
		t.Fatalf("cancel %+v starts=%d", acc, cancelled.startCount)
	}
}

type scriptedDesktop struct {
	pid          int
	image        string
	title        string
	class        string
	labels       []string
	focused      string
	focusKind    string
	covers       bool
	revealPay    bool
	hasBounds    bool
	passwordEdit bool
	left         int
	top          int
	right        int
	bottom       int
	startErr     error
	startCount   int
	clicks       []string
	keys         []string
}

func (s *scriptedDesktop) Start(context.Context, string, string) (int, func(), error) {
	s.startCount++
	if s.startErr != nil {
		return 0, nil, s.startErr
	}
	if s.pid == 0 {
		s.pid = 5252
	}
	return s.pid, func() {}, nil
}

func (s *scriptedDesktop) Observe(context.Context, int) (desktopObservation, error) {
	return desktopObservation{
		Image: s.image, Title: s.title, Class: s.class, Labels: append([]string(nil), s.labels...),
		Focused: s.focused, FocusKind: s.focusKind, Covers: s.covers,
		HasBounds: s.hasBounds, Left: s.left, Top: s.top, Right: s.right, Bottom: s.bottom,
		PasswordEdit: s.passwordEdit,
	}, nil
}

func (s *scriptedDesktop) Click(context.Context, int, int, int) error {
	s.clicks = append(s.clicks, "click")
	if s.revealPay {
		s.labels = []string{"立即支付"}
	}
	return nil
}

func (s *scriptedDesktop) Key(_ context.Context, _ int, key string) error {
	s.keys = append(s.keys, key)
	return nil
}

type scriptedBrowser struct {
	page      acceptancePage
	landed    acceptancePage
	navigated []string
	presses   []string
	clicks    []string
	hands     []string
	pressErr  error
}

func (s *scriptedBrowser) Navigate(_ context.Context, rawURL string) error {
	s.navigated = append(s.navigated, rawURL)
	return nil
}

func (s *scriptedBrowser) Observe(context.Context) (acceptancePage, error) {
	return s.page, nil
}

func (s *scriptedBrowser) Click(context.Context, int, int) error {
	s.clicks = append(s.clicks, "click")
	if s.landed.URL != "" || s.landed.Title != "" || s.landed.Text != "" {
		s.page = s.landed
	}
	return nil
}

func (s *scriptedBrowser) Press(_ context.Context, key string) error {
	if s.pressErr != nil {
		return s.pressErr
	}
	s.presses = append(s.presses, key)
	return nil
}

func (s *scriptedBrowser) HandToUser(_ context.Context, reason, pageURL, title string) error {
	s.hands = append(s.hands, reason+"|"+pageURL+"|"+title)
	return nil
}

type fakeAcceptancePage struct {
	url           string
	title         string
	text          string
	refs          []browser.BrowserElementRef
	flags         browser.BrowserPageFlags
	clicks        []string
	presses       []string
	navigated     int
	landedURL     string
	landedTitle   string
	revealOnClick bool
	revealRefs    []browser.BrowserElementRef
}

func (f *fakeAcceptancePage) Navigate(string) (*browser.BrowserActionResult, error) {
	f.navigated++
	return &browser.BrowserActionResult{}, nil
}

func (f *fakeAcceptancePage) ProbeForModel(string) (*browser.BrowserObservation, error) {
	return &browser.BrowserObservation{Snapshot: browser.BrowserSnapshot{
		SnapshotID:      "snap-1",
		URL:             f.url,
		Title:           f.title,
		PageTextExcerpt: f.text,
		PageFlags:       f.flags,
		Refs:            f.refs,
	}}, nil
}

func (f *fakeAcceptancePage) CurrentURL() string { return f.url }

func (f *fakeAcceptancePage) Click(snapshotID, ref, selector string) (*browser.BrowserActionResult, error) {
	f.clicks = append(f.clicks, snapshotID+"|"+ref+"|"+selector)
	if f.landedURL != "" || f.landedTitle != "" {
		f.url = f.landedURL
		f.title = f.landedTitle
		f.text = ""
		f.refs = nil
	}
	if f.revealOnClick {
		f.refs = append([]browser.BrowserElementRef(nil), f.revealRefs...)
	}
	return &browser.BrowserActionResult{}, nil
}

func (f *fakeAcceptancePage) Press(key string) (*browser.BrowserActionResult, error) {
	f.presses = append(f.presses, key)
	return &browser.BrowserActionResult{}, nil
}

func TestAcceptanceDesktopDrive(t *testing.T) {
	lockAcceptanceHooks(t)
	dir := t.TempDir()
	exe := filepath.Join(dir, "editor.exe")
	acceptanceWritePE(t, exe, acceptanceImageSubsystemGUI)
	userID := "acceptance-desktop"
	agent := acceptanceAgent(t, dir, userID)
	on := true
	acceptanceComputerUseForTest = &on
	x, y := 12, 40
	cb := &codingSubAgentCallbacks{
		filesCreated: map[string]bool{exe: true},
		acceptanceBrief: &codingAcceptanceBrief{Checks: []codingAcceptanceCheck{{
			ID: "play", ClickX: &x, ClickY: &y, Expect: "click",
		}}},
	}
	mismatch := &scriptedDesktop{image: filepath.Join(dir, "not-the-game.exe"), title: "Editor"}
	acceptanceDesktopDriver = mismatch
	_, acc := agent.applyCodingInteractiveAcceptance(nil, "ok", cb, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(mismatch.clicks) != 0 || len(mismatch.keys) != 0 || !acceptanceDetailHas(acc, "image_mismatch") {
		t.Fatalf("mismatch clicks=%v keys=%v acc=%+v", mismatch.clicks, mismatch.keys, acc)
	}

	secret := &scriptedDesktop{image: exe, title: "请输入密码"}
	acceptanceDesktopDriver = secret
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", cb, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(secret.clicks) != 0 || len(secret.keys) != 0 || !acceptanceDetailHas(acc, "handoff_refused") {
		t.Fatalf("secret clicks=%v acc=%+v", secret.clicks, acc)
	}

	pay := &scriptedDesktop{image: exe, title: "Shop", labels: []string{"立即支付"}}
	acceptanceDesktopDriver = pay
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", cb, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(pay.clicks) != 0 || !acceptanceDetailHas(acc, "handoff_refused") {
		t.Fatalf("pay clicks=%v acc=%+v", pay.clicks, acc)
	}

	locked := &scriptedDesktop{image: exe, title: "User Account Control"}
	acceptanceDesktopDriver = locked
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", cb, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(locked.clicks) != 0 || !acceptanceDetailHas(acc, "handoff_refused") {
		t.Fatalf("uac clicks=%v acc=%+v", locked.clicks, acc)
	}

	off := false
	acceptanceComputerUseForTest = &off
	quiet := &scriptedDesktop{image: exe, title: "Editor"}
	acceptanceDesktopDriver = quiet
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", cb, userID, false, false)
	if acc.Reason != acceptanceReasonComputerUseOff || quiet.startCount != 0 || len(quiet.clicks) != 0 || len(quiet.keys) != 0 {
		t.Fatalf("computer use off starts=%d clicks=%v acc=%+v", quiet.startCount, quiet.clicks, acc)
	}

	acceptanceComputerUseForTest = &on
	maclaw := &scriptedDesktop{image: exe, title: "MaClaw", class: "wailsWindow"}
	acceptanceDesktopDriver = maclaw
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", cb, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(maclaw.clicks) != 0 || len(maclaw.keys) != 0 || acc.Reason != acceptanceReasonMaClaw {
		t.Fatalf("maclaw clicks=%v keys=%v acc=%+v", maclaw.clicks, maclaw.keys, acc)
	}

	payKeys := &scriptedDesktop{image: exe, title: "Shop", labels: []string{"立即支付"}}
	acceptanceDesktopDriver = payKeys
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{filesCreated: map[string]bool{exe: true}}, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(payKeys.keys) != 0 || len(payKeys.clicks) != 0 || !acceptanceDetailHas(acc, "handoff_refused") {
		t.Fatalf("pay keys=%v clicks=%v acc=%+v", payKeys.keys, payKeys.clicks, acc)
	}

	// A password caption is not the window title. Arrow keys and clicks still stop.
	secretLabel := &scriptedDesktop{image: exe, title: "Editor", labels: []string{"密码"}, hasBounds: true, right: 200, bottom: 200}
	acceptanceDesktopDriver = secretLabel
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{
		filesCreated: map[string]bool{exe: true},
		acceptanceBrief: &codingAcceptanceBrief{Checks: []codingAcceptanceCheck{{
			ID: "type", ClickX: &x, ClickY: &y, Expect: "click",
		}}},
	}, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(secretLabel.clicks) != 0 || len(secretLabel.keys) != 0 || !acceptanceDetailHas(acc, "handoff_refused") {
		t.Fatalf("password label clicks=%v keys=%v acc=%+v", secretLabel.clicks, secretLabel.keys, acc)
	}
	if acceptanceDesktopRefusal(desktopObservation{Title: "密码学", Labels: []string{"密码学"}}, "left") != "" {
		t.Fatal("密码学 was treated as a password field")
	}
	passwordEdit := &scriptedDesktop{image: exe, title: "Editor", passwordEdit: true, hasBounds: true, right: 200, bottom: 200}
	acceptanceDesktopDriver = passwordEdit
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{
		filesCreated: map[string]bool{exe: true},
		acceptanceBrief: &codingAcceptanceBrief{Checks: []codingAcceptanceCheck{{
			ID: "type", ClickX: &x, ClickY: &y, Expect: "click",
		}}},
	}, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(passwordEdit.clicks) != 0 || len(passwordEdit.keys) != 0 || !acceptanceDetailHas(acc, "handoff_refused") {
		t.Fatalf("password edit clicks=%v keys=%v acc=%+v", passwordEdit.clicks, passwordEdit.keys, acc)
	}

	// A rejected step that still carries coordinates must not click. This is
	// the shape a stored brief can have if the key was refused but the point remained.
	mixX, mixY := 3, 4
	mixDesktop := &scriptedDesktop{image: exe, title: "Editor"}
	acceptanceDesktopDriver = mixDesktop
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{
		filesCreated: map[string]bool{exe: true},
		acceptanceBrief: &codingAcceptanceBrief{Checks: []codingAcceptanceCheck{{
			ID: "mix", ClickX: &mixX, ClickY: &mixY, Keys: []string{"tab"}, Expect: "expect_rejected",
		}}},
	}, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(mixDesktop.clicks) != 0 || len(mixDesktop.keys) != 0 || !acceptanceDetailHas(acc, "expect_rejected") {
		t.Fatalf("rejected click clicks=%v keys=%v acc=%+v", mixDesktop.clicks, mixDesktop.keys, acc)
	}

	clickX, clickY := 8, 9
	revealed := &scriptedDesktop{image: exe, title: "Shop", revealPay: true, hasBounds: true, right: 200, bottom: 200}
	acceptanceDesktopDriver = revealed
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{
		filesCreated: map[string]bool{exe: true},
		acceptanceBrief: &codingAcceptanceBrief{Checks: []codingAcceptanceCheck{{
			ID: "buy", ClickX: &clickX, ClickY: &clickY, Expect: "click",
		}}},
	}, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(revealed.clicks) != 1 || len(revealed.keys) != 0 || !acceptanceDetailHas(acc, "handoff_refused") {
		t.Fatalf("pay after click clicks=%v keys=%v acc=%+v", revealed.clicks, revealed.keys, acc)
	}

	unbounded := &scriptedDesktop{image: exe, title: "Editor"}
	acceptanceDesktopDriver = unbounded
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{
		filesCreated: map[string]bool{exe: true},
		acceptanceBrief: &codingAcceptanceBrief{Checks: []codingAcceptanceCheck{{
			ID: "miss", ClickX: &clickX, ClickY: &clickY, Expect: "click",
		}}},
	}, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(unbounded.clicks) != 0 || !acceptanceDetailHas(acc, "click_outside") {
		t.Fatalf("missing bounds clicks=%v acc=%+v", unbounded.clicks, acc)
	}

	outX, outY := 500, 500
	outsideClick := &scriptedDesktop{image: exe, title: "Editor", hasBounds: true, right: 20, bottom: 20}
	acceptanceDesktopDriver = outsideClick
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{
		filesCreated: map[string]bool{exe: true},
		acceptanceBrief: &codingAcceptanceBrief{Checks: []codingAcceptanceCheck{{
			ID: "out", ClickX: &outX, ClickY: &outY, Expect: "click",
		}}},
	}, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(outsideClick.clicks) != 0 || !strings.Contains(acc.Reason, "窗口之外") {
		t.Fatalf("outside clicks=%v acc=%+v", outsideClick.clicks, acc)
	}

	good := &scriptedDesktop{image: exe, title: "Tetris", covers: true}
	acceptanceDesktopDriver = good
	var notes []string
	agent.onProgress = func(text string) { notes = append(notes, text) }
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{filesCreated: map[string]bool{exe: true}}, userID, false, false)
	if acc.Status != acceptanceStatusPassed || strings.Join(good.keys, ",") != "left,right,down" || len(good.clicks) != 0 {
		t.Fatalf("desktop pass keys=%v clicks=%v acc=%+v", good.keys, good.clicks, acc)
	}
	if !strings.Contains(strings.Join(notes, "\n"), "停止在托盘") {
		t.Fatalf("cover progress %v", notes)
	}
}

func TestAcceptanceWebDrive(t *testing.T) {
	if got := acceptancePageNeedsUser(acceptancePage{Title: "密码学", Text: "置换"}); got != "" {
		t.Fatalf("密码学 was treated as a wall %q", got)
	}
	if acceptancePageNeedsUser(acceptancePage{Title: "请输入密码"}) == "" {
		t.Fatal("password title was not a wall")
	}
	if got := acceptancePageNeedsUser(acceptancePage{Title: "Login"}); got != "login" {
		t.Fatalf("Login title wall %q", got)
	}
	if got := acceptancePageNeedsUser(acceptancePage{Title: "Sign-in"}); got != "login" {
		t.Fatalf("sign-in title wall %q", got)
	}
	if got := acceptancePageNeedsUser(acceptancePage{Title: "signing the build"}); got != "" {
		t.Fatalf("signing was a login wall %q", got)
	}
	if err := acceptanceNavigationAllowed("127.0.0.1", 8080, "http://localhost:8080/"); err == nil {
		t.Fatal("127.0.0.1 admitted localhost")
	}
	if err := acceptanceNavigationAllowed("localhost", 8080, "http://127.0.0.1:8080/"); err == nil {
		t.Fatal("localhost admitted 127.0.0.1")
	}
	if err := acceptanceNavigationAllowed("127.0.0.1", 8080, "about:blank"); err == nil {
		t.Fatal("about: was allowed")
	}
	if err := acceptanceNavigationAllowed("", 8080, "http://127.0.0.1:8080/"); err == nil {
		t.Fatal("empty allowlist was allowed")
	}
	if err := acceptanceNavigationAllowed("127.0.0.1", 8080, "file:///C:/game.html"); err == nil {
		t.Fatal("file: was allowed")
	}
	if err := acceptanceNavigationAllowed("127.0.0.1", 8080, "data:text/html,hi"); err == nil {
		t.Fatal("data: was allowed")
	}
	if err := acceptanceNavigationAllowed("127.0.0.1", 8080, "http://127.0.0.1:9/"); err == nil {
		t.Fatal("other port was allowed")
	}
	if err := acceptanceNavigationAllowed("127.0.0.1", 8080, "http://127.0.0.1:8080/play"); err != nil {
		t.Fatal(err)
	}
	credErr := acceptanceNavigationAllowed("127.0.0.1", 8080, "http://admin:hunter2@127.0.0.1:8080/")
	if credErr == nil || strings.Contains(credErr.Error(), "hunter2") {
		t.Fatalf("userinfo navigation: %v", credErr)
	}
	if err := acceptanceNavigationAllowed("127.0.0.1", 8080, "http://127.0.0.1:8080/\\evil"); err == nil {
		t.Fatal("backslash navigation was allowed")
	}
	encodedErr := acceptanceNavigationAllowed("127.0.0.1", 8080, "http://admin:hunter2%40127.0.0.1:8080/")
	if encodedErr == nil || strings.Contains(encodedErr.Error(), "hunter2") {
		t.Fatalf("encoded userinfo navigation: %v", encodedErr)
	}
	if err := acceptanceNavigationAllowed("127.0.0.1", 8080, "http://127.0.0.1:8080/%5c%5cevil"); err == nil {
		t.Fatal("encoded backslash navigation was allowed")
	}

	lockAcceptanceHooks(t)
	dir := t.TempDir()
	server := filepath.Join(dir, "server.exe")
	acceptanceWritePE(t, server, acceptanceImageSubsystemCUI)
	userID := "acceptance-web"
	agent := acceptanceAgent(t, dir, userID)
	console := &scriptedConsole{}
	desktop := &scriptedDesktop{}
	acceptanceConsoleDriver = console
	acceptanceDesktopDriver = desktop
	page := &scriptedBrowser{page: acceptancePage{URL: "http://127.0.0.1:8080/", Title: "Play", Text: "board"}}
	acceptanceBrowserDriver = page
	acceptanceListenFinder = func(string) (acceptanceListen, bool) {
		return acceptanceListen{Host: "127.0.0.1", Port: 8080, Image: server}, true
	}
	_, acc := agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{filesCreated: map[string]bool{server: true}}, userID, false, false)
	if acc.Kind != acceptanceKindWeb || acc.Status != acceptanceStatusPassed || console.startCount != 0 || desktop.startCount != 0 {
		t.Fatalf("web acc=%+v console=%d desktop=%d", acc, console.startCount, desktop.startCount)
	}
	if len(page.navigated) == 0 || strings.Contains(strings.Join(page.navigated, " "), "localhost") || !strings.Contains(page.navigated[0], "127.0.0.1:8080") {
		t.Fatalf("navigated %v", page.navigated)
	}
	agent.handler.clearCodingExecCheckpoint(userID)

	walled := &scriptedBrowser{page: acceptancePage{URL: "http://127.0.0.1:8080/", Title: "请输入密码", Text: "登录"}}
	acceptanceBrowserDriver = walled
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", nil, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(walled.hands) == 0 || len(walled.clicks) != 0 || desktop.startCount != 0 {
		t.Fatalf("wall hands=%v clicks=%v acc=%+v", walled.hands, walled.clicks, acc)
	}
	agent.handler.clearCodingExecCheckpoint(userID)

	aboutBrief := &codingAcceptanceBrief{Checks: []codingAcceptanceCheck{{ID: "bad", URL: "about:blank", Expect: "navigate"}}}
	blank := &scriptedBrowser{page: acceptancePage{URL: "about:blank"}}
	acceptanceBrowserDriver = blank
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{acceptanceBrief: aboutBrief}, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(blank.navigated) != 0 {
		t.Fatalf("about navigated=%v acc=%+v", blank.navigated, acc)
	}
	agent.handler.clearCodingExecCheckpoint(userID)

	if _, ok := normalizeAcceptanceListen(dir, acceptanceListen{Host: "127.0.0.1", Port: 8080}); ok {
		t.Fatal("listen without an image was accepted")
	}
	outside := filepath.Join(filepath.Dir(dir), "other.exe")
	if _, ok := normalizeAcceptanceListen(dir, acceptanceListen{Host: "127.0.0.1", Port: 8080, Image: outside}); ok {
		t.Fatal("listen image outside the project was accepted")
	}
	if _, ok := normalizeAcceptanceListen(dir, acceptanceListen{Host: "127.0.0.1", Port: 8080, Image: filepath.Join(dir, "test_server.exe")}); ok {
		t.Fatal("listen image of a test executable was accepted")
	}
	if _, ok := normalizeAcceptanceListen(dir, acceptanceListen{Host: "127.0.0.1", Port: 8080, Image: filepath.Join(dir, "Tests", "tetris.EXE")}); !ok {
		t.Fatal("tetris under Tests was rejected as a listen image")
	}

	jumped := &scriptedBrowser{page: acceptancePage{URL: "http://localhost:8080/", Title: "Play"}}
	acceptanceBrowserDriver = jumped
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", nil, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(jumped.clicks) != 0 || len(jumped.navigated) == 0 {
		t.Fatalf("redirect clicks=%v navigated=%v acc=%+v", jumped.clicks, jumped.navigated, acc)
	}
	agent.handler.clearCodingExecCheckpoint(userID)

	badKey := &scriptedBrowser{page: acceptancePage{URL: "http://127.0.0.1:8080/", Title: "Play"}}
	acceptanceBrowserDriver = badKey
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{
		acceptanceBrief: &codingAcceptanceBrief{Checks: []codingAcceptanceCheck{{ID: "tab", Keys: []string{"tab"}, Expect: "navigate"}}},
	}, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(badKey.presses) != 0 || !acceptanceDetailHas(acc, "expect_rejected") {
		t.Fatalf("tab presses=%v acc=%+v", badKey.presses, acc)
	}
	agent.handler.clearCodingExecCheckpoint(userID)

	// Observe can still be a clean title when the key itself hits a secret control.
	secretKey := &scriptedBrowser{
		page:     acceptancePage{URL: "http://127.0.0.1:8080/", Title: "Play", Text: "board"},
		pressErr: &acceptanceClickWallError{wall: "password", key: true},
	}
	acceptanceBrowserDriver = secretKey
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{
		acceptanceBrief: &codingAcceptanceBrief{Checks: []codingAcceptanceCheck{{ID: "move", Keys: []string{"left"}, Expect: "direction_change"}}},
	}, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(secretKey.presses) != 0 || !acceptanceDetailHas(acc, "handoff_refused") || !strings.Contains(acc.Reason, "留给用户") || strings.Contains(acc.Reason, "hunter2") {
		t.Fatalf("secret key presses=%v acc=%+v", secretKey.presses, acc)
	}
	agent.handler.clearCodingExecCheckpoint(userID)

	halfX := 12
	halfPage := &scriptedBrowser{page: acceptancePage{URL: "http://127.0.0.1:8080/", Title: "Play"}}
	acceptanceBrowserDriver = halfPage
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{
		acceptanceBrief: &codingAcceptanceBrief{Checks: []codingAcceptanceCheck{{ID: "half", ClickX: &halfX, Expect: "click"}}},
	}, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(halfPage.clicks) != 0 || len(halfPage.presses) != 0 || !acceptanceDetailHas(acc, "expect_rejected") {
		t.Fatalf("half click clicks=%v presses=%v acc=%+v", halfPage.clicks, halfPage.presses, acc)
	}
	agent.handler.clearCodingExecCheckpoint(userID)

	chordBrief, chordNote := parseAcceptanceBrief(`{"checks":[{"id":"chord","keys":["left","right"],"expect":"left"}]}`)
	if chordNote != "accepted" || len(chordBrief.Checks) != 1 || chordBrief.Checks[0].Expect != "expect_rejected" {
		t.Fatalf("chord brief %+v %s", chordBrief, chordNote)
	}
	chordPage := &scriptedBrowser{page: acceptancePage{URL: "http://127.0.0.1:8080/", Title: "Play"}}
	acceptanceBrowserDriver = chordPage
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{acceptanceBrief: &chordBrief}, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(chordPage.presses) != 0 || len(chordPage.clicks) != 0 || !acceptanceDetailHas(acc, "expect_rejected") {
		t.Fatalf("chord presses=%v clicks=%v acc=%+v", chordPage.presses, chordPage.clicks, acc)
	}
	agent.handler.clearCodingExecCheckpoint(userID)

	spaceBrief, spaceNote := parseAcceptanceBrief(`{"checks":[{"id":"drop","keys":["space"],"expect":"static_change"}]}`)
	if spaceNote != "accepted" || len(spaceBrief.Checks) != 1 || spaceBrief.Checks[0].Expect != "expect_rejected" {
		t.Fatalf("space brief %+v %s", spaceBrief, spaceNote)
	}
	spacePage := &scriptedBrowser{page: acceptancePage{URL: "http://127.0.0.1:8080/", Title: "Play"}}
	acceptanceBrowserDriver = spacePage
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{acceptanceBrief: &spaceBrief}, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(spacePage.presses) != 0 || len(spacePage.clicks) != 0 || !acceptanceDetailHas(acc, "expect_rejected") {
		t.Fatalf("space presses=%v clicks=%v acc=%+v", spacePage.presses, spacePage.clicks, acc)
	}
	agent.handler.clearCodingExecCheckpoint(userID)

	credPage := &scriptedBrowser{page: acceptancePage{URL: "http://127.0.0.1:8080/", Title: "Play"}}
	acceptanceBrowserDriver = credPage
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{
		acceptanceBrief: &codingAcceptanceBrief{Checks: []codingAcceptanceCheck{{
			ID: "cred", URL: "http://admin:hunter2@127.0.0.1:8080/", Expect: "navigate",
		}}},
	}, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(credPage.navigated) != 0 || len(credPage.clicks) != 0 || strings.Contains(acc.Reason, "hunter2") {
		t.Fatalf("userinfo navigated=%v acc=%+v", credPage.navigated, acc)
	}
	agent.handler.clearCodingExecCheckpoint(userID)

	buyX, buyY := 8, 9
	buyPage := &scriptedBrowser{
		page:   acceptancePage{URL: "http://127.0.0.1:8080/", Title: "Play", Text: "board"},
		landed: acceptancePage{URL: "http://127.0.0.1:8080/pay", Title: "立即支付"},
	}
	acceptanceBrowserDriver = buyPage
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{
		acceptanceBrief: &codingAcceptanceBrief{Checks: []codingAcceptanceCheck{{
			ID: "buy", ClickX: &buyX, ClickY: &buyY, Expect: "click",
		}}},
	}, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(buyPage.clicks) != 1 || len(buyPage.hands) == 0 || !acceptanceDetailHas(acc, "handoff_refused") {
		t.Fatalf("pay after click clicks=%v hands=%v acc=%+v", buyPage.clicks, buyPage.hands, acc)
	}
}

func TestAcceptanceWebElementClick(t *testing.T) {
	lockAcceptanceHooks(t)
	origin := "http://127.0.0.1:8080/"
	small := browser.BrowserBoundingBox{X: 4, Y: 4, Width: 30, Height: 16}
	large := browser.BrowserBoundingBox{X: 0, Y: 0, Width: 200, Height: 200}
	page := &fakeAcceptancePage{
		url:   origin,
		title: "Play",
		text:  "board",
		refs: []browser.BrowserElementRef{
			{Ref: "@e2", SnapshotID: "snap-1", Name: "board", Visible: true, InViewport: true, BoundingBox: large},
			{Ref: "@e1", SnapshotID: "snap-1", Name: "开始", Visible: true, InViewport: true, BoundingBox: small},
		},
	}
	live := liveAcceptanceBrowser{sess: page, host: "127.0.0.1", port: 8080}
	if err := live.Click(context.Background(), 10, 10); err != nil || len(page.clicks) != 1 || page.clicks[0] != "snap-1|@e1|" {
		t.Fatalf("bound click err=%v clicks=%v", err, page.clicks)
	}

	page.clicks = nil
	page.url = "http://localhost:8080/"
	if err := live.Click(context.Background(), 10, 10); err == nil || len(page.clicks) != 0 || !strings.Contains(err.Error(), "没有可点击的元素引用") {
		t.Fatalf("off-origin err=%v clicks=%v", err, page.clicks)
	}

	page.url = origin
	page.refs = []browser.BrowserElementRef{{
		Ref: "@pay", SnapshotID: "snap-1", Name: "立即支付", Visible: true, InViewport: true, BoundingBox: small,
	}}
	payErr := live.Click(context.Background(), 10, 10)
	payWall, payRefused := acceptanceClickWall(payErr)
	if !payRefused || payWall != "payment" || len(page.clicks) != 0 || strings.Contains(payErr.Error(), "立即支付") {
		t.Fatalf("pay element wall=%s err=%v clicks=%v", payWall, payErr, page.clicks)
	}

	page.refs = []browser.BrowserElementRef{
		{Ref: "@pay", SnapshotID: "snap-1", Name: "立即支付", Visible: true, InViewport: true, BoundingBox: large},
		{Ref: "@inner", SnapshotID: "snap-1", Visible: true, InViewport: true, BoundingBox: small},
	}
	nestedErr := live.Click(context.Background(), 10, 10)
	nestedWall, nestedRefused := acceptanceClickWall(nestedErr)
	if !nestedRefused || nestedWall != "payment" || len(page.clicks) != 0 || strings.Contains(nestedErr.Error(), "立即支付") {
		t.Fatalf("nested pay wall=%s err=%v clicks=%v", nestedWall, nestedErr, page.clicks)
	}

	page.refs = []browser.BrowserElementRef{{
		Ref: "@hidden", SnapshotID: "snap-1", Name: "开始", Visible: true, InViewport: false, BoundingBox: small,
	}}
	if err := live.Click(context.Background(), 10, 10); err == nil || len(page.clicks) != 0 {
		t.Fatalf("out of viewport err=%v clicks=%v", err, page.clicks)
	}

	page.url = origin
	page.title = "Play"
	page.text = "board"
	page.refs = []browser.BrowserElementRef{{
		Ref: "@e1", SnapshotID: "snap-9", Name: "开始", Visible: true, InViewport: true, BoundingBox: small,
	}}
	page.clicks = nil
	page.landedURL = origin + "pay"
	page.landedTitle = "请输入密码"
	x, y := 10, 10
	acc := runWebAcceptance(context.Background(), live, acceptanceListen{Host: "127.0.0.1", Port: 8080, URL: origin}, []codingAcceptanceCheck{{
		ID: "buy", ClickX: &x, ClickY: &y, Expect: "click",
	}})
	if acc.Status == acceptanceStatusPassed || len(page.clicks) != 1 || page.clicks[0] != "snap-9|@e1|" || !acceptanceDetailHas(acc, "handoff_refused") {
		t.Fatalf("live handoff clicks=%v acc=%+v", page.clicks, acc)
	}
	if !strings.Contains(acc.Reason, "动作之后") || strings.Contains(acc.Reason, "Computer Use 已打开") {
		t.Fatalf("handoff reason %s", acc.Reason)
	}

	page.url = origin
	page.title = "Play"
	page.text = "board"
	page.landedURL = ""
	page.landedTitle = ""
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{
		{Ref: "@login", SnapshotID: "snap-1", Name: "登录", Visible: true, InViewport: true, BoundingBox: large},
		{Ref: "@inner", SnapshotID: "snap-1", Visible: true, InViewport: true, BoundingBox: small},
	}
	acc = runWebAcceptance(context.Background(), live, acceptanceListen{Host: "127.0.0.1", Port: 8080, URL: origin}, []codingAcceptanceCheck{{
		ID: "enter", ClickX: &x, ClickY: &y, Expect: "click",
	}})
	if acc.Status == acceptanceStatusPassed || len(page.clicks) != 0 || !acceptanceDetailHas(acc, "handoff_refused") || !strings.Contains(acc.Reason, "留给用户") {
		t.Fatalf("login control clicks=%v acc=%+v", page.clicks, acc)
	}

	// A password field that is not under the pointer, and whose text is not in
	// the page excerpt, still belongs to the user.
	page.url = origin
	page.title = "Play"
	page.text = "board"
	page.flags = browser.BrowserPageFlags{}
	page.landedURL = ""
	page.landedTitle = ""
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{
		{Ref: "@go", SnapshotID: "snap-1", Name: "开始", Visible: true, InViewport: true, BoundingBox: small},
		{Ref: "@pw", SnapshotID: "snap-1", InputType: "password", Value: "hunter2", Visible: true, InViewport: true, BoundingBox: browser.BrowserBoundingBox{X: 400, Y: 400, Width: 40, Height: 20}},
	}
	acc = runWebAcceptance(context.Background(), live, acceptanceListen{Host: "127.0.0.1", Port: 8080, URL: origin}, []codingAcceptanceCheck{{
		ID: "secret", ClickX: &x, ClickY: &y, Expect: "click",
	}})
	if acc.Status == acceptanceStatusPassed || len(page.clicks) != 0 || !acceptanceDetailHas(acc, "handoff_refused") || !strings.Contains(acc.Reason, "留给用户") || strings.Contains(acc.Reason, "hunter2") {
		t.Fatalf("password field clicks=%v acc=%+v", page.clicks, acc)
	}

	page.clicks = nil
	page.refs = []browser.BrowserElementRef{{
		Ref: "@paybtn", SnapshotID: "snap-1", Name: "继续", Selector: "#pay-btn", Visible: true, InViewport: true, BoundingBox: small,
	}}
	paySelErr := live.Click(context.Background(), 10, 10)
	paySelWall, paySelRefused := acceptanceClickWall(paySelErr)
	if !paySelRefused || paySelWall != "payment" || len(page.clicks) != 0 || strings.Contains(paySelErr.Error(), "pay-btn") {
		t.Fatalf("pay selector wall=%s err=%v clicks=%v", paySelWall, paySelErr, page.clicks)
	}

	page.clicks = nil
	page.flags = browser.BrowserPageFlags{PaymentConfirm: true}
	page.refs = []browser.BrowserElementRef{{
		Ref: "@go", SnapshotID: "snap-1", Name: "开始", Visible: true, InViewport: true, BoundingBox: small,
	}}
	flagErr := live.Click(context.Background(), 10, 10)
	flagWall, flagRefused := acceptanceClickWall(flagErr)
	if !flagRefused || flagWall != "payment" || len(page.clicks) != 0 {
		t.Fatalf("payment flag wall=%s err=%v clicks=%v", flagWall, flagErr, page.clicks)
	}

	page.flags = browser.BrowserPageFlags{}
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{
		{Ref: "@go", SnapshotID: "snap-1", Name: "开始", Visible: true, InViewport: true, BoundingBox: small},
		{Ref: "@pw", SnapshotID: "snap-1", InputType: "password", Visible: false, InViewport: false, BoundingBox: browser.BrowserBoundingBox{X: 400, Y: 400, Width: 40, Height: 20}},
		{Ref: "@note", SnapshotID: "snap-1", Name: "密码学", Visible: true, InViewport: true, BoundingBox: large},
	}
	if err := live.Click(context.Background(), 10, 10); err != nil || len(page.clicks) != 1 || page.clicks[0] != "snap-1|@go|" {
		t.Fatalf("hidden password and 密码学 err=%v clicks=%v", err, page.clicks)
	}

	// The same hidden field must not block a key. A visible password field
	// blocks the key before Press, and the refusal does not repeat the value.
	page.clicks = nil
	page.presses = nil
	acc = runWebAcceptance(context.Background(), live, acceptanceListen{Host: "127.0.0.1", Port: 8080, URL: origin}, []codingAcceptanceCheck{{
		ID: "move", Keys: []string{"left"}, Expect: "direction_change",
	}})
	if acc.Status != acceptanceStatusPassed || len(page.presses) != 1 || page.presses[0] != "left" || len(page.clicks) != 0 {
		t.Fatalf("cryptography key presses=%v clicks=%v acc=%+v", page.presses, page.clicks, acc)
	}

	page.presses = nil
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{
		{Ref: "@go", SnapshotID: "snap-1", Name: "开始", Visible: true, InViewport: true, BoundingBox: small},
		{Ref: "@pw", SnapshotID: "snap-1", InputType: "password", Value: "hunter2", Visible: true, InViewport: true, BoundingBox: browser.BrowserBoundingBox{X: 400, Y: 400, Width: 40, Height: 20}},
	}
	acc = runWebAcceptance(context.Background(), live, acceptanceListen{Host: "127.0.0.1", Port: 8080, URL: origin}, []codingAcceptanceCheck{{
		ID: "move", Keys: []string{"left"}, Expect: "direction_change",
	}})
	if acc.Status == acceptanceStatusPassed || len(page.presses) != 0 || len(page.clicks) != 0 || !acceptanceDetailHas(acc, "handoff_refused") || !strings.Contains(acc.Reason, "留给用户") || strings.Contains(acc.Reason, "hunter2") {
		t.Fatalf("password key presses=%v clicks=%v acc=%+v", page.presses, page.clicks, acc)
	}
	page.presses = nil
	keyErr := live.Press(context.Background(), "left")
	keyWall, keyRefused := acceptanceClickWall(keyErr)
	if !keyRefused || keyWall != "password" || len(page.presses) != 0 || !strings.Contains(keyErr.Error(), "没有按键") || strings.Contains(keyErr.Error(), "hunter2") {
		t.Fatalf("direct password key wall=%s err=%v presses=%v", keyWall, keyErr, page.presses)
	}
	page.url = "http://localhost:8080/"
	page.presses = nil
	if err := live.Press(context.Background(), "left"); err == nil || len(page.presses) != 0 || strings.Contains(err.Error(), "localhost") {
		t.Fatalf("off-origin key err=%v presses=%v", err, page.presses)
	}

	// A click that reveals a password field is not a pass, even when the title stays clean.
	page.url = origin
	page.title = "Play"
	page.text = "board"
	page.flags = browser.BrowserPageFlags{}
	page.clicks = nil
	page.presses = nil
	page.revealOnClick = true
	page.revealRefs = []browser.BrowserElementRef{
		{Ref: "@pw", SnapshotID: "snap-1", InputType: "password", Value: "hunter2", Visible: true, InViewport: true, BoundingBox: browser.BrowserBoundingBox{X: 8, Y: 8, Width: 40, Height: 16}},
	}
	page.refs = []browser.BrowserElementRef{
		{Ref: "@go", SnapshotID: "snap-1", Name: "开始", Visible: true, InViewport: true, BoundingBox: small},
	}
	acc = runWebAcceptance(context.Background(), live, acceptanceListen{Host: "127.0.0.1", Port: 8080, URL: origin}, []codingAcceptanceCheck{{
		ID: "open", ClickX: &x, ClickY: &y, Expect: "click",
	}})
	if acc.Status == acceptanceStatusPassed || len(page.clicks) != 1 || len(page.presses) != 0 || !acceptanceDetailHas(acc, "handoff_refused") || !strings.Contains(acc.Reason, "动作之后") || strings.Contains(acc.Reason, "hunter2") {
		t.Fatalf("password after click clicks=%v presses=%v acc=%+v", page.clicks, page.presses, acc)
	}

	// "Login" is one word. A placeholder can hide name=password on a later selector.
	page.revealOnClick = false
	page.revealRefs = nil
	page.url = origin
	page.title = "Play"
	page.text = "board"
	page.flags = browser.BrowserPageFlags{}
	page.clicks = nil
	page.presses = nil
	page.refs = []browser.BrowserElementRef{
		{Ref: "@go", SnapshotID: "snap-1", Name: "开始", Visible: true, InViewport: true, BoundingBox: small},
		{Ref: "@login", SnapshotID: "snap-1", Name: "Login", Visible: true, InViewport: true, BoundingBox: large},
	}
	loginErr := live.Click(context.Background(), 10, 10)
	loginWall, loginRefused := acceptanceClickWall(loginErr)
	if !loginRefused || loginWall != "login" || len(page.clicks) != 0 || strings.Contains(loginErr.Error(), "Login") {
		t.Fatalf("login word wall=%s err=%v clicks=%v", loginWall, loginErr, page.clicks)
	}
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{{
		Ref: "@go", SnapshotID: "snap-1", Name: "继续", Selector: "#go",
		SelectorCandidates: []string{`input[name="password"]`},
		StableKey:          "input|go|password|textbox|继续|1",
		Value:              "hunter2", Visible: true, InViewport: true, BoundingBox: small,
	}}
	nameErr := live.Click(context.Background(), 10, 10)
	nameWall, nameRefused := acceptanceClickWall(nameErr)
	if !nameRefused || nameWall != "password" || len(page.clicks) != 0 || strings.Contains(nameErr.Error(), "hunter2") || strings.Contains(nameErr.Error(), "password") {
		t.Fatalf("name attribute wall=%s err=%v clicks=%v", nameWall, nameErr, page.clicks)
	}
}

func TestAcceptanceRemoteProof(t *testing.T) {
	lockAcceptanceHooks(t)
	reconnects := 0
	acceptanceSSHReconnect = func() { reconnects++ }
	console := &scriptedConsole{}
	desktop := &scriptedDesktop{}
	acceptanceConsoleDriver = console
	acceptanceDesktopDriver = desktop
	summary, acc := applyRemoteInteractiveAcceptance("逻辑测试已通过。")
	if summary != "逻辑测试已通过。" || acc.Kind != "" || reconnects != 0 || console.startCount != 0 || desktop.startCount != 0 {
		t.Fatalf("no excuse summary=%q acc=%+v starts=%d", summary, acc, console.startCount)
	}
	summary, acc = applyRemoteInteractiveAcceptance(acceptanceTetrisClosing)
	if !strings.Contains(summary, acceptanceReasonRemoteSentence) || strings.Contains(summary, "无法注入按键") || acc.Kind != acceptanceKindRemoteSkipped {
		t.Fatalf("unproven summary=%q acc=%+v", summary, acc)
	}
	if reconnects != 0 || console.startCount != 0 || desktop.startCount != 0 {
		t.Fatalf("unproven wrote local input console=%d desktop=%d reconnects=%d", console.startCount, desktop.startCount, reconnects)
	}
	if acceptanceProveDisplay != nil {
		t.Fatal("production display decision must run with the test hook unset")
	}
	if acceptanceDecideDisplay().Proven {
		t.Fatal("an unset binding was treated as a proven display")
	}
	if !acceptanceCDPTargetAllowed("wss://hub.example/api/v1/desktop-cdp/abc") || !acceptanceCDPTargetAllowed("wss://hub.example/v1/desktops/tunnel/abc") {
		t.Fatal("in-container tunnel was refused")
	}
	if acceptanceCDPTargetAllowed("wss://hub.example/other") || acceptanceCDPTargetAllowed("ws://desktop:hunter2@evil.example/json") {
		t.Fatal("a public debugger host was allowed")
	}

	dials := 0
	var auth string
	userinfo := false
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dials++
		if r.URL.User != nil || strings.Contains(r.URL.String(), "gate-token") {
			userinfo = true
		}
		auth = r.Header.Get("Authorization")
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	hostport := strings.TrimPrefix(srv.URL, "http://")
	wsURL := "ws://desktop:gate-token@" + hostport + "/devtools/page"

	acceptanceDisplayBindingForTurn = func() acceptanceDisplayBinding {
		return acceptanceDisplayBinding{SameTurn: false, Surface: "cdp", CDPURL: wsURL}
	}
	_, acc = applyRemoteInteractiveAcceptance(acceptanceTetrisClosing)
	if acc.Status == acceptanceStatusPassed || dials != 0 || console.startCount != 0 || desktop.startCount != 0 || reconnects != 0 {
		t.Fatalf("unbound dials=%d acc=%+v", dials, acc)
	}

	acceptanceDisplayBindingForTurn = func() acceptanceDisplayBinding {
		return acceptanceDisplayBinding{SameTurn: true, Surface: "cdp", CDPURL: "wss://desktop:hunter2@evil.example/json"}
	}
	summary, acc = applyRemoteInteractiveAcceptance("ok")
	if acc.Status == acceptanceStatusPassed || dials != 0 || strings.Contains(summary, "hunter2") || strings.Contains(acc.Reason, "hunter2") {
		t.Fatalf("public dials=%d summary=%q acc=%+v", dials, summary, acc)
	}

	_, err := driveProvenRemote(context.Background(), acceptanceDisplayProof{
		Proven: true, Surface: "cdp", CDPURL: "wss://desktop:hunter2@evil.example/json",
	})
	if err == nil || strings.Contains(err.Error(), "hunter2") || !strings.Contains(err.Error(), "没有把本机") || dials != 0 {
		t.Fatalf("forced public dial err=%v dials=%d", err, dials)
	}

	acceptanceDisplayBindingForTurn = func() acceptanceDisplayBinding {
		return acceptanceDisplayBinding{SameTurn: true, Surface: "cdp", CDPURL: wsURL}
	}
	summary, acc = applyRemoteInteractiveAcceptance(acceptanceTetrisClosing)
	if acc.Status != acceptanceStatusPassed || acc.Launch != "in-container:cdp" || acc.Kind != acceptanceKindWeb {
		t.Fatalf("cdp acc=%+v summary=%s", acc, summary)
	}
	if dials != 1 || auth != "Bearer gate-token" || userinfo || strings.Contains(summary, "gate-token") || strings.Contains(summary, "无法注入按键") {
		t.Fatalf("dial auth=%q userinfo=%v dials=%d summary=%s", auth, userinfo, dials, summary)
	}
	if console.startCount != 0 || desktop.startCount != 0 || reconnects != 0 {
		t.Fatalf("cdp wrote local input console=%d desktop=%d reconnects=%d", console.startCount, desktop.startCount, reconnects)
	}

	acceptanceDisplayBindingForTurn = func() acceptanceDisplayBinding {
		return acceptanceDisplayBinding{SameTurn: true, Surface: "xdotool"}
	}
	_, acc = applyRemoteInteractiveAcceptance("ok")
	if acc.Status == acceptanceStatusPassed || !strings.Contains(acc.Reason, "测试环境没有安装容器内驱动") {
		t.Fatalf("xdotool without a container exec acc=%+v", acc)
	}
	if dials != 1 || console.startCount != 0 || desktop.startCount != 0 || reconnects != 0 {
		t.Fatalf("xdotool dials=%d console=%d desktop=%d reconnects=%d", dials, console.startCount, desktop.startCount, reconnects)
	}

	ran := ""
	acceptanceInContainerExec = func(_ context.Context, surface string) error {
		ran = surface
		return nil
	}
	_, acc = applyRemoteInteractiveAcceptance("ok")
	if ran != "xdotool" || acc.Status != acceptanceStatusPassed || acc.Launch != "in-container:xdotool" || acc.Kind != acceptanceKindDesktop {
		t.Fatalf("xdotool seam ran=%s acc=%+v", ran, acc)
	}
	if dials != 1 || console.startCount != 0 || desktop.startCount != 0 || reconnects != 0 {
		t.Fatalf("xdotool seam dials=%d console=%d desktop=%d reconnects=%d", dials, console.startCount, desktop.startCount, reconnects)
	}
}

func acceptanceAgent(t *testing.T, dir, userID string) *CodingSubAgent {
	t.Helper()
	h := &IMMessageHandler{}
	h.sessionLoops.Store(userID, &sessionLoopState{windowHeld: true})
	t.Cleanup(func() { h.clearCodingExecCheckpoint(userID) })
	return &CodingSubAgent{
		handler:             h,
		projectPath:         dir,
		turnCtx:             context.Background(),
		acceptanceTurnStart: time.Now(),
		permissionOwnerID:   userID,
	}
}

func acceptanceDetailHas(acc CodingInteractiveAcceptance, detail string) bool {
	for _, check := range acc.Checks {
		if strings.Contains(check.Detail, detail) {
			return true
		}
	}
	return false
}
