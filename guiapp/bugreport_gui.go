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

// maybeClearDiagnosticsForNewDay empties logs and trajectories on the first
// GUI start of the local day. The file companion and a same-day restart leave
// the directories alone. The stamp lives beside those directories so the
// clear itself cannot delete the record of today's run.
func (a *App) maybeClearDiagnosticsForNewDay(now time.Time) {
	if a == nil || a.isFileCompanionProcess() {
		return
	}
	today := now.In(time.Local).Format("2006-01-02")
	path := filepath.Join(a.getMaclawBaseDir(), "diagnostics-day.txt")
	if data, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(data)) == today {
		return
	}
	a.clearBugReportDiagnostics()
	if err := os.WriteFile(path, []byte(today+"\n"), 0o644); err != nil {
		log.Printf("[bug-report] daily diagnostics stamp: %v", err)
	}
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
