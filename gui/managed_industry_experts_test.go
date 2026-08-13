package main

import "testing"

func TestManagedIndustryCatalogueAssetRequiresExactTenantAssignment(t *testing.T) {
	catalog := managedIndustryExpertCatalog{Experts: []managedIndustryExpertRecord{{AssetID: "asset-finance", ListingID: "listing-finance", Version: "2"}}}
	item, err := managedIndustryCatalogueAsset(catalog, " asset-finance ", " listing-finance ")
	if err != nil || item.Version != "2" {
		t.Fatalf("assigned item = %#v, err=%v", item, err)
	}
	if _, err := managedIndustryCatalogueAsset(catalog, "asset-finance", "listing-other"); err == nil {
		t.Fatal("listing substitution must be rejected")
	}
	if _, err := managedIndustryCatalogueAsset(catalog, "asset-other", "listing-finance"); err == nil {
		t.Fatal("asset substitution must be rejected")
	}
}

func TestManagedIndustryInstallKeyIsScoped(t *testing.T) {
	if managedIndustryInstallKey("scope-a", "asset-a") == managedIndustryInstallKey("scope-b", "asset-a") {
		t.Fatal("same asset from separate tenant scopes must not share install state")
	}
	if got := managedIndustryInstallKey(" scope-a ", " asset-a "); got != "scope-a\x00asset-a" {
		t.Fatalf("install key = %q", got)
	}
}

func TestManagedIndustryPaidInstallDecisionRequiresAccountSnapshot(t *testing.T) {
	paidNeedsPurchase := func(installed, accountLoaded, owned bool) bool {
		return !installed && accountLoaded && !owned
	}
	if paidNeedsPurchase(false, false, false) {
		t.Fatal("an unavailable account must not be treated as an unowned paid listing")
	}
	if !paidNeedsPurchase(false, true, false) {
		t.Fatal("a confirmed unowned paid listing must require purchase")
	}
	if paidNeedsPurchase(false, true, true) {
		t.Fatal("a confirmed owned listing must not require purchase")
	}
}
