package main

import (
	"archive/zip"
	"bytes"
	"context"
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

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/gui/petpack"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// petStoreRequest proxies Pet Store calls through the native application. The
// SkillMarket session is never placed in the WebView or in a URL; it only
// travels as an Authorization header from this trusted process to HubCenter.
func (a *App) petStoreRequest(method, path string, body io.Reader, contentType string) ([]byte, error) {
	return a.marketRequest(method, path, body, contentType, 4<<20, 4<<20, "Pet Store")
}

// marketRequest proxies marketplace calls through the native app while keeping
// the request and response ceilings aligned with each market's API contract.
func (a *App) marketRequest(method, path string, body io.Reader, contentType string, maxRequestBytes, maxResponseBytes int64, marketName string) ([]byte, error) {
	if a == nil {
		return nil, errString("app unavailable")
	}
	if maxRequestBytes < 1 || maxResponseBytes < 1 {
		return nil, errString("invalid marketplace request limits")
	}
	// A retry after session refresh needs a fresh request body. Pet Store bodies
	// are bounded by the service's 3 MiB archive limit plus multipart overhead;
	// keep the same 4 MiB boundary used for response reads.
	var payload []byte
	if body != nil {
		var err error
		payload, err = io.ReadAll(io.LimitReader(body, maxRequestBytes+1))
		if err != nil {
			return nil, fmt.Errorf("read %s request: %w", marketName, err)
		}
		if int64(len(payload)) > maxRequestBytes {
			return nil, fmt.Errorf("%s request is too large", marketName)
		}
	}
	// A market session may be absent after migration or cleared on another
	// device. When Hub enrollment is available, obtain one before failing the
	// first request rather than requiring the user to discover a separate login.
	cfg, configErr := a.LoadConfig()
	if configErr != nil {
		return nil, configErr
	}
	// The Pet Store API is served only by HubCenter. RemoteHubURL hosts no
	// /api/v1/pet-store/* routes, so there is deliberately no hub fallback:
	// fail fast with a clear message instead of a stream of 404s.
	if strings.TrimSpace(cfg.RemoteHubCenterURL) == "" {
		return nil, errPetStoreHubCenterMissing
	}
	staleToken := strings.TrimSpace(cfg.SkillMarketSessionToken)
	if staleToken == "" {
		if refreshErr := a.refreshPetStoreSession(""); refreshErr != nil {
			return nil, fmt.Errorf("please sign in to HubCenter before using the Pet Store (%v)", refreshErr)
		}
	}
	data, status, err := a.marketRequestOnce(method, path, payload, contentType, maxResponseBytes, marketName)
	if err == nil || (status != http.StatusUnauthorized && status != http.StatusForbidden) {
		return data, err
	}
	// Hub enrollment credentials can issue a new marketplace session without
	// exposing either credential to the WebView. This matches skill publishing:
	// an expired cached session must not make the Pet Store unusable.
	if refreshErr := a.refreshPetStoreSession(staleToken); refreshErr != nil {
		return nil, fmt.Errorf("%w; sign in again or reconnect this device to HubCenter (%v)", err, refreshErr)
	}
	data, _, retryErr := a.marketRequestOnce(method, path, payload, contentType, maxResponseBytes, marketName)
	if retryErr != nil {
		return nil, fmt.Errorf("%w (after refreshing HubCenter session)", retryErr)
	}
	return data, nil
}

// petStoreRequestOnce makes exactly one authenticated request. Its status is
// returned separately so petStoreRequest can refresh only on 401/403.
func (a *App) petStoreRequestOnce(method, path string, payload []byte, contentType string) ([]byte, int, error) {
	return a.marketRequestOnce(method, path, payload, contentType, 4<<20, "Pet Store")
}

func (a *App) marketRequestOnce(method, path string, payload []byte, contentType string, maxResponseBytes int64, marketName string) ([]byte, int, error) {
	if a == nil {
		return nil, 0, errString("app unavailable")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, 0, err
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubCenterURL), "/")
	if baseURL == "" {
		return nil, 0, errPetStoreHubCenterMissing
	}
	token := strings.TrimSpace(cfg.SkillMarketSessionToken)
	if token == "" {
		return nil, 0, errString("please sign in to HubCenter before using the Pet Store")
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" || base.User != nil || (base.Scheme != "https" && base.Scheme != "http") {
		return nil, 0, errString("HubCenter URL must be an absolute HTTP(S) URL")
	}
	requestPath, err := url.Parse(path)
	if err != nil || requestPath.IsAbs() || requestPath.Host != "" || !strings.HasPrefix(requestPath.Path, "/") {
		return nil, 0, errString("invalid Pet Store request path")
	}
	target := base.ResolveReference(requestPath)
	if !strings.EqualFold(target.Scheme, base.Scheme) || !strings.EqualFold(target.Host, base.Host) {
		return nil, 0, errString("Pet Store request must stay on the configured HubCenter")
	}
	req, err := http.NewRequest(method, target.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
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
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if readErr != nil {
		return nil, resp.StatusCode, fmt.Errorf("read %s response: %w", marketName, readErr)
	}
	if int64(len(data)) > maxResponseBytes {
		return nil, resp.StatusCode, fmt.Errorf("%s response is too large", marketName)
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
		return nil, resp.StatusCode, fmt.Errorf("Pet Store request failed: %s", message)
	}
	return data, resp.StatusCode, nil
}

// refreshPetStoreSession exchanges the existing Hub enrollment credentials for
// a fresh SkillMarket session and persists only that session token. The
// enrollment viewer token remains native-only and is never returned to React.
func (a *App) refreshPetStoreSession(staleToken string) error {
	if a == nil {
		return errString("app unavailable")
	}
	a.petStoreSessionRefreshMu.Lock()
	defer a.petStoreSessionRefreshMu.Unlock()
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	// A concurrent request may have refreshed the token while this one was
	// waiting for the mutex. Reuse it instead of issuing another session.
	currentToken := strings.TrimSpace(cfg.SkillMarketSessionToken)
	// A caller with no cached token can be queued behind another bootstrap
	// request. Once that request has persisted the fresh session, reuse it
	// instead of issuing an identical machine-login for every parallel market
	// request (listings, rankings, and account data commonly start together).
	if (staleToken == "" && currentToken != "") || (staleToken != "" && currentToken != staleToken) {
		return nil
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubCenterURL), "/")
	if baseURL == "" {
		// No hub fallback: the machine-login endpoint is HubCenter-only, and
		// petStoreRequest already fails fast on this configuration. Returning
		// the same error keeps refresh behavior consistent (no retry loop).
		return errPetStoreHubCenterMissing
	}
	account := strings.TrimSpace(cfg.RemoteUserID)
	if account == "" {
		account = strings.TrimSpace(cfg.RemoteEmail)
	}
	machineID := strings.TrimSpace(cfg.RemoteMachineID)
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if account == "" || machineID == "" || viewerToken == "" {
		return errString("Hub enrollment credentials are incomplete")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := remote.NewSkillMarketAuthClient().MachineLogin(ctx, baseURL, account, machineID, viewerToken)
	if err != nil {
		return fmt.Errorf("HubCenter machine login: %w", err)
	}
	if result == nil || strings.TrimSpace(result.SessionToken) == "" {
		return errString("HubCenter machine login returned no session")
	}
	return a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.SkillMarketSessionToken = strings.TrimSpace(result.SessionToken)
	})
}

// ListPetStorePacks returns one page of listings in the signed-in user's
// HubCenter market, without exposing the session credential to the WebView.
func (a *App) ListPetStorePacks(query, sort, order string, page, pageSize int) (map[string]interface{}, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 20 {
		pageSize = 20
	}
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
	// The market drawer needs a human-recognisable contact, never the internal
	// account key. A machine-login session from an older HubCenter can carry the
	// raw user ID in its email field, so prefer a valid server email and recover
	// from the local enrollment contact when necessary. Keep the user object
	// deliberately minimal: the UI has no need for an internal ID.
	var cfg corelib.AppConfig
	if loaded, loadErr := a.LoadConfig(); loadErr == nil {
		cfg = loaded
	}
	serverUser, _ := result["user"].(map[string]interface{})
	serverEmail, _ := serverUser["email"].(string)
	serverPhone := firstPetStoreContactValue(serverUser, "phone_number", "phone", "mobile")
	user := map[string]interface{}{}
	if email := petStoreContactEmail(serverEmail); email != "" {
		user["email"] = email
	} else if email := petStoreContactEmail(cfg.RemoteEmail); email != "" {
		user["email"] = email
	} else if phone := petStoreContactPhone(serverPhone); phone != "" {
		user["phone_number"] = phone
	} else if phone := petStoreContactPhone(cfg.RemoteMobile); phone != "" {
		user["phone_number"] = phone
	}
	result["user"] = user
	return result, nil
}

func firstPetStoreContactValue(values map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func petStoreContactEmail(value string) string {
	value = strings.TrimSpace(value)
	if strings.Count(value, "@") != 1 || strings.ContainsAny(value, " \t\r\n") {
		return ""
	}
	parts := strings.Split(value, "@")
	// Keep this aligned with the frontend account-contact guard. The account
	// endpoint should not emit an address that the UI will immediately reject.
	if parts[0] == "" || parts[1] == "" || !strings.Contains(parts[1], ".") {
		return ""
	}
	return value
}

func petStoreContactPhone(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "phone:"))
	digits := 0
	for _, char := range value {
		if char >= '0' && char <= '9' {
			digits++
			continue
		}
		if !strings.ContainsRune("+()- ", char) {
			return ""
		}
	}
	if digits < 8 || digits > 15 {
		return ""
	}
	return value
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

// GetPetStoreCreatorReport returns the signed-in creator's paid sales and
// free-download metrics for one UTC day, month, or year.
func (a *App) GetPetStoreCreatorReport(period, date string) (map[string]interface{}, error) {
	values := make(url.Values)
	values.Set("period", strings.TrimSpace(period))
	values.Set("date", strings.TrimSpace(date))
	data, err := a.petStoreRequest(http.MethodGet, "/api/v1/pet-store/creator-report?"+values.Encode(), nil, "")
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

// IsPetStorePackInstalled reports whether the current local registry contains
// the exact manifest ID associated with a market listing. The React market uses
// this as presentation state only; InstallPetStorePack remains authoritative
// and still protects against a local-ID collision.
func (a *App) IsPetStorePackInstalled(sourcePackID string) bool {
	sourcePackID = strings.TrimSpace(sourcePackID)
	if !petpack.IsValidPackID(sourcePackID) {
		return false
	}
	reg := petpack.EnsureGlobal()
	return reg != nil && reg.IsPackMarketInstalled(sourcePackID)
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
	// Marketplace content must never silently replace a locally authored or
	// imported pack with the same manifest ID. The dedicated install path also
	// writes the provenance marker before the package becomes visible, avoiding
	// a brief shareable state if the process exits between install and marking.
	installedID, err := reg.InstallMarketZipBytes(data)
	if err != nil {
		return "", err
	}
	if a.ctx != nil {
		a.emitEvent("pet:packs-changed", map[string]any{"id": installedID, "action": "install"})
	}
	if fa := a.existingFloatingAssistant(); fa != nil {
		fa.InvalidatePetPackAssets()
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
	// ExportPetPackZip creates its archives in this private staging directory.
	// Once HubCenter accepted the listing, the draft is no longer needed. Keep
	// externally supplied zip paths untouched for callers outside this flow.
	if draftsDir, dirErr := filepath.Abs(filepath.Join(a.GetDataDir(), "pet-store-drafts")); dirErr == nil {
		if archivePath, pathErr := filepath.Abs(clean); pathErr == nil && filepath.Dir(archivePath) == draftsDir {
			_ = os.Remove(archivePath)
		}
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
	if fa := a.existingFloatingAssistant(); fa != nil {
		fa.InvalidatePetPackAssets()
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
	// If active skin was this pack (user or override), return to the default
	// official ClawMate presentation.
	// Surface patch failures: pack is already gone; caller should still refresh UI.
	var resetErr error
	if a != nil {
		cfg, err := a.LoadConfig()
		if err == nil && cfg.PetSkin == id {
			if _, patchErr := a.PatchConfigFields(map[string]interface{}{
				"pet_skin":                              petpack.DefaultPackID,
				"pet_variant":                           petpack.VariantDefault,
				"pet_figurative_upgrade_prompt_pending": false,
			}); patchErr != nil {
				resetErr = fmt.Errorf("uninstalled %q but failed to reset active skin: %w", id, patchErr)
			}
		}
	}
	if a != nil && a.ctx != nil {
		a.emitEvent("pet:packs-changed", map[string]any{"id": id, "action": "uninstall"})
	}
	if a != nil {
		if fa := a.existingFloatingAssistant(); fa != nil {
			fa.InvalidatePetPackAssets()
		}
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

// ExportPetPackZip creates an app-managed staging archive for the embedded Pet
// Store publishing flow. It deliberately does not open a Save dialog: sharing
// is an in-app action, and requiring a second, unrelated export step meant a
// cancelled/hidden native dialog silently stopped the publish flow before the
// market form was ever shown.
//
// The archive is kept under the application data directory, never in the
// source pack itself. Official bundled packs are intentionally excluded;
// HubCenter verifies whether this stable pack ID is already owned by another
// marketplace creator before accepting the upload.
func (a *App) ExportPetPackZip(id string) (string, error) {
	id = strings.TrimSpace(id)
	if !petpack.IsValidPackID(id) {
		return "", errString("invalid pet pack id")
	}
	if a == nil {
		return "", errString("app unavailable")
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
	draftsDir := filepath.Join(a.GetDataDir(), "pet-store-drafts")
	if err := os.MkdirAll(draftsDir, 0o700); err != nil {
		return "", fmt.Errorf("create pet store draft directory: %w", err)
	}
	dest := filepath.Join(draftsDir, id+"-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".zip")
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

// SetDesktopPetState is the Wails-exported binding that applies the assistant's
// pet state to the native window (K11). It is the only pet-state channel; the
// former FE 'pet:state' event relay was removed.
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

// PetPackRuntimeInfo describes the selected pack's declared renderer level
// versus the level actually in effect on this machine, for the settings
// page's "animation capability / degradation reason" display.
type PetPackRuntimeInfo struct {
	// PackID is the currently selected pet pack.
	PackID string `json:"pack_id"`
	// Variant is the runtime-resolved variant id.
	Variant string `json:"variant"`
	// DeclaredRenderer is the level declared by the pack manifest for this
	// variant: native-character / native-skeleton / native-raster.
	DeclaredRenderer string `json:"declared_renderer"`
	// EffectiveRenderer is the level the runtime is actually using. It can be
	// lower than DeclaredRenderer when accessibility settings force the static
	// path, when a renderer fails to load, or on stub platforms.
	EffectiveRenderer string `json:"effective_renderer"`
	// DegradationReason explains why EffectiveRenderer < DeclaredRenderer.
	// Empty string means no degradation.
	DegradationReason string `json:"degradation_reason"`
}

// GetPetPackRuntimeInfo returns the selected pack's declared and effective
// renderer levels plus the degradation reason, for the settings page.
func (a *App) GetPetPackRuntimeInfo() PetPackRuntimeInfo {
	info := PetPackRuntimeInfo{
		PackID:  petpack.DefaultPackID,
		Variant: petpack.VariantDefault,
	}
	if a != nil {
		if cfg, err := a.LoadConfig(); err == nil {
			if skin := petpack.SanitizeSkinID(cfg.PetSkin, false, nil); skin != "" {
				info.PackID = skin
			}
			info.Variant = petpack.ResolveVariantForRuntime(cfg.PetVariant)
		}
	}
	reg := petpack.EnsureGlobal()
	if reg == nil {
		info.DegradationReason = "宠物包注册表不可用"
		return info
	}
	resolved, err := reg.Resolve(info.PackID, info.Variant)
	if err != nil || resolved == nil {
		info.DegradationReason = "宠物包解析失败，无法确定动画能力"
		return info
	}
	if resolved.VariantID != "" {
		info.Variant = resolved.VariantID
	}
	info.DeclaredRenderer = resolved.Renderer
	info.EffectiveRenderer = resolved.Renderer
	// A live pet window reports the level it is actually running at (renderer
	// load failures, quiet/reduced-motion). Without a window the declared
	// level is the best available answer.
	if a != nil {
		if fa := a.existingFloatingAssistant(); fa != nil {
			info.EffectiveRenderer, info.DegradationReason = fa.PetPackRuntimeLevel(resolved.Renderer)
		}
	}
	return info
}

// EnsurePetPackRegistryScanned forces registry init before config sanitize paths that need allowlist.
func EnsurePetPackRegistryScanned() {
	_ = petpack.EnsureGlobal()
}

var errPetRegistryUnavailable = errString("pet pack registry unavailable")

// errPetStoreHubCenterMissing is the single message every Pet Store binding
// returns when no HubCenter is configured. The hub serves no pet-store
// routes, so falling back to RemoteHubURL only produced guaranteed 404s.
var errPetStoreHubCenterMissing = errString("未配置 HubCenter，宠物市场不可用")

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
