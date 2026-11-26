// Package runtime is the guiapp Wails-runtime adapter: a v2-shaped seam over
// the Wails runtime so the v2→v3 migration (branch wails3-migration) can swap
// the underlying implementation with build tags instead of editing every call
// site.
//
// The package NAME is deliberately `runtime`: 40+ guiapp files import the
// Wails runtime unaliased and call `runtime.SaveFileDialog(...)` etc., so a
// same-named replacement package keeps every call site byte-identical — only
// the import path changes. The directory is named `wruntime` to avoid
// confusion with the standard library's runtime.
//
// The wails/v2 delegation mode (v2.go) was removed when the wails3 runtime
// was promoted to the default (v2 retirement); v3.go is the sole
// implementation.
//
// The option structs below mirror the v2 shapes field-for-field so existing
// struct literals compile unchanged in both modes.
package runtime

// FileFilter mirrors wails/v2 frontend.FileFilter.
type FileFilter struct {
	DisplayName string // Filter information EG: "Image Files (*.jpg, *.png)"
	Pattern     string // semicolon separated list of extensions, EG: "*.jpg;*.png"
}

// OpenDialogOptions mirrors wails/v2 frontend.OpenDialogOptions.
type OpenDialogOptions struct {
	DefaultDirectory           string
	DefaultFilename            string
	Title                      string
	Filters                    []FileFilter
	ShowHiddenFiles            bool
	CanCreateDirectories       bool
	ResolvesAliases            bool
	TreatPackagesAsDirectories bool
}

// SaveDialogOptions mirrors wails/v2 frontend.SaveDialogOptions.
type SaveDialogOptions struct {
	DefaultDirectory           string
	DefaultFilename            string
	Title                      string
	Filters                    []FileFilter
	ShowHiddenFiles            bool
	CanCreateDirectories       bool
	TreatPackagesAsDirectories bool
}
