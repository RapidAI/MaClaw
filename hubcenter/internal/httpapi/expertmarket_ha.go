package httpapi

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// expertMarketEmitMu keeps a listing snapshot from being appended after the
// delete operation that removed it. The dump runs outside the request, so
// without this lock the snapshot can land later in the HA log and recreate
// the listing on every peer.
var expertMarketEmitMu sync.Mutex

var errExpertSnapshotOlder = errors.New("expert market snapshot is older than the stored listing")

// expertMarketSyncRecorder is implemented by ha.Service. Expert Market rows
// are not part of the Pet Store snapshot, so a peer that did not receive the
// original submission serves an empty catalogue until these operations arrive.
type expertMarketSyncRecorder interface {
	AppendExpertMarketListingSnapshot(context.Context, string, any)
	DeleteExpertMarketListingSnapshot(context.Context, string)
	AppendExpertMarketMetrics(context.Context, string, int64)
}

type expertMarketSnapshot struct {
	Listings []expertMarketSnapshotListing `json:"listings"`
}

type expertMarketSnapshotListing struct {
	ID                   string                         `json:"id"`
	OwnerID              string                         `json:"owner_id"`
	OwnerEmail           string                         `json:"owner_email"`
	SourceExpertID       string                         `json:"source_expert_id"`
	Name                 string                         `json:"name"`
	Description          string                         `json:"description"`
	Icon                 string                         `json:"icon"`
	Version              string                         `json:"version"`
	Price                int64                          `json:"price"`
	Visibility           string                         `json:"visibility"`
	Status               string                         `json:"status"`
	PackageSize          int64                          `json:"package_size"`
	DownloadCount        int64                          `json:"download_count"`
	PurchaseCount        int64                          `json:"purchase_count"`
	SalesAmount          int64                          `json:"sales_amount"`
	ReviewNote           string                         `json:"review_note"`
	PlatformDistribution bool                           `json:"platform_distribution"`
	CreatedAt            string                         `json:"created_at"`
	UpdatedAt            string                         `json:"updated_at"`
	ZipBase64            string                         `json:"zip_base64,omitempty"`
	Purchases            []expertMarketSnapshotPurchase `json:"purchases,omitempty"`
	ShareTargets         []expertMarketSnapshotShare    `json:"share_targets,omitempty"`
}

type expertMarketSnapshotPurchase struct {
	ID          string `json:"id"`
	ListingID   string `json:"listing_id"`
	BuyerUserID string `json:"buyer_user_id"`
	BuyerEmail  string `json:"buyer_email"`
	AmountPaid  int64  `json:"amount_paid"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
}

type expertMarketSnapshotShare struct {
	Contact   string `json:"contact"`
	UserID    string `json:"user_id"`
	CreatedAt string `json:"created_at"`
}

func (h *SkillMarketHandlers) emitExpertMarketSync(ctx context.Context, listingID string) {
	if h == nil || h.expertMarketSync == nil {
		return
	}
	listingID = strings.TrimSpace(listingID)
	if listingID == "" {
		return
	}
	ctx = context.WithoutCancel(ctx)
	go func() {
		expertMarketEmitMu.Lock()
		defer expertMarketEmitMu.Unlock()
		snap, err := h.dumpExpertMarketSnapshot(ctx, listingID)
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				log.Printf("[expert-market] WARN: dump sync snapshot for %s failed: %v", listingID, err)
			}
			return
		}
		if len(snap.Listings) != 1 {
			return
		}
		var updatedAt string
		err = h.store.DB().QueryRowContext(ctx, `SELECT updated_at FROM sm_expert_market_listings WHERE id=?`, listingID).Scan(&updatedAt)
		if err != nil || updatedAt != snap.Listings[0].UpdatedAt {
			return
		}
		h.expertMarketSync.AppendExpertMarketListingSnapshot(ctx, listingID, snap)
	}()
}

func (h *SkillMarketHandlers) emitExpertMarketDelete(ctx context.Context, listingID string) {
	if h == nil || h.expertMarketSync == nil {
		return
	}
	listingID = strings.TrimSpace(listingID)
	if listingID == "" {
		return
	}
	expertMarketEmitMu.Lock()
	defer expertMarketEmitMu.Unlock()
	h.expertMarketSync.DeleteExpertMarketListingSnapshot(context.WithoutCancel(ctx), listingID)
}

func (h *SkillMarketHandlers) emitExpertMarketMetrics(ctx context.Context, listingID string, downloads int64) {
	if h == nil || h.expertMarketSync == nil || downloads < 0 {
		return
	}
	listingID = strings.TrimSpace(listingID)
	if listingID == "" {
		return
	}
	h.expertMarketSync.AppendExpertMarketMetrics(context.WithoutCancel(ctx), listingID, downloads)
}

// SeedExpertMarketHASync publishes one snapshot per listing already stored on
// this node. Unchanged payloads are deduplicated by HA, so startup on a node
// that already broadcast them stays quiet, while a peer with an empty expert
// market can catch up.
func (h *SkillMarketHandlers) SeedExpertMarketHASync(ctx context.Context) {
	if h == nil || h.expertMarketSync == nil {
		return
	}
	h.ensureExpertMarketSchema()
	rows, err := h.store.DB().QueryContext(ctx, `SELECT id FROM sm_expert_market_listings ORDER BY id`)
	if err != nil {
		log.Printf("[expert-market] WARN: list listings for HA seed failed: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			h.emitExpertMarketSync(ctx, id)
		}
	}
}

func (h *SkillMarketHandlers) dumpExpertMarketSnapshot(ctx context.Context, listingID string) (*expertMarketSnapshot, error) {
	h.ensureExpertMarketSchema()
	item, err := scanExpertMarketListing(h.store.DB().QueryRowContext(ctx, `SELECT `+expertMarketListingSelectColumns+` FROM sm_expert_market_listings WHERE id=?`, listingID))
	if err != nil {
		return nil, err
	}
	listing := expertMarketSnapshotListing{
		ID: item.ID, OwnerID: item.OwnerID, OwnerEmail: item.OwnerEmail, SourceExpertID: item.SourceExpertID,
		Name: item.Name, Description: item.Description, Icon: item.Icon, Version: item.Version, Price: item.Price,
		Visibility: item.Visibility, Status: item.Status, PackageSize: item.PackageSize, DownloadCount: item.DownloadCount,
		PurchaseCount: item.PurchaseCount, SalesAmount: item.SalesAmount, ReviewNote: item.ReviewNote,
		PlatformDistribution: item.PlatformDistribution, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
	if item.Status != "purged" {
		path, err := h.expertMarketPackagePath(item.ZipPath)
		if err != nil {
			return nil, err
		}
		zipData, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read expert package %s: %w", item.ID, err)
		}
		listing.PackageSize = int64(len(zipData))
		listing.ZipBase64 = base64.StdEncoding.EncodeToString(zipData)
	}
	purchaseRows, err := h.store.DB().QueryContext(ctx, `SELECT id, listing_id, buyer_user_id, buyer_email, amount_paid, status, created_at FROM sm_expert_market_purchases WHERE listing_id=? ORDER BY id`, listingID)
	if err != nil {
		return nil, err
	}
	defer purchaseRows.Close()
	for purchaseRows.Next() {
		var purchase expertMarketSnapshotPurchase
		if err := purchaseRows.Scan(&purchase.ID, &purchase.ListingID, &purchase.BuyerUserID, &purchase.BuyerEmail, &purchase.AmountPaid, &purchase.Status, &purchase.CreatedAt); err != nil {
			return nil, err
		}
		listing.Purchases = append(listing.Purchases, purchase)
	}
	if err := purchaseRows.Err(); err != nil {
		return nil, err
	}
	// The writer pool has one connection. Leave it free before the next query.
	if err := purchaseRows.Close(); err != nil {
		return nil, err
	}
	shareRows, err := h.store.DB().QueryContext(ctx, `SELECT contact, user_id, created_at FROM sm_expert_market_share_targets WHERE listing_id=? ORDER BY contact`, listingID)
	if err != nil {
		return nil, err
	}
	defer shareRows.Close()
	for shareRows.Next() {
		var share expertMarketSnapshotShare
		if err := shareRows.Scan(&share.Contact, &share.UserID, &share.CreatedAt); err != nil {
			return nil, err
		}
		listing.ShareTargets = append(listing.ShareTargets, share)
	}
	if err := shareRows.Err(); err != nil {
		return nil, err
	}
	return &expertMarketSnapshot{Listings: []expertMarketSnapshotListing{listing}}, nil
}

// ApplyExpertMarketSnapshot installs one replicated listing, or removes it
// when the origin hard-deleted a private share.
func (h *SkillMarketHandlers) ApplyExpertMarketSnapshot(ctx context.Context, opType string, raw json.RawMessage) error {
	if opType == "delete" {
		return h.deleteExpertMarketSnapshot(ctx, raw)
	}
	return h.upsertExpertMarketSnapshot(ctx, raw)
}

func (h *SkillMarketHandlers) ApplyExpertMarketMetrics(ctx context.Context, raw json.RawMessage) error {
	h.ensureExpertMarketSchema()
	var payload struct {
		ID        string `json:"id"`
		Downloads int64  `json:"downloads"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	payload.ID = strings.TrimSpace(payload.ID)
	if payload.ID == "" || payload.Downloads < 0 {
		return fmt.Errorf("invalid expert market metrics")
	}
	_, err := h.store.DB().ExecContext(ctx, `UPDATE sm_expert_market_listings SET download_count=MAX(download_count, ?) WHERE id=?`, payload.Downloads, payload.ID)
	return err
}

func (h *SkillMarketHandlers) deleteExpertMarketSnapshot(ctx context.Context, raw json.RawMessage) error {
	h.ensureExpertMarketSchema()
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	id := strings.TrimSpace(payload.ID)
	if !expertMarketIDPattern.MatchString(id) {
		return fmt.Errorf("invalid expert market delete id")
	}
	var zipPath string
	err := h.store.DB().QueryRowContext(ctx, `SELECT zip_path FROM sm_expert_market_listings WHERE id=?`, id).Scan(&zipPath)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	tx, err := h.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO sm_expert_market_removals (id, removed_at) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET removed_at=excluded.removed_at`, id, expertMarketNow()); err != nil {
		return err
	}
	for _, table := range []string{"sm_expert_market_share_targets", "sm_expert_market_events", "sm_expert_market_installations", "sm_expert_market_downloads", "sm_expert_market_purchases"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE listing_id=?", id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sm_expert_market_listings WHERE id=?`, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if strings.TrimSpace(zipPath) != "" {
		_ = h.removeExpertMarketPackage(zipPath)
	}
	_ = h.removeExpertMarketPackage(filepath.Join(h.expertMarketDir(), id+".zip"))
	return nil
}

func (h *SkillMarketHandlers) upsertExpertMarketSnapshot(ctx context.Context, raw json.RawMessage) error {
	h.ensureExpertMarketSchema()
	var snap expertMarketSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return err
	}
	if len(snap.Listings) != 1 {
		return fmt.Errorf("expert market snapshot must contain one listing")
	}
	listing := snap.Listings[0]
	data, err := validateExpertMarketSnapshotListing(&listing)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(h.expertMarketDir(), 0o755); err != nil {
		return err
	}
	tx, err := h.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var removed int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sm_expert_market_removals WHERE id=?`, listing.ID).Scan(&removed); err != nil {
		return err
	}
	if removed > 0 {
		return nil
	}
	var localStatus, localUpdatedAt string
	err = tx.QueryRowContext(ctx, `SELECT status, updated_at FROM sm_expert_market_listings WHERE id=?`, listing.ID).Scan(&localStatus, &localUpdatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if localStatus == "purged" && listing.Status != "purged" {
			return nil
		}
		if !expertMarketSnapshotReplaces(listing.UpdatedAt, localUpdatedAt) {
			return nil
		}
	}
	if err := releaseExpertMarketSourceConflict(ctx, tx, listing); err != nil {
		if errors.Is(err, errExpertSnapshotOlder) {
			return nil
		}
		return err
	}
	zipPath := ""
	var previous []byte
	previousExists := false
	if listing.Status != "purged" {
		zipPath = filepath.Join(h.expertMarketDir(), listing.ID+".zip")
		previous, err = os.ReadFile(zipPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		previousExists = err == nil
		if err := writeExpertMarketArchiveAtomic(zipPath, data); err != nil {
			return err
		}
	}
	dist := 0
	if listing.PlatformDistribution {
		dist = 1
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`, platform_distribution) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET owner_user_id=excluded.owner_user_id, owner_email=excluded.owner_email, source_expert_id=excluded.source_expert_id, name=excluded.name, description=excluded.description, icon=excluded.icon, version=excluded.version, price=excluded.price, visibility=excluded.visibility, status=excluded.status, zip_path=excluded.zip_path, package_size=excluded.package_size, download_count=MAX(sm_expert_market_listings.download_count, excluded.download_count), purchase_count=MAX(sm_expert_market_listings.purchase_count, excluded.purchase_count), sales_amount=MAX(sm_expert_market_listings.sales_amount, excluded.sales_amount), review_note=excluded.review_note, platform_distribution=excluded.platform_distribution, created_at=excluded.created_at, updated_at=excluded.updated_at`,
		listing.ID, listing.OwnerID, listing.OwnerEmail, listing.SourceExpertID, listing.Name, listing.Description, listing.Icon, listing.Version, listing.Price, listing.Visibility, listing.Status, zipPath, listing.PackageSize, listing.DownloadCount, listing.PurchaseCount, listing.SalesAmount, listing.ReviewNote, listing.CreatedAt, listing.UpdatedAt, dist)
	if err != nil {
		restoreExpertMarketArchive(zipPath, previous, previousExists)
		return err
	}
	for _, purchase := range listing.Purchases {
		_, err := tx.ExecContext(ctx, `INSERT INTO sm_expert_market_purchases (id, listing_id, buyer_user_id, buyer_email, amount_paid, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET status=excluded.status, amount_paid=excluded.amount_paid
			WHERE NOT (sm_expert_market_purchases.status='refunded' AND excluded.status<>'refunded')`, purchase.ID, listing.ID, purchase.BuyerUserID, purchase.BuyerEmail, purchase.AmountPaid, purchase.Status, purchase.CreatedAt)
		if isSnapshotUniqueConflict(err) {
			// Same buyer, different purchase id: the nodes sold this listing
			// before either snapshot arrived. Keep one entitlement.
			var result sql.Result
			result, err = tx.ExecContext(ctx, `UPDATE sm_expert_market_purchases SET status=CASE WHEN status='refunded' AND ?<>'refunded' THEN status ELSE ? END, amount_paid=?, buyer_email=? WHERE listing_id=? AND buyer_user_id=?`, purchase.Status, purchase.Status, purchase.AmountPaid, purchase.BuyerEmail, listing.ID, purchase.BuyerUserID)
			if err == nil {
				if changed, _ := result.RowsAffected(); changed == 0 {
					err = fmt.Errorf("expert market purchase %s matched no buyer", purchase.ID)
				}
			}
		}
		if err != nil {
			restoreExpertMarketArchive(zipPath, previous, previousExists)
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sm_expert_market_share_targets WHERE listing_id=?`, listing.ID); err != nil {
		restoreExpertMarketArchive(zipPath, previous, previousExists)
		return err
	}
	for _, share := range listing.ShareTargets {
		if _, err := tx.ExecContext(ctx, `INSERT INTO sm_expert_market_share_targets (listing_id, contact, user_id, created_at) VALUES (?, ?, ?, ?)`, listing.ID, share.Contact, share.UserID, share.CreatedAt); err != nil {
			restoreExpertMarketArchive(zipPath, previous, previousExists)
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		restoreExpertMarketArchive(zipPath, previous, previousExists)
		return err
	}
	if listing.Status == "purged" {
		_ = h.removeExpertMarketPackage(filepath.Join(h.expertMarketDir(), listing.ID+".zip"))
	}
	return nil
}

func validateExpertMarketSnapshotListing(listing *expertMarketSnapshotListing) ([]byte, error) {
	if listing == nil || !expertMarketIDPattern.MatchString(listing.ID) || strings.TrimSpace(listing.OwnerID) == "" || strings.TrimSpace(listing.OwnerEmail) == "" || strings.TrimSpace(listing.Name) == "" || !expertMarketIDPattern.MatchString(listing.SourceExpertID) {
		return nil, fmt.Errorf("invalid expert market listing snapshot")
	}
	switch listing.Visibility {
	case "public", "private", "users":
	default:
		return nil, fmt.Errorf("invalid expert market listing visibility")
	}
	if !expertMarketValidStatus(listing.Status) || listing.Price < 0 || listing.PackageSize < 0 || listing.DownloadCount < 0 || listing.PurchaseCount < 0 || listing.SalesAmount < 0 || !validExpertMarketSnapshotTime(listing.CreatedAt) || !validExpertMarketSnapshotTime(listing.UpdatedAt) {
		return nil, fmt.Errorf("invalid expert market listing snapshot")
	}
	if listing.Visibility == "users" {
		// Same rule as a local share: free, and no industry-distribution consent.
		listing.Price = 0
		listing.PlatformDistribution = false
	}
	for _, purchase := range listing.Purchases {
		if strings.TrimSpace(purchase.ID) == "" || purchase.ListingID != listing.ID || strings.TrimSpace(purchase.BuyerUserID) == "" || strings.TrimSpace(purchase.BuyerEmail) == "" || purchase.AmountPaid < 0 || (purchase.Status != "active" && purchase.Status != "refunded") || !validExpertMarketSnapshotTime(purchase.CreatedAt) {
			return nil, fmt.Errorf("invalid expert market purchase snapshot")
		}
	}
	normalizedShares := make([]expertMarketSnapshotShare, 0, len(listing.ShareTargets))
	seenContacts := map[string]int{}
	for _, share := range listing.ShareTargets {
		share.UserID = strings.TrimSpace(share.UserID)
		contact, ok := normalizeExpertShareContact(share.Contact)
		if !ok || len(share.UserID) > 128 || !validExpertMarketSnapshotTime(share.CreatedAt) {
			return nil, fmt.Errorf("invalid expert market share snapshot")
		}
		share.Contact = contact
		if previous, exists := seenContacts[share.Contact]; exists {
			// 138…, 86…, and 0086… are one person. Keep a single row, and
			// prefer whichever copy already names the account.
			if normalizedShares[previous].UserID == "" && share.UserID != "" {
				normalizedShares[previous].UserID = share.UserID
			}
			continue
		}
		seenContacts[share.Contact] = len(normalizedShares)
		normalizedShares = append(normalizedShares, share)
	}
	listing.ShareTargets = normalizedShares
	if listing.Status == "purged" {
		if listing.PackageSize != 0 || strings.TrimSpace(listing.ZipBase64) != "" {
			return nil, fmt.Errorf("purged expert market snapshot must not include a package")
		}
		return nil, nil
	}
	data, err := base64.StdEncoding.DecodeString(listing.ZipBase64)
	if err != nil {
		return nil, fmt.Errorf("decode expert package %s: %w", listing.ID, err)
	}
	if int64(len(data)) != listing.PackageSize || len(data) == 0 || len(data) > expertMarketMaxArchiveBytes {
		return nil, fmt.Errorf("expert package size mismatch for %s", listing.ID)
	}
	return data, nil
}

// expertMarketSnapshotReplaces reports whether incoming is as new as the row
// already stored. Comparison uses parsed timestamps: RFC3339Nano text does
// not sort chronologically once fractional seconds differ in length.
func expertMarketSnapshotReplaces(incoming, stored string) bool {
	incomingAt, incomingErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(incoming))
	storedAt, storedErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(stored))
	if incomingErr != nil {
		return false
	}
	if storedErr != nil {
		return true
	}
	return !incomingAt.Before(storedAt)
}

// releaseExpertMarketSourceConflict frees the package identity when a newer
// listing of the same expert arrives under a different row id. Returning
// errExpertSnapshotOlder tells the caller to keep the stored listing and
// acknowledge the operation, instead of failing the whole HA batch.
func releaseExpertMarketSourceConflict(ctx context.Context, tx *sql.Tx, listing expertMarketSnapshotListing) error {
	var otherID, otherUpdatedAt string
	err := tx.QueryRowContext(ctx, `SELECT id, updated_at FROM sm_expert_market_listings WHERE source_expert_id=? AND id<>?`, listing.SourceExpertID, listing.ID).Scan(&otherID, &otherUpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !expertMarketSnapshotReplaces(listing.UpdatedAt, otherUpdatedAt) {
		return errExpertSnapshotOlder
	}
	source := "replaced-" + otherID
	if !expertMarketIDPattern.MatchString(source) {
		source = otherID
	}
	// The replaced row is no longer a live share. Leaving its targets in place
	// would keep granting the package after this node has accepted a newer one.
	if _, err = tx.ExecContext(ctx, `DELETE FROM sm_expert_market_share_targets WHERE listing_id=?`, otherID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE sm_expert_market_listings SET source_expert_id=?, status=CASE WHEN status='purged' THEN status ELSE 'deleted' END, updated_at=? WHERE id=?`, source, listing.UpdatedAt, otherID)
	return err
}

func isSnapshotUniqueConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func validExpertMarketSnapshotTime(value string) bool {
	_, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	return err == nil
}

func writeExpertMarketArchiveAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".expert-market-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func restoreExpertMarketArchive(path string, previous []byte, existed bool) {
	if path == "" {
		return
	}
	if existed {
		_ = writeExpertMarketArchiveAtomic(path, previous)
		return
	}
	_ = os.Remove(path)
}
