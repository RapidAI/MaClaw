package main

import (
	"path/filepath"
	"testing"
)

func TestManagedIndustryExpertReconcilePreventsRemovedAssetReuse(t *testing.T) {
	previous := defaultManagedIndustryExpertStore
	dir := t.TempDir()
	defaultManagedIndustryExpertStore = &managedIndustryExpertStore{pathFn: func() string { return filepath.Join(dir, "managed.json") }}
	t.Cleanup(func() { defaultManagedIndustryExpertStore = previous })
	if err := defaultManagedIndustryExpertStore.Save(managedIndustryExpertInstall{AssetID: "asset-a", ListingID: "listing-a", LocalExpertID: "pkgexp-a"}); err != nil {
		t.Fatal(err)
	}
	if !isManagedIndustryExpert("pkgexp-a") || !isActiveManagedIndustryExpert("pkgexp-a") {
		t.Fatal("saved entry should be active and managed")
	}
	if err := defaultManagedIndustryExpertStore.ReconcileActiveAssets("scope-a", 1, "snapshot-empty", map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if !isManagedIndustryExpert("pkgexp-a") {
		t.Fatal("origin must remain read-only after withdrawal")
	}
	if isActiveManagedIndustryExpert("pkgexp-a") {
		t.Fatal("withdrawn asset must not start a new session")
	}
}

func TestManagedIndustryExpertSaveCannotReactivateWithdrawnAsset(t *testing.T) {
	dir := t.TempDir()
	store := &managedIndustryExpertStore{pathFn: func() string { return filepath.Join(dir, "managed.json") }}
	if err := store.ReconcileActiveAssets("scope-a", 1, "snapshot-active", map[string]bool{"asset-a": true}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileActiveAssets("scope-a", 2, "snapshot-withdrawn", map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(managedIndustryExpertInstall{AssetID: "asset-a", ListingID: "listing-a", LocalExpertID: "pkgexp-a"}); err != nil {
		t.Fatal(err)
	}
	item, ok, err := store.ByLocalID("pkgexp-a")
	if err != nil || !ok {
		t.Fatalf("saved item missing: %#v, err=%v", item, err)
	}
	if item.Active {
		t.Fatal("an older installation must not reactivate a withdrawn asset")
	}
}

func TestManagedIndustryExpertReconcileRejectsStaleCatalogue(t *testing.T) {
	dir := t.TempDir()
	store := &managedIndustryExpertStore{pathFn: func() string { return filepath.Join(dir, "managed.json") }}
	if err := store.Save(managedIndustryExpertInstall{AssetID: "asset-a", ListingID: "listing-a", LocalExpertID: "pkgexp-a"}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileActiveAssets("scope-a", 2, "withdrawn", map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileActiveAssets("scope-a", 1, "old-active", map[string]bool{"asset-a": true}); err != nil {
		t.Fatal(err)
	}
	item, ok, err := store.ByLocalID("pkgexp-a")
	if err != nil || !ok {
		t.Fatalf("saved item missing: %#v, err=%v", item, err)
	}
	if item.Active {
		t.Fatal("late older catalogue must not reactivate a withdrawn asset")
	}
}

func TestManagedIndustryExpertScopeChangeDeactivatesPriorTenant(t *testing.T) {
	dir := t.TempDir()
	store := &managedIndustryExpertStore{pathFn: func() string { return filepath.Join(dir, "managed.json") }}
	if err := store.ReconcileActiveAssets("scope-a", 5, "a", map[string]bool{"asset-a": true}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(managedIndustryExpertInstall{AssetID: "asset-a", ListingID: "listing-a", LocalExpertID: "pkgexp-a", CatalogueScope: "scope-a"}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileActiveAssets("scope-b", 1, "b", map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	item, ok, err := store.ByLocalID("pkgexp-a")
	if err != nil || !ok {
		t.Fatalf("saved item missing: %#v, err=%v", item, err)
	}
	if item.Active {
		t.Fatal("a prior tenant's managed expert must become inactive after scope change")
	}
}

func TestManagedIndustryExpertActiveLookupRequiresMatchingScope(t *testing.T) {
	dir := t.TempDir()
	store := &managedIndustryExpertStore{pathFn: func() string { return filepath.Join(dir, "managed.json") }}
	if err := store.ReconcileActiveAssets("scope-a", 1, "a", map[string]bool{"asset-a": true}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(managedIndustryExpertInstall{AssetID: "asset-a", ListingID: "listing-a", LocalExpertID: "pkgexp-a", CatalogueScope: "scope-a"}); err != nil {
		t.Fatal(err)
	}
	if active, err := store.IsActiveLocalIDInScope("pkgexp-a", "scope-b"); err != nil || active {
		t.Fatalf("scope-b active=%v err=%v, want false nil", active, err)
	}
	if active, err := store.IsActiveLocalIDInScope("pkgexp-a", "scope-a"); err != nil || !active {
		t.Fatalf("scope-a active=%v err=%v, want true nil", active, err)
	}
}

func TestManagedIndustryExpertSaveKeepsSameAssetFromPriorScopeManaged(t *testing.T) {
	dir := t.TempDir()
	store := &managedIndustryExpertStore{pathFn: func() string { return filepath.Join(dir, "managed.json") }}
	if err := store.SaveInactive(managedIndustryExpertInstall{AssetID: "asset-a", ListingID: "listing-a", LocalExpertID: "pkgexp-a", CatalogueScope: "scope-a"}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileActiveAssets("scope-b", 1, "b", map[string]bool{"asset-a": true}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(managedIndustryExpertInstall{AssetID: "asset-a", ListingID: "listing-a", LocalExpertID: "pkgexp-b", CatalogueScope: "scope-b"}); err != nil {
		t.Fatal(err)
	}
	old, oldOK, err := store.ByLocalID("pkgexp-a")
	if err != nil || !oldOK || old.Active || old.CatalogueScope != "scope-a" {
		t.Fatalf("prior scope origin overwritten: %#v found=%v err=%v", old, oldOK, err)
	}
	current, currentOK, err := store.ByLocalID("pkgexp-b")
	if err != nil || !currentOK || !current.Active || current.CatalogueScope != "scope-b" {
		t.Fatalf("current scope origin missing: %#v found=%v err=%v", current, currentOK, err)
	}
}

func TestManagedIndustryExpertSameLocalIDCanBeActiveInNewScope(t *testing.T) {
	dir := t.TempDir()
	store := &managedIndustryExpertStore{pathFn: func() string { return filepath.Join(dir, "managed.json") }}
	if err := store.SaveInactive(managedIndustryExpertInstall{AssetID: "asset-a", ListingID: "listing-a", LocalExpertID: "pkgexp-shared", CatalogueScope: "scope-a"}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileActiveAssets("scope-b", 1, "b", map[string]bool{"asset-b": true}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(managedIndustryExpertInstall{AssetID: "asset-b", ListingID: "listing-b", LocalExpertID: "pkgexp-shared", CatalogueScope: "scope-b"}); err != nil {
		t.Fatal(err)
	}
	if active, err := store.IsActiveLocalIDInScope("pkgexp-shared", "scope-b"); err != nil || !active {
		t.Fatalf("new scope active=%v err=%v, want true nil", active, err)
	}
	if active, err := store.IsActiveLocalIDInScope("pkgexp-shared", "scope-a"); err != nil || active {
		t.Fatalf("old scope active=%v err=%v, want false nil", active, err)
	}
	if !store.HasActiveLocalID("pkgexp-shared") {
		t.Fatal("an active current scope must not be shadowed by an older inactive origin")
	}
}

func TestManagedIndustryExpertSaveInactiveLocksUnconfirmedInstallation(t *testing.T) {
	dir := t.TempDir()
	store := &managedIndustryExpertStore{pathFn: func() string { return filepath.Join(dir, "managed.json") }}
	if err := store.SaveInactive(managedIndustryExpertInstall{AssetID: "asset-a", ListingID: "listing-a", LocalExpertID: "pkgexp-a"}); err != nil {
		t.Fatal(err)
	}
	item, ok, err := store.ByLocalID("pkgexp-a")
	if err != nil || !ok {
		t.Fatalf("saved item missing: %#v, err=%v", item, err)
	}
	if item.Active {
		t.Fatal("unconfirmed installation must remain managed but inactive")
	}
}
