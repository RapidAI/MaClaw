//go:build !wails3

package runtime

// v2 mode: pure delegation to the vendored wails/v2/pkg/runtime. Signatures
// and semantics are byte-identical to calling the wails runtime directly, so
// the import-path swap in guiapp is behavior-neutral in the default build.

import (
	"context"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func toWailsFilters(filters []FileFilter) []wailsruntime.FileFilter {
	if filters == nil {
		return nil
	}
	out := make([]wailsruntime.FileFilter, len(filters))
	for i, f := range filters {
		out[i] = wailsruntime.FileFilter{DisplayName: f.DisplayName, Pattern: f.Pattern}
	}
	return out
}

func toWailsOpenOptions(o OpenDialogOptions) wailsruntime.OpenDialogOptions {
	return wailsruntime.OpenDialogOptions{
		DefaultDirectory:           o.DefaultDirectory,
		DefaultFilename:            o.DefaultFilename,
		Title:                      o.Title,
		Filters:                    toWailsFilters(o.Filters),
		ShowHiddenFiles:            o.ShowHiddenFiles,
		CanCreateDirectories:       o.CanCreateDirectories,
		ResolvesAliases:            o.ResolvesAliases,
		TreatPackagesAsDirectories: o.TreatPackagesAsDirectories,
	}
}

func toWailsSaveOptions(o SaveDialogOptions) wailsruntime.SaveDialogOptions {
	return wailsruntime.SaveDialogOptions{
		DefaultDirectory:           o.DefaultDirectory,
		DefaultFilename:            o.DefaultFilename,
		Title:                      o.Title,
		Filters:                    toWailsFilters(o.Filters),
		ShowHiddenFiles:            o.ShowHiddenFiles,
		CanCreateDirectories:       o.CanCreateDirectories,
		TreatPackagesAsDirectories: o.TreatPackagesAsDirectories,
	}
}

// Events.
func EventsEmit(ctx context.Context, eventName string, optionalData ...interface{}) {
	wailsruntime.EventsEmit(ctx, eventName, optionalData...)
}

// Application.
func Quit(ctx context.Context) { wailsruntime.Quit(ctx) }

func BrowserOpenURL(ctx context.Context, url string) { wailsruntime.BrowserOpenURL(ctx, url) }

// Clipboard.
func ClipboardGetText(ctx context.Context) (string, error) {
	return wailsruntime.ClipboardGetText(ctx)
}

func ClipboardSetText(ctx context.Context, text string) error {
	return wailsruntime.ClipboardSetText(ctx, text)
}

// Dialogs.
func OpenFileDialog(ctx context.Context, dialogOptions OpenDialogOptions) (string, error) {
	return wailsruntime.OpenFileDialog(ctx, toWailsOpenOptions(dialogOptions))
}

func OpenDirectoryDialog(ctx context.Context, dialogOptions OpenDialogOptions) (string, error) {
	return wailsruntime.OpenDirectoryDialog(ctx, toWailsOpenOptions(dialogOptions))
}

func OpenMultipleFilesDialog(ctx context.Context, dialogOptions OpenDialogOptions) ([]string, error) {
	return wailsruntime.OpenMultipleFilesDialog(ctx, toWailsOpenOptions(dialogOptions))
}

func SaveFileDialog(ctx context.Context, dialogOptions SaveDialogOptions) (string, error) {
	return wailsruntime.SaveFileDialog(ctx, toWailsSaveOptions(dialogOptions))
}

// Drag and drop.
func OnFileDrop(ctx context.Context, callback func(x, y int, paths []string)) {
	wailsruntime.OnFileDrop(ctx, callback)
}

func OnFileDropOff(ctx context.Context) { wailsruntime.OnFileDropOff(ctx) }

// Window.
func WindowCenter(ctx context.Context) { wailsruntime.WindowCenter(ctx) }
func WindowShow(ctx context.Context)   { wailsruntime.WindowShow(ctx) }
func WindowHide(ctx context.Context)   { wailsruntime.WindowHide(ctx) }
func WindowReload(ctx context.Context) { wailsruntime.WindowReload(ctx) }
func WindowSetSize(ctx context.Context, width int, height int) {
	wailsruntime.WindowSetSize(ctx, width, height)
}

func WindowGetSize(ctx context.Context) (int, int) { return wailsruntime.WindowGetSize(ctx) }

func WindowSetAlwaysOnTop(ctx context.Context, b bool) {
	wailsruntime.WindowSetAlwaysOnTop(ctx, b)
}

func WindowSetPosition(ctx context.Context, x int, y int) {
	wailsruntime.WindowSetPosition(ctx, x, y)
}

func WindowGetPosition(ctx context.Context) (int, int) { return wailsruntime.WindowGetPosition(ctx) }

func WindowSetTitle(ctx context.Context, title string) { wailsruntime.WindowSetTitle(ctx, title) }

func WindowMaximise(ctx context.Context)   { wailsruntime.WindowMaximise(ctx) }
func WindowUnmaximise(ctx context.Context) { wailsruntime.WindowUnmaximise(ctx) }
func WindowMinimise(ctx context.Context)   { wailsruntime.WindowMinimise(ctx) }
func WindowUnminimise(ctx context.Context) { wailsruntime.WindowUnminimise(ctx) }

func WindowIsMaximised(ctx context.Context) bool { return wailsruntime.WindowIsMaximised(ctx) }
func WindowIsMinimised(ctx context.Context) bool { return wailsruntime.WindowIsMinimised(ctx) }
func WindowIsFullscreen(ctx context.Context) bool {
	return wailsruntime.WindowIsFullscreen(ctx)
}

func WindowFullscreen(ctx context.Context)   { wailsruntime.WindowFullscreen(ctx) }
func WindowUnfullscreen(ctx context.Context) { wailsruntime.WindowUnfullscreen(ctx) }

func WindowExecJS(ctx context.Context, js string) { wailsruntime.WindowExecJS(ctx, js) }
