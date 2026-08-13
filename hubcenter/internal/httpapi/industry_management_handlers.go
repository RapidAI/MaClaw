package httpapi

// Industry management is the HubCenter control plane for default experts.
// It owns immutable market-asset snapshots and Hub+tenant assignments. It
// deliberately never creates market purchases: paid assets are merely shown
// to the tenant; each GUI user's normal market account establishes purchase.

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
)

type industryManagementSchemaState struct {
	once sync.Once
	err  error
}

var industryManagementSchemaByDB sync.Map // map[*sql.DB]*industryManagementSchemaState

const (
	// generalIndustryID is a system industry rather than a tenant assignment.
	// It is selected implicitly only when a tenant has no explicit industries.
	generalIndustryID   = "industry_general"
	generalIndustryCode = "general"
)

type IndustryManagementHandlers struct {
	db     *sql.DB
	hubs   *hubs.Service
	market *SkillMarketHandlers
	// Catalogue recomputation spans the assignment and catalogue tables. Keep
	// it serial inside this process so two administrator changes cannot lose a
	// revision increment. The catalogue content itself remains authoritative in
	// SQLite and is safe across restarts.
	catalogueMu sync.Mutex
}

func NewIndustryManagementHandlers(market *SkillMarketHandlers, hubService *hubs.Service) *IndustryManagementHandlers {
	var db *sql.DB
	if market != nil && market.store != nil {
		db = market.store.DB()
	}
	return &IndustryManagementHandlers{db: db, hubs: hubService, market: market}
}

func (h *IndustryManagementHandlers) validateHubTenant(ctx context.Context, hubID, tenantID string) error {
	hubID = strings.TrimSpace(hubID)
	tenantID = strings.TrimSpace(tenantID)
	if hubID == "" || tenantID == "" {
		return errors.New("hub and tenant are required")
	}
	// Unit-level handler construction does not always include the Hub service;
	// production router wiring does. Preserve the isolated handler contract
	// while enforcing the check whenever the authoritative service is present.
	if h == nil || h.hubs == nil {
		return nil
	}
	return h.hubs.ValidateHubID(ctx, hubID)
}

func (h *IndustryManagementHandlers) ensureSchema() error {
	if h == nil || h.db == nil {
		return errors.New("industry management unavailable")
	}
	stateValue, _ := industryManagementSchemaByDB.LoadOrStore(h.db, &industryManagementSchemaState{})
	state := stateValue.(*industryManagementSchemaState)
	state.once.Do(func() {
		for _, stmt := range []string{
			`CREATE TABLE IF NOT EXISTS industry_catalog_industries (id TEXT PRIMARY KEY, code TEXT NOT NULL UNIQUE, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', icon TEXT NOT NULL DEFAULT '', sort_order INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'active', created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS industry_catalog_assets (id TEXT PRIMARY KEY, listing_id TEXT NOT NULL UNIQUE, source_expert_id TEXT NOT NULL, version TEXT NOT NULL, price INTEGER NOT NULL, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', icon TEXT NOT NULL DEFAULT '', definition_json TEXT NOT NULL, package_hash TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'ready', created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS industry_catalog_bindings (industry_id TEXT NOT NULL, asset_id TEXT NOT NULL, display_order INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'active', note TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, PRIMARY KEY(industry_id,asset_id))`,
			`CREATE TABLE IF NOT EXISTS hub_tenant_industry_assignments (hub_id TEXT NOT NULL, tenant_id TEXT NOT NULL, industry_id TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, PRIMARY KEY(hub_id,tenant_id,industry_id))`,
			`CREATE TABLE IF NOT EXISTS hub_tenant_industry_catalogs (hub_id TEXT NOT NULL, tenant_id TEXT NOT NULL, revision INTEGER NOT NULL DEFAULT 0, content_hash TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL, PRIMARY KEY(hub_id,tenant_id))`,
			`CREATE TABLE IF NOT EXISTS industry_catalog_audit_events (id TEXT PRIMARY KEY, actor_id TEXT NOT NULL, action TEXT NOT NULL, target_type TEXT NOT NULL, target_id TEXT NOT NULL, reason TEXT NOT NULL DEFAULT '', before_json TEXT NOT NULL DEFAULT '', after_json TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS idx_industry_assignment_hub_tenant ON hub_tenant_industry_assignments(hub_id,tenant_id)`,
			`CREATE INDEX IF NOT EXISTS idx_industry_audit_target ON industry_catalog_audit_events(target_type,target_id,created_at)`,
		} {
			if _, err := h.db.Exec(stmt); err != nil {
				state.err = err
				return
			}
		}
		now := industryNow()
		// A tenant with no explicit industry selection always falls back to this
		// active system industry. Do not materialize an assignment per tenant:
		// that would make "clear settings" ambiguous and create needless rows.
		// Reassert the system-owned identity on every schema initialization. A
		// historical/manual disable must not silently leave every unconfigured
		// tenant without its required default catalogue.
		if _, err := h.db.Exec(`INSERT INTO industry_catalog_industries(id,code,name,description,icon,sort_order,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET code=excluded.code,name=excluded.name,description=excluded.description,icon=excluded.icon,sort_order=excluded.sort_order,status='active',updated_at=excluded.updated_at`, generalIndustryID, generalIndustryCode, "通用行业", "租户未配置行业时使用的系统默认行业", "🌐", -1000, "active", now, now); err != nil {
			state.err = err
		}
	})
	return state.err
}

type industryRecord struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	SortOrder   int    `json:"sort_order"`
	Status      string `json:"status"`
}
type industryAsset struct {
	ID             string          `json:"id"`
	ListingID      string          `json:"listing_id"`
	SourceExpertID string          `json:"source_expert_id"`
	Version        string          `json:"version"`
	Price          int64           `json:"price"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Icon           string          `json:"icon"`
	Definition     json.RawMessage `json:"definition,omitempty"`
	PackageHash    string          `json:"package_hash"`
	Status         string          `json:"status"`
}

func industryNow() string             { return time.Now().UTC().Format(time.RFC3339Nano) }
func industryID(prefix string) string { return prefix + "_" + uniqueID(prefix) }
func industryAuditActor(r *http.Request) string {
	if r == nil {
		return "admin"
	}
	if actor := strings.TrimSpace(r.Header.Get("X-Admin-User")); actor != "" {
		return actor
	}
	return "admin"
}

func (h *IndustryManagementHandlers) appendAudit(ctx context.Context, r *http.Request, action, targetType, targetID, reason string, before, after any) {
	if h == nil || h.db == nil {
		return
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	_, _ = h.db.ExecContext(ctx, `INSERT INTO industry_catalog_audit_events(id,actor_id,action,target_type,target_id,reason,before_json,after_json,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, industryID("industry_audit"), industryAuditActor(r), action, targetType, targetID, strings.TrimSpace(reason), string(beforeJSON), string(afterJSON), industryNow())
}

func (h *IndustryManagementHandlers) listIndustries(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "INDUSTRY_LIST_FAILED", "industry management unavailable")
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT id,code,name,description,icon,sort_order,status FROM industry_catalog_industries ORDER BY sort_order,name,id`)
	if err != nil {
		writeError(w, 500, "INDUSTRY_LIST_FAILED", "internal error")
		return
	}
	defer rows.Close()
	out := []industryRecord{}
	for rows.Next() {
		var x industryRecord
		if err := rows.Scan(&x.ID, &x.Code, &x.Name, &x.Description, &x.Icon, &x.SortOrder, &x.Status); err != nil {
			writeError(w, 500, "INDUSTRY_LIST_FAILED", "internal error")
			return
		}
		out = append(out, x)
	}
	writeJSON(w, 200, map[string]any{"industries": out})
}

func (h *IndustryManagementHandlers) listAuditEvents(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "INDUSTRY_AUDIT_LIST_FAILED", "industry management unavailable")
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		var parsed int
		if _, err := fmt.Sscanf(raw, "%d", &parsed); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT id,actor_id,action,target_type,target_id,reason,created_at FROM industry_catalog_audit_events ORDER BY created_at DESC,id DESC LIMIT ?`, limit)
	if err != nil {
		writeError(w, 500, "INDUSTRY_AUDIT_LIST_FAILED", "internal error")
		return
	}
	defer rows.Close()
	events := make([]map[string]string, 0)
	for rows.Next() {
		var id, actor, action, targetType, targetID, reason, createdAt string
		if err := rows.Scan(&id, &actor, &action, &targetType, &targetID, &reason, &createdAt); err != nil {
			writeError(w, 500, "INDUSTRY_AUDIT_LIST_FAILED", "internal error")
			return
		}
		events = append(events, map[string]string{"id": id, "actor_id": actor, "action": action, "target_type": targetType, "target_id": targetID, "reason": reason, "created_at": createdAt})
	}
	writeJSON(w, 200, map[string]any{"events": events})
}

func (h *IndustryManagementHandlers) createIndustry(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "INDUSTRY_CREATE_FAILED", "industry management unavailable")
		return
	}
	var in struct {
		Code        string `json:"code"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Icon        string `json:"icon"`
		SortOrder   int    `json:"sort_order"`
		Reason      string `json:"reason"`
	}
	if err := decodeLimitedJSON(w, r, &in, defaultJSONBodyLimit); err != nil {
		writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
		return
	}
	in.Code = strings.ToLower(strings.TrimSpace(in.Code))
	in.Name = strings.TrimSpace(in.Name)
	if in.Code == "" || in.Name == "" || !expertMarketIDPattern.MatchString(in.Code) {
		writeError(w, 400, "INVALID_INDUSTRY", "industry code and name are required")
		return
	}
	now := industryNow()
	out := industryRecord{ID: industryID("industry"), Code: in.Code, Name: in.Name, Description: strings.TrimSpace(in.Description), Icon: strings.TrimSpace(in.Icon), SortOrder: in.SortOrder, Status: "active"}
	if _, err := h.db.ExecContext(r.Context(), `INSERT INTO industry_catalog_industries(id,code,name,description,icon,sort_order,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, out.ID, out.Code, out.Name, out.Description, out.Icon, out.SortOrder, out.Status, now, now); err != nil {
		writeError(w, 409, "INDUSTRY_CREATE_FAILED", "industry code already exists")
		return
	}
	h.appendAudit(r.Context(), r, "industry.created", "industry", out.ID, in.Reason, nil, out)
	writeJSON(w, 201, out)
}

func (h *IndustryManagementHandlers) patchIndustry(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "INDUSTRY_UPDATE_FAILED", "industry management unavailable")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	var in struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Icon        *string `json:"icon"`
		SortOrder   *int    `json:"sort_order"`
		Status      *string `json:"status"`
		Reason      string  `json:"reason"`
	}
	if err := decodeLimitedJSON(w, r, &in, defaultJSONBodyLimit); err != nil {
		writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
		return
	}
	// Read the before image under the same serialization boundary as the update
	// and revision refresh. Otherwise two concurrent PATCH requests can both
	// record an obsolete audit before image even though one overwrote the other.
	h.catalogueMu.Lock()
	defer h.catalogueMu.Unlock()
	var old industryRecord
	err := h.db.QueryRowContext(r.Context(), `SELECT id,code,name,description,icon,sort_order,status FROM industry_catalog_industries WHERE id=?`, id).Scan(&old.ID, &old.Code, &old.Name, &old.Description, &old.Icon, &old.SortOrder, &old.Status)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "INDUSTRY_NOT_FOUND", "industry not found")
		return
	}
	if err != nil {
		writeError(w, 500, "INDUSTRY_UPDATE_FAILED", "internal error")
		return
	}
	if id == generalIndustryID && (in.Name != nil || in.Description != nil || in.Icon != nil || in.SortOrder != nil) {
		writeError(w, 400, "GENERAL_INDUSTRY_MANAGED", "general industry metadata is system managed")
		return
	}
	// Keep the persisted state before applying request fields. Besides making
	// audit records meaningful, this is particularly important for the system
	// general industry because its labels are part of the tenant catalogue.
	before := old
	if in.Name != nil {
		old.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		old.Description = strings.TrimSpace(*in.Description)
	}
	if in.Icon != nil {
		old.Icon = strings.TrimSpace(*in.Icon)
	}
	if in.SortOrder != nil {
		old.SortOrder = *in.SortOrder
	}
	if in.Status != nil && (*in.Status == "active" || *in.Status == "disabled") {
		if id == generalIndustryID && *in.Status != "active" {
			writeError(w, 400, "GENERAL_INDUSTRY_REQUIRED", "general industry must remain active")
			return
		}
		old.Status = *in.Status
	}
	if old.Name == "" {
		writeError(w, 400, "INVALID_INDUSTRY", "industry name is required")
		return
	}
	// The lock acquired above also serializes this state change with catalogue
	// materialization, so Hub pulls cannot cache new content under an old
	// revision.
	_, err = h.db.ExecContext(r.Context(), `UPDATE industry_catalog_industries SET name=?,description=?,icon=?,sort_order=?,status=?,updated_at=? WHERE id=?`, old.Name, old.Description, old.Icon, old.SortOrder, old.Status, industryNow(), id)
	if err != nil {
		writeError(w, 500, "INDUSTRY_UPDATE_FAILED", "internal error")
		return
	}
	if err := h.refreshCataloguesForIndustryLocked(r.Context(), id); err != nil {
		writeError(w, 500, "INDUSTRY_UPDATE_FAILED", "catalogue refresh failed")
		return
	}
	h.appendAudit(r.Context(), r, "industry.updated", "industry", id, in.Reason, before, old)
	writeJSON(w, 200, old)
}

func industryDefinitionFromZip(data []byte) (json.RawMessage, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, file := range zr.File {
		if file.Name != "manifest.json" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(io.LimitReader(rc, 256<<10))
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		var manifest struct {
			Expert struct {
				ID           string   `json:"id"`
				Name         string   `json:"name"`
				Description  string   `json:"description"`
				Icon         string   `json:"icon"`
				SystemPrompt string   `json:"system_prompt"`
				Tools        []string `json:"tools"`
				Skills       []string `json:"skills"`
			} `json:"expert"`
		}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return nil, err
		}
		if strings.TrimSpace(manifest.Expert.Name) == "" || strings.TrimSpace(manifest.Expert.SystemPrompt) == "" {
			return nil, errors.New("market package lacks expert definition")
		}
		out, err := json.Marshal(manifest.Expert)
		return out, err
	}
	return nil, errors.New("market package manifest not found")
}

func (h *IndustryManagementHandlers) listEligibleAssets(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "INDUSTRY_ASSET_LIST_FAILED", "industry management unavailable")
		return
	}
	out := []any{}
	if h.market != nil {
		h.market.ensureExpertMarketSchema()
		rows, err := h.db.QueryContext(r.Context(), `SELECT `+expertMarketListingColumns+` FROM sm_expert_market_listings WHERE visibility='public' AND status='listed' AND platform_distribution=1 ORDER BY updated_at DESC`)
		if err != nil {
			writeError(w, 500, "INDUSTRY_ASSET_LIST_FAILED", "internal error")
			return
		}
		for rows.Next() {
			item, err := scanExpertMarketListing(rows)
			if err != nil {
				_ = rows.Close()
				writeError(w, 500, "INDUSTRY_ASSET_LIST_FAILED", "internal error")
				return
			}
			item.PlatformDistribution = true
			out = append(out, map[string]any{"listing_id": item.ID, "source_expert_id": item.SourceExpertID, "name": item.Name, "description": item.Description, "icon": item.Icon, "version": item.Version, "price": item.Price})
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			writeError(w, 500, "INDUSTRY_ASSET_LIST_FAILED", "internal error")
			return
		}
		_ = rows.Close()
	}
	acquiredRows, err := h.db.QueryContext(r.Context(), `SELECT id,listing_id,source_expert_id,version,price,name,description,icon,definition_json,package_hash,status FROM industry_catalog_assets ORDER BY updated_at DESC,id`)
	if err != nil {
		writeError(w, 500, "INDUSTRY_ASSET_LIST_FAILED", "internal error")
		return
	}
	defer acquiredRows.Close()
	acquired := []industryAsset{}
	for acquiredRows.Next() {
		var asset industryAsset
		var definition string
		if err := acquiredRows.Scan(&asset.ID, &asset.ListingID, &asset.SourceExpertID, &asset.Version, &asset.Price, &asset.Name, &asset.Description, &asset.Icon, &definition, &asset.PackageHash, &asset.Status); err != nil {
			writeError(w, 500, "INDUSTRY_ASSET_LIST_FAILED", "internal error")
			return
		}
		asset.Definition = nil // definitions are never an admin-list display API.
		acquired = append(acquired, asset)
	}
	writeJSON(w, 200, map[string]any{"assets": out, "acquired_assets": acquired})
}

func (h *IndustryManagementHandlers) acquireAsset(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "INDUSTRY_ASSET_ACQUIRE_FAILED", "industry management unavailable")
		return
	}
	h.market.ensureExpertMarketSchema()
	var in struct {
		ListingID string `json:"listing_id"`
		Reason    string `json:"reason"`
	}
	if err := decodeLimitedJSON(w, r, &in, defaultJSONBodyLimit); err != nil {
		writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
		return
	}
	item, err := scanExpertMarketListing(h.db.QueryRowContext(r.Context(), `SELECT `+expertMarketListingColumns+` FROM sm_expert_market_listings WHERE id=? AND visibility='public' AND status='listed' AND platform_distribution=1`, strings.TrimSpace(in.ListingID)))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 400, "INELIGIBLE_LISTING", "listing is not available for platform distribution")
		return
	}
	item.PlatformDistribution = true
	if err != nil {
		writeError(w, 500, "INDUSTRY_ASSET_ACQUIRE_FAILED", "internal error")
		return
	}
	data, err := os.ReadFile(item.ZipPath)
	if err != nil {
		writeError(w, 410, "PACKAGE_UNAVAILABLE", "market package unavailable")
		return
	}
	definition, err := industryDefinitionFromZip(data)
	if err != nil {
		writeError(w, 400, "INVALID_PACKAGE", "market package cannot be used as industry asset")
		return
	}
	sum := sha256.Sum256(data)
	asset := industryAsset{ID: industryID("industry_asset"), ListingID: item.ID, SourceExpertID: item.SourceExpertID, Version: item.Version, Price: item.Price, Name: item.Name, Description: item.Description, Icon: item.Icon, Definition: definition, PackageHash: hex.EncodeToString(sum[:]), Status: "ready"}
	now := industryNow()
	_, err = h.db.ExecContext(r.Context(), `INSERT INTO industry_catalog_assets(id,listing_id,source_expert_id,version,price,name,description,icon,definition_json,package_hash,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(listing_id) DO NOTHING`, asset.ID, asset.ListingID, asset.SourceExpertID, asset.Version, asset.Price, asset.Name, asset.Description, asset.Icon, string(asset.Definition), asset.PackageHash, asset.Status, now, now)
	if err != nil {
		writeError(w, 500, "INDUSTRY_ASSET_ACQUIRE_FAILED", "internal error")
		return
	}
	var storedDefinition string
	if err = h.db.QueryRowContext(r.Context(), `SELECT id,listing_id,source_expert_id,version,price,name,description,icon,definition_json,package_hash,status FROM industry_catalog_assets WHERE listing_id=?`, item.ID).Scan(&asset.ID, &asset.ListingID, &asset.SourceExpertID, &asset.Version, &asset.Price, &asset.Name, &asset.Description, &asset.Icon, &storedDefinition, &asset.PackageHash, &asset.Status); err != nil {
		writeError(w, 500, "INDUSTRY_ASSET_ACQUIRE_FAILED", "internal error")
		return
	}
	asset.Definition = json.RawMessage(storedDefinition)
	h.appendAudit(r.Context(), r, "asset.acquired", "asset", asset.ID, in.Reason, nil, asset)
	writeJSON(w, 201, asset)
}

func (h *IndustryManagementHandlers) replaceBindings(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "INDUSTRY_BINDINGS_FAILED", "industry management unavailable")
		return
	}
	industryID := strings.TrimSpace(r.PathValue("id"))
	var in struct {
		AssetIDs []string `json:"asset_ids"`
		Reason   string   `json:"reason"`
	}
	if err := decodeLimitedJSON(w, r, &in, defaultJSONBodyLimit); err != nil {
		writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
		return
	}
	// Canonicalize once, before both persistence and audit logging. Apart from
	// eliminating harmless duplicate IDs, this makes the audit record describe
	// the actual ordered catalogue configuration rather than raw client input.
	assetIDs := make([]string, 0, len(in.AssetIDs))
	seen := map[string]bool{}
	for _, assetID := range in.AssetIDs {
		assetID = strings.TrimSpace(assetID)
		if assetID == "" || seen[assetID] {
			continue
		}
		seen[assetID] = true
		assetIDs = append(assetIDs, assetID)
	}
	// Serialize validation, mutation, commit and catalogue revision as one
	// transition. Starting transactions before the lock can leave several open
	// writers queued behind an unrelated refresh in SQLite deployments.
	h.catalogueMu.Lock()
	defer h.catalogueMu.Unlock()
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, 500, "INDUSTRY_BINDINGS_FAILED", "internal error")
		return
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM industry_catalog_industries WHERE id=? AND status='active'`, industryID).Scan(&n); err != nil || n == 0 {
		writeError(w, 404, "INDUSTRY_NOT_FOUND", "active industry not found")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM industry_catalog_bindings WHERE industry_id=?`, industryID); err != nil {
		writeError(w, 500, "INDUSTRY_BINDINGS_FAILED", "internal error")
		return
	}
	now := industryNow()
	for order, assetID := range assetIDs {
		if err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM industry_catalog_assets WHERE id=? AND status='ready'`, assetID).Scan(&n); err != nil || n == 0 {
			writeError(w, 400, "INVALID_ASSET", "asset is not ready")
			return
		}
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO industry_catalog_bindings(industry_id,asset_id,display_order,status,created_at,updated_at) VALUES(?,?,?,'active',?,?)`, industryID, assetID, order, now, now); err != nil {
			writeError(w, 500, "INDUSTRY_BINDINGS_FAILED", "internal error")
			return
		}
	}
	// The lock is held across the binding commit and revision refresh so Hub
	// pulls cannot return a changed binding set with a stale revision.
	if err = tx.Commit(); err != nil {
		writeError(w, 500, "INDUSTRY_BINDINGS_FAILED", "internal error")
		return
	}
	if err := h.refreshCataloguesForIndustryLocked(r.Context(), industryID); err != nil {
		writeError(w, 500, "INDUSTRY_BINDINGS_FAILED", "catalogue refresh failed")
		return
	}
	h.appendAudit(r.Context(), r, "industry.bindings.replaced", "industry", industryID, in.Reason, nil, map[string]any{"asset_ids": assetIDs})
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (h *IndustryManagementHandlers) listBindings(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "INDUSTRY_BINDINGS_FAILED", "industry management unavailable")
		return
	}
	industryID := strings.TrimSpace(r.PathValue("id"))
	rows, err := h.db.QueryContext(r.Context(), `SELECT b.asset_id,b.display_order,b.status,a.listing_id,a.name,a.version,a.price FROM industry_catalog_bindings b JOIN industry_catalog_assets a ON a.id=b.asset_id WHERE b.industry_id=? ORDER BY b.display_order,b.asset_id`, industryID)
	if err != nil {
		writeError(w, 500, "INDUSTRY_BINDINGS_FAILED", "internal error")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var assetID, listingID, name, version, status string
		var order int
		var price int64
		if err := rows.Scan(&assetID, &order, &status, &listingID, &name, &version, &price); err != nil {
			writeError(w, 500, "INDUSTRY_BINDINGS_FAILED", "internal error")
			return
		}
		out = append(out, map[string]any{"asset_id": assetID, "listing_id": listingID, "name": name, "version": version, "price": price, "display_order": order, "status": status})
	}
	writeJSON(w, 200, map[string]any{"bindings": out})
}

func canonicalCatalogHash(experts []catalogExpert) string {
	canonical := append([]catalogExpert(nil), experts...)
	for i := range canonical {
		canonical[i].Industries = append([]catalogIndustry(nil), canonical[i].Industries...)
		sort.Slice(canonical[i].Industries, func(a, b int) bool {
			return canonical[i].Industries[a].ID < canonical[i].Industries[b].ID
		})
	}
	sort.Slice(canonical, func(i, j int) bool { return canonical[i].AssetID < canonical[j].AssetID })
	raw, _ := json.Marshal(canonical)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

type catalogIndustry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type catalogExpert struct {
	AssetID      string            `json:"asset_id"`
	ListingID    string            `json:"listing_id"`
	PackageHash  string            `json:"package_hash"`
	Version      string            `json:"version"`
	Price        int64             `json:"price"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Icon         string            `json:"icon"`
	Definition   json.RawMessage   `json:"definition"`
	Industries   []catalogIndustry `json:"industries"`
	DisplayOrder int               `json:"display_order"`
}

func (h *IndustryManagementHandlers) catalogue(ctx context.Context, hubID, tenantID string) ([]catalogExpert, error) {
	// Explicit tenant selections replace the default. Without one, the
	// catalogue is sourced from the built-in general industry.
	var explicitCount int
	// Exclude a legacy explicit general assignment. General is a system
	// fallback now; treating old rows as an explicit selection would preserve
	// an invalid mixed "general + specific" configuration indefinitely.
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM hub_tenant_industry_assignments WHERE hub_id=? AND tenant_id=? AND industry_id<>?`, hubID, tenantID, generalIndustryID).Scan(&explicitCount); err != nil {
		return nil, err
	}
	query := `SELECT a.id,a.listing_id,a.package_hash,a.version,a.price,a.name,a.description,a.icon,a.definition_json,i.id,i.name,b.display_order FROM hub_tenant_industry_assignments x JOIN industry_catalog_industries i ON i.id=x.industry_id AND i.status='active' JOIN industry_catalog_bindings b ON b.industry_id=i.id AND b.status='active' JOIN industry_catalog_assets a ON a.id=b.asset_id AND a.status='ready' WHERE x.hub_id=? AND x.tenant_id=? AND x.industry_id<>? ORDER BY b.display_order,a.id`
	args := []any{hubID, tenantID, generalIndustryID}
	if explicitCount == 0 {
		query = `SELECT a.id,a.listing_id,a.package_hash,a.version,a.price,a.name,a.description,a.icon,a.definition_json,i.id,i.name,b.display_order FROM industry_catalog_industries i JOIN industry_catalog_bindings b ON b.industry_id=i.id AND b.status='active' JOIN industry_catalog_assets a ON a.id=b.asset_id AND a.status='ready' WHERE i.id=? AND i.status='active' ORDER BY b.display_order,a.id`
		args = []any{generalIndustryID}
	}
	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byAsset := map[string]*catalogExpert{}
	for rows.Next() {
		var e catalogExpert
		var industry catalogIndustry
		var def string
		if err := rows.Scan(&e.AssetID, &e.ListingID, &e.PackageHash, &e.Version, &e.Price, &e.Name, &e.Description, &e.Icon, &def, &industry.ID, &industry.Name, &e.DisplayOrder); err != nil {
			return nil, err
		}
		e.Definition = json.RawMessage(def)
		if prior := byAsset[e.AssetID]; prior != nil {
			prior.Industries = append(prior.Industries, industry)
		} else {
			e.Industries = []catalogIndustry{industry}
			byAsset[e.AssetID] = &e
		}
	}
	out := make([]catalogExpert, 0, len(byAsset))
	for _, item := range byAsset {
		out = append(out, *item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DisplayOrder == out[j].DisplayOrder {
			return out[i].AssetID < out[j].AssetID
		}
		return out[i].DisplayOrder < out[j].DisplayOrder
	})
	return out, rows.Err()
}

// refreshTenantCatalogueRevision records the currently effective catalogue
// fingerprint. Hubs still pull the complete authoritative catalogue, but the
// revision lets them reliably notice removals caused by a disabled industry,
// changed binding, or revoked asset. A stable fingerprint never advances the
// revision, so harmless metadata edits do not generate synchronization churn.
func (h *IndustryManagementHandlers) refreshTenantCatalogueRevision(ctx context.Context, hubID, tenantID string) error {
	experts, err := h.catalogue(ctx, hubID, tenantID)
	if err != nil {
		return err
	}
	hash := canonicalCatalogHash(experts)
	var priorHash string
	var priorRevision int64
	err = h.db.QueryRowContext(ctx, `SELECT content_hash,revision FROM hub_tenant_industry_catalogs WHERE hub_id=? AND tenant_id=?`, hubID, tenantID).Scan(&priorHash, &priorRevision)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = h.db.ExecContext(ctx, `INSERT INTO hub_tenant_industry_catalogs(hub_id,tenant_id,revision,content_hash,updated_at) VALUES(?,?,1,?,?)`, hubID, tenantID, hash, industryNow())
		return err
	}
	if err != nil {
		return err
	}
	if priorHash == hash {
		return nil
	}
	_, err = h.db.ExecContext(ctx, `UPDATE hub_tenant_industry_catalogs SET revision=?,content_hash=?,updated_at=? WHERE hub_id=? AND tenant_id=?`, priorRevision+1, hash, industryNow(), hubID, tenantID)
	return err
}

// materializeTenantCatalogue establishes the first revision for a tenant
// scope on demand. This matters for the implicit general-industry fallback:
// no explicit assignment exists to create a row, but Hub still needs a
// non-zero revision it can compare on subsequent synchronizations.
func (h *IndustryManagementHandlers) materializeTenantCatalogue(ctx context.Context, hubID, tenantID string) (int64, string, []catalogExpert, error) {
	h.catalogueMu.Lock()
	defer h.catalogueMu.Unlock()
	if err := h.refreshTenantCatalogueRevision(ctx, hubID, tenantID); err != nil {
		return 0, "", nil, err
	}
	experts, err := h.catalogue(ctx, hubID, tenantID)
	if err != nil {
		return 0, "", nil, err
	}
	var revision int64
	var contentHash string
	if err := h.db.QueryRowContext(ctx, `SELECT revision,content_hash FROM hub_tenant_industry_catalogs WHERE hub_id=? AND tenant_id=?`, hubID, tenantID).Scan(&revision, &contentHash); err != nil {
		return 0, "", nil, err
	}
	return revision, contentHash, experts, nil
}

func (h *IndustryManagementHandlers) refreshCataloguesForIndustry(ctx context.Context, industryID string) error {
	h.catalogueMu.Lock()
	defer h.catalogueMu.Unlock()
	return h.refreshCataloguesForIndustryLocked(ctx, industryID)
}

// refreshCataloguesForIndustryLocked updates every catalogue affected by an
// industry while catalogueMu is held. Callers that mutate industry state use
// it to make the new effective content and its revision visible as one
// ordered control-plane transition.
func (h *IndustryManagementHandlers) refreshCataloguesForIndustryLocked(ctx context.Context, industryID string) error {
	targets, err := h.catalogueTargetsForIndustry(ctx, industryID)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if err := h.refreshTenantCatalogueRevision(ctx, target[0], target[1]); err != nil {
			return err
		}
	}
	return nil
}

// catalogueTargetsForIndustry returns tenants whose effective catalogue can
// reference industryID. For the general industry that includes known tenant
// scopes with no explicit assignment, because their fallback is implicit.
func (h *IndustryManagementHandlers) catalogueTargetsForIndustry(ctx context.Context, industryID string) ([][2]string, error) {
	query := `SELECT DISTINCT hub_id,tenant_id FROM hub_tenant_industry_assignments WHERE industry_id=?`
	args := []any{industryID}
	if industryID == generalIndustryID {
		query += ` UNION SELECT c.hub_id,c.tenant_id FROM hub_tenant_industry_catalogs c WHERE NOT EXISTS (SELECT 1 FROM hub_tenant_industry_assignments x WHERE x.hub_id=c.hub_id AND x.tenant_id=c.tenant_id AND x.industry_id<>?)`
		args = append(args, generalIndustryID)
	}
	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	targets := make([][2]string, 0)
	for rows.Next() {
		var hubID, tenantID string
		if err := rows.Scan(&hubID, &tenantID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		targets = append(targets, [2]string{hubID, tenantID})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return targets, nil
}

func (h *IndustryManagementHandlers) refreshCataloguesForAsset(ctx context.Context, assetID string) error {
	h.catalogueMu.Lock()
	defer h.catalogueMu.Unlock()
	return h.refreshCataloguesForAssetLocked(ctx, assetID)
}

// refreshCataloguesForAssetLocked is the asset counterpart of
// refreshCataloguesForIndustryLocked. Its caller has already acquired
// catalogueMu before publishing the asset state transition.
func (h *IndustryManagementHandlers) refreshCataloguesForAssetLocked(ctx context.Context, assetID string) error {
	// Fast path also avoids holding an empty SQLite cursor while a single
	// connection deployment advances the corresponding asset state.
	var bindingCount int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM industry_catalog_bindings WHERE asset_id=?`, assetID).Scan(&bindingCount); err != nil {
		return err
	}
	if bindingCount == 0 {
		return nil
	}
	rows, err := h.db.QueryContext(ctx, `SELECT DISTINCT industry_id FROM industry_catalog_bindings WHERE asset_id=?`, assetID)
	if err != nil {
		return err
	}
	industryIDs := make([]string, 0)
	for rows.Next() {
		var industryID string
		if err := rows.Scan(&industryID); err != nil {
			_ = rows.Close()
			return err
		}
		industryIDs = append(industryIDs, industryID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	targetSet := map[[2]string]struct{}{}
	for _, industryID := range industryIDs {
		targets, err := h.catalogueTargetsForIndustry(ctx, industryID)
		if err != nil {
			return err
		}
		for _, target := range targets {
			targetSet[target] = struct{}{}
		}
	}
	for target := range targetSet {
		if err := h.refreshTenantCatalogueRevision(ctx, target[0], target[1]); err != nil {
			return err
		}
	}
	return nil
}

func (h *IndustryManagementHandlers) revokeAsset(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "INDUSTRY_ASSET_REVOKE_FAILED", "industry management unavailable")
		return
	}
	assetID := strings.TrimSpace(r.PathValue("id"))
	var in struct {
		Reason string `json:"reason"`
	}
	if err := decodeLimitedJSON(w, r, &in, defaultJSONBodyLimit); err != nil {
		writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
		return
	}
	if strings.TrimSpace(in.Reason) == "" {
		writeError(w, 400, "REASON_REQUIRED", "revocation reason is required")
		return
	}
	// Include the read-before image in the transition lock so concurrent revoke
	// calls cannot report an active record after another request already revoked
	// it and refreshed the affected tenant catalogues.
	h.catalogueMu.Lock()
	defer h.catalogueMu.Unlock()
	var before industryAsset
	var beforeDefinition string
	err := h.db.QueryRowContext(r.Context(), `SELECT id,listing_id,source_expert_id,version,price,name,description,icon,definition_json,package_hash,status FROM industry_catalog_assets WHERE id=?`, assetID).Scan(&before.ID, &before.ListingID, &before.SourceExpertID, &before.Version, &before.Price, &before.Name, &before.Description, &before.Icon, &beforeDefinition, &before.PackageHash, &before.Status)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "ASSET_NOT_FOUND", "industry asset not found")
		return
	}
	before.Definition = json.RawMessage(beforeDefinition)
	if err != nil {
		writeError(w, 500, "INDUSTRY_ASSET_REVOKE_FAILED", "internal error")
		return
	}
	// QueryRow may still own a statement until its result is fully released on
	// some SQLite drivers. Ensure a separate statement can proceed before the
	// write below (production databases are unaffected, this also keeps local
	// single-connection deployments deterministic).
	if err := h.db.PingContext(r.Context()); err != nil {
		writeError(w, 500, "INDUSTRY_ASSET_REVOKE_FAILED", "internal error")
		return
	}
	// The lock acquired above covers the asset transition and its dependent
	// revision refresh.
	if before.Status != "revoked" {
		if _, err = h.db.ExecContext(r.Context(), `UPDATE industry_catalog_assets SET status='revoked',updated_at=? WHERE id=?`, industryNow(), assetID); err != nil {
			writeError(w, 500, "INDUSTRY_ASSET_REVOKE_FAILED", "internal error")
			return
		}
	}
	after := before
	after.Status = "revoked"
	var bindings int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM industry_catalog_bindings WHERE asset_id=?`, assetID).Scan(&bindings); err != nil {
		writeError(w, 500, "INDUSTRY_ASSET_REVOKE_FAILED", "internal error")
		return
	}
	if bindings > 0 {
		if err := h.refreshCataloguesForAssetLocked(r.Context(), assetID); err != nil {
			writeError(w, 500, "INDUSTRY_ASSET_REVOKE_FAILED", "catalogue refresh failed")
			return
		}
	}
	h.appendAudit(r.Context(), r, "asset.revoked", "asset", assetID, in.Reason, before, after)
	writeJSON(w, 200, after)
}

func (h *IndustryManagementHandlers) replaceTenantIndustries(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "TENANT_INDUSTRIES_FAILED", "industry management unavailable")
		return
	}
	hubID, tenantID := strings.TrimSpace(r.PathValue("hubId")), strings.TrimSpace(r.PathValue("tenantId"))
	if err := h.validateHubTenant(r.Context(), hubID, tenantID); err != nil {
		writeError(w, 404, "HUB_NOT_FOUND", "hub not found")
		return
	}
	var in struct {
		IndustryIDs []string `json:"industry_ids"`
		Reason      string   `json:"reason"`
	}
	if err := decodeLimitedJSON(w, r, &in, defaultJSONBodyLimit); err != nil {
		writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
		return
	}
	// Persist and report the same canonical selection. This also ensures a
	// malformed duplicate/blank request cannot be mistaken in the audit trail
	// for a different tenant configuration than the one actually applied.
	industryIDs := make([]string, 0, len(in.IndustryIDs))
	seen := map[string]bool{}
	for _, industryID := range in.IndustryIDs {
		industryID = strings.TrimSpace(industryID)
		if industryID == "" || seen[industryID] {
			continue
		}
		seen[industryID] = true
		industryIDs = append(industryIDs, industryID)
	}
	// Avoid opening a writer transaction before waiting for the transition lock;
	// this prevents unnecessary SQLite writer contention and keeps a Hub pull
	// from observing the committed assignment without its new revision.
	h.catalogueMu.Lock()
	defer h.catalogueMu.Unlock()
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, 500, "TENANT_INDUSTRIES_FAILED", "internal error")
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM hub_tenant_industry_assignments WHERE hub_id=? AND tenant_id=?`, hubID, tenantID); err != nil {
		writeError(w, 500, "TENANT_INDUSTRIES_FAILED", "internal error")
		return
	}
	now := industryNow()
	for _, id := range industryIDs {
		// General is an implicit fallback, never a tenant-selected industry.
		// Accepting it here would let a tenant combine the fallback catalogue
		// with specific industries and make an empty selection semantically
		// different from the same effective general-only catalogue.
		if id == generalIndustryID {
			writeError(w, 400, "GENERAL_INDUSTRY_IMPLICIT", "general industry is applied automatically when no industries are selected")
			return
		}
		var n int
		if err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM industry_catalog_industries WHERE id=? AND status='active'`, id).Scan(&n); err != nil || n == 0 {
			writeError(w, 400, "INVALID_INDUSTRY", "industry is not active")
			return
		}
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO hub_tenant_industry_assignments(hub_id,tenant_id,industry_id,created_at,updated_at) VALUES(?,?,?,?,?)`, hubID, tenantID, id, now, now); err != nil {
			writeError(w, 500, "TENANT_INDUSTRIES_FAILED", "internal error")
			return
		}
	}
	// An assignment replacement changes the entire effective catalogue. The lock
	// already held above prevents a Hub pull from observing its committed rows
	// until the revision has been recomputed.
	if err = tx.Commit(); err != nil {
		writeError(w, 500, "TENANT_INDUSTRIES_FAILED", "internal error")
		return
	}
	err = h.refreshTenantCatalogueRevision(r.Context(), hubID, tenantID)
	if err != nil {
		writeError(w, 500, "TENANT_INDUSTRIES_FAILED", "internal error")
		return
	}
	var revision int64
	var hash string
	if err := h.db.QueryRowContext(r.Context(), `SELECT revision,content_hash FROM hub_tenant_industry_catalogs WHERE hub_id=? AND tenant_id=?`, hubID, tenantID).Scan(&revision, &hash); err != nil {
		writeError(w, 500, "TENANT_INDUSTRIES_FAILED", "internal error")
		return
	}
	h.appendAudit(r.Context(), r, "tenant.industries.replaced", "hub_tenant", hubID+":"+tenantID, in.Reason, nil, map[string]any{"industry_ids": industryIDs, "revision": revision})
	writeJSON(w, 200, map[string]any{"ok": true, "revision": revision, "content_hash": hash})
}

func (h *IndustryManagementHandlers) listTenantIndustries(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "TENANT_INDUSTRIES_FAILED", "industry management unavailable")
		return
	}
	hubID, tenantID := strings.TrimSpace(r.PathValue("hubId")), strings.TrimSpace(r.PathValue("tenantId"))
	if err := h.validateHubTenant(r.Context(), hubID, tenantID); err != nil {
		writeError(w, 404, "HUB_NOT_FOUND", "hub not found")
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT i.id,i.code,i.name,i.status FROM hub_tenant_industry_assignments x JOIN industry_catalog_industries i ON i.id=x.industry_id WHERE x.hub_id=? AND x.tenant_id=? AND x.industry_id<>? ORDER BY i.sort_order,i.name`, hubID, tenantID, generalIndustryID)
	if err != nil {
		writeError(w, 500, "TENANT_INDUSTRIES_FAILED", "internal error")
		return
	}
	defer rows.Close()
	out := []industryRecord{}
	for rows.Next() {
		var item industryRecord
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.Status); err != nil {
			writeError(w, 500, "TENANT_INDUSTRIES_FAILED", "internal error")
			return
		}
		out = append(out, item)
	}
	explicitIDs := func() []string {
		ids := make([]string, 0, len(out))
		for _, item := range out {
			ids = append(ids, item.ID)
		}
		return ids
	}()
	usingDefault := len(explicitIDs) == 0
	if usingDefault {
		var general industryRecord
		if err := h.db.QueryRowContext(r.Context(), `SELECT id,code,name,status FROM industry_catalog_industries WHERE id=? AND status='active'`, generalIndustryID).Scan(&general.ID, &general.Code, &general.Name, &general.Status); err != nil {
			writeError(w, 500, "TENANT_INDUSTRIES_FAILED", "general industry unavailable")
			return
		}
		out = []industryRecord{general}
	}
	writeJSON(w, 200, map[string]any{"industry_ids": explicitIDs, "industries": out, "using_default": usingDefault})
}

func (h *IndustryManagementHandlers) tenantIndustryStatus(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "TENANT_INDUSTRIES_FAILED", "industry management unavailable")
		return
	}
	hubID, tenantID := strings.TrimSpace(r.PathValue("hubId")), strings.TrimSpace(r.PathValue("tenantId"))
	if err := h.validateHubTenant(r.Context(), hubID, tenantID); err != nil {
		writeError(w, 404, "HUB_NOT_FOUND", "hub not found")
		return
	}
	revision, hash, experts, err := h.materializeTenantCatalogue(r.Context(), hubID, tenantID)
	if err != nil {
		writeError(w, 500, "TENANT_INDUSTRIES_FAILED", "internal error")
		return
	}
	var updatedAt string
	if err := h.db.QueryRowContext(r.Context(), `SELECT updated_at FROM hub_tenant_industry_catalogs WHERE hub_id=? AND tenant_id=?`, hubID, tenantID).Scan(&updatedAt); err != nil {
		writeError(w, 500, "TENANT_INDUSTRIES_FAILED", "internal error")
		return
	}
	writeJSON(w, 200, map[string]any{"revision": revision, "content_hash": hash, "updated_at": updatedAt, "expert_count": len(experts)})
}

func (h *IndustryManagementHandlers) getHubCatalogue(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 503, "UNAVAILABLE", "industry management unavailable")
		return
	}
	hubID, tenantID := strings.TrimSpace(r.PathValue("hubId")), strings.TrimSpace(r.PathValue("tenantId"))
	secret := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if h.hubs == nil || h.hubs.VerifyHubSecret(r.Context(), hubID, secret) != nil {
		writeError(w, 401, "HUB_UNAUTHORIZED", "Hub is not registered")
		return
	}
	revision, hash, experts, err := h.materializeTenantCatalogue(r.Context(), hubID, tenantID)
	if err != nil {
		writeError(w, 500, "CATALOGUE_FAILED", "internal error")
		return
	}
	writeJSON(w, 200, map[string]any{"revision": revision, "content_hash": hash, "experts": experts})
}
