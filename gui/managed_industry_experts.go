package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	managedIndustryInstallFlights sync.Map
	// A failed automatic install must not create an endless UI polling and
	// download loop. It is deliberately process-local: a later app start will
	// retry the automatic policy, while this session exposes a retry action.
	managedIndustryInstallFailures sync.Map
)

func managedIndustryInstallKey(scope, assetID string) string {
	return strings.TrimSpace(scope) + "\x00" + strings.TrimSpace(assetID)
}

type managedIndustryExpertView struct {
	AssetID           string   `json:"asset_id"`
	ListingID         string   `json:"listing_id"`
	LocalExpertID     string   `json:"local_expert_id,omitempty"`
	Version           string   `json:"version,omitempty"`
	Price             int64    `json:"price"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Icon              string   `json:"icon"`
	IndustryNames     []string `json:"industry_names"`
	Installed         bool     `json:"installed"`
	AutoInstalling    bool     `json:"auto_installing"`
	AutoInstallFailed bool     `json:"auto_install_failed"`
	PurchaseRequired  bool     `json:"purchase_required"`
	ReadOnly          bool     `json:"read_only"`
}

// managedIndustryCatalogueAsset resolves an action request against the latest
// tenant-scoped catalogue.  The WebView supplies asset/listing identifiers, so
// this check must live in Go: otherwise a caller could label an unrelated
// market installation as industry-managed merely by pairing it with an asset
// ID from the rendered page.
func managedIndustryCatalogueAsset(catalog managedIndustryExpertCatalog, assetID, listingID string) (managedIndustryExpertRecord, error) {
	assetID = strings.TrimSpace(assetID)
	listingID = strings.TrimSpace(listingID)
	for _, item := range catalog.Experts {
		if strings.TrimSpace(item.AssetID) == assetID && strings.TrimSpace(item.ListingID) == listingID {
			return item, nil
		}
	}
	return managedIndustryExpertRecord{}, fmt.Errorf("industry expert is no longer assigned to this tenant")
}

// ListManagedIndustryExperts joins Hub's tenant catalogue with this machine's
// local market-install state. It installs only zero-price entries automatically;
// a paid item must have an existing market entitlement, which is checked by the
// market download endpoint when the user presses purchase/install.
func (a *App) ListManagedIndustryExperts() (string, error) {
	client, err := a.newExpertHubClient()
	if err != nil {
		return "[]", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), expertHubListTimeout)
	defer cancel()
	catalog, err := client.ListManagedIndustryExperts(ctx)
	if err != nil {
		return "[]", nil
	}
	owned := map[string]bool{}
	accountLoaded := false
	if account, accountErr := a.GetExpertMarketAccount(); accountErr == nil {
		accountLoaded = true
		if purchases, ok := account["purchases"].([]interface{}); ok {
			for _, raw := range purchases {
				if listing, ok := raw.(map[string]interface{}); ok {
					owned[strings.TrimSpace(fmt.Sprint(listing["id"]))] = true
				}
			}
		}
		if uploads, ok := account["uploads"].([]interface{}); ok {
			for _, raw := range uploads {
				if listing, ok := raw.(map[string]interface{}); ok {
					owned[strings.TrimSpace(fmt.Sprint(listing["id"]))] = true
				}
			}
		}
	}
	scope := a.managedIndustryCatalogueScope(client)
	if err := reconcileManagedIndustryCatalogue(scope, catalog); err != nil {
		return "", err
	}
	views := make([]managedIndustryExpertView, 0, len(catalog.Experts))
	for _, item := range catalog.Experts {
		view := managedIndustryExpertView{AssetID: item.AssetID, ListingID: item.ListingID, Version: item.Version, Price: item.Price, Name: item.Name, Description: item.Description, Icon: item.Icon, ReadOnly: true}
		for _, industry := range item.Industries {
			if name := strings.TrimSpace(industry.Name); name != "" {
				view.IndustryNames = append(view.IndustryNames, name)
			}
		}
		if install, ok, _ := defaultManagedIndustryExpertStore.ByAssetIDInScope(item.AssetID, scope); ok {
			if active, _ := defaultManagedIndustryExpertStore.IsActiveLocalIDInScope(install.LocalExpertID, scope); active {
				if _, exists, _ := defaultExpertStore.Get(install.LocalExpertID); exists {
					view.Installed = true
					view.LocalExpertID = install.LocalExpertID
				}
			}
		}
		// Free assets and listings the current user already owns are installed
		// without any card action. A paid listing without an entitlement remains
		// a metadata-only placeholder and never starts a download.
		// A failed account lookup is not proof that the current user lacks a paid
		// entitlement. Keep the card in a non-actionable loading state until the
		// account can be read, rather than presenting a misleading purchase action
		// that may charge (or fail for) an already-owned listing.
		view.PurchaseRequired = !view.Installed && item.Price > 0 && accountLoaded && !owned[item.ListingID]
		if !view.Installed && strings.TrimSpace(item.ListingID) != "" && !view.PurchaseRequired {
			installKey := managedIndustryInstallKey(scope, item.AssetID)
			_, installFailed := managedIndustryInstallFailures.Load(installKey)
			view.AutoInstallFailed = installFailed
			view.AutoInstalling = !installFailed
			// Publishers own their listing without a purchase record; download it
			// directly instead of attempting an invalid self-purchase.
			if !installFailed {
				go a.installManagedIndustryExpert(item, item.Price == 0 && !owned[item.ListingID], scope)
			}
		}
		views = append(views, view)
	}
	sort.SliceStable(views, func(i, j int) bool { return views[i].Name < views[j].Name })
	raw, err := json.Marshal(views)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *App) managedIndustryCatalogueScope(client *expertHubClient) string {
	if client == nil {
		return ""
	}
	// The Hub can re-enroll a machine into another tenant. Machine ID alone is
	// therefore not an ownership boundary: include the configured tenant when
	// available, and retain the machine ID as the safe compatibility fallback
	// for older registrations without a tenant field.
	tenantID := ""
	if a != nil {
		if cfg, err := a.LoadConfig(); err == nil {
			tenantID = strings.TrimSpace(cfg.RemoteTenantID)
		}
	}
	if tenantID == "" {
		tenantID = "machine:" + strings.TrimSpace(client.machineID)
	}
	return strings.TrimRight(strings.TrimSpace(client.baseURL), "/") + "\x00" + tenantID
}

func reconcileManagedIndustryCatalogue(scope string, catalog managedIndustryExpertCatalog) error {
	activeAssets := make(map[string]bool, len(catalog.Experts))
	for _, item := range catalog.Experts {
		if assetID := strings.TrimSpace(item.AssetID); assetID != "" {
			activeAssets[assetID] = true
		}
	}
	return defaultManagedIndustryExpertStore.ReconcileActiveAssets(scope, catalog.Revision, catalog.ContentHash, activeAssets)
}

func (a *App) installManagedIndustryExpert(item managedIndustryExpertRecord, acquireFree bool, scope string) {
	if strings.TrimSpace(item.ListingID) == "" {
		return
	}
	installKey := managedIndustryInstallKey(scope, item.AssetID)
	if _, loaded := managedIndustryInstallFlights.LoadOrStore(installKey, struct{}{}); loaded {
		return
	}
	defer managedIndustryInstallFlights.Delete(installKey)
	if acquireFree {
		if _, err := a.PurchaseExpertMarketListing(item.ListingID); err != nil {
			managedIndustryInstallFailures.Store(installKey, struct{}{})
			return
		}
	}
	result, err := a.InstallExpertMarketListingWithExpectedHash(item.ListingID, item.PackageHash)
	if err != nil || result == nil || strings.TrimSpace(result.Expert.ID) == "" {
		managedIndustryInstallFailures.Store(installKey, struct{}{})
		return
	}
	if !a.reconcileManagedIndustryExpertBeforeSave(item, scope) {
		// Download/import has already created the local definition. Preserve its
		// platform origin as inactive so a tenant reassignment or a transient
		// final directory check can never turn it into a personal editable expert.
		_ = defaultManagedIndustryExpertStore.SaveInactive(managedIndustryExpertInstall{AssetID: item.AssetID, ListingID: item.ListingID, LocalExpertID: result.Expert.ID, Version: item.Version, CatalogueScope: scope})
		invalidateExpertDefCache(result.Expert.ID)
		managedIndustryInstallFailures.Store(installKey, struct{}{})
		return
	}
	if err := defaultManagedIndustryExpertStore.Save(managedIndustryExpertInstall{AssetID: item.AssetID, ListingID: item.ListingID, LocalExpertID: result.Expert.ID, Version: item.Version, CatalogueScope: scope}); err != nil {
		managedIndustryInstallFailures.Store(installKey, struct{}{})
		return
	}
	managedIndustryInstallFailures.Delete(installKey)
	invalidateExpertDefCache(result.Expert.ID)
}

// reconcileManagedIndustryExpertBeforeSave closes the long installation window:
// a task launched from an older catalogue may only become a managed active
// expert if the server still assigns the exact asset/listing pair at completion.
func (a *App) reconcileManagedIndustryExpertBeforeSave(expected managedIndustryExpertRecord, expectedScope string) bool {
	client, err := a.newExpertHubClient()
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), expertHubListTimeout)
	defer cancel()
	if a.managedIndustryCatalogueScope(client) != expectedScope {
		return false
	}
	catalog, err := client.ListManagedIndustryExperts(ctx)
	if err != nil {
		return false
	}
	if err := reconcileManagedIndustryCatalogue(expectedScope, catalog); err != nil {
		return false
	}
	_, err = managedIndustryCatalogueAsset(catalog, expected.AssetID, expected.ListingID)
	return err == nil
}

// PurchaseAndInstallManagedIndustryExpert preserves the user-scoped Expert
// Market entitlement boundary. It is the only GUI mutation allowed by a paid
// dashed industry placeholder.
func (a *App) PurchaseAndInstallManagedIndustryExpert(assetID, listingID string) error {
	assetID = strings.TrimSpace(assetID)
	listingID = strings.TrimSpace(listingID)
	if assetID == "" || listingID == "" {
		return fmt.Errorf("industry expert asset and listing are required")
	}
	// Treat the catalogue as the authority for this operation rather than the
	// browser arguments. A tenant reassignment/revocation between rendering a
	// dashed placeholder and pressing its action must not leave a withdrawn or
	// unrelated package marked as platform-managed on the device.
	client, err := a.newExpertHubClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), expertHubListTimeout)
	catalog, err := client.ListManagedIndustryExperts(ctx)
	cancel()
	if err != nil {
		return err
	}
	item, err := managedIndustryCatalogueAsset(catalog, assetID, listingID)
	if err != nil {
		return err
	}
	scope := a.managedIndustryCatalogueScope(client)
	if err := reconcileManagedIndustryCatalogue(scope, catalog); err != nil {
		return err
	}
	if _, err := a.PurchaseExpertMarketListing(listingID); err != nil {
		return err
	}
	result, err := a.InstallExpertMarketListingWithExpectedHash(listingID, item.PackageHash)
	if err != nil {
		return err
	}
	if result == nil || strings.TrimSpace(result.Expert.ID) == "" {
		return fmt.Errorf("industry expert installation returned no expert")
	}
	if !a.reconcileManagedIndustryExpertBeforeSave(item, scope) {
		// The market importer completed before the final authority check. Keep the
		// imported definition locked down instead of exposing it as a standalone
		// editable expert after the platform assignment disappeared.
		_ = defaultManagedIndustryExpertStore.SaveInactive(managedIndustryExpertInstall{AssetID: item.AssetID, ListingID: item.ListingID, LocalExpertID: result.Expert.ID, Version: item.Version, CatalogueScope: scope})
		invalidateExpertDefCache(result.Expert.ID)
		return fmt.Errorf("industry expert is no longer assigned to this tenant")
	}
	if err := defaultManagedIndustryExpertStore.Save(managedIndustryExpertInstall{AssetID: item.AssetID, ListingID: item.ListingID, LocalExpertID: result.Expert.ID, Version: item.Version, CatalogueScope: scope}); err != nil {
		return err
	}
	managedIndustryInstallFailures.Delete(managedIndustryInstallKey(scope, assetID))
	invalidateExpertDefCache(result.Expert.ID)
	return nil
}
