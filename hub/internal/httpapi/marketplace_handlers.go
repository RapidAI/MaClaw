package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	coreskill "github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/capability"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const capabilityMarketPolicySettingKey = "capability_market_policy"

func MarketplacePageHandler(product string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		_, _ = w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><title>Capability Marketplace</title><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{font-family:Segoe UI,Arial,sans-serif;margin:0;background:#f7f8fb;color:#20242c}.wrap{max-width:880px;margin:48px auto;padding:0 24px}.panel{background:#fff;border:1px solid #e3e7ef;border-radius:8px;padding:28px;box-shadow:0 10px 30px rgba(24,35,60,.06)}h1{margin:0 0 12px;font-size:28px}.muted{color:#657084;line-height:1.6}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:12px;margin-top:22px}.item{border:1px solid #e6eaf2;border-radius:8px;padding:14px;background:#fbfcff}.item b{display:block;margin-bottom:6px}</style></head><body><main class="wrap"><section class="panel"><h1>Capability Marketplace</h1><p class="muted">This endpoint is reserved for the unified Skill and MCP marketplace. The API is available under <code>/api/capabilities</code>; the full web UI can be mounted here.</p><div class="grid"><div class="item"><b>Skill</b><span class="muted">Packages and executable abilities.</span></div><div class="item"><b>MCP</b><span class="muted">Server configs, tools, and secret bindings.</span></div><div class="item"><b>Enterprise</b><span class="muted">Managed deployments and recommendations.</span></div></div></section></main></body></html>`))
	}
}

func CapabilityListHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := svc.List(r.Context(), r.URL.Query().Get("type"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func CapabilityDetailHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_CAPABILITY_ID", "capability id is required")
			return
		}
		item, err := svc.Get(r.Context(), id)
		if errors.Is(err, capability.ErrNotFound) {
			writeError(w, http.StatusNotFound, "CAPABILITY_NOT_FOUND", "capability not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_GET_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}

func CapabilityVersionsHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_CAPABILITY_ID", "capability id is required")
			return
		}
		items, err := svc.ListVersions(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_VERSIONS_LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

type capabilityInstallIntentRequest struct {
	CapabilityID   string          `json:"capability_id"`
	CapabilityType string          `json:"capability_type"`
	DisplayName    string          `json:"display_name,omitempty"`
	Description    string          `json:"description,omitempty"`
	Version        string          `json:"version,omitempty"`
	Source         string          `json:"source"`
	Pricing        string          `json:"pricing,omitempty"`
	Price          json.RawMessage `json:"price,omitempty"`
	License        json.RawMessage `json:"license,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	UserReason     string          `json:"user_reason,omitempty"`
}

type capabilityUpsertRequest struct {
	CapabilityType    string          `json:"capability_type"`
	Publisher         string          `json:"publisher"`
	CapabilityID      string          `json:"capability_id"`
	DisplayName       string          `json:"display_name"`
	Description       string          `json:"description,omitempty"`
	Source            string          `json:"source,omitempty"`
	ManagedBy         string          `json:"managed_by,omitempty"`
	Status            string          `json:"status,omitempty"`
	RelationToOrigin  string          `json:"relation_to_origin,omitempty"`
	GlobalKey         string          `json:"global_key,omitempty"`
	OriginKey         string          `json:"origin_key,omitempty"`
	Origin            json.RawMessage `json:"origin,omitempty"`
	Provenance        json.RawMessage `json:"provenance,omitempty"`
	Metadata          json.RawMessage `json:"metadata,omitempty"`
	Version           string          `json:"version,omitempty"`
	VersionKey        string          `json:"version_key,omitempty"`
	PackageURL        string          `json:"package_url,omitempty"`
	PackageChecksum   string          `json:"package_checksum,omitempty"`
	PackageSignature  string          `json:"package_signature,omitempty"`
	Manifest          json.RawMessage `json:"manifest,omitempty"`
	TypeConfig        json.RawMessage `json:"type_config,omitempty"`
	Permissions       json.RawMessage `json:"permissions,omitempty"`
	Pricing           json.RawMessage `json:"pricing,omitempty"`
	License           json.RawMessage `json:"license,omitempty"`
	Compatibility     json.RawMessage `json:"compatibility,omitempty"`
	VersionStatus     string          `json:"version_status,omitempty"`
	SetCurrentVersion bool            `json:"set_current_version"`
}

func AdminCapabilityUpsertHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req capabilityUpsertRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		if strings.TrimSpace(req.CapabilityID) == "" || strings.TrimSpace(req.DisplayName) == "" {
			writeError(w, http.StatusBadRequest, "INVALID_CAPABILITY", "capability_id and display_name are required")
			return
		}
		item, err := svc.UpsertCapability(r.Context(), capability.UpsertCapabilityInput{
			CapabilityType:    req.CapabilityType,
			Publisher:         req.Publisher,
			CapabilityID:      req.CapabilityID,
			DisplayName:       req.DisplayName,
			Description:       req.Description,
			Source:            req.Source,
			ManagedBy:         req.ManagedBy,
			Status:            req.Status,
			RelationToOrigin:  req.RelationToOrigin,
			GlobalKey:         req.GlobalKey,
			OriginKey:         req.OriginKey,
			OriginJSON:        rawJSONOrDefault(req.Origin),
			ProvenanceJSON:    rawJSONOrDefault(req.Provenance),
			MetadataJSON:      rawJSONOrDefault(req.Metadata),
			Version:           req.Version,
			VersionKey:        req.VersionKey,
			PackageURL:        req.PackageURL,
			PackageChecksum:   req.PackageChecksum,
			PackageSignature:  req.PackageSignature,
			ManifestJSON:      rawJSONOrDefault(req.Manifest),
			TypeConfigJSON:    rawJSONOrDefault(req.TypeConfig),
			PermissionsJSON:   rawJSONOrDefault(req.Permissions),
			PricingJSON:       rawJSONOrDefault(req.Pricing),
			LicenseJSON:       rawJSONOrDefault(req.License),
			CompatibilityJSON: rawJSONOrDefault(req.Compatibility),
			VersionStatus:     req.VersionStatus,
			SetCurrentVersion: req.SetCurrentVersion || req.Version != "",
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}

type adminMCPMarketplaceUpsertRequest struct {
	Publisher          string                          `json:"publisher"`
	CapabilityID       string                          `json:"capability_id"`
	DisplayName        string                          `json:"display_name"`
	Description        string                          `json:"description,omitempty"`
	Version            string                          `json:"version,omitempty"`
	VersionKey         string                          `json:"version_key,omitempty"`
	Status             string                          `json:"status,omitempty"`
	MCP                corelib.MCPServerEntry          `json:"mcp"`
	Pricing            json.RawMessage                 `json:"pricing,omitempty"`
	License            json.RawMessage                 `json:"license,omitempty"`
	SecretRequirements []adminMCPSecretRequirementSpec `json:"secret_requirements,omitempty"`
}

type adminMCPSecretRequirementSpec struct {
	Name          string          `json:"name"`
	Label         string          `json:"label,omitempty"`
	Scope         string          `json:"scope,omitempty"`
	StoragePolicy string          `json:"storage_policy,omitempty"`
	Required      *bool           `json:"required,omitempty"`
	HelpURL       string          `json:"help_url,omitempty"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
}

func AdminMCPMarketplaceUpsertHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req adminMCPMarketplaceUpsertRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		if strings.TrimSpace(req.CapabilityID) == "" {
			req.CapabilityID = firstNonEmpty(req.MCP.ID, req.MCP.Name)
		}
		if strings.TrimSpace(req.DisplayName) == "" {
			req.DisplayName = firstNonEmpty(req.MCP.Name, req.CapabilityID)
		}
		if strings.TrimSpace(req.CapabilityID) == "" || strings.TrimSpace(req.DisplayName) == "" {
			writeError(w, http.StatusBadRequest, "INVALID_MCP_CAPABILITY", "capability_id or mcp.id/name is required")
			return
		}
		if strings.TrimSpace(req.MCP.EndpointURL) == "" {
			writeError(w, http.StatusBadRequest, "INVALID_MCP_CAPABILITY", "mcp.endpoint_url is required for marketplace MCP capabilities")
			return
		}
		if strings.TrimSpace(req.Version) == "" {
			req.Version = "1.0.0"
		}
		metadata := map[string]any{
			"server_id":    firstNonEmpty(req.MCP.ID, req.CapabilityID),
			"name":         firstNonEmpty(req.MCP.Name, req.DisplayName),
			"endpoint_url": strings.TrimSpace(req.MCP.EndpointURL),
			"auth_type":    firstNonEmpty(req.MCP.AuthType, "none"),
		}
		if len(req.MCP.Headers) > 0 {
			metadata["headers"] = req.MCP.Headers
		}
		manifest := map[string]any{
			"name":        req.DisplayName,
			"description": req.Description,
			"type":        corelib.CapabilityTypeMCP,
		}
		item, err := svc.UpsertCapability(r.Context(), capability.UpsertCapabilityInput{
			CapabilityType:    corelib.CapabilityTypeMCP,
			Publisher:         req.Publisher,
			CapabilityID:      req.CapabilityID,
			DisplayName:       req.DisplayName,
			Description:       req.Description,
			Source:            corelib.CapabilitySourceEnterpriseHub,
			ManagedBy:         "admin",
			Status:            firstNonEmpty(req.Status, "approved"),
			MetadataJSON:      jsonObjectString(metadata),
			Version:           req.Version,
			VersionKey:        req.VersionKey,
			ManifestJSON:      jsonObjectString(manifest),
			TypeConfigJSON:    jsonObjectString(req.MCP),
			PricingJSON:       rawJSONOrDefault(req.Pricing),
			LicenseJSON:       rawJSONOrDefault(req.License),
			SetCurrentVersion: true,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MCP_CAPABILITY_SAVE_FAILED", err.Error())
			return
		}
		for _, spec := range req.SecretRequirements {
			if strings.TrimSpace(spec.Name) == "" {
				continue
			}
			required := true
			if spec.Required != nil {
				required = *spec.Required
			}
			if _, err := svc.UpsertMCPSecretRequirement(r.Context(), capability.MCPSecretRequirementInput{CapabilityRef: item.ID, VersionKey: item.CurrentVersionKey, Name: spec.Name, Label: spec.Label, Scope: spec.Scope, StoragePolicy: spec.StoragePolicy, Required: required, HelpURL: spec.HelpURL, MetadataJSON: rawJSONOrDefault(spec.Metadata)}); err != nil {
				writeError(w, http.StatusInternalServerError, "MCP_SECRET_REQUIREMENT_SAVE_FAILED", err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, item)
	}
}

func CapabilityInstallIntentHandler(svc *capability.Service, settings store.SystemSettingsRepository, centerStatuses ...capabilityMarketCenterStatusProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req capabilityInstallIntentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		if req.CapabilityID == "" {
			req.CapabilityID = r.PathValue("id")
		}
		policy := corelib.DefaultCapabilityMarketPolicy()
		if settings != nil {
			if loaded, err := loadCapabilityMarketPolicy(r, settings); err == nil {
				policy = loaded
			}
		}
		var enterpriseItem *capability.CapabilitySummary
		if item, err := findEnterpriseCapabilityForInstallIntent(r.Context(), svc, req); err == nil {
			enterpriseItem = item
		}
		existsInEnterprise := enterpriseItem != nil && enterpriseItem.ID != ""
		decision := corelib.DecideCapabilityInstall(corelib.CapabilityInstallDecisionInput{
			Policy:             policy,
			ExistsInEnterprise: existsInEnterprise,
			Source:             req.Source,
			Pricing:            req.Pricing,
			ExternalInstallOK:  true,
			ExternalPurchaseOK: true,
		})
		resp := map[string]any{"action": decision.Action, "reason": decision.Reason}
		switch decision.Action {
		case corelib.CapabilityInstallFromEnterpriseHub:
			if enterpriseItem != nil {
				resp["capability"] = enterpriseItem
			}
			writeJSON(w, http.StatusOK, resp)
			return
		case corelib.CapabilityInstallCreateImportRequest, corelib.CapabilityInstallCreatePurchaseRequest:
			kind := "import"
			if decision.Action == corelib.CapabilityInstallCreatePurchaseRequest {
				kind = "purchase"
			}
			if existing := findOpenAcquisitionRequest(r.Context(), svc, req, kind); existing != nil {
				resp["request_id"] = existing.ID
				writeJSON(w, http.StatusAccepted, resp)
				return
			}
			requestID, err := svc.CreateAcquisitionRequest(r.Context(), capability.AcquisitionRequestInput{
				CapabilityType:      req.CapabilityType,
				Source:              req.Source,
				SourceCapabilityKey: req.CapabilityID,
				SourceVersionKey:    req.Version,
				RequestKind:         kind,
				Reason:              req.UserReason,
				PriceJSON:           rawJSONOrDefault(req.Price),
				LicenseJSON:         rawJSONOrDefault(req.License),
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "ACQUISITION_REQUEST_FAILED", err.Error())
				return
			}
			resp["request_id"] = requestID
			if decision.Action == corelib.CapabilityInstallCreateImportRequest && req.Source == corelib.CapabilitySourceHubCenter {
				var centerStatus capabilityMarketCenterStatusProvider
				if len(centerStatuses) > 0 {
					centerStatus = centerStatuses[0]
				}
				created, err := importFreeHubCenterCapabilityForIntent(r.Context(), svc, centerStatus, req)
				if err != nil {
					writeError(w, http.StatusBadGateway, "HUBCENTER_IMPORT_FAILED", err.Error())
					return
				}
				if created != nil {
					_ = svc.CompleteAcquisitionRequest(r.Context(), requestID, created.ID, jsonObjectString(map[string]any{"mode": "free_import", "source": corelib.CapabilitySourceHubCenter}))
					resp["capability"] = created
					writeJSON(w, http.StatusOK, resp)
					return
				}
			}
			writeJSON(w, http.StatusAccepted, resp)
			return
		case corelib.CapabilityInstallBlocked:
			writeJSON(w, http.StatusForbidden, resp)
			return
		case corelib.CapabilityInstallExternalDirect:
			if req.Source == corelib.CapabilitySourceHubCenter && len(centerStatuses) > 0 {
				var centerStatus capabilityMarketCenterStatusProvider
				centerStatus = centerStatuses[0]
				created, err := importFreeHubCenterCapabilityForIntent(r.Context(), svc, centerStatus, req)
				if err != nil {
					writeError(w, http.StatusBadGateway, "HUBCENTER_IMPORT_FAILED", err.Error())
					return
				}
				if created != nil {
					resp["capability"] = created
					writeJSON(w, http.StatusOK, resp)
					return
				}
			}
			writeJSON(w, http.StatusOK, resp)
			return
		default:
			writeJSON(w, http.StatusOK, resp)
			return
		}
	}
}
func findEnterpriseCapabilityForInstallIntent(ctx context.Context, svc *capability.Service, req capabilityInstallIntentRequest) (*capability.CapabilitySummary, error) {
	if svc == nil {
		return nil, capability.ErrNotFound
	}
	capabilityID := strings.TrimSpace(req.CapabilityID)
	if capabilityID != "" {
		if item, err := svc.Get(ctx, capabilityID); err == nil && strings.TrimSpace(item.ID) != "" && installIntentMatchesEnterpriseCapability(req, *item) {
			return item, nil
		}
	}
	items, err := svc.List(ctx, strings.TrimSpace(req.CapabilityType))
	if err != nil {
		return nil, err
	}
	for i := range items {
		item := items[i]
		if !installIntentMatchesEnterpriseCapability(req, item) {
			continue
		}
		return &item, nil
	}
	return nil, capability.ErrNotFound
}

func installIntentMatchesEnterpriseCapability(req capabilityInstallIntentRequest, item capability.CapabilitySummary) bool {
	capabilityID := strings.TrimSpace(req.CapabilityID)
	if capabilityID == "" {
		return false
	}
	if strings.TrimSpace(item.Status) != "" && !strings.EqualFold(item.Status, "approved") {
		return false
	}
	if strings.TrimSpace(req.CapabilityType) != "" && !strings.EqualFold(strings.TrimSpace(item.CapabilityType), strings.TrimSpace(req.CapabilityType)) {
		return false
	}
	metadata := mapFromRawJSON(json.RawMessage(item.MetadataJSON))
	if !capabilityOriginMatchesInstallIntent(req, metadata) {
		return false
	}
	if strings.TrimSpace(item.CapabilityID) == capabilityID {
		return true
	}
	for _, key := range []string{"capability_id", "server_id", "skill_id", "hub_skill_id", "id"} {
		if stringFromAny(metadata[key]) == capabilityID {
			return true
		}
	}
	return false
}

func capabilityOriginMatchesInstallIntent(req capabilityInstallIntentRequest, metadata map[string]any) bool {
	source := strings.TrimSpace(strings.ToLower(req.Source))
	if source == "" || source == corelib.CapabilitySourceEnterpriseHub {
		return true
	}
	originSource := strings.TrimSpace(strings.ToLower(stringFromAny(metadata["origin_source"])))
	return originSource == source
}
func CapabilityManagedDeploymentsHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := svc.ListManagedDeployments(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MANAGED_DEPLOYMENTS_LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func CapabilityRecommendationsHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := svc.ListRecommendations(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "RECOMMENDATIONS_LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func findOpenAcquisitionRequest(ctx context.Context, svc *capability.Service, req capabilityInstallIntentRequest, kind string) *capability.AcquisitionRequest {
	if svc == nil || strings.TrimSpace(kind) != "purchase" {
		return nil
	}
	items, err := svc.ListAcquisitionRequests(ctx, "")
	if err != nil {
		return nil
	}
	capabilityType := strings.TrimSpace(req.CapabilityType)
	capabilityID := strings.TrimSpace(req.CapabilityID)
	source := strings.TrimSpace(req.Source)
	version := strings.TrimSpace(req.Version)
	for i := range items {
		item := items[i]
		status := strings.TrimSpace(item.Status)
		if status != "pending_review" && status != "approved" {
			continue
		}
		if strings.TrimSpace(item.RequestKind) != kind || strings.TrimSpace(item.Source) != source || strings.TrimSpace(item.SourceCapabilityKey) != capabilityID {
			continue
		}
		if capabilityType != "" && !strings.EqualFold(strings.TrimSpace(item.CapabilityType), capabilityType) {
			continue
		}
		if version != "" && strings.TrimSpace(item.SourceVersionKey) != version {
			continue
		}
		return &item
	}
	return nil
}
func AdminCapabilityAcquisitionRequestsHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := svc.ListAcquisitionRequests(r.Context(), r.URL.Query().Get("status"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ACQUISITION_REQUESTS_LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func AdminCapabilityAcquisitionRequestDetailHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := svc.GetAcquisitionRequest(r.Context(), r.PathValue("id"))
		if errors.Is(err, capability.ErrNotFound) {
			writeError(w, http.StatusNotFound, "ACQUISITION_REQUEST_NOT_FOUND", "acquisition request not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ACQUISITION_REQUEST_GET_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}

func AdminCapabilityApproveAcquisitionHandler(svc *capability.Service, deps ...any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Approval json.RawMessage `json:"approval,omitempty"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		requestID := r.PathValue("id")
		item, getErr := svc.GetAcquisitionRequest(r.Context(), requestID)
		if getErr != nil && !errors.Is(getErr, capability.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "ACQUISITION_GET_FAILED", getErr.Error())
			return
		}
		if err := svc.ApproveAcquisitionRequest(r.Context(), requestID, "admin", rawJSONOrDefault(req.Approval)); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, capability.ErrNotFound) {
				status = http.StatusNotFound
			}
			writeError(w, status, "ACQUISITION_APPROVE_FAILED", err.Error())
			return
		}
		settings, centerStatus := capabilityApprovalDeps(deps...)
		if item != nil && item.RequestKind == "purchase" && item.Source == corelib.CapabilitySourceHubCenter && strings.EqualFold(item.CapabilityType, corelib.CapabilityTypeMCP) && centerStatus != nil {
			created, purchaseJSON, err := purchaseAndImportHubCenterMCPMarketplaceEntry(r.Context(), svc, settings, centerStatus, *item)
			if err != nil {
				writeError(w, http.StatusBadGateway, "HUBCENTER_PURCHASE_FAILED", err.Error())
				return
			}
			if err := svc.CompleteAcquisitionRequest(r.Context(), requestID, created.ID, purchaseJSON); err != nil {
				writeError(w, http.StatusInternalServerError, "ACQUISITION_COMPLETE_FAILED", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "purchased": true, "capability": created})
			return
		}
		if item != nil && item.RequestKind == "purchase" && item.Source == corelib.CapabilitySourceHubCenter && strings.EqualFold(item.CapabilityType, corelib.CapabilityTypeSkill) && centerStatus != nil {
			created, purchaseJSON, err := purchaseAndImportHubCenterSkillMarketplaceEntry(r.Context(), svc, settings, centerStatus, *item)
			if err != nil {
				writeError(w, http.StatusBadGateway, "HUBCENTER_PURCHASE_FAILED", err.Error())
				return
			}
			if err := svc.CompleteAcquisitionRequest(r.Context(), requestID, created.ID, purchaseJSON); err != nil {
				writeError(w, http.StatusInternalServerError, "ACQUISITION_COMPLETE_FAILED", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "purchased": true, "capability": created})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func AdminCapabilityRejectAcquisitionHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Approval json.RawMessage `json:"approval,omitempty"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if err := svc.RejectAcquisitionRequest(r.Context(), r.PathValue("id"), "admin", rawJSONOrDefault(req.Approval)); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, capability.ErrNotFound) {
				status = http.StatusNotFound
			}
			writeError(w, status, "ACQUISITION_REJECT_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func AdminCapabilityCompleteAcquisitionHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ResultCapabilityID string          `json:"result_capability_id"`
			Purchase           json.RawMessage `json:"purchase,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		if err := svc.CompleteAcquisitionRequest(r.Context(), r.PathValue("id"), req.ResultCapabilityID, rawJSONOrDefault(req.Purchase)); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, capability.ErrNotFound) {
				status = http.StatusNotFound
			}
			writeError(w, status, "ACQUISITION_COMPLETE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func AdminCapabilityManagedDeploymentCreateHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			CapabilityRef        string          `json:"capability_ref"`
			CapabilityVersionKey string          `json:"capability_version_key,omitempty"`
			Scope                json.RawMessage `json:"scope,omitempty"`
			DeploymentPolicy     string          `json:"deployment_policy,omitempty"`
			ReinstallIfRemoved   *bool           `json:"reinstall_if_removed,omitempty"`
			RetryIntervalMinutes int             `json:"retry_interval_minutes,omitempty"`
			Enabled              *bool           `json:"enabled,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		if strings.TrimSpace(req.CapabilityRef) == "" {
			writeError(w, http.StatusBadRequest, "INVALID_DEPLOYMENT", "capability_ref is required")
			return
		}
		reinstall := true
		if req.ReinstallIfRemoved != nil {
			reinstall = *req.ReinstallIfRemoved
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		id, err := svc.CreateManagedDeployment(r.Context(), capability.ManagedDeploymentInput{CapabilityRef: req.CapabilityRef, CapabilityVersionKey: req.CapabilityVersionKey, ScopeJSON: rawJSONOrDefault(req.Scope), DeploymentPolicy: req.DeploymentPolicy, ReinstallIfRemoved: reinstall, RetryIntervalMinutes: req.RetryIntervalMinutes, CreatedBy: "admin", Enabled: enabled})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MANAGED_DEPLOYMENT_CREATE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": id})
	}
}

func AdminCapabilityRecommendationCreateHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			CapabilityRef        string          `json:"capability_ref"`
			CapabilityVersionKey string          `json:"capability_version_key,omitempty"`
			Scope                json.RawMessage `json:"scope,omitempty"`
			Reason               string          `json:"recommendation_reason,omitempty"`
			AllowUserDismiss     *bool           `json:"allow_user_dismiss,omitempty"`
			Enabled              *bool           `json:"enabled,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		if strings.TrimSpace(req.CapabilityRef) == "" {
			writeError(w, http.StatusBadRequest, "INVALID_RECOMMENDATION", "capability_ref is required")
			return
		}
		allowDismiss := true
		if req.AllowUserDismiss != nil {
			allowDismiss = *req.AllowUserDismiss
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		id, err := svc.CreateRecommendation(r.Context(), capability.RecommendationInput{CapabilityRef: req.CapabilityRef, CapabilityVersionKey: req.CapabilityVersionKey, ScopeJSON: rawJSONOrDefault(req.Scope), Reason: req.Reason, AllowUserDismiss: allowDismiss, CreatedBy: "admin", Enabled: enabled})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "RECOMMENDATION_CREATE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": id})
	}
}

func AdminBillingCustomerAccountHandler(deps ...any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		settings, centerStatus := capabilityApprovalDeps(deps...)
		adminEmail := hubMarketplaceAdminEmail(r.Context(), settings)
		var state *center.RegistrationState
		if centerStatus != nil {
			state, _ = centerStatus.Status(r.Context())
		}
		status := "not_configured"
		if strings.TrimSpace(adminEmail) != "" && state != nil && strings.TrimSpace(state.HubID) != "" {
			status = "configured"
		}
		account := map[string]any{
			"status":      status,
			"admin_email": adminEmail,
			"hub_id":      hubCenterStateHubID(state),
			"hubcenter":   hubCenterStateBaseURL(state),
		}
		if centerAccount, err := fetchHubCenterCustomerAccount(r.Context(), centerStatus, hubCenterStateHubID(state), adminEmail); err == nil {
			for key, value := range centerAccount {
				account[key] = value
			}
			if _, ok := account["hub_id"]; !ok {
				account["hub_id"] = hubCenterStateHubID(state)
			}
			if _, ok := account["admin_email"]; !ok {
				account["admin_email"] = adminEmail
			}
			account["hubcenter"] = hubCenterStateBaseURL(state)
		}
		writeJSON(w, http.StatusOK, account)
	}
}

func fetchHubCenterCustomerAccount(ctx context.Context, centerStatus capabilityMarketCenterStatusProvider, hubID, adminEmail string) (map[string]any, error) {
	baseURL, err := hubCenterMarketplaceBaseURL(ctx, centerStatus)
	if err != nil {
		return nil, err
	}
	if baseURL == "" {
		return nil, errors.New("hubcenter marketplace is not configured")
	}
	values := url.Values{}
	if strings.TrimSpace(hubID) != "" {
		values.Set("hub_id", strings.TrimSpace(hubID))
	}
	if strings.TrimSpace(adminEmail) != "" {
		values.Set("admin_email", strings.TrimSpace(adminEmail))
	}
	endpoint := baseURL + "/api/capability-market/customer-account"
	if encoded := values.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("hubcenter customer account returned status " + resp.Status)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}
func AdminBillingLicensesHandler(settings store.SystemSettingsRepository, centerStatus capabilityMarketCenterStatusProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		baseURL, err := hubCenterMarketplaceBaseURL(r.Context(), centerStatus)
		if err != nil {
			writeError(w, http.StatusBadGateway, "HUBCENTER_STATUS_FAILED", err.Error())
			return
		}
		if baseURL == "" {
			writeError(w, http.StatusBadGateway, "HUBCENTER_NOT_CONFIGURED", "HubCenter marketplace is not configured")
			return
		}
		state, _ := centerStatus.Status(r.Context())
		adminEmail := firstNonEmpty(r.URL.Query().Get("admin_email"), r.URL.Query().Get("email"), hubMarketplaceAdminEmail(r.Context(), settings))
		values := url.Values{}
		if state != nil && strings.TrimSpace(state.HubID) != "" {
			values.Set("hub_id", strings.TrimSpace(state.HubID))
		}
		if adminEmail != "" {
			values.Set("admin_email", adminEmail)
		}
		endpoint := baseURL + "/api/capability-market/billing/licenses"
		if encoded := values.Encode(); encoded != "" {
			endpoint += "?" + encoded
		}
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "BILLING_LICENSES_REQUEST_FAILED", err.Error())
			return
		}
		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			writeError(w, http.StatusBadGateway, "HUBCENTER_LICENSES_FAILED", err.Error())
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			writeError(w, http.StatusBadGateway, "HUBCENTER_LICENSES_FAILED", "HubCenter licenses returned status "+resp.Status)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadGateway, "HUBCENTER_LICENSES_DECODE_FAILED", err.Error())
			return
		}
		payload["hub_id"] = ""
		if state != nil {
			payload["hub_id"] = state.HubID
		}
		payload["admin_email"] = adminEmail
		writeJSON(w, http.StatusOK, payload)
	}
}

func hubCenterStateHubID(state *center.RegistrationState) string {
	if state == nil {
		return ""
	}
	return strings.TrimSpace(state.HubID)
}

func hubCenterStateBaseURL(state *center.RegistrationState) string {
	if state == nil {
		return ""
	}
	return firstNonEmpty(state.ActiveBaseURL, state.BaseURL)
}

type capabilityMarketCenterStatusProvider interface {
	Status(ctx context.Context) (*center.RegistrationState, error)
}

func AdminCapabilityExternalSearchHandler(centerStatus capabilityMarketCenterStatusProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		source := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("source")))
		allowedSources := corelib.AdminMarketplaceSearchSources(corelib.CapabilityMarketplaceHostHub)
		if source != "" && !corelib.AdminMarketplaceCanSearchSource(corelib.CapabilityMarketplaceHostHub, source) {
			writeError(w, http.StatusForbidden, "SOURCE_NOT_ALLOWED", "source is not allowed for Hub marketplace admin search")
			return
		}
		capabilityType := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("type")))
		if capabilityType == "" {
			capabilityType = corelib.CapabilityTypeSkill
		}
		if source == corelib.CapabilitySourceHubCenter {
			var items []any
			var err error
			switch capabilityType {
			case corelib.CapabilityTypeMCP:
				items, err = searchHubCenterMCPMarketplace(r.Context(), centerStatus, r.URL.Query().Get("q"))
			case corelib.CapabilityTypeSkill:
				items, err = searchHubCenterSkillMarketplace(r.Context(), centerStatus, r.URL.Query().Get("q"))
			}
			if err != nil {
				writeError(w, http.StatusBadGateway, "HUBCENTER_SEARCH_FAILED", err.Error())
				return
			}
			if items != nil {
				writeJSON(w, http.StatusOK, map[string]any{"allowed_sources": allowedSources, "items": items})
				return
			}
		}
		if (source == corelib.CapabilitySourceClawHub || source == corelib.CapabilitySourceGitHub || source == "") && capabilityType == corelib.CapabilityTypeSkill {
			items := searchExternalSkillMarketplace(r.Context(), source, r.URL.Query().Get("q"), allowedSources)
			writeJSON(w, http.StatusOK, map[string]any{"allowed_sources": allowedSources, "items": items})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"allowed_sources": allowedSources,
			"items":           []any{},
		})
	}
}

func searchExternalSkillMarketplace(ctx context.Context, source, query string, allowedSources []string) []any {
	sources := make([]string, 0, len(allowedSources))
	if source != "" {
		sources = append(sources, source)
	} else {
		for _, allowed := range allowedSources {
			if allowed == corelib.CapabilitySourceClawHub || allowed == corelib.CapabilitySourceGitHub {
				sources = append(sources, allowed)
			}
		}
	}
	results := coreskill.DefaultHubClient().SearchAllFiltered(ctx, "", query, sources)
	items := make([]any, 0, len(results))
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
	return items
}
func hubCenterMarketplaceBaseURL(ctx context.Context, centerStatus capabilityMarketCenterStatusProvider) (string, error) {
	if centerStatus == nil {
		return "", nil
	}
	status, err := centerStatus.Status(ctx)
	if err != nil {
		return "", err
	}
	baseURL := firstNonEmpty(status.ActiveBaseURL, status.BaseURL)
	if baseURL == "" && len(status.BaseURLs) > 0 {
		baseURL = status.BaseURLs[0]
	}
	return strings.TrimRight(strings.TrimSpace(baseURL), "/"), nil
}

func searchHubCenterSkillMarketplace(ctx context.Context, centerStatus capabilityMarketCenterStatusProvider, query string) ([]any, error) {
	baseURL, err := hubCenterMarketplaceBaseURL(ctx, centerStatus)
	if err != nil {
		return nil, err
	}
	if baseURL == "" {
		return []any{}, nil
	}
	endpoint := baseURL + "/api/capability-market/search"
	values := url.Values{}
	if strings.TrimSpace(query) != "" {
		values.Set("q", strings.TrimSpace(query))
	}
	values.Set("top_n", "20")
	if encoded := values.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("hubcenter skill marketplace returned status " + resp.Status)
	}
	var payload struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	items := make([]any, 0, len(payload.Results))
	for _, item := range payload.Results {
		item["source"] = corelib.CapabilitySourceHubCenter
		item["capability_type"] = corelib.CapabilityTypeSkill
		items = append(items, item)
	}
	return items, nil
}

func searchHubCenterMCPMarketplace(ctx context.Context, centerStatus capabilityMarketCenterStatusProvider, query string) ([]any, error) {
	baseURL, err := hubCenterMarketplaceBaseURL(ctx, centerStatus)
	if err != nil {
		return nil, err
	}
	if baseURL == "" {
		return []any{}, nil
	}
	endpoint := baseURL + "/api/capability-market/mcp"
	if strings.TrimSpace(query) != "" {
		endpoint += "?q=" + url.QueryEscape(strings.TrimSpace(query))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("hubcenter marketplace returned status " + resp.Status)
	}
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	items := make([]any, 0, len(payload.Items))
	for _, item := range payload.Items {
		item["source"] = corelib.CapabilitySourceHubCenter
		item["capability_type"] = corelib.CapabilityTypeMCP
		items = append(items, item)
	}
	return items, nil
}

type hubCenterSkillMarketplaceEntry struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Version     string         `json:"version"`
	Author      string         `json:"author"`
	TrustLevel  string         `json:"trust_level"`
	Price       int64          `json:"price,omitempty"`
	UploaderID  string         `json:"uploader_id,omitempty"`
	Status      string         `json:"status,omitempty"`
	RequiredEnv []string       `json:"required_env,omitempty"`
	Permissions []string       `json:"permissions,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Manifest    map[string]any `json:"manifest,omitempty"`
}

type hubCenterMCPMarketplaceEntry struct {
	ID                 string                          `json:"id"`
	CapabilityType     string                          `json:"capability_type"`
	Publisher          string                          `json:"publisher"`
	CapabilityID       string                          `json:"capability_id"`
	DisplayName        string                          `json:"display_name"`
	Description        string                          `json:"description,omitempty"`
	Version            string                          `json:"version,omitempty"`
	VersionKey         string                          `json:"version_key,omitempty"`
	Pricing            json.RawMessage                 `json:"pricing,omitempty"`
	License            json.RawMessage                 `json:"license,omitempty"`
	MCP                corelib.MCPServerEntry          `json:"mcp"`
	SecretRequirements []adminMCPSecretRequirementSpec `json:"secret_requirements,omitempty"`
}

func fetchHubCenterMCPMarketplaceEntry(ctx context.Context, centerStatus capabilityMarketCenterStatusProvider, capabilityID string) (*hubCenterMCPMarketplaceEntry, error) {
	baseURL, err := hubCenterMarketplaceBaseURL(ctx, centerStatus)
	if err != nil {
		return nil, err
	}
	if baseURL == "" {
		return nil, errors.New("hubcenter marketplace is not configured")
	}
	endpoint := baseURL + "/api/capability-market/mcp/" + url.PathEscape(strings.TrimSpace(capabilityID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("hubcenter marketplace returned status " + resp.Status)
	}
	var item hubCenterMCPMarketplaceEntry
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return nil, err
	}
	if strings.TrimSpace(item.CapabilityID) == "" {
		item.CapabilityID = firstNonEmpty(item.ID, item.MCP.ID, item.MCP.Name)
	}
	if strings.TrimSpace(item.DisplayName) == "" {
		item.DisplayName = firstNonEmpty(item.MCP.Name, item.CapabilityID)
	}
	return &item, nil
}

func capabilityApprovalDeps(deps ...any) (store.SystemSettingsRepository, capabilityMarketCenterStatusProvider) {
	var settings store.SystemSettingsRepository
	var centerStatus capabilityMarketCenterStatusProvider
	for _, dep := range deps {
		if dep == nil {
			continue
		}
		if v, ok := dep.(store.SystemSettingsRepository); ok {
			settings = v
		}
		if v, ok := dep.(capabilityMarketCenterStatusProvider); ok {
			centerStatus = v
		}
	}
	return settings, centerStatus
}

func fetchHubCenterSkillMarketplaceEntry(ctx context.Context, centerStatus capabilityMarketCenterStatusProvider, skillID string) (*hubCenterSkillMarketplaceEntry, error) {
	baseURL, err := hubCenterMarketplaceBaseURL(ctx, centerStatus)
	if err != nil {
		return nil, err
	}
	if baseURL == "" {
		return nil, errors.New("hubcenter marketplace is not configured")
	}
	endpoint := baseURL + "/api/v1/skills/" + url.PathEscape(strings.TrimSpace(skillID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("hubcenter skill marketplace returned status " + resp.Status)
	}
	var item hubCenterSkillMarketplaceEntry
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return nil, err
	}
	if strings.TrimSpace(item.ID) == "" {
		item.ID = strings.TrimSpace(skillID)
	}
	if strings.TrimSpace(item.Name) == "" {
		item.Name = item.ID
	}
	return &item, nil
}

func importHubCenterSkillMarketplaceEntry(ctx context.Context, svc *capability.Service, centerStatus capabilityMarketCenterStatusProvider, skillID string) (*capability.CapabilitySummary, error) {
	baseURL, err := hubCenterMarketplaceBaseURL(ctx, centerStatus)
	if err != nil {
		return nil, err
	}
	item, err := fetchHubCenterSkillMarketplaceEntry(ctx, centerStatus, skillID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(item.Version) == "" {
		item.Version = "1.0.0"
	}
	publisher := firstNonEmpty(item.Author, item.UploaderID, "hubcenter")
	metadata := map[string]any{
		"skill_id":      item.ID,
		"hub_skill_id":  item.ID,
		"hub_url":       baseURL,
		"skill_hub_url": baseURL,
		"origin_source": corelib.CapabilitySourceHubCenter,
		"trust_level":   item.TrustLevel,
	}
	if item.Price > 0 {
		metadata["pricing"] = corelib.CapabilityPricingPaid
	}
	manifest := map[string]any{
		"name":        item.Name,
		"description": item.Description,
		"type":        corelib.CapabilityTypeSkill,
		"tags":        item.Tags,
	}
	origin := map[string]any{
		"source":        corelib.CapabilitySourceHubCenter,
		"capability_id": item.ID,
		"version":       item.Version,
	}
	pricing := map[string]any{"mode": corelib.CapabilityPricingFree}
	if item.Price > 0 {
		pricing = map[string]any{"mode": corelib.CapabilityPricingPaid, "credits": item.Price}
	}
	return svc.UpsertCapability(ctx, capability.UpsertCapabilityInput{
		CapabilityType:    corelib.CapabilityTypeSkill,
		Publisher:         publisher,
		CapabilityID:      item.ID,
		DisplayName:       item.Name,
		Description:       item.Description,
		Source:            corelib.CapabilitySourceEnterpriseHub,
		ManagedBy:         "admin",
		Status:            "approved",
		RelationToOrigin:  "mirrored",
		OriginKey:         corelib.CapabilitySourceHubCenter + ":" + corelib.CapabilityTypeSkill + ":" + publisher + ":" + item.ID,
		OriginJSON:        jsonObjectString(origin),
		MetadataJSON:      jsonObjectString(metadata),
		Version:           item.Version,
		ManifestJSON:      jsonObjectString(manifest),
		TypeConfigJSON:    jsonObjectString(item),
		PricingJSON:       jsonObjectString(pricing),
		SetCurrentVersion: true,
	})
}

func purchaseAndImportHubCenterSkillMarketplaceEntry(ctx context.Context, svc *capability.Service, settings store.SystemSettingsRepository, centerStatus capabilityMarketCenterStatusProvider, req capability.AcquisitionRequest) (*capability.CapabilitySummary, string, error) {
	baseURL, err := hubCenterMarketplaceBaseURL(ctx, centerStatus)
	if err != nil {
		return nil, "", err
	}
	if baseURL == "" {
		return nil, "", errors.New("hubcenter marketplace is not configured")
	}
	adminEmail := hubMarketplaceAdminEmail(ctx, settings)
	if adminEmail == "" {
		return nil, "", errors.New("admin email is required for paid Skill purchase")
	}
	endpoint := baseURL + "/api/capability-market/capabilities/" + url.PathEscape(strings.TrimSpace(req.SourceCapabilityKey)) + "/download?email=" + url.QueryEscape(adminEmail)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", errors.New("hubcenter Skill purchase returned status " + resp.Status)
	}
	var purchase map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&purchase); err != nil {
		return nil, "", err
	}
	delete(purchase, "encrypted_data")
	purchase["admin_email"] = adminEmail
	purchase["source"] = corelib.CapabilitySourceHubCenter
	created, err := importHubCenterSkillMarketplaceEntry(ctx, svc, centerStatus, req.SourceCapabilityKey)
	if err != nil {
		return nil, "", err
	}
	return created, jsonObjectString(purchase), nil
}

func purchaseAndImportHubCenterMCPMarketplaceEntry(ctx context.Context, svc *capability.Service, settings store.SystemSettingsRepository, centerStatus capabilityMarketCenterStatusProvider, req capability.AcquisitionRequest) (*capability.CapabilitySummary, string, error) {
	baseURL, err := hubCenterMarketplaceBaseURL(ctx, centerStatus)
	if err != nil {
		return nil, "", err
	}
	if baseURL == "" {
		return nil, "", errors.New("hubcenter marketplace is not configured")
	}
	status, _ := centerStatus.Status(ctx)
	adminEmail := hubMarketplaceAdminEmail(ctx, settings)
	payload := map[string]any{
		"hub_id":      "",
		"admin_email": adminEmail,
		"request_id":  req.ID,
	}
	if status != nil {
		payload["hub_id"] = status.HubID
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	endpoint := baseURL + "/api/capability-market/mcp/" + url.PathEscape(strings.TrimSpace(req.SourceCapabilityKey)) + "/purchase"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", errors.New("hubcenter MCP purchase returned status " + resp.Status)
	}
	var purchase map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&purchase); err != nil {
		return nil, "", err
	}
	created, err := importHubCenterMCPMarketplaceEntry(ctx, svc, centerStatus, req.SourceCapabilityKey)
	if err != nil {
		return nil, "", err
	}
	return created, jsonObjectString(purchase), nil
}

func hubMarketplaceAdminEmail(ctx context.Context, settings store.SystemSettingsRepository) string {
	if settings == nil {
		return ""
	}
	raw, err := settings.Get(ctx, "admin_email")
	if err != nil {
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

func importHubCenterMCPMarketplaceEntry(ctx context.Context, svc *capability.Service, centerStatus capabilityMarketCenterStatusProvider, capabilityID string) (*capability.CapabilitySummary, error) {
	item, err := fetchHubCenterMCPMarketplaceEntry(ctx, centerStatus, capabilityID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(item.CapabilityID) == "" || strings.TrimSpace(item.DisplayName) == "" {
		return nil, errors.New("hubcenter MCP capability is missing capability_id/display_name")
	}
	if strings.TrimSpace(item.Version) == "" {
		item.Version = "1.0.0"
	}
	if strings.TrimSpace(item.Publisher) == "" {
		item.Publisher = "hubcenter"
	}
	metadata := map[string]any{
		"server_id":     firstNonEmpty(item.MCP.ID, item.CapabilityID),
		"name":          firstNonEmpty(item.MCP.Name, item.DisplayName),
		"endpoint_url":  strings.TrimSpace(item.MCP.EndpointURL),
		"auth_type":     firstNonEmpty(item.MCP.AuthType, "none"),
		"origin_source": corelib.CapabilitySourceHubCenter,
		"pricing":       corelib.CapabilityPricingFree,
	}
	if len(item.Pricing) > 0 {
		metadata["pricing"] = item.Pricing
	}
	if len(item.MCP.Headers) > 0 {
		metadata["headers"] = item.MCP.Headers
	}
	manifest := map[string]any{
		"name":        item.DisplayName,
		"description": item.Description,
		"type":        corelib.CapabilityTypeMCP,
	}
	origin := map[string]any{
		"source":        corelib.CapabilitySourceHubCenter,
		"capability_id": item.CapabilityID,
		"version_key":   item.VersionKey,
	}
	created, err := svc.UpsertCapability(ctx, capability.UpsertCapabilityInput{
		CapabilityType:    corelib.CapabilityTypeMCP,
		Publisher:         item.Publisher,
		CapabilityID:      item.CapabilityID,
		DisplayName:       item.DisplayName,
		Description:       item.Description,
		Source:            corelib.CapabilitySourceEnterpriseHub,
		ManagedBy:         "admin",
		Status:            "approved",
		RelationToOrigin:  "mirrored",
		OriginKey:         firstNonEmpty(item.VersionKey, corelib.CapabilitySourceHubCenter+":"+corelib.CapabilityTypeMCP+":"+item.Publisher+":"+item.CapabilityID),
		OriginJSON:        jsonObjectString(origin),
		MetadataJSON:      jsonObjectString(metadata),
		Version:           item.Version,
		ManifestJSON:      jsonObjectString(manifest),
		TypeConfigJSON:    jsonObjectString(item.MCP),
		PricingJSON:       rawJSONOrDefault(item.Pricing),
		LicenseJSON:       rawJSONOrDefault(item.License),
		SetCurrentVersion: true,
	})
	if err != nil {
		return nil, err
	}
	for _, spec := range item.SecretRequirements {
		if strings.TrimSpace(spec.Name) == "" {
			continue
		}
		required := true
		if spec.Required != nil {
			required = *spec.Required
		}
		if _, err := svc.UpsertMCPSecretRequirement(ctx, capability.MCPSecretRequirementInput{CapabilityRef: created.ID, VersionKey: created.CurrentVersionKey, Name: spec.Name, Label: spec.Label, Scope: spec.Scope, StoragePolicy: spec.StoragePolicy, Required: required, HelpURL: spec.HelpURL, MetadataJSON: rawJSONOrDefault(spec.Metadata)}); err != nil {
			return nil, err
		}
	}
	return created, nil
}

func importFreeExternalSkillCapability(ctx context.Context, svc *capability.Service, req capabilityInstallIntentRequest) (*capability.CapabilitySummary, error) {
	metadata := mapFromRawJSON(req.Metadata)
	metadata["origin_source"] = req.Source
	metadata["pricing"] = firstNonEmpty(req.Pricing, corelib.CapabilityPricingFree)
	metadata["skill_id"] = firstNonEmpty(stringFromAny(metadata["skill_id"]), stringFromAny(metadata["id"]), stringFromAny(metadata["capability_id"]), req.CapabilityID)
	if req.Source == corelib.CapabilitySourceGitHub {
		metadata["install_ref"] = firstNonEmpty(stringFromAny(metadata["install_ref"]), stringFromAny(metadata["InstallRef"]))
		metadata["repo_url"] = firstNonEmpty(stringFromAny(metadata["repo_url"]), stringFromAny(metadata["RepoURL"]))
		metadata["file_path"] = firstNonEmpty(stringFromAny(metadata["file_path"]), stringFromAny(metadata["FilePath"]))
	}
	capabilityID := firstNonEmpty(req.CapabilityID, stringFromAny(metadata["skill_id"]))
	if capabilityID == "" {
		return nil, errors.New("external skill capability_id is required")
	}
	displayName := firstNonEmpty(req.DisplayName, stringFromAny(metadata["display_name"]), stringFromAny(metadata["name"]), capabilityID)
	description := firstNonEmpty(req.Description, stringFromAny(metadata["description"]), stringFromAny(metadata["summary"]))
	version := firstNonEmpty(req.Version, stringFromAny(metadata["version"]), "1.0.0")
	versionKey := req.Source + ":" + corelib.CapabilityTypeSkill + ":" + capabilityID + "@" + version
	origin := map[string]any{"source": req.Source, "capability_id": capabilityID, "version_key": versionKey}
	manifest := map[string]any{"name": displayName, "description": description, "type": corelib.CapabilityTypeSkill}
	return svc.UpsertCapability(ctx, capability.UpsertCapabilityInput{
		CapabilityType:    corelib.CapabilityTypeSkill,
		Publisher:         firstNonEmpty(stringFromAny(metadata["author"]), req.Source),
		CapabilityID:      capabilityID,
		DisplayName:       displayName,
		Description:       description,
		Source:            corelib.CapabilitySourceEnterpriseHub,
		ManagedBy:         "admin",
		Status:            "approved",
		RelationToOrigin:  "mirrored",
		OriginKey:         req.Source + ":" + corelib.CapabilityTypeSkill + ":" + capabilityID,
		OriginJSON:        jsonObjectString(origin),
		MetadataJSON:      jsonObjectString(metadata),
		Version:           version,
		VersionKey:        versionKey,
		ManifestJSON:      jsonObjectString(manifest),
		PricingJSON:       rawJSONOrDefault(req.Price),
		LicenseJSON:       rawJSONOrDefault(req.License),
		SetCurrentVersion: true,
	})
}

func importFreeHubCenterCapabilityForIntent(ctx context.Context, svc *capability.Service, centerStatus capabilityMarketCenterStatusProvider, req capabilityInstallIntentRequest) (*capability.CapabilitySummary, error) {
	if !strings.EqualFold(req.Pricing, corelib.CapabilityPricingFree) && strings.TrimSpace(req.Pricing) != "" {
		return nil, nil
	}
	if strings.EqualFold(req.CapabilityType, corelib.CapabilityTypeMCP) {
		return importHubCenterMCPMarketplaceEntry(ctx, svc, centerStatus, req.CapabilityID)
	}
	if strings.EqualFold(req.CapabilityType, corelib.CapabilityTypeSkill) {
		return importHubCenterSkillMarketplaceEntry(ctx, svc, centerStatus, req.CapabilityID)
	}
	return nil, nil
}
func AdminCapabilityImportIntentHandler(svc *capability.Service, settings store.SystemSettingsRepository, centerStatuses ...capabilityMarketCenterStatusProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req capabilityInstallIntentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		if !corelib.AdminMarketplaceCanSearchSource(corelib.CapabilityMarketplaceHostHub, req.Source) {
			writeError(w, http.StatusForbidden, "SOURCE_NOT_ALLOWED", "source is not allowed for Hub marketplace admin import")
			return
		}
		policy := corelib.DefaultCapabilityMarketPolicy()
		if settings != nil {
			if loaded, err := loadCapabilityMarketPolicy(r, settings); err == nil {
				policy = loaded
			}
		}
		adminSearchOnly := false
		policy.EnterpriseOnlySearch = &adminSearchOnly
		var enterpriseItem *capability.CapabilitySummary
		if item, err := findEnterpriseCapabilityForInstallIntent(r.Context(), svc, req); err == nil {
			enterpriseItem = item
		}
		decision := corelib.DecideCapabilityInstall(corelib.CapabilityInstallDecisionInput{
			Policy:             policy,
			Source:             req.Source,
			Pricing:            req.Pricing,
			ExistsInEnterprise: enterpriseItem != nil && strings.TrimSpace(enterpriseItem.ID) != "",
			ExternalInstallOK:  true,
			ExternalPurchaseOK: true,
		})
		if decision.Action == corelib.CapabilityInstallFromEnterpriseHub {
			writeJSON(w, http.StatusOK, map[string]any{"action": decision.Action, "reason": decision.Reason, "capability": enterpriseItem})
			return
		}
		if decision.Action == corelib.CapabilityInstallBlocked {
			writeJSON(w, http.StatusForbidden, map[string]any{"action": decision.Action, "reason": decision.Reason})
			return
		}
		kind := "import"
		if decision.Action == corelib.CapabilityInstallCreatePurchaseRequest {
			kind = "purchase"
		}
		if existing := findOpenAcquisitionRequest(r.Context(), svc, req, kind); existing != nil {
			writeJSON(w, http.StatusAccepted, map[string]any{"action": decision.Action, "reason": decision.Reason, "request_id": existing.ID})
			return
		}
		requestID, err := svc.CreateAcquisitionRequest(r.Context(), capability.AcquisitionRequestInput{
			RequesterUserID:     "admin",
			CapabilityType:      req.CapabilityType,
			Source:              req.Source,
			SourceCapabilityKey: req.CapabilityID,
			SourceVersionKey:    req.Version,
			RequestKind:         kind,
			Reason:              req.UserReason,
			PriceJSON:           rawJSONOrDefault(req.Price),
			LicenseJSON:         rawJSONOrDefault(req.License),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ACQUISITION_REQUEST_FAILED", err.Error())
			return
		}
		if (decision.Action == corelib.CapabilityInstallCreateImportRequest || decision.Action == corelib.CapabilityInstallExternalDirect) && req.Source == corelib.CapabilitySourceHubCenter && strings.EqualFold(req.CapabilityType, corelib.CapabilityTypeMCP) {
			var centerStatus capabilityMarketCenterStatusProvider
			if len(centerStatuses) > 0 {
				centerStatus = centerStatuses[0]
			}
			created, err := importHubCenterMCPMarketplaceEntry(r.Context(), svc, centerStatus, req.CapabilityID)
			if err != nil {
				writeError(w, http.StatusBadGateway, "HUBCENTER_IMPORT_FAILED", err.Error())
				return
			}
			_ = svc.CompleteAcquisitionRequest(r.Context(), requestID, created.ID, jsonObjectString(map[string]any{"mode": "free_import", "source": corelib.CapabilitySourceHubCenter}))
			writeJSON(w, http.StatusOK, map[string]any{"action": decision.Action, "reason": decision.Reason, "request_id": requestID, "capability": created})
			return
		}
		if (decision.Action == corelib.CapabilityInstallCreateImportRequest || decision.Action == corelib.CapabilityInstallExternalDirect) && req.Source == corelib.CapabilitySourceHubCenter && strings.EqualFold(req.CapabilityType, corelib.CapabilityTypeSkill) {
			var centerStatus capabilityMarketCenterStatusProvider
			if len(centerStatuses) > 0 {
				centerStatus = centerStatuses[0]
			}
			created, err := importHubCenterSkillMarketplaceEntry(r.Context(), svc, centerStatus, req.CapabilityID)
			if err != nil {
				writeError(w, http.StatusBadGateway, "HUBCENTER_IMPORT_FAILED", err.Error())
				return
			}
			_ = svc.CompleteAcquisitionRequest(r.Context(), requestID, created.ID, jsonObjectString(map[string]any{"mode": "free_import", "source": corelib.CapabilitySourceHubCenter}))
			writeJSON(w, http.StatusOK, map[string]any{"action": decision.Action, "reason": decision.Reason, "request_id": requestID, "capability": created})
			return
		}
		if (decision.Action == corelib.CapabilityInstallCreateImportRequest || decision.Action == corelib.CapabilityInstallExternalDirect) && strings.EqualFold(req.CapabilityType, corelib.CapabilityTypeSkill) && (req.Source == corelib.CapabilitySourceClawHub || req.Source == corelib.CapabilitySourceGitHub) {
			created, err := importFreeExternalSkillCapability(r.Context(), svc, req)
			if err != nil {
				writeError(w, http.StatusBadGateway, "EXTERNAL_SKILL_IMPORT_FAILED", err.Error())
				return
			}
			_ = svc.CompleteAcquisitionRequest(r.Context(), requestID, created.ID, jsonObjectString(map[string]any{"mode": "free_import", "source": req.Source}))
			writeJSON(w, http.StatusOK, map[string]any{"action": decision.Action, "reason": decision.Reason, "request_id": requestID, "capability": created})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"action": decision.Action, "reason": decision.Reason, "request_id": requestID})
	}
}

func AdminCapabilityMarketPolicyGetHandler(settings store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		policy, err := loadCapabilityMarketPolicy(r, settings)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_MARKET_POLICY_GET_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"policy": policy})
	}
}

func AdminCapabilityMarketPolicyUpdateHandler(settings store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if settings == nil {
			writeError(w, http.StatusInternalServerError, "SETTINGS_UNAVAILABLE", "system settings repository is unavailable")
			return
		}
		var req struct {
			Policy *corelib.CapabilityMarketPolicy `json:"policy"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		if req.Policy == nil {
			writeError(w, http.StatusBadRequest, "INVALID_POLICY", "policy is required")
			return
		}
		policy := req.Policy.WithDefaults()
		data, err := json.Marshal(policy)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_MARKET_POLICY_ENCODE_FAILED", err.Error())
			return
		}
		if err := settings.Set(r.Context(), capabilityMarketPolicySettingKey, string(data)); err != nil {
			writeError(w, http.StatusInternalServerError, "CAPABILITY_MARKET_POLICY_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"policy": policy})
	}
}

func MCPSecretRequirementsHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := svc.ListMCPSecretRequirements(r.Context(), r.PathValue("id"), r.URL.Query().Get("version_key"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MCP_SECRET_REQUIREMENTS_LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func MCPHubSecretsHandler(identity viewerAuthenticator, svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateMarketplaceViewer(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		items, err := svc.ListMCPHubSecrets(r.Context(), principal.UserID, r.URL.Query().Get("mcp_server_id"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MCP_HUB_SECRETS_LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func MCPHubSecretUpsertHandler(identity viewerAuthenticator, svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateMarketplaceViewer(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		var req struct {
			MCPServerID     string          `json:"mcp_server_id"`
			RequirementName string          `json:"requirement_name"`
			SecretValue     string          `json:"secret_value"`
			Metadata        json.RawMessage `json:"metadata,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		item, err := svc.UpsertMCPHubSecret(r.Context(), capability.MCPHubSecretInput{UserID: principal.UserID, MCPServerID: req.MCPServerID, RequirementName: req.RequirementName, SecretValue: req.SecretValue, MetadataJSON: rawJSONOrDefault(req.Metadata)})
		if err != nil {
			writeError(w, http.StatusBadRequest, "MCP_HUB_SECRET_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}

func MCPSecretBindingsHandler(identity viewerAuthenticator, svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateMarketplaceViewer(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		items, err := svc.ListMCPSecretBindings(r.Context(), principal.UserID, r.URL.Query().Get("mcp_server_id"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MCP_SECRET_BINDINGS_LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func MCPSecretBindingUpsertHandler(identity viewerAuthenticator, svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateMarketplaceViewer(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		var req struct {
			MCPServerID     string `json:"mcp_server_id"`
			RequirementName string `json:"requirement_name"`
			Storage         string `json:"storage"`
			HubSecretRef    string `json:"hub_secret_ref,omitempty"`
			LocalSecretRef  string `json:"local_secret_ref,omitempty"`
			Status          string `json:"status,omitempty"`
			LastVerifiedAt  string `json:"last_verified_at,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		item, err := svc.UpsertMCPSecretBinding(r.Context(), capability.MCPSecretBindingInput{UserID: principal.UserID, MCPServerID: req.MCPServerID, RequirementName: req.RequirementName, Storage: req.Storage, HubSecretRef: req.HubSecretRef, LocalSecretRef: req.LocalSecretRef, Status: req.Status, LastVerifiedAt: req.LastVerifiedAt})
		if err != nil {
			writeError(w, http.StatusBadRequest, "MCP_SECRET_BINDING_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}

func AdminMCPSecretRequirementUpsertHandler(svc *capability.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			CapabilityRef string          `json:"capability_ref"`
			VersionKey    string          `json:"version_key"`
			Name          string          `json:"name"`
			Label         string          `json:"label,omitempty"`
			Scope         string          `json:"scope,omitempty"`
			StoragePolicy string          `json:"storage_policy,omitempty"`
			Required      *bool           `json:"required,omitempty"`
			HelpURL       string          `json:"help_url,omitempty"`
			Metadata      json.RawMessage `json:"metadata,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		if strings.TrimSpace(req.CapabilityRef) == "" || strings.TrimSpace(req.Name) == "" {
			writeError(w, http.StatusBadRequest, "INVALID_SECRET_REQUIREMENT", "capability_ref and name are required")
			return
		}
		required := true
		if req.Required != nil {
			required = *req.Required
		}
		id, err := svc.UpsertMCPSecretRequirement(r.Context(), capability.MCPSecretRequirementInput{CapabilityRef: req.CapabilityRef, VersionKey: req.VersionKey, Name: req.Name, Label: req.Label, Scope: req.Scope, StoragePolicy: req.StoragePolicy, Required: required, HelpURL: req.HelpURL, MetadataJSON: rawJSONOrDefault(req.Metadata)})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MCP_SECRET_REQUIREMENT_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": id})
	}
}

type marketplaceViewerPrincipal struct {
	UserID string
	Email  string
}

type viewerAuthenticator interface {
	AuthenticateViewer(ctx context.Context, rawToken string) (*auth.ViewerPrincipal, error)
}

func authenticateMarketplaceViewer(r *http.Request, identity viewerAuthenticator) (*marketplaceViewerPrincipal, error) {
	if identity == nil {
		return nil, auth.ErrInvalidUserCredentials
	}
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return nil, auth.ErrInvalidUserCredentials
	}
	principal, err := identity.AuthenticateViewer(r.Context(), strings.TrimSpace(authz[7:]))
	if err != nil {
		return nil, err
	}
	return &marketplaceViewerPrincipal{UserID: principal.UserID, Email: principal.Email}, nil
}

func loadCapabilityMarketPolicy(r *http.Request, settings store.SystemSettingsRepository) (corelib.CapabilityMarketPolicy, error) {
	policy := corelib.DefaultCapabilityMarketPolicy()
	if settings == nil {
		return policy, nil
	}
	raw, err := settings.Get(r.Context(), capabilityMarketPolicySettingKey)
	if err != nil {
		return policy, err
	}
	if strings.TrimSpace(raw) == "" {
		return policy, nil
	}
	if err := json.Unmarshal([]byte(raw), &policy); err != nil {
		return policy, err
	}
	return policy.WithDefaults(), nil
}

func mapFromRawJSON(raw json.RawMessage) map[string]any {
	out := map[string]any{}
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || string(raw) == "null" {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = map[string]any{}
	}
	return out
}

func stringFromAny(value any) string {
	s, _ := value.(string)
	return strings.TrimSpace(s)
}

func rawJSONOrDefault(raw json.RawMessage) string {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || string(raw) == "null" {
		return "{}"
	}
	return string(raw)
}

func jsonObjectString(v any) string {
	data, err := json.Marshal(v)
	if err != nil || len(data) == 0 || string(data) == "null" {
		return "{}"
	}
	return string(data)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
