package httpapi

// AI Expert Market is deliberately separate from the capability marketplace.
// An expert package is a portable, declarative configuration archive; it is
// not an executable Skill. The market stores an immutable archive per listing,
// grants permanent download entitlements, and reuses the existing Credits
// ledger for payment.

import (
	"archive/zip"
	"bytes"
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
)

var (
	expertMarketSchemaByDB sync.Map
	expertMarketIDPattern  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
)

type expertMarketListing struct {
	ID             string `json:"id"`
	OwnerID        string `json:"-"`
	OwnerEmail     string `json:"-"`
	SourceExpertID string `json:"source_expert_id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Icon           string `json:"icon"`
	Version        string `json:"version"`
	Price          int64  `json:"price"`
	Status         string `json:"status"`
	ZipPath        string `json:"-"`
	PackageSize    int64  `json:"package_size"`
	DownloadCount  int64  `json:"download_count"`
	PurchaseCount  int64  `json:"purchase_count"`
	SalesAmount    int64  `json:"sales_amount"`
	ReviewNote     string `json:"review_note,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type expertMarketPublicListing struct {
	ID             string `json:"id"`
	SourceExpertID string `json:"source_expert_id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Icon           string `json:"icon"`
	Version        string `json:"version"`
	Price          int64  `json:"price"`
	PackageSize    int64  `json:"package_size"`
	DownloadCount  int64  `json:"download_count"`
	PurchaseCount  int64  `json:"purchase_count"`
	Owned          bool   `json:"owned"`
	CreatedAt      string `json:"created_at"`
}

type expertMarketAdminListing struct {
	expertMarketPublicListing
	Status      string `json:"status"`
	OwnerEmail  string `json:"owner_email"`
	SalesAmount int64  `json:"sales_amount"`
	ReviewNote  string `json:"review_note,omitempty"`
	UpdatedAt   string `json:"updated_at"`
}

const expertMarketListingColumns = "id, owner_user_id, owner_email, source_expert_id, name, description, icon, version, price, status, zip_path, package_size, download_count, purchase_count, sales_amount, review_note, created_at, updated_at"

func qualifiedExpertMarketListingColumns(alias string) string {
	if strings.TrimSpace(alias) == "" {
		return expertMarketListingColumns
	}
	columns := strings.Split(expertMarketListingColumns, ", ")
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
			version TEXT NOT NULL DEFAULT '1', price INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'pending_review',
			zip_path TEXT NOT NULL, package_size INTEGER NOT NULL DEFAULT 0, download_count INTEGER NOT NULL DEFAULT 0,
			purchase_count INTEGER NOT NULL DEFAULT 0, sales_amount INTEGER NOT NULL DEFAULT 0, review_note TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`)
		// The stable package identity is global. Without this guard a buyer could
		// download an expert package and re-submit it as their own listing.
		_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_sm_expert_market_source_global ON sm_expert_market_listings(source_expert_id)`)
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sm_expert_market_status_updated ON sm_expert_market_listings(status, updated_at DESC)`)
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
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS sm_expert_market_installations (
			id TEXT PRIMARY KEY, listing_id TEXT NOT NULL, user_id TEXT NOT NULL,
			local_expert_id TEXT NOT NULL DEFAULT '', version TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL, failure_stage TEXT NOT NULL DEFAULT '', error_message TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`)
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sm_expert_market_installations_listing ON sm_expert_market_installations(listing_id, created_at DESC)`)
	})
}

func (h *SkillMarketHandlers) expertMarketDir() string {
	return filepath.Join(h.dataDir, "expert-market")
}

func expertMarketNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func scanExpertMarketListing(row interface{ Scan(...any) error }) (*expertMarketListing, error) {
	var item expertMarketListing
	err := row.Scan(&item.ID, &item.OwnerID, &item.OwnerEmail, &item.SourceExpertID, &item.Name, &item.Description, &item.Icon, &item.Version, &item.Price, &item.Status, &item.ZipPath, &item.PackageSize, &item.DownloadCount, &item.PurchaseCount, &item.SalesAmount, &item.ReviewNote, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func publicExpertMarketListing(item *expertMarketListing) expertMarketPublicListing {
	return expertMarketPublicListing{ID: item.ID, SourceExpertID: item.SourceExpertID, Name: item.Name, Description: item.Description, Icon: item.Icon, Version: item.Version, Price: item.Price, PackageSize: item.PackageSize, DownloadCount: item.DownloadCount, PurchaseCount: item.PurchaseCount, CreatedAt: item.CreatedAt}
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
	rows, err := h.store.ReadDB().QueryContext(r.Context(), query, args...)
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
	return expertMarketAdminListing{expertMarketPublicListing: publicExpertMarketListing(item), Status: item.Status, OwnerEmail: item.OwnerEmail, SalesAmount: item.SalesAmount, ReviewNote: item.ReviewNote, UpdatedAt: item.UpdatedAt}
}

func (h *SkillMarketHandlers) expertMarketUser(r *http.Request) (string, string, error) {
	// Expert Market has identical session/account semantics to Pet Store. Keep a
	// separate named helper so its future policy can diverge without leaking the
	// Pet Store domain into this API.
	return h.petStoreUser(r)
}

func expertMarketValidStatus(status string) bool {
	switch status {
	case "pending_review", "listed", "unlisted", "rejected", "deleted", "purged":
		return true
	default:
		return false
	}
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
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
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
	where, args := " WHERE status='listed' AND (name LIKE ? ESCAPE '\\' OR description LIKE ? ESCAPE '\\')", []any{"%" + escaped + "%", "%" + escaped + "%"}
	var total int
	// The admin review screen reloads immediately after a moderation decision.
	// Read from the primary here so that an approved/rejected listing is visible
	// in that reload even when a replica or read-only connection is behind.
	if err := h.store.DB().QueryRowContext(r.Context(), "SELECT COUNT(*) FROM sm_expert_market_listings"+where, args...).Scan(&total); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows, err := h.store.ReadDB().QueryContext(r.Context(), "SELECT "+expertMarketListingColumns+" FROM sm_expert_market_listings"+where+" ORDER BY "+sortColumn+" DESC, id ASC LIMIT ? OFFSET ?", append(args, pageSize, (page-1)*pageSize)...)
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
	item, err := scanExpertMarketListing(h.store.ReadDB().QueryRowContext(r.Context(), "SELECT "+expertMarketListingColumns+" FROM sm_expert_market_listings WHERE id=? AND status='listed'", r.PathValue("id")))
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
	item := &expertMarketListing{ID: id, OwnerID: userID, OwnerEmail: email, SourceExpertID: sourceID, Name: name, Description: description, Icon: icon, Version: version, Price: price, Status: "pending_review", ZipPath: path, PackageSize: int64(len(data)), CreatedAt: now, UpdatedAt: now}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), "INSERT INTO sm_expert_market_listings ("+expertMarketListingColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0, 0, '', ?, ?)", item.ID, item.OwnerID, item.OwnerEmail, item.SourceExpertID, item.Name, item.Description, item.Icon, item.Version, item.Price, item.Status, item.ZipPath, item.PackageSize, item.CreatedAt, item.UpdatedAt)
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
	h.recordExpertMarketEventAs(r, id, email, "submitted", "submitted for review")
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
	rows, err := h.store.DB().QueryContext(r.Context(), "SELECT "+expertMarketListingColumns+" FROM sm_expert_market_listings WHERE owner_user_id=? ORDER BY updated_at DESC", userID)
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
	purchaseRows, err := h.store.DB().QueryContext(r.Context(), "SELECT "+qualifiedExpertMarketListingColumns("l")+" FROM sm_expert_market_purchases p JOIN sm_expert_market_listings l ON l.id=p.listing_id WHERE p.buyer_user_id=? AND p.status='active' ORDER BY p.created_at DESC", userID)
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
	id := r.PathValue("id")
	item, err := scanExpertMarketListing(h.store.ReadDB().QueryRowContext(r.Context(), "SELECT "+expertMarketListingColumns+" FROM sm_expert_market_listings WHERE id=? AND status='listed'", id))
	if err != nil {
		smError(w, http.StatusNotFound, "expert listing not found")
		return
	}
	if item.OwnerID == buyerID {
		smError(w, http.StatusBadRequest, "you own this expert listing")
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
	id := r.PathValue("id")
	item, err := scanExpertMarketListing(h.store.ReadDB().QueryRowContext(r.Context(), "SELECT "+expertMarketListingColumns+" FROM sm_expert_market_listings WHERE id=?", id))
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
	if item.OwnerID != userID {
		var n int
		_ = h.store.ReadDB().QueryRowContext(r.Context(), "SELECT COUNT(*) FROM sm_expert_market_purchases WHERE listing_id=? AND buyer_user_id=? AND status='active'", id, userID).Scan(&n)
		if n == 0 {
			smError(w, http.StatusForbidden, "purchase required")
			return
		}
	}
	if _, err := os.Stat(item.ZipPath); err != nil {
		smError(w, http.StatusGone, "expert package file is unavailable")
		return
	}
	// A unique event makes download_count a count of entitled people, not retry clicks.
	_, _ = h.store.DB().ExecContext(r.Context(), "INSERT OR IGNORE INTO sm_expert_market_downloads (id,listing_id,downloader_user_id,created_at) VALUES (?,?,?,?)", "expert_"+uniqueID("download"), id, userID, expertMarketNow())
	_, _ = h.store.DB().ExecContext(r.Context(), "UPDATE sm_expert_market_listings SET download_count=(SELECT COUNT(*) FROM sm_expert_market_downloads WHERE listing_id=?), updated_at=updated_at WHERE id=?", id, id)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(item.SourceExpertID, "\"", "")+`.zip"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, item.ZipPath)
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
	id := strings.TrimSpace(r.PathValue("id"))
	var body struct {
		Status        string `json:"status"`
		LocalExpertID string `json:"local_expert_id"`
		Version       string `json:"version"`
		FailureStage  string `json:"failure_stage"`
		ErrorMessage  string `json:"error_message"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body); err != nil {
		smError(w, http.StatusBadRequest, "invalid installation report")
		return
	}
	body.Status = strings.ToLower(strings.TrimSpace(body.Status))
	if body.Status != "installed" && body.Status != "failed" {
		smError(w, http.StatusBadRequest, "installation status must be installed or failed")
		return
	}
	if body.Status == "installed" && strings.TrimSpace(body.LocalExpertID) == "" {
		smError(w, http.StatusBadRequest, "local expert id is required for a successful installation")
		return
	}
	var ownerID string
	if err := h.store.ReadDB().QueryRowContext(r.Context(), `SELECT owner_user_id FROM sm_expert_market_listings WHERE id=?`, id).Scan(&ownerID); err != nil {
		smError(w, http.StatusNotFound, "expert listing not found")
		return
	}
	if ownerID != userID {
		var owned int
		if err := h.store.ReadDB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sm_expert_market_purchases WHERE listing_id=? AND buyer_user_id=? AND status='active'`, id, userID).Scan(&owned); err != nil || owned == 0 {
			smError(w, http.StatusForbidden, "purchase required")
			return
		}
	}
	now := expertMarketNow()
	_, err = h.store.DB().ExecContext(r.Context(), `INSERT INTO sm_expert_market_installations (id, listing_id, user_id, local_expert_id, version, status, failure_stage, error_message, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "expert_"+uniqueID("install"), id, userID, strings.TrimSpace(body.LocalExpertID), strings.TrimSpace(body.Version), body.Status, strings.TrimSpace(body.FailureStage), strings.TrimSpace(body.ErrorMessage), now, now)
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
	res, err := h.store.DB().ExecContext(r.Context(), "UPDATE sm_expert_market_listings SET status='unlisted', updated_at=? WHERE id=? AND owner_user_id=? AND status IN ('pending_review','approved','listed','rejected')", expertMarketNow(), r.PathValue("id"), userID)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		smError(w, http.StatusNotFound, "expert listing cannot be withdrawn")
		return
	}
	h.recordExpertMarketEventAs(r, r.PathValue("id"), userID, "withdrawn", "withdrawn by publisher")
	writeJSON(w, http.StatusOK, map[string]string{"status": "unlisted"})
}

func (h *SkillMarketHandlers) AdminListExpertMarketListings(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if status != "" && !expertMarketValidStatus(status) {
		smError(w, http.StatusBadRequest, "invalid expert market status")
		return
	}
	where, args := "", []any{}
	if status != "" {
		where = " WHERE status=?"
		args = []any{status}
	}
	var total int
	if err := h.store.DB().QueryRowContext(r.Context(), "SELECT COUNT(*) FROM sm_expert_market_listings"+where, args...).Scan(&total); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows, err := h.store.DB().QueryContext(r.Context(), "SELECT "+expertMarketListingColumns+" FROM sm_expert_market_listings"+where+" ORDER BY updated_at DESC,id ASC LIMIT 20 OFFSET ?", append(args, (page-1)*20)...)
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
	_ = json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body)
	body.Reason = strings.TrimSpace(body.Reason)
	if body.Reason == "" {
		smError(w, http.StatusBadRequest, "a moderation reason is required")
		return
	}
	res, err := h.store.DB().ExecContext(r.Context(), "UPDATE sm_expert_market_listings SET status=?, review_note=?, updated_at=? WHERE id=? AND status=?", to, body.Reason, expertMarketNow(), r.PathValue("id"), from)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		smError(w, http.StatusConflict, "expert listing is not in the expected status")
		return
	}
	h.recordExpertMarketEvent(r, r.PathValue("id"), eventAction, body.Reason)
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
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body)
	body.Reason = strings.TrimSpace(body.Reason)
	if body.Reason == "" {
		smError(w, http.StatusBadRequest, "a moderation reason is required")
		return
	}
	res, err := h.store.DB().ExecContext(r.Context(), "UPDATE sm_expert_market_listings SET status='unlisted', review_note=?, updated_at=? WHERE id=? AND status='listed'", body.Reason, expertMarketNow(), r.PathValue("id"))
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		smError(w, http.StatusConflict, "listed expert not found")
		return
	}
	h.recordExpertMarketEvent(r, r.PathValue("id"), "unlisted", body.Reason)
	writeJSON(w, http.StatusOK, map[string]string{"status": "unlisted"})
}
func (h *SkillMarketHandlers) AdminDeleteExpertMarketListing(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body)
	body.Reason = strings.TrimSpace(body.Reason)
	if body.Reason == "" {
		smError(w, http.StatusBadRequest, "a moderation reason is required")
		return
	}
	res, err := h.store.DB().ExecContext(r.Context(), "UPDATE sm_expert_market_listings SET status='deleted', review_note=?, updated_at=? WHERE id=? AND status='unlisted'", body.Reason, expertMarketNow(), r.PathValue("id"))
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		smError(w, http.StatusConflict, "only unlisted expert listings can be deleted")
		return
	}
	h.recordExpertMarketEvent(r, r.PathValue("id"), "deleted", body.Reason)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// AdminPurgeExpertMarketListing is the explicit irreversible second step after
// logical deletion. It preserves a terminal database tombstone for order and
// moderation audit, while removing the package bytes from disk.
func (h *SkillMarketHandlers) AdminPurgeExpertMarketListing(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		smError(w, http.StatusNotFound, "expert listing not found")
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body)
	body.Reason = strings.TrimSpace(body.Reason)
	if body.Reason == "" {
		smError(w, http.StatusBadRequest, "a moderation reason is required")
		return
	}
	var path, status string
	if err := h.store.DB().QueryRowContext(r.Context(), `SELECT zip_path, status FROM sm_expert_market_listings WHERE id=?`, id).Scan(&path, &status); err != nil {
		smError(w, http.StatusNotFound, "expert listing not found")
		return
	}
	if status != "deleted" {
		smError(w, http.StatusConflict, "only deleted expert listings can be permanently deleted")
		return
	}
	var activeEntitlements int
	if err := h.store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sm_expert_market_purchases WHERE listing_id=? AND status='active'`, id).Scan(&activeEntitlements); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if activeEntitlements > 0 {
		smError(w, http.StatusConflict, "cannot permanently delete an expert with active entitlements")
		return
	}
	if path = strings.TrimSpace(path); path != "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			smError(w, http.StatusInternalServerError, fmt.Sprintf("remove expert package: %v", err))
			return
		}
	}
	res, err := h.store.DB().ExecContext(r.Context(), `UPDATE sm_expert_market_listings SET status='purged', zip_path='', package_size=0, updated_at=? WHERE id=? AND status='deleted'`, expertMarketNow(), id)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		smError(w, http.StatusConflict, "only deleted expert listings can be permanently deleted")
		return
	}
	h.recordExpertMarketEvent(r, id, "purged", body.Reason)
	writeJSON(w, http.StatusOK, map[string]string{"status": "purged"})
}

func (h *SkillMarketHandlers) AdminListExpertMarketEvents(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		smError(w, http.StatusNotFound, "expert listing not found")
		return
	}
	rows, err := h.store.DB().QueryContext(r.Context(), `SELECT id, actor, action, reason, created_at FROM sm_expert_market_events WHERE listing_id=? ORDER BY created_at DESC, id DESC`, id)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	events := make([]map[string]string, 0)
	for rows.Next() {
		var eventID, actor, action, reason, createdAt string
		if err := rows.Scan(&eventID, &actor, &action, &reason, &createdAt); err != nil {
			smError(w, http.StatusInternalServerError, err.Error())
			return
		}
		events = append(events, map[string]string{"id": eventID, "actor": actor, "action": action, "reason": reason, "created_at": createdAt})
	}
	if err := rows.Err(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
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
