package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/gui/petpack"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// petStoreRequest proxies Pet Store calls through the native application. The
// SkillMarket session is never placed in the WebView or in a URL; it only
// travels as an Authorization header from this trusted process to HubCenter.
func (a *App) petStoreRequest(method, path string, body io.Reader, contentType string) ([]byte, error) {
	if a == nil {
		return nil, errString("app unavailable")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	baseURL, token := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubCenterURL), "/"), strings.TrimSpace(cfg.SkillMarketSessionToken)
	if baseURL == "" || token == "" {
		return nil, errString("please sign in to HubCenter before using the Pet Store")
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" || base.User != nil || (base.Scheme != "https" && base.Scheme != "http") {
		return nil, errString("HubCenter URL must be an absolute HTTP(S) URL")
	}
	requestPath, err := url.Parse(path)
	if err != nil || requestPath.IsAbs() || requestPath.Host != "" || !strings.HasPrefix(requestPath.Path, "/") {
		return nil, errString("invalid Pet Store request path")
	}
	target := base.ResolveReference(requestPath)
	if !strings.EqualFold(target.Scheme, base.Scheme) || !strings.EqualFold(target.Host, base.Host) {
		return nil, errString("Pet Store request must stay on the configured HubCenter")
	}
	req, err := http.NewRequest(method, target.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	// Authorization is intentionally native-only. Do not follow redirects: a
	// redirect is both unexpected for the API and an unnecessary opportunity to
	// carry an authenticated request beyond the configured HubCenter origin.
	resp, err := (&http.Client{
		Timeout: 90 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if readErr != nil {
		return nil, fmt.Errorf("read Pet Store response: %w", readErr)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		message := strings.TrimSpace(string(data))
		var result struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &result) == nil {
			if strings.TrimSpace(result.Message) != "" {
				message = result.Message
			} else if strings.TrimSpace(result.Error) != "" {
				message = result.Error
			}
		}
		if message == "" {
			message = resp.Status
		}
		return nil, fmt.Errorf("Pet Store request failed: %s", message)
	}
	return data, nil
}

// ListPetStorePacks returns one page of listings in the signed-in user's
// HubCenter market, without exposing the session credential to the WebView.
func (a *App) ListPetStorePacks(query, sort, order string, page, pageSize int) (map[string]interface{}, error) {
	values := make(url.Values)
	values.Set("q", strings.TrimSpace(query))
	values.Set("sort", strings.TrimSpace(sort))
	values.Set("order", strings.TrimSpace(order))
	values.Set("page", strconv.Itoa(page))
	values.Set("page_size", strconv.Itoa(pageSize))
	data, err := a.petStoreRequest(http.MethodGet, "/api/v1/pet-store/packs?"+values.Encode(), nil, "")
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetPetStoreAccount returns the embedded User Center data for the current
// HubCenter account: identity, Credits, uploads, and permanent purchases.
func (a *App) GetPetStoreAccount() (map[string]interface{}, error) {
	data, err := a.petStoreRequest(http.MethodGet, "/api/v1/pet-store/account", nil, "")
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (a *App) GetPetStoreRankings() (map[string]interface{}, error) {
	data, err := a.petStoreRequest(http.MethodGet, "/api/v1/pet-store/rankings", nil, "")
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// CanPublishPetStorePack asks HubCenter whether this local pack identity is
// already claimed by another creator. A locally installed Zip stays shareable
// unless the same stable manifest ID already belongs to somebody else's market
// listing; HubCenter repeats this check when publishing to prevent races.
func (a *App) CanPublishPetStorePack(sourcePackID string) (bool, error) {
	sourcePackID = strings.TrimSpace(sourcePackID)
	if !petpack.IsValidPackID(sourcePackID) {
		return false, errString("invalid pet pack id")
	}
	data, err := a.petStoreRequest(http.MethodGet, "/api/v1/pet-store/packs/source/"+url.PathEscape(sourcePackID)+"/publishability", nil, "")
	if err != nil {
		return false, err
	}
	var result struct {
		CanPublish bool `json:"can_publish"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return false, err
	}
	return result.CanPublish, nil
}

func (a *App) PurchasePetStorePack(id string) (map[string]interface{}, error) {
	data, err := a.petStoreRequest(http.MethodPost, "/api/v1/pet-store/packs/"+url.PathEscape(strings.TrimSpace(id))+"/purchase", nil, "")
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (a *App) WithdrawPetStorePack(id string) error {
	_, err := a.petStoreRequest(http.MethodPost, "/api/v1/pet-store/packs/"+url.PathEscape(strings.TrimSpace(id))+"/withdraw", nil, "")
	return err
}

// InstallPetStorePack downloads a permanent entitlement through the native
// authenticated bridge and validates/installs it via the same registry path as
// a local ZIP. No archive is exposed to a browser download flow.
func (a *App) InstallPetStorePack(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", errString("pet store pack id is required")
	}
	data, err := a.petStoreRequest(http.MethodGet, "/api/v1/pet-store/packs/"+url.PathEscape(id)+"/download", nil, "")
	if err != nil {
		return "", err
	}
	// The bridge caps reads at 4 MiB; the registry accepts at most
	// MaxZipArchiveBytes. Check before installing so an over-limit (or
	// truncated) download fails with a clear size error instead of a
	// corrupt-zip one.
	if len(data) == 0 {
		return "", errString("Pet Store returned an empty pet pack archive")
	}
	if int64(len(data)) > petpack.MaxZipArchiveBytes {
		return "", fmt.Errorf("pet pack archive exceeds the %d byte size limit", petpack.MaxZipArchiveBytes)
	}
	reg := petpack.EnsureGlobal()
	if reg == nil {
		return "", errPetRegistryUnavailable
	}
	installedID, err := reg.InstallZipBytes(data)
	if err != nil {
		return "", err
	}
	if err := reg.SetPackSource(installedID, petpack.SourceMarket); err != nil {
		return "", fmt.Errorf("mark pet pack source: %w", err)
	}
	if a.ctx != nil {
		a.emitEvent("pet:packs-changed", map[string]any{"id": installedID, "action": "install"})
	}
	return installedID, nil
}

// SubmitPetStorePack publishes a locally exported user pack through the same
// native authenticated bridge. Its file never crosses the WebView boundary.
func (a *App) SubmitPetStorePack(zipPath, name, description, version string, price int64, sourcePackID string) (map[string]interface{}, error) {
	if price < 0 || price > 999999 {
		return nil, errString("price must be 0-999999 credits")
	}
	sourcePackID = strings.TrimSpace(sourcePackID)
	if !petpack.IsValidPackID(sourcePackID) {
		return nil, errString("a valid locally created pet pack is required for sharing")
	}
	reg := petpack.EnsureGlobal()
	if reg == nil {
		return nil, errPetRegistryUnavailable
	}
	pack, ok := reg.Get(sourcePackID)
	if !ok || pack == nil || pack.Scope != petpack.ScopeUser {
		return nil, errString("only locally installed pet packs can be shared")
	}
	if reg.IsPackMarketInstalled(sourcePackID) {
		return nil, errString("Pet Store downloads cannot be shared again")
	}
	clean, err := validatePetPackZipPath(zipPath)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(clean)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for field, value := range map[string]string{
		"name":           strings.TrimSpace(name),
		"description":    strings.TrimSpace(description),
		"version":        strings.TrimSpace(version),
		"price":          strconv.FormatInt(price, 10),
		"source_pack_id": sourcePackID,
	} {
		if err := mw.WriteField(field, value); err != nil {
			return nil, fmt.Errorf("write Pet Store form field %s: %w", field, err)
		}
	}
	part, err := mw.CreateFormFile("zip", filepath.Base(clean))
	if err != nil {
		return nil, err
	}
	if _, err = io.Copy(part, file); err != nil {
		return nil, err
	}
	if err = mw.Close(); err != nil {
		return nil, err
	}
	data, err := a.petStoreRequest(http.MethodPost, "/api/v1/pet-store/packs", &body, mw.FormDataContentType())
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

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
	// A Zip installed from the local file picker is treated as the local user's
	// custom pack. HubCenter's source-pack-ID comparison, not the transport used
	// to install it, determines whether another market creator already owns it.
	if err := reg.SetPackSource(id, petpack.SourceCreated); err != nil {
		return "", fmt.Errorf("mark pet pack source: %w", err)
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

// ExportPetPackZip saves a locally installed custom pet pack as a portable zip
// ready for the Pet Store upload form. Official bundled packs are intentionally
// excluded; HubCenter verifies whether this stable pack ID is already owned by
// another marketplace creator before accepting the upload.
func (a *App) ExportPetPackZip(id string) (string, error) {
	id = strings.TrimSpace(id)
	if !petpack.IsValidPackID(id) {
		return "", errString("invalid pet pack id")
	}
	if a == nil || a.ctx == nil {
		return "", errString("app context unavailable")
	}
	reg := petpack.EnsureGlobal()
	if reg == nil {
		return "", errPetRegistryUnavailable
	}
	_ = reg.Scan()
	pack, ok := reg.Get(id)
	if !ok || pack == nil || pack.Scope != petpack.ScopeUser {
		return "", errString("only locally installed pet packs can be shared")
	}
	if reg.IsPackMarketInstalled(id) {
		return "", errString("Pet Store downloads cannot be shared again")
	}
	dest, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export Pet Pack for MaClaw Pet Store",
		DefaultFilename: id + ".zip",
		Filters: []runtime.FileFilter{
			{DisplayName: "Pet Pack Zip (*.zip)", Pattern: "*.zip"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
	if err != nil || strings.TrimSpace(dest) == "" {
		return "", err
	}
	if !strings.EqualFold(filepath.Ext(dest), ".zip") {
		dest += ".zip"
	}
	if err := writePetPackZip(pack.Dir, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func writePetPackZip(sourceDir, dest string) error {
	root, err := filepath.Abs(sourceDir)
	if err != nil {
		return err
	}
	target, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	f, err := os.Create(target)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	writeErr := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to export symlink in pet pack")
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported pet pack file")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("unsafe pet pack path")
		}
		if petpack.IsPackSourceMarker(rel) {
			return nil
		}
		entry, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(entry, in)
		closeErr := in.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	closeErr := zw.Close()
	fileCloseErr := f.Close()
	if writeErr != nil {
		_ = os.Remove(target)
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(target)
		return closeErr
	}
	if fileCloseErr != nil {
		_ = os.Remove(target)
	}
	return fileCloseErr
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
