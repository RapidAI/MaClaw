package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/mcp"
	coreskill "github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

const capabilityMarketMCPCatalogKey = "capability_market_mcp_catalog"
const capabilityMarketMCPPurchasesKey = "capability_market_mcp_purchases"
const capabilityMarketCustomerAccountKey = "capability_market_customer_account"
const capabilityMarketPublicBaseURLKey = "server_public_base_url"

type CapabilityMarketCustomerAccount struct {
	Status           string         `json:"status"`
	CustomerID       string         `json:"customer_id,omitempty"`
	HubID            string         `json:"hub_id,omitempty"`
	AdminEmail       string         `json:"admin_email,omitempty"`
	BillingEmail     string         `json:"billing_email,omitempty"`
	IdentitySource   string         `json:"identity_source,omitempty"`
	LoginURL         string         `json:"login_url,omitempty"`
	BillingPortalURL string         `json:"billing_portal_url,omitempty"`
	RenewalURL       string         `json:"renewal_url,omitempty"`
	Message          string         `json:"message,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
}

func CapabilityMarketCustomerAccountHandler(settings store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		account := loadCapabilityMarketCustomerAccount(r.Context(), settings)
		queryHubID := strings.TrimSpace(r.URL.Query().Get("hub_id"))
		queryEmail := strings.TrimSpace(firstCapabilityMarketNonEmpty(r.URL.Query().Get("admin_email"), r.URL.Query().Get("buyer_email"), r.URL.Query().Get("email")))
		if queryHubID != "" {
			account.HubID = queryHubID
		}
		if queryEmail != "" {
			account.AdminEmail = queryEmail
			if account.BillingEmail == "" {
				account.BillingEmail = queryEmail
			}
		}
		if account.AdminEmail == "" {
			account.AdminEmail = capabilityMarketSettingString(r.Context(), settings, "admin_email")
		}
		if account.BillingEmail == "" {
			account.BillingEmail = account.AdminEmail
		}
		if account.CustomerID == "" {
			account.CustomerID = firstCapabilityMarketNonEmpty(account.HubID, account.AdminEmail)
		}
		if account.IdentitySource == "" {
			if queryHubID != "" || queryEmail != "" {
				account.IdentitySource = "request"
			} else if account.CustomerID != "" || account.AdminEmail != "" {
				account.IdentitySource = "settings"
			}
		}
		baseURL := capabilityMarketSettingString(r.Context(), settings, capabilityMarketPublicBaseURLKey)
		if baseURL != "" {
			baseURL = strings.TrimRight(baseURL, "/")
			if account.LoginURL == "" {
				account.LoginURL = baseURL + "/admin"
			}
			if account.BillingPortalURL == "" {
				account.BillingPortalURL = baseURL + "/marketplace"
			}
			if account.RenewalURL == "" {
				account.RenewalURL = baseURL + "/marketplace"
			}
		}
		if account.Status == "" {
			if account.CustomerID != "" && account.AdminEmail != "" {
				account.Status = "configured"
			} else {
				account.Status = "not_configured"
				account.Message = "Hub customer account uses hub_id plus admin_email. Set admin_email or pass it in the billing request."
			}
		}
		writeJSON(w, http.StatusOK, account)
	}
}

type capabilityMarketSkillLicenseProvider interface {
	CapabilityMarketSkillLicenses(ctx context.Context, buyerEmail string) ([]CapabilityMarketLicenseRecord, error)
}

type CapabilityMarketLicenseRecord struct {
	CapabilityType string         `json:"capability_type"`
	CapabilityID   string         `json:"capability_id"`
	Source         string         `json:"source"`
	PurchaseID     string         `json:"purchase_id"`
	HubID          string         `json:"hub_id,omitempty"`
	BuyerEmail     string         `json:"buyer_email,omitempty"`
	AdminEmail     string         `json:"admin_email,omitempty"`
	VersionKey     string         `json:"version_key,omitempty"`
	Status         string         `json:"status"`
	Pricing        map[string]any `json:"pricing,omitempty"`
	License        map[string]any `json:"license,omitempty"`
	CreatedAt      string         `json:"created_at,omitempty"`
}

func CapabilityMarketBillingLicensesHandler(deps ...any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var repo store.SystemSettingsRepository
		var skillLicenses capabilityMarketSkillLicenseProvider
		for _, dep := range deps {
			if v, ok := dep.(store.SystemSettingsRepository); ok {
				repo = v
			}
			if v, ok := dep.(capabilityMarketSkillLicenseProvider); ok {
				skillLicenses = v
			}
		}
		purchases, err := loadCapabilityMarketMCPPurchases(r.Context(), repo)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MCP_PURCHASES_LOAD_FAILED", err.Error())
			return
		}
		hubID := strings.TrimSpace(r.URL.Query().Get("hub_id"))
		adminEmail := strings.TrimSpace(firstCapabilityMarketNonEmpty(r.URL.Query().Get("admin_email"), r.URL.Query().Get("buyer_email"), r.URL.Query().Get("email")))
		items := make([]CapabilityMarketLicenseRecord, 0, len(purchases.Items))
		for _, item := range purchases.Items {
			if hubID != "" && item.HubID != hubID {
				continue
			}
			if adminEmail != "" && !strings.EqualFold(item.AdminEmail, adminEmail) {
				continue
			}
			items = append(items, CapabilityMarketLicenseRecord{CapabilityType: corelib.CapabilityTypeMCP, CapabilityID: item.CapabilityID, Source: corelib.CapabilitySourceHubCenter, PurchaseID: item.PurchaseID, HubID: item.HubID, AdminEmail: item.AdminEmail, VersionKey: item.VersionKey, Status: item.Status, Pricing: item.Pricing, License: item.License, CreatedAt: item.CreatedAt})
		}
		if skillLicenses != nil {
			skillItems, err := skillLicenses.CapabilityMarketSkillLicenses(r.Context(), adminEmail)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "SKILL_PURCHASES_LOAD_FAILED", err.Error())
				return
			}
			items = append(items, skillItems...)
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

type CapabilityMarketMCPEntry struct {
	ID                 string                           `json:"id"`
	CapabilityType     string                           `json:"capability_type"`
	Publisher          string                           `json:"publisher"`
	CapabilityID       string                           `json:"capability_id"`
	DisplayName        string                           `json:"display_name"`
	Description        string                           `json:"description,omitempty"`
	Source             string                           `json:"source"`
	Status             string                           `json:"status"`
	Version            string                           `json:"version,omitempty"`
	VersionKey         string                           `json:"version_key,omitempty"`
	Pricing            map[string]any                   `json:"pricing,omitempty"`
	License            map[string]any                   `json:"license,omitempty"`
	MCP                corelib.MCPServerEntry           `json:"mcp"`
	SecretRequirements []CapabilityMCPSecretRequirement `json:"secret_requirements,omitempty"`
	UpdatedAt          string                           `json:"updated_at,omitempty"`
}

type CapabilityMCPSecretRequirement struct {
	Name          string         `json:"name"`
	Label         string         `json:"label,omitempty"`
	Scope         string         `json:"scope,omitempty"`
	StoragePolicy string         `json:"storage_policy,omitempty"`
	Required      *bool          `json:"required,omitempty"`
	HelpURL       string         `json:"help_url,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type capabilityMarketMCPCatalog struct {
	Items []CapabilityMarketMCPEntry `json:"items"`
}

type CapabilityMarketMCPPurchaseRecord struct {
	PurchaseID   string         `json:"purchase_id"`
	HubID        string         `json:"hub_id,omitempty"`
	AdminEmail   string         `json:"admin_email,omitempty"`
	RequestID    string         `json:"request_id,omitempty"`
	CapabilityID string         `json:"capability_id"`
	VersionKey   string         `json:"version_key,omitempty"`
	Pricing      map[string]any `json:"pricing,omitempty"`
	License      map[string]any `json:"license,omitempty"`
	Status       string         `json:"status"`
	CreatedAt    string         `json:"created_at"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

type capabilityMarketMCPPurchases struct {
	Items []CapabilityMarketMCPPurchaseRecord `json:"items"`
}

func CapabilityMarketMCPListHandler(settings store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		catalog, err := loadCapabilityMarketMCPCatalog(r.Context(), settings)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MCP_CATALOG_LOAD_FAILED", err.Error())
			return
		}
		q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
		items := make([]CapabilityMarketMCPEntry, 0, len(catalog.Items))
		for _, item := range catalog.Items {
			if strings.TrimSpace(item.Status) == "" {
				item.Status = "approved"
			}
			if item.Status != "approved" && r.URL.Query().Get("include_drafts") != "1" {
				continue
			}
			if q == "" || strings.Contains(strings.ToLower(item.DisplayName), q) || strings.Contains(strings.ToLower(item.Description), q) || strings.Contains(strings.ToLower(item.CapabilityID), q) {
				items = append(items, item)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func CapabilityMarketMCPDetailHandler(settings store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		catalog, err := loadCapabilityMarketMCPCatalog(r.Context(), settings)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MCP_CATALOG_LOAD_FAILED", err.Error())
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		for _, item := range catalog.Items {
			if item.ID == id || item.CapabilityID == id {
				writeJSON(w, http.StatusOK, item)
				return
			}
		}
		writeError(w, http.StatusNotFound, "MCP_CAPABILITY_NOT_FOUND", "MCP capability not found")
	}
}

type capabilityMarketMCPPurchaseRequest struct {
	HubID        string         `json:"hub_id,omitempty"`
	AdminEmail   string         `json:"admin_email,omitempty"`
	RequestID    string         `json:"request_id,omitempty"`
	BuyerContext map[string]any `json:"buyer_context,omitempty"`
}

func CapabilityMarketMCPPurchaseHandler(settings store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req capabilityMarketMCPPurchaseRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		catalog, err := loadCapabilityMarketMCPCatalog(r.Context(), settings)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MCP_CATALOG_LOAD_FAILED", err.Error())
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		for _, item := range catalog.Items {
			if item.ID != id && item.CapabilityID != id {
				continue
			}
			pricingMode := strings.ToLower(strings.TrimSpace(stringFromCapabilityMarketMap(item.Pricing, "mode")))
			if pricingMode == "" {
				pricingMode = corelib.CapabilityPricingFree
			}
			if pricingMode != corelib.CapabilityPricingFree && strings.TrimSpace(req.AdminEmail) == "" {
				writeError(w, http.StatusBadRequest, "ADMIN_EMAIL_REQUIRED", "admin_email is required for paid MCP purchase")
				return
			}
			purchaseID := "mcp_pur_" + time.Now().UTC().Format("20060102150405")
			record := CapabilityMarketMCPPurchaseRecord{
				PurchaseID:   purchaseID,
				HubID:        strings.TrimSpace(req.HubID),
				AdminEmail:   strings.TrimSpace(req.AdminEmail),
				RequestID:    strings.TrimSpace(req.RequestID),
				CapabilityID: item.CapabilityID,
				VersionKey:   item.VersionKey,
				Pricing:      item.Pricing,
				License:      item.License,
				Status:       "purchased",
				CreatedAt:    time.Now().UTC().Format(time.RFC3339),
			}
			if err := appendCapabilityMarketMCPPurchase(r.Context(), settings, record); err != nil {
				writeError(w, http.StatusInternalServerError, "MCP_PURCHASE_SAVE_FAILED", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"purchase_id": purchaseID,
				"status":      "purchased",
				"hub_id":      record.HubID,
				"admin_email": record.AdminEmail,
				"request_id":  record.RequestID,
				"pricing":     item.Pricing,
				"license":     item.License,
				"capability":  item,
			})
			return
		}
		writeError(w, http.StatusNotFound, "MCP_CAPABILITY_NOT_FOUND", "MCP capability not found")
	}
}

func AdminCapabilityMarketMCPUpsertHandler(settings store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if settings == nil {
			writeError(w, http.StatusInternalServerError, "SETTINGS_UNAVAILABLE", "system settings repository is unavailable")
			return
		}
		var req CapabilityMarketMCPEntry
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		item := normalizeCapabilityMarketMCPEntry(req)
		if item.CapabilityID == "" || item.DisplayName == "" {
			writeError(w, http.StatusBadRequest, "INVALID_MCP_CAPABILITY", "capability_id and display_name are required")
			return
		}
		catalog, err := loadCapabilityMarketMCPCatalog(r.Context(), settings)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MCP_CATALOG_LOAD_FAILED", err.Error())
			return
		}
		replaced := false
		for i := range catalog.Items {
			if catalog.Items[i].ID == item.ID || catalog.Items[i].CapabilityID == item.CapabilityID {
				catalog.Items[i] = item
				replaced = true
				break
			}
		}
		if !replaced {
			catalog.Items = append(catalog.Items, item)
		}
		if err := saveCapabilityMarketMCPCatalog(r.Context(), settings, catalog); err != nil {
			writeError(w, http.StatusInternalServerError, "MCP_CATALOG_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}

func AdminCapabilityMarketMCPDeleteHandler(settings store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if settings == nil {
			writeError(w, http.StatusInternalServerError, "SETTINGS_UNAVAILABLE", "system settings repository is unavailable")
			return
		}
		catalog, err := loadCapabilityMarketMCPCatalog(r.Context(), settings)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MCP_CATALOG_LOAD_FAILED", err.Error())
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		kept := catalog.Items[:0]
		deleted := false
		for _, item := range catalog.Items {
			if item.ID == id || item.CapabilityID == id {
				deleted = true
				continue
			}
			kept = append(kept, item)
		}
		if !deleted {
			writeError(w, http.StatusNotFound, "MCP_CAPABILITY_NOT_FOUND", "MCP capability not found")
			return
		}
		catalog.Items = kept
		if err := saveCapabilityMarketMCPCatalog(r.Context(), settings, catalog); err != nil {
			writeError(w, http.StatusInternalServerError, "MCP_CATALOG_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func AdminCapabilityMarketExternalSearchHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		source := corelib.NormalizeCapabilitySource(r.URL.Query().Get("source"))
		allowedSources := corelib.AdminMarketplaceSearchSources(corelib.CapabilityMarketplaceHostHubCenter)
		if source != "" && !corelib.AdminMarketplaceCanSearchSource(corelib.CapabilityMarketplaceHostHubCenter, source) {
			writeError(w, http.StatusForbidden, "SOURCE_NOT_ALLOWED", "source is not allowed for HubCenter marketplace admin search")
			return
		}
		capabilityType := corelib.NormalizeCapabilityType(r.URL.Query().Get("type"))
		if capabilityType == "" {
			capabilityType = corelib.CapabilityTypeSkill
		}
		sources := allowedSources
		if source != "" {
			sources = []string{source}
		}
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if query == "" {
			writeJSON(w, http.StatusOK, map[string]any{"allowed_sources": allowedSources, "items": []any{}})
			return
		}

		switch capabilityType {
		case corelib.CapabilityTypeSkill:
			results := coreskill.DefaultHubClient().SearchAllFiltered(r.Context(), "", query, sources)
			items := make([]map[string]any, 0, len(results))
			for _, result := range results {
				item := map[string]any{
					"id":              result.ID,
					"capability_id":   result.ID,
					"capability_type": corelib.CapabilityTypeSkill,
					"name":            result.Name,
					"display_name":    result.Name,
					"description":     result.Description,
					"version":         result.Version,
					"author":          result.Author,
					"trust_level":     result.TrustLevel,
					"avg_rating":      result.AvgRating,
					"downloads":       result.Downloads,
					"score":           result.Score,
					"source":          result.Source,
					"pricing":         corelib.CapabilityPricingFree,
				}
				if result.RepoURL != "" {
					item["repo_url"] = result.RepoURL
				}
				if result.FilePath != "" {
					item["file_path"] = result.FilePath
				}
				if result.InstallRef != "" {
					item["install_ref"] = result.InstallRef
				}
				items = append(items, item)
			}
			writeJSON(w, http.StatusOK, map[string]any{"allowed_sources": allowedSources, "items": items})

		case corelib.CapabilityTypeMCP:
			results := coreskill.DefaultHubClient().SearchMCPFiltered(r.Context(), query, sources)
			items := make([]map[string]any, 0, len(results))
			for _, result := range results {
				item := map[string]any{
					"id":              result.ID,
					"capability_id":   result.ID,
					"capability_type": corelib.CapabilityTypeMCP,
					"name":            result.Name,
					"display_name":    result.Name,
					"description":     result.Description,
					"version":         result.Version,
					"author":          result.Author,
					"source":          result.Source,
					"pricing":         corelib.CapabilityPricingFree,
				}
				if result.RepoURL != "" {
					item["repo_url"] = result.RepoURL
				}
				if result.InstallRef != "" {
					item["install_ref"] = result.InstallRef
				}
				items = append(items, item)
			}
			writeJSON(w, http.StatusOK, map[string]any{"allowed_sources": allowedSources, "items": items})

		default:
			writeJSON(w, http.StatusOK, map[string]any{"allowed_sources": allowedSources, "items": []any{}})
		}
	}
}

func loadCapabilityMarketMCPCatalog(ctx context.Context, settings store.SystemSettingsRepository) (capabilityMarketMCPCatalog, error) {
	catalog := capabilityMarketMCPCatalog{Items: []CapabilityMarketMCPEntry{}}
	if settings == nil {
		return catalog, nil
	}
	raw, err := settings.Get(ctx, capabilityMarketMCPCatalogKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return catalog, err
	}
	if err := json.Unmarshal([]byte(raw), &catalog); err != nil {
		return catalog, err
	}
	if catalog.Items == nil {
		catalog.Items = []CapabilityMarketMCPEntry{}
	}
	return catalog, nil
}

func loadCapabilityMarketMCPPurchases(ctx context.Context, settings store.SystemSettingsRepository) (capabilityMarketMCPPurchases, error) {
	purchases := capabilityMarketMCPPurchases{Items: []CapabilityMarketMCPPurchaseRecord{}}
	if settings == nil {
		return purchases, nil
	}
	raw, err := settings.Get(ctx, capabilityMarketMCPPurchasesKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return purchases, err
	}
	if err := json.Unmarshal([]byte(raw), &purchases); err != nil {
		return purchases, err
	}
	if purchases.Items == nil {
		purchases.Items = []CapabilityMarketMCPPurchaseRecord{}
	}
	return purchases, nil
}

func saveCapabilityMarketMCPPurchases(ctx context.Context, settings store.SystemSettingsRepository, purchases capabilityMarketMCPPurchases) error {
	if settings == nil {
		return nil
	}
	if purchases.Items == nil {
		purchases.Items = []CapabilityMarketMCPPurchaseRecord{}
	}
	data, err := json.Marshal(purchases)
	if err != nil {
		return err
	}
	return settings.Set(ctx, capabilityMarketMCPPurchasesKey, string(data))
}

func appendCapabilityMarketMCPPurchase(ctx context.Context, settings store.SystemSettingsRepository, record CapabilityMarketMCPPurchaseRecord) error {
	purchases, err := loadCapabilityMarketMCPPurchases(ctx, settings)
	if err != nil {
		return err
	}
	for i := range purchases.Items {
		if purchases.Items[i].PurchaseID == record.PurchaseID || (record.RequestID != "" && purchases.Items[i].RequestID == record.RequestID) {
			purchases.Items[i] = record
			return saveCapabilityMarketMCPPurchases(ctx, settings, purchases)
		}
	}
	purchases.Items = append(purchases.Items, record)
	return saveCapabilityMarketMCPPurchases(ctx, settings, purchases)
}

func saveCapabilityMarketMCPCatalog(ctx context.Context, settings store.SystemSettingsRepository, catalog capabilityMarketMCPCatalog) error {
	if catalog.Items == nil {
		catalog.Items = []CapabilityMarketMCPEntry{}
	}
	data, err := json.Marshal(catalog)
	if err != nil {
		return err
	}
	return settings.Set(ctx, capabilityMarketMCPCatalogKey, string(data))
}

func normalizeCapabilityMarketMCPEntry(item CapabilityMarketMCPEntry) CapabilityMarketMCPEntry {
	item.CapabilityType = corelib.CapabilityTypeMCP
	item.Source = corelib.CapabilitySourceHubCenter
	item.CapabilityID = firstCapabilityMarketNonEmpty(item.CapabilityID, item.MCP.ID, item.MCP.Name)
	item.DisplayName = firstCapabilityMarketNonEmpty(item.DisplayName, item.MCP.Name, item.CapabilityID)
	if strings.TrimSpace(item.ID) == "" {
		item.ID = item.CapabilityID
	}
	if strings.TrimSpace(item.Publisher) == "" {
		item.Publisher = "hubcenter"
	}
	if strings.TrimSpace(item.Status) == "" {
		item.Status = "approved"
	}
	if strings.TrimSpace(item.Version) == "" {
		item.Version = "1.0.0"
	}
	if strings.TrimSpace(item.VersionKey) == "" {
		item.VersionKey = item.Source + ":" + item.CapabilityType + ":" + item.Publisher + ":" + item.CapabilityID + "@" + item.Version
	}
	if strings.TrimSpace(item.MCP.ID) == "" {
		item.MCP.ID = item.CapabilityID
	}
	if strings.TrimSpace(item.MCP.Name) == "" {
		item.MCP.Name = item.DisplayName
	}
	if strings.TrimSpace(item.MCP.AuthType) == "" {
		item.MCP.AuthType = "none"
	}
	if item.MCP.Source == "" {
		item.MCP.Source = corelib.MCPSourceMarket
	}
	for i := range item.SecretRequirements {
		if strings.TrimSpace(item.SecretRequirements[i].Scope) == "" {
			item.SecretRequirements[i].Scope = "user"
		}
		if strings.TrimSpace(item.SecretRequirements[i].StoragePolicy) == "" {
			item.SecretRequirements[i].StoragePolicy = "hub_or_local"
		}
		if item.SecretRequirements[i].Required == nil {
			required := true
			item.SecretRequirements[i].Required = &required
		}
	}
	item.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return item
}

func loadCapabilityMarketCustomerAccount(ctx context.Context, settings store.SystemSettingsRepository) CapabilityMarketCustomerAccount {
	account := CapabilityMarketCustomerAccount{}
	raw := capabilityMarketSettingRaw(ctx, settings, capabilityMarketCustomerAccountKey)
	if strings.TrimSpace(raw) == "" {
		return account
	}
	if err := json.Unmarshal([]byte(raw), &account); err == nil {
		return account
	}
	var wrapped struct {
		Value CapabilityMarketCustomerAccount `json:"value"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapped); err == nil {
		return wrapped.Value
	}
	return account
}

func capabilityMarketSettingString(ctx context.Context, settings store.SystemSettingsRepository, key string) string {
	raw := capabilityMarketSettingRaw(ctx, settings, key)
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	var wrapped struct {
		Value string `json:"value"`
	}
	if json.Unmarshal([]byte(raw), &wrapped) == nil && strings.TrimSpace(wrapped.Value) != "" {
		return strings.TrimSpace(wrapped.Value)
	}
	var value string
	if json.Unmarshal([]byte(raw), &value) == nil {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(raw)
}

func capabilityMarketSettingRaw(ctx context.Context, settings store.SystemSettingsRepository, key string) string {
	if settings == nil || strings.TrimSpace(key) == "" {
		return ""
	}
	raw, err := settings.Get(ctx, key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(raw)
}
func stringFromCapabilityMarketMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if value, ok := m[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func firstCapabilityMarketNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}


// AdminMCPValidateHandler validates an MCP Server's connectivity, tool availability,
// schema correctness, and runtime health. Returns a combined ValidationReport.
func AdminMCPValidateHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			EndpointURL string            `json:"endpoint_url"`
			Transport   string            `json:"transport"`
			Headers     map[string]string `json:"headers,omitempty"`
			APIKey      string            `json:"api_key,omitempty"`
			Checks      []string          `json:"checks"` // ["all"] or subset of ["connectivity","tools","schema","health"]
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		if strings.TrimSpace(req.EndpointURL) == "" {
			writeError(w, http.StatusBadRequest, "MISSING_ENDPOINT", "endpoint_url is required")
			return
		}

		config := mcp.MCPServerConfig{
			EndpointURL: strings.TrimSpace(req.EndpointURL),
			Transport:   req.Transport,
			Headers:     req.Headers,
			APIKey:      req.APIKey,
		}

		validator := mcp.NewValidator()
		report, err := validator.Validate(r.Context(), config)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "VALIDATION_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, report)
	}
}

// AdminCapabilityMarketImportHandler imports a capability (Skill or MCP) from an
// external source (ClawHub/GitHub) into HubCenter as a free capability package.
func AdminCapabilityMarketImportHandler(settings store.SystemSettingsRepository, skillStore *skill.SkillStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			CapabilityType string `json:"capability_type"` // "skill" | "mcp"
			Source         string `json:"source"`          // "clawhub" | "github"
			InstallRef     string `json:"install_ref"`     // source-specific install reference
			RunValidation  bool   `json:"run_validation"`  // run MCP validation before import
			DisplayName    string `json:"display_name"`
			Description    string `json:"description"`
			EndpointURL    string `json:"endpoint_url,omitempty"` // for MCP imports
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}

		capType := corelib.NormalizeCapabilityType(req.CapabilityType)
		source := corelib.NormalizeCapabilitySource(req.Source)

		if capType == "" {
			writeError(w, http.StatusBadRequest, "MISSING_TYPE", "capability_type is required (skill or mcp)")
			return
		}
		if source == "" || source == corelib.CapabilitySourceHubCenter {
			writeError(w, http.StatusBadRequest, "INVALID_SOURCE", "source must be clawhub or github (cannot import from self)")
			return
		}
		if strings.TrimSpace(req.InstallRef) == "" && strings.TrimSpace(req.EndpointURL) == "" {
			writeError(w, http.StatusBadRequest, "MISSING_REF", "install_ref or endpoint_url is required")
			return
		}

		result := map[string]any{
			"capability_type": capType,
			"source":          source,
			"pricing":         corelib.CapabilityPricingFree,
			"imported_at":     time.Now().UTC().Format(time.RFC3339),
		}

		switch capType {
		case corelib.CapabilityTypeSkill:
			// Download and register skill from external source.
			client := coreskill.DefaultHubClient()
			var entry *corelib.NLSkillEntry
			var err error
			switch source {
			case corelib.CapabilitySourceClawHub:
				entry, err = client.DownloadClawHub(r.Context(), req.InstallRef)
			case corelib.CapabilitySourceGitHub:
				entry, err = client.DownloadGitHub(r.Context(), req.InstallRef)
			default:
				writeError(w, http.StatusBadRequest, "UNSUPPORTED_SOURCE", "unsupported source for skill import: "+source)
				return
			}
			if err != nil {
				writeError(w, http.StatusBadGateway, "DOWNLOAD_FAILED", "failed to download skill: "+err.Error())
				return
			}
			if entry != nil {
				result["capability_id"] = entry.Name
				result["display_name"] = entry.Name
				result["description"] = entry.Description
				result["version"] = entry.HubVersion

				// Persist to SkillStore so it appears in the Capability Catalog.
				if skillStore != nil {
					now := time.Now().UTC().Format(time.RFC3339)
					// Ensure SourceProject is always set for dedup.
					sourceURL := entry.SourceProject
					if sourceURL == "" {
						sourceURL = source + ":" + req.InstallRef
					}
					hubSkill := skill.HubSkillFull{
						HubSkillMeta: skill.HubSkillMeta{
							ID:          skill.GenerateImportID(),
							Name:        entry.Name,
							Description: entry.Description,
							Version:     entry.HubVersion,
							TrustLevel:  entry.TrustLevel,
							CreatedAt:   now,
							UpdatedAt:   now,
							Visible:     true,
							Status:      "published",
							SourceURL:   sourceURL,
						},
						Triggers: entry.Triggers,
					}
					if hubSkill.TrustLevel == "" {
						hubSkill.TrustLevel = "community"
					}
					// Convert NLSkillStep → HubSkillStep
					for _, step := range entry.Steps {
						hubSkill.Steps = append(hubSkill.Steps, skill.HubSkillStep{
							Action:  step.Action,
							Params:  step.Params,
							OnError: step.OnError,
						})
					}
					// Check for duplicate by source URL + name
					if existing := skillStore.FindBySourceURL(sourceURL, entry.Name); existing != nil {
						hubSkill.ID = existing.ID
						hubSkill.CreatedAt = existing.CreatedAt
						hubSkill.Downloads = existing.Downloads
						hubSkill.DownloadCount = existing.DownloadCount
						hubSkill.RatingSum = existing.RatingSum
						hubSkill.RatingCount = existing.RatingCount
						hubSkill.AvgRating = existing.AvgRating
					}
					if err := skillStore.Publish(hubSkill); err != nil {
						result["publish_warning"] = "downloaded but failed to publish to catalog: " + err.Error()
					} else {
						result["published"] = true
						result["skill_id"] = hubSkill.ID
					}
				}
			}

		case corelib.CapabilityTypeMCP:
			// Register MCP configuration.
			capID := strings.TrimSpace(req.InstallRef)
			if capID == "" {
				capID = strings.TrimSpace(req.DisplayName)
			}
			result["capability_id"] = capID
			result["display_name"] = req.DisplayName
			result["description"] = req.Description

			// Run MCP validation if requested.
			if req.RunValidation && strings.TrimSpace(req.EndpointURL) != "" {
				config := mcp.MCPServerConfig{
					EndpointURL: strings.TrimSpace(req.EndpointURL),
					Transport:   "streamable-http",
				}
				validator := mcp.NewValidator()
				report, _ := validator.Validate(r.Context(), config)
				if report != nil {
					result["validation_report"] = report
					if report.OverallStatus == "fail" {
						result["validation_status"] = "failed"
					} else {
						result["validation_status"] = "passed"
					}
				}
			}

			// Store in MCP catalog (normalize entry to fill defaults).
			if settings != nil {
				catalog, _ := loadCapabilityMarketMCPCatalog(r.Context(), settings)
				entry := normalizeCapabilityMarketMCPEntry(CapabilityMarketMCPEntry{
					ID:             capID,
					CapabilityID:   capID,
					CapabilityType: corelib.CapabilityTypeMCP,
					DisplayName:    req.DisplayName,
					Description:    req.Description,
					Source:         source,
					Pricing:        map[string]any{"mode": corelib.CapabilityPricingFree},
					MCP: corelib.MCPServerEntry{
						EndpointURL: strings.TrimSpace(req.EndpointURL),
						Name:        req.DisplayName,
					},
				})
				// Override source to preserve the external origin (normalizeCapabilityMarketMCPEntry sets it to hubcenter).
				entry.Source = source
				// Upsert: replace if exists, append if new.
				replaced := false
				for i := range catalog.Items {
					if catalog.Items[i].CapabilityID == capID {
						catalog.Items[i] = entry
						replaced = true
						break
					}
				}
				if !replaced {
					catalog.Items = append(catalog.Items, entry)
				}
				_ = saveCapabilityMarketMCPCatalog(r.Context(), settings, catalog)
			}

		default:
			writeError(w, http.StatusBadRequest, "UNSUPPORTED_TYPE", "unsupported capability_type: "+capType)
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}
