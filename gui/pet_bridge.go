package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/gui/petpack"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ListPetPacks returns installed (bundled + user) pet packs for settings UI.
// Always re-scans so packs dropped into the user directory appear without restart.
func (a *App) ListPetPacks() []petpack.PackInfo {
	reg := petpack.EnsureGlobal()
	if reg == nil {
		return nil
	}
	_ = reg.Scan()
	return reg.List()
}

// SelectPetPackZip opens a native file dialog for a pet-pack zip.
// Returns empty string if the user cancels or the dialog is unavailable.
func (a *App) SelectPetPackZip() string {
	if a == nil || a.ctx == nil {
		return ""
	}
	return a.selectFile(
		"Select Pet Pack Zip (.zip with pet-pack.yaml)",
		[]runtime.FileFilter{{DisplayName: "Pet Pack Zip (*.zip)", Pattern: "*.zip"}},
	)
}

// validatePetPackZipPath checks path is a non-empty existing .zip file within size limits.
func validatePetPackZipPath(zipPath string) (string, error) {
	zipPath = strings.TrimSpace(zipPath)
	if zipPath == "" {
		return "", errString("pet pack zip path is empty")
	}
	clean := filepath.Clean(zipPath)
	if strings.EqualFold(filepath.Ext(clean), ".zip") == false {
		return "", fmt.Errorf("pet pack must be a .zip file")
	}
	st, err := os.Stat(clean)
	if err != nil {
		return "", fmt.Errorf("pet pack zip not found: %w", err)
	}
	if st.IsDir() {
		return "", fmt.Errorf("pet pack path is a directory, expected .zip file")
	}
	if st.Size() > petpack.MaxZipArchiveBytes {
		return "", fmt.Errorf("zip too large (max %d bytes)", petpack.MaxZipArchiveBytes)
	}
	return clean, nil
}

// InstallPetPackZip installs a pet pack from a local zip path.
func (a *App) InstallPetPackZip(zipPath string) (string, error) {
	clean, err := validatePetPackZipPath(zipPath)
	if err != nil {
		return "", err
	}
	reg := petpack.EnsureGlobal()
	if reg == nil {
		return "", errPetRegistryUnavailable
	}
	id, err := reg.InstallZip(clean)
	if err != nil {
		return "", err
	}
	if a != nil && a.ctx != nil {
		a.emitEvent("pet:packs-changed", map[string]any{"id": id, "action": "install"})
	}
	return id, nil
}

// UninstallPetPack removes a user-installed pack by id (not bundled-only official).
func (a *App) UninstallPetPack(id string) error {
	id = strings.TrimSpace(id)
	if id == "" || !petpack.IsValidPackID(id) {
		return errString("invalid pet pack id")
	}
	reg := petpack.EnsureGlobal()
	if reg == nil {
		return errPetRegistryUnavailable
	}
	if err := reg.Uninstall(id); err != nil {
		return err
	}
	// If active skin was this pack (user or override), fall back to clawmate classic.
	// Surface patch failures: pack is already gone; caller should still refresh UI.
	var resetErr error
	if a != nil {
		cfg, err := a.LoadConfig()
		if err == nil && cfg.PetSkin == id {
			if _, patchErr := a.PatchConfigFields(map[string]interface{}{
				"pet_skin":                              petpack.DefaultPackID,
				"pet_variant":                           petpack.VariantClassic,
				"pet_figurative_upgrade_prompt_pending": false,
			}); patchErr != nil {
				resetErr = fmt.Errorf("uninstalled %q but failed to reset active skin: %w", id, patchErr)
			}
		}
	}
	if a != nil && a.ctx != nil {
		a.emitEvent("pet:packs-changed", map[string]any{"id": id, "action": "uninstall"})
	}
	return resetErr
}

// GetPetPackPreviewDataURL returns a data: URL for the pack's preview/idle image.
// Empty string when unavailable (client keeps builtin SVG fallback).
func (a *App) GetPetPackPreviewDataURL(packID string) string {
	return encodePetAssetDataURL(func(reg *petpack.Registry) ([]byte, string, error) {
		return reg.LoadPreviewBytes(packID)
	})
}

// GetPetPackStateFrameDataURL returns a data: URL for a pack state frame (settings live preview).
// variant should be default/figurative for raster frames; classic returns empty.
func (a *App) GetPetPackStateFrameDataURL(packID, state, variant string) string {
	return encodePetAssetDataURL(func(reg *petpack.Registry) ([]byte, string, error) {
		return reg.LoadStateFrameBytes(packID, state, variant)
	})
}

// OpenPetPacksDir reveals the user pet-packs directory in the OS file manager.
// Creates the directory if missing.
func (a *App) OpenPetPacksDir() error {
	dir := petpack.UserPacksDir()
	if strings.TrimSpace(dir) == "" {
		return errString("pet packs directory unavailable")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create pet packs dir: %w", err)
	}
	// Prefer revealing the directory itself (select a placeholder if empty).
	if a == nil {
		return errString("app unavailable")
	}
	// ShowItemInFolder expects a file on Windows (/select,); open folder via system open.
	if err := startSystemOpen(dir); err != nil {
		// Fallback: reveal dir by selecting a known child or the path string.
		return a.ShowItemInFolder(dir)
	}
	return nil
}

// GetPetPacksDir returns the absolute user pet-packs directory path (for UI display).
func (a *App) GetPetPacksDir() string {
	return petpack.UserPacksDir()
}

func encodePetAssetDataURL(load func(*petpack.Registry) ([]byte, string, error)) string {
	reg := petpack.EnsureGlobal()
	if reg == nil || load == nil {
		return ""
	}
	data, contentType, err := load(reg)
	// One retry after rescan only when the pack id is missing (install race / external drop).
	// Do not rescan for expected misses (classic frames, empty assets).
	if err != nil && isPetPackNotFound(err) {
		_ = reg.Scan()
		data, contentType, err = load(reg)
	}
	if err != nil || len(data) == 0 {
		return ""
	}
	if contentType == "" {
		contentType = "image/png"
	}
	// Cap payload size for settings data URLs (same order as zip single-file limit).
	if len(data) > 512*1024 {
		return ""
	}
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// SetDesktopPetState is the Wails-exported bridge for FE pet:state → native window (K11).
// Uses the existing floating manager only — does not create the pet window just to apply state.
func (a *App) SetDesktopPetState(state string, ttlMs int) {
	if a == nil {
		return
	}
	fa := a.existingFloatingAssistant()
	if fa == nil {
		return
	}
	fa.SetPetRuntimeState(state, ttlMs)
}

// GetDesktopPetState returns the native pet runtime state (tests / diagnostics).
func (a *App) GetDesktopPetState() string {
	if a == nil {
		return string(petpack.StateIdle)
	}
	fa := a.existingFloatingAssistant()
	if fa == nil {
		return string(petpack.StateIdle)
	}
	return fa.CurrentPetRuntimeState()
}

// EnsurePetPackRegistryScanned forces registry init before config sanitize paths that need allowlist.
func EnsurePetPackRegistryScanned() {
	_ = petpack.EnsureGlobal()
}

var errPetRegistryUnavailable = errString("pet pack registry unavailable")

type errString string

func (e errString) Error() string { return string(e) }

// isPetPackNotFound matches registry "pack not found" errors without over-matching
// unrelated messages (e.g. classic variant has no frames).
func isPetPackNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "not found") && strings.Contains(msg, "pack")
}
