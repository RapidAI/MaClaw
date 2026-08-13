// Package industryexpert stores the HubCenter-managed, tenant-scoped expert
// catalogue separately from personal experts.  Keeping this state out of the
// normal experts table is essential: normal experts use bidirectional LWW
// synchronisation, while industry experts are a read-only control-plane feed.
package industryexpert

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Industry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Expert is one immutable asset selected by HubCenter for a tenant.
// Definition intentionally remains JSON: its runtime contract is shared with
// MaClaw GUI and should not be duplicated in Hub merely for persistence.
type Expert struct {
	AssetID      string          `json:"asset_id"`
	ListingID    string          `json:"listing_id"`
	PackageHash  string          `json:"package_hash"`
	Version      string          `json:"version,omitempty"`
	Price        int64           `json:"price"`
	Name         string          `json:"name,omitempty"`
	Description  string          `json:"description,omitempty"`
	Icon         string          `json:"icon,omitempty"`
	Industries   []Industry      `json:"industries"`
	DisplayOrder int             `json:"display_order"`
	Definition   json.RawMessage `json:"definition"`
}

type Catalog struct {
	Revision    int64    `json:"revision"`
	ContentHash string   `json:"content_hash,omitempty"`
	Experts     []Expert `json:"experts"`
}

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) InitSchema(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("managed industry expert store unavailable")
	}
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS managed_industry_expert_catalogs (
			tenant_id TEXT PRIMARY KEY, revision INTEGER NOT NULL DEFAULT 0,
			content_hash TEXT NOT NULL DEFAULT '', sync_status TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '', applied_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS managed_industry_experts (
			tenant_id TEXT NOT NULL, asset_id TEXT NOT NULL, listing_id TEXT NOT NULL DEFAULT '', package_hash TEXT NOT NULL DEFAULT '', version TEXT NOT NULL DEFAULT '', price INTEGER NOT NULL DEFAULT 0,
			name TEXT NOT NULL DEFAULT '', description TEXT NOT NULL DEFAULT '', icon TEXT NOT NULL DEFAULT '',
			definition_json TEXT NOT NULL, industries_json TEXT NOT NULL DEFAULT '[]',
			display_order INTEGER NOT NULL DEFAULT 0, revision INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY(tenant_id, asset_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_managed_industry_experts_tenant_order
			ON managed_industry_experts(tenant_id, display_order, asset_id)`,
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("managed industry expert schema: %w", err)
		}
	}
	// These additive upgrades keep existing Hub caches readable. New catalogues
	// always include listing metadata so GUI can apply the per-user market
	// entitlement without receiving a paid expert's definition.
	for _, stmt := range []string{
		`ALTER TABLE managed_industry_experts ADD COLUMN listing_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE managed_industry_experts ADD COLUMN package_hash TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE managed_industry_experts ADD COLUMN price INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE managed_industry_experts ADD COLUMN name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE managed_industry_experts ADD COLUMN description TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE managed_industry_experts ADD COLUMN icon TEXT NOT NULL DEFAULT ''`,
	} {
		_, _ = s.db.ExecContext(ctx, stmt)
	}
	return nil
}

func normalizeTenantID(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "tenant_default"
	}
	return v
}

func validateCatalog(in Catalog) error {
	seenAssets, seenExpertIDs := map[string]bool{}, map[string]bool{}
	for _, item := range in.Experts {
		assetID := strings.TrimSpace(item.AssetID)
		if assetID == "" || seenAssets[assetID] {
			return fmt.Errorf("invalid duplicate or empty industry asset")
		}
		seenAssets[assetID] = true
		if item.Price < 0 {
			return fmt.Errorf("invalid managed expert price")
		}
		// Every managed asset is an immutable Expert Market archive, regardless
		// of price. Requiring both its listing and the acquired archive hash makes
		// the Hub cache fail closed rather than letting an older/incomplete
		// control-plane response bypass GUI's package pinning.
		if strings.TrimSpace(item.ListingID) == "" {
			return fmt.Errorf("managed expert requires market listing")
		}
		packageHash := strings.TrimSpace(item.PackageHash)
		if len(packageHash) != sha256HexLength {
			return fmt.Errorf("managed expert requires sha256 package hash")
		}
		if _, err := hex.DecodeString(packageHash); err != nil {
			return fmt.Errorf("managed expert has invalid package hash")
		}
		if len(item.Definition) == 0 || string(item.Definition) == "null" {
			continue
		}
		var definition struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			SystemPrompt string `json:"system_prompt"`
		}
		if err := json.Unmarshal(item.Definition, &definition); err != nil || strings.TrimSpace(definition.ID) == "" || strings.TrimSpace(definition.Name) == "" || strings.TrimSpace(definition.SystemPrompt) == "" {
			return fmt.Errorf("invalid managed expert definition for asset %s", item.AssetID)
		}
		expertID := strings.TrimSpace(definition.ID)
		if seenExpertIDs[expertID] {
			return fmt.Errorf("duplicate managed expert id %s", expertID)
		}
		seenExpertIDs[expertID] = true
	}
	return nil
}

const sha256HexLength = 64

// Replace atomically applies a verified complete snapshot.  A caller may pass
// an empty (but valid) snapshot to revoke a tenant catalogue.
func (s *Store) Replace(ctx context.Context, tenantID string, catalog Catalog) error {
	if err := validateCatalog(catalog); err != nil {
		return err
	}
	tenantID = normalizeTenantID(tenantID)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Re-registration can overlap, or an older HubCenter response can arrive
	// after a newer one. A revision must never move the local control-plane
	// cache backwards, otherwise removed managed experts can reappear.
	var currentRevision int64
	err = tx.QueryRowContext(ctx, `SELECT revision FROM managed_industry_expert_catalogs WHERE tenant_id=?`, tenantID).Scan(&currentRevision)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil && catalog.Revision < currentRevision {
		return tx.Commit()
	}
	if err == nil && catalog.Revision == currentRevision {
		var currentHash string
		if err := tx.QueryRowContext(ctx, `SELECT content_hash FROM managed_industry_expert_catalogs WHERE tenant_id=?`, tenantID).Scan(&currentHash); err != nil {
			return err
		}
		if strings.TrimSpace(currentHash) != strings.TrimSpace(catalog.ContentHash) {
			return fmt.Errorf("managed industry catalogue revision %d has conflicting content", catalog.Revision)
		}
		// A retry may successfully retrieve the exact catalogue after a transient
		// failure. Keep the immutable rows intact, but clear the stale error so
		// monitoring reflects the most recent successful synchronization.
		if _, err := tx.ExecContext(ctx, `UPDATE managed_industry_expert_catalogs SET sync_status='ready',last_error='',applied_at=? WHERE tenant_id=?`, time.Now().UTC().Format(time.RFC3339Nano), tenantID); err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM managed_industry_experts WHERE tenant_id=?`, tenantID); err != nil {
		return err
	}
	for _, item := range catalog.Experts {
		definition, _ := json.Marshal(json.RawMessage(item.Definition))
		industries, _ := json.Marshal(item.Industries)
		if _, err = tx.ExecContext(ctx, `INSERT INTO managed_industry_experts(tenant_id,asset_id,listing_id,package_hash,version,price,name,description,icon,definition_json,industries_json,display_order,revision) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, tenantID, strings.TrimSpace(item.AssetID), strings.TrimSpace(item.ListingID), strings.TrimSpace(item.PackageHash), strings.TrimSpace(item.Version), item.Price, strings.TrimSpace(item.Name), strings.TrimSpace(item.Description), strings.TrimSpace(item.Icon), string(definition), string(industries), item.DisplayOrder, catalog.Revision); err != nil {
			return err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO managed_industry_expert_catalogs(tenant_id,revision,content_hash,sync_status,last_error,applied_at) VALUES(?,?,?,'ready','',?) ON CONFLICT(tenant_id) DO UPDATE SET revision=excluded.revision,content_hash=excluded.content_hash,sync_status='ready',last_error='',applied_at=excluded.applied_at`, tenantID, catalog.Revision, strings.TrimSpace(catalog.ContentHash), now)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MarkFailure(ctx context.Context, tenantID string, err error) {
	if s == nil || s.db == nil || err == nil {
		return
	}
	_, _ = s.db.ExecContext(ctx, `INSERT INTO managed_industry_expert_catalogs(tenant_id,sync_status,last_error) VALUES(?,'error',?) ON CONFLICT(tenant_id) DO UPDATE SET sync_status='error',last_error=excluded.last_error`, normalizeTenantID(tenantID), truncateError(err.Error()))
}

func truncateError(v string) string {
	if len(v) > 500 {
		return v[:500]
	}
	return v
}

func (s *Store) List(ctx context.Context, tenantID string) (Catalog, error) {
	tenantID = normalizeTenantID(tenantID)
	out := Catalog{Experts: []Expert{}}
	_ = s.db.QueryRowContext(ctx, `SELECT revision,content_hash FROM managed_industry_expert_catalogs WHERE tenant_id=?`, tenantID).Scan(&out.Revision, &out.ContentHash)
	rows, err := s.db.QueryContext(ctx, `SELECT asset_id,listing_id,package_hash,version,price,name,description,icon,definition_json,industries_json,display_order FROM managed_industry_experts WHERE tenant_id=? ORDER BY display_order,asset_id`, tenantID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item Expert
		var definition, industries string
		if err := rows.Scan(&item.AssetID, &item.ListingID, &item.PackageHash, &item.Version, &item.Price, &item.Name, &item.Description, &item.Icon, &definition, &industries, &item.DisplayOrder); err != nil {
			return out, err
		}
		item.Definition = json.RawMessage(definition)
		if err := json.Unmarshal([]byte(industries), &item.Industries); err != nil {
			return out, err
		}
		if item.Industries == nil {
			item.Industries = []Industry{}
		}
		out.Experts = append(out.Experts, item)
	}
	return out, rows.Err()
}
