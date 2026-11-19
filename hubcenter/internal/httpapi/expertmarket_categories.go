package httpapi

// Expert Market categories are a deliberately FINITE, administrator-curated
// taxonomy. The desktop AI expert library renders them as top-level filter
// chips (全部 / each name / 其它) and a share must pick one whenever the list
// is non-empty. Rows are replicated to HA peers as one full-list snapshot, so
// a desktop probing any node sees the same chips and every node validates a
// submission against the same taxonomy.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"slices"
	"strings"
)

// expertMarketCategoryMaxRunes keeps chips one line long on every surface
// that renders them (desktop chips row, admin card, market dialog badge).
const expertMarketCategoryMaxRunes = 24

// expertMarketCategoryMaxCount bounds the replicated taxonomy. The desktop
// renders every name as a chip; a taxonomy beyond one wrap row is a misuse.
const expertMarketCategoryMaxCount = 64

// expertMarketCategorySeedNames is the out-of-the-box taxonomy. It exists so
// the feature is immediately visible before an administrator edits it; the
// seed marker below keeps a restart from resurrecting categories an
// administrator later deleted.
var expertMarketCategorySeedNames = []string{"科研", "办公", "其它"}

type expertMarketCategory struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
	Listings  int64  `json:"listings,omitempty"`
}

func (h *SkillMarketHandlers) ensureExpertMarketCategorySchema(db *sql.DB) {
	if db == nil {
		return
	}
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS sm_expert_market_categories (
		id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, sort_order INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL
	)`)
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS sm_expert_market_meta (
		key TEXT PRIMARY KEY, value TEXT NOT NULL
	)`)
	var seeded string
	_ = db.QueryRow(`SELECT value FROM sm_expert_market_meta WHERE key='expert_categories_seeded'`).Scan(&seeded)
	if seeded == "1" {
		return
	}
	now := expertMarketNow()
	for i, name := range expertMarketCategorySeedNames {
		_, _ = db.Exec(`INSERT OR IGNORE INTO sm_expert_market_categories (id, name, sort_order, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			"expert_cat_"+uniqueID("seed"), name, i+1, now, now)
	}
	_, _ = db.Exec(`INSERT INTO sm_expert_market_meta (key, value) VALUES ('expert_categories_seeded', '1') ON CONFLICT(key) DO UPDATE SET value=excluded.value`)
}

func (h *SkillMarketHandlers) ensureExpertMarketCategories() {
	h.ensureExpertMarketSchema()
}

// expertMarketCategoryRows returns the ordered taxonomy. sort_order wins, and
// creation time breaks ties so a fresh category never jumps ahead of an
// equally ranked older one.
func (h *SkillMarketHandlers) expertMarketCategoryRows(ctx context.Context) ([]expertMarketCategory, error) {
	h.ensureExpertMarketCategories()
	rows, err := h.store.DB().QueryContext(ctx, `SELECT id, name, sort_order FROM sm_expert_market_categories ORDER BY sort_order ASC, created_at ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]expertMarketCategory, 0, 8)
	for rows.Next() {
		var item expertMarketCategory
		if err := rows.Scan(&item.ID, &item.Name, &item.SortOrder); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (h *SkillMarketHandlers) expertMarketCategoryNames(ctx context.Context) ([]string, error) {
	items, err := h.expertMarketCategoryRows(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	return names, nil
}

// normalizeExpertMarketCategoryName trims and bounds a category name. Names
// are matched verbatim after trimming; no case folding, because the taxonomy
// is dominated by CJK words where folding is meaningless.
func normalizeExpertMarketCategoryName(raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || len([]rune(name)) > expertMarketCategoryMaxRunes {
		return "", false
	}
	return name, true
}

// emitExpertMarketCategoriesSync broadcasts the full taxonomy after every
// mutation. This runs SYNCHRONOUSLY in the request: the payload is one tiny
// SELECT, and an async read could interleave with a concurrent mutation's
// commit such that an older snapshot lands in the HA log after a newer one
// (read order ≠ append order), leaving peers converged on a stale taxonomy —
// which then silently rejects valid share submissions. Deduplication still
// keeps repeated emits of unchanged data quiet.
func (h *SkillMarketHandlers) emitExpertMarketCategoriesSync(ctx context.Context) {
	if h == nil || h.expertMarketSync == nil {
		return
	}
	rows, err := h.expertMarketCategoryRows(context.WithoutCancel(ctx))
	if err != nil {
		return
	}
	h.expertMarketSync.AppendExpertMarketCategoriesSnapshot(context.WithoutCancel(ctx), expertMarketCategoriesSnapshot{ID: "categories", Categories: rows})
}

type expertMarketCategoriesSnapshot struct {
	// ID pins the full-list snapshot to its fixed replication key so the HA
	// payload-identity check passes ("categories").
	ID         string                 `json:"id"`
	Categories []expertMarketCategory `json:"categories"`
}

// SeedExpertMarketCategoriesHASync publishes the current taxonomy once at
// startup so a freshly joined (or lagging) peer converges even without a new
// mutation. Unchanged payloads are deduplicated by the HA log.
func (h *SkillMarketHandlers) SeedExpertMarketCategoriesHASync(ctx context.Context) {
	if h == nil || h.expertMarketSync == nil {
		return
	}
	rows, err := h.expertMarketCategoryRows(ctx)
	if err != nil {
		log.Printf("[expert-market] WARN: list categories for HA seed failed: %v", err)
		return
	}
	h.expertMarketSync.AppendExpertMarketCategoriesSnapshot(ctx, expertMarketCategoriesSnapshot{ID: "categories", Categories: rows})
}

// GET /api/v1/expert-market/categories — public, same auth posture as the
// catalogue (an optional session so signed-in desktops get a stable answer).
func (h *SkillMarketHandlers) ListExpertMarketCategories(w http.ResponseWriter, r *http.Request) {
	if _, err := h.optionalExpertMarketUser(r); err != nil {
		smError(w, http.StatusUnauthorized, err.Error())
		return
	}
	items, err := h.expertMarketCategoryRows(r.Context())
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"categories": items})
}

func (h *SkillMarketHandlers) adminExpertMarketCategoryActor(r *http.Request) string {
	actor := "administrator"
	if admin := AdminFromContext(r.Context()); admin != nil {
		actor = firstNonEmpty(admin.Username, admin.Email, actor)
	}
	return actor
}

// GET /api/v1/admin/expert-market/categories — taxonomy plus per-name listing
// counts so the admin card can show which names are actually in use.
func (h *SkillMarketHandlers) AdminListExpertMarketCategories(w http.ResponseWriter, r *http.Request) {
	items, err := h.expertMarketCategoryRows(r.Context())
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	counts := map[string]int64{}
	// Counts answer "how many listings carry this name" for the admin card;
	// rows already deleted or purged do not represent usable market entries.
	rows, err := h.store.DB().QueryContext(r.Context(), `SELECT category, COUNT(*) FROM sm_expert_market_listings WHERE status NOT IN ('deleted', 'purged') GROUP BY category`)
	if err == nil {
		for rows.Next() {
			var name string
			var count int64
			if rows.Scan(&name, &count) == nil {
				counts[name] = count
			}
		}
		rows.Close()
	}
	for i := range items {
		items[i].Listings = counts[items[i].Name]
	}
	writeJSON(w, http.StatusOK, map[string]any{"categories": items})
}

// POST /api/v1/admin/expert-market/categories {name}
func (h *SkillMarketHandlers) AdminCreateExpertMarketCategory(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketCategories()
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		smError(w, http.StatusBadRequest, "invalid category request")
		return
	}
	name, ok := normalizeExpertMarketCategoryName(body.Name)
	if !ok {
		smError(w, http.StatusBadRequest, fmt.Sprintf("category name must be 1-%d characters", expertMarketCategoryMaxRunes))
		return
	}
	tx, err := h.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	var existing int
	if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sm_expert_market_categories WHERE name=?`, name).Scan(&existing); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing > 0 {
		smError(w, http.StatusConflict, "category already exists")
		return
	}
	// The taxonomy is deliberately finite — the desktop renders every name as a
	// chip and the replication cap must match the origin's ceiling, otherwise
	// peers silently truncate and the taxonomies diverge.
	var count int
	if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sm_expert_market_categories`).Scan(&count); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if count >= expertMarketCategoryMaxCount {
		smError(w, http.StatusConflict, fmt.Sprintf("category limit reached (%d)", expertMarketCategoryMaxCount))
		return
	}
	// New categories append after every current one.
	var maxOrder sql.NullInt64
	_ = tx.QueryRowContext(r.Context(), `SELECT MAX(sort_order) FROM sm_expert_market_categories`).Scan(&maxOrder)
	nextOrder := int(maxOrder.Int64) + 1
	now := expertMarketNow()
	id := "expert_cat_" + uniqueID("cat")
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO sm_expert_market_categories (id, name, sort_order, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, id, name, nextOrder, now, now); err != nil {
		// Two concurrent creates can pass the COUNT check and collide on the
		// UNIQUE name; report that as a conflict, not a server error.
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			smError(w, http.StatusConflict, "category already exists")
			return
		}
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.emitExpertMarketCategoriesSync(r.Context())
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "name": name, "sort_order": nextOrder})
}

// PUT /api/v1/admin/expert-market/categories/{id} {name} — rename. Listings
// carrying the old name follow it so the taxonomy and the catalogue stay in
// lockstep; this update deliberately does not bump listing updated_at, which
// would reshuffle the moderation list over a cosmetic edit.
func (h *SkillMarketHandlers) AdminUpdateExpertMarketCategory(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketCategories()
	id := strings.TrimSpace(r.PathValue("id"))
	if !expertMarketIDPattern.MatchString(id) {
		smError(w, http.StatusNotFound, "category not found")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		smError(w, http.StatusBadRequest, "invalid category request")
		return
	}
	name, ok := normalizeExpertMarketCategoryName(body.Name)
	if !ok {
		smError(w, http.StatusBadRequest, fmt.Sprintf("category name must be 1-%d characters", expertMarketCategoryMaxRunes))
		return
	}
	tx, err := h.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	var oldName string
	if err := tx.QueryRowContext(r.Context(), `SELECT name FROM sm_expert_market_categories WHERE id=?`, id).Scan(&oldName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			smError(w, http.StatusNotFound, "category not found")
			return
		}
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if oldName != name {
		var existing int
		if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sm_expert_market_categories WHERE name=? AND id<>?`, name, id).Scan(&existing); err != nil {
			smError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if existing > 0 {
			smError(w, http.StatusConflict, "category already exists")
			return
		}
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE sm_expert_market_categories SET name=?, updated_at=? WHERE id=?`, name, expertMarketNow(), id); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if oldName != name {
		if _, err := tx.ExecContext(r.Context(), `UPDATE sm_expert_market_listings SET category=? WHERE category=?`, name, oldName); err != nil {
			smError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.emitExpertMarketCategoriesSync(r.Context())
	if oldName != name {
		// The transaction already rewrote the listing rows to the new name, so
		// the re-broadcast must select by the NEW name — the old one matches
		// nothing and the renamed listings would never reach HA peers.
		h.emitRenamedCategoryListings(r.Context(), name)
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "name": name})
}

// emitRenamedCategoryListings re-broadcasts every listing row that followed a
// rename (called with the NEW name — the rows already carry it). Only those
// rows changed; unchanged listings keep their deduplicated snapshots.
func (h *SkillMarketHandlers) emitRenamedCategoryListings(ctx context.Context, newName string) {
	ctx = context.WithoutCancel(ctx)
	go func() {
		rows, err := h.store.DB().QueryContext(ctx, `SELECT id FROM sm_expert_market_listings WHERE category=?`, strings.TrimSpace(newName))
		if err != nil {
			return
		}
		ids := make([]string, 0, 16)
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		for _, id := range ids {
			h.emitExpertMarketSync(ctx, id)
		}
	}()
}

// POST /api/v1/admin/expert-market/categories/reorder {ids: [...]} — the full
// ordered id list. Absent ids keep their relative order after the listed ones.
func (h *SkillMarketHandlers) AdminReorderExpertMarketCategories(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketCategories()
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		smError(w, http.StatusBadRequest, "invalid category request")
		return
	}
	items, err := h.expertMarketCategoryRows(r.Context())
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	byID := make(map[string]expertMarketCategory, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	ordered := make([]string, 0, len(items))
	for _, id := range body.IDs {
		if _, exists := byID[strings.TrimSpace(id)]; exists {
			ordered = append(ordered, strings.TrimSpace(id))
			delete(byID, strings.TrimSpace(id))
		}
	}
	for _, item := range items {
		if _, remains := byID[item.ID]; remains {
			ordered = append(ordered, item.ID)
		}
	}
	tx, err := h.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	now := expertMarketNow()
	for i, id := range ordered {
		if _, err := tx.ExecContext(r.Context(), `UPDATE sm_expert_market_categories SET sort_order=?, updated_at=? WHERE id=?`, i+1, now, id); err != nil {
			smError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.emitExpertMarketCategoriesSync(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"ids": ordered})
}

// DELETE /api/v1/admin/expert-market/categories/{id} — removes the name from
// the taxonomy. Listing rows KEEP the deleted text: the desktop renders any
// name outside the current taxonomy as 其它, and re-creating the same name
// restores those listings without a mass update.
func (h *SkillMarketHandlers) AdminDeleteExpertMarketCategory(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketCategories()
	id := strings.TrimSpace(r.PathValue("id"))
	if !expertMarketIDPattern.MatchString(id) {
		smError(w, http.StatusNotFound, "category not found")
		return
	}
	res, err := h.store.DB().ExecContext(r.Context(), `DELETE FROM sm_expert_market_categories WHERE id=?`, id)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		smError(w, http.StatusNotFound, "category not found")
		return
	}
	h.emitExpertMarketCategoriesSync(r.Context())
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// POST /api/v1/admin/expert-market/experts/{id}/category {category} — sets or
// clears one listing's category during review. An empty value clears the tag.
func (h *SkillMarketHandlers) AdminSetExpertMarketListingCategory(w http.ResponseWriter, r *http.Request) {
	h.ensureExpertMarketSchema()
	listingID, ok := expertMarketListingID(w, r)
	if !ok {
		return
	}
	var body struct {
		Category string `json:"category"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		smError(w, http.StatusBadRequest, "invalid category request")
		return
	}
	category := strings.TrimSpace(body.Category)
	if category != "" {
		if len([]rune(category)) > expertMarketCategoryMaxRunes {
			smError(w, http.StatusBadRequest, fmt.Sprintf("category name must be 1-%d characters", expertMarketCategoryMaxRunes))
			return
		}
		allowed, err := h.expertMarketCategoryNames(r.Context())
		if err != nil {
			smError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !slices.Contains(allowed, category) {
			smError(w, http.StatusBadRequest, "category is not available; refresh categories and try again")
			return
		}
	}
	var before string
	err := h.store.DB().QueryRowContext(r.Context(), `SELECT category FROM sm_expert_market_listings WHERE id=?`, listingID).Scan(&before)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			smError(w, http.StatusNotFound, "expert listing not found")
			return
		}
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if before == category {
		writeJSON(w, http.StatusOK, map[string]string{"category": category})
		return
	}
	// One transaction like every other listing mutation: a lost audit event
	// must not leave a silently retagged listing behind.
	tx, err := h.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `UPDATE sm_expert_market_listings SET category=? WHERE id=?`, category, listingID); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	actor := h.adminExpertMarketCategoryActor(r)
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO sm_expert_market_events (id, listing_id, actor, action, reason, created_at) VALUES (?, ?, ?, 'category_set', ?, ?)`,
		"expert_"+uniqueID("event"), listingID, actor, fmt.Sprintf("%s -> %s", before, category), expertMarketNow()); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.emitExpertMarketSync(r.Context(), listingID)
	writeJSON(w, http.StatusOK, map[string]string{"category": category})
}

// ApplyExpertMarketCategoriesSnapshot converges a peer's taxonomy to the
// origin's full list. Replacing the table (rather than merging) keeps every
// node identical after deletes and reorders; a node promoted to leader later
// therefore validates submissions with the taxonomy the administrator set.
func (h *SkillMarketHandlers) ApplyExpertMarketCategoriesSnapshot(ctx context.Context, opType string, raw json.RawMessage) error {
	if opType == "delete" {
		// The taxonomy is replicated as full-list upserts only. A delete op
		// would remove one row's replication key, not a category.
		return nil
	}
	h.ensureExpertMarketCategories()
	var snap expertMarketCategoriesSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return err
	}
	seen := make(map[string]bool, len(snap.Categories))
	normalized := make([]expertMarketCategory, 0, len(snap.Categories))
	for _, item := range snap.Categories {
		// The taxonomy is deliberately finite — the desktop renders every name
		// as a chip, so a runaway snapshot would wrap the filter row forever.
		if len(normalized) >= expertMarketCategoryMaxCount {
			break
		}
		name, ok := normalizeExpertMarketCategoryName(item.Name)
		if !ok || seen[name] {
			continue
		}
		seen[name] = true
		if item.SortOrder <= 0 {
			item.SortOrder = len(normalized) + 1
		}
		item.Name = name
		normalized = append(normalized, item)
	}
	tx, err := h.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM sm_expert_market_categories`); err != nil {
		return err
	}
	now := expertMarketNow()
	for _, item := range normalized {
		id := strings.TrimSpace(item.ID)
		if !expertMarketIDPattern.MatchString(id) {
			id = "expert_cat_" + uniqueID("cat")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO sm_expert_market_categories (id, name, sort_order, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, id, item.Name, item.SortOrder, now, now); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sm_expert_market_meta (key, value) VALUES ('expert_categories_seeded', '1') ON CONFLICT(key) DO UPDATE SET value=excluded.value`); err != nil {
		return err
	}
	return tx.Commit()
}
