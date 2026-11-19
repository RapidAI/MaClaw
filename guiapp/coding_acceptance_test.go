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
	clickErr     error
	keyErr       error
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
	if s.clickErr != nil {
		return s.clickErr
	}
	s.clicks = append(s.clicks, "click")
	if s.revealPay {
		s.labels = []string{"立即支付"}
	}
	return nil
}

func (s *scriptedDesktop) Key(_ context.Context, _ int, key string) error {
	if s.keyErr != nil {
		return s.keyErr
	}
	s.keys = append(s.keys, key)
	return nil
}

type scriptedBrowser struct {
	page       acceptancePage
	landed     acceptancePage
	navigated  []string
	presses    []string
	clicks     []string
	hands      []string
	pressErr   error
	clickErr   error
	navErr     error
	observeErr error
	stop       func()
}

func (s *scriptedBrowser) Navigate(_ context.Context, rawURL string) error {
	if s.navErr != nil {
		if s.stop != nil {
			s.stop()
		}
		return s.navErr
	}
	s.navigated = append(s.navigated, rawURL)
	return nil
}

func (s *scriptedBrowser) Observe(context.Context) (acceptancePage, error) {
	if s.observeErr != nil {
		if s.stop != nil {
			s.stop()
		}
		return acceptancePage{}, s.observeErr
	}
	return s.page, nil
}

func (s *scriptedBrowser) Click(context.Context, int, int) error {
	if s.clickErr != nil {
		return s.clickErr
	}
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
	actionStatus  string
	actionDetail  string
	actionDisplay string
	navStatus     string
	navDetail     string
	navDisplay    string
	navErr        error
	// navResultURL and actionResultURL are the address the action result
	// reports. They stay independent of the probe, which can still be the
	// previous document.
	navResultURL    string
	actionResultURL string
}

func (f *fakeAcceptancePage) actionResult(status, detail, display, resultURL string) *browser.BrowserActionResult {
	result := &browser.BrowserActionResult{Status: status, Detail: detail, Display: display}
	if strings.TrimSpace(resultURL) != "" {
		result.Data = map[string]interface{}{"url": resultURL}
	}
	return result
}

func (f *fakeAcceptancePage) Navigate(string) (*browser.BrowserActionResult, error) {
	f.navigated++
	if f.navErr != nil {
		return nil, f.navErr
	}
	return f.actionResult(f.navStatus, f.navDetail, f.navDisplay, f.navResultURL), nil
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
	return f.actionResult(f.actionStatus, f.actionDetail, f.actionDisplay, f.actionResultURL), nil
}

func (f *fakeAcceptancePage) Press(key string) (*browser.BrowserActionResult, error) {
	f.presses = append(f.presses, key)
	return f.actionResult(f.actionStatus, f.actionDetail, f.actionDisplay, f.actionResultURL), nil
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

	// The first look is a normal window. The driver then sees a secret surface
	// and must not be recorded as a generic click failure.
	lateSecret := &scriptedDesktop{
		image: exe, title: "Editor", hasBounds: true, right: 200, bottom: 200,
		clickErr: &acceptanceDesktopDriveFault{
			detail: "handoff_refused",
			reason: "秘密、支付或系统安全界面已拒绝。没有点击，没有按键，也没有等待确认。",
		},
	}
	acceptanceDesktopDriver = lateSecret
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{
		filesCreated: map[string]bool{exe: true},
		acceptanceBrief: &codingAcceptanceBrief{Checks: []codingAcceptanceCheck{{
			ID: "late", ClickX: &clickX, ClickY: &clickY, Expect: "click",
		}}},
	}, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(lateSecret.clicks) != 0 || !acceptanceDetailHas(acc, "handoff_refused") || strings.Contains(acc.Reason, "点击失败") || !strings.Contains(acc.Reason, "秘密") {
		t.Fatalf("late secret clicks=%v acc=%+v", lateSecret.clicks, acc)
	}

	lateImage := &scriptedDesktop{
		image: exe, title: "Tetris",
		keyErr: &acceptanceDesktopDriveFault{
			detail: "image_mismatch",
			reason: "前台映像路径与刚构建的程序不一致，没有点击也没有按键。",
		},
	}
	acceptanceDesktopDriver = lateImage
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{filesCreated: map[string]bool{exe: true}}, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(lateImage.keys) != 0 || !acceptanceDetailHas(acc, "image_mismatch") || strings.Contains(acc.Reason, "按键失败") {
		t.Fatalf("late image keys=%v acc=%+v", lateImage.keys, acc)
	}

	lateBoom := &scriptedDesktop{
		image: exe, title: "Editor", hasBounds: true, right: 200, bottom: 200,
		clickErr: errors.New("boom"),
	}
	acceptanceDesktopDriver = lateBoom
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{
		filesCreated: map[string]bool{exe: true},
		acceptanceBrief: &codingAcceptanceBrief{Checks: []codingAcceptanceCheck{{
			ID: "boom", ClickX: &clickX, ClickY: &clickY, Expect: "click",
		}}},
	}, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(lateBoom.clicks) != 0 || !acceptanceDetailHas(acc, "click_failed") || !strings.Contains(acc.Reason, "点击失败") {
		t.Fatalf("plain click error clicks=%v acc=%+v", lateBoom.clicks, acc)
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
	parseErr := acceptanceNavigationAllowed("127.0.0.1", 8080, "http://127.0.0.1:8080/%zzhunter2")
	if parseErr == nil || strings.Contains(parseErr.Error(), "hunter2") || strings.Contains(parseErr.Error(), "%zz") {
		t.Fatalf("parse error echoed the url: %v", parseErr)
	}
	hostErr := acceptanceNavigationAllowed("127.0.0.1", 8080, "http://evil.example/hunter2")
	if hostErr == nil || strings.Contains(hostErr.Error(), "hunter2") || strings.Contains(hostErr.Error(), "evil.example") {
		t.Fatalf("host refusal echoed the target: %v", hostErr)
	}
	schemeErr := acceptanceNavigationAllowed("127.0.0.1", 8080, "hunter2:secret")
	if schemeErr == nil || strings.Contains(schemeErr.Error(), "hunter2") || strings.Contains(schemeErr.Error(), "secret") {
		t.Fatalf("scheme refusal echoed the target: %v", schemeErr)
	}
	portErr := acceptanceNavigationAllowed("127.0.0.1", 8080, "http://127.0.0.1:9/hunter2")
	if portErr == nil || strings.Contains(portErr.Error(), "hunter2") || strings.Contains(portErr.Error(), "9") {
		t.Fatalf("port refusal echoed the target: %v", portErr)
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

	loginTitle := &scriptedBrowser{page: acceptancePage{URL: "http://127.0.0.1:8080/", Title: "Login", Text: "board"}}
	acceptanceBrowserDriver = loginTitle
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", nil, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(loginTitle.hands) == 0 || len(loginTitle.clicks) != 0 {
		t.Fatalf("login title hands=%v clicks=%v acc=%+v", loginTitle.hands, loginTitle.clicks, acc)
	}
	agent.handler.clearCodingExecCheckpoint(userID)

	// Words in the page body are prose. The start button is still clicked.
	proseX, proseY := 8, 9
	prose := &scriptedBrowser{page: acceptancePage{
		URL:   "http://127.0.0.1:8080/",
		Title: "Play",
		Text:  "请输入密码 Login 立即支付 signing",
	}}
	acceptanceBrowserDriver = prose
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{
		filesCreated: map[string]bool{server: true},
		acceptanceBrief: &codingAcceptanceBrief{Checks: []codingAcceptanceCheck{{
			ID: "play", ClickX: &proseX, ClickY: &proseY, Expect: "click",
		}}},
	}, userID, false, false)
	if acc.Status != acceptanceStatusPassed || len(prose.clicks) != 1 || acceptanceDetailHas(acc, "handoff_refused") || strings.Contains(acc.Reason, "密码") || strings.Contains(acc.Reason, "Login") {
		t.Fatalf("prose body clicks=%v acc=%+v", prose.clicks, acc)
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
	if acc.Status == acceptanceStatusPassed || len(jumped.clicks) != 0 || len(jumped.navigated) == 0 || strings.Contains(acc.Reason, "localhost") || !strings.Contains(acc.Reason, "离开了监听地址") {
		t.Fatalf("redirect clicks=%v navigated=%v acc=%+v", jumped.clicks, jumped.navigated, acc)
	}
	agent.handler.clearCodingExecCheckpoint(userID)

	leftX, leftY := 3, 4
	left := &scriptedBrowser{
		page:   acceptancePage{URL: "http://127.0.0.1:8080/", Title: "Play", Text: "board"},
		landed: acceptancePage{URL: "http://evil.example/x?token=hunter2", Title: "away"},
	}
	acceptanceBrowserDriver = left
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{
		acceptanceBrief: &codingAcceptanceBrief{Checks: []codingAcceptanceCheck{{
			ID: "leave", ClickX: &leftX, ClickY: &leftY, Expect: "click",
		}}},
	}, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(left.clicks) != 1 || !strings.Contains(acc.Reason, "动作之后页面离开了监听地址") || strings.Contains(acc.Reason, "hunter2") || strings.Contains(acc.Reason, "evil.example") {
		t.Fatalf("left after click clicks=%v acc=%+v", left.clicks, acc)
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

	// The second look can refuse the origin. That sentence stays the reason.
	offKey := &scriptedBrowser{
		page: acceptancePage{URL: "http://127.0.0.1:8080/", Title: "Play", Text: "board"},
		pressErr: &acceptanceWebDriveFault{
			detail: "navigation_refused",
			reason: "隔离浏览器没有留在监听地址。没有按键。",
		},
	}
	acceptanceBrowserDriver = offKey
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{
		acceptanceBrief: &codingAcceptanceBrief{Checks: []codingAcceptanceCheck{{ID: "move", Keys: []string{"left"}, Expect: "direction_change"}}},
	}, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(offKey.presses) != 0 || !acceptanceDetailHas(acc, "navigation_refused") || !strings.Contains(acc.Reason, "没有按键") || strings.Contains(acc.Reason, "localhost") {
		t.Fatalf("off-origin key presses=%v acc=%+v", offKey.presses, acc)
	}
	agent.handler.clearCodingExecCheckpoint(userID)

	// An unclassified click error must not be copied into the summary.
	boomClick := &scriptedBrowser{
		page:     acceptancePage{URL: "http://127.0.0.1:8080/", Title: "Play", Text: "board"},
		clickErr: errors.New("boom hunter2"),
	}
	acceptanceBrowserDriver = boomClick
	boomX, boomY := 4, 4
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{
		acceptanceBrief: &codingAcceptanceBrief{Checks: []codingAcceptanceCheck{{ID: "play", ClickX: &boomX, ClickY: &boomY, Expect: "click"}}},
	}, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(boomClick.clicks) != 0 || !acceptanceDetailHas(acc, "click_failed") || strings.Contains(acc.Reason, "hunter2") || strings.Contains(acc.Reason, "boom") || !strings.Contains(acc.Reason, "没有点到") {
		t.Fatalf("plain click error clicks=%v acc=%+v", boomClick.clicks, acc)
	}
	agent.handler.clearCodingExecCheckpoint(userID)

	// A blocked navigation did not open the page. The old page must not pass.
	blockedNav := &scriptedBrowser{
		page:   acceptancePage{URL: "http://127.0.0.1:8080/", Title: "Play", Text: "board"},
		navErr: &acceptanceClickWallError{wall: "payment"},
	}
	acceptanceBrowserDriver = blockedNav
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", nil, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(blockedNav.navigated) != 0 || len(blockedNav.hands) == 0 || !acceptanceDetailHas(acc, "handoff_refused") || !strings.Contains(acc.Reason, "留给用户") {
		t.Fatalf("blocked navigate navigated=%v hands=%v acc=%+v", blockedNav.navigated, blockedNav.hands, acc)
	}
	agent.handler.clearCodingExecCheckpoint(userID)

	boomNav := &scriptedBrowser{
		page:   acceptancePage{URL: "http://127.0.0.1:8080/", Title: "Play", Text: "board"},
		navErr: errors.New("boom hunter2"),
	}
	acceptanceBrowserDriver = boomNav
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", nil, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(boomNav.navigated) != 0 || !acceptanceDetailHas(acc, "navigate_failed") || strings.Contains(acc.Reason, "hunter2") || strings.Contains(acc.Reason, "boom") || !strings.Contains(acc.Reason, "没有打开页面") {
		t.Fatalf("plain navigate error navigated=%v acc=%+v", boomNav.navigated, acc)
	}
	agent.handler.clearCodingExecCheckpoint(userID)

	boomObserve := &scriptedBrowser{
		page:       acceptancePage{URL: "http://127.0.0.1:8080/", Title: "Play", Text: "board"},
		observeErr: errors.New("boom hunter2"),
	}
	acceptanceBrowserDriver = boomObserve
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", nil, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(boomObserve.navigated) == 0 || !acceptanceDetailHas(acc, "observe_failed") || strings.Contains(acc.Reason, "hunter2") || strings.Contains(acc.Reason, "boom") || !strings.Contains(acc.Reason, "观察页面失败") {
		t.Fatalf("plain observe error navigated=%v acc=%+v", boomObserve.navigated, acc)
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

	parsePage := &scriptedBrowser{page: acceptancePage{URL: "http://127.0.0.1:8080/", Title: "Play"}}
	acceptanceBrowserDriver = parsePage
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{
		acceptanceBrief: &codingAcceptanceBrief{Checks: []codingAcceptanceCheck{{
			ID: "parse", URL: "http://127.0.0.1:8080/%zzhunter2", Expect: "navigate",
		}}},
	}, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(parsePage.navigated) != 0 || !acceptanceDetailHas(acc, "navigation_refused") || strings.Contains(acc.Reason, "hunter2") || strings.Contains(acc.Reason, "%zz") {
		t.Fatalf("parse url navigated=%v acc=%+v", parsePage.navigated, acc)
	}
	agent.handler.clearCodingExecCheckpoint(userID)

	hostPage := &scriptedBrowser{page: acceptancePage{URL: "http://127.0.0.1:8080/", Title: "Play"}}
	acceptanceBrowserDriver = hostPage
	_, acc = agent.applyCodingInteractiveAcceptance(nil, "ok", &codingSubAgentCallbacks{
		acceptanceBrief: &codingAcceptanceBrief{Checks: []codingAcceptanceCheck{{
			ID: "host", URL: "http://evil.example/hunter2", Expect: "navigate",
		}}},
	}, userID, false, false)
	if acc.Status == acceptanceStatusPassed || len(hostPage.navigated) != 0 || !acceptanceDetailHas(acc, "navigation_refused") || strings.Contains(acc.Reason, "hunter2") || strings.Contains(acc.Reason, "evil.example") {
		t.Fatalf("foreign host navigated=%v acc=%+v", hostPage.navigated, acc)
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
	// The pay token on the left of a combinator names an ancestor. The button
	// is the last compound. The control's own class and id still count.
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{{
		Ref: "@go", SnapshotID: "snap-1", Name: "开始",
		Selector:           `#pay-btn > button:nth-of-type(1)`,
		SelectorCandidates: []string{`div[data-testid="buy-now"] > button:nth-of-type(1)`, `#go-pay button`},
		Visible:            true, InViewport: true, BoundingBox: small,
	}}
	if err := live.Click(context.Background(), 10, 10); err != nil || len(page.clicks) != 1 {
		t.Fatalf("ancestor pay selector err=%v clicks=%v", err, page.clicks)
	}
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{{
		Ref: "@pay", SnapshotID: "snap-1", Name: "继续", Selector: "button.pay-btn",
		Visible: true, InViewport: true, BoundingBox: small,
	}}
	ownPay := live.Click(context.Background(), 10, 10)
	ownPayWall, ownPayRefused := acceptanceClickWall(ownPay)
	if !ownPayRefused || ownPayWall != "payment" || len(page.clicks) != 0 || strings.Contains(ownPay.Error(), "pay-btn") {
		t.Fatalf("own pay class wall=%s err=%v clicks=%v", ownPayWall, ownPay, page.clicks)
	}
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{{
		Ref: "@pay", SnapshotID: "snap-1", Name: "继续",
		StableKey: "button|pay-btn||button|继续|1",
		Visible:   true, InViewport: true, BoundingBox: small,
	}}
	ownKey := live.Click(context.Background(), 10, 10)
	ownKeyWall, ownKeyRefused := acceptanceClickWall(ownKey)
	if !ownKeyRefused || ownKeyWall != "payment" || len(page.clicks) != 0 || strings.Contains(ownKey.Error(), "pay-btn") {
		t.Fatalf("own pay id wall=%s err=%v clicks=%v", ownKeyWall, ownKey, page.clicks)
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

	// autocomplete is not the visible name. The probe appends it as a selector.
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{{
		Ref: "@go", SnapshotID: "snap-1", Name: "继续", Selector: "#go",
		SelectorCandidates: []string{`input[autocomplete="current-password"]`},
		Value:              "hunter2", Visible: true, InViewport: true, BoundingBox: small,
	}}
	acErr := live.Click(context.Background(), 10, 10)
	acWall, acRefused := acceptanceClickWall(acErr)
	if !acRefused || acWall != "password" || len(page.clicks) != 0 || strings.Contains(acErr.Error(), "hunter2") || strings.Contains(acErr.Error(), "password") || strings.Contains(acErr.Error(), "current-password") {
		t.Fatalf("autocomplete password wall=%s err=%v clicks=%v", acWall, acErr, page.clicks)
	}
	for _, token := range []struct {
		sel  string
		wall string
	}{
		{`input[autocomplete="one-time-code"]`, "captcha"},
		{`input[autocomplete="cc-csc"]`, "payment"},
		{`input[autocomplete="cc-number"]`, "payment"},
		{`input[autocomplete="cc-exp"]`, "payment"},
	} {
		page.clicks = nil
		page.refs = []browser.BrowserElementRef{{
			Ref: "@go", SnapshotID: "snap-1", Name: "继续", Selector: "#go",
			SelectorCandidates: []string{token.sel},
			Visible:            true, InViewport: true, BoundingBox: small,
		}}
		err := live.Click(context.Background(), 10, 10)
		wall, refused := acceptanceClickWall(err)
		if !refused || wall != token.wall || len(page.clicks) != 0 || strings.Contains(err.Error(), "one-time-code") || strings.Contains(err.Error(), "cc-csc") || strings.Contains(err.Error(), "cc-number") || strings.Contains(err.Error(), "cc-exp") {
			t.Fatalf("autocomplete %s wall=%s err=%v clicks=%v", token.sel, wall, err, page.clicks)
		}
	}
	if acceptancePageNeedsUser(acceptancePage{Title: "one-time offer"}) != "" {
		t.Fatal("one-time offer was treated as a code field")
	}
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{{
		Ref: "@go", SnapshotID: "snap-1", Name: "继续", Selector: "#go",
		SelectorCandidates: []string{`input[autocomplete="email"]`},
		Visible:            true, InViewport: true, BoundingBox: small,
	}}
	if err := live.Click(context.Background(), 10, 10); err != nil || len(page.clicks) != 1 {
		t.Fatalf("email autocomplete err=%v clicks=%v", err, page.clicks)
	}

	// A nil error with status ask/blocked did not send the click.
	page.clicks = nil
	page.actionStatus = "unchanged"
	if err := live.Click(context.Background(), 10, 10); err != nil || len(page.clicks) != 1 {
		t.Fatalf("unchanged click err=%v clicks=%v", err, page.clicks)
	}
	page.clicks = nil
	page.actionStatus = "ask"
	page.actionDetail = "login_qr"
	page.actionDisplay = "hunter2"
	askErr := live.Click(context.Background(), 10, 10)
	askWall, askRefused := acceptanceClickWall(askErr)
	if !askRefused || askWall != "login" || strings.Contains(askErr.Error(), "hunter2") || strings.Contains(askErr.Error(), "login_qr") {
		t.Fatalf("ask result wall=%s err=%v", askWall, askErr)
	}
	page.actionStatus = "blocked"
	page.actionDetail = ""
	page.actionDisplay = "已在下单/支付前停下 hunter2"
	page.clicks = nil
	acc = runWebAcceptance(context.Background(), live, acceptanceListen{Host: "127.0.0.1", Port: 8080, URL: origin}, []codingAcceptanceCheck{{
		ID: "pay", ClickX: &x, ClickY: &y, Expect: "click",
	}})
	if acc.Status == acceptanceStatusPassed || !acceptanceDetailHas(acc, "handoff_refused") || !strings.Contains(acc.Reason, "留给用户") || strings.Contains(acc.Reason, "hunter2") || strings.Contains(acc.Reason, "支付前") {
		t.Fatalf("blocked click acc=%+v", acc)
	}

	// unchanged on the same document landed. A different document did not.
	// ask and blocked did not open the page, and their display is not copied.
	page.actionStatus = "ok"
	page.actionDetail = ""
	page.actionDisplay = ""
	page.clicks = nil
	page.navStatus = "unchanged"
	if err := live.Navigate(context.Background(), origin); err != nil {
		t.Fatalf("unchanged navigate err=%v", err)
	}
	if err := live.Navigate(context.Background(), "http://127.0.0.1:8080"); err != nil {
		t.Fatalf("unchanged equivalent document err=%v", err)
	}
	stayed := live.Navigate(context.Background(), origin+"pay?token=hunter2")
	stayedDetail, stayedReason := acceptanceWebNavigateFailure(stayed)
	if stayedDetail != "navigation_refused" || !strings.Contains(stayedReason, "没有打开请求的页面") || strings.Contains(stayed.Error(), "hunter2") || strings.Contains(stayed.Error(), "pay") {
		t.Fatalf("unchanged other document detail=%s err=%v", stayedDetail, stayed)
	}
	page.clicks = nil
	acc = runWebAcceptance(context.Background(), live, acceptanceListen{Host: "127.0.0.1", Port: 8080, URL: origin}, []codingAcceptanceCheck{{
		ID: "stay", URL: origin + "pay?token=hunter2", Expect: "navigate",
	}})
	if acc.Status == acceptanceStatusPassed || len(page.clicks) != 0 || !acceptanceDetailHas(acc, "navigation_refused") || !strings.Contains(acc.Reason, "没有打开请求的页面") || strings.Contains(acc.Reason, "hunter2") {
		t.Fatalf("unchanged navigate through runner clicks=%v acc=%+v", page.clicks, acc)
	}
	page.navStatus = "ask"
	page.navDetail = "login_qr"
	page.navDisplay = "hunter2"
	askNav := live.Navigate(context.Background(), origin)
	askNavWall, askNavRefused := acceptanceClickWall(askNav)
	if !askNavRefused || askNavWall != "login" || strings.Contains(askNav.Error(), "hunter2") || strings.Contains(askNav.Error(), "login_qr") {
		t.Fatalf("ask navigate wall=%s err=%v", askNavWall, askNav)
	}
	page.navStatus = "blocked"
	page.navDetail = ""
	page.navDisplay = "已在下单/支付前停下 hunter2"
	blockedNavErr := live.Navigate(context.Background(), origin)
	blockedNavWall, blockedNavRefused := acceptanceClickWall(blockedNavErr)
	if !blockedNavRefused || blockedNavWall != "payment" || strings.Contains(blockedNavErr.Error(), "hunter2") || strings.Contains(blockedNavErr.Error(), "支付前") {
		t.Fatalf("blocked navigate wall=%s err=%v", blockedNavWall, blockedNavErr)
	}
	page.navStatus = ""
	page.navDisplay = ""
	page.navErr = errors.New("boom hunter2")
	plainNav := live.Navigate(context.Background(), origin)
	if plainNav == nil || strings.Contains(plainNav.Error(), "hunter2") || strings.Contains(plainNav.Error(), "boom") || !strings.Contains(plainNav.Error(), "没有打开页面") {
		t.Fatalf("plain navigate err=%v", plainNav)
	}
	page.navErr = nil
	page.navStatus = "blocked"
	page.navDisplay = "已在下单/支付前停下 hunter2"
	page.clicks = nil
	acc = runWebAcceptance(context.Background(), live, acceptanceListen{Host: "127.0.0.1", Port: 8080, URL: origin}, []codingAcceptanceCheck{{
		ID: "pay", ClickX: &x, ClickY: &y, Expect: "click",
	}})
	if acc.Status == acceptanceStatusPassed || len(page.clicks) != 0 || !acceptanceDetailHas(acc, "handoff_refused") || !strings.Contains(acc.Reason, "留给用户") || strings.Contains(acc.Reason, "hunter2") || strings.Contains(acc.Reason, "支付前") {
		t.Fatalf("blocked navigate through runner clicks=%v acc=%+v", page.clicks, acc)
	}

	navCtx, navCancel := context.WithCancel(context.Background())
	stopping := &scriptedBrowser{navErr: errors.New("boom hunter2"), stop: navCancel}
	acc = runWebAcceptance(navCtx, stopping, acceptanceListen{Host: "127.0.0.1", Port: 8080, URL: origin}, nil)
	if acc.Status != acceptanceStatusCancelled || acc.Reason != acceptanceReasonCancelled || len(stopping.navigated) != 0 || strings.Contains(acc.Reason, "hunter2") {
		t.Fatalf("cancel during navigate acc=%+v", acc)
	}
	obsCtx, obsCancel := context.WithCancel(context.Background())
	stoppingObserve := &scriptedBrowser{
		page:       acceptancePage{URL: origin, Title: "Play"},
		observeErr: errors.New("boom hunter2"),
		stop:       obsCancel,
	}
	acc = runWebAcceptance(obsCtx, stoppingObserve, acceptanceListen{Host: "127.0.0.1", Port: 8080, URL: origin}, nil)
	if acc.Status != acceptanceStatusCancelled || acc.Reason != acceptanceReasonCancelled || strings.Contains(acc.Reason, "hunter2") {
		t.Fatalf("cancel during observe acc=%+v", acc)
	}

	// The action result can name a disallowed document while the next probe
	// still shows this origin. That result is not a pass, and it is not echoed.
	page.navErr = nil
	page.navStatus = "unchanged"
	page.navDetail = ""
	page.navDisplay = ""
	page.navResultURL = "http://admin:hunter2@127.0.0.1:8080/"
	page.url = origin
	page.clicks = nil
	page.navigated = 0
	credNav := live.Navigate(context.Background(), origin)
	credDetail, credReason := acceptanceWebNavigateFailure(credNav)
	if credDetail != "navigation_refused" || !strings.Contains(credReason, "没有打开请求的页面") || strings.Contains(credNav.Error(), "hunter2") || strings.Contains(credNav.Error(), "admin") {
		t.Fatalf("userinfo result detail=%s err=%v", credDetail, credNav)
	}
	acc = runWebAcceptance(context.Background(), live, acceptanceListen{Host: "127.0.0.1", Port: 8080, URL: origin}, []codingAcceptanceCheck{{
		ID: "stay", URL: origin, Expect: "navigate",
	}})
	if acc.Status == acceptanceStatusPassed || len(page.clicks) != 0 || !acceptanceDetailHas(acc, "navigation_refused") || !strings.Contains(acc.Reason, "没有打开请求的页面") || strings.Contains(acc.Reason, "hunter2") || strings.Contains(acc.Reason, "admin") {
		t.Fatalf("userinfo result through runner clicks=%v acc=%+v", page.clicks, acc)
	}
	page.navResultURL = ""
	page.url = "http://admin:hunter2@127.0.0.1:8080/"
	bareCred := live.Navigate(context.Background(), origin)
	if bareCred == nil || strings.Contains(bareCred.Error(), "hunter2") || strings.Contains(bareCred.Error(), "admin") || !strings.Contains(bareCred.Error(), "没有打开请求的页面") {
		t.Fatalf("userinfo current url err=%v", bareCred)
	}
	page.url = origin
	page.navStatus = "ok"
	page.navResultURL = "http://evil.example/hunter2"
	leftNav := live.Navigate(context.Background(), origin)
	leftDetail, leftReason := acceptanceWebNavigateFailure(leftNav)
	if leftDetail != "navigation_refused" || !strings.Contains(leftReason, "没有打开请求的页面") || strings.Contains(leftNav.Error(), "hunter2") || strings.Contains(leftNav.Error(), "evil.example") {
		t.Fatalf("off-origin result detail=%s err=%v", leftDetail, leftNav)
	}
	page.clicks = nil
	acc = runWebAcceptance(context.Background(), live, acceptanceListen{Host: "127.0.0.1", Port: 8080, URL: origin}, []codingAcceptanceCheck{{
		ID: "leave", ClickX: &x, ClickY: &y, Expect: "click",
	}})
	if acc.Status == acceptanceStatusPassed || len(page.clicks) != 0 || !acceptanceDetailHas(acc, "navigation_refused") || strings.Contains(acc.Reason, "hunter2") || strings.Contains(acc.Reason, "evil.example") {
		t.Fatalf("off-origin result through runner clicks=%v acc=%+v", page.clicks, acc)
	}
	page.navResultURL = "http://127.0.0.1:8080/play"
	page.clicks = nil
	acc = runWebAcceptance(context.Background(), live, acceptanceListen{Host: "127.0.0.1", Port: 8080, URL: origin}, []codingAcceptanceCheck{{
		ID: "redirect", URL: origin, Expect: "navigate",
	}})
	if acc.Status != acceptanceStatusPassed || len(page.clicks) != 0 || strings.Contains(acc.Reason, "play") {
		t.Fatalf("same-host redirect acc=%+v", acc)
	}
	page.navResultURL = ""
	page.navStatus = "blocked"
	page.navDisplay = "已在下单/支付前停下 hunter2"
	page.navResultURL = "http://evil.example/hunter2"
	blockedWithURL := live.Navigate(context.Background(), origin)
	blockedURLWall, blockedURLRefused := acceptanceClickWall(blockedWithURL)
	if !blockedURLRefused || blockedURLWall != "payment" || strings.Contains(blockedWithURL.Error(), "hunter2") || strings.Contains(blockedWithURL.Error(), "evil.example") {
		t.Fatalf("blocked result url wall=%s err=%v", blockedURLWall, blockedWithURL)
	}
	page.navStatus = ""
	page.navDisplay = ""
	page.navResultURL = ""

	page.actionStatus = "unchanged"
	page.actionResultURL = origin
	page.clicks = nil
	if err := live.Click(context.Background(), 10, 10); err != nil || len(page.clicks) != 1 {
		t.Fatalf("unchanged click on origin err=%v clicks=%v", err, page.clicks)
	}
	page.actionStatus = "ok"
	page.actionResultURL = "http://evil.example/hunter2"
	page.clicks = nil
	leftClick := live.Click(context.Background(), 10, 10)
	clickDetail, clickReason := acceptanceWebFailure(leftClick, false)
	if clickDetail != "navigation_refused" || len(page.clicks) != 1 || !strings.Contains(clickReason, "离开了监听地址") || strings.Contains(leftClick.Error(), "hunter2") || strings.Contains(leftClick.Error(), "evil.example") {
		t.Fatalf("click result url detail=%s clicks=%v err=%v", clickDetail, page.clicks, leftClick)
	}
	page.actionResultURL = "http://127.0.0.1:9/hunter2"
	page.presses = nil
	leftPress := live.Press(context.Background(), "left")
	pressDetail, pressReason := acceptanceWebFailure(leftPress, true)
	if pressDetail != "navigation_refused" || len(page.presses) != 1 || !strings.Contains(pressReason, "离开了监听地址") || strings.Contains(leftPress.Error(), "hunter2") || strings.Contains(leftPress.Error(), "9") {
		t.Fatalf("press result url detail=%s presses=%v err=%v", pressDetail, page.presses, leftPress)
	}
	page.actionStatus = ""
	page.actionResultURL = "http://evil.example/hunter2"
	page.clicks = nil
	page.presses = nil
	acc = runWebAcceptance(context.Background(), live, acceptanceListen{Host: "127.0.0.1", Port: 8080, URL: origin}, []codingAcceptanceCheck{{
		ID: "leave", ClickX: &x, ClickY: &y, Expect: "click",
	}})
	if acc.Status == acceptanceStatusPassed || len(page.clicks) != 1 || !acceptanceDetailHas(acc, "navigation_refused") || !strings.Contains(acc.Reason, "离开了监听地址") || strings.Contains(acc.Reason, "hunter2") || strings.Contains(acc.Reason, "evil.example") {
		t.Fatalf("click result through runner clicks=%v acc=%+v", page.clicks, acc)
	}
	page.actionResultURL = ""

	// A paragraph on the control is not a caption. The probe copies that
	// paragraph into the stable key and can also put it in an aria-label
	// selector. A short pay label still is.
	page.actionStatus = ""
	page.navStatus = ""
	page.navResultURL = ""
	page.navDisplay = ""
	page.navErr = nil
	page.flags = browser.BrowserPageFlags{}
	page.url = origin
	page.title = "Play"
	page.text = "请输入密码 Login 立即支付"
	page.revealOnClick = false
	longCaption := "这是一段放在按钮上的说明文字，里面写了登录、立即支付和密码，但不是按钮标题。"
	page.refs = []browser.BrowserElementRef{{
		Ref: "@go", SnapshotID: "snap-1", Name: "开始",
		Text:               longCaption,
		StableKey:          "button|||button|" + longCaption + "|1",
		SelectorCandidates: []string{`[role="button"][aria-label="` + longCaption + `"]`},
		Visible:            true, InViewport: true, BoundingBox: small,
	}}
	page.clicks = nil
	if err := live.Click(context.Background(), 10, 10); err != nil || len(page.clicks) != 1 {
		t.Fatalf("long control text err=%v clicks=%v", err, page.clicks)
	}
	page.clicks = nil
	acc = runWebAcceptance(context.Background(), live, acceptanceListen{Host: "127.0.0.1", Port: 8080, URL: origin}, []codingAcceptanceCheck{{
		ID: "play", ClickX: &x, ClickY: &y, Expect: "click",
	}})
	if acc.Status != acceptanceStatusPassed || len(page.clicks) != 1 || acceptanceDetailHas(acc, "handoff_refused") || strings.Contains(acc.Reason, "密码") || strings.Contains(acc.Reason, "Login") {
		t.Fatalf("prose page clicks=%v acc=%+v", page.clicks, acc)
	}
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{{
		Ref: "@pay", SnapshotID: "snap-1", Text: "立即支付", Visible: true, InViewport: true, BoundingBox: small,
	}}
	shortPay := live.Click(context.Background(), 10, 10)
	shortWall, shortRefused := acceptanceClickWall(shortPay)
	if !shortRefused || shortWall != "payment" || len(page.clicks) != 0 || strings.Contains(shortPay.Error(), "立即支付") {
		t.Fatalf("short pay text wall=%s err=%v clicks=%v", shortWall, shortPay, page.clicks)
	}
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{{
		Ref: "@in", SnapshotID: "snap-1", Name: "Sign-in", Visible: true, InViewport: true, BoundingBox: small,
	}}
	signErr := live.Click(context.Background(), 10, 10)
	signWall, signRefused := acceptanceClickWall(signErr)
	if !signRefused || signWall != "login" || len(page.clicks) != 0 || strings.Contains(signErr.Error(), "Sign-in") {
		t.Fatalf("sign-in control wall=%s err=%v clicks=%v", signWall, signErr, page.clicks)
	}
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{{
		Ref: "@sub", SnapshotID: "snap-1", Name: "密码学", Visible: true, InViewport: true, BoundingBox: small,
	}}
	if err := live.Click(context.Background(), 10, 10); err != nil || len(page.clicks) != 1 {
		t.Fatalf("密码学 control err=%v clicks=%v", err, page.clicks)
	}

	// The probe's accessible name is the same long sentence the stable key stores.
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{{
		Ref: "@go", SnapshotID: "snap-1", Name: longCaption, Text: longCaption,
		StableKey:          "button|||button|" + longCaption + "|1",
		SelectorCandidates: []string{`[role="button"][aria-label="` + longCaption + `"]`},
		Visible:            true, InViewport: true, BoundingBox: small,
	}}
	if err := live.Click(context.Background(), 10, 10); err != nil || len(page.clicks) != 1 {
		t.Fatalf("long stable caption err=%v clicks=%v", err, page.clicks)
	}
	piped := "登录|" + longCaption
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{{
		Ref: "@go", SnapshotID: "snap-1", Name: piped, Text: piped,
		StableKey: "button|||button|" + piped + "|1",
		Visible:   true, InViewport: true, BoundingBox: small,
	}}
	if err := live.Click(context.Background(), 10, 10); err != nil || len(page.clicks) != 1 {
		t.Fatalf("piped long caption err=%v clicks=%v", err, page.clicks)
	}

	// A short caption in the stable key is still a label. An id is not.
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{{
		Ref: "@go", SnapshotID: "snap-1", Name: "开始",
		StableKey: "button|||button|登录|1",
		Visible:   true, InViewport: true, BoundingBox: small,
	}}
	loginKey := live.Click(context.Background(), 10, 10)
	loginKeyWall, loginKeyRefused := acceptanceClickWall(loginKey)
	if !loginKeyRefused || loginKeyWall != "login" || len(page.clicks) != 0 || loginKey.Error() != "隔离浏览器拒绝了登录、验证码、密码或支付控件。没有点击。" {
		t.Fatalf("stable caption wall=%s err=%v clicks=%v", loginKeyWall, loginKey, page.clicks)
	}
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{{
		Ref: "@go", SnapshotID: "snap-1", Name: "开始",
		StableKey: "form|login-form||form|开始|1",
		Visible:   true, InViewport: true, BoundingBox: small,
	}}
	if err := live.Click(context.Background(), 10, 10); err != nil || len(page.clicks) != 1 {
		t.Fatalf("login-form id err=%v clicks=%v", err, page.clicks)
	}
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{{
		Ref: "@go", SnapshotID: "snap-1", Name: "继续",
		SelectorCandidates: []string{`input[name="passwordless"]`},
		Visible:            true, InViewport: true, BoundingBox: small,
	}}
	if err := live.Click(context.Background(), 10, 10); err != nil || len(page.clicks) != 1 {
		t.Fatalf("passwordless err=%v clicks=%v", err, page.clicks)
	}
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{{
		Ref: "@pw", SnapshotID: "snap-1", Name: "继续",
		Selector: `input[type="password"]`,
		Visible:  true, InViewport: true, BoundingBox: small,
	}}
	typeErr := live.Click(context.Background(), 10, 10)
	typeWall, typeRefused := acceptanceClickWall(typeErr)
	if !typeRefused || typeWall != "password" || len(page.clicks) != 0 || strings.Contains(typeErr.Error(), "password") {
		t.Fatalf("type password wall=%s err=%v clicks=%v", typeWall, typeErr, page.clicks)
	}
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{{
		Ref: "@pw", SnapshotID: "snap-1", Name: "继续",
		SelectorCandidates: []string{`input[name="Password"]`},
		Visible:            true, InViewport: true, BoundingBox: small,
	}}
	foldErr := live.Click(context.Background(), 10, 10)
	foldWall, foldRefused := acceptanceClickWall(foldErr)
	if !foldRefused || foldWall != "password" || len(page.clicks) != 0 || strings.Contains(foldErr.Error(), "Password") || strings.Contains(foldErr.Error(), "password") {
		t.Fatalf("folded name wall=%s err=%v clicks=%v", foldWall, foldErr, page.clicks)
	}
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{{
		Ref: "@pw", SnapshotID: "snap-1", Name: "继续", Selector: "#go",
		StableKey: "input|go|password|textbox|继续|1",
		Visible:   true, InViewport: true, BoundingBox: small,
	}}
	keyOnly := live.Click(context.Background(), 10, 10)
	keyOnlyWall, keyOnlyRefused := acceptanceClickWall(keyOnly)
	if !keyOnlyRefused || keyOnlyWall != "password" || len(page.clicks) != 0 || strings.Contains(keyOnly.Error(), "password") {
		t.Fatalf("stable html name wall=%s err=%v clicks=%v", keyOnlyWall, keyOnly, page.clicks)
	}
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{{
		Ref: "@pw", SnapshotID: "snap-1", Name: "继续", Selector: "#go",
		SelectorCandidates: []string{`input[autocomplete="new-password"]`},
		Visible:            true, InViewport: true, BoundingBox: small,
	}}
	newErr := live.Click(context.Background(), 10, 10)
	newWall, newRefused := acceptanceClickWall(newErr)
	if !newRefused || newWall != "password" || len(page.clicks) != 0 || strings.Contains(newErr.Error(), "password") || strings.Contains(newErr.Error(), "new-password") {
		t.Fatalf("new-password wall=%s err=%v clicks=%v", newWall, newErr, page.clicks)
	}

	// Body-text flags are not a control. The widget, the login form, and the
	// card checkout still are. A paragraph that names the tokens is not a field.
	if got := acceptanceFlagWall(browser.BrowserPageFlags{MFA: true, Captcha: true, LoginQR: true}); got != "" {
		t.Fatalf("body flag wall=%s", got)
	}
	if acceptanceFlagWall(browser.BrowserPageFlags{CaptchaWidget: true}) != "captcha" || acceptanceFlagWall(browser.BrowserPageFlags{LoginWall: true}) != "login" || acceptanceFlagWall(browser.BrowserPageFlags{PaymentConfirm: true}) != "payment" {
		t.Fatal("structural page flag was dropped")
	}
	page.flags = browser.BrowserPageFlags{MFA: true, Captcha: true, LoginQR: true}
	page.title = "Play"
	page.text = "请输入验证码 one-time offer 扫码登录"
	noisy := longCaption + " otp one-time-code current-password pay-btn"
	page.refs = []browser.BrowserElementRef{{
		Ref: "@go", SnapshotID: "snap-1", Name: "开始", Text: noisy,
		StableKey:          "button|||button|" + noisy + "|1",
		SelectorCandidates: []string{`[role="button"][aria-label="` + noisy + `"]`},
		Visible:            true, InViewport: true, BoundingBox: small,
	}}
	page.clicks = nil
	if err := live.Click(context.Background(), 10, 10); err != nil || len(page.clicks) != 1 {
		t.Fatalf("body flags and token prose err=%v clicks=%v", err, page.clicks)
	}
	page.flags = browser.BrowserPageFlags{CaptchaWidget: true}
	page.clicks = nil
	widgetErr := live.Click(context.Background(), 10, 10)
	widgetWall, widgetRefused := acceptanceClickWall(widgetErr)
	if !widgetRefused || widgetWall != "captcha" || len(page.clicks) != 0 {
		t.Fatalf("captcha widget wall=%s err=%v clicks=%v", widgetWall, widgetErr, page.clicks)
	}
	// LoginWall without a visible entry is a hidden input plus body text.
	page.flags = browser.BrowserPageFlags{LoginWall: true}
	page.text = "请输入密码"
	page.clicks = nil
	if err := live.Click(context.Background(), 10, 10); err != nil || len(page.clicks) != 1 {
		t.Fatalf("login flag without entry err=%v clicks=%v", err, page.clicks)
	}
	page.refs = []browser.BrowserElementRef{
		{Ref: "@go", SnapshotID: "snap-1", Name: "开始", Visible: true, InViewport: true, BoundingBox: small},
		{Ref: "@mail", SnapshotID: "snap-1", Name: "邮箱", InputType: "email", Visible: true, InViewport: true, BoundingBox: small},
	}
	page.clicks = nil
	formErr := live.Click(context.Background(), 10, 10)
	formWall, formRefused := acceptanceClickWall(formErr)
	if !formRefused || formWall != "login" || len(page.clicks) != 0 || strings.Contains(formErr.Error(), "邮箱") {
		t.Fatalf("email entry wall=%s err=%v clicks=%v", formWall, formErr, page.clicks)
	}
	page.refs = []browser.BrowserElementRef{{
		Ref: "@name", SnapshotID: "snap-1", Name: "名字", InputType: "text",
		StableKey: "input|player||textbox|名字|1",
		Visible:   true, InViewport: true, BoundingBox: small,
	}}
	page.clicks = nil
	if err := live.Click(context.Background(), 10, 10); err != nil || len(page.clicks) != 1 {
		t.Fatalf("player name err=%v clicks=%v", err, page.clicks)
	}
	page.refs = []browser.BrowserElementRef{{
		Ref: "@mail", SnapshotID: "snap-1", Name: "继续", InputType: "text",
		StableKey: "input|id|user_email|textbox|继续|1",
		Visible:   true, InViewport: true, BoundingBox: small,
	}}
	page.clicks = nil
	mailErr := live.Click(context.Background(), 10, 10)
	mailWall, mailRefused := acceptanceClickWall(mailErr)
	if !mailRefused || mailWall != "login" || len(page.clicks) != 0 || strings.Contains(mailErr.Error(), "email") {
		t.Fatalf("user_email wall=%s err=%v clicks=%v", mailWall, mailErr, page.clicks)
	}
	// The id or HTML name is a word, not a character sequence. A compound
	// field still confirms the flag. headphone contains the letters of phone
	// and does not.
	for _, tc := range []struct {
		key  string
		sel  string
		wall bool
	}{
		{key: "input||user-phone|input|继续|1", wall: true},
		{key: "input|userEmail||input|继续|1", wall: true},
		{key: "input|userEmail2||input|继续|1", wall: true},
		{key: "input|HTMLEmail||input|继续|1", wall: true},
		{key: "input|telephone||input|继续|1", wall: true},
		{key: "input|id|cellphone|input|继续|1", wall: true},
		{sel: `input[name="email2"]`, wall: true},
		{sel: `input[name="user-phone"]`, wall: true},
		{key: "input|email-support||input|帮助|1", wall: true},
		{key: "button|email-support||button|帮助|1"},
		{key: "input|headphone||input|音量|1"},
		{key: "input|id|microphone|input|音量|1"},
		{key: "input|automobile||input|继续|1"},
		{key: "input|id|premailer|input|继续|1"},
		{key: "input|headphone2||input|音量|1"},
		{key: "input|template||input|继续|1"},
		{sel: `input[name="headphone"]`},
		// The name on the left of a combinator is an ancestor. The control is
		// the last compound. A button is not an entry field.
		{key: "input|player||input|名字|1", sel: `form[name="email"] > input:nth-of-type(1)`},
		{sel: `button[name="user_email"]`},
		{sel: `div[name="phone"] > input[name="player"]`},
		{sel: `form[name="password"] > button:nth-of-type(1)`},
		{sel: `form[autocomplete="current-password"] > button:nth-of-type(1)`},
		{sel: `input[type="text"][name="user_email"]:nth-of-type(2)`, wall: true},
		{sel: `input[name="email"]:nth-child(n+1)`, wall: true},
	} {
		page.clicks = nil
		ref := browser.BrowserElementRef{
			Ref: "@f", SnapshotID: "snap-1", Name: "继续", InputType: "text",
			Visible: true, InViewport: true, BoundingBox: small,
		}
		if tc.key != "" {
			ref.StableKey = tc.key
		}
		if tc.sel != "" {
			ref.SelectorCandidates = []string{tc.sel}
		}
		page.refs = []browser.BrowserElementRef{ref}
		err := live.Click(context.Background(), 10, 10)
		if !tc.wall {
			if err != nil || len(page.clicks) != 1 {
				t.Fatalf("login name click key=%s sel=%s err=%v clicks=%v", tc.key, tc.sel, err, page.clicks)
			}
			continue
		}
		wall, refused := acceptanceClickWall(err)
		if !refused || wall != "login" || len(page.clicks) != 0 || strings.Contains(strings.ToLower(err.Error()), "email") || strings.Contains(strings.ToLower(err.Error()), "phone") {
			t.Fatalf("login name wall key=%s sel=%s wall=%s err=%v clicks=%v", tc.key, tc.sel, wall, err, page.clicks)
		}
	}
	page.flags = browser.BrowserPageFlags{}
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{{
		Ref: "@id", SnapshotID: "snap-1", Name: "继续",
		StableKey: "input|otp||textbox|继续|1",
		Visible:   true, InViewport: true, BoundingBox: small,
	}}
	if err := live.Click(context.Background(), 10, 10); err != nil || len(page.clicks) != 1 {
		t.Fatalf("otp id err=%v clicks=%v", err, page.clicks)
	}
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{{
		Ref: "@otp", SnapshotID: "snap-1", Name: "继续", Selector: "#go",
		SelectorCandidates: []string{`input[name="OTP"]`},
		Visible:            true, InViewport: true, BoundingBox: small,
	}}
	otpErr := live.Click(context.Background(), 10, 10)
	otpWall, otpRefused := acceptanceClickWall(otpErr)
	if !otpRefused || otpWall != "password" || len(page.clicks) != 0 || strings.Contains(otpErr.Error(), "otp") || strings.Contains(otpErr.Error(), "OTP") {
		t.Fatalf("otp name wall=%s err=%v clicks=%v", otpWall, otpErr, page.clicks)
	}
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{{
		Ref: "@otp", SnapshotID: "snap-1", Name: "继续", Selector: "#go",
		StableKey: "input|code|otp|textbox|继续|1",
		Visible:   true, InViewport: true, BoundingBox: small,
	}}
	otpKey := live.Click(context.Background(), 10, 10)
	otpKeyWall, otpKeyRefused := acceptanceClickWall(otpKey)
	if !otpKeyRefused || otpKeyWall != "password" || len(page.clicks) != 0 || strings.Contains(otpKey.Error(), "otp") {
		t.Fatalf("otp stable name wall=%s err=%v clicks=%v", otpKeyWall, otpKey, page.clicks)
	}
	page.clicks = nil
	page.refs = []browser.BrowserElementRef{{
		Ref: "@code", SnapshotID: "snap-1", Name: "继续",
		SelectorCandidates: []string{`input[name="otpcode"]`},
		Visible:            true, InViewport: true, BoundingBox: small,
	}}
	if err := live.Click(context.Background(), 10, 10); err != nil || len(page.clicks) != 1 {
		t.Fatalf("otpcode err=%v clicks=%v", err, page.clicks)
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
