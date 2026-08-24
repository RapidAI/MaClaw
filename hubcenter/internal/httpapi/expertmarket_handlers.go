package httpapi

// AI Expert Market is deliberately separate from the capability marketplace.
// An expert package is a portable, declarative configuration archive; it is
// not an executable Skill. The market stores an immutable archive per listing,
// grants permanent download entitlements, and reuses the existing Credits
// ledger for payment.

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
)

const (
	expertMarketMaxArchiveBytes      = 8 << 20
	expertMarketMultipartOverheadMax = 128 << 10
	expertMarketMaxRequestBytes      = expertMarketMaxArchiveBytes + expertMarketMultipartOverheadMax
	expertMarketMaxFiles             = 160
	expertMarketMaxUnpacked          = 20 << 20
	expertMarketMaxSkills            = 128
	expertMarketMaxPage              = 100000
	expertMarketMaxRequestIDRunes    = 128
)

var (
	expertMarketSchemaByDB sync.Map
	expertMarketIDPattern  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
)

type expertMarketListing struct {
	ID                   string `json:"id"`
	OwnerID              string `json:"-"`
	OwnerEmail           string `json:"-"`
	SourceExpertID       string `json:"source_expert_id"`
	Name                 string `json:"name"`
	Description          string `json:"description"`
	Icon                 string `json:"icon"`
	Version              string `json:"version"`
	Price                int64  `json:"price"`
	Visibility           string `json:"visibility"`
	Status               string `json:"status"`
	ZipPath              string `json:"-"`
	PackageSize          int64  `json:"package_size"`
	DownloadCount        int64  `json:"download_count"`
	PurchaseCount        int64  `json:"purchase_count"`
	SalesAmount          int64  `json:"sales_amount"`
	ReviewNote           string `json:"review_note,omitempty"`
	PlatformDistribution bool   `json:"platform_distribution"`
	CreatedAt            string `json:"created_at"`
	UpdatedAt            string `json:"updated_at"`
}

type expertMarketPublicListing struct {
	ID                   string `json:"id"`
	SourceExpertID       string `json:"source_expert_id"`
	Name                 string `json:"name"`
	Description          string `json:"description"`
	Icon                 string `json:"icon"`
	Version              string `json:"version"`
	Price                int64  `json:"price"`
	PackageSize          int64  `json:"package_size"`
	DownloadCount        int64  `json:"download_count"`
	PurchaseCount        int64  `json:"purchase_count"`
	PlatformDistribution bool   `json:"platform_distribution"`
	Owned                bool   `json:"owned"`
	CreatedAt            string `json:"created_at"`
}

type expertMarketAdminListing struct {
	expertMarketPublicListing
	Visibility  string `json:"visibility"`
	Status      string `json:"status"`
	OwnerID     string `json:"owner_id"`
	OwnerEmail  string `json:"owner_email"`
	SalesAmount int64  `json:"sales_amount"`
	ReviewNote  string `json:"review_note,omitempty"`
	UpdatedAt   string `json:"updated_at"`
}

// Keep the legacy test and administration INSERT column sequence stable. The
// distribution bit was added later, so reads use the explicit SELECT constant
// below while historical fixtures and maintenance SQL retain this sequence.
const expertMarketListingColumns = "id, owner_user_id, owner_email, source_expert_id, name, description, icon, version, price, visibility, status, zip_path, package_size, download_count, purchase_count, sales_amount, review_note, created_at, updated_at"
const expertMarketListingSelectColumns = expertMarketListingColumns + ", platform_distribution"

func qualifiedExpertMarketListingSelectColumns(alias string) string {
	if strings.TrimSpace(alias) == "" {
		return expertMarketListingSelectColumns
	}
	columns := strings.Split(expertMarketListingSelectColumns, ", ")
	for i, column := range columns {
		columns[i] = alias + "." + column
	}
	return strings.Join(columns, ", ")
}

func (h *SkillMarketHandlers) ensureExpertMarketSchema() {
	if h == nil || h.store == nil || h.store.DB() == nil {
		return
	}
	db := h.store.DB()
	once, _ := expertMarketSchemaByDB.LoadOrStore(db, &sync.Once{})
	once.(*sync.Once).Do(func() {
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS sm_expert_market_listings (
			id TEXT PRIMARY KEY, owner_user_id TEXT NOT NULL, owner_email TEXT NOT NULL,
			source_expert_id TEXT NOT NULL, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', icon TEXT NOT NULL DEFAULT '',
			version TEXT NOT NULL DEFAULT '1', price INTEGER NOT NULL DEFAULT 0, visibility TEXT NOT NULL DEFAULT 'public', status TEXT NOT NULL DEFAULT 'pending_review',
			zip_path TEXT NOT NULL, package_size INTEGER NOT NULL DEFAULT 0, download_count INTEGER NOT NULL DEFAULT 0,
			purchase_count INTEGER NOT NULL DEFAULT 0, sales_amount INTEGER NOT NULL DEFAULT 0, review_note TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`)
		// Existing installations predate the visibility choice. Keep their
		// behaviour unchanged by treating them as public submissions.
		_, _ = db.Exec(`ALTER TABLE sm_expert_market_listings ADD COLUMN visibility TEXT NOT NULL DEFAULT 'public'`)
		// Platform distribution is opt-in. A listing can be freely downloaded by
		// an individual while still being ineligible for tenant-wide industry
		// distribution until its author gives this separate permission.
		_, _ = db.Exec(`ALTER TABLE sm_expert_market_listings ADD COLUMN platform_distribution INTEGER NOT NULL DEFAULT 0`)
		_, _ = db.Exec(`UPDATE sm_expert_market_listings SET visibility='public' WHERE visibility IS NULL OR visibility NOT IN ('public', 'private')`)
		// The stable package identity is global. Without this guard a buyer could
		// download an expert package and re-submit it as their own listing.
		_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_sm_expert_market_source_global ON sm_expert_market_listings(source_expert_id)`)
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sm_expert_market_status_updated ON sm_expert_market_listings(status, updated_at DESC)`)
		// Public browsing always scopes to this pair before sorting. This keeps
		// catalogue scans bounded as private and archived submissions accumulate.
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sm_expert_market_public_created ON sm_expert_market_listings(visibility, status, created_at DESC, id)`)
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sm_expert_market_public_downloads ON sm_expert_market_listings(visibility, status, download_count DESC, id)`)
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sm_expert_market_public_sales ON sm_expert_market_listings(visibility, status, sales_amount DESC, id)`)
		// Before approval became an immediate publication, listings could remain
		// in an intermediate "approved" state. Fold that legacy state into the
		// current lifecycle so existing submissions are not stranded without an
		// available action in the admin screen.
		_, _ = db.Exec(`UPDATE sm_expert_market_listings SET status='listed' WHERE status='approved'`)
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sm_expert_market_owner ON sm_expert_market_listings(owner_user_id, updated_at DESC)`)
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS sm_expert_market_purchases (
			id TEXT PRIMARY KEY, listing_id TEXT NOT NULL, buyer_user_id TEXT NOT NULL, buyer_email TEXT NOT NULL,
			amount_paid INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'active', created_at TEXT NOT NULL,
			UNIQUE(listing_id, buyer_user_id)
		)`)
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sm_expert_market_purchases_buyer ON sm_expert_market_purchases(buyer_user_id, status, listing_id)`)
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS sm_expert_market_downloads (
			id TEXT PRIMARY KEY, listing_id TEXT NOT NULL, downloader_user_id TEXT NOT NULL, created_at TEXT NOT NULL,
			UNIQUE(listing_id, downloader_user_id)
		)`)
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS sm_expert_market_events (
			id TEXT PRIMARY KEY, listing_id TEXT NOT NULL, actor TEXT NOT NULL, action TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
		)`)
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sm_expert_market_events_listing ON sm_expert_market_events(listing_id, created_at DESC)`)
		// Lifecycle events predate owner administration and only preserve a text
		// actor/reason. Keep them for compatibility, but write owner changes and
		// administrator-only operations to a structured append-only audit trail.
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS sm_expert_market_audit_events (
			id TEXT PRIMARY KEY, listing_id TEXT NOT NULL, actor_admin_id TEXT NOT NULL,
			action TEXT NOT NULL, reason TEXT NOT NULL DEFAULT '', before_json TEXT NOT NULL DEFAULT '{}',
			after_json TEXT NOT NULL DEFAULT '{}', request_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
		)`)
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sm_expert_market_audit_events_listing ON sm_expert_market_audit_events(listing_id, created_at DESC)`)
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS sm_expert_market_installations (
			id TEXT PRIMARY KEY, listing_id TEXT NOT NULL, user_id TEXT NOT NULL,
			local_expert_id TEXT NOT NULL DEFAULT '', version TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL, failure_stage TEXT NOT NULL DEFAULT '', error_message TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`)
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sm_expert_market_installations_listing ON sm_expert_market_installations(listing_id, created_at DESC)`)
		// Reports represent the current local install outcome for a buyer and
		// package, not an unbounded event stream. Consolidate any rows written
		// before this uniqueness rule was introduced, retaining the latest state.
		_, _ = db.Exec(`DELETE FROM sm_expert_market_installations
			WHERE id IN (
				SELECT older.id FROM sm_expert_market_installations older
				JOIN sm_expert_market_installations newer
					ON newer.listing_id=older.listing_id AND newer.user_id=older.user_id
					AND (newer.updated_at > older.updated_at OR (newer.updated_at=older.updated_at AND newer.id > older.id))
			)`)
		_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_sm_expert_market_installations_listing_user ON sm_expert_market_installations(listing_id, user_id)`)
	})
}

func (h *SkillMarketHandlers) expertMarketDir() string {
	return filepath.Join(h.dataDir, "expert-market")
}

// removeExpertMarketPackage only permits archive paths owned by this market's
// storage directory. Database rows can outlive a configuration migration or be
// edited by an operator; they must never turn a private-share deletion into an
// arbitrary filesystem delete.
func (h *SkillMarketHandlers) removeExpertMarketPackage(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	base, err := filepath.Abs(h.expertMarketDir())
	if err != nil {
		return fmt.Errorf("resolve expert package directory: %w", err)
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve expert package path: %w", err)
	}
	info, err := os.Lstat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inspect expert package: %w", err)
	}
	// Packages are always immutable regular .zip files written by the market.
	// Do not follow a final symlink or accept a directory here: either shape
	// would make a malformed legacy row capable of deleting something other
	// than one stored package. Parent symlinks are still resolved below before
	// the containment check.
	if !info.Mode().IsRegular() {
		return fmt.Errorf("expert package is not a regular file")
	}
	// Absolute-path containment alone is insufficient: a legacy or manually
	// edited row could point through a directory symlink inside expert-market to
	// a file outside it. Resolve both paths before enforcing containment so the
	// cleanup endpoint cannot become an arbitrary filesystem deletion primitive.
	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return fmt.Errorf("resolve expert package directory: %w", err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return fmt.Errorf("resolve expert package path: %w", err)
	}
	rel, err := filepath.Rel(resolvedBase, resolvedTarget)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("expert package is outside market storage")
	}
	if err := os.Remove(resolvedTarget); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove expert package: %w", err)
	}
	return nil
}

// expertMarketPackagePath accepts only the ordinary archive files this market
// writes below its own storage directory. Listing metadata is operational data,
// not a filesystem capability: it must not let a malformed legacy row expose
// an arbitrary file through the download endpoint.
func (h *SkillMarketHandlers) expertMarketPackagePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("expert package is unavailable")
	}
	base, err := filepath.Abs(h.expertMarketDir())
	if err != nil {
		return "", fmt.Errorf("resolve expert package directory: %w", err)
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve expert package path: %w", err)
	}
	info, err := os.Lstat(target)
	if err != nil {
		return "", fmt.Errorf("inspect expert package: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("expert package is not a regular file")
	}
	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", fmt.Errorf("resolve expert package directory: %w", err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("resolve expert package path: %w", err)
	}
	rel, err := filepath.Rel(resolvedBase, resolvedTarget)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("expert package is outside market storage")
	}
	return resolvedTarget, nil
}

func expertMarketNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func scanExpertMarketListing(row interface{ Scan(...any) error }) (*expertMarketListing, error) {
	var item expertMarketListing
	err := row.Scan(&item.ID, &item.OwnerID, &item.OwnerEmail, &item.SourceExpertID, &item.Name, &item.Description, &item.Icon, &item.Version, &item.Price, &item.Visibility, &item.Status, &item.ZipPath, &item.PackageSize, &item.DownloadCount, &item.PurchaseCount, &item.SalesAmount, &item.ReviewNote, &item.CreatedAt, &item.UpdatedAt, &item.PlatformDistribution)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func publicExpertMarketListing(item *expertMarketListing) expertMarketPublicListing {
	return expertMarketPublicListing{ID: item.ID, SourceExpertID: item.SourceExpertID, Name: item.Name, Description: item.Description, Icon: item.Icon, Version: item.Version, Price: item.Price, PackageSize: item.PackageSize, DownloadCount: item.DownloadCount, PurchaseCount: item.PurchaseCount, PlatformDistribution: item.PlatformDistribution, CreatedAt: item.CreatedAt}
}

// expertMarketOwnedListings resolves all of one page's entitlement flags in a
// single query. Keeping this out of the listing query preserves the durable
// market ordering while avoiding an N+1 purchase lookup for every card.
func (h *SkillMarketHandlers) expertMarketOwnedListings(r *http.Request, userID string, ids []string) (map[string]bool, error) {
	owned := make(map[string]bool)
	if len(ids) == 0 {
		return owned, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, userID)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	// Publishers are entitled to retrieve their own listed package as well. The
	// consumer UI uses this flag to offer Install instead of an impossible
	// self-purchase.
	query := `SELECT listing_id FROM sm_expert_market_purchases WHERE buyer_user_id=? AND status='active' AND listing_id IN (` + strings.Join(placeholders, ",") + `) UNION SELECT id AS listing_id FROM sm_expert_market_listings WHERE owner_user_id=? AND id IN (` + strings.Join(placeholders, ",") + `)`
	args = append(args, userID)
	for _, id := range ids {
		args = append(args, id)
	}
	// The caller uses this ownership bit in the same catalogue response. Keep
	// it on the primary with the catalogue rows so a replica cannot report a
	// just-purchased/listed expert as unowned or fail while the primary is live.
	rows, err := h.store.DB().QueryContext(r.Context(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		owned[id] = true
	}
	return owned, rows.Err()
}

func adminExpertMarketListing(item *expertMarketListing) expertMarketAdminListing {
	return expertMarketAdminListing{expertMarketPublicListing: publicExpertMarketListing(item), Visibility: item.Visibility, Status: item.Status, OwnerID: item.OwnerID, OwnerEmail: item.OwnerEmail, SalesAmount: item.SalesAmount, ReviewNote: item.ReviewNote, UpdatedAt: item.UpdatedAt}
}

func (h *SkillMarketHandlers) expertMarketUser(r *http.Request) (string, string, error) {
	// Expert Market has identical session/account semantics to Pet Store. Keep a
	// separate named helper so its future policy can diverge without leaking the
	// Pet Store domain into this API.
	return h.petStoreUser(r)
}

func expertMarketValidStatus(status string) bool {
	switch status {
	case "pending_review", "listed", "private", "unlisted", "rejected", "deleted", "purged":
		return true
	default:
		return false
	}
}

// expertMarketPageBounds prevents a deliberately huge page query from
// overflowing OFFSET arithmetic or forcing SQLite to walk an impractical
// number of rows. Page 1 remains the forgiving default for malformed input.
func expertMarketPageBounds(raw string) (int, bool) {
	if strings.TrimSpace(raw) == "" {
		return 1, true
	}
	page, err := strconv.Atoi(raw)
	if err != nil || page < 1 || page > expertMarketMaxPage {
		return 0, false
	}
	return page, true
}

// expertMarketListingID keeps every listing-scoped endpoint on the same route
// contract. IDs are generated by the service; accepting arbitrary path text
// adds needless database work and makes lifecycle responses inconsistent.
func expertMarketListingID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.TrimSpace(r.PathValue("id"))
	if !expertMarketIDPattern.MatchString(id) {
		smError(w, http.StatusNotFound, "expert listing not found")
		return "", false
	}
	return id, true
}

func expertMarketManifest(data []byte) (sourceID, name, description, icon string, err error) {
	if len(data) == 0 || len(data) > expertMarketMaxArchiveBytes {
		return "", "", "", "", fmt.Errorf("expert package exceeds size limit")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", "", "", "", fmt.Errorf("invalid expert package: %w", err)
	}
	if len(zr.File) == 0 || len(zr.File) > expertMarketMaxFiles {
		return "", "", "", "", fmt.Errorf("expert package has invalid file count")
	}
	var manifestData []byte
	files := make(map[string]bool, len(zr.File))
	var total uint64
	for _, file := range zr.File {
		// Match the desktop importer exactly: directory entries cannot be
		// installed there, so they must not pass market submission either.
		if file == nil || file.FileInfo().IsDir() {
			return "", "", "", "", fmt.Errorf("expert package contains unsupported directory entry")
		}
		if file.FileInfo().Mode()&os.ModeSymlink != 0 {
			return "", "", "", "", fmt.Errorf("expert package contains unsupported symbolic link")
		}
		path := filepath.ToSlash(file.Name)
		if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "../") || strings.Contains(path, "\\") || path != filepath.ToSlash(filepath.Clean(path)) || files[path] {
			return "", "", "", "", fmt.Errorf("expert package contains unsafe path")
		}
		files[path] = true
		total += file.UncompressedSize64
		if total > expertMarketMaxUnpacked {
			return "", "", "", "", fmt.Errorf("expert package expands beyond size limit")
		}
		if path == "manifest.json" {
			if file.UncompressedSize64 > 256<<10 {
				return "", "", "", "", fmt.Errorf("expert manifest is too large")
			}
			rc, openErr := file.Open()
			if openErr != nil {
				return "", "", "", "", openErr
			}
			manifestData, err = io.ReadAll(io.LimitReader(rc, 256<<10+1))
			_ = rc.Close()
			if err != nil || len(manifestData) > 256<<10 {
				return "", "", "", "", fmt.Errorf("read expert manifest")
			}
		}
	}
	if len(manifestData) == 0 {
		return "", "", "", "", fmt.Errorf("expert package is missing manifest.json")
	}
	var manifest struct {
		Format          string `json:"format"`
		Version         int    `json:"version"`
		ExpertPackageID string `json:"expert_package_id"`
		Expert          struct {
			ID           string   `json:"id"`
			Name         string   `json:"name"`
			Description  string   `json:"description"`
			Icon         string   `json:"icon"`
			SystemPrompt string   `json:"system_prompt"`
			Tools        []string `json:"tools"`
			Skills       []string `json:"skills"`
			Builtin      bool     `json:"builtin"`
		} `json:"expert"`
		Skills []struct {
			Name    string `json:"name"`
			Archive string `json:"archive"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return "", "", "", "", fmt.Errorf("parse expert manifest: %w", err)
	}
	if manifest.Format != "maclaw-expert-package" || manifest.Version != 1 || !strings.HasPrefix(manifest.ExpertPackageID, "pkgexp-") || !expertMarketIDPattern.MatchString(manifest.ExpertPackageID) {
		return "", "", "", "", fmt.Errorf("unsupported or invalid expert package")
	}
	name, description, icon = strings.TrimSpace(manifest.Expert.Name), strings.TrimSpace(manifest.Expert.Description), strings.TrimSpace(manifest.Expert.Icon)
	if name == "" || len([]rune(name)) > 80 || len([]rune(description)) > 1000 || len([]rune(icon)) > 32 {
		return "", "", "", "", fmt.Errorf("expert package has invalid display fields")
	}
	if strings.TrimSpace(manifest.Expert.SystemPrompt) == "" || len([]rune(manifest.Expert.SystemPrompt)) > 64000 {
		return "", "", "", "", fmt.Errorf("expert package has an invalid system prompt")
	}
	// Expert packages are strictly for user-created definitions. This matches
	// the desktop importer, which refuses built-in experts before any local
	// state changes. The exporter deliberately omits the local expert ID.
	if manifest.Expert.Builtin || strings.HasPrefix(strings.ToLower(strings.TrimSpace(manifest.Expert.ID)), "builtin-") {
		return "", "", "", "", fmt.Errorf("expert package must contain a user-created expert")
	}
	if len(manifest.Skills) > expertMarketMaxSkills || len(manifest.Expert.Tools) > 128 || len(manifest.Expert.Skills) > expertMarketMaxSkills {
		return "", "", "", "", fmt.Errorf("expert package declares too many dependencies")
	}
	declaredArchives := map[string]bool{"manifest.json": true}
	declaredSkills := make(map[string]bool, len(manifest.Skills))
	for _, skill := range manifest.Skills {
		skill.Name = strings.TrimSpace(skill.Name)
		skill.Archive = strings.TrimSpace(skill.Archive)
		if skill.Name == "" || !expertMarketIDPattern.MatchString(skill.Name) || skill.Archive == "" || !strings.HasPrefix(skill.Archive, "skills/") || !strings.HasSuffix(skill.Archive, ".zip") || skill.Archive != filepath.ToSlash(filepath.Clean(skill.Archive)) || declaredArchives[skill.Archive] || declaredSkills[skill.Name] {
			return "", "", "", "", fmt.Errorf("expert package has an invalid skill declaration")
		}
		if !files[skill.Archive] {
			return "", "", "", "", fmt.Errorf("expert package is missing skill archive %q", skill.Archive)
		}
		declaredArchives[skill.Archive] = true
		declaredSkills[skill.Name] = true
	}
	for path := range files {
		if !declaredArchives[path] {
			return "", "", "", "", fmt.Errorf("expert package contains unexpected file %q", path)
		}
	}
	for _, name := range manifest.Expert.Skills {
		name = strings.TrimSpace(name)
		if name == "" || !declaredSkills[name] {
			return "", "", "", "", fmt.Errorf("expert package requires undeclared skill %q", name)
		}
	}
	return manifest.ExpertPackageID, name, description, icon, nil
}

func (h *SkillMarketHandlers) ListExpertMarketListings(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	userID, _, err := h.expertMarketUser(r)
	if err != nil {
		smError(w, http.StatusUnauthorized, err.Error())
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) > 100 {
		smError(w, http.StatusBadRequest, "search query is too long")
		return
	}
	page, ok := expertMarketPageBounds(r.URL.Query().Get("page"))
	if !ok {
		smError(w, http.StatusBadRequest, "invalid page")
		return
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 30 {
		pageSize = 20
	}
	sortKey := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort")))
	sortColumn := "created_at"
	if sortKey == "downloads" {
		sortColumn = "download_count"
	} else if sortKey == "sales" {
		sortColumn = "sales_amount"
	}
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
	where, args := " WHERE status='listed' AND visibility='public' AND (name LIKE ? ESCAPE '\\' OR description LIKE ? ESCAPE '\\')", []any{"%" + escaped + "%", "%" + escaped + "%"}
	var total int
	// A public catalogue response must be internally consistent. Reading its
	// count from the primary but its rows from a lagging replica can yield an
	// empty grid with a non-zero total immediately after moderation or unlisting.
	// Use the primary for this compact, paginated query.
	if err := h.store.DB().QueryRowContext(r.Context(), "SELECT COUNT(*) FROM sm_expert_market_listings"+where, args...).Scan(&total); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows, err := h.store.DB().QueryContext(r.Context(), "SELECT "+expertMarketListingSelectColumns+" FROM sm_expert_market_listings"+where+" ORDER BY "+sortColumn+" DESC, id ASC LIMIT ? OFFSET ?", append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	items := make([]expertMarketPublicListing, 0)
	ids := make([]string, 0, pageSize)
	for rows.Next() {
		item, scanErr := scanExpertMarketListing(rows)
		if scanErr != nil {
			smError(w, http.StatusInternalServerError, scanErr.Error())
			return
		}
		items = append(items, publicExpertMarketListing(item))
		ids = append(ids, item.ID)
	}
	if err := rows.Err(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	owned, err := h.expertMarketOwnedListings(r, userID, ids)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range items {
		items[i].Owned = owned[items[i].ID]
	}
	writeJSON(w, http.StatusOK, map[string]any{"experts": items, "total": total, "page": page, "page_size": pageSize, "total_pages": (total + pageSize - 1) / pageSize})
}

func (h *SkillMarketHandlers) GetExpertMarketListing(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	userID, _, err := h.expertMarketUser(r)
	if err != nil {
		smError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, ok := expertMarketListingID(w, r)
	if !ok {
		return
	}
	// Details must have the same distribution boundary as catalogue search and
	// purchase. A private listing is never a public resource, even if an older
	// or manual database update left it with the listed status.
	item, err := scanExpertMarketListing(h.store.DB().QueryRowContext(r.Context(), "SELECT "+expertMarketListingSelectColumns+" FROM sm_expert_market_listings WHERE id=? AND visibility='public' AND status='listed'", id))
	if err != nil {
		smError(w, http.StatusNotFound, "expert listing not found")
		return
	}
	public := publicExpertMarketListing(item)
	owned, ownErr := h.expertMarketOwnedListings(r, userID, []string{item.ID})
	if ownErr != nil {
		smError(w, http.StatusInternalServerError, ownErr.Error())
		return
	}
	public.Owned = owned[item.ID]
	writeJSON(w, http.StatusOK, public)
}

func (h *SkillMarketHandlers) SubmitExpertMarketListing(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	userID, email, err := h.expertMarketUser(r)
	if err != nil {
		smError(w, http.StatusUnauthorized, err.Error())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, expertMarketMaxRequestBytes)
	if err := r.ParseMultipartForm(expertMarketMaxRequestBytes); err != nil {
		smError(w, http.StatusBadRequest, "invalid expert package form")
		return
	}
	price, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("price")), 10, 64)
	if err != nil || price < 0 || price > 999999 {
		smError(w, http.StatusBadRequest, "price must be 0-999999 credits")
		return
	}
	visibility := strings.ToLower(strings.TrimSpace(r.FormValue("visibility")))
	if visibility == "" {
		visibility = "public"
	}
	platformDistribution := strings.EqualFold(strings.TrimSpace(r.FormValue("platform_distribution")), "true") || strings.TrimSpace(r.FormValue("platform_distribution")) == "1"
	if visibility != "public" && visibility != "private" {
		smError(w, http.StatusBadRequest, "visibility must be public or private")
		return
	}
	// Distribution is an explicit consent for a public listing. Ignore any
	// stale or forged opt-in sent with a private share so making it public in a
	// later lifecycle step cannot silently reactivate that consent.
	if visibility == "private" {
		platformDistribution = false
	}
	version := strings.TrimSpace(r.FormValue("version"))
	if version == "" {
		version = "1"
	}
	if len([]rune(version)) > 80 {
		smError(w, http.StatusBadRequest, "version is too long")
		return
	}
	f, _, err := r.FormFile("package")
	if err != nil {
		smError(w, http.StatusBadRequest, "expert package is required")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, expertMarketMaxArchiveBytes+1))
	if err != nil {
		smError(w, http.StatusBadRequest, "cannot read expert package")
		return
	}
	sourceID, name, description, icon, err := expertMarketManifest(data)
	if err != nil {
		smError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.MkdirAll(h.expertMarketDir(), 0o755); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id := "expert_" + uniqueID("listing")
	path := filepath.Join(h.expertMarketDir(), id+".zip")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		smError(w, http.StatusInternalServerError, "save expert package: "+err.Error())
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(path)
		}
	}()
	now := expertMarketNow()
	tx, err := h.store.BeginImmediate(r.Context())
	if err != nil {
		smError(w, http.StatusInternalServerError, "create expert listing")
		return
	}
	defer tx.Rollback()
	var existing int
	if err = tx.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM sm_expert_market_listings WHERE source_expert_id=?", sourceID).Scan(&existing); err == nil && existing > 0 {
		smError(w, http.StatusConflict, "this expert package is already submitted; use its existing listing")
		return
	}
	status := "pending_review"
	if visibility == "private" {
		status = "private"
	}
	item := &expertMarketListing{ID: id, OwnerID: userID, OwnerEmail: email, SourceExpertID: sourceID, Name: name, Description: description, Icon: icon, Version: version, Price: price, Visibility: visibility, Status: status, ZipPath: path, PackageSize: int64(len(data)), PlatformDistribution: platformDistribution, CreatedAt: now, UpdatedAt: now}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), "INSERT INTO sm_expert_market_listings ("+expertMarketListingColumns+", platform_distribution) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0, 0, '', ?, ?, ?)", item.ID, item.OwnerID, item.OwnerEmail, item.SourceExpertID, item.Name, item.Description, item.Icon, item.Version, item.Price, item.Visibility, item.Status, item.ZipPath, item.PackageSize, item.CreatedAt, item.UpdatedAt, item.PlatformDistribution)
	}
	if err == nil {
		action, reason := "submitted", "submitted for review"
		if visibility == "private" {
			action, reason = "shared_privately", "shared privately without review"
		}
		// Event actors use the durable principal too. A contact can change when
		// the same Hub user signs in through a different bound identity.
		err = h.recordExpertMarketEventAsTx(r, tx, id, userID, action, reason)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			smError(w, http.StatusConflict, "this expert is already submitted")
		} else {
			smError(w, http.StatusInternalServerError, "create expert listing: "+err.Error())
		}
		return
	}
	committed = true
	writeJSON(w, http.StatusCreated, adminExpertMarketListing(item))
}

func (h *SkillMarketHandlers) GetExpertMarketAccount(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	userID, email, err := h.expertMarketUser(r)
	if err != nil {
		smError(w, http.StatusUnauthorized, err.Error())
		return
	}
	// Account data must reflect a just-created submission. In HA deployments the
	// read connection can lag the local writer, causing the creator's new expert
	// to disappear from “My submissions” immediately after a successful share.
	// Owner-scoped account data is small and consistency-sensitive, so read it
	// from the authoritative writer connection.
	rows, err := h.store.DB().QueryContext(r.Context(), "SELECT "+expertMarketListingSelectColumns+" FROM sm_expert_market_listings WHERE owner_user_id=? ORDER BY updated_at DESC", userID)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	uploads := make([]expertMarketAdminListing, 0)
	for rows.Next() {
		item, scanErr := scanExpertMarketListing(rows)
		if scanErr != nil {
			smError(w, http.StatusInternalServerError, scanErr.Error())
			return
		}
		uploads = append(uploads, adminExpertMarketListing(item))
	}
	// A corrupt legacy entitlement must not turn an owner-only record into a
	// buyer-visible library item. Only public listings can ever be acquired.
	purchaseRows, err := h.store.DB().QueryContext(r.Context(), "SELECT "+qualifiedExpertMarketListingSelectColumns("l")+" FROM sm_expert_market_purchases p JOIN sm_expert_market_listings l ON l.id=p.listing_id WHERE p.buyer_user_id=? AND p.status='active' AND l.visibility='public' ORDER BY p.created_at DESC", userID)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer purchaseRows.Close()
	purchases := make([]expertMarketAdminListing, 0)
	for purchaseRows.Next() {
		item, scanErr := scanExpertMarketListing(purchaseRows)
		if scanErr != nil {
			smError(w, http.StatusInternalServerError, scanErr.Error())
			return
		}
		purchases = append(purchases, adminExpertMarketListing(item))
	}
	credits := int64(0)
	if h.creditsSvc != nil {
		credits, _ = h.creditsSvc.GetBalance(r.Context(), userID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": map[string]string{"email": email}, "credits": credits, "uploads": uploads, "purchases": purchases})
}

func (h *SkillMarketHandlers) PurchaseExpertMarketListing(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	buyerID, buyerEmail, err := h.expertMarketUser(r)
	if err != nil {
		smError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, ok := expertMarketListingID(w, r)
	if !ok {
		return
	}
	// The Credits transaction verifies this state again, but using the primary
	// here avoids showing a freshly unlisted/private package as purchasable
	// merely because a replica has not caught up.
	item, err := scanExpertMarketListing(h.store.DB().QueryRowContext(r.Context(), "SELECT "+expertMarketListingSelectColumns+" FROM sm_expert_market_listings WHERE id=? AND visibility='public' AND status='listed'", id))
	if err != nil {
		smError(w, http.StatusNotFound, "expert listing not found")
		return
	}
	if item.OwnerID == buyerID {
		// Owners have an implicit entitlement, but the GUI uses the purchase
		// endpoint to normalize both free auto-installs and paid publisher
		// installs before downloading. Return the same idempotent owned result
		// rather than making an otherwise valid industry-default card fail.
		writeJSON(w, http.StatusOK, map[string]any{"status": "owned", "download_url": "/api/v1/expert-market/experts/" + id + "/download"})
		return
	}
	if h.creditsSvc == nil {
		smError(w, http.StatusServiceUnavailable, "credits service unavailable")
		return
	}
	err = h.creditsSvc.CompleteExpertMarketPurchase(r.Context(), buyerID, buyerEmail, item.OwnerID, id, "expert_"+uniqueID("entitlement"), "expert_"+uniqueID("purchase"), item.Price)
	if errors.Is(err, skillmarket.ErrExpertMarketAlreadyOwned) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "owned", "download_url": "/api/v1/expert-market/experts/" + id + "/download"})
		return
	}
	if errors.Is(err, skillmarket.ErrExpertMarketUnavailable) {
		smError(w, http.StatusConflict, "expert listing is no longer available")
		return
	}
	if errors.Is(err, skillmarket.ErrInsufficientCredits) {
		smError(w, http.StatusPaymentRequired, fmt.Sprintf("insufficient credits: need %d", item.Price))
		return
	}
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "owned", "amount_paid": item.Price, "download_url": "/api/v1/expert-market/experts/" + id + "/download"})
}

func (h *SkillMarketHandlers) DownloadExpertMarketListing(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	userID, _, err := h.expertMarketUser(r)
	if err != nil {
		smError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, ok := expertMarketListingID(w, r)
	if !ok {
		return
	}
	// Download authorization preserves existing entitlements after an unlist,
	// so it needs the latest listing and purchase state—not a lagging replica.
	item, err := scanExpertMarketListing(h.store.DB().QueryRowContext(r.Context(), "SELECT "+expertMarketListingSelectColumns+" FROM sm_expert_market_listings WHERE id=?", id))
	if err != nil {
		smError(w, http.StatusNotFound, "expert listing not found")
		return
	}
	// A logical delete is an archive operation. Existing purchasers retain their
	// permanent entitlement and must be able to restore the exact package they
	// bought. Only a package that was safely purged is unavailable to everyone.
	if item.Status == "purged" {
		smError(w, http.StatusGone, "expert listing has been removed")
		return
	}
	if item.Visibility == "private" && (item.Status != "private" || item.OwnerID != userID) {
		smError(w, http.StatusGone, "expert listing has been removed")
		return
	}
	if item.OwnerID != userID {
		var n int
		_ = h.store.DB().QueryRowContext(r.Context(), "SELECT COUNT(*) FROM sm_expert_market_purchases WHERE listing_id=? AND buyer_user_id=? AND status='active'", id, userID).Scan(&n)
		if n == 0 {
			smError(w, http.StatusForbidden, "purchase required")
			return
		}
	}
	packagePath, err := h.expertMarketPackagePath(item.ZipPath)
	if err != nil {
		smError(w, http.StatusGone, "expert package file is unavailable")
		return
	}
	// A unique event makes download_count a count of entitled people, not retry clicks.
	_, _ = h.store.DB().ExecContext(r.Context(), "INSERT OR IGNORE INTO sm_expert_market_downloads (id,listing_id,downloader_user_id,created_at) VALUES (?,?,?,?)", "expert_"+uniqueID("download"), id, userID, expertMarketNow())
	_, _ = h.store.DB().ExecContext(r.Context(), "UPDATE sm_expert_market_listings SET download_count=(SELECT COUNT(*) FROM sm_expert_market_downloads WHERE listing_id=?), updated_at=updated_at WHERE id=?", id, id)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(item.SourceExpertID, "\"", "")+`.zip"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, packagePath)
}

// ReportExpertMarketInstallation records the local installer outcome for an
// entitled package. It never alters Credits or entitlements: installation is
// intentionally retryable and independent from purchase settlement.
func (h *SkillMarketHandlers) ReportExpertMarketInstallation(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	userID, _, err := h.expertMarketUser(r)
	if err != nil {
		smError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, ok := expertMarketListingID(w, r)
	if !ok {
		return
	}
	var body struct {
		Status        string `json:"status"`
		LocalExpertID string `json:"local_expert_id"`
		Version       string `json:"version"`
		FailureStage  string `json:"failure_stage"`
		ErrorMessage  string `json:"error_message"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	if err := decoder.Decode(&body); err != nil {
		smError(w, http.StatusBadRequest, "invalid installation report")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		smError(w, http.StatusBadRequest, "invalid installation report")
		return
	}
	body.Status = strings.ToLower(strings.TrimSpace(body.Status))
	body.LocalExpertID = strings.TrimSpace(body.LocalExpertID)
	body.Version = strings.TrimSpace(body.Version)
	body.FailureStage = strings.TrimSpace(body.FailureStage)
	body.ErrorMessage = strings.TrimSpace(body.ErrorMessage)
	if body.Status != "installed" && body.Status != "failed" {
		smError(w, http.StatusBadRequest, "installation status must be installed or failed")
		return
	}
	if body.Status == "installed" && body.LocalExpertID == "" {
		smError(w, http.StatusBadRequest, "local expert id is required for a successful installation")
		return
	}
	if len([]rune(body.LocalExpertID)) > 128 || len([]rune(body.Version)) > 80 || len([]rune(body.FailureStage)) > 80 || len([]rune(body.ErrorMessage)) > 2048 {
		smError(w, http.StatusBadRequest, "installation report field is too long")
		return
	}
	var ownerID, visibility string
	if err := h.store.DB().QueryRowContext(r.Context(), `SELECT owner_user_id, visibility FROM sm_expert_market_listings WHERE id=?`, id).Scan(&ownerID, &visibility); err != nil {
		smError(w, http.StatusNotFound, "expert listing not found")
		return
	}
	if visibility != "public" {
		smError(w, http.StatusGone, "expert listing has been removed")
		return
	}
	if ownerID != userID {
		var owned int
		if err := h.store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sm_expert_market_purchases WHERE listing_id=? AND buyer_user_id=? AND status='active'`, id, userID).Scan(&owned); err != nil || owned == 0 {
			smError(w, http.StatusForbidden, "purchase required")
			return
		}
	}
	now := expertMarketNow()
	// Repeated installer retries should replace the previous state rather than
	// creating an unbounded telemetry row for the same user and package.
	_, err = h.store.DB().ExecContext(r.Context(), `INSERT INTO sm_expert_market_installations (id, listing_id, user_id, local_expert_id, version, status, failure_stage, error_message, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(listing_id, user_id) DO UPDATE SET local_expert_id=excluded.local_expert_id, version=excluded.version, status=excluded.status, failure_stage=excluded.failure_stage, error_message=excluded.error_message, updated_at=excluded.updated_at`, "expert_"+uniqueID("install"), id, userID, body.LocalExpertID, body.Version, body.Status, body.FailureStage, body.ErrorMessage, now, now)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": body.Status})
}

func (h *SkillMarketHandlers) WithdrawExpertMarketListing(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	userID, _, err := h.expertMarketUser(r)
	if err != nil {
		smError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, ok := expertMarketListingID(w, r)
	if !ok {
		return
	}
	tx, err := h.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(r.Context(), "UPDATE sm_expert_market_listings SET status='unlisted', updated_at=? WHERE id=? AND owner_user_id=? AND visibility='public' AND status IN ('pending_review','approved','listed','rejected')", expertMarketNow(), id, userID)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		smError(w, http.StatusNotFound, "expert listing cannot be unlisted")
		return
	}
	if err := h.recordExpertMarketEventAsTx(r, tx, id, userID, "withdrawn", "withdrawn by publisher"); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"visibility": "public", "status": "unlisted"})
}

// DeletePrivateExpertMarketListing permanently removes an owner-only share.
// Private listings are never purchasable, so unlike public unlisting there is
// no distribution history to retain. Guard the entitlement check regardless so
// a malformed legacy row cannot discard a buyer's download rights.
func (h *SkillMarketHandlers) DeletePrivateExpertMarketListing(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	userID, _, err := h.expertMarketUser(r)
	if err != nil {
		smError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, ok := expertMarketListingID(w, r)
	if !ok {
		return
	}
	tx, err := h.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	var zipPath string
	if err := tx.QueryRowContext(r.Context(), `SELECT zip_path FROM sm_expert_market_listings WHERE id=? AND owner_user_id=? AND visibility='private' AND status='private'`, id, userID).Scan(&zipPath); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			smError(w, http.StatusNotFound, "private expert listing not found")
			return
		}
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var entitlements int
	if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sm_expert_market_purchases WHERE listing_id=? AND status='active'`, id).Scan(&entitlements); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if entitlements > 0 {
		smError(w, http.StatusConflict, "cannot delete a private expert with active entitlements")
		return
	}
	// Remove dependent records first. The listing and its private activity no
	// longer appear in any account or administrative surface after deletion.
	for _, table := range []string{"sm_expert_market_events", "sm_expert_market_installations", "sm_expert_market_downloads", "sm_expert_market_purchases"} {
		if _, err := tx.ExecContext(r.Context(), "DELETE FROM "+table+" WHERE listing_id=?", id); err != nil {
			smError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	res, err := tx.ExecContext(r.Context(), `DELETE FROM sm_expert_market_listings WHERE id=? AND owner_user_id=? AND visibility='private' AND status='private'`, id, userID)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		smError(w, http.StatusNotFound, "private expert listing not found")
		return
	}
	if err := tx.Commit(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Commit database removal before deleting bytes: an orphaned file is safe,
	// while a listing that points to a removed archive is not. A failed cleanup
	// is therefore best-effort and can no longer expose the private package.
	if err := h.removeExpertMarketPackage(zipPath); err != nil {
		// The record is already inaccessible. Log the cleanup issue so it can
		// be collected without exposing the private archive or failing a user
		// operation that has otherwise completed.
		fmt.Printf("expert market: private package cleanup for %s failed: %v\n", id, err)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// MakeExpertMarketListingPrivate removes a public submission from the market
// and keeps it owner-only. Unlike unlisting, this changes its visibility.
func (h *SkillMarketHandlers) MakeExpertMarketListingPrivate(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	userID, _, err := h.expertMarketUser(r)
	if err != nil {
		smError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, ok := expertMarketListingID(w, r)
	if !ok {
		return
	}
	tx, err := h.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	// A private listing must not retain industry-wide distribution consent. This
	// also prevents a later re-publish from resurrecting consent granted for a
	// previous public version without the owner making that choice again.
	res, err := tx.ExecContext(r.Context(), "UPDATE sm_expert_market_listings SET visibility='private', status='private', platform_distribution=0, updated_at=? WHERE id=? AND owner_user_id=? AND visibility='public' AND status IN ('pending_review','approved','listed','unlisted','rejected')", expertMarketNow(), id, userID)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		smError(w, http.StatusNotFound, "expert listing cannot be made private")
		return
	}
	if err := h.recordExpertMarketEventAsTx(r, tx, id, userID, "made_private", "public listing made private"); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"visibility": "private", "status": "private"})
}

// PublishExpertMarketListing changes a private share back to public. The
// package must go through moderation again before it can reappear in search.
func (h *SkillMarketHandlers) PublishExpertMarketListing(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	userID, _, err := h.expertMarketUser(r)
	if err != nil {
		smError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id, ok := expertMarketListingID(w, r)
	if !ok {
		return
	}
	now := expertMarketNow()
	tx, err := h.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(r.Context(), "UPDATE sm_expert_market_listings SET visibility='public', status='pending_review', review_note='', updated_at=? WHERE id=? AND owner_user_id=? AND visibility='private' AND status='private'", now, id, userID)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		smError(w, http.StatusNotFound, "private expert listing cannot be submitted for review")
		return
	}
	if err := h.recordExpertMarketEventAsTx(r, tx, id, userID, "submitted", "private listing submitted for public review"); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"visibility": "public", "status": "pending_review"})
}

type expertMarketAdminUser struct {
	ID     string `json:"id"`
	Email  string `json:"email"`
	Status string `json:"status"`
}

func expertMarketLikeQuery(value string) string {
	return strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(strings.TrimSpace(value))
}

func decodeExpertMarketAdminReason(w http.ResponseWriter, r *http.Request, field, requiredMessage string) (string, bool) {
	var body struct {
		Reason string `json:"reason"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	if err := decoder.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		smError(w, http.StatusBadRequest, "invalid request body")
		return "", false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		smError(w, http.StatusBadRequest, "invalid request body")
		return "", false
	}
	reason := strings.TrimSpace(body.Reason)
	if reason == "" {
		smError(w, http.StatusBadRequest, requiredMessage)
		return "", false
	}
	if len([]rune(reason)) > 2048 {
		smError(w, http.StatusBadRequest, field+" is too long")
		return "", false
	}
	return reason, true
}

func expertMarketAdminActor(r *http.Request) string {
	if admin := AdminFromContext(r.Context()); admin != nil {
		return firstNonEmpty(strings.TrimSpace(admin.ID), strings.TrimSpace(admin.Username), strings.TrimSpace(admin.Email), "administrator")
	}
	return "administrator"
}

func expertMarketRequestID(r *http.Request) string {
	if r == nil {
		return ""
	}
	requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
	if len([]rune(requestID)) > expertMarketMaxRequestIDRunes {
		return ""
	}
	return requestID
}

func (h *SkillMarketHandlers) recordExpertMarketAuditTx(r *http.Request, tx *sql.Tx, listingID, action, reason string, before, after map[string]string) error {
	if tx == nil {
		return fmt.Errorf("missing transaction")
	}
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		return err
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO sm_expert_market_audit_events (id, listing_id, actor_admin_id, action, reason, before_json, after_json, request_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, "expert_"+uniqueID("audit"), listingID, expertMarketAdminActor(r), action, strings.TrimSpace(reason), string(beforeJSON), string(afterJSON), expertMarketRequestID(r), expertMarketNow())
	return err
}

// AdminListExpertMarketUsers provides the paginated target picker used by a
// transfer-owner dialog. The market currently owns only ID/email identity
// fields; this endpoint deliberately does not claim to expose other profile
// attributes until the central identity directory is integrated.
func (h *SkillMarketHandlers) AdminListExpertMarketUsers(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	page, ok := expertMarketPageBounds(r.URL.Query().Get("page"))
	if !ok {
		smError(w, http.StatusBadRequest, "invalid page")
		return
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 50 {
		pageSize = 20
	}
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	if len([]rune(keyword)) > 100 {
		smError(w, http.StatusBadRequest, "keyword is too long")
		return
	}
	// This endpoint exposes contact data for a transfer picker. Requiring a
	// useful search term prevents a compromised administrator token or UI bug
	// from enumerating the entire verified-user directory page by page.
	if len([]rune(keyword)) < 2 {
		smError(w, http.StatusBadRequest, "enter at least 2 characters to search users")
		return
	}
	where, args := " WHERE status='verified'", []any{}
	like := "%" + expertMarketLikeQuery(keyword) + "%"
	where += " AND (id LIKE ? ESCAPE '\\' OR email LIKE ? ESCAPE '\\')"
	args = append(args, like, like)
	var total int
	if err := h.store.DB().QueryRowContext(r.Context(), "SELECT COUNT(*) FROM sm_users"+where, args...).Scan(&total); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows, err := h.store.DB().QueryContext(r.Context(), "SELECT id, email, status FROM sm_users"+where+" ORDER BY email COLLATE NOCASE ASC, id ASC LIMIT ? OFFSET ?", append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	users := make([]expertMarketAdminUser, 0, pageSize)
	for rows.Next() {
		var user expertMarketAdminUser
		if err := rows.Scan(&user.ID, &user.Email, &user.Status); err != nil {
			smError(w, http.StatusInternalServerError, err.Error())
			return
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users, "page": page, "page_size": pageSize, "total": total, "total_pages": (total + pageSize - 1) / pageSize})
}

func (h *SkillMarketHandlers) AdminTransferExpertMarketOwner(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	var body struct {
		TargetUserID    string `json:"target_user_id"`
		ExpectedOwnerID string `json:"expected_owner_id"`
		Reason          string `json:"reason"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	if err := decoder.Decode(&body); err != nil {
		smError(w, http.StatusBadRequest, "invalid owner transfer request")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		smError(w, http.StatusBadRequest, "invalid owner transfer request")
		return
	}
	body.TargetUserID, body.ExpectedOwnerID, body.Reason = strings.TrimSpace(body.TargetUserID), strings.TrimSpace(body.ExpectedOwnerID), strings.TrimSpace(body.Reason)
	if body.TargetUserID == "" || body.ExpectedOwnerID == "" || body.Reason == "" {
		smError(w, http.StatusBadRequest, "target_user_id, expected_owner_id, and reason are required")
		return
	}
	if len([]rune(body.Reason)) > 2048 {
		smError(w, http.StatusBadRequest, "reason is too long")
		return
	}
	tx, err := h.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	var targetEmail, targetStatus string
	if err := tx.QueryRowContext(r.Context(), `SELECT email, status FROM sm_users WHERE id=?`, body.TargetUserID).Scan(&targetEmail, &targetStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			smError(w, http.StatusBadRequest, "target user not found")
			return
		}
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if targetStatus != "verified" {
		smError(w, http.StatusConflict, "target user is not active")
		return
	}
	id, ok := expertMarketListingID(w, r)
	if !ok {
		return
	}
	var ownerID, ownerEmail, visibility, status string
	if err := tx.QueryRowContext(r.Context(), `SELECT owner_user_id, owner_email, visibility, status FROM sm_expert_market_listings WHERE id=?`, id).Scan(&ownerID, &ownerEmail, &visibility, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			smError(w, http.StatusNotFound, "expert listing not found")
			return
		}
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if visibility != "private" || status != "private" {
		smError(w, http.StatusConflict, "only active private experts can be transferred")
		return
	}
	if ownerID != body.ExpectedOwnerID {
		smError(w, http.StatusConflict, "OWNER_CHANGED")
		return
	}
	if ownerID == body.TargetUserID {
		smError(w, http.StatusConflict, "OWNER_UNCHANGED")
		return
	}
	now := expertMarketNow()
	res, err := tx.ExecContext(r.Context(), `UPDATE sm_expert_market_listings SET owner_user_id=?, owner_email=?, updated_at=? WHERE id=? AND owner_user_id=? AND visibility='private' AND status='private'`, body.TargetUserID, targetEmail, now, id, body.ExpectedOwnerID)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		smError(w, http.StatusConflict, "OWNER_CHANGED")
		return
	}
	// The listing itself retains the current owner_email for presentation. Audit
	// snapshots deliberately contain only durable IDs and lifecycle state, so
	// contact data does not get copied into a long-lived operational log.
	before, after := map[string]string{"owner_user_id": ownerID, "visibility": visibility, "status": status}, map[string]string{"owner_user_id": body.TargetUserID, "visibility": visibility, "status": status}
	if err := h.recordExpertMarketAuditTx(r, tx, id, "transfer_owner", body.Reason, before, after); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"owner_id": body.TargetUserID, "owner_email": targetEmail, "visibility": visibility, "status": status})
}

// AdminSubmitExpertMarketPublication moves an owner-only expert into the
// existing review queue. It intentionally does not bypass moderation.
func (h *SkillMarketHandlers) AdminSubmitExpertMarketPublication(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	reason, ok := decodeExpertMarketAdminReason(w, r, "reason", "a publication reason is required")
	if !ok {
		return
	}
	id, ok := expertMarketListingID(w, r)
	if !ok {
		return
	}
	tx, err := h.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(r.Context(), `UPDATE sm_expert_market_listings SET visibility='public', status='pending_review', review_note='', updated_at=? WHERE id=? AND visibility='private' AND status='private'`, expertMarketNow(), id)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		smError(w, http.StatusConflict, "only active private experts can be submitted for publication")
		return
	}
	before, after := map[string]string{"visibility": "private", "status": "private"}, map[string]string{"visibility": "public", "status": "pending_review"}
	if err := h.recordExpertMarketAuditTx(r, tx, id, "submit_publication", reason, before, after); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, after)
}

func (h *SkillMarketHandlers) AdminDeletePrivateExpertMarketListing(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	reason, ok := decodeExpertMarketAdminReason(w, r, "reason", "a deletion reason is required")
	if !ok {
		return
	}
	id, ok := expertMarketListingID(w, r)
	if !ok {
		return
	}
	tx, err := h.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	var visibility, status string
	if err := tx.QueryRowContext(r.Context(), `SELECT visibility, status FROM sm_expert_market_listings WHERE id=?`, id).Scan(&visibility, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			smError(w, http.StatusNotFound, "expert listing not found")
			return
		}
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if visibility != "private" {
		smError(w, http.StatusConflict, "only private experts can be deleted here")
		return
	}
	if status == "deleted" {
		if err := tx.Commit(); err != nil {
			smError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"visibility": "private", "status": "deleted"})
		return
	}
	if status != "private" {
		smError(w, http.StatusConflict, "only active private experts can be deleted")
		return
	}
	res, err := tx.ExecContext(r.Context(), `UPDATE sm_expert_market_listings SET status='deleted', updated_at=? WHERE id=? AND visibility='private' AND status='private'`, expertMarketNow(), id)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		smError(w, http.StatusConflict, "private expert changed before deletion")
		return
	}
	before, after := map[string]string{"visibility": "private", "status": "private"}, map[string]string{"visibility": "private", "status": "deleted"}
	if err := h.recordExpertMarketAuditTx(r, tx, id, "delete_private", reason, before, after); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, after)
}

// AdminPurgePrivateExpertMarketListing is the explicitly destructive follow-up
// to an administrator's private delete. It preserves the listing tombstone and
// audit records, but removes the stored package only when no entitlement exists.
func (h *SkillMarketHandlers) AdminPurgePrivateExpertMarketListing(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	reason, ok := decodeExpertMarketAdminReason(w, r, "reason", "a purge reason is required")
	if !ok {
		return
	}
	id, ok := expertMarketListingID(w, r)
	if !ok {
		return
	}
	tx, err := h.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	var path, visibility, status string
	if err := tx.QueryRowContext(r.Context(), `SELECT zip_path, visibility, status FROM sm_expert_market_listings WHERE id=?`, id).Scan(&path, &visibility, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			smError(w, http.StatusNotFound, "expert listing not found")
			return
		}
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if visibility != "private" || status != "deleted" {
		smError(w, http.StatusConflict, "only deleted private experts can be permanently deleted")
		return
	}
	var activeEntitlements int
	if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sm_expert_market_purchases WHERE listing_id=? AND status='active'`, id).Scan(&activeEntitlements); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if activeEntitlements > 0 {
		smError(w, http.StatusConflict, "cannot permanently delete an expert with active entitlements")
		return
	}
	res, err := tx.ExecContext(r.Context(), `UPDATE sm_expert_market_listings SET status='purged', zip_path='', package_size=0, updated_at=? WHERE id=? AND visibility='private' AND status='deleted'`, expertMarketNow(), id)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		smError(w, http.StatusConflict, "only deleted private experts can be permanently deleted")
		return
	}
	before, after := map[string]string{"visibility": "private", "status": "deleted"}, map[string]string{"visibility": "private", "status": "purged"}
	if err := h.recordExpertMarketAuditTx(r, tx, id, "purge_private", reason, before, after); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Validate and write the terminal database state before deleting bytes. If
	// either database operation fails, rollback leaves the deleted listing and
	// its package intact so an administrator can safely retry the purge.
	if err := h.removeExpertMarketPackage(path); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, after)
}

func (h *SkillMarketHandlers) AdminListExpertMarketListings(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	page, ok := expertMarketPageBounds(r.URL.Query().Get("page"))
	if !ok {
		smError(w, http.StatusBadRequest, "invalid page")
		return
	}
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if status != "" && !expertMarketValidStatus(status) {
		smError(w, http.StatusBadRequest, "invalid expert market status")
		return
	}
	visibility := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("visibility")))
	if visibility != "" && visibility != "public" && visibility != "private" {
		smError(w, http.StatusBadRequest, "invalid expert market visibility")
		return
	}
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	if len([]rune(keyword)) > 100 {
		smError(w, http.StatusBadRequest, "keyword is too long")
		return
	}
	whereParts, args := make([]string, 0, 3), []any{}
	if status != "" {
		whereParts = append(whereParts, "status=?")
		args = append(args, status)
	}
	if visibility != "" {
		whereParts = append(whereParts, "visibility=?")
		args = append(args, visibility)
	}
	if keyword != "" {
		like := "%" + expertMarketLikeQuery(keyword) + "%"
		whereParts = append(whereParts, "(id LIKE ? ESCAPE '\\' OR source_expert_id LIKE ? ESCAPE '\\' OR name LIKE ? ESCAPE '\\' OR description LIKE ? ESCAPE '\\' OR owner_user_id LIKE ? ESCAPE '\\' OR owner_email LIKE ? ESCAPE '\\')")
		args = append(args, like, like, like, like, like, like)
	}
	where := ""
	if len(whereParts) > 0 {
		where = " WHERE " + strings.Join(whereParts, " AND ")
	}
	var total int
	if err := h.store.DB().QueryRowContext(r.Context(), "SELECT COUNT(*) FROM sm_expert_market_listings"+where, args...).Scan(&total); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows, err := h.store.DB().QueryContext(r.Context(), "SELECT "+expertMarketListingSelectColumns+" FROM sm_expert_market_listings"+where+" ORDER BY updated_at DESC,id ASC LIMIT 20 OFFSET ?", append(args, (page-1)*20)...)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	items := make([]expertMarketAdminListing, 0)
	for rows.Next() {
		item, e := scanExpertMarketListing(rows)
		if e != nil {
			smError(w, http.StatusInternalServerError, e.Error())
			return
		}
		items = append(items, adminExpertMarketListing(item))
	}
	if err := rows.Err(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"experts": items, "page": page, "page_size": 20, "total": total, "total_pages": (total + 19) / 20})
}

func (h *SkillMarketHandlers) adminSetExpertMarketStatus(w http.ResponseWriter, r *http.Request, from, to, eventAction string) {
	h.ensureExpertMarketSchema()
	var body struct {
		Reason string `json:"reason"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	if err := decoder.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		smError(w, http.StatusBadRequest, "invalid moderation request")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		smError(w, http.StatusBadRequest, "invalid moderation request")
		return
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if len([]rune(body.Reason)) > 2048 {
		smError(w, http.StatusBadRequest, "reason is too long")
		return
	}
	listingID, ok := expertMarketListingID(w, r)
	if !ok {
		return
	}
	tx, err := h.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	now := expertMarketNow()
	// Moderation may only publish a public review submission. Keep this guard
	// close to the state transition so a malformed legacy row cannot turn an
	// owner-only share into a market listing.
	res, err := tx.ExecContext(r.Context(), "UPDATE sm_expert_market_listings SET status=?, review_note=?, updated_at=? WHERE id=? AND visibility='public' AND status=?", to, body.Reason, now, listingID, from)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		smError(w, http.StatusConflict, "expert listing is not in the expected status")
		return
	}
	actor := "administrator"
	if admin := AdminFromContext(r.Context()); admin != nil {
		actor = firstNonEmpty(admin.Username, admin.Email, actor)
	}
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO sm_expert_market_events (id, listing_id, actor, action, reason, created_at) VALUES (?, ?, ?, ?, ?, ?)`, "expert_"+uniqueID("event"), listingID, actor, eventAction, body.Reason, now); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.recordExpertMarketAuditTx(r, tx, listingID, eventAction, body.Reason, map[string]string{"visibility": "public", "status": from}, map[string]string{"visibility": "public", "status": to}); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": to})
}
func (h *SkillMarketHandlers) AdminApproveExpertMarketListing(w http.ResponseWriter, r *http.Request) {
	// Approval publishes the listing immediately. Requiring a second "list"
	// operation makes routine moderation needlessly error-prone.
	h.adminSetExpertMarketStatus(w, r, "pending_review", "listed", "approved")
}
func (h *SkillMarketHandlers) AdminRejectExpertMarketListing(w http.ResponseWriter, r *http.Request) {
	h.adminSetExpertMarketStatus(w, r, "pending_review", "rejected", "rejected")
}
func (h *SkillMarketHandlers) AdminUnlistExpertMarketListing(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	reason, ok := decodeExpertMarketAdminReason(w, r, "reason", "a moderation reason is required")
	if !ok {
		return
	}
	// Keep the listing transition and its moderation audit record atomic. A
	// successful state change without an event makes later review impossible.
	id, ok := expertMarketListingID(w, r)
	if !ok {
		return
	}
	tx, err := h.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	now := expertMarketNow()
	// A private share has no moderation lifecycle in the public market. Keep
	// administrator actions scoped to public listings so a malformed legacy row
	// cannot turn an owner-only expert into an unlisted/deleted market record.
	res, err := tx.ExecContext(r.Context(), "UPDATE sm_expert_market_listings SET status='unlisted', review_note=?, updated_at=? WHERE id=? AND visibility='public' AND status='listed'", reason, now, id)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		smError(w, http.StatusConflict, "listed expert not found")
		return
	}
	if err := h.recordExpertMarketEventTx(r, tx, id, "unlisted", reason); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.recordExpertMarketAuditTx(r, tx, id, "unlisted", reason, map[string]string{"visibility": "public", "status": "listed"}, map[string]string{"visibility": "public", "status": "unlisted"}); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "unlisted"})
}
func (h *SkillMarketHandlers) AdminDeleteExpertMarketListing(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	reason, ok := decodeExpertMarketAdminReason(w, r, "reason", "a moderation reason is required")
	if !ok {
		return
	}
	id, ok := expertMarketListingID(w, r)
	if !ok {
		return
	}
	tx, err := h.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	now := expertMarketNow()
	res, err := tx.ExecContext(r.Context(), "UPDATE sm_expert_market_listings SET status='deleted', review_note=?, updated_at=? WHERE id=? AND visibility='public' AND status='unlisted'", reason, now, id)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		smError(w, http.StatusConflict, "only unlisted expert listings can be deleted")
		return
	}
	if err := h.recordExpertMarketEventTx(r, tx, id, "deleted", reason); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.recordExpertMarketAuditTx(r, tx, id, "deleted", reason, map[string]string{"visibility": "public", "status": "unlisted"}, map[string]string{"visibility": "public", "status": "deleted"}); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// AdminPurgeExpertMarketListing is the explicit irreversible second step after
// logical deletion. It preserves a terminal database tombstone for order and
// moderation audit, while removing the package bytes from disk.
func (h *SkillMarketHandlers) AdminPurgeExpertMarketListing(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	id, ok := expertMarketListingID(w, r)
	if !ok {
		return
	}
	reason, ok := decodeExpertMarketAdminReason(w, r, "reason", "a moderation reason is required")
	if !ok {
		return
	}
	tx, err := h.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	var path, status, visibility string
	if err := tx.QueryRowContext(r.Context(), `SELECT zip_path, status, visibility FROM sm_expert_market_listings WHERE id=?`, id).Scan(&path, &status, &visibility); err != nil {
		smError(w, http.StatusNotFound, "expert listing not found")
		return
	}
	if visibility != "public" || status != "deleted" {
		smError(w, http.StatusConflict, "only deleted expert listings can be permanently deleted")
		return
	}
	var activeEntitlements int
	if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sm_expert_market_purchases WHERE listing_id=? AND status='active'`, id).Scan(&activeEntitlements); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if activeEntitlements > 0 {
		smError(w, http.StatusConflict, "cannot permanently delete an expert with active entitlements")
		return
	}
	res, err := tx.ExecContext(r.Context(), `UPDATE sm_expert_market_listings SET status='purged', zip_path='', package_size=0, updated_at=? WHERE id=? AND visibility='public' AND status='deleted'`, expertMarketNow(), id)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		smError(w, http.StatusConflict, "only deleted expert listings can be permanently deleted")
		return
	}
	if err := h.recordExpertMarketEventTx(r, tx, id, "purged", reason); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.recordExpertMarketAuditTx(r, tx, id, "purged", reason, map[string]string{"visibility": "public", "status": "deleted"}, map[string]string{"visibility": "public", "status": "purged"}); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Keep audit insertion and tombstoning atomic before deleting package bytes.
	// If either database write fails, the package remains available for a safe
	// retry instead of leaving an undeclared, partially purged archive.
	if err := h.removeExpertMarketPackage(path); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "purged"})
}

func (h *SkillMarketHandlers) AdminListExpertMarketEvents(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	id, ok := expertMarketListingID(w, r)
	if !ok {
		return
	}
	page, ok := expertMarketPageBounds(r.URL.Query().Get("page"))
	if !ok {
		smError(w, http.StatusBadRequest, "invalid page")
		return
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	var listingCount int
	if err := h.store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sm_expert_market_listings WHERE id=?`, id).Scan(&listingCount); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if listingCount == 0 {
		smError(w, http.StatusNotFound, "expert listing not found")
		return
	}
	rows, err := h.store.DB().QueryContext(r.Context(), `
		SELECT id, actor, action, reason, created_at, source, before_json, after_json, request_id
		FROM (
			SELECT id, actor, action, reason, created_at, 'legacy' AS source, '' AS before_json, '' AS after_json, '' AS request_id
			FROM sm_expert_market_events WHERE listing_id=?
			UNION ALL
			SELECT id, actor_admin_id, action, reason, created_at, 'structured' AS source, before_json, after_json, request_id
			FROM sm_expert_market_audit_events WHERE listing_id=?
		)
		ORDER BY julianday(created_at) DESC, created_at DESC, id DESC
		LIMIT ? OFFSET ?`, id, id, pageSize+1, (page-1)*pageSize)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	events := make([]map[string]string, 0, pageSize+1)
	for rows.Next() {
		var eventID, actor, action, reason, createdAt, source, beforeJSON, afterJSON, requestID string
		if err := rows.Scan(&eventID, &actor, &action, &reason, &createdAt, &source, &beforeJSON, &afterJSON, &requestID); err != nil {
			smError(w, http.StatusInternalServerError, err.Error())
			return
		}
		event := map[string]string{"id": eventID, "actor": actor, "action": action, "reason": reason, "created_at": createdAt, "source": source}
		if source == "structured" {
			event["before_json"] = beforeJSON
			event["after_json"] = afterJSON
			event["request_id"] = requestID
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	hasMore := len(events) > pageSize
	if hasMore {
		events = events[:pageSize]
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "page": page, "page_size": pageSize, "has_more": hasMore})
}

func (h *SkillMarketHandlers) recordExpertMarketEvent(r *http.Request, listingID, action, reason string) {
	if h == nil || h.store == nil || r == nil || strings.TrimSpace(listingID) == "" {
		return
	}
	actor := "administrator"
	if admin := AdminFromContext(r.Context()); admin != nil {
		actor = firstNonEmpty(admin.Username, admin.Email, actor)
	}
	h.recordExpertMarketEventAs(r, listingID, actor, action, reason)
}

func (h *SkillMarketHandlers) recordExpertMarketEventAs(r *http.Request, listingID, actor, action, reason string) {
	if h == nil || h.store == nil || r == nil || strings.TrimSpace(listingID) == "" {
		return
	}
	actor = firstNonEmpty(strings.TrimSpace(actor), "system")
	_, _ = h.store.DB().ExecContext(r.Context(), `INSERT INTO sm_expert_market_events (id, listing_id, actor, action, reason, created_at) VALUES (?, ?, ?, ?, ?, ?)`, "expert_"+uniqueID("event"), listingID, actor, action, strings.TrimSpace(reason), expertMarketNow())
}

func (h *SkillMarketHandlers) recordExpertMarketEventAsTx(r *http.Request, tx *sql.Tx, listingID, actor, action, reason string) error {
	if h == nil || h.store == nil || r == nil || tx == nil || strings.TrimSpace(listingID) == "" {
		return fmt.Errorf("invalid expert market event")
	}
	_, err := tx.ExecContext(r.Context(), `INSERT INTO sm_expert_market_events (id, listing_id, actor, action, reason, created_at) VALUES (?, ?, ?, ?, ?, ?)`, "expert_"+uniqueID("event"), listingID, firstNonEmpty(strings.TrimSpace(actor), "system"), action, strings.TrimSpace(reason), expertMarketNow())
	return err
}

func (h *SkillMarketHandlers) recordExpertMarketEventTx(r *http.Request, tx *sql.Tx, listingID, action, reason string) error {
	if h == nil || h.store == nil || r == nil || tx == nil || strings.TrimSpace(listingID) == "" {
		return fmt.Errorf("invalid expert market event")
	}
	actor := "administrator"
	if admin := AdminFromContext(r.Context()); admin != nil {
		actor = firstNonEmpty(admin.Username, admin.Email, actor)
	}
	_, err := tx.ExecContext(r.Context(), `INSERT INTO sm_expert_market_events (id, listing_id, actor, action, reason, created_at) VALUES (?, ?, ?, ?, ?, ?)`, "expert_"+uniqueID("event"), listingID, actor, action, strings.TrimSpace(reason), expertMarketNow())
	return err
}
