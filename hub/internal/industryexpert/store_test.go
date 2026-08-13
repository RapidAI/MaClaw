package industryexpert

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	_ "modernc.org/sqlite"
)

const testPackageHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestReplaceAndListCatalogKeepsPaidListingMetadata(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db)
	if err := store.InitSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	definition, _ := json.Marshal(map[string]any{"id": "pkgexp-finance", "name": "Finance", "system_prompt": "help"})
	catalog := Catalog{Revision: 7, ContentHash: "hash", Experts: []Expert{{AssetID: "asset-1", ListingID: "listing-1", PackageHash: testPackageHash, Version: "2", Price: 99, Name: "Finance", Description: "review", Icon: "📈", Definition: definition, Industries: []Industry{{ID: "finance", Name: "Finance"}}}}}
	if err := store.Replace(context.Background(), "tenant-a", catalog); err != nil {
		t.Fatal(err)
	}
	got, err := store.List(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 7 || len(got.Experts) != 1 || got.Experts[0].ListingID != "listing-1" || got.Experts[0].PackageHash != testPackageHash || got.Experts[0].Price != 99 {
		t.Fatalf("unexpected catalog: %#v", got)
	}
}

func TestReplaceRejectsPaidAssetWithoutListing(t *testing.T) {
	db, _ := sql.Open("sqlite", ":memory:")
	defer db.Close()
	store := NewStore(db)
	if err := store.InitSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	definition, _ := json.Marshal(map[string]any{"id": "pkgexp-x", "name": "X", "system_prompt": "help"})
	if err := store.Replace(context.Background(), "tenant", Catalog{Experts: []Expert{{AssetID: "asset", Price: 1, Definition: definition}}}); err == nil {
		t.Fatal("expected paid listing validation failure")
	}
}

func TestReplaceRejectsManagedAssetWithoutImmutablePackageHash(t *testing.T) {
	db, _ := sql.Open("sqlite", ":memory:")
	defer db.Close()
	store := NewStore(db)
	if err := store.InitSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	definition, _ := json.Marshal(map[string]any{"id": "pkgexp-x", "name": "X", "system_prompt": "help"})
	if err := store.Replace(context.Background(), "tenant", Catalog{Experts: []Expert{{AssetID: "asset", ListingID: "listing", Definition: definition}}}); err == nil {
		t.Fatal("expected missing package hash validation failure")
	}
}

func TestReplaceRejectsNonCanonicalDuplicateAssetIDs(t *testing.T) {
	db, _ := sql.Open("sqlite", ":memory:")
	defer db.Close()
	store := NewStore(db)
	if err := store.InitSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	definition, _ := json.Marshal(map[string]any{"id": "pkgexp-x", "name": "X", "system_prompt": "help"})
	catalog := Catalog{Experts: []Expert{
		{AssetID: "asset", ListingID: "listing-a", PackageHash: testPackageHash, Definition: definition},
		{AssetID: " asset ", ListingID: "listing-b", PackageHash: testPackageHash, Definition: definition},
	}}
	if err := store.Replace(context.Background(), "tenant", catalog); err == nil {
		t.Fatal("expected duplicate normalized asset id validation failure")
	}
}

func TestReplaceRejectsInvalidPackageHash(t *testing.T) {
	db, _ := sql.Open("sqlite", ":memory:")
	defer db.Close()
	store := NewStore(db)
	if err := store.InitSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	definition, _ := json.Marshal(map[string]any{"id": "pkgexp-x", "name": "X", "system_prompt": "help"})
	if err := store.Replace(context.Background(), "tenant", Catalog{Experts: []Expert{{AssetID: "asset", ListingID: "listing", PackageHash: "not-a-sha256", Definition: definition}}}); err == nil {
		t.Fatal("expected invalid package hash validation failure")
	}
}

func TestReplaceDoesNotRollbackNewerManagedCatalogue(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db)
	if err := store.InitSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	definition, _ := json.Marshal(map[string]any{"id": "pkgexp-current", "name": "Current", "system_prompt": "help"})
	newer := Catalog{Revision: 2, ContentHash: "new", Experts: []Expert{{AssetID: "asset-new", ListingID: "listing-new", PackageHash: testPackageHash, Definition: definition}}}
	if err := store.Replace(context.Background(), "tenant", newer); err != nil {
		t.Fatal(err)
	}
	older := Catalog{Revision: 1, ContentHash: "old", Experts: []Expert{{AssetID: "asset-old", ListingID: "listing-old", PackageHash: testPackageHash, Definition: definition}}}
	if err := store.Replace(context.Background(), "tenant", older); err != nil {
		t.Fatal(err)
	}
	got, err := store.List(context.Background(), "tenant")
	if err != nil || got.Revision != 2 || got.ContentHash != "new" || len(got.Experts) != 1 || got.Experts[0].AssetID != "asset-new" {
		t.Fatalf("stale replace rolled back catalogue: %#v err=%v", got, err)
	}
}

func TestReplaceRejectsConflictingSameRevision(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db)
	if err := store.InitSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	definition, _ := json.Marshal(map[string]any{"id": "pkgexp-current", "name": "Current", "system_prompt": "help"})
	if err := store.Replace(context.Background(), "tenant", Catalog{Revision: 2, ContentHash: "hash-a", Experts: []Expert{{AssetID: "asset-a", ListingID: "listing-a", PackageHash: testPackageHash, Definition: definition}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Replace(context.Background(), "tenant", Catalog{Revision: 2, ContentHash: "hash-b", Experts: []Expert{{AssetID: "asset-b", ListingID: "listing-b", PackageHash: testPackageHash, Definition: definition}}}); err == nil {
		t.Fatal("expected conflicting revision rejection")
	}
}

func TestSameRevisionSuccessfulRetryClearsSyncFailure(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db)
	if err := store.InitSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	definition, _ := json.Marshal(map[string]any{"id": "pkgexp-current", "name": "Current", "system_prompt": "help"})
	catalog := Catalog{Revision: 2, ContentHash: "hash", Experts: []Expert{{AssetID: "asset", ListingID: "listing", PackageHash: testPackageHash, Definition: definition}}}
	if err := store.Replace(context.Background(), "tenant", catalog); err != nil {
		t.Fatal(err)
	}
	store.MarkFailure(context.Background(), "tenant", sql.ErrConnDone)
	if err := store.Replace(context.Background(), "tenant", catalog); err != nil {
		t.Fatal(err)
	}
	var status, lastError string
	if err := db.QueryRow(`SELECT sync_status,last_error FROM managed_industry_expert_catalogs WHERE tenant_id='tenant'`).Scan(&status, &lastError); err != nil {
		t.Fatal(err)
	}
	if status != "ready" || lastError != "" {
		t.Fatalf("successful retry did not clear failure: status=%q error=%q", status, lastError)
	}
}
