package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

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
		t.Fatalf("a withdrawn listing must release the source ID: %v", err)
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
	if _, err := io.WriteString(entry, "schema_version: 1\nid: "+id+"\nname: Creator pet\n"); err != nil {
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
