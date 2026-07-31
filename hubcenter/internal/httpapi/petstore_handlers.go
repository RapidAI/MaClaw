package httpapi

// The pet store stays in the HTTP layer intentionally: it is a small asset
// market whose authentication and Credits semantics are shared with
// SkillMarket, while its archive rules and lifecycle are fundamentally
// different from executable skills.

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
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
	"gopkg.in/yaml.v3"
)

// petStoreSyncRecorder is implemented by ha.Service. Keeping this small
// boundary in the HTTP package avoids an import cycle while allowing the
// asset-backed Pet Store to participate in the existing HA operation stream.
type petStoreSyncRecorder interface {
	AppendPetStorePackSnapshot(context.Context, string, any)
	AppendPetStoreMetrics(context.Context, string, int64)
}

// ApplyPetStoreMetrics handles the small, high-frequency download update HA
// operation. Counts only move forward so retry/reorder cannot erase activity.
func (h *SkillMarketHandlers) ApplyPetStoreMetrics(ctx context.Context, raw json.RawMessage) error {
	h.ensurePetStoreSchema()
	var payload struct {
		ID        string `json:"id"`
		Downloads int64  `json:"downloads"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	if strings.TrimSpace(payload.ID) == "" || payload.Downloads < 0 {
		return fmt.Errorf("invalid pet store metrics")
	}
	_, err := h.store.DB().ExecContext(ctx, `UPDATE sm_pet_store_packs SET download_count=MAX(download_count, ?) WHERE id=?`, payload.Downloads, payload.ID)
	return err
}

const (
	petStoreMaxArchiveBytes = 3 << 20
	petStoreMaxFiles        = 64
	petStoreMaxFileBytes    = 512 << 10
	petStoreMaxUnpacked     = 2 << 20
	petStorePlatformFeePct  = int64(30)
)

var (
	petStoreSchemaByDB sync.Map // map[*sql.DB]*sync.Once; test/server stores are independent
	// source_pack_id links a listing back to a local desktop pack. It is an
	// optional client-provided correlation key, so validate it here rather than
	// importing the desktop GUI package into HubCenter.
	petStoreSourcePackIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)
)

type petStorePack struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Version       string `json:"version"`
	Price         int64  `json:"price"`
	Status        string `json:"status"`
	OwnerID       string `json:"-"`
	OwnerEmail    string `json:"owner"`
	SourcePackID  string `json:"source_pack_id,omitempty"`
	ZipPath       string `json:"-"`
	PackageSize   int64  `json:"package_size"`
	DownloadCount int64  `json:"download_count"`
	PurchaseCount int64  `json:"purchase_count"`
	SalesAmount   int64  `json:"sales_amount"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// petStorePublicPack is deliberately separate from the persistence record.
// It prevents future fields on petStorePack (paths, ownership IDs, moderation
// notes) from accidentally becoming part of a public browse/ranking response.
type petStorePublicPack struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Version       string `json:"version"`
	Price         int64  `json:"price"`
	PackageSize   int64  `json:"package_size"`
	DownloadCount int64  `json:"download_count"`
	PurchaseCount int64  `json:"purchase_count"`
	SalesAmount   int64  `json:"sales_amount"`
	CreatedAt     string `json:"created_at"`
}

func publicPetStorePack(p *petStorePack) petStorePublicPack {
	if p == nil {
		return petStorePublicPack{}
	}
	return petStorePublicPack{ID: p.ID, Name: p.Name, Description: p.Description, Version: p.Version, Price: p.Price, PackageSize: p.PackageSize, DownloadCount: p.DownloadCount, PurchaseCount: p.PurchaseCount, SalesAmount: p.SalesAmount, CreatedAt: p.CreatedAt}
}

// petStoreCreatorAlias is a stable public label for anonymous market ranking.
// The account email remains available only in the signed-in user's own center.
func petStoreCreatorAlias(ownerID string) string {
	digest := sha256.Sum256([]byte(ownerID))
	return fmt.Sprintf("Creator %x", digest[:4])
}

func publicPetStorePacks(packs []*petStorePack) []petStorePublicPack {
	result := make([]petStorePublicPack, 0, len(packs))
	for _, p := range packs {
		result = append(result, publicPetStorePack(p))
	}
	return result
}

type petStoreSnapshot struct {
	Packs     []petStoreSnapshotPack     `json:"packs"`
	Purchases []petStoreSnapshotPurchase `json:"purchases"`
}

type petStoreSnapshotPack struct {
	ID            string `json:"id"`
	OwnerID       string `json:"owner_id"`
	OwnerEmail    string `json:"owner_email"`
	SourcePackID  string `json:"source_pack_id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Version       string `json:"version"`
	Price         int64  `json:"price"`
	Status        string `json:"status"`
	PackageSize   int64  `json:"package_size"`
	DownloadCount int64  `json:"download_count"`
	PurchaseCount int64  `json:"purchase_count"`
	SalesAmount   int64  `json:"sales_amount"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	ZipBase64     string `json:"zip_base64"`
}

type petStoreSnapshotPurchase struct {
	ID          string `json:"id"`
	PackID      string `json:"pack_id"`
	BuyerUserID string `json:"buyer_user_id"`
	BuyerEmail  string `json:"buyer_email"`
	AmountPaid  int64  `json:"amount_paid"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
}

func validPetStoreSnapshotTime(value string) bool {
	_, err := time.Parse(time.RFC3339, value)
	return err == nil
}

// petStoreSnapshotClaimWins provides a deterministic tie-breaker for legacy
// split-brain data that predates the market-wide active source ID constraint.
// Newer listings win; an equal timestamp is broken by immutable listing ID so
// every HA peer converges on the same active pack instead of rejecting sync.
func petStoreSnapshotClaimWins(candidateID, candidateUpdatedAt, existingID, existingUpdatedAt string) bool {
	candidateAt, candidateErr := time.Parse(time.RFC3339, candidateUpdatedAt)
	existingAt, existingErr := time.Parse(time.RFC3339, existingUpdatedAt)
	if candidateErr != nil || existingErr != nil {
		return candidateID > existingID
	}
	if !candidateAt.Equal(existingAt) {
		return candidateAt.After(existingAt)
	}
	return candidateID > existingID
}

func (h *SkillMarketHandlers) ensurePetStoreSchema() {
	if h == nil || h.store == nil {
		return
	}
	db := h.store.DB()
	if db == nil {
		return
	}
	onceValue, _ := petStoreSchemaByDB.LoadOrStore(db, &sync.Once{})
	onceValue.(*sync.Once).Do(func() {
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS sm_pet_store_packs (
			id TEXT PRIMARY KEY, owner_user_id TEXT NOT NULL, owner_email TEXT NOT NULL, source_pack_id TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', version TEXT NOT NULL DEFAULT '1.0.0',
			price INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'active', zip_path TEXT NOT NULL,
			package_size INTEGER NOT NULL DEFAULT 0, download_count INTEGER NOT NULL DEFAULT 0,
			purchase_count INTEGER NOT NULL DEFAULT 0, sales_amount INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`)
		// Existing installations may have created this table before sales sorting
		// was introduced. SQLite has no ADD COLUMN IF NOT EXISTS, so a duplicate
		// column error is deliberately harmless here.
		_, _ = db.Exec(`ALTER TABLE sm_pet_store_packs ADD COLUMN sales_amount INTEGER NOT NULL DEFAULT 0`)
		_, _ = db.Exec(`ALTER TABLE sm_pet_store_packs ADD COLUMN source_pack_id TEXT NOT NULL DEFAULT ''`)
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sm_pet_store_packs_active ON sm_pet_store_packs(status, created_at DESC)`)
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sm_pet_store_packs_downloads ON sm_pet_store_packs(status, download_count DESC, id)`)
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sm_pet_store_packs_sales ON sm_pet_store_packs(status, sales_amount DESC, id)`)
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sm_pet_store_packs_creator ON sm_pet_store_packs(status, owner_user_id, owner_email)`)
		// A pre-market installation can contain legacy duplicate active listings.
		// Retain the most recently updated one (then the greatest stable ID) and
		// withdraw the rest before adding the market-wide uniqueness guard. Buyers
		// retain their permanent download entitlement to every withdrawn listing.
		_, _ = db.Exec(`UPDATE sm_pet_store_packs AS loser
			SET status='withdrawn', updated_at=?
			WHERE loser.status='active' AND loser.source_pack_id <> ''
			  AND EXISTS (
				SELECT 1 FROM sm_pet_store_packs AS winner
				WHERE winner.status='active' AND winner.source_pack_id=loser.source_pack_id
				  AND (winner.updated_at > loser.updated_at OR (winner.updated_at=loser.updated_at AND winner.id > loser.id))
			  )`, time.Now().UTC().Format(time.RFC3339))
		// One creator can have at most one active listing for a local pack. This
		// keeps the desktop's Share/Unlist state deterministic for its owner.
		_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_sm_pet_store_packs_one_active_source
			ON sm_pet_store_packs(owner_user_id, source_pack_id)
			WHERE source_pack_id <> '' AND status = 'active'`)
		// The manifest ID is the market identity, not merely an owner's local
		// label. This second partial index is the final SQLite-level guard for two
		// different accounts racing the read-before-write ownership check.
		_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_sm_pet_store_packs_one_active_market_source
			ON sm_pet_store_packs(source_pack_id)
			WHERE source_pack_id <> '' AND status = 'active'`)
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS sm_pet_store_purchases (
			id TEXT PRIMARY KEY, pack_id TEXT NOT NULL, buyer_user_id TEXT NOT NULL, buyer_email TEXT NOT NULL,
			amount_paid INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'active', created_at TEXT NOT NULL,
			UNIQUE(pack_id, buyer_user_id)
		)`)
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sm_pet_store_purchases_buyer ON sm_pet_store_purchases(buyer_user_id, status)`)
	})
}

func (h *SkillMarketHandlers) emitPetStoreSync(ctx context.Context, packID string) {
	if h == nil || h.petStoreSync == nil {
		return
	}
	packID = strings.TrimSpace(packID)
	if packID == "" {
		return
	}
	ctx = context.WithoutCancel(ctx)
	go func() {
		snap, err := h.dumpPetStorePackSnapshot(ctx, packID)
		if err == nil && len(snap.Packs) == 1 {
			h.petStoreSync.AppendPetStorePackSnapshot(ctx, packID, snap)
		}
	}()
}

// dumpPetStorePackSnapshot keeps a single HA operation under the 128 MB peer
// payload limit even as the catalog grows. A pack archive is capped at 3 MB,
// so its base64 snapshot is bounded to roughly 4 MB plus metadata.
func (h *SkillMarketHandlers) dumpPetStorePackSnapshot(ctx context.Context, packID string) (*petStoreSnapshot, error) {
	h.ensurePetStoreSchema()
	snap := &petStoreSnapshot{Packs: make([]petStoreSnapshotPack, 0), Purchases: make([]petStoreSnapshotPurchase, 0)}
	rows, err := h.store.ReadDB().QueryContext(ctx, `SELECT `+petStorePackColumns+` FROM sm_pet_store_packs WHERE id=?`, packID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanPetStorePack(rows)
		if err != nil {
			return nil, err
		}
		zipData, err := os.ReadFile(p.ZipPath)
		if err != nil {
			return nil, fmt.Errorf("read pet store archive %s: %w", p.ID, err)
		}
		snap.Packs = append(snap.Packs, petStoreSnapshotPack{ID: p.ID, OwnerID: p.OwnerID, OwnerEmail: p.OwnerEmail, SourcePackID: p.SourcePackID, Name: p.Name, Description: p.Description, Version: p.Version, Price: p.Price, Status: p.Status, PackageSize: p.PackageSize, DownloadCount: p.DownloadCount, PurchaseCount: p.PurchaseCount, SalesAmount: p.SalesAmount, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, ZipBase64: base64.StdEncoding.EncodeToString(zipData)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = h.store.ReadDB().QueryContext(ctx, `SELECT id, pack_id, buyer_user_id, buyer_email, amount_paid, status, created_at FROM sm_pet_store_purchases WHERE pack_id=? ORDER BY id`, packID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item petStoreSnapshotPurchase
		if err := rows.Scan(&item.ID, &item.PackID, &item.BuyerUserID, &item.BuyerEmail, &item.AmountPaid, &item.Status, &item.CreatedAt); err != nil {
			return nil, err
		}
		snap.Purchases = append(snap.Purchases, item)
	}
	return snap, rows.Err()
}

// SeedPetStoreHASync adds one bounded operation per existing listing when HA
// is enabled. It is safe to call during every startup: HA payload de-dup keeps
// unchanged listings quiet, while a new node can catch up after history prune.
func (h *SkillMarketHandlers) SeedPetStoreHASync(ctx context.Context) {
	if h == nil || h.petStoreSync == nil {
		return
	}
	h.ensurePetStoreSchema()
	rows, err := h.store.ReadDB().QueryContext(ctx, `SELECT id FROM sm_pet_store_packs ORDER BY id`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var packID string
		if err := rows.Scan(&packID); err == nil {
			h.emitPetStoreSync(ctx, packID)
		}
	}
}

// ApplyPetStoreSnapshot is called by HA after operation ordering/version checks.
// Files are written before the listing is committed, so a receiving node never
// exposes a listing whose archive cannot yet be served after a failover.
func (h *SkillMarketHandlers) ApplyPetStoreSnapshot(ctx context.Context, raw json.RawMessage) error {
	h.ensurePetStoreSchema()
	var snap petStoreSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return err
	}
	if len(snap.Packs) == 0 {
		return fmt.Errorf("pet store snapshot has no packs")
	}
	if err := os.MkdirAll(h.petStoreDir(), 0o755); err != nil {
		return err
	}
	// Validate every payload before touching the filesystem or database. This
	// keeps malformed HA operations from leaving partial archives behind.
	type archiveToWrite struct {
		pack petStoreSnapshotPack
		data []byte
	}
	archives := make([]archiveToWrite, 0, len(snap.Packs))
	packIDs := make(map[string]struct{}, len(snap.Packs))
	for _, p := range snap.Packs {
		if strings.TrimSpace(p.ID) == "" || !strings.HasPrefix(p.ID, "pet_") || filepath.Base(p.ID) != p.ID || strings.ContainsAny(p.ID, `\\/:`) {
			return fmt.Errorf("invalid pet store pack snapshot id")
		}
		if _, exists := packIDs[p.ID]; exists {
			return fmt.Errorf("duplicate pet store pack snapshot id")
		}
		if strings.TrimSpace(p.OwnerID) == "" || strings.TrimSpace(p.OwnerEmail) == "" || strings.TrimSpace(p.Name) == "" || (p.SourcePackID != "" && !petStoreSourcePackIDPattern.MatchString(p.SourcePackID)) || len([]rune(p.Name)) > 60 || len([]rune(p.Description)) > 1000 || len([]rune(p.Version)) > 80 || !validPetStoreSnapshotTime(p.CreatedAt) || !validPetStoreSnapshotTime(p.UpdatedAt) || p.Price < 0 || p.PackageSize < 0 || p.DownloadCount < 0 || p.PurchaseCount < 0 || p.SalesAmount < 0 || (p.Status != "active" && p.Status != "withdrawn") {
			return fmt.Errorf("invalid pet store pack snapshot")
		}
		data, err := base64.StdEncoding.DecodeString(p.ZipBase64)
		if err != nil {
			return fmt.Errorf("decode pet store archive %s: %w", p.ID, err)
		}
		if int64(len(data)) != p.PackageSize {
			return fmt.Errorf("pet store archive size mismatch for %s", p.ID)
		}
		archiveSourcePackID, err := petStoreArchiveSourcePackID(data)
		if err != nil {
			return fmt.Errorf("validate pet store archive %s: %w", p.ID, err)
		}
		if p.SourcePackID != "" && p.SourcePackID != archiveSourcePackID {
			return fmt.Errorf("pet store snapshot source pack id does not match archive for %s", p.ID)
		}
		packIDs[p.ID] = struct{}{}
		archives = append(archives, archiveToWrite{pack: p, data: data})
	}
	for _, p := range snap.Purchases {
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.PackID) == "" || strings.TrimSpace(p.BuyerUserID) == "" || strings.TrimSpace(p.BuyerEmail) == "" || !validPetStoreSnapshotTime(p.CreatedAt) || p.AmountPaid < 0 || p.Status != "active" {
			return fmt.Errorf("invalid pet store purchase snapshot")
		}
		if _, found := packIDs[p.PackID]; !found {
			return fmt.Errorf("pet store purchase references an absent pack")
		}
	}
	tx, err := h.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i := range archives {
		archive := archives[i]
		p := &archives[i].pack
		// Snapshot delivery is at-least-once and can arrive out of order. Check
		// the listing version before touching either its archive or its source-ID
		// claim. The UPSERT below already protects the database, but without this
		// guard an older snapshot could still overwrite the newer zip on disk.
		var localUpdatedAt string
		err := tx.QueryRowContext(ctx, `SELECT updated_at FROM sm_pet_store_packs WHERE id=?`, p.ID).Scan(&localUpdatedAt)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("find existing pet store pack: %w", err)
		}
		if err == nil {
			incomingAt, _ := time.Parse(time.RFC3339, p.UpdatedAt)
			localAt, localTimeErr := time.Parse(time.RFC3339, localUpdatedAt)
			if localTimeErr == nil && incomingAt.Before(localAt) {
				continue
			}
		}
		if p.Status == "active" && p.SourcePackID != "" {
			var existingID, existingUpdatedAt string
			err = tx.QueryRowContext(ctx, `SELECT id, updated_at FROM sm_pet_store_packs WHERE source_pack_id=? AND status='active' AND id<>? ORDER BY updated_at DESC, id DESC LIMIT 1`, p.SourcePackID, p.ID).Scan(&existingID, &existingUpdatedAt)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("find conflicting pet store source pack: %w", err)
			}
			if err == nil {
				if petStoreSnapshotClaimWins(p.ID, p.UpdatedAt, existingID, existingUpdatedAt) {
					if _, err := tx.ExecContext(ctx, `UPDATE sm_pet_store_packs SET status='withdrawn', updated_at=? WHERE id=? AND status='active'`, p.UpdatedAt, existingID); err != nil {
						return fmt.Errorf("withdraw conflicting pet store pack: %w", err)
					}
				} else {
					// Keep the incoming archive and entitlements for audit/downloads,
					// but make its listing non-discoverable so the market-wide unique
					// index can accept this HA operation.
					p.Status = "withdrawn"
				}
			}
		}
		pack := *p
		zipPath := filepath.Join(h.petStoreDir(), pack.ID+".zip")
		if err := os.WriteFile(zipPath, archive.data, 0o600); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO sm_pet_store_packs (`+petStorePackColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET owner_user_id=excluded.owner_user_id, owner_email=excluded.owner_email, source_pack_id=excluded.source_pack_id, name=excluded.name, description=excluded.description, version=excluded.version, price=excluded.price, status=excluded.status, zip_path=excluded.zip_path, package_size=excluded.package_size, download_count=MAX(sm_pet_store_packs.download_count, excluded.download_count), purchase_count=MAX(sm_pet_store_packs.purchase_count, excluded.purchase_count), sales_amount=MAX(sm_pet_store_packs.sales_amount, excluded.sales_amount), created_at=excluded.created_at, updated_at=excluded.updated_at WHERE excluded.updated_at >= sm_pet_store_packs.updated_at`, pack.ID, pack.OwnerID, pack.OwnerEmail, pack.SourcePackID, pack.Name, pack.Description, pack.Version, pack.Price, pack.Status, zipPath, pack.PackageSize, pack.DownloadCount, pack.PurchaseCount, pack.SalesAmount, pack.CreatedAt, pack.UpdatedAt)
		if err != nil {
			return err
		}
	}
	for _, p := range snap.Purchases {
		_, err := tx.ExecContext(ctx, `INSERT INTO sm_pet_store_purchases (id, pack_id, buyer_user_id, buyer_email, amount_paid, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET status=excluded.status`, p.ID, p.PackID, p.BuyerUserID, p.BuyerEmail, p.AmountPaid, p.Status, p.CreatedAt)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (h *SkillMarketHandlers) petStoreUser(r *http.Request) (string, string, error) {
	if h == nil || h.authSvc == nil || h.userSvc == nil {
		return "", "", fmt.Errorf("pet store authentication unavailable")
	}
	token := extractSessionToken(r)
	if token == "" {
		return "", "", fmt.Errorf("session token required")
	}
	sess, err := h.authSvc.ValidateSession(r.Context(), token)
	if err != nil {
		return "", "", fmt.Errorf("session expired or invalid")
	}
	u, err := h.userSvc.EnsureAccountWithID(r.Context(), sess.UserID, strings.TrimSpace(sess.Email))
	if err != nil {
		return "", "", err
	}
	return u.ID, u.Email, nil
}

func petStoreUniqueID(prefix string) string {
	return strings.ReplaceAll(uniqueID(prefix), "sub_", "")
}

// petStoreArchiveSourcePackID validates the archive and returns the stable ID
// declared by its own manifest. The caller must compare it with the submitted
// source_pack_id: trusting a form field alone would let an archive impersonate
// an unrelated market pack.
func petStoreArchiveSourcePackID(data []byte) (string, error) {
	if len(data) == 0 || len(data) > petStoreMaxArchiveBytes {
		return "", fmt.Errorf("pet pack archive must be between 1 byte and %d bytes", petStoreMaxArchiveBytes)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("invalid zip: %w", err)
	}
	if len(zr.File) == 0 || len(zr.File) > petStoreMaxFiles {
		return "", fmt.Errorf("pet pack must contain 1-%d files", petStoreMaxFiles)
	}
	allowed := map[string]bool{".png": true, ".webp": true, ".svg": true, ".yaml": true, ".yml": true, ".json": true, ".txt": true, ".md": true}
	var total int64
	var manifestData []byte
	for _, f := range zr.File {
		if f == nil || strings.HasSuffix(f.Name, "/") {
			continue
		}
		name := filepath.ToSlash(f.Name)
		if len(name) > 180 || strings.HasPrefix(name, "/") || strings.Contains(name, "..") || strings.Contains(name, ":") {
			return "", fmt.Errorf("unsafe archive path")
		}
		if !allowed[strings.ToLower(filepath.Ext(name))] {
			return "", fmt.Errorf("unsupported file type in pet pack")
		}
		if f.UncompressedSize64 > petStoreMaxFileBytes {
			return "", fmt.Errorf("a pet pack file exceeds %d bytes", petStoreMaxFileBytes)
		}
		total += int64(f.UncompressedSize64)
		if total > petStoreMaxUnpacked {
			return "", fmt.Errorf("pet pack exceeds %d bytes when unpacked", petStoreMaxUnpacked)
		}
		base := strings.ToLower(filepath.Base(name))
		if base == "pet-pack.yaml" || base == "pet-pack.yml" {
			if manifestData != nil {
				return "", fmt.Errorf("pet pack must contain exactly one manifest")
			}
			rc, err := f.Open()
			if err != nil {
				return "", fmt.Errorf("read pet pack manifest: %w", err)
			}
			manifestData, err = io.ReadAll(io.LimitReader(rc, petStoreMaxFileBytes+1))
			closeErr := rc.Close()
			if err != nil {
				return "", fmt.Errorf("read pet pack manifest: %w", err)
			}
			if closeErr != nil {
				return "", fmt.Errorf("close pet pack manifest: %w", closeErr)
			}
			if int64(len(manifestData)) > petStoreMaxFileBytes {
				return "", fmt.Errorf("pet pack manifest exceeds %d bytes", petStoreMaxFileBytes)
			}
		}
	}
	if manifestData == nil {
		return "", fmt.Errorf("pet pack is missing pet-pack.yaml")
	}
	var manifest struct {
		ID string `yaml:"id"`
	}
	if err := yaml.Unmarshal(manifestData, &manifest); err != nil {
		return "", fmt.Errorf("invalid pet pack manifest: %w", err)
	}
	manifest.ID = strings.TrimSpace(manifest.ID)
	if !petStoreSourcePackIDPattern.MatchString(manifest.ID) {
		return "", fmt.Errorf("invalid pet pack manifest id")
	}
	return manifest.ID, nil
}

// petStoreArchiveError is retained for HA snapshot validation callers that
// only need archive validity, not the stable local identity.
func petStoreArchiveError(data []byte) error {
	_, err := petStoreArchiveSourcePackID(data)
	return err
}

func (h *SkillMarketHandlers) petStoreDir() string { return filepath.Join(h.dataDir, "pet-store") }

func scanPetStorePack(scanner interface{ Scan(...any) error }) (*petStorePack, error) {
	p := &petStorePack{}
	err := scanner.Scan(&p.ID, &p.OwnerID, &p.OwnerEmail, &p.SourcePackID, &p.Name, &p.Description, &p.Version, &p.Price, &p.Status, &p.ZipPath, &p.PackageSize, &p.DownloadCount, &p.PurchaseCount, &p.SalesAmount, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

const petStorePackColumns = `id, owner_user_id, owner_email, source_pack_id, name, description, version, price, status, zip_path, package_size, download_count, purchase_count, sales_amount, created_at, updated_at`

func qualifiedPetStorePackColumns(alias string) string {
	columns := strings.Split(petStorePackColumns, ", ")
	for i, column := range columns {
		columns[i] = alias + "." + column
	}
	return strings.Join(columns, ", ")
}

func (h *SkillMarketHandlers) ListPetStorePacks(w http.ResponseWriter, r *http.Request) {
	h.ensurePetStoreSchema()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 20 {
		pageSize = 20
	}
	sortKey := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort")))
	sortColumn := "created_at"
	switch sortKey {
	case "downloads":
		sortColumn = "download_count"
	case "sales":
		sortColumn = "sales_amount"
	case "published", "":
		sortColumn = "created_at"
	}
	order := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("order")))
	if order != "ASC" {
		order = "DESC"
	}
	where := " WHERE status='active' AND (name LIKE ? OR description LIKE ?)"
	args := []any{"%" + q + "%", "%" + q + "%"}
	var total int
	if err := h.store.ReadDB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sm_pet_store_packs`+where, args...).Scan(&total); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows, err := h.store.ReadDB().QueryContext(r.Context(), `SELECT `+petStorePackColumns+` FROM sm_pet_store_packs`+where+` ORDER BY `+sortColumn+` `+order+`, id ASC LIMIT ? OFFSET ?`, append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	packs := make([]*petStorePack, 0)
	for rows.Next() {
		p, err := scanPetStorePack(rows)
		if err != nil {
			smError(w, http.StatusInternalServerError, err.Error())
			return
		}
		packs = append(packs, p)
	}
	if err := rows.Err(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"packs": publicPetStorePacks(packs), "total": total, "page": page, "page_size": pageSize, "total_pages": (total + pageSize - 1) / pageSize})
}

// GetPetStoreRankings provides the three fixed-size discovery rails displayed
// at the top of the embedded market. The same pack may appear in more than one
// rail; each ranking answers a distinct question for the buyer.
func (h *SkillMarketHandlers) GetPetStoreRankings(w http.ResponseWriter, r *http.Request) {
	h.ensurePetStoreSchema()
	query := func(orderBy string) ([]*petStorePack, error) {
		rows, err := h.store.ReadDB().QueryContext(r.Context(), `SELECT `+petStorePackColumns+` FROM sm_pet_store_packs WHERE status='active' ORDER BY `+orderBy+` DESC, id ASC LIMIT 10`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		packs := make([]*petStorePack, 0, 10)
		for rows.Next() {
			p, err := scanPetStorePack(rows)
			if err != nil {
				return nil, err
			}
			packs = append(packs, p)
		}
		return packs, rows.Err()
	}
	downloads, err := query("download_count")
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sales, err := query("sales_amount")
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Creator ranking is intentionally computed from active published listings:
	// no separate denormalized leaderboard is required and withdrawals vanish
	// from the chart immediately.
	rows, err := h.store.ReadDB().QueryContext(r.Context(), `SELECT owner_user_id, COUNT(*) AS pack_count, COALESCE(SUM(download_count), 0) AS downloads, COALESCE(SUM(sales_amount), 0) AS sales_amount FROM sm_pet_store_packs WHERE status='active' GROUP BY owner_user_id ORDER BY sales_amount DESC, downloads DESC, pack_count DESC, owner_user_id ASC LIMIT 10`)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	creators := make([]map[string]any, 0, 10)
	for rows.Next() {
		var ownerID string
		var packCount, creatorDownloads, creatorSales int64
		if err := rows.Scan(&ownerID, &packCount, &creatorDownloads, &creatorSales); err != nil {
			smError(w, http.StatusInternalServerError, err.Error())
			return
		}
		creators = append(creators, map[string]any{"creator": petStoreCreatorAlias(ownerID), "pack_count": packCount, "downloads": creatorDownloads, "sales_amount": creatorSales})
	}
	if err := rows.Err(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"creators": creators, "downloads": publicPetStorePacks(downloads), "sales": publicPetStorePacks(sales)})
}

// GetPetStoreAccount gives the embedded desktop market a single authenticated
// source for its account drawer. It intentionally exposes only the active
// account's public identity, Credits balance, own listings, and lifetime buys.
func (h *SkillMarketHandlers) GetPetStoreAccount(w http.ResponseWriter, r *http.Request) {
	h.ensurePetStoreSchema()
	userID, email, err := h.petStoreUser(r)
	if err != nil {
		smError(w, http.StatusUnauthorized, err.Error())
		return
	}
	balance, err := h.creditsSvc.GetBalance(r.Context(), userID)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	uploads, err := h.petStorePacksForOwner(r, userID)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// petStorePackColumns is intentionally unqualified for single-table queries.
	// This joined query must qualify the listing columns: both tables have an id,
	// and SQLite otherwise rejects the account endpoint as ambiguous.
	rows, err := h.store.ReadDB().QueryContext(r.Context(), `SELECT `+qualifiedPetStorePackColumns("p")+`, b.created_at FROM sm_pet_store_purchases b JOIN sm_pet_store_packs p ON p.id=b.pack_id WHERE b.buyer_user_id=? AND b.status='active' ORDER BY b.created_at DESC`, userID)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	purchases := make([]map[string]any, 0)
	for rows.Next() {
		p := &petStorePack{}
		var purchasedAt string
		if err := rows.Scan(&p.ID, &p.OwnerID, &p.OwnerEmail, &p.SourcePackID, &p.Name, &p.Description, &p.Version, &p.Price, &p.Status, &p.ZipPath, &p.PackageSize, &p.DownloadCount, &p.PurchaseCount, &p.SalesAmount, &p.CreatedAt, &p.UpdatedAt, &purchasedAt); err != nil {
			smError(w, http.StatusInternalServerError, err.Error())
			return
		}
		purchases = append(purchases, map[string]any{"pack": publicPetStorePack(p), "purchased_at": purchasedAt})
	}
	if err := rows.Err(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": map[string]string{"id": userID, "email": email}, "credits": balance, "uploads": uploads, "purchases": purchases})
}

func (h *SkillMarketHandlers) petStorePacksForOwner(r *http.Request, userID string) ([]*petStorePack, error) {
	rows, err := h.store.ReadDB().QueryContext(r.Context(), `SELECT `+petStorePackColumns+` FROM sm_pet_store_packs WHERE owner_user_id=? ORDER BY updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	packs := make([]*petStorePack, 0)
	for rows.Next() {
		p, err := scanPetStorePack(rows)
		if err != nil {
			return nil, err
		}
		packs = append(packs, p)
	}
	return packs, rows.Err()
}

func (h *SkillMarketHandlers) ListMyPetStorePacks(w http.ResponseWriter, r *http.Request) {
	h.ensurePetStoreSchema()
	userID, _, err := h.petStoreUser(r)
	if err != nil {
		smError(w, http.StatusUnauthorized, err.Error())
		return
	}
	packs, err := h.petStorePacksForOwner(r, userID)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"packs": packs})
}

// CanPublishPetStorePack identifies whether a local pack may be offered by
// the signed-in creator. The stable manifest ID is a marketplace identity: an
// ID already claimed by another account cannot be re-uploaded, while the same
// creator may manage (or re-publish after withdrawing) their own listing.
func (h *SkillMarketHandlers) CanPublishPetStorePack(w http.ResponseWriter, r *http.Request) {
	h.ensurePetStoreSchema()
	userID, _, err := h.petStoreUser(r)
	if err != nil {
		smError(w, http.StatusUnauthorized, err.Error())
		return
	}
	sourcePackID := strings.TrimSpace(r.PathValue("sourcePackID"))
	if !petStoreSourcePackIDPattern.MatchString(sourcePackID) {
		smError(w, http.StatusBadRequest, "invalid source pet pack id")
		return
	}
	var foreignListingID string
	err = h.store.ReadDB().QueryRowContext(r.Context(), `SELECT id FROM sm_pet_store_packs WHERE source_pack_id=? AND owner_user_id<>? AND status='active' ORDER BY updated_at DESC, id ASC LIMIT 1`, sourcePackID, userID).Scan(&foreignListingID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		smError(w, http.StatusInternalServerError, "check pet pack ownership: "+err.Error())
		return
	}
	if foreignListingID != "" {
		writeJSON(w, http.StatusOK, map[string]any{"can_publish": false, "reason": "source_pack_id_claimed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"can_publish": true})
}

func (h *SkillMarketHandlers) GetPetStorePack(w http.ResponseWriter, r *http.Request) {
	h.ensurePetStoreSchema()
	id := r.PathValue("id")
	p, err := scanPetStorePack(h.store.ReadDB().QueryRowContext(r.Context(), `SELECT `+petStorePackColumns+` FROM sm_pet_store_packs WHERE id=? AND status='active'`, id))
	if err != nil {
		smError(w, http.StatusNotFound, "pet pack not found")
		return
	}
	writeJSON(w, http.StatusOK, publicPetStorePack(p))
}

func (h *SkillMarketHandlers) SubmitPetStorePack(w http.ResponseWriter, r *http.Request) {
	h.ensurePetStoreSchema()
	userID, email, err := h.petStoreUser(r)
	if err != nil {
		smError(w, http.StatusUnauthorized, err.Error())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, petStoreMaxArchiveBytes+128<<10)
	if err := r.ParseMultipartForm(petStoreMaxArchiveBytes + 128<<10); err != nil {
		smError(w, http.StatusBadRequest, "invalid pet pack form")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	desc := strings.TrimSpace(r.FormValue("description"))
	version := strings.TrimSpace(r.FormValue("version"))
	if version == "" {
		version = "1.0.0"
	}
	price, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("price")), 10, 64)
	if err != nil || price < 0 || price > 999999 {
		smError(w, http.StatusBadRequest, "price must be 0-999999 credits")
		return
	}
	sourcePackID := strings.TrimSpace(r.FormValue("source_pack_id"))
	if !petStoreSourcePackIDPattern.MatchString(sourcePackID) {
		smError(w, http.StatusBadRequest, "invalid source pet pack id")
		return
	}
	{
		var foreignListingID string
		err := h.store.ReadDB().QueryRowContext(r.Context(), `SELECT id FROM sm_pet_store_packs WHERE source_pack_id=? AND owner_user_id<>? AND status='active' ORDER BY updated_at DESC, id ASC LIMIT 1`, sourcePackID, userID).Scan(&foreignListingID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			smError(w, http.StatusInternalServerError, "check pet pack ownership: "+err.Error())
			return
		}
		if foreignListingID != "" {
			smError(w, http.StatusConflict, "this pet pack ID is already published by another creator")
			return
		}
		var activeListings int
		if err := h.store.ReadDB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sm_pet_store_packs WHERE owner_user_id=? AND source_pack_id=? AND status='active'`, userID, sourcePackID).Scan(&activeListings); err != nil {
			smError(w, http.StatusInternalServerError, "check existing pet pack listing: "+err.Error())
			return
		}
		if activeListings > 0 {
			smError(w, http.StatusConflict, "this local pet pack is already listed; unlist it before publishing again")
			return
		}
	}
	if name == "" || len([]rune(name)) > 60 || len([]rune(desc)) > 1000 || len([]rune(version)) > 80 {
		smError(w, http.StatusBadRequest, "name or description is too long")
		return
	}
	f, _, err := r.FormFile("zip")
	if err != nil {
		smError(w, http.StatusBadRequest, "pet pack zip is required")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, petStoreMaxArchiveBytes+1))
	if err != nil {
		smError(w, http.StatusBadRequest, "cannot read pet pack")
		return
	}
	archiveSourcePackID, err := petStoreArchiveSourcePackID(data)
	if err != nil {
		smError(w, http.StatusBadRequest, err.Error())
		return
	}
	if archiveSourcePackID != sourcePackID {
		smError(w, http.StatusBadRequest, "source pet pack id does not match pet-pack.yaml")
		return
	}
	if err := os.MkdirAll(h.petStoreDir(), 0o755); err != nil {
		smError(w, http.StatusInternalServerError, "create pet store directory: "+err.Error())
		return
	}
	id := "pet_" + petStoreUniqueID("pack")
	zipPath := filepath.Join(h.petStoreDir(), id+".zip")
	if err := os.WriteFile(zipPath, data, 0o600); err != nil {
		smError(w, http.StatusInternalServerError, "save pet pack: "+err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	p := &petStorePack{ID: id, OwnerID: userID, OwnerEmail: email, SourcePackID: sourcePackID, Name: name, Description: desc, Version: version, Price: price, Status: "active", ZipPath: zipPath, PackageSize: int64(len(data)), CreatedAt: now, UpdatedAt: now}
	_, err = h.store.DB().ExecContext(r.Context(), `INSERT INTO sm_pet_store_packs (`+petStorePackColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0, 0, ?, ?)`, p.ID, p.OwnerID, p.OwnerEmail, p.SourcePackID, p.Name, p.Description, p.Version, p.Price, p.Status, p.ZipPath, p.PackageSize, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		_ = os.Remove(zipPath)
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
			smError(w, http.StatusConflict, "this pet pack ID is already published by another creator")
			return
		}
		smError(w, http.StatusInternalServerError, "create listing: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, p)
	h.emitPetStoreSync(r.Context(), p.ID)
}

func (h *SkillMarketHandlers) PurchasePetStorePack(w http.ResponseWriter, r *http.Request) {
	h.ensurePetStoreSchema()
	buyerID, buyerEmail, err := h.petStoreUser(r)
	if err != nil {
		smError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id := r.PathValue("id")
	// CompletePetStorePurchase uses one SQLite IMMEDIATE transaction. The
	// database unique constraint is therefore the concurrency guard: a repeated
	// click races harmlessly and its entire transaction rolls back, rather than
	// serializing purchases for every unrelated buyer across the whole process.
	p, err := scanPetStorePack(h.store.ReadDB().QueryRowContext(r.Context(), `SELECT `+petStorePackColumns+` FROM sm_pet_store_packs WHERE id=? AND status='active'`, id))
	if err != nil {
		smError(w, http.StatusNotFound, "pet pack not found")
		return
	}
	if p.OwnerID == buyerID {
		smError(w, http.StatusBadRequest, "you already own this pet pack")
		return
	}
	var count int
	_ = h.store.ReadDB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sm_pet_store_purchases WHERE pack_id=? AND buyer_user_id=? AND status='active'`, id, buyerID).Scan(&count)
	if count > 0 {
		writeJSON(w, http.StatusOK, map[string]any{"status": "owned", "download_url": "/api/v1/pet-store/packs/" + id + "/download"})
		return
	}
	// Debit, seller credit, platform fee, lifetime entitlement, and the listing
	// counters must commit together. The older multi-transaction sequence could
	// charge a buyer while a later entitlement write failed.
	purchaseID := "pet_" + petStoreUniqueID("purchase")
	entitlementID := "pet_" + petStoreUniqueID("entitlement")
	if h.creditsSvc == nil {
		smError(w, http.StatusServiceUnavailable, "credits service unavailable")
		return
	}
	if err := h.creditsSvc.CompletePetStorePurchase(r.Context(), buyerID, buyerEmail, p.OwnerID, id, entitlementID, purchaseID, p.Price); err != nil {
		if errors.Is(err, skillmarket.ErrPetStoreAlreadyOwned) {
			writeJSON(w, http.StatusOK, map[string]any{"status": "owned", "download_url": "/api/v1/pet-store/packs/" + id + "/download"})
			return
		}
		if errors.Is(err, skillmarket.ErrPetStoreUnavailable) {
			smError(w, http.StatusConflict, "pet pack is no longer available")
			return
		}
		if strings.Contains(err.Error(), "insufficient") {
			smError(w, http.StatusPaymentRequired, fmt.Sprintf("insufficient credits: need %d", p.Price))
		} else {
			smError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "owned", "amount_paid": p.Price, "download_url": "/api/v1/pet-store/packs/" + id + "/download"})
	h.emitPetStoreSync(r.Context(), id)
}

func (h *SkillMarketHandlers) DownloadPetStorePack(w http.ResponseWriter, r *http.Request) {
	h.ensurePetStoreSchema()
	userID, _, err := h.petStoreUser(r)
	if err != nil {
		smError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id := r.PathValue("id")
	p, err := scanPetStorePack(h.store.ReadDB().QueryRowContext(r.Context(), `SELECT `+petStorePackColumns+` FROM sm_pet_store_packs WHERE id=?`, id))
	if err != nil {
		smError(w, http.StatusNotFound, "pet pack not found")
		return
	}
	if userID != p.OwnerID {
		var n int
		_ = h.store.ReadDB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sm_pet_store_purchases WHERE pack_id=? AND buyer_user_id=? AND status='active'`, id, userID).Scan(&n)
		if n == 0 {
			smError(w, http.StatusForbidden, "purchase required")
			return
		}
	}
	if _, err := os.Stat(p.ZipPath); err != nil {
		smError(w, http.StatusGone, "pet pack file is unavailable")
		return
	}
	_, _ = h.store.DB().ExecContext(r.Context(), `UPDATE sm_pet_store_packs SET download_count=download_count+1 WHERE id=?`, id)
	// The listing itself is immutable for a download. Replicate only the counter
	// rather than its base64 archive so popular packages do not amplify HA I/O.
	if h.petStoreSync != nil {
		var downloads int64
		if err := h.store.DB().QueryRowContext(r.Context(), `SELECT download_count FROM sm_pet_store_packs WHERE id=?`, id).Scan(&downloads); err == nil {
			h.petStoreSync.AppendPetStoreMetrics(context.WithoutCancel(r.Context()), id, downloads)
		}
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(p.ID, "\"", "")+`.zip"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, p.ZipPath)
}

func (h *SkillMarketHandlers) WithdrawPetStorePack(w http.ResponseWriter, r *http.Request) {
	h.ensurePetStoreSchema()
	userID, _, err := h.petStoreUser(r)
	if err != nil {
		smError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id := r.PathValue("id")
	res, err := h.store.DB().ExecContext(r.Context(), `UPDATE sm_pet_store_packs SET status='withdrawn', updated_at=? WHERE id=? AND owner_user_id=? AND status='active'`, time.Now().UTC().Format(time.RFC3339), id, userID)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		smError(w, http.StatusNotFound, "active pet pack not found or not owned")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "withdrawn"})
	h.emitPetStoreSync(r.Context(), id)
}
