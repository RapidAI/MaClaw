package runtime

// v3 mode: implementation over the wails/v3 application objects. The
// bootstrap (guiapp/main_wails3.go) installs the application and main-window
// references at startup; every function below then behaves like its v2
// counterpart. ctx parameters are kept for signature compatibility with the
// v2-shaped call sites and are ignored — v3 targets objects, not contexts.

import (
	"context"
	"errors"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

var (
	v3App        *application.App
	v3MainWindow *application.WebviewWindow
)

// SetApplication installs the running v3 application. Must be called before
// any adapted runtime call can take effect.
func SetApplication(app *application.App) { v3App = app }

// SetMainWindow installs the main webview window the v2-shaped Window*
// functions operate on (v2 targeted the single window implicitly).
func SetMainWindow(win *application.WebviewWindow) { v3MainWindow = win }

// MainWindow returns the registered main window (used by the bootstrap for
// the DOM-ready hook wiring).
func MainWindow() *application.WebviewWindow { return v3MainWindow }

// Application returns the registered v3 application.
func Application() *application.App { return v3App }

func v3Ready() bool { return v3App != nil }

// Events.
func EventsEmit(ctx context.Context, eventName string, optionalData ...interface{}) {
	if !v3Ready() {
		return
	}
	v3App.Event.Emit(eventName, optionalData...)
}

// Application.
func Quit(ctx context.Context) {
	if v3Ready() {
		v3App.Quit()
	}
}

func BrowserOpenURL(ctx context.Context, url string) {
	if !v3Ready() {
		return
	}
	if err := v3App.Browser.OpenURL(url); err != nil {
		log.Printf("[wruntime] BrowserOpenURL %q: %v", url, err)
	}
}

// Clipboard. v3 reports success as bool; surface the failure as an error to
// keep the v2-shaped (error) return.
func ClipboardGetText(ctx context.Context) (string, error) {
	if !v3Ready() {
		return "", errors.New("wails3 runtime not ready")
	}
	text, ok := v3App.Clipboard.Text()
	if !ok {
		return "", errors.New("clipboard read failed")
	}
	return text, nil
}

func ClipboardSetText(ctx context.Context, text string) error {
	if !v3Ready() {
		return errors.New("wails3 runtime not ready")
	}
	if !v3App.Clipboard.SetText(text) {
		return errors.New("clipboard write failed")
	}
	return nil
}

// Dialogs.

func applyOpenDialogOptions(d *application.OpenFileDialogStruct, o OpenDialogOptions) *application.OpenFileDialogStruct {
	if o.Title != "" {
		d = d.SetTitle(o.Title)
	}
	if o.DefaultDirectory != "" {
		d = d.SetDirectory(o.DefaultDirectory)
	}
	// NOTE: v3's open dialog has no SetFilename builder (save does); the v2
	// DefaultFilename option has no direct equivalent here.
	if o.CanCreateDirectories {
		d = d.CanCreateDirectories(true)
	}
	if o.ShowHiddenFiles {
		d = d.ShowHiddenFiles(true)
	}
	for _, f := range o.Filters {
		d = d.AddFilter(f.DisplayName, f.Pattern)
	}
	if v3MainWindow != nil {
		d = d.AttachToWindow(v3MainWindow)
	}
	return d
}

func applySaveDialogOptions(d *application.SaveFileDialogStruct, o SaveDialogOptions) *application.SaveFileDialogStruct {
	if o.Title != "" {
		d = d.SetMessage(o.Title)
	}
	if o.DefaultDirectory != "" {
		d = d.SetDirectory(o.DefaultDirectory)
	}
	if o.DefaultFilename != "" {
		d = d.SetFilename(o.DefaultFilename)
	}
	if o.CanCreateDirectories {
		d = d.CanCreateDirectories(true)
	}
	if o.ShowHiddenFiles {
		d = d.ShowHiddenFiles(true)
	}
	for _, f := range o.Filters {
		d = d.AddFilter(f.DisplayName, f.Pattern)
	}
	if v3MainWindow != nil {
		d = d.AttachToWindow(v3MainWindow)
	}
	return d
}

func OpenFileDialog(ctx context.Context, dialogOptions OpenDialogOptions) (string, error) {
	if !v3Ready() {
		return "", errors.New("wails3 runtime not ready")
	}
	d := applyOpenDialogOptions(v3App.Dialog.OpenFile(), dialogOptions)
	return d.PromptForSingleSelection()
}

func OpenDirectoryDialog(ctx context.Context, dialogOptions OpenDialogOptions) (string, error) {
	if !v3Ready() {
		return "", errors.New("wails3 runtime not ready")
	}
	d := applyOpenDialogOptions(v3App.Dialog.OpenFile(), dialogOptions)
	d = d.CanChooseDirectories(true).CanChooseFiles(false)
	return d.PromptForSingleSelection()
}

func OpenMultipleFilesDialog(ctx context.Context, dialogOptions OpenDialogOptions) ([]string, error) {
	if !v3Ready() {
		return nil, errors.New("wails3 runtime not ready")
	}
	d := applyOpenDialogOptions(v3App.Dialog.OpenFile(), dialogOptions)
	return d.PromptForMultipleSelection()
}

func SaveFileDialog(ctx context.Context, dialogOptions SaveDialogOptions) (string, error) {
	if !v3Ready() {
		return "", errors.New("wails3 runtime not ready")
	}
	d := applySaveDialogOptions(v3App.Dialog.SaveFile(), dialogOptions)
	return d.PromptForSingleSelection()
}

// Drag and drop. v3 delivers drops as the WindowFilesDropped window event;
// the v2 callback shape receives only the resolved paths (no coordinates).
// OnFileDropOff is not wired: v3 registration happens once via the bootstrap.
func OnFileDrop(ctx context.Context, callback func(x, y int, paths []string)) {
	if !v3Ready() || v3MainWindow == nil {
		log.Printf("[wruntime] OnFileDrop ignored: runtime not ready")
		return
	}
	v3MainWindow.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		files := event.Context().DroppedFiles()
		callback(0, 0, files)
	})
}

func OnFileDropOff(ctx context.Context) {
	// v3 OnWindowEvent returns an unregistrer; dropping the callback is
	// acceptable for the guiapp usage (one registration for the app life).
	log.Printf("[wruntime] OnFileDropOff: no-op under v3 (single registration)")
}

// Window.
func WindowCenter(ctx context.Context) {
	if v3MainWindow != nil {
		v3MainWindow.Center()
	}
}

func WindowShow(ctx context.Context) {
	if v3MainWindow != nil {
		v3MainWindow.Show()
	}
}

func WindowHide(ctx context.Context) {
	if v3MainWindow != nil {
		v3MainWindow.Hide()
	}
}

func WindowReload(ctx context.Context) {
	if v3MainWindow != nil {
		v3MainWindow.Reload()
	}
}

func WindowSetSize(ctx context.Context, width int, height int) {
	if v3MainWindow != nil {
		v3MainWindow.SetSize(width, height)
	}
}

func WindowGetSize(ctx context.Context) (int, int) {
	if v3MainWindow == nil {
		return 0, 0
	}
	return v3MainWindow.Size()
}

func WindowSetAlwaysOnTop(ctx context.Context, b bool) {
	if v3MainWindow != nil {
		v3MainWindow.SetAlwaysOnTop(b)
	}
}

func WindowSetPosition(ctx context.Context, x int, y int) {
	if v3MainWindow != nil {
		v3MainWindow.SetPosition(x, y)
	}
}

func WindowGetPosition(ctx context.Context) (int, int) {
	if v3MainWindow == nil {
		return 0, 0
	}
	return v3MainWindow.Position()
}

func WindowSetTitle(ctx context.Context, title string) {
	if v3MainWindow != nil {
		v3MainWindow.SetTitle(title)
	}
}

func WindowMaximise(ctx context.Context) {
	if v3MainWindow != nil {
		v3MainWindow.Maximise()
	}
}

func WindowUnmaximise(ctx context.Context) {
	if v3MainWindow != nil {
		v3MainWindow.UnMaximise()
	}
}

func WindowMinimise(ctx context.Context) {
	if v3MainWindow != nil {
		v3MainWindow.Minimise()
	}
}

func WindowUnminimise(ctx context.Context) {
	if v3MainWindow != nil {
		v3MainWindow.UnMinimise()
	}
}

func WindowIsMaximised(ctx context.Context) bool {
	if v3MainWindow == nil {
		return false
	}
	return v3MainWindow.IsMaximised()
}

func WindowIsMinimised(ctx context.Context) bool {
	if v3MainWindow == nil {
		return false
	}
	return v3MainWindow.IsMinimised()
}

func WindowIsFullscreen(ctx context.Context) bool {
	if v3MainWindow == nil {
		return false
	}
	return v3MainWindow.IsFullscreen()
}

func WindowFullscreen(ctx context.Context) {
	if v3MainWindow != nil {
		v3MainWindow.Fullscreen()
	}
}

func WindowUnfullscreen(ctx context.Context) {
	if v3MainWindow != nil {
		v3MainWindow.UnFullscreen()
	}
}

func WindowExecJS(ctx context.Context, js string) {
	if v3MainWindow != nil {
		v3MainWindow.ExecJS(js)
	}
}
