package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/gui/petpack"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	_ "modernc.org/sqlite"
)

func TestGetPetStoreAccountReturnsPurchasedPack(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatalf("new skillmarket store: %v", err)
	}
	userSvc := skillmarket.NewUserService(store, nil)
	authSvc := skillmarket.NewAuthService(store, nil, "")
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, UserSvc: userSvc, AuthSvc: authSvc, CreditsSvc: skillmarket.NewCreditsService(store), DataDir: t.TempDir()})

	ctx := context.Background()
	buyer, err := userSvc.EnsureAccountWithID(ctx, "buyer-1", "buyer@example.test")
	if err != nil {
		t.Fatalf("ensure buyer: %v", err)
	}
	if _, err := userSvc.EnsureAccountWithID(ctx, "seller-1", "seller@example.test"); err != nil {
		t.Fatalf("ensure seller: %v", err)
	}
	session, err := authSvc.CreateSessionForUser(ctx, buyer.ID, buyer.Email)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	h.ensurePetStoreSchema()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO sm_pet_store_packs (`+petStorePackColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "pet_1", "seller-1", "seller@example.test", "source-pet", "Purchased pet", "", "1.0.0", 5, "active", "unused.zip", 1, 0, 1, 5, now, now); err != nil {
		t.Fatalf("seed pack: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO sm_pet_store_purchases (id, pack_id, buyer_user_id, buyer_email, amount_paid, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "purchase_1", "pet_1", buyer.ID, buyer.Email, 5, "active", now); err != nil {
		t.Fatalf("seed purchase: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pet-store/account", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	rec := httptest.NewRecorder()
	h.GetPetStoreAccount(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("account status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Purchases []struct {
			Pack struct {
				ID string `json:"id"`
			} `json:"pack"`
		} `json:"purchases"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Purchases) != 1 || body.Purchases[0].Pack.ID != "pet_1" {
		t.Fatalf("purchases=%+v, want pet_1", body.Purchases)
	}
}

func TestPetStoreReissuedPackKeepsSourceEntitlement(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	userSvc := skillmarket.NewUserService(store, nil)
	authSvc := skillmarket.NewAuthService(store, nil, "")
	creditsSvc := skillmarket.NewCreditsService(store)
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, UserSvc: userSvc, AuthSvc: authSvc, CreditsSvc: creditsSvc, DataDir: t.TempDir()})
	ctx := context.Background()
	buyer, err := userSvc.EnsureAccountWithID(ctx, "reissue-buyer", "reissue-buyer@example.test")
	if err != nil {
		t.Fatal(err)
	}
	seller, err := userSvc.EnsureAccountWithID(ctx, "reissue-seller", "reissue-seller@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := authSvc.CreateSessionForUser(ctx, buyer.ID, buyer.Email)
	if err != nil {
		t.Fatal(err)
	}
	h.ensurePetStoreSchema()
	archive := validPetStoreTestArchive(t, "reissued-pet")
	if err := os.MkdirAll(h.petStoreDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	newZipPath := filepath.Join(h.petStoreDir(), "new-listing.zip")
	if err := os.WriteFile(newZipPath, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	insertPack := `INSERT INTO sm_pet_store_packs (` + petStorePackColumns + `) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := db.Exec(insertPack, "old-listing", seller.ID, seller.Email, "reissued-pet", "Earlier release", "", "1.0.0", 5, "withdrawn", "retained-old.zip", 1, 0, 1, 5, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(insertPack, "new-listing", seller.ID, seller.Email, "reissued-pet", "Current release", "", "2.0.0", 9, "active", newZipPath, int64(len(archive)), 0, 0, 0, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sm_pet_store_purchases (id, pack_id, buyer_user_id, buyer_email, amount_paid, status, created_at) VALUES (?, ?, ?, ?, ?, 'active', ?)`, "old-purchase", "old-listing", buyer.ID, buyer.Email, 5, now); err != nil {
		t.Fatal(err)
	}
	balanceBefore, err := creditsSvc.GetBalance(ctx, buyer.ID)
	if err != nil {
		t.Fatal(err)
	}
	purchaseReq := httptest.NewRequest(http.MethodPost, "/api/v1/pet-store/packs/new-listing/purchase", nil)
	purchaseReq.SetPathValue("id", "new-listing")
	purchaseReq.Header.Set("Authorization", "Bearer "+session.Token)
	purchaseRec := httptest.NewRecorder()
	h.PurchasePetStorePack(purchaseRec, purchaseReq)
	if purchaseRec.Code != http.StatusOK || !bytes.Contains(purchaseRec.Body.Bytes(), []byte(`"status":"owned"`)) {
		t.Fatalf("reissued purchase status=%d body=%s", purchaseRec.Code, purchaseRec.Body.String())
	}
	balanceAfter, err := creditsSvc.GetBalance(ctx, buyer.ID)
	if err != nil || balanceAfter != balanceBefore {
		t.Fatalf("buyer balance before=%d after=%d err=%v", balanceBefore, balanceAfter, err)
	}
	var newPurchases, newPurchaseCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sm_pet_store_purchases WHERE pack_id='new-listing'`).Scan(&newPurchases); err != nil || newPurchases != 0 {
		t.Fatalf("new purchases=%d err=%v", newPurchases, err)
	}
	if err := db.QueryRow(`SELECT purchase_count FROM sm_pet_store_packs WHERE id='new-listing'`).Scan(&newPurchaseCount); err != nil || newPurchaseCount != 0 {
		t.Fatalf("new listing purchase_count=%d err=%v", newPurchaseCount, err)
	}
	downloadReq := httptest.NewRequest(http.MethodGet, "/api/v1/pet-store/packs/new-listing/download", nil)
	downloadReq.SetPathValue("id", "new-listing")
	downloadReq.Header.Set("Authorization", "Bearer "+session.Token)
	downloadRec := httptest.NewRecorder()
	h.DownloadPetStorePack(downloadRec, downloadReq)
	if downloadRec.Code != http.StatusOK {
		t.Fatalf("reissued download status=%d body=%s", downloadRec.Code, downloadRec.Body.String())
	}
	var downloadEvents int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sm_pet_store_downloads WHERE pack_id='new-listing' AND downloader_user_id=?`, buyer.ID).Scan(&downloadEvents); err != nil || downloadEvents != 1 {
		t.Fatalf("new listing download events=%d err=%v", downloadEvents, err)
	}
}

func TestGetPetStoreCreatorReportSeparatesPaidSalesAndFreeDownloads(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	userSvc := skillmarket.NewUserService(store, nil)
	authSvc := skillmarket.NewAuthService(store, nil, "")
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, UserSvc: userSvc, AuthSvc: authSvc, CreditsSvc: skillmarket.NewCreditsService(store), DataDir: t.TempDir()})
	ctx := context.Background()
	creator, err := userSvc.EnsureAccountWithID(ctx, "creator-report", "creator@example.test")
	if err != nil {
		t.Fatal(err)
	}
	buyer, err := userSvc.EnsureAccountWithID(ctx, "buyer-report", "buyer@example.test")
	if err != nil {
		t.Fatal(err)
	}
	previousBuyer, err := userSvc.EnsureAccountWithID(ctx, "buyer-previous-report", "buyer-previous@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := authSvc.CreateSessionForUser(ctx, creator.ID, creator.Email)
	if err != nil {
		t.Fatal(err)
	}
	h.ensurePetStoreSchema()
	created := "2026-08-01T00:00:00Z"
	insertPack := `INSERT INTO sm_pet_store_packs (` + petStorePackColumns + `) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	for _, pack := range [][]any{
		{"pet_paid", creator.ID, creator.Email, "paid-pet", "Paid pet", "", "1.0.0", int64(8), "withdrawn", "unused.zip", int64(0), int64(0), int64(0), int64(0), created, created},
		{"pet_free", creator.ID, creator.Email, "free-pet", "Free pet", "", "1.0.0", int64(0), "active", "unused.zip", int64(0), int64(0), int64(0), int64(0), created, created},
	} {
		if _, err := db.Exec(insertPack, pack...); err != nil {
			t.Fatal(err)
		}
	}
	for _, purchase := range [][]any{
		{"sale_current", "pet_paid", buyer.ID, buyer.Email, int64(8), "active", "2026-08-15T10:00:00Z"},
		{"free_acquisition", "pet_free", buyer.ID, buyer.Email, int64(0), "active", "2026-08-15T10:00:00Z"},
		{"sale_previous", "pet_paid", previousBuyer.ID, previousBuyer.Email, int64(8), "active", "2026-07-15T10:00:00Z"},
	} {
		if _, err := db.Exec(`INSERT INTO sm_pet_store_purchases (id, pack_id, buyer_user_id, buyer_email, amount_paid, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, purchase...); err != nil {
			t.Fatal(err)
		}
	}
	for _, event := range [][]any{
		{"download_current", "pet_free", buyer.ID, "2026-08-18T10:00:00Z"},
		{"download_previous", "pet_free", buyer.ID, "2026-07-18T10:00:00Z"},
		{"download_paid", "pet_paid", buyer.ID, "2026-08-18T10:00:00Z"},
	} {
		if _, err := db.Exec(`INSERT INTO sm_pet_store_downloads (id, pack_id, downloader_user_id, created_at) VALUES (?, ?, ?, ?)`, event...); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pet-store/creator-report?period=month&date=2026-08-20", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	rec := httptest.NewRecorder()
	h.GetPetStoreCreatorReport(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("report status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		PaidSummary struct {
			SalesAmount int64 `json:"sales_amount"`
			SalesCount  int64 `json:"sales_count"`
			PaidPacks   int64 `json:"paid_pack_count"`
		} `json:"paid_summary"`
		PreviousPaidSummary struct {
			SalesAmount int64 `json:"sales_amount"`
		} `json:"previous_paid_summary"`
		PaidPacks         []map[string]any `json:"paid_packs"`
		FreeDownloadPacks []map[string]any `json:"free_download_packs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.PaidSummary.SalesAmount != 8 || body.PaidSummary.SalesCount != 1 || body.PaidSummary.PaidPacks != 1 {
		t.Fatalf("paid summary=%+v", body.PaidSummary)
	}
	if body.PreviousPaidSummary.SalesAmount != 8 || len(body.PaidPacks) != 1 || body.PaidPacks[0]["id"] != "pet_paid" {
		t.Fatalf("paid report=%+v previous=%+v", body.PaidPacks, body.PreviousPaidSummary)
	}
	if len(body.FreeDownloadPacks) != 1 || body.FreeDownloadPacks[0]["id"] != "pet_free" || body.FreeDownloadPacks[0]["download_count"] != float64(1) {
		t.Fatalf("free downloads=%+v", body.FreeDownloadPacks)
	}
}

func TestGetPetStoreAccountRetainsUnavailablePurchaseHistory(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatalf("new skillmarket store: %v", err)
	}
	userSvc := skillmarket.NewUserService(store, nil)
	authSvc := skillmarket.NewAuthService(store, nil, "")
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, UserSvc: userSvc, AuthSvc: authSvc, CreditsSvc: skillmarket.NewCreditsService(store), DataDir: t.TempDir()})
	ctx := context.Background()
	buyer, err := userSvc.EnsureAccountWithID(ctx, "buyer-history", "buyer-history@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := userSvc.EnsureAccountWithID(ctx, "seller-history", "seller-history@example.test"); err != nil {
		t.Fatal(err)
	}
	session, err := authSvc.CreateSessionForUser(ctx, buyer.ID, buyer.Email)
	if err != nil {
		t.Fatal(err)
	}
	h.ensurePetStoreSchema()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO sm_pet_store_packs (`+petStorePackColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "pet_history", "seller-history", "seller-history@example.test", "history-pet", "Removed pet", "", "1.0.0", 0, "withdrawn", "unused.zip", 1, 0, 1, 0, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sm_pet_store_purchases (id, pack_id, buyer_user_id, buyer_email, amount_paid, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "purchase_history", "pet_history", buyer.ID, buyer.Email, 0, "active", now); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pet-store/account", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	rec := httptest.NewRecorder()
	h.GetPetStoreAccount(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("account status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Purchases []struct {
			Pack struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"pack"`
		} `json:"purchases"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Purchases) != 1 || body.Purchases[0].Pack.ID != "pet_history" || body.Purchases[0].Pack.Status != "withdrawn" {
		t.Fatalf("purchases=%+v", body.Purchases)
	}
}

func TestGetPetStoreAccountHidesDeletedOwnUploads(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	userSvc := skillmarket.NewUserService(store, nil)
	authSvc := skillmarket.NewAuthService(store, nil, "")
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, UserSvc: userSvc, AuthSvc: authSvc, CreditsSvc: skillmarket.NewCreditsService(store), DataDir: t.TempDir()})
	ctx := context.Background()
	creator, err := userSvc.EnsureAccountWithID(ctx, "creator-uploads", "creator-uploads@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := authSvc.CreateSessionForUser(ctx, creator.ID, creator.Email)
	if err != nil {
		t.Fatal(err)
	}
	h.ensurePetStoreSchema()
	now := time.Now().UTC().Format(time.RFC3339)
	for _, pack := range [][]any{
		{"pet_upload_active", creator.ID, creator.Email, "upload-active", "Visible upload", "", "1.0.0", 0, "active", "unused.zip", 1, 0, 0, 0, now, now},
		{"pet_upload_deleted", creator.ID, creator.Email, "upload-deleted", "Deleted upload", "", "1.0.0", 0, "deleted", "unused.zip", 1, 0, 0, 0, now, now},
	} {
		if _, err := db.Exec(`INSERT INTO sm_pet_store_packs (`+petStorePackColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, pack...); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pet-store/account", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	rec := httptest.NewRecorder()
	h.GetPetStoreAccount(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("account status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Uploads []struct {
			ID string `json:"id"`
		} `json:"uploads"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Uploads) != 1 || body.Uploads[0].ID != "pet_upload_active" {
		t.Fatalf("uploads=%+v, want only active listing", body.Uploads)
	}

	mineReq := httptest.NewRequest(http.MethodGet, "/api/v1/pet-store/packs/mine", nil)
	mineReq.Header.Set("Authorization", "Bearer "+session.Token)
	mineRec := httptest.NewRecorder()
	h.ListMyPetStorePacks(mineRec, mineReq)
	if mineRec.Code != http.StatusOK || strings.Contains(mineRec.Body.String(), "pet_upload_deleted") || !strings.Contains(mineRec.Body.String(), "pet_upload_active") {
		t.Fatalf("my packs must hide deleted upload: status=%d body=%s", mineRec.Code, mineRec.Body.String())
	}
}

func TestPetStoreArchiveAcceptsV2SkeletonPack(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	var pngBuf bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 64, G: 120, B: 180, A: 255})
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"pet-pack.yaml": `schema_version: 2
id: v2-crab
name: V2 Crab
renderer: native-skeleton
assets:
  native:
    idle: native/idle.png
  rig:
    definition: rig/crab.pet-rig.json
    textures: [rig/shell.png]
`,
		"rig/crab.pet-rig.json": `{"version":1,"bones":[{"name":"root","x":128,"y":128}],"slots":[{"name":"shell","bone":"root","texture":"rig/shell.png"}],"clips":{"idle":{"duration_ms":1000,"loop":true,"tracks":{"root":[{"at_ms":0},{"at_ms":1000,"y":-2,"ease":"ease-in-out"}]}}}}`,
	}
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	texture, err := zw.Create("rig/shell.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := texture.Write(pngBuf.Bytes()); err != nil {
		t.Fatal(err)
	}
	idle, err := zw.Create("native/idle.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := idle.Write(pngBuf.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	id, err := petStoreArchiveSourcePackID(buf.Bytes())
	if err != nil {
		t.Fatalf("v2 skeleton pack should be accepted: %v", err)
	}
	if id != "v2-crab" {
		t.Fatalf("id=%q", id)
	}
}

func TestPetStoreArchiveValidatesV3CharacterPerformancePack(t *testing.T) {
	valid := petStoreV3CharacterArchive(t, false)
	id, err := petStoreArchiveSourcePackID(valid)
	if err != nil {
		t.Fatalf("v3 character pack should be accepted: %v", err)
	}
	if id != "v3-lamp" {
		t.Fatalf("id=%q", id)
	}

	invalid := petStoreV3CharacterArchive(t, true)
	if _, err := petStoreArchiveSourcePackID(invalid); err == nil {
		t.Fatal("v3 character pack with an unknown reaction clip was accepted")
	}
}

// petStoreV3CharacterArchive deliberately builds the complete public v3
// contract. It proves the market accepts new performance packs and applies the
// same clip-reference checks as desktop discovery before a listing is stored.
func petStoreV3CharacterArchive(t *testing.T, invalidReaction bool) []byte {
	t.Helper()
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	clipNames := []string{
		"idle_in", "idle_loop", "idle_out", "listen_in", "listen_loop", "listen_out", "think_in", "think_loop", "think_out",
		"speak_in", "speak_loop", "speak_out", "done_in", "done_loop", "done_out", "alert_in", "alert_loop", "alert_out", "quiet_in", "quiet_loop", "quiet_out", "expr", "gaze", "react",
	}
	clips := make(map[string]petpack.RigClip, len(clipNames))
	for _, name := range clipNames {
		clips[name] = petpack.RigClip{DurationMS: 1000, Loop: true, Tracks: map[string][]petpack.RigKeyframe{"root": {{AtMS: 0}, {AtMS: 1000}}}}
	}
	for i := 0; i < 6; i++ {
		clips[fmt.Sprintf("expr_%d", i)] = petpack.RigClip{DurationMS: 1000, Loop: true, Tracks: map[string][]petpack.RigKeyframe{"root": {{AtMS: 0}, {AtMS: 1000}}}}
	}
	for i := 0; i < 4; i++ {
		clips[fmt.Sprintf("gaze_%d", i)] = petpack.RigClip{DurationMS: 1000, Loop: true, Tracks: map[string][]petpack.RigKeyframe{"root": {{AtMS: 0}, {AtMS: 1000}}}}
	}
	rig, err := json.Marshal(petpack.Rig{
		Version: 1,
		Bones:   []petpack.RigBone{{Name: "root"}},
		Slots:   []petpack.RigSlot{{Name: "body", Bone: "root", Texture: "rig/body.png"}},
		Clips:   clips,
	})
	if err != nil {
		t.Fatal(err)
	}
	performer := petStoreV3Performer()
	if invalidReaction {
		performer.Reactions["reaction_0"] = petpack.PerformerReaction{Event: "click", Play: "missing_clip", Interrupt: "soft", CooldownMS: 500, Weight: 1}
	}
	performerJSON, err := json.Marshal(performer)
	if err != nil {
		t.Fatal(err)
	}

	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	write := func(name string, data []byte) {
		t.Helper()
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	write("pet-pack.yaml", []byte(`schema_version: 3
id: v3-lamp
name: V3 Lamp
renderer: native-character
capabilities: {pet_performance_v3: true}
assets:
  native: {idle: native/idle.png}
  rig: {definition: rig/character.rig.json, textures: [rig/body.png]}
  character: {definition: character/performer.json}
fallback: {renderer: native-skeleton, idle: native/idle.png}
`))
	write("native/idle.png", pngBuf.Bytes())
	write("rig/body.png", pngBuf.Bytes())
	write("rig/character.rig.json", rig)
	write("character/performer.json", performerJSON)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func petStoreV3Performer() petpack.Performer {
	p := petpack.Performer{
		Version:   1,
		Moods:     []string{"calm", "curious", "focused", "pleased", "concerned", "tired"},
		Layers:    []string{"body", "expression", "gaze", "secondary"},
		Behaviors: map[string]petpack.PerformerBehavior{}, States: map[string]petpack.PerformerState{}, Events: map[string]petpack.PerformerEvent{}, Reactions: map[string]petpack.PerformerReaction{},
		Rules: petpack.PerformerRules{NoRepeatLast: 3, CrossfadeMS: 180, MaxInterruptMS: 800},
	}
	idlePool := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("idle_%d", i)
		idlePool = append(idlePool, name)
		p.Behaviors[name] = petpack.PerformerBehavior{Enter: "idle_in", Loop: "idle_loop", Exit: "idle_out", Weight: 1, MinMS: 2500, MaxMS: 3500, CooldownMS: 500}
	}
	p.States["idle"] = petpack.PerformerState{BehaviorPool: idlePool, Expression: "expr_0", Gaze: "gaze_0"}
	for i, state := range []struct {
		name  string
		clips [3]string
	}{
		{"listening", [3]string{"listen_in", "listen_loop", "listen_out"}},
		{"thinking", [3]string{"think_in", "think_loop", "think_out"}},
		{"speaking", [3]string{"speak_in", "speak_loop", "speak_out"}},
		{"done", [3]string{"done_in", "done_loop", "done_out"}},
		{"alert", [3]string{"alert_in", "alert_loop", "alert_out"}},
		{"quiet", [3]string{"quiet_in", "quiet_loop", "quiet_out"}},
	} {
		p.States[state.name] = petpack.PerformerState{Enter: state.clips[0], Loop: state.clips[1], Exit: state.clips[2], Expression: fmt.Sprintf("expr_%d", (i+1)%6), Gaze: fmt.Sprintf("gaze_%d", (i+1)%4)}
	}
	events := []string{"click", "hover", "drag_start", "drag_end", "task_started", "task_done", "task_failed", "long_idle"}
	for _, event := range events {
		p.Events[event] = petpack.PerformerEvent{Play: "react", Interrupt: "soft", CooldownMS: 500}
	}
	for i := 0; i < 12; i++ {
		p.Reactions[fmt.Sprintf("reaction_%d", i)] = petpack.PerformerReaction{Event: events[i%len(events)], Play: "react", Interrupt: "soft", CooldownMS: 500, Weight: 1}
	}
	return p
}

func TestPetStorePackPreviewDataURLDecodesPNGAndCachesResult(t *testing.T) {
	resetPetStorePreviewCache()
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	var imageData bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 220, G: 70, B: 80, A: 255})
	img.SetNRGBA(1, 0, color.NRGBA{R: 70, G: 120, B: 220, A: 0})
	if err := png.Encode(&imageData, img); err != nil {
		t.Fatal(err)
	}
	w, err := zw.Create("native/idle.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(imageData.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "preview.zip")
	if err := os.WriteFile(path, archive.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	preview := petStorePackPreviewDataURL(path)
	if !strings.HasPrefix(preview, "data:image/jpeg;base64,") {
		t.Fatalf("preview=%q", preview)
	}
	if cached := petStorePackPreviewDataURL(path); cached != preview {
		t.Fatalf("cached preview differs")
	}
	encoded := strings.TrimPrefix(preview, "data:image/jpeg;base64,")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	decodedImage, _, err := image.Decode(bytes.NewReader(decoded))
	if err != nil {
		t.Fatal(err)
	}
	if got := decodedImage.Bounds().Size(); got.X != 192 || got.Y != 112 {
		t.Fatalf("preview dimensions=%v", got)
	}
}

func TestPetStorePreviewUsesManifestPreviewBeforeArchiveOrder(t *testing.T) {
	resetPetStorePreviewCache()
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	writePNG := func(name string, pixel color.NRGBA) {
		t.Helper()
		var data bytes.Buffer
		img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
		img.SetNRGBA(0, 0, pixel)
		if err := png.Encode(&data, img); err != nil {
			t.Fatal(err)
		}
		file, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(data.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	// A texture happens to be first in the ZIP. The declared cover must win.
	writePNG("rig/texture.png", color.NRGBA{R: 220, G: 20, B: 20, A: 255})
	manifest, err := zw.Create("pet-pack.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.Write([]byte("schema_version: 1\nid: cover-pet\nrenderer: native-raster\npreview: cover.png\nassets:\n  native:\n    idle: native/idle.png\n")); err != nil {
		t.Fatal(err)
	}
	writePNG("cover.png", color.NRGBA{R: 20, G: 180, B: 40, A: 255})
	writePNG("native/idle.png", color.NRGBA{R: 30, G: 70, B: 220, A: 255})
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cover.zip")
	if err := os.WriteFile(path, archive.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	preview := petStorePackPreviewDataURL(path)
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(preview, "data:image/jpeg;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	result, _, err := image.Decode(bytes.NewReader(decoded))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := result.At(96, 56).RGBA()
	if g <= r || g <= b {
		t.Fatalf("preview did not use declared cover: r=%d g=%d b=%d", r, g, b)
	}
}

func TestAdminListPetStorePacksIncludesPreviewDataURL(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, DataDir: t.TempDir()})
	h.ensurePetStoreSchema()
	archive := validPetStoreTestArchive(t, "admin-preview-pack")
	if err := os.MkdirAll(h.petStoreDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(h.petStoreDir(), "pet_admin_preview.zip")
	if err := os.WriteFile(zipPath, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	now := petStoreNow()
	if _, err := db.Exec(`INSERT INTO sm_pet_store_packs (`+petStorePackColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "pet_admin_preview", "owner-1", "owner@example.test", "admin-preview-pack", "Admin preview", "", "1.0.0", 0, "active", zipPath, len(archive), 0, 0, 0, now, now); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/pet-store/packs", nil)
	rec := httptest.NewRecorder()
	h.AdminListPetStorePacks(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Packs []struct {
			PreviewDataURL string `json:"preview_data_url"`
			OwnerEmail     string `json:"owner_email"`
		} `json:"packs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Packs) != 1 || !strings.HasPrefix(payload.Packs[0].PreviewDataURL, "data:image/jpeg;base64,") {
		t.Fatalf("admin preview=%q", payload.Packs[0].PreviewDataURL)
	}
	if payload.Packs[0].OwnerEmail != "owner@example.test" {
		t.Fatalf("admin publisher=%q", payload.Packs[0].OwnerEmail)
	}
}

func TestAdminListPetStorePacksHidesDeletedByDefaultButSupportsAuditFilter(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, DataDir: t.TempDir()})
	h.ensurePetStoreSchema()
	now := petStoreNow()
	for id, status := range map[string]string{"pet_active_listing": "active", "pet_deleted_listing": "deleted"} {
		if _, err := db.Exec(`INSERT INTO sm_pet_store_packs (`+petStorePackColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, "owner-1", "owner@example.test", id, id, "", "1.0.0", 0, status, "unused.zip", 1, 0, 0, 0, now, now); err != nil {
			t.Fatal(err)
		}
	}

	list := func(rawQuery string) string {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/pet-store/packs"+rawQuery, nil)
		rec := httptest.NewRecorder()
		h.AdminListPetStorePacks(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("list %q status=%d body=%s", rawQuery, rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	defaultList := list("")
	if !strings.Contains(defaultList, "pet_active_listing") || strings.Contains(defaultList, "pet_deleted_listing") {
		t.Fatalf("default list should hide deleted tombstones: %s", defaultList)
	}
	deletedList := list("?status=deleted")
	if !strings.Contains(deletedList, "pet_deleted_listing") || strings.Contains(deletedList, "pet_active_listing") {
		t.Fatalf("deleted audit filter should only show deleted tombstones: %s", deletedList)
	}
}

func TestPetStorePublicPackDoesNotExposePublisherEmail(t *testing.T) {
	pack := publicPetStorePack(&petStorePack{
		ID:         "pet_public",
		OwnerEmail: "creator@example.test",
		Name:       "Public pet",
		ZipPath:    "missing.zip",
	})
	payload, err := json.Marshal(pack)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(payload, []byte("creator@example.test")) || bytes.Contains(payload, []byte(`"owner"`)) {
		t.Fatalf("public payload exposes publisher identity: %s", payload)
	}
}

func TestPetStoreUserRequiresPublisherEmail(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	userSvc := skillmarket.NewUserService(store, nil)
	authSvc := skillmarket.NewAuthService(store, nil, "")
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, UserSvc: userSvc, AuthSvc: authSvc, DataDir: t.TempDir()})
	session, err := authSvc.CreateSessionForUser(context.Background(), "creator-without-email", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pet-store/packs/mine", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	if _, _, err := h.petStoreUser(req); err == nil || !strings.Contains(err.Error(), "publisher email is required") {
		t.Fatalf("petStoreUser error=%v, want publisher email requirement", err)
	}
	var users int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sm_users WHERE id=?`, "creator-without-email").Scan(&users); err != nil {
		t.Fatal(err)
	}
	if users != 0 {
		t.Fatalf("empty-email session created %d user record(s)", users)
	}
}

func TestPetStoreUserAdoptsEmailFirstAccount(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	userSvc := skillmarket.NewUserService(store, nil)
	authSvc := skillmarket.NewAuthService(store, nil, "")
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, UserSvc: userSvc, AuthSvc: authSvc, DataDir: t.TempDir()})
	legacy, err := userSvc.EnsureAccount(context.Background(), "owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := authSvc.CreateSessionForUser(context.Background(), "usr_hub", "owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pet-store/account", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	id, email, err := h.petStoreUser(req)
	if err != nil {
		t.Fatalf("petStoreUser: %v", err)
	}
	if id != legacy.ID || email != "owner@example.test" {
		t.Fatalf("petStoreUser = %s/%s, want adopted legacy account %s", id, email, legacy.ID)
	}
}

func TestPetStorePreviewAssetsPutsCoverFirst(t *testing.T) {
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	manifest, err := zw.Create("pet-pack.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(manifest, "schema_version: 1\nid: preview-first\nname: Preview first\npreview: cover.png\nrenderer: native-raster\nassets:\n  native:\n    idle: native/idle.png\n"); err != nil {
		t.Fatal(err)
	}
	first, err := zw.Create("native/idle.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Write(petStoreTestPNG(t)); err != nil {
		t.Fatal(err)
	}
	cover, err := zw.Create("cover.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cover.Write(petStoreTestPNG(t)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	assets, err := petStorePreviewAssets(archive.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) < 2 || assets[0]["name"] != "cover.png" || assets[1]["name"] != "native/idle.png" {
		t.Fatalf("preview order=%v", assets)
	}
}

func TestPetStorePreviewAssetsSupportsJPEGAndCapsResults(t *testing.T) {
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	var jpegData bytes.Buffer
	if err := jpeg.Encode(&jpegData, image.NewNRGBA(image.Rect(0, 0, 1, 1)), nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 13; i++ {
		name := fmt.Sprintf("preview-%02d.png", i)
		if i == 0 {
			name = "cover.jpg"
		}
		file, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			_, err = file.Write(jpegData.Bytes())
		} else {
			_, err = file.Write(petStoreTestPNG(t))
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	assets, err := petStorePreviewAssets(archive.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 12 {
		t.Fatalf("preview count=%d, want 12", len(assets))
	}
	if assets[0]["mime"] != "image/jpeg" || !strings.HasPrefix(assets[0]["data_url"], "data:image/jpeg;base64,") {
		t.Fatalf("jpeg preview=%+v", assets[0])
	}
}

func TestPetStoreArchiveRejectsV2SkeletonMissingTexture(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"pet-pack.yaml": `schema_version: 2
id: invalid-v2-crab
name: Invalid V2 Crab
renderer: native-skeleton
assets:
  native: {idle: native/idle.png}
  rig: {definition: rig/crab.pet-rig.json, textures: [rig/missing.png]}
`,
		"rig/crab.pet-rig.json": `{"version":1,"bones":[{"name":"root"}],"slots":[{"name":"shell","bone":"root","texture":"rig/missing.png"}],"clips":{}}`,
	}
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	idle, err := zw.Create("native/idle.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := idle.Write(petStoreTestPNG(t)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := petStoreArchiveSourcePackID(buf.Bytes()); err == nil {
		t.Fatal("expected missing v2 rig texture rejection")
	}
}

func TestPetStoreArchiveRejectsDuplicatePath(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, manifest := range []string{
		"schema_version: 1\nid: duplicate-pet\nrenderer: native-raster\nassets:\n  native:\n    idle: native/idle.png\n",
		"schema_version: 1\nid: different-pet\nrenderer: native-raster\nassets:\n  native:\n    idle: native/idle.png\n",
	} {
		w, err := zw.Create("pet-pack.yaml")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(manifest)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := petStoreArchiveSourcePackID(buf.Bytes()); err == nil {
		t.Fatal("expected duplicate archive path rejection")
	}
}

func TestPetStoreArchiveValidatesInheritedSkeletonVariant(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"pet-pack.yaml": `schema_version: 2
id: inherited-v2-crab
name: Inherited V2 Crab
renderer: native-skeleton
assets:
  native: {idle: native/idle.png}
  rig: {definition: rig/root.json, textures: [rig/root.png]}
variants:
  - id: default
    assets:
      rig: {definition: rig/default.json, textures: [rig/missing.png]}
`,
		"rig/root.json":    `{"version":1,"bones":[{"name":"root"}],"slots":[{"name":"shell","bone":"root","texture":"rig/root.png"}],"clips":{}}`,
		"rig/default.json": `{"version":1,"bones":[{"name":"root"}],"slots":[{"name":"shell","bone":"root","texture":"rig/missing.png"}],"clips":{}}`,
	}
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	idle, err := zw.Create("native/idle.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := idle.Write(petStoreTestPNG(t)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := petStoreArchiveSourcePackID(buf.Bytes()); err == nil {
		t.Fatal("expected inherited skeleton variant texture rejection")
	}
}

func petStoreTestPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestSubmitPetStorePackPersistsSourcePackID(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatalf("new skillmarket store: %v", err)
	}
	userSvc := skillmarket.NewUserService(store, nil)
	authSvc := skillmarket.NewAuthService(store, nil, "")
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, UserSvc: userSvc, AuthSvc: authSvc, CreditsSvc: skillmarket.NewCreditsService(store), DataDir: t.TempDir()})
	ctx := context.Background()
	user, err := userSvc.EnsureAccountWithID(ctx, "creator-1", "creator@example.test")
	if err != nil {
		t.Fatalf("ensure creator: %v", err)
	}
	session, err := authSvc.CreateSessionForUser(ctx, user.ID, user.Email)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("name", "Creator pet"); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("price", "0"); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("source_pack_id", "creator-pet"); err != nil {
		t.Fatal(err)
	}
	part, err := mw.CreateFormFile("zip", "creator-pet.zip")
	if err != nil {
		t.Fatal(err)
	}
	archive := validPetStoreTestArchive(t)
	if _, err := part.Write(archive); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pet-store/packs", &body)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.SubmitPetStorePack(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response petStorePack
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode submit response: %v", err)
	}
	if response.SourcePackID != "creator-pet" {
		t.Fatalf("response source_pack_id=%q", response.SourcePackID)
	}
	var stored string
	if err := db.QueryRow(`SELECT source_pack_id FROM sm_pet_store_packs WHERE id=?`, response.ID).Scan(&stored); err != nil {
		t.Fatalf("read stored source pack id: %v", err)
	}
	if stored != "creator-pet" {
		t.Fatalf("stored source_pack_id=%q", stored)
	}

	duplicateReq := newPetStoreSubmitRequest(t, session.Token, "creator-pet")
	duplicateRec := httptest.NewRecorder()
	h.SubmitPetStorePack(duplicateRec, duplicateReq)
	if duplicateRec.Code != http.StatusConflict {
		t.Fatalf("duplicate submit status=%d body=%s, want conflict", duplicateRec.Code, duplicateRec.Body.String())
	}
}

func TestPetStorePackIDCannotBeClaimedByAnotherCreator(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatalf("new skillmarket store: %v", err)
	}
	userSvc := skillmarket.NewUserService(store, nil)
	authSvc := skillmarket.NewAuthService(store, nil, "")
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, UserSvc: userSvc, AuthSvc: authSvc, CreditsSvc: skillmarket.NewCreditsService(store), DataDir: t.TempDir()})
	ctx := context.Background()
	owner, err := userSvc.EnsureAccountWithID(ctx, "owner-1", "owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	other, err := userSvc.EnsureAccountWithID(ctx, "other-1", "other@example.test")
	if err != nil {
		t.Fatal(err)
	}
	ownerSession, err := authSvc.CreateSessionForUser(ctx, owner.ID, owner.Email)
	if err != nil {
		t.Fatal(err)
	}
	otherSession, err := authSvc.CreateSessionForUser(ctx, other.ID, other.Email)
	if err != nil {
		t.Fatal(err)
	}

	first := httptest.NewRecorder()
	h.SubmitPetStorePack(first, newPetStoreSubmitRequest(t, ownerSession.Token, "shared-pack"))
	if first.Code != http.StatusCreated {
		t.Fatalf("owner submit status=%d body=%s", first.Code, first.Body.String())
	}

	check := httptest.NewRequest(http.MethodGet, "/api/v1/pet-store/packs/source/shared-pack/publishability", nil)
	check.SetPathValue("sourcePackID", "shared-pack")
	check.Header.Set("Authorization", "Bearer "+otherSession.Token)
	checkRec := httptest.NewRecorder()
	h.CanPublishPetStorePack(checkRec, check)
	if checkRec.Code != http.StatusOK || !bytes.Contains(checkRec.Body.Bytes(), []byte(`"can_publish":false`)) {
		t.Fatalf("publishability status=%d body=%s", checkRec.Code, checkRec.Body.String())
	}

	second := httptest.NewRecorder()
	h.SubmitPetStorePack(second, newPetStoreSubmitRequest(t, otherSession.Token, "shared-pack"))
	if second.Code != http.StatusConflict {
		t.Fatalf("other creator submit status=%d body=%s, want conflict", second.Code, second.Body.String())
	}

	ownerCheck := httptest.NewRequest(http.MethodGet, "/api/v1/pet-store/packs/source/shared-pack/publishability", nil)
	ownerCheck.SetPathValue("sourcePackID", "shared-pack")
	ownerCheck.Header.Set("Authorization", "Bearer "+ownerSession.Token)
	ownerCheckRec := httptest.NewRecorder()
	h.CanPublishPetStorePack(ownerCheckRec, ownerCheck)
	if ownerCheckRec.Code != http.StatusOK || !bytes.Contains(ownerCheckRec.Body.Bytes(), []byte(`"can_publish":true`)) {
		t.Fatalf("owner publishability status=%d body=%s", ownerCheckRec.Code, ownerCheckRec.Body.String())
	}
}

func TestPetStoreSchemaRejectsConcurrentActiveClaimsForSamePackID(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatalf("new skillmarket store: %v", err)
	}
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, DataDir: t.TempDir()})
	h.ensurePetStoreSchema()
	now := time.Now().UTC().Format(time.RFC3339)
	insert := `INSERT INTO sm_pet_store_packs (` + petStorePackColumns + `) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := db.Exec(insert, "pet_owner", "owner-1", "owner@example.test", "shared-pack", "Owner", "", "1.0.0", 0, "active", "owner.zip", 1, 0, 0, 0, now, now); err != nil {
		t.Fatalf("seed owner listing: %v", err)
	}
	if _, err := db.Exec(insert, "pet_other", "other-1", "other@example.test", "shared-pack", "Other", "", "1.0.0", 0, "active", "other.zip", 1, 0, 0, 0, now, now); err == nil {
		t.Fatal("second active creator claim should violate the market-wide source ID index")
	}
	if _, err := db.Exec(`UPDATE sm_pet_store_packs SET status='withdrawn' WHERE id='pet_owner'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(insert, "pet_other", "other-1", "other@example.test", "shared-pack", "Other", "", "1.0.0", 0, "active", "other.zip", 1, 0, 0, 0, now, now); err != nil {
		t.Fatalf("a withdrawn listing must release the active listing slot: %v", err)
	}
}

func TestWithdrawnPetStorePackKeepsCreatorIdentityClaim(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	userSvc := skillmarket.NewUserService(store, nil)
	authSvc := skillmarket.NewAuthService(store, nil, "")
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, UserSvc: userSvc, AuthSvc: authSvc, CreditsSvc: skillmarket.NewCreditsService(store), DataDir: t.TempDir()})
	ctx := context.Background()
	owner, err := userSvc.EnsureAccountWithID(ctx, "withdraw-owner", "withdraw-owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	other, err := userSvc.EnsureAccountWithID(ctx, "withdraw-other", "withdraw-other@example.test")
	if err != nil {
		t.Fatal(err)
	}
	ownerSession, err := authSvc.CreateSessionForUser(ctx, owner.ID, owner.Email)
	if err != nil {
		t.Fatal(err)
	}
	otherSession, err := authSvc.CreateSessionForUser(ctx, other.ID, other.Email)
	if err != nil {
		t.Fatal(err)
	}
	h.ensurePetStoreSchema()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO sm_pet_store_packs (`+petStorePackColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "withdrawn-claim", owner.ID, owner.Email, "durable-pack", "Durable pet", "", "1.0.0", 0, "withdrawn", "unused.zip", 1, 0, 0, 0, now, now); err != nil {
		t.Fatal(err)
	}
	canPublish := func(session string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/pet-store/packs/source/durable-pack/publishability", nil)
		req.SetPathValue("sourcePackID", "durable-pack")
		req.Header.Set("Authorization", "Bearer "+session)
		rec := httptest.NewRecorder()
		h.CanPublishPetStorePack(rec, req)
		return rec
	}
	if rec := canPublish(ownerSession.Token); rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"can_publish":true`)) {
		t.Fatalf("owner should re-publish after withdrawal: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := canPublish(otherSession.Token); rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"can_publish":false`)) {
		t.Fatalf("withdrawn identity must remain unavailable to another creator: status=%d body=%s", rec.Code, rec.Body.String())
	}
	otherSubmit := httptest.NewRecorder()
	h.SubmitPetStorePack(otherSubmit, newPetStoreSubmitRequest(t, otherSession.Token, "durable-pack"))
	if otherSubmit.Code != http.StatusConflict {
		t.Fatalf("another creator must not publish a withdrawn identity: status=%d body=%s", otherSubmit.Code, otherSubmit.Body.String())
	}
}

func TestDeletedPetStorePackAllowsOriginalCreatorToRepublish(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	userSvc := skillmarket.NewUserService(store, nil)
	authSvc := skillmarket.NewAuthService(store, nil, "")
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, UserSvc: userSvc, AuthSvc: authSvc, CreditsSvc: skillmarket.NewCreditsService(store), DataDir: t.TempDir()})
	ctx := context.Background()
	owner, err := userSvc.EnsureAccountWithID(ctx, "deleted-owner", "deleted-owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	other, err := userSvc.EnsureAccountWithID(ctx, "deleted-other", "deleted-other@example.test")
	if err != nil {
		t.Fatal(err)
	}
	ownerSession, err := authSvc.CreateSessionForUser(ctx, owner.ID, owner.Email)
	if err != nil {
		t.Fatal(err)
	}
	otherSession, err := authSvc.CreateSessionForUser(ctx, other.ID, other.Email)
	if err != nil {
		t.Fatal(err)
	}
	h.ensurePetStoreSchema()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO sm_pet_store_packs (`+petStorePackColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "deleted-claim", owner.ID, owner.Email, "removed-pack", "Removed pet", "", "1.0.0", 0, "deleted", "unused.zip", 1, 0, 0, 0, now, now); err != nil {
		t.Fatal(err)
	}
	canPublish := func(session string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/pet-store/packs/source/removed-pack/publishability", nil)
		req.SetPathValue("sourcePackID", "removed-pack")
		req.Header.Set("Authorization", "Bearer "+session)
		rec := httptest.NewRecorder()
		h.CanPublishPetStorePack(rec, req)
		return rec
	}
	if rec := canPublish(ownerSession.Token); rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"can_publish":true`)) {
		t.Fatalf("original creator should re-publish a removed identity: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := canPublish(otherSession.Token); rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"can_publish":false`)) {
		t.Fatalf("another creator must not reuse a removed identity: status=%d body=%s", rec.Code, rec.Body.String())
	}
	ownerSubmit := httptest.NewRecorder()
	h.SubmitPetStorePack(ownerSubmit, newPetStoreSubmitRequest(t, ownerSession.Token, "removed-pack"))
	if ownerSubmit.Code != http.StatusCreated {
		t.Fatalf("original creator should submit a removed identity: status=%d body=%s", ownerSubmit.Code, ownerSubmit.Body.String())
	}
	otherSubmit := httptest.NewRecorder()
	h.SubmitPetStorePack(otherSubmit, newPetStoreSubmitRequest(t, otherSession.Token, "removed-pack"))
	if otherSubmit.Code != http.StatusConflict {
		t.Fatalf("another creator must not submit a removed identity: status=%d body=%s", otherSubmit.Code, otherSubmit.Body.String())
	}
}

func TestPetStoreSchemaRepairsLegacyDuplicateActivePackIDs(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatalf("new skillmarket store: %v", err)
	}
	// Model a database created before the market-wide source ID index existed.
	if _, err := db.Exec(`CREATE TABLE sm_pet_store_packs (
		id TEXT PRIMARY KEY, owner_user_id TEXT NOT NULL, owner_email TEXT NOT NULL, source_pack_id TEXT NOT NULL DEFAULT '',
		name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', version TEXT NOT NULL DEFAULT '1.0.0',
		price INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'active', zip_path TEXT NOT NULL,
		package_size INTEGER NOT NULL DEFAULT 0, download_count INTEGER NOT NULL DEFAULT 0,
		purchase_count INTEGER NOT NULL DEFAULT 0, sales_amount INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	older := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	newer := time.Now().UTC().Format(time.RFC3339)
	insert := `INSERT INTO sm_pet_store_packs (` + petStorePackColumns + `) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	for _, pack := range [][]any{
		{"pet_old", "owner-1", "owner@example.test", "shared-pack", "Old", "", "1.0.0", int64(0), "active", "old.zip", int64(1), int64(0), int64(0), int64(0), older, older},
		{"pet_new", "owner-2", "other@example.test", "shared-pack", "New", "", "1.0.0", int64(0), "active", "new.zip", int64(1), int64(0), int64(0), int64(0), newer, newer},
	} {
		if _, err := db.Exec(insert, pack...); err != nil {
			t.Fatal(err)
		}
	}
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, DataDir: t.TempDir()})
	h.ensurePetStoreSchema()
	var status string
	if err := db.QueryRow(`SELECT status FROM sm_pet_store_packs WHERE id='pet_old'`).Scan(&status); err != nil || status != "withdrawn" {
		t.Fatalf("legacy loser status=%q err=%v", status, err)
	}
	if err := db.QueryRow(`SELECT status FROM sm_pet_store_packs WHERE id='pet_new'`).Scan(&status); err != nil || status != "active" {
		t.Fatalf("legacy winner status=%q err=%v", status, err)
	}
}

func TestPetStoreSnapshotClaimTieBreaker(t *testing.T) {
	older := "2026-07-31T10:00:00Z"
	newer := "2026-07-31T10:01:00Z"
	if !petStoreSnapshotClaimWins("pet_new", newer, "pet_old", older) {
		t.Fatal("newer snapshot should win")
	}
	if petStoreSnapshotClaimWins("pet_old", older, "pet_new", newer) {
		t.Fatal("older snapshot should not win")
	}
	if !petStoreSnapshotClaimWins("pet_b", older, "pet_a", older) {
		t.Fatal("greater listing ID should win equal timestamp tie")
	}
	if petStoreSnapshotClaimWins("pet_a", older, "pet_b", older) {
		t.Fatal("smaller listing ID should lose equal timestamp tie")
	}
}

func TestApplyPetStoreSnapshotDoesNotReplaceNewerArchiveWithOlderSnapshot(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatalf("new skillmarket store: %v", err)
	}
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, DataDir: t.TempDir()})

	newerAt := time.Now().UTC().Truncate(time.Second)
	olderAt := newerAt.Add(-time.Minute)
	newerArchive := validPetStoreTestArchiveWithMarker(t, "creator-pet", "new")
	olderArchive := validPetStoreTestArchiveWithMarker(t, "creator-pet", "old")
	apply := func(name string, updatedAt time.Time, archive []byte) {
		t.Helper()
		raw, err := json.Marshal(petStoreSnapshot{Packs: []petStoreSnapshotPack{{
			ID: "pet_ha_archive", OwnerID: "creator-1", OwnerEmail: "creator@example.test",
			SourcePackID: "creator-pet", Name: name, Version: "1.0.0", Price: 0,
			Status: "active", PackageSize: int64(len(archive)), CreatedAt: olderAt.Format(time.RFC3339),
			UpdatedAt: updatedAt.Format(time.RFC3339), ZipBase64: base64.StdEncoding.EncodeToString(archive),
		}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.ApplyPetStoreSnapshot(context.Background(), raw); err != nil {
			t.Fatalf("apply snapshot: %v", err)
		}
	}
	apply("Newer pet", newerAt, newerArchive)
	apply("Older pet", olderAt, olderArchive)

	data, err := os.ReadFile(filepath.Join(h.petStoreDir(), "pet_ha_archive.zip"))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if !bytes.Equal(data, newerArchive) {
		t.Fatal("older HA snapshot replaced the newer archive")
	}
	var name, updatedAt string
	if err := db.QueryRow(`SELECT name, updated_at FROM sm_pet_store_packs WHERE id='pet_ha_archive'`).Scan(&name, &updatedAt); err != nil {
		t.Fatal(err)
	}
	if name != "Newer pet" || updatedAt != newerAt.Format(time.RFC3339) {
		t.Fatalf("listing regressed to name=%q updated_at=%q", name, updatedAt)
	}
}

func TestPausedPetStorePackIsHiddenButBuyerKeepsDownload(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	userSvc := skillmarket.NewUserService(store, nil)
	authSvc := skillmarket.NewAuthService(store, nil, "")
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, UserSvc: userSvc, AuthSvc: authSvc, CreditsSvc: skillmarket.NewCreditsService(store), DataDir: t.TempDir()})
	ctx := context.Background()
	owner, err := userSvc.EnsureAccountWithID(ctx, "owner-1", "owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	buyer, err := userSvc.EnsureAccountWithID(ctx, "buyer-1", "buyer@example.test")
	if err != nil {
		t.Fatal(err)
	}
	buyerSession, err := authSvc.CreateSessionForUser(ctx, buyer.ID, buyer.Email)
	if err != nil {
		t.Fatal(err)
	}

	h.ensurePetStoreSchema()
	archive := validPetStoreTestArchive(t, "paused-pack")
	zipPath := filepath.Join(h.petStoreDir(), "pet_paused.zip")
	if err := os.MkdirAll(h.petStoreDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(zipPath, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO sm_pet_store_packs (`+petStorePackColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "pet_paused", owner.ID, owner.Email, "paused-pack", "Paused pet", "", "1.0.0", 0, "paused", zipPath, len(archive), 0, 0, 0, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sm_pet_store_purchases (id, pack_id, buyer_user_id, buyer_email, amount_paid, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "purchase_paused", "pet_paused", buyer.ID, buyer.Email, 0, "active", now); err != nil {
		t.Fatal(err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/pet-store/packs", nil)
	listRec := httptest.NewRecorder()
	h.ListPetStorePacks(listRec, listReq)
	if listRec.Code != http.StatusOK || bytes.Contains(listRec.Body.Bytes(), []byte("pet_paused")) {
		t.Fatalf("paused listing should be hidden: status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/pet-store/packs/pet_paused", nil)
	getReq.SetPathValue("id", "pet_paused")
	getRec := httptest.NewRecorder()
	h.GetPetStorePack(getRec, getReq)
	if getRec.Code != http.StatusNotFound {
		t.Fatalf("paused listing detail status=%d body=%s, want 404", getRec.Code, getRec.Body.String())
	}
	accountReq := httptest.NewRequest(http.MethodGet, "/api/v1/pet-store/account", nil)
	accountReq.Header.Set("Authorization", "Bearer "+buyerSession.Token)
	accountRec := httptest.NewRecorder()
	h.GetPetStoreAccount(accountRec, accountReq)
	if accountRec.Code != http.StatusOK || !bytes.Contains(accountRec.Body.Bytes(), []byte(`"id":"pet_paused"`)) || !bytes.Contains(accountRec.Body.Bytes(), []byte(`"status":"paused"`)) {
		t.Fatalf("paused purchase should remain visible as unavailable history: status=%d body=%s", accountRec.Code, accountRec.Body.String())
	}
	downloadReq := httptest.NewRequest(http.MethodGet, "/api/v1/pet-store/packs/pet_paused/download", nil)
	downloadReq.SetPathValue("id", "pet_paused")
	downloadReq.Header.Set("Authorization", "Bearer "+buyerSession.Token)
	downloadRec := httptest.NewRecorder()
	h.DownloadPetStorePack(downloadRec, downloadReq)
	// A pause only stops new sales; the buyer's existing entitlement survives it.
	if downloadRec.Code != http.StatusOK {
		t.Fatalf("paused download status=%d body=%s, want 200 for entitled buyer", downloadRec.Code, downloadRec.Body.String())
	}

	resumeReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/pet-store/packs/pet_paused/resume", nil)
	resumeReq.SetPathValue("id", "pet_paused")
	resumeRec := httptest.NewRecorder()
	h.AdminResumePetStorePack(resumeRec, resumeReq)
	if resumeRec.Code != http.StatusOK {
		t.Fatalf("resume status=%d body=%s", resumeRec.Code, resumeRec.Body.String())
	}
	getRec = httptest.NewRecorder()
	h.GetPetStorePack(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("resumed listing detail status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	accountRec = httptest.NewRecorder()
	h.GetPetStoreAccount(accountRec, accountReq)
	if accountRec.Code != http.StatusOK || !bytes.Contains(accountRec.Body.Bytes(), []byte("pet_paused")) {
		t.Fatalf("resumed listing should appear in purchased browser: status=%d body=%s", accountRec.Code, accountRec.Body.String())
	}
	downloadRec = httptest.NewRecorder()
	h.DownloadPetStorePack(downloadRec, downloadReq)
	if downloadRec.Code != http.StatusOK {
		t.Fatalf("resumed download status=%d body=%s", downloadRec.Code, downloadRec.Body.String())
	}
	// Both deliveries are recorded as events, but the catalogue counter tracks
	// distinct downloaders, so a repeat download by the same buyer must not
	// inflate download_count.
	var downloadEvents int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sm_pet_store_downloads WHERE pack_id='pet_paused' AND downloader_user_id=?`, buyer.ID).Scan(&downloadEvents); err != nil || downloadEvents != 2 {
		t.Fatalf("download events=%d err=%v, want two successful delivery events", downloadEvents, err)
	}
	var downloadCount int64
	if err := db.QueryRow(`SELECT download_count FROM sm_pet_store_packs WHERE id='pet_paused'`).Scan(&downloadCount); err != nil || downloadCount != 1 {
		t.Fatalf("download count=%d err=%v, want one distinct downloader", downloadCount, err)
	}
}

func TestDownloadPetStorePackChecksStatusAgainBeforeServing(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	userSvc := skillmarket.NewUserService(store, nil)
	authSvc := skillmarket.NewAuthService(store, nil, "")
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, UserSvc: userSvc, AuthSvc: authSvc, CreditsSvc: skillmarket.NewCreditsService(store), DataDir: t.TempDir()})
	owner, err := userSvc.EnsureAccountWithID(context.Background(), "owner-1", "owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := authSvc.CreateSessionForUser(context.Background(), owner.ID, owner.Email)
	if err != nil {
		t.Fatal(err)
	}
	h.ensurePetStoreSchema()
	archive := validPetStoreTestArchive(t, "race-pack")
	zipPath := filepath.Join(h.petStoreDir(), "pet_race.zip")
	if err := os.MkdirAll(h.petStoreDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(zipPath, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	now := petStoreNow()
	if _, err := db.Exec(`INSERT INTO sm_pet_store_packs (`+petStorePackColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "pet_race", owner.ID, owner.Email, "race-pack", "Race pet", "", "1.0.0", 0, "active", zipPath, len(archive), 0, 0, 0, now, now); err != nil {
		t.Fatal(err)
	}
	// This mimics an administrative deletion after authorization but before the
	// archive can be sent. Terminal moderation states must still be refused even
	// though withdrawal and pause no longer revoke entitlements.
	if _, err := db.Exec(`UPDATE sm_pet_store_packs SET status='deleted', updated_at=? WHERE id='pet_race'`, petStoreNow()); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pet-store/packs/pet_race/download", nil)
	req.SetPathValue("id", "pet_race")
	req.Header.Set("Authorization", "Bearer "+session.Token)
	rec := httptest.NewRecorder()
	h.DownloadPetStorePack(rec, req)
	if rec.Code != http.StatusGone {
		t.Fatalf("download status=%d body=%s, want 410", rec.Code, rec.Body.String())
	}
	var downloads int64
	if err := db.QueryRow(`SELECT download_count FROM sm_pet_store_packs WHERE id='pet_race'`).Scan(&downloads); err != nil {
		t.Fatal(err)
	}
	if downloads != 0 {
		t.Fatalf("download count=%d, want 0", downloads)
	}
}

func TestPausedPetStorePackRetainsItsSourceIDClaim(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	userSvc := skillmarket.NewUserService(store, nil)
	authSvc := skillmarket.NewAuthService(store, nil, "")
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, UserSvc: userSvc, AuthSvc: authSvc, CreditsSvc: skillmarket.NewCreditsService(store), DataDir: t.TempDir()})
	owner, err := userSvc.EnsureAccountWithID(context.Background(), "owner-1", "owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	other, err := userSvc.EnsureAccountWithID(context.Background(), "other-1", "other@example.test")
	if err != nil {
		t.Fatal(err)
	}
	otherSession, err := authSvc.CreateSessionForUser(context.Background(), other.ID, other.Email)
	if err != nil {
		t.Fatal(err)
	}

	h.ensurePetStoreSchema()
	archive := validPetStoreTestArchive(t, "reserved-pack")
	zipPath := filepath.Join(h.petStoreDir(), "pet_reserved.zip")
	if err := os.MkdirAll(h.petStoreDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(zipPath, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO sm_pet_store_packs (`+petStorePackColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "pet_reserved", owner.ID, owner.Email, "reserved-pack", "Reserved pet", "", "1.0.0", 0, "paused", zipPath, len(archive), 0, 0, 0, now, now); err != nil {
		t.Fatal(err)
	}

	checkReq := httptest.NewRequest(http.MethodGet, "/api/v1/pet-store/packs/source/reserved-pack/publishability", nil)
	checkReq.SetPathValue("sourcePackID", "reserved-pack")
	checkReq.Header.Set("Authorization", "Bearer "+otherSession.Token)
	checkRec := httptest.NewRecorder()
	h.CanPublishPetStorePack(checkRec, checkReq)
	if checkRec.Code != http.StatusOK || !bytes.Contains(checkRec.Body.Bytes(), []byte(`"can_publish":false`)) {
		t.Fatalf("paused claim should prevent another creator publishing: status=%d body=%s", checkRec.Code, checkRec.Body.String())
	}

	resumeReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/pet-store/packs/pet_reserved/resume", nil)
	resumeReq.SetPathValue("id", "pet_reserved")
	resumeRec := httptest.NewRecorder()
	h.AdminResumePetStorePack(resumeRec, resumeReq)
	if resumeRec.Code != http.StatusOK {
		t.Fatalf("resume retained listing status=%d body=%s", resumeRec.Code, resumeRec.Body.String())
	}
}

type petStoreDeleteMailer struct {
	sends int
}

func (m *petStoreDeleteMailer) Send(_ context.Context, _ []string, _ string, _ string) error {
	m.sends++
	return nil
}

func (*petStoreDeleteMailer) SendHubRegistrationConfirmation(context.Context, string, string, string) error {
	return nil
}

func TestAdminDeletePetStorePackIsIdempotent(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	mailer := &petStoreDeleteMailer{}
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, DataDir: t.TempDir(), PetStoreMailer: mailer})
	h.ensurePetStoreSchema()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO sm_pet_store_packs (`+petStorePackColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "pet_delete_once", "owner-1", "owner@example.test", "delete-once", "Delete once", "", "1.0.0", 0, "active", "unused.zip", 1, 0, 0, 0, now, now); err != nil {
		t.Fatal(err)
	}

	deleteListing := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/pet-store/packs/pet_delete_once", nil)
		req.SetPathValue("id", "pet_delete_once")
		rec := httptest.NewRecorder()
		h.AdminDeletePetStorePack(rec, req)
		return rec
	}
	if rec := deleteListing(); rec.Code != http.StatusOK {
		t.Fatalf("first delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := deleteListing(); rec.Code != http.StatusOK {
		t.Fatalf("repeated delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	if mailer.sends != 1 {
		t.Fatalf("mailer sends=%d, want 1", mailer.sends)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM sm_pet_store_packs WHERE id='pet_delete_once'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "deleted" {
		t.Fatalf("status=%q, want deleted", status)
	}
}

func TestAdminPurgePetStorePackRemovesDeletedListingAndArchive(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, DataDir: t.TempDir()})
	h.ensurePetStoreSchema()
	archive := validPetStoreTestArchive(t, "purge-pack")
	if err := os.MkdirAll(h.petStoreDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(h.petStoreDir(), "pet_purge_once.zip")
	if err := os.WriteFile(zipPath, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	now := petStoreNow()
	if _, err := db.Exec(`INSERT INTO sm_pet_store_packs (`+petStorePackColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "pet_purge_once", "owner-1", "owner@example.test", "purge-pack", "Purge once", "", "1.0.0", 0, "deleted", zipPath, len(archive), 0, 0, 0, now, now); err != nil {
		t.Fatal(err)
	}

	purge := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/pet-store/packs/pet_purge_once/purge", nil)
		req.SetPathValue("id", "pet_purge_once")
		rec := httptest.NewRecorder()
		h.AdminPurgePetStorePack(rec, req)
		return rec
	}
	if rec := purge(); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"purged"`) {
		t.Fatalf("purge status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(zipPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("purged archive should be removed, stat err=%v", err)
	}
	var status, storedZipPath string
	var size int64
	if err := db.QueryRow(`SELECT status, zip_path, package_size FROM sm_pet_store_packs WHERE id='pet_purge_once'`).Scan(&status, &storedZipPath, &size); err != nil {
		t.Fatal(err)
	}
	if status != "purged" || storedZipPath != "" || size != 0 {
		t.Fatalf("purged row status=%q zip=%q size=%d", status, storedZipPath, size)
	}
	if rec := purge(); rec.Code != http.StatusOK {
		t.Fatalf("repeated purge status=%d body=%s", rec.Code, rec.Body.String())
	}
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/pet-store/packs/pet_purge_once", nil)
	deleteReq.SetPathValue("id", "pet_purge_once")
	deleteRec := httptest.NewRecorder()
	h.AdminDeletePetStorePack(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK || !strings.Contains(deleteRec.Body.String(), `"status":"purged"`) {
		t.Fatalf("delete after purge status=%d body=%s", deleteRec.Code, deleteRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/pet-store/packs?status=deleted", nil)
	listRec := httptest.NewRecorder()
	h.AdminListPetStorePacks(listRec, listReq)
	if listRec.Code != http.StatusOK || strings.Contains(listRec.Body.String(), "pet_purge_once") {
		t.Fatalf("deleted filter must hide purged rows: status=%d body=%s", listRec.Code, listRec.Body.String())
	}
}

func TestAdminPurgePetStorePackRejectsNonDeletedListing(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, DataDir: t.TempDir()})
	h.ensurePetStoreSchema()
	now := petStoreNow()
	if _, err := db.Exec(`INSERT INTO sm_pet_store_packs (`+petStorePackColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "pet_cannot_purge", "owner-1", "owner@example.test", "cannot-purge", "Cannot purge", "", "1.0.0", 0, "active", "unused.zip", 1, 0, 0, 0, now, now); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/pet-store/packs/pet_cannot_purge/purge", nil)
	req.SetPathValue("id", "pet_cannot_purge")
	rec := httptest.NewRecorder()
	h.AdminPurgePetStorePack(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("purge active listing status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestApplyPetStoreSnapshotAcceptsPausedDeletedAndPurgedStatuses(t *testing.T) {
	for _, status := range []string{"paused", "deleted", "purged"} {
		t.Run(status, func(t *testing.T) {
			db, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			store, err := skillmarket.NewStore(db, db)
			if err != nil {
				t.Fatal(err)
			}
			h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, DataDir: t.TempDir()})
			archive := validPetStoreTestArchive(t, "ha-"+status)
			now := time.Now().UTC().Format(time.RFC3339)
			packageSize, zipBase64 := int64(len(archive)), base64.StdEncoding.EncodeToString(archive)
			if status == "purged" {
				packageSize, zipBase64 = 0, ""
			}
			raw, err := json.Marshal(petStoreSnapshot{Packs: []petStoreSnapshotPack{{ID: "pet_ha_" + status, OwnerID: "owner-1", OwnerEmail: "owner@example.test", SourcePackID: "ha-" + status, Name: "HA pet", Version: "1.0.0", Status: status, PackageSize: packageSize, CreatedAt: now, UpdatedAt: now, ZipBase64: zipBase64}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := h.ApplyPetStoreSnapshot(context.Background(), raw); err != nil {
				t.Fatalf("apply %s snapshot: %v", status, err)
			}
		})
	}
}

func TestApplyPurgedPetStoreSnapshotRemovesArchiveAndRejectsOlderActiveSnapshot(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, DataDir: t.TempDir()})
	archive := validPetStoreTestArchive(t, "purge-ha-pack")
	older := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	newer := petStoreNow()
	apply := func(status, updatedAt string, packageSize int64, zipBase64 string) error {
		raw, marshalErr := json.Marshal(petStoreSnapshot{Packs: []petStoreSnapshotPack{{
			ID: "pet_purge_ha", OwnerID: "owner-1", OwnerEmail: "owner@example.test", SourcePackID: "purge-ha-pack",
			Name: "Purge HA", Version: "1.0.0", Status: status, PackageSize: packageSize,
			CreatedAt: older, UpdatedAt: updatedAt, ZipBase64: zipBase64,
		}}})
		if marshalErr != nil {
			return marshalErr
		}
		return h.ApplyPetStoreSnapshot(context.Background(), raw)
	}
	if err := apply("active", older, int64(len(archive)), base64.StdEncoding.EncodeToString(archive)); err != nil {
		t.Fatalf("apply active snapshot: %v", err)
	}
	zipPath := filepath.Join(h.petStoreDir(), "pet_purge_ha.zip")
	if _, err := os.Stat(zipPath); err != nil {
		t.Fatalf("active snapshot archive missing: %v", err)
	}
	if err := apply("purged", newer, 0, ""); err != nil {
		t.Fatalf("apply purge snapshot: %v", err)
	}
	if _, err := os.Stat(zipPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("purged snapshot should remove archive, stat err=%v", err)
	}
	if err := apply("active", time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano), int64(len(archive)), base64.StdEncoding.EncodeToString(archive)); err != nil {
		t.Fatalf("apply clock-skewed active snapshot: %v", err)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM sm_pet_store_packs WHERE id='pet_purge_ha'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "purged" {
		t.Fatalf("active snapshot resurrected purged pack: status=%q", status)
	}
	if _, err := os.Stat(zipPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("active snapshot recreated archive, stat err=%v", err)
	}
}

func TestWritePetStoreArchiveAtomicReplacesArchiveWithoutTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pet_atomic.zip")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writePetStoreArchiveAtomic(path, []byte("new archive")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new archive" {
		t.Fatalf("archive content=%q", data)
	}
	temps, err := filepath.Glob(filepath.Join(dir, ".pet-store-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temps) != 0 {
		t.Fatalf("temporary archives remain: %v", temps)
	}
}

func TestApplyPetStoreSnapshotRestoresArchiveWhenTransactionFails(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, DataDir: t.TempDir()})
	h.ensurePetStoreSchema()
	oldArchive := validPetStoreTestArchiveWithMarker(t, "rollback-pack", "old")
	newArchive := validPetStoreTestArchiveWithMarker(t, "rollback-pack", "new")
	if err := os.MkdirAll(h.petStoreDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(h.petStoreDir(), "pet_snapshot_rollback.zip")
	if err := os.WriteFile(zipPath, oldArchive, 0o600); err != nil {
		t.Fatal(err)
	}
	older := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO sm_pet_store_packs (`+petStorePackColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "pet_snapshot_rollback", "owner-1", "owner@example.test", "rollback-pack", "Rollback", "", "1.0.0", 0, "active", zipPath, len(oldArchive), 0, 0, 0, older, older); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER reject_pet_snapshot_update BEFORE UPDATE ON sm_pet_store_packs WHEN NEW.id='pet_snapshot_rollback' BEGIN SELECT RAISE(ABORT, 'forced snapshot failure'); END`); err != nil {
		t.Fatal(err)
	}
	newer := time.Now().UTC().Format(time.RFC3339Nano)
	raw, err := json.Marshal(petStoreSnapshot{Packs: []petStoreSnapshotPack{{
		ID: "pet_snapshot_rollback", OwnerID: "owner-1", OwnerEmail: "owner@example.test", SourcePackID: "rollback-pack",
		Name: "Rollback", Version: "1.0.1", Status: "active", PackageSize: int64(len(newArchive)),
		CreatedAt: older, UpdatedAt: newer, ZipBase64: base64.StdEncoding.EncodeToString(newArchive),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.ApplyPetStoreSnapshot(context.Background(), raw); err == nil {
		t.Fatal("expected snapshot transaction failure")
	}
	actual, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, oldArchive) {
		t.Fatal("failed snapshot replaced the committed archive")
	}
}

func TestPetStoreSnapshotTimeAcceptsNanosecondModerationVersion(t *testing.T) {
	if !validPetStoreSnapshotTime("2026-07-31T10:20:30.123456789Z") {
		t.Fatal("RFC3339Nano moderation timestamp should be accepted")
	}
	if validPetStoreSnapshotTime("not-a-time") {
		t.Fatal("invalid timestamp should be rejected")
	}
}

func TestApplyPetStoreSnapshotResolvesPausedSourceIDClaims(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, DataDir: t.TempDir()})
	archive := validPetStoreTestArchive(t, "shared-ha-pack")
	older := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	newer := petStoreNow()
	apply := func(id, status, updatedAt string) {
		t.Helper()
		raw, marshalErr := json.Marshal(petStoreSnapshot{Packs: []petStoreSnapshotPack{{
			ID: id, OwnerID: id + "-owner", OwnerEmail: id + "@example.test", SourcePackID: "shared-ha-pack",
			Name: id, Version: "1.0.0", Status: status, PackageSize: int64(len(archive)),
			CreatedAt: older, UpdatedAt: updatedAt, ZipBase64: base64.StdEncoding.EncodeToString(archive),
		}}})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if err := h.ApplyPetStoreSnapshot(context.Background(), raw); err != nil {
			t.Fatalf("apply %s: %v", id, err)
		}
	}
	apply("pet_paused_claim", "paused", older)
	apply("pet_active_claim", "active", newer)
	var pausedStatus, activeStatus string
	if err := db.QueryRow(`SELECT status FROM sm_pet_store_packs WHERE id='pet_paused_claim'`).Scan(&pausedStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status FROM sm_pet_store_packs WHERE id='pet_active_claim'`).Scan(&activeStatus); err != nil {
		t.Fatal(err)
	}
	if pausedStatus != "withdrawn" || activeStatus != "active" {
		t.Fatalf("source claim statuses paused=%q active=%q", pausedStatus, activeStatus)
	}
}

func TestSubmitPetStorePackRejectsMismatchedManifestID(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatalf("new skillmarket store: %v", err)
	}
	userSvc := skillmarket.NewUserService(store, nil)
	authSvc := skillmarket.NewAuthService(store, nil, "")
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, UserSvc: userSvc, AuthSvc: authSvc, CreditsSvc: skillmarket.NewCreditsService(store), DataDir: t.TempDir()})
	user, err := userSvc.EnsureAccountWithID(context.Background(), "creator-1", "creator@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := authSvc.CreateSessionForUser(context.Background(), user.ID, user.Email)
	if err != nil {
		t.Fatal(err)
	}

	req := newPetStoreSubmitRequest(t, session.Token, "claimed-pack")
	// Build a new multipart request with a different manifest ID, while keeping
	// the submitted source_pack_id unchanged.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for name, value := range map[string]string{"name": "Creator pet", "price": "0", "source_pack_id": "claimed-pack"} {
		if err := mw.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := mw.CreateFormFile("zip", "other-pack.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(validPetStoreTestArchive(t, "other-pack")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/pet-store/packs", &body)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.SubmitPetStorePack(rec, req)
	if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte("does not match")) {
		t.Fatalf("mismatch submit status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func newPetStoreSubmitRequest(t *testing.T, token, sourcePackID string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for name, value := range map[string]string{
		"name":           "Creator pet",
		"price":          "0",
		"source_pack_id": sourcePackID,
	} {
		if err := mw.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := mw.CreateFormFile("zip", "creator-pet.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(validPetStoreTestArchive(t, sourcePackID)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pet-store/packs", &body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func validPetStoreTestArchive(t *testing.T, sourcePackID ...string) []byte {
	return validPetStoreTestArchiveWithMarker(t, firstPetStoreTestArchiveID(sourcePackID), "")
}

func firstPetStoreTestArchiveID(sourcePackID []string) string {
	id := "creator-pet"
	if len(sourcePackID) > 0 {
		id = sourcePackID[0]
	}
	return id
}

func validPetStoreTestArchiveWithMarker(t *testing.T, id, marker string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pet.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	entry, err := zw.Create("pet-pack.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(entry, "schema_version: 1\nid: "+id+"\nname: Creator pet\nrenderer: native-raster\nassets:\n  native:\n    idle: native/idle.png\n"); err != nil {
		t.Fatal(err)
	}
	idle, err := zw.Create("native/idle.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := idle.Write(petStoreTestPNG(t)); err != nil {
		t.Fatal(err)
	}
	if marker != "" {
		entry, err := zw.Create("marker.txt")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, marker); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSubmitPetStorePackFallsBackToManifestID(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatalf("new skillmarket store: %v", err)
	}
	userSvc := skillmarket.NewUserService(store, nil)
	authSvc := skillmarket.NewAuthService(store, nil, "")
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, UserSvc: userSvc, AuthSvc: authSvc, CreditsSvc: skillmarket.NewCreditsService(store), DataDir: t.TempDir()})
	user, err := userSvc.EnsureAccountWithID(context.Background(), "creator-web", "creator-web@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := authSvc.CreateSessionForUser(context.Background(), user.ID, user.Email)
	if err != nil {
		t.Fatal(err)
	}

	// The web publish form does not collect source_pack_id. Submitting without
	// the field must fall back to the manifest ID inside the archive.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for name, value := range map[string]string{"name": "Web pet", "price": "0"} {
		if err := mw.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := mw.CreateFormFile("zip", "web-pet.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(validPetStoreTestArchive(t, "web-fallback-pet")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pet-store/packs", &body)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.SubmitPetStorePack(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("fallback submit status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created petStorePack
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created pack: %v", err)
	}
	if created.SourcePackID != "web-fallback-pet" {
		t.Fatalf("source_pack_id=%q, want manifest fallback web-fallback-pet", created.SourcePackID)
	}
}

func TestListPetStorePacksEscapesLikeMetacharacters(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatalf("new skillmarket store: %v", err)
	}
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, DataDir: t.TempDir()})
	h.ensurePetStoreSchema()
	now := time.Now().UTC().Format(time.RFC3339)
	insertPack := `INSERT INTO sm_pet_store_packs (` + petStorePackColumns + `) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	for _, p := range []struct{ id, name string }{
		{"pet_pct", "100% Fluffy"},
		{"pet_plain", "plain fluffy"},
		{"pet_under", "cute_pet"},
		{"pet_x", "cutexpet"},
	} {
		if _, err := db.Exec(insertPack, p.id, "owner-1", "owner@example.test", "src-"+p.id, p.name, "", "1.0.0", 0, "active", "unused.zip", 1, 0, 0, 0, now, now); err != nil {
			t.Fatal(err)
		}
	}
	search := func(q string) (int, []string) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/pet-store/packs?q="+q, nil)
		rec := httptest.NewRecorder()
		h.ListPetStorePacks(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("list q=%q status=%d body=%s", q, rec.Code, rec.Body.String())
		}
		var body struct {
			Total int `json:"total"`
			Packs []struct {
				ID string `json:"id"`
			} `json:"packs"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		ids := make([]string, 0, len(body.Packs))
		for _, p := range body.Packs {
			ids = append(ids, p.ID)
		}
		return body.Total, ids
	}
	// A bare "%" must behave as a literal character, not a wildcard.
	if total, _ := search("%25"); total != 1 {
		t.Fatalf("literal %% search total=%d, want 1", total)
	}
	if total, ids := search("100%25"); total != 1 || ids[0] != "pet_pct" {
		t.Fatalf("100%% search total=%d ids=%v, want pet_pct only", total, ids)
	}
	// "_" must be literal too: cute_pet matches, cutexpet does not.
	if total, ids := search("cute_pet"); total != 1 || ids[0] != "pet_under" {
		t.Fatalf("underscore search total=%d ids=%v, want pet_under only", total, ids)
	}
	// Ordinary substring search still works.
	if total, _ := search("fluffy"); total != 2 {
		t.Fatalf("substring search total=%d, want 2", total)
	}
}

func TestAdminListPetStorePacksReturnsZeroTotalPagesWhenEmpty(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatalf("new skillmarket store: %v", err)
	}
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, DataDir: t.TempDir()})
	h.ensurePetStoreSchema()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/pet-store/packs", nil)
	rec := httptest.NewRecorder()
	h.AdminListPetStorePacks(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Total      int `json:"total"`
		TotalPages int `json:"total_pages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode admin list: %v", err)
	}
	if body.Total != 0 || body.TotalPages != 0 {
		t.Fatalf("total=%d total_pages=%d, want 0/0 to match the public listing", body.Total, body.TotalPages)
	}
}

func TestWithdrawnPetStorePackBuyerCanDownloadButStrangerCannot(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	userSvc := skillmarket.NewUserService(store, nil)
	authSvc := skillmarket.NewAuthService(store, nil, "")
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, UserSvc: userSvc, AuthSvc: authSvc, CreditsSvc: skillmarket.NewCreditsService(store), DataDir: t.TempDir()})
	ctx := context.Background()
	owner, err := userSvc.EnsureAccountWithID(ctx, "wd-owner", "wd-owner@example.test")
	if err != nil {
		t.Fatal(err)
	}
	buyer, err := userSvc.EnsureAccountWithID(ctx, "wd-buyer", "wd-buyer@example.test")
	if err != nil {
		t.Fatal(err)
	}
	stranger, err := userSvc.EnsureAccountWithID(ctx, "wd-stranger", "wd-stranger@example.test")
	if err != nil {
		t.Fatal(err)
	}
	buyerSession, err := authSvc.CreateSessionForUser(ctx, buyer.ID, buyer.Email)
	if err != nil {
		t.Fatal(err)
	}
	strangerSession, err := authSvc.CreateSessionForUser(ctx, stranger.ID, stranger.Email)
	if err != nil {
		t.Fatal(err)
	}
	h.ensurePetStoreSchema()
	archive := validPetStoreTestArchive(t, "withdrawn-pet")
	zipPath := filepath.Join(h.petStoreDir(), "pet_withdrawn.zip")
	if err := os.MkdirAll(h.petStoreDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(zipPath, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO sm_pet_store_packs (`+petStorePackColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "pet_withdrawn", owner.ID, owner.Email, "withdrawn-pet", "Withdrawn pet", "", "1.0.0", 5, "withdrawn", zipPath, len(archive), 0, 1, 5, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sm_pet_store_purchases (id, pack_id, buyer_user_id, buyer_email, amount_paid, status, created_at) VALUES (?, ?, ?, ?, ?, 'active', ?)`, "purchase_withdrawn", "pet_withdrawn", buyer.ID, buyer.Email, 5, now); err != nil {
		t.Fatal(err)
	}

	// Buy-out semantics: withdrawal stops new sales, not existing entitlements.
	download := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/pet-store/packs/pet_withdrawn/download", nil)
		req.SetPathValue("id", "pet_withdrawn")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.DownloadPetStorePack(rec, req)
		return rec
	}
	if rec := download(buyerSession.Token); rec.Code != http.StatusOK {
		t.Fatalf("buyer download of withdrawn pack status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if rec := download(strangerSession.Token); rec.Code != http.StatusForbidden {
		t.Fatalf("stranger download of withdrawn pack status=%d body=%s, want 403", rec.Code, rec.Body.String())
	}
	// A repeat download is still a delivery event but not a new downloader.
	if rec := download(buyerSession.Token); rec.Code != http.StatusOK {
		t.Fatalf("repeat buyer download status=%d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var events int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sm_pet_store_downloads WHERE pack_id='pet_withdrawn'`).Scan(&events); err != nil || events != 2 {
		t.Fatalf("download events=%d err=%v, want 2", events, err)
	}
	var count int64
	if err := db.QueryRow(`SELECT download_count FROM sm_pet_store_packs WHERE id='pet_withdrawn'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("download_count=%d err=%v, want 1 distinct downloader", count, err)
	}
	// A withdrawn listing must not accept new purchases.
	purchaseReq := httptest.NewRequest(http.MethodPost, "/api/v1/pet-store/packs/pet_withdrawn/purchase", nil)
	purchaseReq.SetPathValue("id", "pet_withdrawn")
	purchaseReq.Header.Set("Authorization", "Bearer "+strangerSession.Token)
	purchaseRec := httptest.NewRecorder()
	h.PurchasePetStorePack(purchaseRec, purchaseReq)
	if purchaseRec.Code != http.StatusNotFound {
		t.Fatalf("purchase of withdrawn pack status=%d body=%s, want 404", purchaseRec.Code, purchaseRec.Body.String())
	}
}

func TestPurchasePetStorePackInsufficientCreditsReturns402(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	userSvc := skillmarket.NewUserService(store, nil)
	authSvc := skillmarket.NewAuthService(store, nil, "")
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: store, UserSvc: userSvc, AuthSvc: authSvc, CreditsSvc: skillmarket.NewCreditsService(store), DataDir: t.TempDir()})
	ctx := context.Background()
	seller, err := userSvc.EnsureAccountWithID(ctx, "paid-seller", "paid-seller@example.test")
	if err != nil {
		t.Fatal(err)
	}
	buyer, err := userSvc.EnsureAccountWithID(ctx, "broke-buyer", "broke-buyer@example.test")
	if err != nil {
		t.Fatal(err)
	}
	session, err := authSvc.CreateSessionForUser(ctx, buyer.ID, buyer.Email)
	if err != nil {
		t.Fatal(err)
	}
	h.ensurePetStoreSchema()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO sm_pet_store_packs (`+petStorePackColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, "pet_paid", seller.ID, seller.Email, "paid-pet", "Paid pet", "", "1.0.0", 100, "active", "unused.zip", 1, 0, 0, 0, now, now); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pet-store/packs/pet_paid/purchase", nil)
	req.SetPathValue("id", "pet_paid")
	req.Header.Set("Authorization", "Bearer "+session.Token)
	rec := httptest.NewRecorder()
	h.PurchasePetStorePack(rec, req)
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("insufficient credits status=%d body=%s, want 402", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("insufficient credits")) {
		t.Fatalf("body=%s, want insufficient credits message", rec.Body.String())
	}
}

// TestPetStorePreviewCacheIsBounded ensures the preview cache cannot grow past
// petStorePreviewCacheMaxEntries no matter how many distinct archives are
// previewed, and that updating an existing key does not count as a new entry.
func TestPetStorePreviewCacheIsBounded(t *testing.T) {
	resetPetStorePreviewCache()
	t.Cleanup(resetPetStorePreviewCache)

	for i := 0; i < petStorePreviewCacheMaxEntries+50; i++ {
		petStorePreviewCacheStore(fmt.Sprintf("pack-%d.zip", i), petStorePreviewCacheEntry{size: int64(i)})
	}
	count := 0
	petStorePreviewCache.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count > petStorePreviewCacheMaxEntries {
		t.Fatalf("preview cache holds %d entries, want at most %d", count, petStorePreviewCacheMaxEntries)
	}

	// Replacing an existing key must not consume another slot. The cache was
	// rebuilt during the inserts above, so use the most recent key, which is
	// guaranteed to have survived the clear.
	lastKey := fmt.Sprintf("pack-%d.zip", petStorePreviewCacheMaxEntries+49)
	petStorePreviewCacheStore(lastKey, petStorePreviewCacheEntry{size: 999})
	after := 0
	petStorePreviewCache.Range(func(_, _ any) bool {
		after++
		return true
	})
	if after != count {
		t.Fatalf("entry count changed on replace: before=%d after=%d", count, after)
	}
}
