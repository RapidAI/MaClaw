package guiapp

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	guiBugReportScreenshotName = "screen_shot.png"
	guiBugReportNotesName      = "bugreports.txt"
	guiBugReportNoteLimit      = 32 << 10
	// Same ceiling as the diagnostics ZIP. The PNG is already in memory
	// from the window capture; a tighter cap rejects a shot the upload
	// would still accept.
	guiBugReportScreenshotMaxBytes = 100 << 20
	// NativeScreenshotRect refuses a wider or taller rectangle. A pixel
	// product below a real desktop (8K is about 33M) rejects that capture
	// after it has already been encoded.
	guiBugReportScreenshotMaxEdge = 16384
)

// guiBugReportRect is a screen rectangle in pixels. Left/Top can be negative
// on a multi-monitor desktop or on a maximized window whose resize border
// hangs off the monitor.
type guiBugReportRect struct {
	Left, Top, Right, Bottom int32
}

// guiBugReportCaptureRect chooses the pixels to copy. GetWindowRect includes
// the invisible resize border and the drop shadow, and BitBlt paints that
// margin black. The visible frame (DWM extended frame bounds) is used when
// it lies inside the window and is smaller on at least one edge. A restored
// window is often flush on the top and inset on the other sides; requiring
// every edge to be strictly inset would keep that margin. A failed, empty,
// equal, or sticking-out visible frame keeps the window rectangle. An empty
// window rectangle is not a capture.
func guiBugReportCaptureRect(window, visible guiBugReportRect, visibleOK bool) (guiBugReportRect, bool) {
	if window.Right <= window.Left || window.Bottom <= window.Top {
		return guiBugReportRect{}, false
	}
	if visibleOK && guiBugReportRectInside(visible, window) {
		return visible, true
	}
	return window, true
}

func guiBugReportRectInside(inner, outer guiBugReportRect) bool {
	if inner.Right <= inner.Left || inner.Bottom <= inner.Top {
		return false
	}
	if inner.Left < outer.Left || inner.Top < outer.Top || inner.Right > outer.Right || inner.Bottom > outer.Bottom {
		return false
	}
	return inner.Left > outer.Left || inner.Top > outer.Top || inner.Right < outer.Right || inner.Bottom < outer.Bottom
}

// bugReportRootFile is an extra file written at the diagnostics ZIP root.
// The About-page report leaves this empty. The header bug button adds the
// main-window screenshot and the typed description so an agent can open the
// same package and see both without a separate attachment download.
type bugReportRootFile struct {
	name string
	data []byte
}

// diagnosticsDayStampFile records that today's first GUI start already ran the
// diagnostic reset. It lives beside the logs and trajectories directories so
// the clear itself cannot delete the record of today's run.
const diagnosticsDayStampFile = "diagnostics-day.txt"

// diagnosticsDayStampText is the stamp body. The "reset" token keeps an older
// clear-only stamp ("2006-01-02") from counting, so the first launch after
// this change still resets even when today's logs were already cleared.
func diagnosticsDayStampText(day string) string {
	return day + " reset"
}

// maybeClearDiagnosticsForNewDay runs the About problem-report collection
// reset on the first GUI start of the local day. It reports whether that
// reset ran, so the caller can keep using the switches it just forced on.
// The file companion and a same-day restart leave both the directories and
// the switches alone. The bug-report session switch itself is not changed.
// An already-open session keeps a restore point from when it started, often
// with both switches off. That point is moved to on, so closing the session
// later the same day cannot undo today's collection. A closed session's
// restore point is left as it is.
func (a *App) maybeClearDiagnosticsForNewDay(now time.Time) bool {
	if a == nil || a.isFileCompanionProcess() {
		return false
	}
	today := now.In(time.Local).Format("2006-01-02")
	base := a.getMaclawBaseDir()
	path := filepath.Join(base, diagnosticsDayStampFile)
	if data, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(data)) == diagnosticsDayStampText(today) {
		return false
	}
	if !a.resetDiagnosticsForDailyOpen() {
		return false
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		log.Printf("[bug-report] daily diagnostics stamp: %v", err)
		return true
	}
	if err := os.WriteFile(path, []byte(diagnosticsDayStampText(today)+"\n"), 0o644); err != nil {
		log.Printf("[bug-report] daily diagnostics stamp: %v", err)
	}
	return true
}

// resetDiagnosticsForDailyOpen forces the two collection switches on, then
// empties logs and trajectories. The gate is turned on before the clear so
// the fresh log records the clear itself, not only the always-kept lines.
// A failed enable leaves the directories and the day stamp alone so the
// next launch retries without having discarded the previous log.
func (a *App) resetDiagnosticsForDailyOpen() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		log.Printf("[bug-report] daily reset: load config: %v", err)
		return false
	}
	patch := map[string]interface{}{}
	if !cfg.LogDetailEnabled || !cfg.LLMTrajectoryLogging {
		patch["log_detail_enabled"] = true
		patch["llm_trajectory_logging"] = true
	}
	// Closing an open session writes these two fields back onto the live
	// switches. They have to follow today's on state, or that close turns
	// collection off and this day's stamp will not turn it on again.
	if cfg.BugReportEnabled && (!cfg.BugReportPreviousLogDetail || !cfg.BugReportPreviousTrajectory) {
		patch["bug_report_previous_log_detail"] = true
		patch["bug_report_previous_trajectory"] = true
	}
	if len(patch) > 0 {
		if _, err := a.PatchConfigFields(patch); err != nil {
			log.Printf("[bug-report] daily reset: enable switches: %v", err)
			return false
		}
	}
	corelib.SetLogDetailEnabled(true)
	a.clearBugReportDiagnostics()
	log.Printf("[bug-report] daily reset: log detail and LLM trajectory on")
	return true
}

// CaptureGUIBugReportScreenshot stores a PNG of the main window before the
// description dialog opens, so the dialog itself is not in the picture.
// The returned token identifies that capture. The dialog passes the same
// token to DiscardGUIBugReportScreenshot when the user cancels.
func (a *App) CaptureGUIBugReportScreenshot() (string, error) {
	if a == nil {
		return "", fmt.Errorf("application is not ready")
	}
	pngBytes, err := captureBugReportWindowPNG()
	if err != nil {
		return "", err
	}
	if err := validateGUIBugReportScreenshot(pngBytes); err != nil {
		return "", err
	}
	path := filepath.Join(a.GetTempDir(), fmt.Sprintf("gui-screen-shot-%d.png", time.Now().UnixNano()))
	if err := os.WriteFile(path, pngBytes, 0o600); err != nil {
		return "", err
	}
	return a.storeGUIBugReportShot(path), nil
}

// storeGUIBugReportShot publishes path as the capture the open dialog owns.
// The token is the path. A replacement removes the previous file itself.
// A discard of the previous token then finds a different path stored and
// leaves the new file in place. Wails runs each binding on its own
// goroutine, so the close of one dialog can run after the next capture has
// already been stored.
func (a *App) storeGUIBugReportShot(path string) string {
	if a == nil {
		return ""
	}
	a.guiBugReportShotMu.Lock()
	previous := a.guiBugReportShot
	a.guiBugReportShot = path
	a.guiBugReportShotMu.Unlock()
	if previous != "" && previous != path {
		_ = os.Remove(previous)
	}
	return path
}

// DiscardGUIBugReportScreenshot drops a capture the user did not send.
// token is the value CaptureGUIBugReportScreenshot returned for that dialog.
// A token that is no longer the stored capture belongs to a dialog that has
// already been replaced, and this call does not touch the replacement.
func (a *App) DiscardGUIBugReportScreenshot(token string) {
	if a == nil {
		return
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	a.guiBugReportShotMu.Lock()
	if a.guiBugReportShot != token {
		a.guiBugReportShotMu.Unlock()
		return
	}
	a.guiBugReportShot = ""
	a.guiBugReportShotMu.Unlock()
	_ = os.Remove(token)
}

// SubmitGUIBugReport posts the same HubCenter diagnostics package as
// SubmitBugReport, and adds screen_shot.png plus bugreports.txt at the ZIP root.
func (a *App) SubmitGUIBugReport(description string) (map[string]interface{}, error) {
	if a == nil {
		return nil, fmt.Errorf("application is not ready")
	}
	description = strings.TrimSpace(description)
	if description == "" {
		return nil, fmt.Errorf("problem description is required")
	}
	if len(description) > guiBugReportNoteLimit {
		return nil, fmt.Errorf("problem description is too long")
	}
	a.guiBugReportShotMu.Lock()
	shot := a.guiBugReportShot
	a.guiBugReportShot = ""
	a.guiBugReportShotMu.Unlock()
	if strings.TrimSpace(shot) == "" {
		return nil, fmt.Errorf("main window screenshot is missing; open the report again")
	}
	pngBytes, err := os.ReadFile(shot)
	if err != nil {
		_ = os.Remove(shot)
		return nil, err
	}
	if err := validateGUIBugReportScreenshot(pngBytes); err != nil {
		_ = os.Remove(shot)
		return nil, err
	}
	osVersion := strings.TrimSpace(a.getOSVersion())
	if osVersion == "" || strings.EqualFold(osVersion, "Unknown") {
		osVersion = runtime.GOOS
	}
	notes := []byte(description)
	if !bytes.HasSuffix(notes, []byte("\n")) {
		notes = append(notes, '\n')
	}
	result, err := a.submitBugReport(osVersion, description, nil, []bugReportRootFile{
		{name: guiBugReportScreenshotName, data: pngBytes},
		{name: guiBugReportNotesName, data: notes},
	})
	if err != nil {
		// The dialog stays open on a HubCenter failure. Keep the capture so
		// Send can be pressed again without photographing the error text.
		a.keepGUIBugReportShot(shot)
		return nil, err
	}
	_ = os.Remove(shot)
	return result, nil
}

// keepGUIBugReportShot puts a capture back after a failed send. A newer
// capture that landed during the upload wins, and this file is dropped.
func (a *App) keepGUIBugReportShot(path string) {
	if a == nil || strings.TrimSpace(path) == "" {
		return
	}
	a.guiBugReportShotMu.Lock()
	defer a.guiBugReportShotMu.Unlock()
	if a.guiBugReportShot == "" {
		a.guiBugReportShot = path
		return
	}
	if a.guiBugReportShot != path {
		_ = os.Remove(path)
	}
}

func validateGUIBugReportScreenshot(data []byte) error {
	if len(data) == 0 || len(data) > guiBugReportScreenshotMaxBytes {
		return fmt.Errorf("main window screenshot is empty or too large")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("main window screenshot is not a PNG")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > guiBugReportScreenshotMaxEdge || cfg.Height > guiBugReportScreenshotMaxEdge {
		return fmt.Errorf("main window screenshot dimensions are not supported")
	}
	return nil
}

func writeBugReportZipRoot(zw *zip.Writer, name string, data []byte) error {
	name = strings.TrimSpace(name)
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid diagnostics root entry %q", name)
	}
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}
