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
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/gui/petpack"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
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
	// petStorePreviewCacheMaxEntries caps the preview cache so a growing pack
	// inventory cannot turn it into an unbounded memory sink.
	petStorePreviewCacheMaxEntries = 256
)

var (
	petStoreSchemaByDB   sync.Map // map[*sql.DB]*sync.Once; test/server stores are independent
	petStorePreviewCache sync.Map // map[string]petStorePreviewCacheEntry; keyed by archive path
	// petStorePreviewCacheSize tracks the entry count so the cache can be
	// cleared once it exceeds petStorePreviewCacheMaxEntries.
	petStorePreviewCacheSize atomic.Int64
	// source_pack_id links a listing back to a local desktop pack. It is an
	// optional client-provided correlation key, so validate it here rather than
	// importing the desktop GUI package into HubCenter.
	petStoreSourcePackIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)
)

type petStorePreviewCacheEntry struct {
	modTime time.Time
	size    int64
	dataURL string
}

// petStorePreviewCacheStore inserts or replaces a preview entry while keeping
// the cache bounded. Eviction is deliberately crude: once the entry count
// passes petStorePreviewCacheMaxEntries the whole map is dropped and rebuilt
// lazily. Previews are cheap to regenerate (a few ms of image work), so a
// mutex-protected LRU would add bookkeeping without a real payoff here.
func petStorePreviewCacheStore(zipPath string, entry petStorePreviewCacheEntry) {
	if _, loaded := petStorePreviewCache.LoadOrStore(zipPath, entry); loaded {
		petStorePreviewCache.Store(zipPath, entry)
		return
	}
	if petStorePreviewCacheSize.Add(1) <= petStorePreviewCacheMaxEntries {
		return
	}
	// Full: rebuild from scratch with just this entry.
	petStorePreviewCache = sync.Map{}
	petStorePreviewCacheSize.Store(1)
	petStorePreviewCache.Store(zipPath, entry)
}

// resetPetStorePreviewCache clears the preview cache; used by tests that need
// a clean slate.
func resetPetStorePreviewCache() {
	petStorePreviewCache = sync.Map{}
	petStorePreviewCacheSize.Store(0)
}

type petStorePack struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Version       string `json:"version"`
	Price         int64  `json:"price"`
	Status        string `json:"status"`
	OwnerID       string `json:"-"`
	OwnerEmail    string `json:"-"`
	SourcePackID  string `json:"source_pack_id,omitempty"`
	ZipPath       string `json:"-"`
	PackageSize   int64  `json:"package_size"`
	DownloadCount int64  `json:"download_count"`
	PurchaseCount int64  `json:"purchase_count"`
	SalesAmount   int64  `json:"sales_amount"`
	// PreviewDataURL is populated only for listing responses. It contains a
	// bounded, re-encoded image generated from the package's declared cover or
	// idle frame, never a URL to the installable archive.
	PreviewDataURL string `json:"preview_data_url,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// petStorePublicPack is deliberately separate from the persistence record.
// It prevents future fields on petStorePack (paths, ownership IDs, moderation
// notes) from accidentally becoming part of a public browse/ranking response.
type petStorePublicPack struct {
	ID string `json:"id"`
	// SourcePackID is the stable manifest ID. Desktop clients compare it with
	// their local registry to distinguish an already installed market pack from
	// an entitlement that is merely owned.
	SourcePackID  string `json:"source_pack_id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Version       string `json:"version"`
	Price         int64  `json:"price"`
	PackageSize   int64  `json:"package_size"`
	DownloadCount int64  `json:"download_count"`
	PurchaseCount int64  `json:"purchase_count"`
	SalesAmount   int64  `json:"sales_amount"`
	CreatedAt     string `json:"created_at"`
	// PreviewDataURL is a small, sanitized JPEG thumbnail made from the pack's
	// own raster assets. It lets buyers recognize the pet without exposing the
	// installable archive or an asset download URL.
	PreviewDataURL string `json:"preview_data_url,omitempty"`
}

// petStoreAdminPack is intentionally separate from both the persistence record
// and the public listing payload. Moderators need a traceable publisher email,
// while market clients must never receive that personal identifier.
type petStoreAdminPack struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Version        string `json:"version"`
	Price          int64  `json:"price"`
	Status         string `json:"status"`
	OwnerEmail     string `json:"owner_email"`
	SourcePackID   string `json:"source_pack_id,omitempty"`
	PackageSize    int64  `json:"package_size"`
	DownloadCount  int64  `json:"download_count"`
	PurchaseCount  int64  `json:"purchase_count"`
	SalesAmount    int64  `json:"sales_amount"`
	PreviewDataURL string `json:"preview_data_url,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

func adminPetStorePack(p *petStorePack) petStoreAdminPack {
	if p == nil {
		return petStoreAdminPack{}
	}
	return petStoreAdminPack{
		ID:             p.ID,
		Name:           p.Name,
		Description:    p.Description,
		Version:        p.Version,
		Price:          p.Price,
		Status:         p.Status,
		OwnerEmail:     p.OwnerEmail,
		SourcePackID:   p.SourcePackID,
		PackageSize:    p.PackageSize,
		DownloadCount:  p.DownloadCount,
		PurchaseCount:  p.PurchaseCount,
		SalesAmount:    p.SalesAmount,
		PreviewDataURL: p.PreviewDataURL,
		CreatedAt:      p.CreatedAt,
		UpdatedAt:      p.UpdatedAt,
	}
}

func publicPetStorePack(p *petStorePack) petStorePublicPack {
	if p == nil {
		return petStorePublicPack{}
	}
	return petStorePublicPack{ID: p.ID, SourcePackID: p.SourcePackID, Name: p.Name, Description: p.Description, Version: p.Version, Price: p.Price, PackageSize: p.PackageSize, DownloadCount: p.DownloadCount, PurchaseCount: p.PurchaseCount, SalesAmount: p.SalesAmount, CreatedAt: p.CreatedAt, PreviewDataURL: petStorePackPreviewDataURL(p.ZipPath)}
}

// petStorePackPreviewDataURL selects the first valid raster asset from a
// published archive and re-encodes it as a bounded thumbnail. Invalid or
// image-less archives simply fall back to the client-side monogram.
func petStorePackPreviewDataURL(zipPath string) string {
	info, err := os.Stat(zipPath)
	if err != nil || info.IsDir() || info.Size() <= 0 || info.Size() > petStoreMaxArchiveBytes {
		return ""
	}
	if cached, ok := petStorePreviewCache.Load(zipPath); ok {
		entry := cached.(petStorePreviewCacheEntry)
		if entry.size == info.Size() && entry.modTime.Equal(info.ModTime()) {
			return entry.dataURL
		}
	}
	data, err := os.ReadFile(zipPath)
	if err != nil || len(data) == 0 || int64(len(data)) > petStoreMaxArchiveBytes {
		return ""
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ""
	}
	preferred := petStorePreviewCandidatePaths(zr)
	for _, name := range preferred {
		f := petStoreZipFile(zr, name)
		if f == nil {
			continue
		}
		if f.FileInfo().IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.Name))
		if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".webp" {
			continue
		}
		if f.UncompressedSize64 == 0 || f.UncompressedSize64 > petStoreMaxFileBytes {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		img, _, decodeErr := image.Decode(io.LimitReader(rc, petStoreMaxFileBytes+1))
		_ = rc.Close()
		if decodeErr != nil || img == nil {
			continue
		}
		bounds := img.Bounds()
		if bounds.Dx() <= 0 || bounds.Dy() <= 0 || int64(bounds.Dx())*int64(bounds.Dy()) > 16_000_000 {
			continue
		}
		// Keep the original aspect ratio. Most pet assets have transparent
		// margins, so compose them onto a neutral background before JPEG encoding
		// rather than turning those pixels black or stretching the character.
		thumbBounds := image.Rect(0, 0, 192, 112)
		thumb := image.NewRGBA(thumbBounds)
		for y := thumbBounds.Min.Y; y < thumbBounds.Max.Y; y++ {
			for x := thumbBounds.Min.X; x < thumbBounds.Max.X; x++ {
				thumb.SetRGBA(x, y, color.RGBA{R: 241, G: 245, B: 249, A: 255})
			}
		}
		scale := min(float64(thumbBounds.Dx())/float64(bounds.Dx()), float64(thumbBounds.Dy())/float64(bounds.Dy()))
		width, height := max(1, int(float64(bounds.Dx())*scale)), max(1, int(float64(bounds.Dy())*scale))
		destination := image.Rect((thumbBounds.Dx()-width)/2, (thumbBounds.Dy()-height)/2, (thumbBounds.Dx()-width)/2+width, (thumbBounds.Dy()-height)/2+height)
		xdraw.CatmullRom.Scale(thumb, destination, img, bounds, xdraw.Over, nil)
		var out bytes.Buffer
		if jpeg.Encode(&out, thumb, &jpeg.Options{Quality: 76}) != nil || out.Len() == 0 || out.Len() > 48<<10 {
			continue
		}
		dataURL := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(out.Bytes())
		petStorePreviewCacheStore(zipPath, petStorePreviewCacheEntry{modTime: info.ModTime(), size: info.Size(), dataURL: dataURL})
		return dataURL
	}
	petStorePreviewCacheStore(zipPath, petStorePreviewCacheEntry{modTime: info.ModTime(), size: info.Size()})
	return ""
}

// petStorePreviewCandidatePaths follows the same preference as the desktop
// renderer: an explicit preview first, then the active native idle frame. This
// prevents arbitrary texture ordering inside a ZIP from becoming a listing's
// public cover image.
func petStorePreviewCandidatePaths(zr *zip.Reader) []string {
	if zr == nil {
		return nil
	}
	var manifestData []byte
	for _, f := range zr.File {
		if f == nil || f.FileInfo().IsDir() || strings.ToLower(filepath.Base(f.Name)) != "pet-pack.yaml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			break
		}
		manifestData, err = io.ReadAll(io.LimitReader(rc, petStoreMaxFileBytes+1))
		_ = rc.Close()
		if err != nil || int64(len(manifestData)) > petStoreMaxFileBytes {
			manifestData = nil
		}
		break
	}
	if len(manifestData) > 0 {
		var manifest petpack.PetPackManifest
		if yaml.Unmarshal(manifestData, &manifest) == nil {
			seen := map[string]bool{}
			paths := make([]string, 0, 3)
			add := func(path string) {
				path = filepath.ToSlash(strings.TrimSpace(path))
				if path == "" || seen[path] {
					return
				}
				seen[path] = true
				paths = append(paths, path)
			}
			add(manifest.Preview)
			add(manifest.Assets.Preview)
			add(petpack.DefaultNativeAssets(&manifest)["idle"])
			if len(paths) > 0 {
				return paths
			}
		}
	}
	// Legacy accepted archives always contain an idle native asset. This
	// fallback retains compatibility if an older manifest cannot be decoded.
	for _, f := range zr.File {
		if f != nil && !f.FileInfo().IsDir() && strings.EqualFold(filepath.Base(f.Name), "idle.png") {
			return []string{filepath.ToSlash(f.Name)}
		}
	}
	return nil
}

func petStoreZipFile(zr *zip.Reader, want string) *zip.File {
	want = filepath.ToSlash(strings.TrimSpace(want))
	for _, f := range zr.File {
		if f != nil && filepath.ToSlash(f.Name) == want {
			return f
		}
	}
	return nil
}

// petStoreAccountPack adds lifecycle state only to the buyer's authenticated
// purchase history. Browse and ranking APIs deliberately remain status-free.
type petStoreAccountPack struct {
	petStorePublicPack
	Status string `json:"status"`
}

func accountPetStorePack(p *petStorePack) petStoreAccountPack {
	return petStoreAccountPack{petStorePublicPack: publicPetStorePack(p), Status: p.Status}
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
	_, err := time.Parse(time.RFC3339Nano, value)
	return err == nil
}

// petStoreSnapshotClaimWins provides a deterministic tie-breaker for legacy
// split-brain data that predates the market-wide active source ID constraint.
// Newer listings win; an equal timestamp is broken by immutable listing ID so
// every HA peer converges on the same active pack instead of rejecting sync.
func petStoreSnapshotClaimWins(candidateID, candidateUpdatedAt, existingID, existingUpdatedAt string) bool {
	candidateAt, candidateErr := time.Parse(time.RFC3339Nano, candidateUpdatedAt)
	existingAt, existingErr := time.Parse(time.RFC3339Nano, existingUpdatedAt)
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
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sm_pet_store_packs_owner_id ON sm_pet_store_packs(owner_user_id, id)`)
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
			  )`, petStoreNow())
		// A paused listing is merely hidden, not relinquished. It must keep the
		// active-listing slot so an administrator can always restore it without a
		// later publication taking that slot. A withdrawn listing releases only the
		// active-listing slot; listing records retain their source identity forever.
		// This prevents a deleted pack's existing buyers from being granted an
		// unrelated new creator's pack merely because the manifest ID was reused.
		_, _ = db.Exec(`DROP INDEX IF EXISTS idx_sm_pet_store_packs_one_active_source`)
		_, _ = db.Exec(`DROP INDEX IF EXISTS idx_sm_pet_store_packs_one_active_market_source`)
		// A prior version could have allowed one active and one paused listing with
		// the same source ID. Keep the currently active listing (or newest paused
		// listing where no active one exists) and withdraw the stale paused claim
		// before installing the stronger uniqueness rule.
		_, _ = db.Exec(`UPDATE sm_pet_store_packs AS loser
			SET status='withdrawn', updated_at=?
			WHERE loser.status='paused' AND loser.source_pack_id <> ''
			  AND EXISTS (
				SELECT 1 FROM sm_pet_store_packs AS winner
				WHERE winner.source_pack_id=loser.source_pack_id
				  AND winner.status IN ('active','paused')
				  AND (winner.status='active' OR winner.updated_at > loser.updated_at OR (winner.updated_at=loser.updated_at AND winner.id > loser.id))
			  )`, petStoreNow())
		_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_sm_pet_store_packs_one_active_source
			ON sm_pet_store_packs(owner_user_id, source_pack_id)
			WHERE source_pack_id <> '' AND status IN ('active','paused')`)
		// The manifest ID is the market identity, not merely an owner's local
		// label. This second partial index is the final SQLite-level guard for two
		// different accounts racing the read-before-write ownership check.
		_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_sm_pet_store_packs_one_active_market_source
			ON sm_pet_store_packs(source_pack_id)
			WHERE source_pack_id <> '' AND status IN ('active','paused')`)
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS sm_pet_store_purchases (
			id TEXT PRIMARY KEY, pack_id TEXT NOT NULL, buyer_user_id TEXT NOT NULL, buyer_email TEXT NOT NULL,
			amount_paid INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'active', created_at TEXT NOT NULL,
			UNIQUE(pack_id, buyer_user_id)
		)`)
		// Buyer account, purchase, and download authorization begin with a buyer's
		// active entitlements and then join to their listings. Including pack_id
		// avoids an extra table lookup for those hot paths as the ledger grows.
		// Recreate the earlier two-column version so existing installations receive
		// the covering index as well.
		_, _ = db.Exec(`DROP INDEX IF EXISTS idx_sm_pet_store_purchases_buyer`)
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sm_pet_store_purchases_buyer ON sm_pet_store_purchases(buyer_user_id, status, pack_id)`)
		// Creator reports start from a creator's listings and then constrain the
		// purchase ledger by listing, status, and period. The buyer-facing index
		// above cannot serve that access path once the ledger becomes large.
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sm_pet_store_purchases_pack_report ON sm_pet_store_purchases(pack_id, status, created_at)`)
		// Cumulative download_count is useful for catalogue sorting but cannot
		// answer a date-bounded creator report. Keep a small immutable event row
		// for each successful archive delivery instead.
		_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS sm_pet_store_downloads (
			id TEXT PRIMARY KEY, pack_id TEXT NOT NULL, downloader_user_id TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`)
		_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sm_pet_store_downloads_pack_time ON sm_pet_store_downloads(pack_id, created_at)`)
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
		if err != nil {
			log.Printf("[petstore] WARN: dump sync snapshot for %s failed: %v", packID, err)
			return
		}
		if len(snap.Packs) == 1 {
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
		// A purged listing remains as a tiny terminal HA tombstone. It deliberately
		// carries no archive, so delayed active snapshots cannot resurrect either
		// the listing or its package file on a peer.
		if p.Status == "purged" {
			snap.Packs = append(snap.Packs, petStoreSnapshotPack{ID: p.ID, OwnerID: p.OwnerID, OwnerEmail: p.OwnerEmail, SourcePackID: p.SourcePackID, Name: p.Name, Description: p.Description, Version: p.Version, Price: p.Price, Status: p.Status, PackageSize: 0, DownloadCount: p.DownloadCount, PurchaseCount: p.PurchaseCount, SalesAmount: p.SalesAmount, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt})
			continue
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
func (h *SkillMarketHandlers) ApplyPetStoreSnapshot(ctx context.Context, raw json.RawMessage) (err error) {
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
	type archiveBackup struct {
		path   string
		data   []byte
		exists bool
	}
	archiveBackups := make([]archiveBackup, 0, len(snap.Packs))
	committed := false
	defer func() {
		if committed {
			return
		}
		// Archives are installed before their rows commit so a successful row
		// never points at a missing file. If that transaction aborts, restore the
		// prior files (or remove newly-created ones) to keep disk and DB aligned.
		for i := len(archiveBackups) - 1; i >= 0; i-- {
			backup := archiveBackups[i]
			if backup.exists {
				_ = writePetStoreArchiveAtomic(backup.path, backup.data)
			} else {
				_ = os.Remove(backup.path)
			}
		}
	}()
	packIDs := make(map[string]struct{}, len(snap.Packs))
	for _, p := range snap.Packs {
		if strings.TrimSpace(p.ID) == "" || !strings.HasPrefix(p.ID, "pet_") || filepath.Base(p.ID) != p.ID || strings.ContainsAny(p.ID, `\\/:`) {
			return fmt.Errorf("invalid pet store pack snapshot id")
		}
		if _, exists := packIDs[p.ID]; exists {
			return fmt.Errorf("duplicate pet store pack snapshot id")
		}
		if strings.TrimSpace(p.OwnerID) == "" || strings.TrimSpace(p.OwnerEmail) == "" || strings.TrimSpace(p.Name) == "" || (p.SourcePackID != "" && !petStoreSourcePackIDPattern.MatchString(p.SourcePackID)) || len([]rune(p.Name)) > 60 || len([]rune(p.Description)) > 1000 || len([]rune(p.Version)) > 80 || !validPetStoreSnapshotTime(p.CreatedAt) || !validPetStoreSnapshotTime(p.UpdatedAt) || p.Price < 0 || p.PackageSize < 0 || p.DownloadCount < 0 || p.PurchaseCount < 0 || p.SalesAmount < 0 || !validPetStorePackStatus(p.Status) {
			return fmt.Errorf("invalid pet store pack snapshot")
		}
		data := []byte(nil)
		var err error
		if p.Status == "purged" {
			if p.PackageSize != 0 || strings.TrimSpace(p.ZipBase64) != "" {
				return fmt.Errorf("purged pet store snapshot must not include an archive")
			}
		} else {
			data, err = base64.StdEncoding.DecodeString(p.ZipBase64)
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
		var localStatus, localUpdatedAt string
		err := tx.QueryRowContext(ctx, `SELECT status, updated_at FROM sm_pet_store_packs WHERE id=?`, p.ID).Scan(&localStatus, &localUpdatedAt)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("find existing pet store pack: %w", err)
		}
		if err == nil {
			// A purge is terminal. There is no supported workflow that can revive a
			// purged listing, so even a clock-skewed active peer snapshot must not
			// recreate its package file or make the listing visible again.
			if localStatus == "purged" && p.Status != "purged" {
				continue
			}
			incomingAt, incomingTimeErr := time.Parse(time.RFC3339Nano, p.UpdatedAt)
			localAt, localTimeErr := time.Parse(time.RFC3339Nano, localUpdatedAt)
			if incomingTimeErr == nil && localTimeErr == nil && incomingAt.Before(localAt) {
				continue
			}
		}
		if petStorePackClaimsSourceID(p.Status) && p.SourcePackID != "" {
			var existingID, existingUpdatedAt string
			err = tx.QueryRowContext(ctx, `SELECT id, updated_at FROM sm_pet_store_packs WHERE source_pack_id=? AND status IN ('active','paused') AND id<>? ORDER BY updated_at DESC, id DESC LIMIT 1`, p.SourcePackID, p.ID).Scan(&existingID, &existingUpdatedAt)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("find conflicting pet store source pack: %w", err)
			}
			if err == nil {
				if petStoreSnapshotClaimWins(p.ID, p.UpdatedAt, existingID, existingUpdatedAt) {
					if _, err := tx.ExecContext(ctx, `UPDATE sm_pet_store_packs SET status='withdrawn', updated_at=? WHERE id=? AND status IN ('active','paused')`, p.UpdatedAt, existingID); err != nil {
						return fmt.Errorf("withdraw conflicting pet store pack: %w", err)
					}
				} else {
					// Keep the incoming archive and entitlements for audit, but make
					// its listing non-discoverable. Paused listings retain the same
					// source-ID claim as active listings, so this is also required for
					// the stronger active-or-paused uniqueness index.
					p.Status = "withdrawn"
				}
			}
		}
		pack := *p
		zipPath := ""
		if pack.Status != "purged" {
			zipPath = filepath.Join(h.petStoreDir(), pack.ID+".zip")
			oldData, readErr := os.ReadFile(zipPath)
			if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
				return fmt.Errorf("backup pet store archive %s: %w", pack.ID, readErr)
			}
			archiveBackups = append(archiveBackups, archiveBackup{path: zipPath, data: oldData, exists: readErr == nil})
			if err := writePetStoreArchiveAtomic(zipPath, archive.data); err != nil {
				return err
			}
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
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	for _, archive := range archives {
		if archive.pack.Status != "purged" {
			continue
		}
		// The conventional path is deterministic. Ignore an absent archive: the
		// state is already converged, and retrying this HA operation is harmless.
		if err := h.removePetStorePackArchive(archive.pack.ID); err != nil {
			return fmt.Errorf("remove purged pet store archive %s: %w", archive.pack.ID, err)
		}
	}
	return nil
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
	// A market listing is a traceable commercial record. Do not create an
	// account with an empty contact value from an incomplete machine session:
	// that would leave moderation without a usable publisher identity.
	if strings.TrimSpace(sess.UserID) == "" || strings.TrimSpace(sess.Email) == "" {
		return "", "", fmt.Errorf("publisher email is required")
	}
	// The authenticated Hub user ID is the account identity. Email/phone are
	// login contacts and may change or coexist for the same person, so never
	// resolve market ownership through the session's contact value.
	u, err := h.userSvc.EnsureAccountWithID(r.Context(), sess.UserID, strings.TrimSpace(sess.Email))
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(u.ID) == "" || u.ID != strings.TrimSpace(sess.UserID) || strings.TrimSpace(u.Email) == "" {
		return "", "", fmt.Errorf("publisher email is required")
	}
	return u.ID, u.Email, nil
}

func petStoreUniqueID(prefix string) string {
	return uniqueID(prefix)
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
	seenPaths := make(map[string]bool, len(zr.File))
	for _, f := range zr.File {
		if f == nil || strings.HasSuffix(f.Name, "/") {
			continue
		}
		name := filepath.ToSlash(f.Name)
		if len(name) > 180 || strings.HasPrefix(name, "/") || strings.Contains(name, "..") || strings.Contains(name, ":") {
			return "", fmt.Errorf("unsafe archive path")
		}
		clean := filepath.ToSlash(filepath.Clean(name))
		if clean == "." || strings.HasPrefix(clean, "../") || seenPaths[clean] {
			return "", fmt.Errorf("duplicate or unsafe archive path")
		}
		seenPaths[clean] = true
		name = clean
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
	var manifest petpack.PetPackManifest
	if err := yaml.Unmarshal(manifestData, &manifest); err != nil {
		return "", fmt.Errorf("invalid pet pack manifest: %w", err)
	}
	if err := petpack.ValidateManifest(&manifest); err != nil {
		return "", fmt.Errorf("invalid pet pack manifest: %w", err)
	}
	if renderer, idlePath := petpack.DefaultPresentation(&manifest); renderer != petpack.RendererProcedural {
		if err := petStoreValidateStaticFrameArchive(zr, idlePath); err != nil {
			return "", err
		}
	}
	for _, rigAssets := range petpack.SkeletonRigAssets(&manifest) {
		if err := petStoreValidateRigArchive(zr, rigAssets); err != nil {
			return "", err
		}
	}
	for _, presentation := range petpack.CharacterPresentations(&manifest) {
		if err := petStoreValidateCharacterArchive(zr, presentation.Character, presentation.Rig); err != nil {
			return "", err
		}
	}
	manifest.ID = strings.TrimSpace(manifest.ID)
	if !petStoreSourcePackIDPattern.MatchString(manifest.ID) {
		return "", fmt.Errorf("invalid pet pack manifest id")
	}
	return manifest.ID, nil
}

func petStoreValidateStaticFrameArchive(zr *zip.Reader, path string) error {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if zr == nil || path == "" {
		return fmt.Errorf("native idle fallback is missing")
	}
	for _, file := range zr.File {
		if file == nil || filepath.ToSlash(file.Name) != path {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return fmt.Errorf("read native idle fallback: %w", err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(rc, petStoreMaxFileBytes+1))
		closeErr := rc.Close()
		if readErr != nil || closeErr != nil {
			return fmt.Errorf("read native idle fallback: %s", path)
		}
		if err := petpack.ValidateStaticFrameData(raw); err != nil {
			return fmt.Errorf("invalid native idle fallback: %w", err)
		}
		return nil
	}
	return fmt.Errorf("native idle fallback is missing: %s", path)
}

func petStoreValidateRigArchive(zr *zip.Reader, assets *petpack.PetPackRigAssets) error {
	if zr == nil || assets == nil {
		return fmt.Errorf("native-skeleton pack is missing rig assets")
	}
	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		if f != nil && !strings.HasSuffix(f.Name, "/") {
			files[filepath.ToSlash(f.Name)] = f
		}
	}
	definition := filepath.ToSlash(strings.TrimSpace(assets.Definition))
	f := files[definition]
	if f == nil {
		return fmt.Errorf("pet-rig definition is missing: %s", definition)
	}
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("read pet-rig definition: %w", err)
	}
	raw, readErr := io.ReadAll(io.LimitReader(rc, petStoreMaxFileBytes+1))
	closeErr := rc.Close()
	if readErr != nil {
		return fmt.Errorf("read pet-rig definition: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close pet-rig definition: %w", closeErr)
	}
	if int64(len(raw)) > petStoreMaxFileBytes {
		return fmt.Errorf("pet-rig definition exceeds %d bytes", petStoreMaxFileBytes)
	}
	var rig petpack.Rig
	if err := json.Unmarshal(raw, &rig); err != nil {
		return fmt.Errorf("invalid pet-rig definition: %w", err)
	}
	if err := petpack.ValidateRig(&rig, assets); err != nil {
		return fmt.Errorf("invalid pet-rig definition: %w", err)
	}
	textureData := make(map[string][]byte, len(assets.Textures))
	for _, texture := range assets.Textures {
		texture = filepath.ToSlash(strings.TrimSpace(texture))
		textureFile := files[texture]
		if textureFile == nil {
			return fmt.Errorf("pet-rig texture is missing: %s", texture)
		}
		rc, err := textureFile.Open()
		if err != nil {
			return fmt.Errorf("read pet-rig texture %s: %w", texture, err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(rc, petStoreMaxFileBytes+1))
		closeErr := rc.Close()
		if readErr != nil || closeErr != nil || int64(len(raw)) > petStoreMaxFileBytes {
			return fmt.Errorf("read pet-rig texture: %s", texture)
		}
		textureData[texture] = raw
	}
	if err := petpack.ValidateRigTextureData(assets, textureData); err != nil {
		return fmt.Errorf("invalid pet-rig texture: %w", err)
	}
	return nil
}

// petStoreValidateCharacterArchive applies the same performer validation used
// by desktop scans. A market listing therefore cannot sell a pack that later
// fails to resolve its state machine or clip references locally.
func petStoreValidateCharacterArchive(zr *zip.Reader, character *petpack.PetPackCharacterAssets, rigAssets *petpack.PetPackRigAssets) error {
	if zr == nil || character == nil || rigAssets == nil {
		return fmt.Errorf("native-character pack is missing character or rig assets")
	}
	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		if f != nil && !strings.HasSuffix(f.Name, "/") {
			files[filepath.ToSlash(f.Name)] = f
		}
	}
	read := func(path, label string) ([]byte, error) {
		path = filepath.ToSlash(strings.TrimSpace(path))
		f := files[path]
		if f == nil {
			return nil, fmt.Errorf("%s is missing: %s", label, path)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", label, err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(rc, petStoreMaxFileBytes+1))
		closeErr := rc.Close()
		if readErr != nil || closeErr != nil || int64(len(raw)) > petStoreMaxFileBytes {
			return nil, fmt.Errorf("read %s: %s", label, path)
		}
		return raw, nil
	}
	performerRaw, err := read(character.Definition, "character definition")
	if err != nil {
		return err
	}
	rigRaw, err := read(rigAssets.Definition, "pet-rig definition")
	if err != nil {
		return err
	}
	if _, err := petpack.ValidateCharacterDefinition(performerRaw, rigRaw, rigAssets); err != nil {
		return fmt.Errorf("invalid character definition: %w", err)
	}
	return nil
}

func (h *SkillMarketHandlers) petStoreDir() string { return filepath.Join(h.dataDir, "pet-store") }

// writePetStoreArchiveAtomic keeps a partial disk write from becoming the
// archive served by a concurrent reader. The rename is atomic within the
// package directory, and cleanup leaves no stale temporary artifact behind.
func writePetStoreArchiveAtomic(path string, data []byte) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("empty pet pack archive path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".pet-store-*.tmp")
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
	where := " WHERE status='active' AND (name LIKE ? ESCAPE '\\' OR description LIKE ? ESCAPE '\\')"
	// LIKE metacharacters in the search box must match literally: an unescaped
	// "%" would otherwise turn every query into a full-catalogue scan.
	escapedQuery := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
	args := []any{"%" + escapedQuery + "%", "%" + escapedQuery + "%"}
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
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return packs, nil
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
// account's public identity, Credits balance, own visible listings, and lifetime buys.
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
	// A purchase is a permanent entitlement. Moderation or a creator withdrawal
	// hides the listing from public discovery but must not erase it from the
	// buyer's account history; the client can present its unavailable state while
	// the download endpoint remains the final policy gate.
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
		purchases = append(purchases, map[string]any{"pack": accountPetStorePack(p), "purchased_at": purchasedAt})
	}
	if err := rows.Err(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": map[string]string{"id": userID, "email": email}, "credits": balance, "uploads": uploads, "purchases": purchases})
}

// GetPetStoreCreatorReport returns date-bounded creator metrics. Sales are
// deliberately derived from purchase ledger entries instead of the listing's
// lifetime counters, while downloads come from delivery events. This keeps
// free downloads and paid sales as separate, non-overlapping measures.
func (h *SkillMarketHandlers) GetPetStoreCreatorReport(w http.ResponseWriter, r *http.Request) {
	h.ensurePetStoreSchema()
	userID, _, err := h.petStoreUser(r)
	if err != nil {
		smError(w, http.StatusUnauthorized, err.Error())
		return
	}
	period, start, end, err := petStoreReportRange(r.URL.Query().Get("period"), r.URL.Query().Get("date"), time.Now().UTC())
	if err != nil {
		smError(w, http.StatusBadRequest, err.Error())
		return
	}
	previousStart := petStorePreviousReportStart(period, start)
	type reportPack struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		Status         string `json:"status"`
		PreviewDataURL string `json:"preview_data_url,omitempty"`
		SalesCount     int64  `json:"sales_count,omitempty"`
		SalesAmount    int64  `json:"sales_amount,omitempty"`
		DownloadCount  int64  `json:"download_count,omitempty"`
	}
	loadSales := func(from, to time.Time, includePacks bool) (int64, int64, int64, []reportPack, error) {
		var amount, count, packCount int64
		queryErr := h.store.ReadDB().QueryRowContext(r.Context(), `SELECT COALESCE(SUM(b.amount_paid), 0), COUNT(b.id), COUNT(DISTINCT p.id)
			FROM sm_pet_store_purchases b JOIN sm_pet_store_packs p ON p.id=b.pack_id
			WHERE p.owner_user_id=? AND b.status='active' AND b.amount_paid>0 AND b.created_at>=? AND b.created_at<?`, userID, from.Format(time.RFC3339Nano), to.Format(time.RFC3339Nano)).Scan(&amount, &count, &packCount)
		if queryErr != nil {
			return 0, 0, 0, nil, queryErr
		}
		if !includePacks {
			return amount, count, packCount, nil, nil
		}
		rows, queryErr := h.store.ReadDB().QueryContext(r.Context(), `SELECT p.id, p.name, p.status, p.zip_path, COUNT(b.id), COALESCE(SUM(b.amount_paid), 0)
			FROM sm_pet_store_purchases b JOIN sm_pet_store_packs p ON p.id=b.pack_id
			WHERE p.owner_user_id=? AND b.status='active' AND b.amount_paid>0 AND b.created_at>=? AND b.created_at<?
			GROUP BY p.id ORDER BY SUM(b.amount_paid) DESC, COUNT(b.id) DESC, p.name ASC LIMIT 5`, userID, from.Format(time.RFC3339Nano), to.Format(time.RFC3339Nano))
		if queryErr != nil {
			return 0, 0, 0, nil, queryErr
		}
		defer rows.Close()
		packs := make([]reportPack, 0, 5)
		for rows.Next() {
			var pack reportPack
			var zipPath string
			if scanErr := rows.Scan(&pack.ID, &pack.Name, &pack.Status, &zipPath, &pack.SalesCount, &pack.SalesAmount); scanErr != nil {
				return 0, 0, 0, nil, scanErr
			}
			pack.PreviewDataURL = petStorePackPreviewDataURL(zipPath)
			packs = append(packs, pack)
		}
		if queryErr = rows.Err(); queryErr != nil {
			return 0, 0, 0, nil, queryErr
		}
		return amount, count, packCount, packs, nil
	}
	loadDownloads := func() ([]reportPack, error) {
		rows, queryErr := h.store.ReadDB().QueryContext(r.Context(), `SELECT p.id, p.name, p.status, p.zip_path, COUNT(d.id)
			FROM sm_pet_store_downloads d JOIN sm_pet_store_packs p ON p.id=d.pack_id
			WHERE p.owner_user_id=? AND p.price=0 AND d.created_at>=? AND d.created_at<?
			GROUP BY p.id ORDER BY COUNT(d.id) DESC, p.name ASC LIMIT 5`, userID, start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano))
		if queryErr != nil {
			return nil, queryErr
		}
		defer rows.Close()
		packs := make([]reportPack, 0)
		for rows.Next() {
			var pack reportPack
			var zipPath string
			if scanErr := rows.Scan(&pack.ID, &pack.Name, &pack.Status, &zipPath, &pack.DownloadCount); scanErr != nil {
				return nil, scanErr
			}
			pack.PreviewDataURL = petStorePackPreviewDataURL(zipPath)
			packs = append(packs, pack)
		}
		return packs, rows.Err()
	}
	amount, salesCount, paidPackCount, sales, err := loadSales(start, end, true)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	previousAmount, previousSalesCount, _, _, err := loadSales(previousStart, start, false)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	downloads, err := loadDownloads()
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"period": period, "start_at": start.Format(time.RFC3339), "end_at": end.Format(time.RFC3339),
		"paid_summary":          map[string]int64{"sales_amount": amount, "sales_count": salesCount, "paid_pack_count": paidPackCount},
		"previous_paid_summary": map[string]int64{"sales_amount": previousAmount, "sales_count": previousSalesCount},
		"paid_packs":            sales, "free_download_packs": downloads,
	})
}

func petStoreReportRange(rawPeriod, rawDate string, now time.Time) (string, time.Time, time.Time, error) {
	period := strings.ToLower(strings.TrimSpace(rawPeriod))
	if period == "" {
		period = "month"
	}
	if period != "day" && period != "month" && period != "year" {
		return "", time.Time{}, time.Time{}, fmt.Errorf("period must be day, month, or year")
	}
	anchor := now.UTC()
	if date := strings.TrimSpace(rawDate); date != "" {
		parsed, err := time.Parse("2006-01-02", date)
		if err != nil {
			return "", time.Time{}, time.Time{}, fmt.Errorf("date must use YYYY-MM-DD")
		}
		anchor = parsed.UTC()
	}
	var start, end time.Time
	switch period {
	case "day":
		start = time.Date(anchor.Year(), anchor.Month(), anchor.Day(), 0, 0, 0, 0, time.UTC)
		end = start.AddDate(0, 0, 1)
	case "month":
		start = time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, time.UTC)
		end = start.AddDate(0, 1, 0)
	case "year":
		start = time.Date(anchor.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		end = start.AddDate(1, 0, 0)
	}
	return period, start, end, nil
}

func petStorePreviousReportStart(period string, start time.Time) time.Time {
	switch period {
	case "day":
		return start.AddDate(0, 0, -1)
	case "year":
		return start.AddDate(-1, 0, 0)
	default:
		return start.AddDate(0, -1, 0)
	}
}

func (h *SkillMarketHandlers) petStorePacksForOwner(r *http.Request, userID string) ([]*petStorePack, error) {
	// Deleted listings are tombstones retained for moderation and HA convergence.
	// They are not creator-manageable listings, so keep them out of My uploads.
	rows, err := h.store.ReadDB().QueryContext(r.Context(), `SELECT `+petStorePackColumns+` FROM sm_pet_store_packs WHERE owner_user_id=? AND status NOT IN ('deleted','purged') ORDER BY updated_at DESC`, userID)
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
// the signed-in creator. The stable manifest ID is a marketplace identity:
// withdrawal or deletion allows its original creator to publish an updated
// listing, but never hands the ID to another account.
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
	err = h.store.ReadDB().QueryRowContext(r.Context(), `SELECT id FROM sm_pet_store_packs WHERE source_pack_id=? AND owner_user_id<>? ORDER BY updated_at DESC, id ASC LIMIT 1`, sourcePackID, userID).Scan(&foreignListingID)
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
	// source_pack_id is optional: the web publish form does not collect it.
	// When omitted, the stable manifest ID inside the archive becomes the
	// listing identity. A non-empty value must still match the manifest so a
	// client cannot publish an archive under an unrelated pack identity.
	if sourcePackID != "" && !petStoreSourcePackIDPattern.MatchString(sourcePackID) {
		smError(w, http.StatusBadRequest, "invalid source pet pack id")
		return
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
	if sourcePackID == "" {
		sourcePackID = archiveSourcePackID
	} else if archiveSourcePackID != sourcePackID {
		smError(w, http.StatusBadRequest, "source pet pack id does not match pet-pack.yaml")
		return
	}
	{
		var foreignListingID string
		err := h.store.ReadDB().QueryRowContext(r.Context(), `SELECT id FROM sm_pet_store_packs WHERE source_pack_id=? AND owner_user_id<>? ORDER BY updated_at DESC, id ASC LIMIT 1`, sourcePackID, userID).Scan(&foreignListingID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			smError(w, http.StatusInternalServerError, "check pet pack ownership: "+err.Error())
			return
		}
		if foreignListingID != "" {
			smError(w, http.StatusConflict, "this pet pack ID is already published by another creator")
			return
		}
		var retainedListings int
		if err := h.store.ReadDB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sm_pet_store_packs WHERE owner_user_id=? AND source_pack_id=? AND status IN ('active','paused')`, userID, sourcePackID).Scan(&retainedListings); err != nil {
			smError(w, http.StatusInternalServerError, "check existing pet pack listing: "+err.Error())
			return
		}
		if retainedListings > 0 {
			smError(w, http.StatusConflict, "this local pet pack is already listed or paused; unlist it before publishing again")
			return
		}
	}
	if err := os.MkdirAll(h.petStoreDir(), 0o755); err != nil {
		smError(w, http.StatusInternalServerError, "create pet store directory: "+err.Error())
		return
	}
	id := "pet_" + petStoreUniqueID("pack")
	zipPath := filepath.Join(h.petStoreDir(), id+".zip")
	if err := writePetStoreArchiveAtomic(zipPath, data); err != nil {
		smError(w, http.StatusInternalServerError, "save pet pack: "+err.Error())
		return
	}
	archiveCommitted := false
	defer func() {
		if archiveCommitted {
			return
		}
		_ = os.Remove(zipPath)
	}()
	now := petStoreNow()
	p := &petStorePack{ID: id, OwnerID: userID, OwnerEmail: email, SourcePackID: sourcePackID, Name: name, Description: desc, Version: version, Price: price, Status: "active", ZipPath: zipPath, PackageSize: int64(len(data)), CreatedAt: now, UpdatedAt: now}
	// The lightweight checks above give a quick conflict response before writing
	// the archive. Repeat them inside one IMMEDIATE transaction immediately
	// before insertion: a withdrawn listing retains its creator's identity claim,
	// and a competing creator must not win by racing the earlier read.
	tx, err := h.store.BeginImmediate(r.Context())
	if err == nil {
		defer tx.Rollback()
		var foreignListingID string
		err = tx.QueryRowContext(r.Context(), `SELECT id FROM sm_pet_store_packs WHERE source_pack_id=? AND owner_user_id<>? ORDER BY updated_at DESC, id ASC LIMIT 1`, sourcePackID, userID).Scan(&foreignListingID)
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
		}
		if err == nil && foreignListingID != "" {
			err = fmt.Errorf("pet store source pack ID claimed")
		}
	}
	if err == nil {
		var retainedListings int
		err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sm_pet_store_packs WHERE owner_user_id=? AND source_pack_id=? AND status IN ('active','paused')`, userID, sourcePackID).Scan(&retainedListings)
		if err == nil && retainedListings > 0 {
			err = fmt.Errorf("pet store source pack already listed")
		}
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO sm_pet_store_packs (`+petStorePackColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0, 0, ?, ?)`, p.ID, p.OwnerID, p.OwnerEmail, p.SourcePackID, p.Name, p.Description, p.Version, p.Price, p.Status, p.ZipPath, p.PackageSize, p.CreatedAt, p.UpdatedAt)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "claimed") {
			smError(w, http.StatusConflict, "this pet pack ID is already published by another creator")
			return
		}
		if strings.Contains(strings.ToLower(err.Error()), "already listed") {
			smError(w, http.StatusConflict, "this local pet pack is already listed or paused; unlist it before publishing again")
			return
		}
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
			smError(w, http.StatusConflict, "this pet pack ID is already published by another creator")
			return
		}
		smError(w, http.StatusInternalServerError, "create listing: "+err.Error())
		return
	}
	archiveCommitted = true
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
	// CompletePetStorePurchase uses one SQLite IMMEDIATE transaction. It checks
	// ownership by the stable source_pack_id under that transaction, so repeated
	// clicks and re-issued listings race harmlessly without double charging.
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
	_ = h.store.ReadDB().QueryRowContext(r.Context(), `SELECT COUNT(*)
		FROM sm_pet_store_purchases b
		JOIN sm_pet_store_packs owned ON owned.id=b.pack_id
		WHERE b.buyer_user_id=? AND b.status='active' AND owned.source_pack_id=?`, buyerID, p.SourcePackID).Scan(&count)
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
		if errors.Is(err, skillmarket.ErrInsufficientCredits) {
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
	// Buy-out semantics: a purchase is a permanent entitlement. Withdrawal or
	// an administrative pause only stops new sales; the creator and every
	// entitled buyer keep downloading their copy. Deletion and purge are
	// terminal moderation states and block every download.
	if p.Status == "deleted" || p.Status == "purged" {
		smError(w, http.StatusGone, "pet pack has been removed")
		return
	}
	if userID != p.OwnerID {
		var n int
		_ = h.store.ReadDB().QueryRowContext(r.Context(), `SELECT COUNT(*)
			FROM sm_pet_store_purchases b
			JOIN sm_pet_store_packs owned ON owned.id=b.pack_id
			WHERE b.buyer_user_id=? AND b.status='active'
			  AND ((?<>'' AND owned.source_pack_id=?) OR (?='' AND owned.id=?))`, userID, p.SourcePackID, p.SourcePackID, p.SourcePackID, id).Scan(&n)
		if n == 0 {
			smError(w, http.StatusForbidden, "purchase required")
			return
		}
	}
	// Re-check immediately before opening the archive. A moderator can delete or
	// purge the listing after the initial entitlement lookup; do not start a
	// download that has just been terminally removed. Withdrawal and pause do
	// not revoke existing entitlements, so they are not re-checked here.
	var currentStatus string
	if err := h.store.ReadDB().QueryRowContext(r.Context(), `SELECT status FROM sm_pet_store_packs WHERE id=?`, id).Scan(&currentStatus); err != nil {
		smError(w, http.StatusNotFound, "pet pack not found")
		return
	}
	if currentStatus == "deleted" || currentStatus == "purged" {
		smError(w, http.StatusGone, "pet pack has been removed")
		return
	}
	if _, err := os.Stat(p.ZipPath); err != nil {
		smError(w, http.StatusGone, "pet pack file is unavailable")
		return
	}
	tx, err := h.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		smError(w, http.StatusInternalServerError, "record pet pack download")
		return
	}
	defer tx.Rollback()
	// download_count tracks distinct downloaders, not raw deliveries: a repeat
	// download by the same user is still recorded as a delivery event (creator
	// reports are date-bounded) but must not inflate the catalogue counter.
	var priorDownloads int
	if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sm_pet_store_downloads WHERE pack_id=? AND downloader_user_id=?`, id, userID).Scan(&priorDownloads); err != nil {
		smError(w, http.StatusInternalServerError, "record pet pack download")
		return
	}
	if priorDownloads == 0 {
		result, err := tx.ExecContext(r.Context(), `UPDATE sm_pet_store_packs SET download_count=download_count+1 WHERE id=? AND status NOT IN ('deleted','purged')`, id)
		if err != nil {
			smError(w, http.StatusInternalServerError, "record pet pack download")
			return
		}
		if changed, _ := result.RowsAffected(); changed == 0 {
			smError(w, http.StatusGone, "pet pack is unavailable")
			return
		}
	}
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO sm_pet_store_downloads (id, pack_id, downloader_user_id, created_at) VALUES (?, ?, ?, ?)`, "pet_"+petStoreUniqueID("download"), id, userID, petStoreNow()); err != nil {
		smError(w, http.StatusInternalServerError, "record pet pack download event")
		return
	}
	if err := tx.Commit(); err != nil {
		smError(w, http.StatusInternalServerError, "record pet pack download")
		return
	}
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
	res, err := h.store.DB().ExecContext(r.Context(), `UPDATE sm_pet_store_packs SET status='withdrawn', updated_at=? WHERE id=? AND owner_user_id=? AND status='active'`, petStoreNow(), id, userID)
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

// AdminListPetStorePacks is the moderation inventory. Listings are published
// immediately by design; this endpoint gives HubCenter operators control after
// publication without turning the user flow into a review queue.
func (h *SkillMarketHandlers) AdminListPetStorePacks(w http.ResponseWriter, r *http.Request) {
	h.ensurePetStoreSchema()
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	const pageSize = 20
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if status != "" && !validPetStorePackStatus(status) {
		smError(w, http.StatusBadRequest, "invalid pet store status")
		return
	}
	// Deletion is a tombstone for HA convergence and purchase audit. It should
	// not, however, remain in the default moderation queue after an operator
	// confirms deletion. Operators can still explicitly select status=deleted
	// when they need to inspect those audit records.
	where, args := " WHERE status NOT IN ('deleted','purged')", []any{}
	if status != "" {
		where, args = " WHERE status=?", append(args, status)
	}
	var total int
	if err := h.store.ReadDB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sm_pet_store_packs`+where, args...).Scan(&total); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows, err := h.store.ReadDB().QueryContext(r.Context(), `SELECT `+petStorePackColumns+` FROM sm_pet_store_packs`+where+` ORDER BY updated_at DESC, id ASC LIMIT ? OFFSET ?`, append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	packs := make([]petStoreAdminPack, 0, pageSize)
	for rows.Next() {
		p, scanErr := scanPetStorePack(rows)
		if scanErr != nil {
			smError(w, http.StatusInternalServerError, scanErr.Error())
			return
		}
		p.PreviewDataURL = petStorePackPreviewDataURL(p.ZipPath)
		packs = append(packs, adminPetStorePack(p))
	}
	if err := rows.Err(); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// total_pages stays 0 for an empty result set, matching the public listing
	// endpoint, so pagination UIs render "no pages" instead of a phantom page 1.
	totalPages := (total + pageSize - 1) / pageSize
	writeJSON(w, http.StatusOK, map[string]any{"packs": packs, "page": page, "page_size": pageSize, "total": total, "total_pages": totalPages})
}

// AdminPreviewPetStorePack returns only safe raster asset entries
// embedded in an archive. The archive itself is never made a public admin URL.
func (h *SkillMarketHandlers) AdminPreviewPetStorePack(w http.ResponseWriter, r *http.Request) {
	h.ensurePetStoreSchema()
	id := strings.TrimSpace(r.PathValue("id"))
	p, err := scanPetStorePack(h.store.ReadDB().QueryRowContext(r.Context(), `SELECT `+petStorePackColumns+` FROM sm_pet_store_packs WHERE id=?`, id))
	if err != nil {
		smError(w, http.StatusNotFound, "pet pack not found")
		return
	}
	data, err := os.ReadFile(p.ZipPath)
	if err != nil {
		smError(w, http.StatusGone, "pet pack file is unavailable")
		return
	}
	if len(data) == 0 || int64(len(data)) > petStoreMaxArchiveBytes {
		smError(w, http.StatusBadRequest, "invalid pet pack archive")
		return
	}
	assets, err := petStorePreviewAssets(data)
	if err != nil {
		smError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pack": p, "images": assets})
}

func petStorePreviewAssets(data []byte) ([]map[string]string, error) {
	if len(data) == 0 || int64(len(data)) > petStoreMaxArchiveBytes {
		return nil, fmt.Errorf("invalid pet pack archive")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("invalid zip: %w", err)
	}
	assets := make([]map[string]string, 0, 8)
	const maxPreviewAssets = 12
	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		if f != nil && !f.FileInfo().IsDir() {
			files[filepath.ToSlash(f.Name)] = f
		}
	}
	add := func(f *zip.File) error {
		if len(assets) >= maxPreviewAssets {
			return nil
		}
		if f == nil || f.FileInfo().IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(f.Name))
		// SVG can execute script-like content in an administrator's browser. Only
		// expose raster previews unless a dedicated SVG sanitizer is introduced.
		mime := map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp"}[ext]
		if mime == "" {
			return nil
		}
		if f.UncompressedSize64 == 0 || f.UncompressedSize64 > petStoreMaxFileBytes {
			return nil
		}
		rc, openErr := f.Open()
		if openErr != nil {
			return openErr
		}
		content, readErr := io.ReadAll(io.LimitReader(rc, petStoreMaxFileBytes+1))
		closeErr := rc.Close()
		if readErr != nil || closeErr != nil || int64(len(content)) > petStoreMaxFileBytes {
			return fmt.Errorf("read pet pack image")
		}
		assets = append(assets, map[string]string{"name": filepath.ToSlash(f.Name), "mime": mime, "data_url": "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(content)})
		return nil
	}
	seen := make(map[string]struct{}, maxPreviewAssets)
	for _, path := range petStorePreviewCandidatePaths(zr) {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path == "" || files[path] == nil {
			continue
		}
		seen[path] = struct{}{}
		if err := add(files[path]); err != nil {
			return nil, err
		}
	}
	for _, f := range zr.File {
		if len(assets) >= maxPreviewAssets {
			break
		}
		if f != nil {
			if _, ok := seen[filepath.ToSlash(f.Name)]; ok {
				continue
			}
		}
		if err := add(f); err != nil {
			return nil, err
		}
	}
	return assets, nil
}

func (h *SkillMarketHandlers) AdminPausePetStorePack(w http.ResponseWriter, r *http.Request) {
	h.adminSetPetStorePackStatus(w, r, "active", "paused", "Pet Store listing paused")
}

func (h *SkillMarketHandlers) AdminResumePetStorePack(w http.ResponseWriter, r *http.Request) {
	h.ensurePetStoreSchema()
	id := strings.TrimSpace(r.PathValue("id"))
	now := petStoreNow()
	res, err := h.store.DB().ExecContext(r.Context(), `UPDATE sm_pet_store_packs
		SET status='active', updated_at=?
		WHERE id=? AND status='paused'
		  AND NOT EXISTS (
			SELECT 1 FROM sm_pet_store_packs AS conflict
			WHERE conflict.source_pack_id=sm_pet_store_packs.source_pack_id
			  AND conflict.id<>sm_pet_store_packs.id
			  AND conflict.status='active'
		)`, now, id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
			smError(w, http.StatusConflict, "another active listing already owns this pet pack ID")
			return
		}
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if count, _ := res.RowsAffected(); count == 0 {
		var status string
		err := h.store.ReadDB().QueryRowContext(r.Context(), `SELECT status FROM sm_pet_store_packs WHERE id=?`, id).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) || status != "paused" {
			smError(w, http.StatusNotFound, "paused pet pack not found")
		} else {
			smError(w, http.StatusConflict, "another active listing already owns this pet pack ID")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "active", "message": "Pet Store listing resumed"})
	h.emitPetStoreSync(r.Context(), id)
}

func (h *SkillMarketHandlers) adminSetPetStorePackStatus(w http.ResponseWriter, r *http.Request, from, to, result string) {
	h.ensurePetStoreSchema()
	id := strings.TrimSpace(r.PathValue("id"))
	res, err := h.store.DB().ExecContext(r.Context(), `UPDATE sm_pet_store_packs SET status=?, updated_at=? WHERE id=? AND status=?`, to, petStoreNow(), id, from)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if count, _ := res.RowsAffected(); count == 0 {
		smError(w, http.StatusNotFound, "pet pack is not in the expected status")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": to, "message": result})
	h.emitPetStoreSync(r.Context(), id)
}

// AdminDeletePetStorePack logically removes a listing. Existing
// buyers retain their purchase record for audit, but cannot download an asset
// that has been deleted by a moderator. The creator is notified after state is
// committed; a mail outage must never undo safety moderation.
func (h *SkillMarketHandlers) AdminDeletePetStorePack(w http.ResponseWriter, r *http.Request) {
	h.ensurePetStoreSchema()
	id := strings.TrimSpace(r.PathValue("id"))
	p, err := scanPetStorePack(h.store.ReadDB().QueryRowContext(r.Context(), `SELECT `+petStorePackColumns+` FROM sm_pet_store_packs WHERE id=?`, id))
	if err != nil {
		smError(w, http.StatusNotFound, "pet pack not found")
		return
	}
	// Keep a tombstone rather than hard-deleting its database row: replicated
	// peers receive the deletion, historic purchases remain attributable, and a
	// delayed HA snapshot cannot recreate a visible listing.
	now := petStoreNow()
	result, err := h.store.DB().ExecContext(r.Context(), `UPDATE sm_pet_store_packs SET status='deleted', updated_at=? WHERE id=? AND status NOT IN ('deleted','purged')`, now, id)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		// Deleting an already-removed listing is deliberately idempotent. Do not
		// send the creator a duplicate moderation email or replicate a no-op. A
		// terminal purge must report its actual state instead of misleading API
		// clients into treating it as a regular deleted audit record.
		var status string
		if err := h.store.ReadDB().QueryRowContext(r.Context(), `SELECT status FROM sm_pet_store_packs WHERE id=?`, id).Scan(&status); err != nil {
			smError(w, http.StatusNotFound, "pet pack not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": status})
		return
	}
	if h.petStoreMailer != nil && strings.TrimSpace(p.OwnerEmail) != "" {
		_ = h.petStoreMailer.Send(context.WithoutCancel(r.Context()), []string{p.OwnerEmail}, "Your Pet Store listing was removed", fmt.Sprintf("Hello,\r\n\r\nYour pet pack \"%s\" (%s) was removed from the Pet Store by a HubCenter administrator. Existing downloads are no longer available.\r\n\r\nIf you believe this was a mistake, please contact your HubCenter administrator.\r\n", p.Name, p.ID))
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	// Preserve the archive until an HA-aware retention pass collects it. The
	// deleted status prevents all client downloads immediately and makes this
	// tombstone safe to replicate to every peer.
	h.emitPetStoreSync(r.Context(), id)
}

// AdminPurgePetStorePack is the explicit second step after logical deletion.
// It removes the local archive and hides the listing even from the deleted
// audit filter, while retaining a terminal HA tombstone to prevent a delayed
// peer snapshot from bringing the package back.
func (h *SkillMarketHandlers) AdminPurgePetStorePack(w http.ResponseWriter, r *http.Request) {
	h.ensurePetStoreSchema()
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		smError(w, http.StatusNotFound, "pet pack not found")
		return
	}
	result, err := h.store.DB().ExecContext(r.Context(), `UPDATE sm_pet_store_packs
		SET status='purged', zip_path='', package_size=0, updated_at=?
		WHERE id=? AND status='deleted'`, petStoreNow(), id)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		var status string
		err := h.store.ReadDB().QueryRowContext(r.Context(), `SELECT status FROM sm_pet_store_packs WHERE id=?`, id).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) || status != "purged" {
			smError(w, http.StatusConflict, "only deleted pet packs can be permanently removed")
			return
		}
	}
	// Queue the terminal state before best-effort local cleanup. The HA recorder
	// snapshots it asynchronously, so the persisted tombstone is already enough
	// to prevent resurrection even if this node's local file cleanup fails.
	h.emitPetStoreSync(r.Context(), id)
	if err := h.removePetStorePackArchive(id); err != nil {
		// The terminal state has already blocked the file from every endpoint.
		// Surface the cleanup failure for operators without rolling it back into a
		// downloadable state.
		smError(w, http.StatusInternalServerError, "remove pet pack archive: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "purged"})
}

func (h *SkillMarketHandlers) removePetStorePackArchive(id string) error {
	id = strings.TrimSpace(id)
	if !strings.HasPrefix(id, "pet_") || filepath.Base(id) != id || strings.ContainsAny(id, `\\/:`) {
		return fmt.Errorf("invalid pet pack id")
	}
	if err := os.Remove(filepath.Join(h.petStoreDir(), id+".zip")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func validPetStorePackStatus(status string) bool {
	switch status {
	case "active", "paused", "withdrawn", "deleted", "purged":
		return true
	default:
		return false
	}
}

func petStorePackClaimsSourceID(status string) bool {
	// This protects the discoverable/pausable listing slot. Source identity is
	// deliberately broader (every historic listing) and is enforced in the
	// publication transaction, so a withdrawn or deleted pack can be re-issued
	// only by its original creator and never repurposed by another account.
	return status == "active" || status == "paused"
}

// RFC3339Nano preserves a strict ordering for sequential moderation changes.
// HA snapshot conflict resolution compares these timestamps, so second-level
// precision could otherwise let a delayed pause overwrite a same-second resume.
func petStoreNow() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
