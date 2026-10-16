package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/ha"
)

// latexTemplateSyncRecorder is the HubCenter HA log. Skill packages publish one
// catalogue snapshot after each change; LaTeX templates do the same, and the
// snapshot also carries each package's zip so a peer can still serve downloads
// after failover.
type latexTemplateSyncRecorder interface {
	AppendLatexTemplateSnapshot(ctx context.Context, snap any)
}

type latexTemplateHASeeder interface {
	HasEntityTypeOps(ctx context.Context, entityTypes ...string) (bool, error)
	AppendLatexTemplateSnapshot(ctx context.Context, snap any)
}

const (
	latexTemplateSyncDumpRetries = 3
	latexTemplateSyncRetryDelay  = 100 * time.Millisecond
)

type latexTemplateSnapshot struct {
	Categories      []latexTemplateSnapshotCategory  `json:"categories"`
	Templates       []latexTemplateSnapshotTemplate  `json:"templates"`
	TemplateDeletes []latexTemplateSnapshotTombstone `json:"template_deletes,omitempty"`
	CategoryDeletes []latexTemplateSnapshotTombstone `json:"category_deletes,omitempty"`
}

// latexTemplateSnapshotTombstone is how a delete survives a peer that has not
// seen the row yet. A later snapshot that simply omits the package must not
// delete it: that peer may have published a template this node has not applied.
type latexTemplateSnapshotTombstone struct {
	ID        string `json:"id"`
	DeletedAt string `json:"deleted_at"`
}

type latexTemplateSnapshotCategory struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
	Status      string `json:"status"`
	Builtin     bool   `json:"builtin"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type latexTemplateSnapshotTemplate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CategoryID  string `json:"category_id"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	MainFile    string `json:"main_file"`
	FileCount   int    `json:"file_count"`
	SizeBytes   int64  `json:"size_bytes"`
	SHA256      string `json:"sha256"`
	Status      string `json:"status"`
	ReviewNote  string `json:"review_note,omitempty"`
	OwnerID     string `json:"owner_id,omitempty"`
	OwnerEmail  string `json:"owner_email,omitempty"`
	Source      string `json:"source"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	ZipBase64   string `json:"zip_base64"`
}

func (h *LatexTemplateHandlers) SetSyncRecorder(rec latexTemplateSyncRecorder) {
	if h == nil {
		return
	}
	h.syncMu.Lock()
	defer h.syncMu.Unlock()
	h.sync = rec
}

func (h *LatexTemplateHandlers) emitSync(ctx context.Context) {
	if h == nil || h.currentSyncRecorder() == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	} else {
		ctx = context.WithoutCancel(ctx)
	}
	if !h.beginSyncEmission() {
		return
	}
	go h.runSyncEmission(ctx)
}

func (h *LatexTemplateHandlers) currentSyncRecorder() latexTemplateSyncRecorder {
	if h == nil {
		return nil
	}
	h.syncMu.Lock()
	defer h.syncMu.Unlock()
	return h.sync
}

func (h *LatexTemplateHandlers) beginSyncEmission() bool {
	h.syncMu.Lock()
	defer h.syncMu.Unlock()
	if h.syncRunning {
		h.syncPending = true
		return false
	}
	h.syncRunning = true
	return true
}

func (h *LatexTemplateHandlers) finishOrContinueSyncEmission() bool {
	h.syncMu.Lock()
	defer h.syncMu.Unlock()
	if h.syncPending {
		h.syncPending = false
		return true
	}
	h.syncRunning = false
	return false
}

func (h *LatexTemplateHandlers) runSyncEmission(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[hubcenter][latex-templates] sync emission recovered: %v", r)
			h.recoverSyncEmission(ctx)
		}
	}()
	for {
		snap, err := h.dumpSnapshotWithRetry(ctx)
		if err == nil {
			if rec := h.currentSyncRecorder(); rec != nil {
				rec.AppendLatexTemplateSnapshot(ctx, snap)
			}
		}
		if !h.finishOrContinueSyncEmission() {
			return
		}
	}
}

func (h *LatexTemplateHandlers) recoverSyncEmission(ctx context.Context) {
	if h == nil {
		return
	}
	restart := false
	h.syncMu.Lock()
	if h.syncPending && h.sync != nil {
		h.syncPending = false
		h.syncRunning = true
		restart = true
	} else {
		h.syncRunning = false
		h.syncPending = false
	}
	h.syncMu.Unlock()
	if restart {
		go h.runSyncEmission(ctx)
	}
}

func (h *LatexTemplateHandlers) dumpSnapshotWithRetry(ctx context.Context) (*latexTemplateSnapshot, error) {
	var lastErr error
	for attempt := 0; attempt <= latexTemplateSyncDumpRetries; attempt++ {
		snap, err := h.DumpSnapshot(ctx)
		if err == nil {
			return snap, nil
		}
		lastErr = err
		if attempt < latexTemplateSyncDumpRetries {
			time.Sleep(latexTemplateSyncRetryDelay)
		}
	}
	log.Printf("[hubcenter][latex-templates] sync snapshot dump failed after %d retries: %v", latexTemplateSyncDumpRetries, lastErr)
	return nil, lastErr
}

// DumpHASnapshot is the HA catch-up shape: payload plus how much of it is
// worth sending. Stock built-in categories alone count as empty.
func (h *LatexTemplateHandlers) DumpHASnapshot(ctx context.Context) (any, int, error) {
	snap, err := h.DumpSnapshot(ctx)
	if err != nil || snap == nil {
		return snap, 0, err
	}
	return snap, latexTemplateSnapshotReplicationCount(snap), nil
}

// latexTemplateSnapshotReplicationCount is how much of a snapshot is worth
// sending. Stock built-in categories exist on every node, so they do not count.
// A renamed category, a package, or a delete does.
func latexTemplateSnapshotReplicationCount(snap *latexTemplateSnapshot) int {
	if snap == nil {
		return 0
	}
	n := len(snap.Templates) + len(snap.TemplateDeletes) + len(snap.CategoryDeletes)
	if n > 0 {
		return n
	}
	if latexTemplateSnapshotHasCustomCategory(snap.Categories) {
		return 1
	}
	return 0
}

func latexTemplateSnapshotHasCustomCategory(categories []latexTemplateSnapshotCategory) bool {
	for _, item := range categories {
		if !latexTemplateBuiltinUnchanged(item.ID, item.Name, item.Description, item.SortOrder, item.Status) {
			return true
		}
	}
	return false
}

func latexTemplateBuiltinUnchanged(id, name, description string, sort int, status string) bool {
	for _, category := range builtinLatexTemplateCategories {
		if category.ID != id {
			continue
		}
		return category.Name == name && category.Description == description && category.SortOrder == sort && (status == "" || status == category.Status)
	}
	return false
}

func (h *LatexTemplateHandlers) CountSnapshotRecords(ctx context.Context) (int64, error) {
	if h == nil || h.db == nil {
		return 0, nil
	}
	if err := h.ensureSchema(); err != nil {
		return 0, err
	}
	var categories, templates int64
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM latex_template_categories`).Scan(&categories); err != nil {
		return 0, err
	}
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM latex_templates`).Scan(&templates); err != nil {
		return 0, err
	}
	return categories + templates, nil
}

func (h *LatexTemplateHandlers) DumpSnapshot(ctx context.Context) (*latexTemplateSnapshot, error) {
	if h == nil || h.db == nil {
		return &latexTemplateSnapshot{}, nil
	}
	if err := h.ensureSchema(); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	snap := &latexTemplateSnapshot{}
	categoryRows, err := h.db.QueryContext(ctx, `SELECT id,name,description,sort_order,status,builtin,created_at,updated_at FROM latex_template_categories ORDER BY sort_order,id`)
	if err != nil {
		return nil, err
	}
	defer categoryRows.Close()
	for categoryRows.Next() {
		var item latexTemplateSnapshotCategory
		var builtin int
		if err := categoryRows.Scan(&item.ID, &item.Name, &item.Description, &item.SortOrder, &item.Status, &builtin, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Builtin = builtin != 0
		snap.Categories = append(snap.Categories, item)
	}
	if err := categoryRows.Err(); err != nil {
		return nil, err
	}
	rows, err := h.db.QueryContext(ctx, `SELECT `+latexTemplateSelectColumns+`,zip_path FROM latex_templates ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item LatexTemplate
		var zipPath string
		if err := rows.Scan(newLatexTemplateScanTargets(&item, &zipPath)...); err != nil {
			return nil, err
		}
		data, err := os.ReadFile(zipPath)
		if err != nil {
			return nil, fmt.Errorf("read latex template %s: %w", item.ID, err)
		}
		snap.Templates = append(snap.Templates, latexTemplateSnapshotTemplate{
			ID: item.ID, Name: item.Name, Description: item.Description, CategoryID: item.CategoryID,
			Version: item.Version, Author: item.Author, MainFile: item.MainFile, FileCount: item.FileCount,
			SizeBytes: item.SizeBytes, SHA256: item.SHA256, Status: item.Status, ReviewNote: item.ReviewNote,
			OwnerID: item.OwnerID, OwnerEmail: item.OwnerEmail, Source: item.Source,
			CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
			ZipBase64: base64.StdEncoding.EncodeToString(data),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if snap.TemplateDeletes, err = h.dumpLatexTombstones(ctx, "latex_template_tombstones"); err != nil {
		return nil, err
	}
	if snap.CategoryDeletes, err = h.dumpLatexTombstones(ctx, "latex_template_category_tombstones"); err != nil {
		return nil, err
	}
	if snap.Categories == nil {
		snap.Categories = []latexTemplateSnapshotCategory{}
	}
	if snap.Templates == nil {
		snap.Templates = []latexTemplateSnapshotTemplate{}
	}
	return snap, nil
}

func (h *LatexTemplateHandlers) dumpLatexTombstones(ctx context.Context, table string) ([]latexTemplateSnapshotTombstone, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT id, deleted_at FROM `+table+` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []latexTemplateSnapshotTombstone
	for rows.Next() {
		var item latexTemplateSnapshotTombstone
		if err := rows.Scan(&item.ID, &item.DeletedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type latexTemplateStagedPackage struct {
	item    latexTemplateSnapshotTemplate
	sum     string
	final   string
	staging string
	replace bool
}

// ApplySnapshot merges a peer catalogue into this node. Packages the snapshot
// does not mention stay in place. A delete arrives as a tombstone, and a row
// is changed only when the snapshot is at least as new. Package bytes land in
// a side file first and replace the live zip only after the catalogue commit
// succeeds.
func (h *LatexTemplateHandlers) ApplySnapshot(ctx context.Context, raw json.RawMessage) error {
	if h == nil || h.db == nil || len(raw) == 0 {
		return nil
	}
	var snap latexTemplateSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return err
	}
	if err := h.ensureSchema(); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	// Stage beside the live zip while the catalogue lock is held, so a second
	// apply cannot publish over the same side file.
	staged, err := h.stageLatexTemplateSnapshot(ctx, snap)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		for _, item := range staged {
			if item.replace {
				_ = os.Remove(item.staging)
			}
		}
	}()
	blockedCategory := map[string]bool{}
	for _, item := range snap.Categories {
		blocked, err := h.latexSnapshotRowBlocked(ctx, "latex_template_category_tombstones", "latex_template_categories", item.ID, item.UpdatedAt)
		if err != nil {
			return err
		}
		blockedCategory[item.ID] = blocked
	}
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range snap.Categories {
		if !latexTemplateSnapshotID(item.ID) {
			return fmt.Errorf("latex template category id %q is not a single path segment", item.ID)
		}
		if blockedCategory[item.ID] {
			continue
		}
		builtin := 0
		if item.Builtin {
			builtin = 1
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO latex_template_categories(id,name,description,sort_order,status,builtin,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET name=excluded.name, description=excluded.description, sort_order=excluded.sort_order, status=excluded.status, builtin=MAX(latex_template_categories.builtin, excluded.builtin), updated_at=excluded.updated_at`,
			item.ID, item.Name, item.Description, item.SortOrder, firstNonEmptyLatexTemplate(item.Status, "active"), builtin, item.CreatedAt, item.UpdatedAt); err != nil {
			return fmt.Errorf("upsert latex category %s: %w", item.ID, err)
		}
	}
	for _, item := range staged {
		zipPath := item.final
		if _, err := tx.ExecContext(ctx, `INSERT INTO latex_templates(id,name,description,category_id,version,author,main_file,file_count,size_bytes,sha256,zip_path,status,review_note,owner_id,owner_email,source,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET name=excluded.name, description=excluded.description, category_id=excluded.category_id, version=excluded.version, author=excluded.author, main_file=excluded.main_file, file_count=excluded.file_count, size_bytes=excluded.size_bytes, sha256=excluded.sha256, zip_path=excluded.zip_path, status=excluded.status, review_note=excluded.review_note, owner_id=excluded.owner_id, owner_email=excluded.owner_email, source=excluded.source, created_at=excluded.created_at, updated_at=excluded.updated_at`,
			item.item.ID, item.item.Name, item.item.Description, item.item.CategoryID, item.item.Version, item.item.Author, item.item.MainFile, item.item.FileCount, item.item.SizeBytes, item.sum, zipPath, item.item.Status, item.item.ReviewNote, item.item.OwnerID, item.item.OwnerEmail, item.item.Source, item.item.CreatedAt, item.item.UpdatedAt); err != nil {
			return fmt.Errorf("upsert latex template %s: %w", item.item.ID, err)
		}
	}
	removedTemplates, err := h.applyLatexTombstones(ctx, tx, "latex_template_tombstones", "latex_templates", snap.TemplateDeletes)
	if err != nil {
		return err
	}
	if _, err := h.applyLatexCategoryTombstones(ctx, tx, snap.CategoryDeletes); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	removed := map[string]struct{}{}
	for _, id := range removedTemplates {
		removed[id] = struct{}{}
	}
	for _, item := range staged {
		if _, drop := removed[item.item.ID]; drop {
			_ = os.Remove(item.staging)
			continue
		}
		if !item.replace {
			continue
		}
		if err := publishLatexTemplateZip(item.staging, item.final); err != nil {
			return fmt.Errorf("publish latex template %s: %w", item.item.ID, err)
		}
	}
	committed = true
	for _, id := range removedTemplates {
		_ = os.Remove(filepath.Join(h.packageDir(), id+".zip"))
	}
	return nil
}

func latexTemplateBuiltinCategory(id string) bool {
	for _, category := range builtinLatexTemplateCategories {
		if category.ID == id {
			return true
		}
	}
	return false
}

// latexSnapshotRowBlocked reports that the local row or a local tombstone is
// newer than this snapshot entry, so applying it would roll the catalogue back.
func (h *LatexTemplateHandlers) latexSnapshotRowBlocked(ctx context.Context, tombstoneTable, rowTable, id, updatedAt string) (bool, error) {
	var deletedAt string
	err := h.db.QueryRowContext(ctx, `SELECT deleted_at FROM `+tombstoneTable+` WHERE id=?`, id).Scan(&deletedAt)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	if err == nil && latexTemplateTimeOK(deletedAt) && updatedAt != "" && !latexTemplateTimeAfter(updatedAt, deletedAt) {
		return true, nil
	}
	var localUpdated string
	err = h.db.QueryRowContext(ctx, `SELECT updated_at FROM `+rowTable+` WHERE id=?`, id).Scan(&localUpdated)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, ok := latexTemplateParsedTime(updatedAt); !ok {
		return true, nil
	}
	return latexTemplateTimeAfter(localUpdated, updatedAt), nil
}

func rememberLatexTombstone(ctx context.Context, tx *sql.Tx, table, id, deletedAt string) error {
	var existing string
	err := tx.QueryRowContext(ctx, `SELECT deleted_at FROM `+table+` WHERE id=?`, id).Scan(&existing)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil && latexTemplateTimeAfter(existing, deletedAt) {
		return nil
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO `+table+`(id, deleted_at) VALUES(?, ?) ON CONFLICT(id) DO UPDATE SET deleted_at=excluded.deleted_at`, id, deletedAt)
	return err
}

func (h *LatexTemplateHandlers) applyLatexTombstones(ctx context.Context, tx *sql.Tx, tombstoneTable, rowTable string, items []latexTemplateSnapshotTombstone) ([]string, error) {
	var removed []string
	for _, item := range items {
		if !latexTemplateSnapshotID(item.ID) || !latexTemplateTimeOK(item.DeletedAt) {
			continue
		}
		if err := rememberLatexTombstone(ctx, tx, tombstoneTable, item.ID, item.DeletedAt); err != nil {
			return nil, err
		}
		var localUpdated string
		err := tx.QueryRowContext(ctx, `SELECT updated_at FROM `+rowTable+` WHERE id=?`, item.ID).Scan(&localUpdated)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		if latexTemplateTimeAfter(localUpdated, item.DeletedAt) {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+rowTable+` WHERE id=?`, item.ID); err != nil {
			return nil, err
		}
		removed = append(removed, item.ID)
	}
	return removed, nil
}

func (h *LatexTemplateHandlers) applyLatexCategoryTombstones(ctx context.Context, tx *sql.Tx, items []latexTemplateSnapshotTombstone) ([]string, error) {
	var removed []string
	for _, item := range items {
		if !latexTemplateSnapshotID(item.ID) || !latexTemplateTimeOK(item.DeletedAt) || latexTemplateBuiltinCategory(item.ID) {
			continue
		}
		if err := rememberLatexTombstone(ctx, tx, "latex_template_category_tombstones", item.ID, item.DeletedAt); err != nil {
			return nil, err
		}
		var localUpdated string
		err := tx.QueryRowContext(ctx, `SELECT updated_at FROM latex_template_categories WHERE id=? AND builtin=0`, item.ID).Scan(&localUpdated)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		if latexTemplateTimeAfter(localUpdated, item.DeletedAt) {
			continue
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM latex_template_categories WHERE id=? AND builtin=0 AND NOT EXISTS (SELECT 1 FROM latex_templates WHERE category_id = latex_template_categories.id)`, item.ID)
		if err != nil {
			return nil, err
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			removed = append(removed, item.ID)
		}
	}
	return removed, nil
}

func (h *LatexTemplateHandlers) stageLatexTemplateSnapshot(ctx context.Context, snap latexTemplateSnapshot) ([]latexTemplateStagedPackage, error) {
	for _, item := range snap.Categories {
		if !latexTemplateSnapshotID(item.ID) {
			return nil, fmt.Errorf("latex template category id %q is not a single path segment", item.ID)
		}
	}
	incomingDelete := map[string]string{}
	for _, item := range snap.TemplateDeletes {
		if !latexTemplateSnapshotID(item.ID) || !latexTemplateTimeOK(item.DeletedAt) {
			continue
		}
		if prev := incomingDelete[item.ID]; prev == "" || latexTemplateTimeAfter(item.DeletedAt, prev) {
			incomingDelete[item.ID] = item.DeletedAt
		}
	}
	seen := map[string]struct{}{}
	staged := make([]latexTemplateStagedPackage, 0, len(snap.Templates))
	cleanup := func() {
		for _, item := range staged {
			if item.replace {
				_ = os.Remove(item.staging)
			}
		}
	}
	for _, item := range snap.Templates {
		if !latexTemplateSnapshotID(item.ID) {
			cleanup()
			return nil, fmt.Errorf("latex template id %q is not a single path segment", item.ID)
		}
		if _, dup := seen[item.ID]; dup {
			cleanup()
			return nil, fmt.Errorf("latex template id %q is duplicated", item.ID)
		}
		seen[item.ID] = struct{}{}
		if strings.TrimSpace(item.ZipBase64) == "" {
			cleanup()
			return nil, fmt.Errorf("latex template %s is missing its package", item.ID)
		}
		if deletedAt := incomingDelete[item.ID]; deletedAt != "" && !latexTemplateTimeAfter(item.UpdatedAt, deletedAt) {
			continue
		}
		blocked, err := h.latexSnapshotRowBlocked(ctx, "latex_template_tombstones", "latex_templates", item.ID, item.UpdatedAt)
		if err != nil {
			cleanup()
			return nil, err
		}
		if blocked {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(item.ZipBase64)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("decode latex template %s: %w", item.ID, err)
		}
		sum := sha256.Sum256(data)
		got := hex.EncodeToString(sum[:])
		if item.SHA256 != "" && !strings.EqualFold(item.SHA256, got) {
			cleanup()
			return nil, fmt.Errorf("latex template %s checksum mismatch", item.ID)
		}
		final := filepath.Join(h.packageDir(), item.ID+".zip")
		entry := latexTemplateStagedPackage{item: item, sum: got, final: final}
		if latexTemplateFileSHA(final) == got {
			staged = append(staged, entry)
			continue
		}
		if err := os.MkdirAll(h.packageDir(), 0o755); err != nil {
			cleanup()
			return nil, err
		}
		entry.staging = final + ".ha"
		entry.replace = true
		if err := os.WriteFile(entry.staging, data, 0o644); err != nil {
			cleanup()
			return nil, err
		}
		staged = append(staged, entry)
	}
	return staged, nil
}

// publishLatexTemplateZip moves a staged package onto the download path.
// Windows refuses to rename over a file that is already there, so the previous
// zip is moved aside and restored if the publish does not complete.
func publishLatexTemplateZip(staging, final string) error {
	backup := final + ".bak"
	_ = os.Remove(backup)
	if err := os.Rename(final, backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(staging, final); err != nil {
		if _, statErr := os.Stat(backup); statErr == nil {
			_ = os.Rename(backup, final)
		}
		return err
	}
	_ = os.Remove(backup)
	return nil
}

func latexTemplateFileSHA(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return ""
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func latexTemplateSnapshotID(id string) bool {
	id = strings.TrimSpace(id)
	return id != "" && id != "." && id != ".." && filepath.Base(id) == id && !strings.ContainsAny(id, `/\`)
}

type latexTemplateQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func (h *LatexTemplateHandlers) listLatexTemplateIDs(ctx context.Context, q latexTemplateQueryer) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT id FROM latex_templates`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (h *LatexTemplateHandlers) listLatexCategoryIDs(ctx context.Context, q latexTemplateQueryer) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT id FROM latex_template_categories`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// seedHASnapshot publishes the local catalogue once, when this cluster has
// never logged a LaTeX snapshot. Later uploads emit their own snapshots.
// An empty catalogue is not seeded. Deletes travel as tombstones, so a peer
// that has not seen a package leaves that package in place.
func (h *LatexTemplateHandlers) seedHASnapshot(ctx context.Context, seeder latexTemplateHASeeder) {
	if h == nil || seeder == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	seeded, err := seeder.HasEntityTypeOps(ctx, ha.EntityLatexTemplateSnapshot)
	if err != nil {
		log.Printf("[hubcenter][ha] inspect latex template seed state failed: %v", err)
		return
	}
	if seeded {
		return
	}
	if err := h.ensureSchema(); err != nil {
		log.Printf("[hubcenter][ha] seed latex template snapshot failed: %v", err)
		return
	}
	// Publish through the same single-flight emitter as a local edit. A second
	// dump here could append an older catalogue after a newer upload.
	// Built-in categories alone are not a catalogue: every node creates them.
	n, err := h.latexCatalogueSeedCount(ctx)
	if err != nil {
		log.Printf("[hubcenter][ha] seed latex template snapshot failed: %v", err)
		return
	}
	if n == 0 {
		return
	}
	h.emitSync(ctx)
	log.Printf("[hubcenter][ha] seeded latex template snapshot: records=%d", n)
}

func (h *LatexTemplateHandlers) latexCatalogueSeedCount(ctx context.Context) (int, error) {
	var n int
	if err := h.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(1) FROM latex_templates) + (SELECT COUNT(1) FROM latex_template_tombstones) + (SELECT COUNT(1) FROM latex_template_category_tombstones)`).Scan(&n); err != nil {
		return 0, err
	}
	if n > 0 {
		return n, nil
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id,name,description,sort_order,status FROM latex_template_categories`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name, description, status string
		var sort int
		if err := rows.Scan(&id, &name, &description, &sort, &status); err != nil {
			return 0, err
		}
		if !latexTemplateBuiltinUnchanged(id, name, description, sort, status) {
			return 1, nil
		}
	}
	return 0, rows.Err()
}
