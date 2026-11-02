package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestApplyExpertMarketSnapshotCopiesAListingOntoAnEmptyPeer(t *testing.T) {
	origin, _, _, _ := newExpertMarketTestHandler(t)
	peer, _, _, _ := newExpertMarketTestHandler(t)
	origin.ensureExpertMarketSchema()
	archive := expertMarketArchive(t, "pkgexp-ha-peer", "长篇连载写手")
	if err := os.MkdirAll(origin.expertMarketDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(origin.expertMarketDir(), "expert_listing-ha.zip")
	if err := os.WriteFile(zipPath, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	now := "2026-11-02T00:00:00.123456789Z"
	_, err := origin.store.DB().Exec(`INSERT INTO sm_expert_market_listings (`+expertMarketListingColumns+`, platform_distribution) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"expert_listing-ha", "owner-1", "owner@example.test", "pkgexp-ha-peer", "长篇连载写手", "writes long chapters", "", "1", 0, "public", "listed", zipPath, len(archive), 3, 1, 0, "", now, now, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = origin.store.DB().Exec(`INSERT INTO sm_expert_market_purchases (id, listing_id, buyer_user_id, buyer_email, amount_paid, status, created_at) VALUES (?,?,?,?,?,?,?)`,
		"expert_purchase-ha", "expert_listing-ha", "buyer-1", "buyer@example.test", 0, "active", now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = origin.store.DB().Exec(`INSERT INTO sm_expert_market_share_targets (listing_id, contact, user_id, created_at) VALUES (?,?,?,?)`,
		"expert_listing-ha", "phone:13800138000", "user-shared", now)
	if err != nil {
		t.Fatal(err)
	}
	// Production keeps a single writer connection. Dumping purchases and share
	// targets on that connection must not wait on itself.
	origin.store.DB().SetMaxOpenConns(1)
	snap, err := origin.dumpExpertMarketSnapshot(context.Background(), "expert_listing-ha")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.ApplyExpertMarketSnapshot(context.Background(), "upsert", raw); err != nil {
		t.Fatal(err)
	}
	var name, status string
	var downloads int64
	if err := peer.store.DB().QueryRow(`SELECT name, status, download_count FROM sm_expert_market_listings WHERE id=?`, "expert_listing-ha").Scan(&name, &status, &downloads); err != nil {
		t.Fatal(err)
	}
	if name != "长篇连载写手" || status != "listed" || downloads != 3 {
		t.Fatalf("peer listing name=%q status=%q downloads=%d", name, status, downloads)
	}
	copied, err := os.ReadFile(filepath.Join(peer.expertMarketDir(), "expert_listing-ha.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if len(copied) != len(archive) {
		t.Fatalf("copied package=%d, want %d", len(copied), len(archive))
	}
	var purchases int
	if err := peer.store.DB().QueryRow(`SELECT COUNT(*) FROM sm_expert_market_purchases WHERE listing_id=? AND buyer_user_id=?`, "expert_listing-ha", "buyer-1").Scan(&purchases); err != nil || purchases != 1 {
		t.Fatalf("purchases=%d err=%v", purchases, err)
	}
	var sharedUser string
	if err := peer.store.DB().QueryRow(`SELECT user_id FROM sm_expert_market_share_targets WHERE listing_id=? AND contact=?`, "expert_listing-ha", "phone:13800138000").Scan(&sharedUser); err != nil || sharedUser != "user-shared" {
		t.Fatalf("shared user=%q err=%v", sharedUser, err)
	}

	stale := *snap
	stale.Listings[0].Name = "stale name"
	stale.Listings[0].UpdatedAt = "2026-11-01T00:00:00Z"
	staleRaw, err := json.Marshal(stale)
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.ApplyExpertMarketSnapshot(context.Background(), "upsert", staleRaw); err != nil {
		t.Fatal(err)
	}
	if err := peer.store.DB().QueryRow(`SELECT name FROM sm_expert_market_listings WHERE id=?`, "expert_listing-ha").Scan(&name); err != nil || name != "长篇连载写手" {
		t.Fatalf("older snapshot replaced the listing with %q err=%v", name, err)
	}
	if _, err := peer.store.DB().Exec(`UPDATE sm_expert_market_purchases SET id='expert_purchase-local', status='refunded' WHERE listing_id=?`, "expert_listing-ha"); err != nil {
		t.Fatal(err)
	}
	if err := peer.ApplyExpertMarketSnapshot(context.Background(), "upsert", raw); err != nil {
		t.Fatal(err)
	}
	var purchaseCount int
	var purchaseStatus string
	if err := peer.store.DB().QueryRow(`SELECT COUNT(*), MIN(status) FROM sm_expert_market_purchases WHERE listing_id=? AND buyer_user_id=?`, "expert_listing-ha", "buyer-1").Scan(&purchaseCount, &purchaseStatus); err != nil || purchaseCount != 1 || purchaseStatus != "refunded" {
		t.Fatalf("buyer entitlement count=%d status=%q err=%v", purchaseCount, purchaseStatus, err)
	}

	if err := peer.ApplyExpertMarketSnapshot(context.Background(), "delete", []byte(`{"id":"expert_listing-ha"}`)); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := peer.store.DB().QueryRow(`SELECT COUNT(*) FROM sm_expert_market_listings WHERE id=?`, "expert_listing-ha").Scan(&left); err != nil || left != 0 {
		t.Fatalf("listing left=%d err=%v", left, err)
	}
	if err := peer.ApplyExpertMarketSnapshot(context.Background(), "upsert", raw); err != nil {
		t.Fatal(err)
	}
	if err := peer.store.DB().QueryRow(`SELECT COUNT(*) FROM sm_expert_market_listings WHERE id=?`, "expert_listing-ha").Scan(&left); err != nil || left != 0 {
		t.Fatalf("delayed snapshot restored a deleted listing: left=%d err=%v", left, err)
	}
}

func TestApplyExpertMarketSnapshotKeepsTheNewerSourceListing(t *testing.T) {
	peer, _, _, _ := newExpertMarketTestHandler(t)
	archive := expertMarketArchive(t, "pkgexp-ha-peer", "同包专家")
	older := expertMarketSnapshot{Listings: []expertMarketSnapshotListing{{
		ID: "expert_listing-old", OwnerID: "owner-1", OwnerEmail: "owner@example.test", SourceExpertID: "pkgexp-same",
		Name: "旧提交", Version: "1", Visibility: "users", Status: "shared", PackageSize: int64(len(archive)),
		CreatedAt: "2026-11-01T00:00:00Z", UpdatedAt: "2026-11-01T00:00:00Z",
		ZipBase64:    base64.StdEncoding.EncodeToString(archive),
		ShareTargets: []expertMarketSnapshotShare{{Contact: " phone:13800138000 ", UserID: " user-shared ", CreatedAt: "2026-11-01T00:00:00Z"}},
	}}}
	newer := older
	newer.Listings = []expertMarketSnapshotListing{older.Listings[0]}
	newer.Listings[0].ID = "expert_listing-new"
	newer.Listings[0].Name = "新提交"
	newer.Listings[0].Visibility = "public"
	newer.Listings[0].Status = "listed"
	newer.Listings[0].ShareTargets = nil
	newer.Listings[0].UpdatedAt = "2026-11-02T00:00:00Z"
	older.Listings[0].Price = 9
	older.Listings[0].PlatformDistribution = true
	older.Listings[0].ShareTargets = []expertMarketSnapshotShare{
		{Contact: "phone:8613800138000", UserID: "", CreatedAt: "2026-11-01T00:00:00Z"},
		{Contact: "PHONE:13800138000", UserID: " user-shared ", CreatedAt: "2026-11-01T00:00:00Z"},
	}
	apply := func(snap expertMarketSnapshot) {
		t.Helper()
		raw, err := json.Marshal(snap)
		if err != nil {
			t.Fatal(err)
		}
		if err := peer.ApplyExpertMarketSnapshot(context.Background(), "upsert", raw); err != nil {
			t.Fatal(err)
		}
	}
	apply(older)
	var storedContact, storedUser string
	var shareCount, storedPrice, storedDistribution int
	if err := peer.store.DB().QueryRow(`SELECT COUNT(*), MIN(contact), MIN(user_id) FROM sm_expert_market_share_targets WHERE listing_id=?`, "expert_listing-old").Scan(&shareCount, &storedContact, &storedUser); err != nil || shareCount != 1 || storedContact != "phone:13800138000" || storedUser != "user-shared" {
		t.Fatalf("stored shares=%d contact=%q user=%q err=%v", shareCount, storedContact, storedUser, err)
	}
	if err := peer.store.DB().QueryRow(`SELECT price, platform_distribution FROM sm_expert_market_listings WHERE id=?`, "expert_listing-old").Scan(&storedPrice, &storedDistribution); err != nil || storedPrice != 0 || storedDistribution != 0 {
		t.Fatalf("user share price=%d distribution=%d err=%v", storedPrice, storedDistribution, err)
	}
	apply(newer)
	var oldStatus, oldSource, newName string
	if err := peer.store.DB().QueryRow(`SELECT status, source_expert_id FROM sm_expert_market_listings WHERE id=?`, "expert_listing-old").Scan(&oldStatus, &oldSource); err != nil {
		t.Fatal(err)
	}
	if oldStatus != "deleted" || oldSource == "pkgexp-same" {
		t.Fatalf("old listing status=%q source=%q", oldStatus, oldSource)
	}
	var shared int
	if err := peer.store.DB().QueryRow(`SELECT COUNT(*) FROM sm_expert_market_share_targets WHERE listing_id=?`, "expert_listing-old").Scan(&shared); err != nil || shared != 0 {
		t.Fatalf("replaced share targets=%d err=%v", shared, err)
	}
	if err := peer.store.DB().QueryRow(`SELECT name FROM sm_expert_market_listings WHERE id=? AND source_expert_id=?`, "expert_listing-new", "pkgexp-same").Scan(&newName); err != nil || newName != "新提交" {
		t.Fatalf("new listing name=%q err=%v", newName, err)
	}
	apply(older)
	if err := peer.store.DB().QueryRow(`SELECT name FROM sm_expert_market_listings WHERE id=?`, "expert_listing-new").Scan(&newName); err != nil || newName != "新提交" {
		t.Fatalf("older duplicate replaced the newer listing: name=%q err=%v", newName, err)
	}
}
