package guiapp

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// Interactive acceptance runs after the coding model loop, on the host.
// PR 1 drives a freshly built Windows console program with WriteConsoleInput.
// It does not call SendInput, focus a window, or open a browser.

const codingAcceptanceBriefToolName = "submit_interactive_acceptance"

const (
	acceptanceKindNone          = "none"
	acceptanceKindConsole       = "console"
	acceptanceKindDesktop       = "desktop"
	acceptanceKindWeb           = "web"
	acceptanceKindRemoteSkipped = "remote_skipped"
	acceptanceKindRefused       = "refused"

	acceptanceStatusSkipped         = "skipped"
	acceptanceStatusPassed          = "passed"
	acceptanceStatusFailed          = "failed"
	acceptanceStatusCancelled       = "cancelled"
	acceptanceStatusPlatformBlocked = "platform_blocked"
)

const (
	acceptanceSampleInterval = 150 * time.Millisecond
	acceptanceAttachWindow   = 3 * time.Second
	acceptanceCLIExitProbe   = 1500 * time.Millisecond
	acceptanceBudget         = 2 * time.Minute
	acceptanceHelperTimeout  = 5 * time.Second
	acceptanceMaxChecks      = 8
	acceptanceGridCols       = 120
	acceptanceGridRows       = 40
	acceptanceScanDirCap     = 2000
	acceptanceScanExeCap     = 200
	acceptanceOutputDirName  = "acceptance"

	acceptanceImageSubsystemGUI = 2
	acceptanceImageSubsystemCUI = 3

	acceptanceReasonNoWindow       = "交互验证未做，因为这一回合没有持有 owner turn window"
	acceptanceReasonNoTurnCtx      = "交互验证未做，因为这一回合没有可取消的执行上下文"
	acceptanceReasonNoCandidate    = "交互验证未做，因为没有找到本回合新产生的控制台程序。"
	acceptanceReasonGUI            = "交互验证未做，因为这个程序是窗口程序，控制台验收不会启动它。"
	acceptanceReasonMultiple       = "多个可执行文件，未选定交互目标"
	acceptanceReasonMaClaw         = "不会驱动 MaClaw 自己的界面"
	acceptanceReasonDisabled       = "交互验证已关闭"
	acceptanceReasonNoPlatform     = "当前系统还没有控制台缓冲验收"
	acceptanceReasonTestNoDriver   = "测试环境没有安装控制台驱动，没有启动进程。"
	acceptanceReasonTestNoDesktop  = "测试环境没有安装窗口驱动，没有发送键鼠。"
	acceptanceReasonTestNoBrowser  = "测试环境没有安装浏览器驱动，没有启动 Chrome。"
	acceptanceReasonComputerUseOff = "Computer Use 已关闭（computer_use_enabled=false）"
	acceptanceReasonCancelled      = "交互验证已取消"
	acceptanceReasonPriorPass      = "交互验证沿用上次通过的记录"
	acceptanceReasonRemoteSentence = "交互验证未做，因为远端会话没有本机可驱动的显示；不会把本机的控制台输入或键鼠指向另一台机器。"
	acceptanceDesktopMaxSteps      = 40
)

// CodingInteractiveAcceptance is the host result of one post-turn console
// check. It is not part of the model tool protocol and does not change the
// task Status.
type CodingInteractiveAcceptance struct {
	Kind     string
	Status   string
	Launch   string
	Checks   []CodingAcceptanceCheckResult
	Reason   string
	Attempts int
}

// CodingAcceptanceCheckResult is one scripted key compared with the no-key clock.
type CodingAcceptanceCheckResult struct {
	ID     string
	Key    string
	Expect string
	Passed bool
	Detail string
}

type codingAcceptanceCheck struct {
	ID     string   `json:"id"`
	Keys   []string `json:"keys"`
	Expect string   `json:"expect"`
	// ClickX and ClickY are a window or page click. Both must be set.
	// A console check still needs exactly one key.
	ClickX *int `json:"click_x,omitempty"`
	ClickY *int `json:"click_y,omitempty"`
	// URL is a web navigation target. The host allows only the listened origin.
	URL string `json:"url,omitempty"`
}

type codingAcceptanceBrief struct {
	Launch     string                  `json:"launch"`
	WorkingDir string                  `json:"working_dir"`
	Kind       string                  `json:"kind"`
	Checks     []codingAcceptanceCheck `json:"checks"`
	SecretLike bool                    `json:"-"`
}

type consoleCell struct {
	col int
	row int
}

type consoleGrid struct {
	cols  int
	rows  int
	text  string
	cells map[[2]int]struct{}
}

type consoleHelperResult struct {
	OK          bool   `json:"ok"`
	Reason      string `json:"reason,omitempty"`
	Win32       int    `json:"win32,omitempty"`
	ConsoleHWND uint64 `json:"console_hwnd,omitempty"`
	Cols        int    `json:"cols,omitempty"`
	Rows        int    `json:"rows,omitempty"`
	Text        string `json:"text,omitempty"`
}

// consoleAcceptanceSession launches one process and reads or writes its console.
// Tests install a fake. Production uses the Windows helper, and never falls
// through to it while testing.Testing is set.
type consoleAcceptanceSession interface {
	Start(ctx context.Context, exe, dir string) (pid int, exitedWithin func(time.Duration) bool, kill func(), err error)
	Probe(ctx context.Context, pid int) (consoleHelperResult, error)
	Read(ctx context.Context, pid int) (consoleGrid, error)
	Write(ctx context.Context, pid int, key string) error
}

var (
	acceptanceConsoleDriver      consoleAcceptanceSession
	acceptanceDesktopDriver      desktopAcceptanceSession
	acceptanceBrowserDriver      acceptanceBrowserSession
	acceptanceListenFinder       func(project string) (acceptanceListen, bool)
	acceptanceProveDisplay       func() acceptanceDisplayProof
	acceptanceRemoteDriver       acceptanceRemoteSession
	acceptanceComputerUseForTest *bool
	// acceptanceDisplayBindingForTurn is the production identity of this
	// turn's display. It stays nil until a same-turn container binding
	// exists. An SSH target is not that container.
	acceptanceDisplayBindingForTurn func() acceptanceDisplayBinding
	// acceptanceInContainerExec runs xdotool inside the proven container.
	// It stays nil until that channel is connected, and it never runs a
	// local binary.
	acceptanceInContainerExec func(ctx context.Context, surface string) error
	// acceptanceReadForeground and acceptanceDesktopClickHook let a test
	// observe one foreground read and whether SendInput would run. Production
	// leaves both nil.
	acceptanceReadForeground   func() (desktopObservation, bool)
	acceptanceDesktopClickHook func(x, y int) error
	// acceptanceSSHReconnect counts a reconnect. Remote acceptance never calls it.
	acceptanceSSHReconnect   func()
	acceptanceEnabledForTest *bool
	acceptanceNow            = time.Now
	acceptanceWait           = time.Sleep
	acceptanceDriving        atomic.Bool
	acceptanceTarget         atomic.Value // string

	// acceptanceStopCancel is Computer Use's stop hook for a drive that has
	// no computeruse.Session. Tests replace it so a bare App does not boot the hub.
	acceptanceStopCancel = func(a *App) {
		if a == nil {
			return
		}
		if _, err := a.CancelAIAssistantSession(); err != nil {
			logComputerUseControl("cancel assistant after acceptance stop: %v", err)
		}
	}
)

func acceptanceDriversInstalled() bool {
	return acceptanceConsoleDriver != nil || acceptanceDesktopDriver != nil || acceptanceBrowserDriver != nil || acceptanceRemoteDriver != nil || acceptanceListenFinder != nil || acceptanceProveDisplay != nil
}

func codingAcceptanceHookActive() bool {
	if acceptanceEnabledForTest != nil || acceptanceDriversInstalled() {
		return true
	}
	return !testing.Testing()
}

func codingInteractiveAcceptanceEnabled(a *App) bool {
	if acceptanceEnabledForTest != nil {
		return *acceptanceEnabledForTest
	}
	if acceptanceDriversInstalled() {
		return true
	}
	if testing.Testing() {
		return false
	}
	if a == nil {
		return true
	}
	cfg, err := a.LoadConfig()
	if err != nil || cfg.CodingInteractiveAcceptance == nil {
		return true
	}
	return *cfg.CodingInteractiveAcceptance
}

func setAcceptanceDriving(target string) {
	acceptanceTarget.Store(target)
	acceptanceDriving.Store(true)
}

func clearAcceptanceDriving() {
	acceptanceDriving.Store(false)
	acceptanceTarget.Store("")
}

func acceptanceDrivingSnapshot() (bool, string) {
	target, _ := acceptanceTarget.Load().(string)
	return acceptanceDriving.Load(), target
}

func isConsoleBufferCommand(args []string) bool {
	return len(args) > 1 && args[1] == "console-buffer"
}

func sessionForAcceptance() (consoleAcceptanceSession, string) {
	if acceptanceConsoleDriver != nil {
		return acceptanceConsoleDriver, ""
	}
	if testing.Testing() {
		return nil, acceptanceReasonTestNoDriver
	}
	return platformAcceptanceSession()
}

func acceptanceRoleIsWorker(role codingSubAgentRole) bool {
	return role == "" || role == codingRoleWorker
}

func (s *CodingSubAgent) finishCodingInteractiveAcceptance(task *TaskItem, summary string, cb *codingSubAgentCallbacks, userID string, inquiry, goalTurn bool) (string, CodingInteractiveAcceptance) {
	if s == nil || !codingAcceptanceHookActive() {
		return summary, CodingInteractiveAcceptance{}
	}
	return s.applyCodingInteractiveAcceptance(task, summary, cb, userID, inquiry, goalTurn)
}

func (s *CodingSubAgent) applyCodingInteractiveAcceptance(task *TaskItem, summary string, cb *codingSubAgentCallbacks, userID string, inquiry, goalTurn bool) (string, CodingInteractiveAcceptance) {
	if s.horizonPosture || s.nestDepth != 0 || !acceptanceRoleIsWorker(s.role) || inquiry || goalTurn {
		// Horizon already has its own computer tools. Do not claim the owner
		// window was missing. Inquiry, goal-plan, and nested roles do not drive.
		if acceptanceExcusePresent(summary) {
			summary = stripAcceptanceExcuseClauses(summary)
		}
		return summary, CodingInteractiveAcceptance{Kind: acceptanceKindNone, Status: acceptanceStatusSkipped}
	}
	project := strings.TrimSpace(s.projectPath)
	var files []string
	var cmds []CodingSubAgentCommandResult
	var brief *codingAcceptanceBrief
	if cb != nil {
		files = cb.getFilesCreated()
		cmds = cb.getCommandsRun()
		brief = cb.acceptanceBrief
	}
	var choice acceptanceTargetChoice
	switch listens := findAcceptanceListens(project); {
	case len(listens) > 1:
		choice = acceptanceTargetChoice{kind: acceptanceKindNone, status: acceptanceStatusSkipped, reason: acceptanceReasonMultiple, multiple: true}
	case len(listens) == 1:
		listen := listens[0]
		choice = acceptanceTargetChoice{
			drive:    true,
			kind:     acceptanceKindWeb,
			launch:   listen.URL,
			artifact: listen.artifact,
			listen:   listen,
		}
	default:
		cands := findAcceptanceCandidates(project, files, cmds, s.acceptanceTurnStart)
		choice = chooseAcceptanceTarget(project, cands, brief)
	}
	if !choice.drive {
		acc := CodingInteractiveAcceptance{Kind: choice.kind, Status: choice.status, Launch: choice.launch, Reason: choice.reason}
		if choice.kind == "" && choice.reason == "" && !acceptanceExcusePresent(summary) {
			return summary, CodingInteractiveAcceptance{}
		}
		if choice.reason == "" && acceptanceExcusePresent(summary) && !choice.guiOnly && !choice.multiple && choice.kind != acceptanceKindRefused {
			acc.Kind = acceptanceKindNone
			acc.Status = acceptanceStatusSkipped
			acc.Reason = acceptanceReasonNoCandidate
		}
		return spliceAcceptanceSummary(summary, acc), acc
	}

	acc := CodingInteractiveAcceptance{Kind: choice.kind, Launch: choice.launch, Status: acceptanceStatusSkipped}
	if acc.Kind == "" {
		acc.Kind = acceptanceKindConsole
	}
	owner := strings.TrimSpace(userID)
	if owner == "" && s.loopCtx != nil {
		owner = strings.TrimSpace(s.loopCtx.UserID)
	}
	if owner == "" {
		owner = strings.TrimSpace(s.permissionOwnerID)
	}
	if s.handler == nil || !s.handler.ownerWindowHeld(owner) {
		acc.Reason = acceptanceReasonNoWindow
		return spliceAcceptanceSummary(summary, acc), acc
	}
	if s.turnCtx == nil {
		acc.Reason = acceptanceReasonNoTurnCtx
		return spliceAcceptanceSummary(summary, acc), acc
	}
	app := (*App)(nil)
	if s.handler != nil {
		app = s.handler.app
	}
	if !codingInteractiveAcceptanceEnabled(app) {
		acc.Reason = acceptanceReasonDisabled
		return spliceAcceptanceSummary(summary, acc), acc
	}
	if s.handler != nil {
		if prev, ok := s.handler.lookupCodingInteractiveAcceptance(owner); ok && prev.Status == acceptanceStatusPassed && prev.Artifact == choice.artifact && choice.artifact != "" {
			acc.Status = acceptanceStatusSkipped
			evidence := "buffer.txt"
			if choice.kind == acceptanceKindWeb {
				evidence = "page.txt"
			}
			acc.Reason = acceptanceReasonPriorPass + "：" + filepath.Join(project, acceptanceOutputDirName, evidence)
			return spliceAcceptanceSummary(summary, acc), acc
		}
	}
	if choice.kind == acceptanceKindDesktop && !acceptanceComputerUseOn(app) {
		acc.Reason = acceptanceReasonComputerUseOff
		return spliceAcceptanceSummary(summary, acc), acc
	}

	ctx, cancel := context.WithTimeout(s.turnCtx, acceptanceBudget)
	defer cancel()
	if s.onProgress != nil {
		emitCodingAgentEvent(s.onProgress, codingAcceptanceProgressEvent(task, choice.launch))
	}
	checks := acceptanceChecksFor(brief)
	if choice.kind == acceptanceKindWeb {
		checks = acceptanceWebChecks(brief, choice.listen)
	}
	var driven CodingInteractiveAcceptance
	var grid consoleGrid
	switch choice.kind {
	case acceptanceKindDesktop:
		desktop, why := sessionForDesktopAcceptance()
		if desktop == nil {
			acc.Status = acceptanceStatusPlatformBlocked
			acc.Reason = why
			if acc.Reason == "" {
				acc.Reason = acceptanceReasonTestNoDesktop
			}
			return spliceAcceptanceSummary(summary, acc), acc
		}
		driven = runDesktopAcceptance(ctx, desktop, choice.launch, choice.workDir, checks, func() {
			if s.onProgress != nil {
				emitCodingAgentEvent(s.onProgress, codingAcceptanceProgressEvent(task, choice.launch+" 停止在托盘"))
			}
		})
	case acceptanceKindWeb:
		browserSess, why := sessionForBrowserAcceptance(choice.listen)
		if browserSess == nil {
			acc.Status = acceptanceStatusPlatformBlocked
			acc.Reason = why
			if acc.Reason == "" {
				acc.Reason = acceptanceReasonTestNoBrowser
			}
			return spliceAcceptanceSummary(summary, acc), acc
		}
		driven = runWebAcceptance(ctx, browserSess, choice.listen, checks)
	default:
		sess, why := sessionForAcceptance()
		if sess == nil {
			acc.Status = acceptanceStatusPlatformBlocked
			acc.Reason = why
			if acc.Reason == "" {
				acc.Reason = acceptanceReasonNoPlatform
			}
			return spliceAcceptanceSummary(summary, acc), acc
		}
		driven, grid = runConsoleAcceptance(ctx, sess, choice.launch, choice.workDir, checks)
		driven.Kind = acceptanceKindConsole
	}
	driven.Launch = choice.launch
	if driven.Status == acceptanceStatusPassed && s.handler != nil && choice.artifact != "" {
		s.handler.rememberCodingInteractiveAcceptance(owner, codingInteractiveAcceptanceRecord{
			Artifact:  choice.artifact,
			Status:    acceptanceStatusPassed,
			FlakeUsed: false,
		})
	}
	if grid.text != "" {
		writeAcceptanceBuffer(project, grid)
	}
	if choice.kind == acceptanceKindWeb && driven.Launch != "" {
		writeAcceptanceBufferFile(project, "page.txt", driven.Launch)
	}
	return spliceAcceptanceSummary(summary, driven), driven
}

type acceptanceTargetChoice struct {
	drive    bool
	kind     string
	status   string
	reason   string
	launch   string
	workDir  string
	artifact string
	guiOnly  bool
	multiple bool
	listen   acceptanceListen
}

func chooseAcceptanceTarget(project string, cands []acceptanceCandidate, brief *codingAcceptanceBrief) acceptanceTargetChoice {
	var cui, gui []acceptanceCandidate
	for _, c := range cands {
		if isTestExecutableBase(c.Path) || acceptanceImageIsMaClaw(c.Path) {
			continue
		}
		switch c.Subsystem {
		case acceptanceImageSubsystemCUI:
			cui = append(cui, c)
		case acceptanceImageSubsystemGUI:
			gui = append(gui, c)
		}
	}
	sawMaClaw := false
	for _, c := range cands {
		if acceptanceImageIsMaClaw(c.Path) {
			sawMaClaw = true
			break
		}
	}
	briefCand, briefTrusted := briefLaunchCandidate(project, brief)
	if briefCand.Path != "" && acceptanceImageIsMaClaw(briefCand.Path) {
		sawMaClaw = true
		briefTrusted = false
	}
	if choice, ok := chooseGUIAcceptanceTarget(project, gui, brief, briefCand, briefTrusted); ok {
		return choice
	}
	if len(cui) == 0 && briefTrusted && briefCand.Subsystem == acceptanceImageSubsystemCUI {
		cui = []acceptanceCandidate{briefCand}
	}
	if len(cui) > 1 {
		if briefTrusted && briefCand.Subsystem == acceptanceImageSubsystemCUI {
			return driveChoice(briefCand, briefWorkDir(project, brief, briefCand.Path), acceptanceKindConsole)
		}
		return acceptanceTargetChoice{kind: acceptanceKindNone, status: acceptanceStatusSkipped, reason: acceptanceReasonMultiple, multiple: true}
	}
	if len(cui) == 1 {
		target := cui[0]
		if briefTrusted && briefCand.Subsystem == acceptanceImageSubsystemCUI {
			target = briefCand
		}
		return driveChoice(target, briefWorkDir(project, brief, target.Path), acceptanceKindConsole)
	}
	if sawMaClaw {
		return acceptanceTargetChoice{kind: acceptanceKindRefused, status: acceptanceStatusSkipped, reason: acceptanceReasonMaClaw}
	}
	return acceptanceTargetChoice{}
}

func chooseGUIAcceptanceTarget(project string, gui []acceptanceCandidate, brief *codingAcceptanceBrief, briefCand acceptanceCandidate, briefTrusted bool) (acceptanceTargetChoice, bool) {
	targets := gui
	if len(targets) == 0 && briefTrusted && briefCand.Subsystem == acceptanceImageSubsystemGUI {
		targets = []acceptanceCandidate{briefCand}
	}
	if len(targets) == 0 {
		return acceptanceTargetChoice{}, false
	}
	if len(targets) > 1 {
		if briefTrusted && briefCand.Subsystem == acceptanceImageSubsystemGUI {
			return driveChoice(briefCand, briefWorkDir(project, brief, briefCand.Path), acceptanceKindDesktop), true
		}
		return acceptanceTargetChoice{kind: acceptanceKindNone, status: acceptanceStatusSkipped, reason: acceptanceReasonMultiple, multiple: true}, true
	}
	target := targets[0]
	if briefTrusted && briefCand.Subsystem == acceptanceImageSubsystemGUI {
		target = briefCand
	}
	return driveChoice(target, briefWorkDir(project, brief, target.Path), acceptanceKindDesktop), true
}

func driveChoice(c acceptanceCandidate, workDir, kind string) acceptanceTargetChoice {
	if workDir == "" {
		workDir = filepath.Dir(c.Path)
	}
	if kind == "" {
		kind = acceptanceKindConsole
	}
	return acceptanceTargetChoice{
		drive:    true,
		kind:     kind,
		launch:   c.Path,
		workDir:  workDir,
		artifact: c.artifact,
	}
}

func briefWorkDir(project string, brief *codingAcceptanceBrief, exe string) string {
	if brief != nil {
		if dir, ok := resolveInsideProject(project, brief.WorkingDir, project); ok {
			return dir
		}
	}
	if exe == "" {
		return project
	}
	return filepath.Dir(exe)
}

func briefLaunchCandidate(project string, brief *codingAcceptanceBrief) (acceptanceCandidate, bool) {
	if brief == nil || strings.TrimSpace(brief.Launch) == "" || project == "" {
		return acceptanceCandidate{}, false
	}
	base := project
	if dir, ok := resolveInsideProject(project, brief.WorkingDir, project); ok {
		base = dir
	}
	path, ok := resolveInsideProject(project, brief.Launch, base)
	if !ok || acceptanceImageIsMaClaw(path) || isTestExecutableBase(path) {
		return acceptanceCandidate{}, false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return acceptanceCandidate{}, false
	}
	sub, ok := readPESubsystem(path)
	if !ok || (sub != acceptanceImageSubsystemCUI && sub != acceptanceImageSubsystemGUI) {
		return acceptanceCandidate{}, false
	}
	return acceptanceCandidate{
		Path:      path,
		Subsystem: sub,
		Size:      info.Size(),
		ModTime:   info.ModTime(),
		Origin:    "brief",
		artifact:  acceptanceArtifactKey(path, info),
	}, true
}

func acceptanceChecksFor(brief *codingAcceptanceBrief) []codingAcceptanceCheck {
	if brief == nil || brief.SecretLike || len(brief.Checks) == 0 {
		return defaultAcceptanceChecks()
	}
	return brief.Checks
}

// acceptanceWebChecks opens the listened origin. A missing brief does not
// borrow the console left/right/down script.
func acceptanceWebChecks(brief *codingAcceptanceBrief, listen acceptanceListen) []codingAcceptanceCheck {
	if brief == nil || brief.SecretLike || len(brief.Checks) == 0 {
		return []codingAcceptanceCheck{{ID: "open", URL: listen.URL, Expect: "navigate"}}
	}
	return brief.Checks
}

func defaultAcceptanceChecks() []codingAcceptanceCheck {
	return []codingAcceptanceCheck{
		{ID: "left", Keys: []string{"left"}, Expect: "left"},
		{ID: "right", Keys: []string{"right"}, Expect: "right"},
		{ID: "down", Keys: []string{"down"}, Expect: "down"},
	}
}

func runConsoleAcceptance(ctx context.Context, sess consoleAcceptanceSession, exe, dir string, checks []codingAcceptanceCheck) (CodingInteractiveAcceptance, consoleGrid) {
	acc := CodingInteractiveAcceptance{Kind: acceptanceKindConsole, Launch: exe, Status: acceptanceStatusFailed}
	if sess == nil {
		acc.Status = acceptanceStatusPlatformBlocked
		acc.Reason = acceptanceReasonTestNoDriver
		return acc, consoleGrid{}
	}
	var (
		pid  int
		kill func()
		grid consoleGrid
	)
	defer func() {
		if kill != nil {
			kill()
		}
		clearAcceptanceDriving()
	}()
	var lastProbe consoleHelperResult
	for attempt := 1; attempt <= 2; attempt++ {
		if ctx.Err() != nil {
			acc.Status = acceptanceStatusCancelled
			acc.Reason = acceptanceReasonCancelled
			acc.Attempts = attempt - 1
			return acc, grid
		}
		nextPID, exited, nextKill, err := sess.Start(ctx, exe, dir)
		acc.Attempts = attempt
		if err != nil || nextPID == 0 {
			acc.Status = acceptanceStatusPlatformBlocked
			acc.Reason = "CreateProcess 失败。没有向其他窗口发送按键。"
			if err != nil {
				acc.Reason = "CreateProcess 失败：" + err.Error() + "。没有向其他窗口发送按键。"
			}
			return acc, grid
		}
		if kill != nil {
			kill()
		}
		pid, kill = nextPID, nextKill
		setAcceptanceDriving(exe)
		if exited != nil && exited(acceptanceCLIExitProbe) {
			acc.Kind = acceptanceKindNone
			acc.Status = acceptanceStatusSkipped
			acc.Reason = "进程在 1.5 秒内退出，按命令行程序记录，没有写控制台输入。"
			log.Printf("[coding-acceptance] pid=%d exited within 1.5s", pid)
			return acc, grid
		}
		probe, probeErr := pollAcceptanceAttach(ctx, sess, pid)
		lastProbe = probe
		if probeErr == nil && probe.OK {
			break
		}
		if ctx.Err() != nil {
			acc.Status = acceptanceStatusCancelled
			acc.Reason = acceptanceReasonCancelled
			return acc, grid
		}
		log.Printf("[coding-acceptance] probe attempt=%d pid=%d reason=%s win32=%d hwnd=%d err=%v", attempt, pid, probe.Reason, probe.Win32, probe.ConsoleHWND, probeErr)
		if attempt == 2 {
			acc.Status = acceptanceStatusPlatformBlocked
			acc.Reason = acceptanceProbeFailureReason(probe)
			return acc, grid
		}
		if kill != nil {
			kill()
			kill = nil
		}
		clearAcceptanceDriving()
	}

	b2, err := sess.Read(ctx, pid)
	if err != nil {
		acc.Status = acceptanceStatusFailed
		acc.Reason = "读取控制台缓冲失败：" + err.Error()
		return acc, grid
	}
	if err := acceptanceSleep(ctx, acceptanceSampleInterval); err != nil {
		acc.Status = acceptanceStatusCancelled
		acc.Reason = acceptanceReasonCancelled
		return acc, b2
	}
	b3, err := sess.Read(ctx, pid)
	if err != nil {
		acc.Status = acceptanceStatusFailed
		acc.Reason = "读取控制台缓冲失败：" + err.Error()
		return acc, b2
	}
	grid = b3
	// B2 and B3 are the one no-key clock for every check. Each K is compared
	// with that same B3, not with the previous keyed frame. A visible-window
	// screenshot is optional and is not a pass signal; PR 1 does not capture one.
	acc.Checks = make([]CodingAcceptanceCheckResult, 0, len(checks))
	passed := true
	for _, check := range checks {
		if ctx.Err() != nil {
			acc.Status = acceptanceStatusCancelled
			acc.Reason = acceptanceReasonCancelled
			acc.Checks = append(acc.Checks, failedAcceptanceCheck(check, "cancelled"))
			return acc, grid
		}
		key := ""
		if len(check.Keys) == 1 {
			key = check.Keys[0]
		}
		result := CodingAcceptanceCheckResult{ID: check.ID, Key: key, Expect: check.Expect}
		if !acceptanceKeyAllowed(key) || check.Expect == "expect_rejected" {
			result.Detail = "expect_rejected"
			result.Passed = false
			acc.Checks = append(acc.Checks, result)
			passed = false
			break
		}
		if err := sess.Write(ctx, pid, key); err != nil {
			result.Detail = "write_failed"
			acc.Checks = append(acc.Checks, result)
			passed = false
			acc.Reason = "写入控制台输入缓冲失败：" + err.Error() + "。没有把键发给前台窗口。"
			break
		}
		if err := acceptanceSleep(ctx, acceptanceSampleInterval); err != nil {
			acc.Status = acceptanceStatusCancelled
			acc.Reason = acceptanceReasonCancelled
			result.Detail = "cancelled"
			acc.Checks = append(acc.Checks, result)
			return acc, grid
		}
		k, err := sess.Read(ctx, pid)
		if err != nil {
			result.Detail = "read_failed"
			acc.Checks = append(acc.Checks, result)
			passed = false
			acc.Reason = "读取控制台缓冲失败：" + err.Error()
			break
		}
		grid = k
		ok, detail := evaluateAcceptanceCheck(check.Expect, b2, b3, k)
		result.Passed = ok
		result.Detail = detail
		acc.Checks = append(acc.Checks, result)
		if !ok {
			passed = false
			acc.Reason = fmt.Sprintf("已用 WriteConsoleInput 向 PID %d 的控制台输入缓冲写入按键，画面没有按脚本方向变化；没有把键发给前台窗口。", pid)
			break
		}
	}
	if acc.Status == acceptanceStatusCancelled {
		return acc, grid
	}
	if passed {
		acc.Status = acceptanceStatusPassed
		acc.Reason = ""
	} else if acc.Status != acceptanceStatusPlatformBlocked {
		acc.Status = acceptanceStatusFailed
	}
	_ = lastProbe
	log.Printf("[coding-acceptance] pid=%d status=%s checks=%d", pid, acc.Status, len(acc.Checks))
	return acc, grid
}

func failedAcceptanceCheck(check codingAcceptanceCheck, detail string) CodingAcceptanceCheckResult {
	key := ""
	if len(check.Keys) == 1 {
		key = check.Keys[0]
	}
	return CodingAcceptanceCheckResult{ID: check.ID, Key: key, Expect: check.Expect, Detail: detail}
}

func acceptanceProbeFailureReason(probe consoleHelperResult) string {
	switch probe.Reason {
	case "attached_host_console":
		return fmt.Sprintf("已重试启动一次。AttachConsole 附到了 MaClaw 自己的控制台，错误 %d。没有向其他窗口发送按键。", probe.Win32)
	case "console_pid_mismatch":
		return fmt.Sprintf("已重试启动一次。控制台进程列表与目标进程不一致，错误 %d。没有向其他窗口发送按键。", probe.Win32)
	default:
		return fmt.Sprintf("AttachConsole(pid) 在 3 秒内失败，错误 %d，已重试启动一次。没有向其他窗口发送按键。", probe.Win32)
	}
}

func pollAcceptanceAttach(ctx context.Context, sess consoleAcceptanceSession, pid int) (consoleHelperResult, error) {
	deadline := acceptanceNow().Add(acceptanceAttachWindow)
	var last consoleHelperResult
	var lastErr error
	for {
		if ctx.Err() != nil {
			return last, ctx.Err()
		}
		last, lastErr = sess.Probe(ctx, pid)
		if lastErr == nil && last.OK {
			return last, nil
		}
		if last.Reason == "attached_host_console" || last.Reason == "console_pid_mismatch" {
			if lastErr == nil {
				lastErr = errors.New(last.Reason)
			}
			return last, lastErr
		}
		if !acceptanceNow().Before(deadline) {
			if lastErr == nil {
				if last.Reason == "" {
					last.Reason = "attach_console_failed"
				}
				lastErr = errors.New(last.Reason)
			}
			return last, lastErr
		}
		if err := acceptanceSleep(ctx, 50*time.Millisecond); err != nil {
			return last, err
		}
	}
}

func acceptanceSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	acceptanceWait(d)
	return ctx.Err()
}

func codingAcceptanceProgressEvent(task *TaskItem, launch string) CodingAgentEvent {
	event := newCodingAgentTaskEvent(codingAgentEventPhaseVerifyingUI, task, "", "")
	event.Event = codingAgentEventKindAssistantNote.String()
	detail := "正在验证界面"
	if strings.TrimSpace(launch) != "" {
		detail += "：" + launch
	}
	event.Detail = detail
	return event
}

func writeAcceptanceBuffer(project string, grid consoleGrid) {
	project = strings.TrimSpace(project)
	if project == "" {
		return
	}
	dir := filepath.Join(project, acceptanceOutputDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[coding-acceptance] buffer dir: %v", err)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "buffer.txt"), []byte(grid.truncatedText()), 0o644); err != nil {
		log.Printf("[coding-acceptance] buffer write: %v", err)
	}
}

func writeAcceptanceBufferFile(project, name, text string) {
	project = strings.TrimSpace(project)
	if project == "" || strings.TrimSpace(text) == "" {
		return
	}
	dir := filepath.Join(project, acceptanceOutputDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[coding-acceptance] note dir: %v", err)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
		log.Printf("[coding-acceptance] note write: %v", err)
	}
}

func (g consoleGrid) truncatedText() string {
	if g.text == "" {
		return ""
	}
	lines := strings.Split(g.text, "\n")
	if len(lines) > acceptanceGridRows {
		lines = lines[:acceptanceGridRows]
	}
	for i, line := range lines {
		runes := []rune(line)
		if len(runes) > acceptanceGridCols {
			lines[i] = string(runes[:acceptanceGridCols])
		}
	}
	return strings.Join(lines, "\n")
}

func buildCodingAcceptanceBriefToolDefinition() map[string]interface{} {
	return buildToolDef(
		codingAcceptanceBriefToolName,
		"记录本回合交互验收脚本，不会启动程序。宿主在回合结束后自行验收控制台程序。checks 最多 8 步，每步一个按键。key 只能是 left、right、up、down、space、enter、z、x、c。expect 只能是 left、right、down、rotate、hard_drop。不要在参数里写密码、验证码或 otp。",
		map[string]interface{}{
			"launch": map[string]interface{}{
				"type":        "string",
				"description": "可选。项目内要验收的可执行文件路径。",
			},
			"working_dir": map[string]interface{}{
				"type":        "string",
				"description": "可选。项目内工作目录。",
			},
			"kind": map[string]interface{}{
				"type":        "string",
				"description": "可选。宿主不信任这个字段。",
			},
			"checks": map[string]interface{}{
				"type":        "array",
				"description": "可选。省略时宿主使用 left、right、down。",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id":     map[string]interface{}{"type": "string"},
						"keys":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
						"expect": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
		nil,
	)
}

func (c *codingSubAgentCallbacks) executeSubmitInteractiveAcceptance(argsJSON string) codingToolExecutionResult {
	brief, text := parseAcceptanceBrief(argsJSON)
	if c != nil && !strings.HasPrefix(text, "ignored") {
		stored := brief
		c.acceptanceBrief = &stored
	}
	return codingToolExecutionResult{Text: text, Outcome: codingToolOutcomeSuccess}
}

func parseAcceptanceBrief(raw string) (codingAcceptanceBrief, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return codingAcceptanceBrief{}, "accepted"
	}
	if acceptanceTextHasSecret(raw) {
		return codingAcceptanceBrief{SecretLike: true, Checks: defaultAcceptanceChecks()}, "accepted: default keys"
	}
	var brief codingAcceptanceBrief
	if err := json.Unmarshal([]byte(raw), &brief); err != nil {
		return codingAcceptanceBrief{}, "ignored: invalid json"
	}
	if acceptanceBriefHasSecret(brief) {
		kept := codingAcceptanceBrief{SecretLike: true, Checks: defaultAcceptanceChecks()}
		if !acceptanceTextHasSecret(brief.Launch) {
			kept.Launch = brief.Launch
		}
		if !acceptanceTextHasSecret(brief.WorkingDir) {
			kept.WorkingDir = brief.WorkingDir
		}
		return kept, "accepted: default keys"
	}
	normalized, rejectedNote := normalizeAcceptanceChecks(brief.Checks)
	brief.Checks = normalized
	if rejectedNote != "" && len(normalized) == 0 {
		return brief, "ignored: " + rejectedNote
	}
	return brief, "accepted"
}

func normalizeAcceptanceChecks(in []codingAcceptanceCheck) ([]codingAcceptanceCheck, string) {
	if len(in) > acceptanceMaxChecks {
		in = in[:acceptanceMaxChecks]
	}
	out := make([]codingAcceptanceCheck, 0, len(in))
	for i, check := range in {
		check.ID = strings.TrimSpace(check.ID)
		if check.ID == "" {
			check.ID = fmt.Sprintf("check-%d", i+1)
		}
		check.Expect = strings.ToLower(strings.TrimSpace(check.Expect))
		check.URL = strings.TrimSpace(check.URL)
		// One coordinate is not a click. A click mixed with keys is not a click
		// either: the runner must not press a rejected key by hitting a point.
		partialClick := (check.ClickX != nil) != (check.ClickY != nil)
		mixedClick := check.ClickX != nil && check.ClickY != nil && len(check.Keys) > 0
		if partialClick || mixedClick {
			check.Expect = "expect_rejected"
			check.Keys = nil
			check.ClickX = nil
			check.ClickY = nil
			out = append(out, check)
			continue
		}
		if check.ClickX != nil && check.ClickY != nil && len(check.Keys) == 0 {
			if check.Expect == "" || check.Expect == "click" {
				check.Expect = "click"
			}
			out = append(out, check)
			continue
		}
		if check.URL != "" && len(check.Keys) == 0 && check.ClickX == nil {
			if check.Expect == "" {
				check.Expect = "navigate"
			}
			out = append(out, check)
			continue
		}
		if len(check.Keys) != 1 {
			check.Expect = "expect_rejected"
			check.Keys = nil
			out = append(out, check)
			continue
		}
		key := strings.ToLower(strings.TrimSpace(check.Keys[0]))
		check.Keys = []string{key}
		if !acceptanceKeyAllowed(key) {
			check.Expect = "expect_rejected"
			out = append(out, check)
			continue
		}
		if check.Expect == "buffer_changed" && (key == "left" || key == "right" || key == "down") {
			check.Expect = key
		}
		if !acceptanceExpectAllowed(check.Expect) {
			check.Expect = "expect_rejected"
		}
		out = append(out, check)
	}
	return out, ""
}

func acceptanceKeyAllowed(key string) bool {
	switch key {
	case "left", "right", "up", "down", "space", "enter", "z", "x", "c":
		return true
	default:
		return false
	}
}

func acceptanceExpectAllowed(expect string) bool {
	switch expect {
	case "left", "right", "down", "rotate", "hard_drop":
		return true
	default:
		return false
	}
}

func acceptanceTextHasSecret(text string) bool {
	if strings.Contains(text, "密码") || strings.Contains(text, "验证码") || strings.Contains(text, "驗證碼") {
		return true
	}
	return acceptanceSecretWord.MatchString(text)
}

var acceptanceSecretWord = regexp.MustCompile(`(?i)(^|[^a-z])(password|otp)([^a-z]|$)`)

// acceptanceLoginWord matches a sign-in control. "login" and "sign-in" count.
// "signing" does not. The brief detector does not use this pattern.
var acceptanceLoginWord = regexp.MustCompile(`(?i)(^|[^a-z0-9])(log[\s-]*in|sign[\s-]*in)([^a-z0-9]|$)`)

func acceptanceBriefHasSecret(brief codingAcceptanceBrief) bool {
	if acceptanceTextHasSecret(brief.Launch) || acceptanceTextHasSecret(brief.WorkingDir) || acceptanceTextHasSecret(brief.Kind) {
		return true
	}
	for _, check := range brief.Checks {
		if acceptanceTextHasSecret(check.ID) || acceptanceTextHasSecret(check.Expect) {
			return true
		}
		for _, key := range check.Keys {
			if acceptanceTextHasSecret(key) {
				return true
			}
		}
	}
	return false
}

func evaluateAcceptanceCheck(expect string, b2, b3, k consoleGrid) (bool, string) {
	expect = strings.ToLower(strings.TrimSpace(expect))
	if expect == "expect_rejected" || !acceptanceExpectAllowed(expect) {
		return false, "expect_rejected"
	}
	switch expect {
	case "left", "right", "down":
		keyed, keyDetail := directionHolds(expect, b3, k)
		clock, _ := directionHolds(expect, b2, b3)
		if !keyed {
			return false, keyDetail
		}
		if clock {
			return false, keyDetail + "，时钟已有同样位移"
		}
		return true, keyDetail + "，时钟未先发生"
	case "rotate":
		appC, vanC := consoleDiff(b2, b3)
		clockEmpty := len(appC) == 0 && len(vanC) == 0
		clockTranslation := isCellTranslation(appC, vanC)
		if !clockEmpty && !clockTranslation {
			return false, "clock_ambiguous"
		}
		appK, vanK := consoleDiff(b3, k)
		if len(appK) == 0 || len(vanK) == 0 {
			if len(appK) == 0 && len(vanK) == 0 {
				return false, "input_ignored"
			}
			return false, "rotate 一侧为空"
		}
		if isCellTranslation(appK, vanK) {
			return false, "keyed_translation"
		}
		return true, "形状变化不是平移"
	case "hard_drop":
		jumpK := rowJump(b3, k)
		jumpC := rowJump(b2, b3)
		detail := fmt.Sprintf("行跳 %.1f，时钟 %.1f", jumpK, jumpC)
		if jumpK >= jumpC+2 {
			return true, detail
		}
		return false, detail
	default:
		return false, "expect_rejected"
	}
}

func directionHolds(expect string, prev, next consoleGrid) (bool, string) {
	app, van := consoleDiff(prev, next)
	if len(app) == 0 && len(van) == 0 {
		return false, "input_ignored"
	}
	if len(app) == 0 || len(van) == 0 {
		return false, "一侧为空"
	}
	ac, ar, aok := cellCentroid(app)
	vc, vr, vok := cellCentroid(van)
	if !aok || !vok {
		return false, "一侧为空"
	}
	switch expect {
	case "left":
		return ac <= vc-1, fmt.Sprintf("列重心差 %.1f", ac-vc)
	case "right":
		return ac >= vc+1, fmt.Sprintf("列重心差 %.1f", ac-vc)
	case "down":
		return ar >= vr+1, fmt.Sprintf("行重心差 %.1f", ar-vr)
	default:
		return false, "expect_rejected"
	}
}

func rowJump(prev, next consoleGrid) float64 {
	app, van := consoleDiff(prev, next)
	if len(app) == 0 || len(van) == 0 {
		return 0
	}
	_, ar, aok := cellCentroid(app)
	_, vr, vok := cellCentroid(van)
	if !aok || !vok {
		return 0
	}
	return ar - vr
}

func consoleDiff(prev, next consoleGrid) (appeared, vanished []consoleCell) {
	if prev.cells == nil {
		prev.cells = map[[2]int]struct{}{}
	}
	if next.cells == nil {
		next.cells = map[[2]int]struct{}{}
	}
	for pos := range next.cells {
		if _, ok := prev.cells[pos]; !ok {
			appeared = append(appeared, consoleCell{col: pos[0], row: pos[1]})
		}
	}
	for pos := range prev.cells {
		if _, ok := next.cells[pos]; !ok {
			vanished = append(vanished, consoleCell{col: pos[0], row: pos[1]})
		}
	}
	sortCells(appeared)
	sortCells(vanished)
	return appeared, vanished
}

func sortCells(cells []consoleCell) {
	sort.Slice(cells, func(i, j int) bool {
		if cells[i].row != cells[j].row {
			return cells[i].row < cells[j].row
		}
		return cells[i].col < cells[j].col
	})
}

func cellCentroid(cells []consoleCell) (col, row float64, ok bool) {
	if len(cells) == 0 {
		return 0, 0, false
	}
	var sc, sr float64
	for _, c := range cells {
		sc += float64(c.col)
		sr += float64(c.row)
	}
	n := float64(len(cells))
	return sc / n, sr / n, true
}

func isCellTranslation(appeared, vanished []consoleCell) bool {
	if len(appeared) == 0 || len(appeared) != len(vanished) {
		return false
	}
	appeared = append([]consoleCell(nil), appeared...)
	vanished = append([]consoleCell(nil), vanished...)
	sortCells(appeared)
	sortCells(vanished)
	dc := appeared[0].col - vanished[0].col
	dr := appeared[0].row - vanished[0].row
	if dc == 0 && dr == 0 {
		return false
	}
	vanishedSet := make(map[[2]int]struct{}, len(vanished))
	for _, c := range vanished {
		vanishedSet[[2]int{c.col, c.row}] = struct{}{}
	}
	for _, c := range appeared {
		if _, ok := vanishedSet[[2]int{c.col - dc, c.row - dr}]; !ok {
			return false
		}
	}
	return true
}

func gridFromRows(rows []string) consoleGrid {
	g := consoleGrid{rows: len(rows), cells: map[[2]int]struct{}{}, text: strings.Join(rows, "\n")}
	for r, row := range rows {
		runes := []rune(row)
		if len(runes) > g.cols {
			g.cols = len(runes)
		}
		for c, ch := range runes {
			if acceptanceCellBlank(ch) {
				continue
			}
			g.cells[[2]int{c, r}] = struct{}{}
		}
	}
	return g
}

func gridFromText(text string) consoleGrid {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return consoleGrid{cells: map[[2]int]struct{}{}}
	}
	return gridFromRows(strings.Split(text, "\n"))
}

func acceptanceCellBlank(ch rune) bool {
	return ch == ' ' || ch == '\t' || ch == 0
}

func acceptanceExcusePresent(summary string) bool {
	return strings.Contains(summary, "需人工运行") ||
		strings.Contains(summary, "无法注入按键") ||
		strings.Contains(summary, "本环境无法") ||
		strings.Contains(summary, "交互式终端")
}

func stripAcceptanceExcuseClauses(summary string) string {
	summary = strings.ReplaceAll(summary, "（本环境无法注入按键）", "")
	summary = strings.ReplaceAll(summary, "(本环境无法注入按键)", "")
	summary = strings.ReplaceAll(summary, "本环境无法注入按键", "")
	summary = strings.ReplaceAll(summary, "无法注入按键", "")
	for _, marker := range []string{"交互式终端", "需人工运行"} {
		for {
			i := strings.Index(summary, marker)
			if i < 0 {
				break
			}
			start := i
			if start > 0 {
				prev, size := utf8.DecodeLastRuneInString(summary[:start])
				if prev == '；' || prev == ';' || prev == '，' || prev == ',' || prev == ' ' {
					start -= size
				}
			}
			rest := summary[i:]
			endRel := strings.IndexAny(rest, "。\n")
			if endRel < 0 {
				summary = strings.TrimSpace(summary[:start])
				break
			}
			summary = strings.TrimSpace(summary[:start]) + summary[i+endRel:]
		}
	}
	return strings.TrimSpace(summary)
}

func rewriteFailedPassClaims(summary string) string {
	summary = strings.ReplaceAll(summary, "端到端边界全部验证通过", "逻辑测试已通过，交互验证未通过")
	summary = strings.ReplaceAll(summary, "全部验证通过", "逻辑测试已通过，交互验证未通过")
	return summary
}

func spliceAcceptanceSummary(summary string, acc CodingInteractiveAcceptance) string {
	summary = strings.TrimSpace(summary)
	switch acc.Status {
	case acceptanceStatusPassed:
		summary = stripAcceptanceExcuseClauses(summary)
		return joinAcceptanceSummary(summary, acceptanceEvidenceText(acc))
	case acceptanceStatusFailed:
		summary = rewriteFailedPassClaims(summary)
		summary = stripAcceptanceExcuseClauses(summary)
		return joinAcceptanceSummary(summary, acceptanceEvidenceText(acc))
	case acceptanceStatusCancelled:
		summary = stripAcceptanceExcuseClauses(summary)
		return joinAcceptanceSummary(summary, acceptanceReasonCancelled)
	case acceptanceStatusPlatformBlocked:
		summary = strings.ReplaceAll(summary, "（本环境无法注入按键）", "")
		summary = strings.ReplaceAll(summary, "本环境无法注入按键", "")
		summary = strings.ReplaceAll(summary, "无法注入按键", "")
		return joinAcceptanceSummary(strings.TrimSpace(summary), acc.Reason)
	case acceptanceStatusSkipped:
		summary = stripAcceptanceExcuseClauses(summary)
		return joinAcceptanceSummary(summary, acc.Reason)
	default:
		if acc.Reason == "" {
			return summary
		}
		summary = stripAcceptanceExcuseClauses(summary)
		return joinAcceptanceSummary(summary, acc.Reason)
	}
}

func acceptanceEvidenceText(acc CodingInteractiveAcceptance) string {
	var b strings.Builder
	if acc.Status == acceptanceStatusPassed {
		b.WriteString("交互验证已通过。")
	} else if acc.Status == acceptanceStatusFailed {
		b.WriteString("交互验证未通过。")
	}
	for _, check := range acc.Checks {
		fmt.Fprintf(&b, "\n- %s 键 %s 期望 %s：%s", check.ID, check.Key, check.Expect, check.Detail)
	}
	if acc.Status == acceptanceStatusFailed && strings.TrimSpace(acc.Reason) != "" {
		b.WriteString("\n")
		b.WriteString(acc.Reason)
	}
	return strings.TrimSpace(b.String())
}

func joinAcceptanceSummary(summary, extra string) string {
	summary = strings.TrimSpace(summary)
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return summary
	}
	if summary == "" {
		return extra
	}
	if strings.Contains(summary, extra) {
		return summary
	}
	return summary + "\n\n" + extra
}

func spliceRemoteInteractiveAcceptance(summary string) string {
	if !acceptanceExcusePresent(summary) {
		return summary
	}
	summary = stripAcceptanceExcuseClauses(summary)
	return joinAcceptanceSummary(summary, acceptanceReasonRemoteSentence)
}

type acceptanceCandidate struct {
	Path      string
	Subsystem uint16
	Size      int64
	ModTime   time.Time
	Origin    string
	artifact  string
}

func findAcceptanceCandidates(project string, files []string, cmds []CodingSubAgentCommandResult, turnStart time.Time) []acceptanceCandidate {
	project = strings.TrimSpace(project)
	if project == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []acceptanceCandidate
	add := func(path, origin string, requireMtime bool) {
		if path == "" || isTestExecutableBase(path) {
			return
		}
		key := acceptancePathKey(path)
		if seen[key] {
			return
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return
		}
		if requireMtime && !turnStart.IsZero() && info.ModTime().Before(turnStart.Add(-2*time.Second)) {
			return
		}
		sub, ok := readPESubsystem(path)
		if !ok {
			return
		}
		if sub != acceptanceImageSubsystemCUI && sub != acceptanceImageSubsystemGUI {
			return
		}
		seen[key] = true
		out = append(out, acceptanceCandidate{
			Path:      path,
			Subsystem: sub,
			Size:      info.Size(),
			ModTime:   info.ModTime(),
			Origin:    origin,
			artifact:  acceptanceArtifactKey(path, info),
		})
	}
	for _, file := range files {
		if !strings.EqualFold(filepath.Ext(file), ".exe") {
			continue
		}
		if path, ok := resolveInsideProject(project, file, project); ok {
			add(path, "created", false)
		}
	}
	var workDirs []string
	buildLike := false
	for _, cmd := range cmds {
		if commandLooksLikeBuild(cmd.Command) {
			buildLike = true
		}
		work := cmd.WorkingDir
		if abs, ok := resolveInsideProject(project, work, project); ok {
			work = abs
		} else if strings.TrimSpace(work) == "" {
			work = project
		}
		if pathWithin(project, work) {
			workDirs = append(workDirs, work)
		}
		for _, token := range acceptanceExeTokens(cmd.Command) {
			if path, ok := resolveInsideProject(project, token, work); ok {
				add(path, "command", false)
			}
		}
	}
	// A zero turn start must not treat every old executable as new. Command
	// lines and files created this turn are still eligible without an mtime.
	if buildLike && !turnStart.IsZero() {
		scanAcceptanceBuildOutputs(project, workDirs, turnStart, seen, &out)
	}
	return out
}

func scanAcceptanceBuildOutputs(project string, workDirs []string, turnStart time.Time, seen map[string]bool, out *[]acceptanceCandidate) {
	budget := &acceptanceScanBudget{}
	var named []string
	discoverAcceptanceNamedDirs(project, project, 0, 3, &named, budget)
	type root struct {
		path  string
		depth int
	}
	roots := []root{{project, 2}}
	for _, wd := range workDirs {
		if pathWithin(project, wd) {
			roots = append(roots, root{wd, 2})
		}
	}
	for _, dir := range named {
		roots = append(roots, root{dir, 3})
	}
	for _, r := range roots {
		collectAcceptanceEXEs(project, r.path, 0, r.depth, turnStart, seen, out, budget)
	}
}

type acceptanceScanBudget struct {
	dirs int
	exes int
}

func discoverAcceptanceNamedDirs(project, dir string, depth, maxDepth int, out *[]string, budget *acceptanceScanBudget) {
	if dir == "" || depth > maxDepth || budget.dirs >= acceptanceScanDirCap {
		return
	}
	if !pathWithin(project, dir) && acceptancePathKey(dir) != acceptancePathKey(project) {
		return
	}
	budget.dirs++
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || acceptanceSkipDir(e.Name()) {
			continue
		}
		next := filepath.Join(dir, e.Name())
		if acceptanceNamedOutputDir(e.Name()) {
			*out = append(*out, next)
		}
		if depth < maxDepth {
			discoverAcceptanceNamedDirs(project, next, depth+1, maxDepth, out, budget)
		}
	}
}

func collectAcceptanceEXEs(project, dir string, depth, maxDepth int, turnStart time.Time, seen map[string]bool, out *[]acceptanceCandidate, budget *acceptanceScanBudget) {
	if dir == "" || depth > maxDepth || budget.dirs >= acceptanceScanDirCap || budget.exes >= acceptanceScanExeCap {
		return
	}
	if !pathWithin(project, dir) && acceptancePathKey(dir) != acceptancePathKey(project) {
		return
	}
	budget.dirs++
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		next := filepath.Join(dir, name)
		if e.IsDir() {
			if acceptanceSkipDir(name) {
				continue
			}
			if depth < maxDepth {
				collectAcceptanceEXEs(project, next, depth+1, maxDepth, turnStart, seen, out, budget)
			}
			continue
		}
		if budget.exes >= acceptanceScanExeCap || !strings.EqualFold(filepath.Ext(name), ".exe") || isTestExecutableBase(name) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if !turnStart.IsZero() && info.ModTime().Before(turnStart.Add(-2*time.Second)) {
			continue
		}
		key := acceptancePathKey(next)
		if seen[key] {
			continue
		}
		sub, ok := readPESubsystem(next)
		if !ok || (sub != acceptanceImageSubsystemCUI && sub != acceptanceImageSubsystemGUI) {
			continue
		}
		seen[key] = true
		budget.exes++
		*out = append(*out, acceptanceCandidate{
			Path:      next,
			Subsystem: sub,
			Size:      info.Size(),
			ModTime:   info.ModTime(),
			Origin:    "scan",
			artifact:  acceptanceArtifactKey(next, info),
		})
	}
}

func acceptanceSkipDir(name string) bool {
	switch strings.ToLower(name) {
	case ".git", "node_modules", "vendor", ".cache", "dist", "third_party", acceptanceOutputDirName:
		return true
	default:
		return false
	}
}

func acceptanceNamedOutputDir(name string) bool {
	switch strings.ToLower(name) {
	case "build", "out", "bin", "debug", "release":
		return true
	default:
		return false
	}
}

func commandLooksLikeBuild(cmd string) bool {
	low := strings.ToLower(cmd)
	for _, needle := range []string{"g++", "gcc", "clang", "cmake", "mingw", "cargo", "go build"} {
		if strings.Contains(low, needle) {
			return true
		}
	}
	for _, field := range strings.Fields(low) {
		base := strings.Trim(filepath.Base(strings.Trim(field, `"'`)), `"'`)
		if base == "cl" || base == "cl.exe" {
			return true
		}
	}
	return false
}

var acceptanceExeToken = regexp.MustCompile(`(?i)"([^"]+\.exe)"|'([^']+\.exe)'|(?:^|[\s=:])((?:[A-Za-z]:[\\/])?(?:[^\\/:*?"<>|\s]+[\\/])*[^\\/:*?"<>|\r\n]+\.exe)`)

func acceptanceExeTokens(command string) []string {
	var out []string
	for _, match := range acceptanceExeToken.FindAllStringSubmatch(command, -1) {
		for _, part := range match[1:] {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func isTestExecutableBase(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	if !strings.HasSuffix(base, ".exe") {
		return false
	}
	stem := strings.TrimSuffix(base, ".exe")
	return strings.HasPrefix(stem, "test_") || strings.HasSuffix(stem, "_test") || strings.HasSuffix(stem, ".test")
}

func acceptanceImageIsMaClaw(path string) bool {
	path = filepath.Clean(path)
	exe, err := os.Executable()
	if err != nil || path == "" {
		return false
	}
	exe = filepath.Clean(exe)
	if acceptancePathKey(path) == acceptancePathKey(exe) {
		return true
	}
	dir := filepath.Dir(exe)
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func acceptanceArtifactKey(path string, info os.FileInfo) string {
	if info == nil {
		return path
	}
	return path + "|" + strconv.FormatInt(info.Size(), 10) + "|" + strconv.FormatInt(info.ModTime().UnixNano(), 10)
}

func acceptancePathKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

func pathWithin(root, path string) bool {
	root = strings.TrimSpace(root)
	path = strings.TrimSpace(path)
	if root == "" || path == "" {
		return false
	}
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func resolveInsideProject(project, path, base string) (string, bool) {
	path = strings.Trim(strings.TrimSpace(path), `"'`)
	if path == "" || strings.TrimSpace(project) == "" {
		return "", false
	}
	if !filepath.IsAbs(path) {
		if strings.TrimSpace(base) == "" {
			base = project
		}
		path = filepath.Join(base, path)
	}
	path = filepath.Clean(path)
	if !pathWithin(project, path) && acceptancePathKey(path) != acceptancePathKey(project) {
		return "", false
	}
	return path, true
}

func readPESubsystem(path string) (uint16, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	var head [0x40]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return 0, false
	}
	if head[0] != 'M' || head[1] != 'Z' {
		return 0, false
	}
	lfanew := int(binary.LittleEndian.Uint32(head[0x3C:]))
	if lfanew <= 0 || lfanew > 1<<20 {
		return 0, false
	}
	// Optional-header subsystem sits 68 bytes into the optional header,
	// which itself starts 24 bytes after e_lfanew. Real images often place
	// that header far past the first 4 KiB.
	need := lfanew + 4 + 20 + 70
	if need < 0x40 {
		need = 0x40
	}
	buf := make([]byte, need)
	copy(buf, head[:])
	if need > 0x40 {
		if _, err := io.ReadFull(f, buf[0x40:]); err != nil {
			return 0, false
		}
	}
	return peSubsystem(buf)
}

func peSubsystem(b []byte) (uint16, bool) {
	if len(b) < 0x40 || b[0] != 'M' || b[1] != 'Z' {
		return 0, false
	}
	lfanew := int(binary.LittleEndian.Uint32(b[0x3C:]))
	if lfanew <= 0 || lfanew > 1<<20 {
		return 0, false
	}
	if len(b) < lfanew+4+20+2 {
		return 0, false
	}
	if string(b[lfanew:lfanew+4]) != "PE\x00\x00" {
		return 0, false
	}
	coff := lfanew + 4
	sizeOpt := int(binary.LittleEndian.Uint16(b[coff+16:]))
	if sizeOpt < 70 {
		return 0, false
	}
	opt := coff + 20
	if len(b) < opt+70 {
		return 0, false
	}
	magic := binary.LittleEndian.Uint16(b[opt:])
	if magic != 0x10b && magic != 0x20b {
		return 0, false
	}
	return binary.LittleEndian.Uint16(b[opt+68:]), true
}

func parseConsoleBufferArgs(args []string) (pid, host int, op, key string, err error) {
	// args is os.Args: program, console-buffer, flags, operation.
	if len(args) < 2 || args[1] != "console-buffer" {
		return 0, 0, "", "", errors.New("not console-buffer")
	}
	for i := 2; i < len(args); i++ {
		switch args[i] {
		case "--pid":
			if i+1 >= len(args) {
				return 0, 0, "", "", errors.New("missing --pid")
			}
			i++
			pid, err = strconv.Atoi(args[i])
			if err != nil {
				return 0, 0, "", "", err
			}
		case "--host":
			if i+1 >= len(args) {
				return 0, 0, "", "", errors.New("missing --host")
			}
			i++
			host, err = strconv.Atoi(args[i])
			if err != nil {
				return 0, 0, "", "", err
			}
		default:
			if op == "" {
				op = strings.ToLower(args[i])
				continue
			}
			if op == "write" && key == "" {
				key = strings.ToLower(args[i])
				continue
			}
			return 0, 0, "", "", errors.New("unexpected argument")
		}
	}
	if pid <= 0 || host <= 0 {
		return 0, 0, "", "", errors.New("pid and host are required")
	}
	switch op {
	case "probe", "read":
		return pid, host, op, "", nil
	case "write":
		if !acceptanceKeyAllowed(key) {
			return 0, 0, "", "", errors.New("key rejected")
		}
		return pid, host, op, key, nil
	default:
		return 0, 0, "", "", errors.New("operation must be probe, read, or write")
	}
}

func init() {
	codingStaticCompatibilityInventory = append(codingStaticCompatibilityInventory, codingStaticAcceptanceBriefItem())
}

// codingStaticAcceptanceBriefItem is the local inventory row for the
// record-only brief. Read-only so an uncorrelated surface can still show it.
// There is no remote row: a remote model must not submit a brief this host would run.
func codingStaticAcceptanceBriefItem() codingStaticCompatibilityInventoryItem {
	return codingStaticCompatibilityInventoryItem{
		HostKind:     codingStaticCompatibilityHostLocal,
		Name:         codingAcceptanceBriefToolName,
		Capability:   tool.CapabilityID("coding.interactive_acceptance_brief"),
		Effect:       tool.EffectReadOnly,
		BindingScope: "host",
		ControlPlane: false,
	}
}
