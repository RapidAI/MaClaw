package main

import (
	"bytes"
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
)

const (
	expertMarketMaxArchiveBytes      = 8 << 20
	expertMarketMultipartOverheadMax = 128 << 10
	expertMarketMaxRequestBytes      = expertMarketMaxArchiveBytes + expertMarketMultipartOverheadMax
	expertMarketMaxResponseBytes     = 9 << 20
)

// expertMarketRequest uses the same native-only authenticated channel as the
// Pet Store. Reusing this boundary means neither the HubCenter session nor an
// installable package crosses into the WebView.
func (a *App) expertMarketRequest(method, path string, body io.Reader, contentType string) ([]byte, error) {
	return a.marketRequest(method, path, body, contentType, expertMarketMaxRequestBytes, expertMarketMaxResponseBytes, "Expert Market")
}

func (a *App) ListExpertMarketListings(query, sort string, page, pageSize int) (map[string]interface{}, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 30 {
		pageSize = 20
	}
	values := url.Values{}
	values.Set("q", strings.TrimSpace(query))
	values.Set("sort", strings.TrimSpace(sort))
	values.Set("page", strconv.Itoa(page))
	values.Set("page_size", strconv.Itoa(pageSize))
	data, err := a.expertMarketRequest(http.MethodGet, "/api/v1/expert-market/experts?"+values.Encode(), nil, "")
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	a.markExpertMarketListingsInstalled(result, "experts")
	return result, nil
}

func (a *App) GetExpertMarketAccount() (map[string]interface{}, error) {
	data, err := a.expertMarketRequest(http.MethodGet, "/api/v1/expert-market/account", nil, "")
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	// HubCenter stores a privacy-preserving package identity rather than the
	// desktop record id. Resolve it locally for the owner-facing Utilities card
	// actions; this field never crosses the network or appears in public lists.
	localExperts, _, err := defaultExpertStore.List()
	if err != nil {
		return result, nil
	}
	byPackageID := make(map[string]string, len(localExperts))
	for _, expert := range localExperts {
		if expert.Builtin || strings.TrimSpace(expert.ID) == "" {
			continue
		}
		byPackageID[expertPackageIdentity(expert)] = expert.ID
	}
	uploads, _ := result["uploads"].([]interface{})
	for _, raw := range uploads {
		listing, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if localID := byPackageID[strings.TrimSpace(fmt.Sprint(listing["source_expert_id"]))]; localID != "" {
			listing["local_expert_id"] = localID
		}
	}
	// A market install is a receiver-owned local state. Enrich both account
	// collections so the UI can distinguish an entitlement from an installed
	// copy and offer the correct action.
	a.markExpertMarketListingsInstalled(result, "uploads", "purchases")
	return result, nil
}

// markExpertMarketListingsInstalled annotates server listings with the local
// package-identity expert, without sending device-specific data to HubCenter.
// The same helper is used for Explore and My library so both surfaces agree.
func (a *App) markExpertMarketListingsInstalled(result map[string]interface{}, keys ...string) {
	if result == nil {
		return
	}
	installed, err := defaultExpertStore.ListMarketInstallIDs()
	if err != nil {
		return
	}
	if len(installed) == 0 {
		return
	}
	for _, key := range keys {
		items, _ := result[key].([]interface{})
		for _, raw := range items {
			listing, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			packageID := strings.TrimSpace(fmt.Sprint(listing["source_expert_id"]))
			if installed[packageID] {
				listing["installed"] = true
				listing["local_expert_id"] = packageID
			}
		}
	}
}

func (a *App) PurchaseExpertMarketListing(id string) (map[string]interface{}, error) {
	data, err := a.expertMarketRequest(http.MethodPost, "/api/v1/expert-market/experts/"+url.PathEscape(strings.TrimSpace(id))+"/purchase", nil, "")
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	return result, json.Unmarshal(data, &result)
}

// InstallExpertMarketListing downloads an entitlement into a staging file and
// calls the existing hardened package importer. A successful install returns
// the imported Expert definition and any dependency installation summary.
func (a *App) InstallExpertMarketListing(id string) (*ExpertPackageImportResult, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("expert market listing id is required")
	}
	report := func(status, stage string, result *ExpertPackageImportResult, installErr error) {
		payload := map[string]string{"status": status, "failure_stage": stage}
		if result != nil {
			payload["local_expert_id"] = result.Expert.ID
		}
		if installErr != nil {
			payload["error_message"] = installErr.Error()
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return
		}
		_, _ = a.expertMarketRequest(http.MethodPost, "/api/v1/expert-market/experts/"+url.PathEscape(id)+"/installations", bytes.NewReader(body), "application/json")
	}
	data, err := a.expertMarketRequest(http.MethodGet, "/api/v1/expert-market/experts/"+url.PathEscape(id)+"/download", nil, "")
	if err != nil {
		report("failed", "download", nil, err)
		return nil, err
	}
	if len(data) == 0 || len(data) > 8<<20 {
		err := fmt.Errorf("invalid expert market package size")
		report("failed", "download", nil, err)
		return nil, err
	}
	dir := filepath.Join(a.GetTempDir(), "expert-market")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		report("failed", "download", nil, err)
		return nil, err
	}
	file, err := os.CreateTemp(dir, "download-*.zip")
	if err != nil {
		report("failed", "download", nil, err)
		return nil, err
	}
	path := file.Name()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		report("failed", "download", nil, err)
		return nil, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		report("failed", "download", nil, err)
		return nil, err
	}
	defer os.Remove(path)
	result, err := a.importExpertPackageFromFile(path, true)
	if err != nil {
		stage := "create_local_expert"
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "package") || strings.Contains(lower, "archive") || strings.Contains(lower, "manifest") || strings.Contains(lower, "zip") {
			stage = "validate"
		} else if strings.Contains(lower, "skill") || strings.Contains(lower, "dependenc") {
			stage = "dependencies"
		}
		report("failed", stage, nil, err)
		return nil, err
	}
	report("installed", "", result, nil)
	return result, nil
}

// UninstallExpertMarketListing removes only the local definition installed
// from this listing. Market entitlements and declared skills are retained:
// they may be shared with other experts and the user can reinstall later.
// Do not create a Hub tombstone here: this is a device-local install state,
// not a request to delete a user's cloud-synced expert definition.
func (a *App) UninstallExpertMarketListing(localExpertID string) error {
	localExpertID = strings.TrimSpace(localExpertID)
	if !strings.HasPrefix(localExpertID, expertPackageIDPrefix) {
		return fmt.Errorf("expert market installation id is invalid")
	}
	if _, ok, err := defaultExpertStore.Get(localExpertID); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("installed expert was not found")
	}
	if installed, err := defaultExpertStore.IsMarketInstall(localExpertID); err != nil {
		return err
	} else if !installed {
		return fmt.Errorf("expert was not installed from the market")
	}
	if err := defaultExpertStore.MarkLocalOnly(localExpertID); err != nil {
		return err
	}
	if err := defaultExpertStore.Delete(localExpertID, false); err != nil {
		return err
	}
	invalidateExpertDefCache(localExpertID)
	return nil
}

// SubmitExpertMarketListing exports a local custom Expert only after the
// caller has selected it in the native UI. Builtins are rejected by the
// existing package exporter, and the package never crosses into the WebView.
func (a *App) SubmitExpertMarketListing(expertID, version string, price int64, visibility string) (map[string]interface{}, error) {
	if price < 0 || price > 999999 {
		return nil, fmt.Errorf("price must be 0-999999 credits")
	}
	visibility = strings.ToLower(strings.TrimSpace(visibility))
	if visibility == "" {
		visibility = "public"
	}
	if visibility != "public" && visibility != "private" {
		return nil, fmt.Errorf("visibility must be public or private")
	}
	dir := filepath.Join(a.GetDataDir(), "expert-market-drafts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	draft, err := os.CreateTemp(dir, "expert-market-*.zip")
	if err != nil {
		return nil, err
	}
	path := draft.Name()
	if err := draft.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	if err := a.ExportExpertPackageToFile(expertID, path); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	defer os.Remove(path)
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("version", strings.TrimSpace(version)); err != nil {
		return nil, err
	}
	if err := mw.WriteField("price", strconv.FormatInt(price, 10)); err != nil {
		return nil, err
	}
	if err := mw.WriteField("visibility", visibility); err != nil {
		return nil, err
	}
	part, err := mw.CreateFormFile("package", filepath.Base(path))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	data, err := a.expertMarketRequest(http.MethodPost, "/api/v1/expert-market/experts", &body, mw.FormDataContentType())
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	return result, json.Unmarshal(data, &result)
}

func (a *App) WithdrawExpertMarketListing(id string) error {
	_, err := a.expertMarketRequest(http.MethodPost, "/api/v1/expert-market/experts/"+url.PathEscape(strings.TrimSpace(id))+"/withdraw", nil, "")
	return err
}

// DeletePrivateExpertMarketListing permanently removes an owner-only share.
func (a *App) DeletePrivateExpertMarketListing(id string) error {
	_, err := a.expertMarketRequest(http.MethodDelete, "/api/v1/expert-market/experts/"+url.PathEscape(strings.TrimSpace(id))+"/private", nil, "")
	return err
}

func (a *App) MakeExpertMarketListingPrivate(id string) (map[string]interface{}, error) {
	data, err := a.expertMarketRequest(http.MethodPost, "/api/v1/expert-market/experts/"+url.PathEscape(strings.TrimSpace(id))+"/make-private", nil, "")
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	return result, json.Unmarshal(data, &result)
}

// PublishExpertMarketListing changes an owner-only package to public. HubCenter
// records the transition and places it back into the moderation queue.
func (a *App) PublishExpertMarketListing(id string) (map[string]interface{}, error) {
	data, err := a.expertMarketRequest(http.MethodPost, "/api/v1/expert-market/experts/"+url.PathEscape(strings.TrimSpace(id))+"/publish", nil, "")
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	return result, json.Unmarshal(data, &result)
}
