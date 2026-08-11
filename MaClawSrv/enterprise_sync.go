package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/enterpriseknowledge"
)

// enterpriseSyncCoordinator runs Hub→local digital-asset sync for MaClawSrv users
// that have RemoteHubURL + RemoteViewerToken configured.
type enterpriseSyncCoordinator struct {
	svc      *agentservice.Service
	deviceID string
	stopCh   chan struct{}
	wg       sync.WaitGroup

	mu           sync.Mutex
	running      bool
	lastRunAt    time.Time
	lastError    string
	lastSynced   int
	lastEligible int
	disabled     bool

	// perUser serializes purge vs sync for the same tenant/user data dir (SQLite writers).
	perUser sync.Map // key "tenantID/userID" -> *sync.Mutex
}

type enterpriseSyncCoordStatus struct {
	Enabled      bool   `json:"enabled"`
	Running      bool   `json:"running"`
	LastRunAt    string `json:"last_run_at,omitempty"`
	LastError    string `json:"last_error,omitempty"`
	LastSynced   int    `json:"last_synced_users"`
	LastEligible int    `json:"last_eligible_users"`
	DeviceID     string `json:"device_id,omitempty"`
	Interval     string `json:"interval"`
}

type enterpriseUserLibraryView struct {
	LibraryID       string `json:"library_id"`
	Name            string `json:"name"`
	LastRev         int64  `json:"last_rev"`
	AccessState     string `json:"access_state"`
	ACLFingerprint  string `json:"acl_fingerprint"`
	LastSyncAt      string `json:"last_sync_at"`
	LastError       string `json:"last_error"`
	UserSyncEnabled bool   `json:"user_sync_enabled"`
	HubSyncEnabled  bool   `json:"hub_sync_enabled"`
}

type enterpriseUserSyncStatus struct {
	HubConfigured bool                        `json:"hub_configured"`
	HubURL        string                      `json:"hub_url,omitempty"`
	LibraryCount  int                         `json:"library_count"`
	Libraries     []enterpriseUserLibraryView `json:"libraries"`
	Coordinator   enterpriseSyncCoordStatus   `json:"coordinator"`
}

// enterpriseTenantProgress is admin rollup of enterprise cache state per tenant.
type enterpriseTenantProgress struct {
	TenantID              string                      `json:"tenant_id"`
	TenantName            string                      `json:"tenant_name,omitempty"`
	UsersTotal            int                         `json:"users_total"`
	UsersActive           int                         `json:"users_active"`
	UsersHubConfigured    int                         `json:"users_hub_configured"`
	UsersWithLibraries    int                         `json:"users_with_libraries"`
	UsersWithError        int                         `json:"users_with_error"`
	LibraryCount          int                         `json:"library_count"`
	ActiveLibraries       int                         `json:"active_libraries"`
	SyncDisabledLibraries int                         `json:"sync_disabled_libraries"`
	RevokedLibraries      int                         `json:"revoked_libraries"`
	LastSyncAt            string                      `json:"last_sync_at,omitempty"`
	Users                 []enterpriseUserProgressRow `json:"users,omitempty"`
}

type enterpriseUserProgressRow struct {
	UserID         string                      `json:"user_id"`
	Name           string                      `json:"name,omitempty"`
	Email          string                      `json:"email,omitempty"`
	Status         string                      `json:"status,omitempty"`
	HubConfigured  bool                        `json:"hub_configured"`
	HubURL         string                      `json:"hub_url,omitempty"`
	LibraryCount   int                         `json:"library_count"`
	ActiveCount    int                         `json:"active_count"`
	LastSyncAt     string                      `json:"last_sync_at,omitempty"`
	HasError       bool                        `json:"has_error"`
	LastError      string                      `json:"last_error,omitempty"`
	Libraries      []enterpriseUserLibraryView `json:"libraries,omitempty"`
}

type enterpriseTenantProgressReport struct {
	Coordinator enterpriseSyncCoordStatus   `json:"coordinator"`
	Tenants     []enterpriseTenantProgress  `json:"tenants"`
	TotalUsers  int                         `json:"total_users_scanned"`
}

func startEnterpriseDigitalAssetSync(svc *agentservice.Service) *enterpriseSyncCoordinator {
	if svc == nil {
		return nil
	}
	// Opt-out: MACLAW_ENTERPRISE_SYNC_DISABLED=true
	disabled := false
	if v := strings.TrimSpace(os.Getenv("MACLAW_ENTERPRISE_SYNC_DISABLED")); v == "1" || strings.EqualFold(v, "true") {
		log.Printf("[enterprise-sync] disabled via MACLAW_ENTERPRISE_SYNC_DISABLED")
		disabled = true
	}
	host, _ := os.Hostname()
	c := &enterpriseSyncCoordinator{
		svc:      svc,
		deviceID: "maclawsrv-" + host,
		stopCh:   make(chan struct{}),
		disabled: disabled,
	}
	if !disabled {
		c.wg.Add(1)
		go c.loop()
		log.Printf("[enterprise-sync] coordinator started (device_id=%s)", c.deviceID)
	}
	return c
}

func (c *enterpriseSyncCoordinator) Stop() {
	if c == nil {
		return
	}
	select {
	case <-c.stopCh:
	default:
		close(c.stopCh)
	}
	c.wg.Wait()
}

func (c *enterpriseSyncCoordinator) Status() enterpriseSyncCoordStatus {
	if c == nil {
		return enterpriseSyncCoordStatus{Enabled: false, Interval: "30m"}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	st := enterpriseSyncCoordStatus{
		Enabled:      !c.disabled,
		Running:      c.running,
		LastError:    c.lastError,
		LastSynced:   c.lastSynced,
		LastEligible: c.lastEligible,
		DeviceID:     c.deviceID,
		Interval:     "30m",
	}
	if !c.lastRunAt.IsZero() {
		st.LastRunAt = c.lastRunAt.UTC().Format(time.RFC3339)
	}
	return st
}

func (c *enterpriseSyncCoordinator) loop() {
	defer c.wg.Done()
	// Stagger first run slightly to avoid thundering herd with GUI clients.
	timer := time.NewTimer(45 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-timer.C:
			// Bound each background cycle so a stuck Hub cannot hang the coordinator forever.
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			// Wire stopCh so process shutdown cancels in-flight Hub HTTP promptly
			// (otherwise Stop() can block on wg until the full cycle timeout).
			stopWatchDone := make(chan struct{})
			go func() {
				select {
				case <-c.stopCh:
					cancel()
				case <-stopWatchDone:
				}
			}()
			if _, err := c.SyncAll(ctx); err != nil && !errors.Is(err, enterpriseknowledge.ErrSyncInProgress) {
				log.Printf("[enterprise-sync] background cycle: %v", err)
			}
			close(stopWatchDone)
			cancel()
			select {
			case <-c.stopCh:
				return
			default:
			}
			timer.Reset(30 * time.Minute)
		}
	}
}

// SyncAll runs one full cycle for all active users with hub credentials.
func (c *enterpriseSyncCoordinator) SyncAll(ctx context.Context) (enterpriseSyncCoordStatus, error) {
	if c == nil {
		return enterpriseSyncCoordStatus{Enabled: false}, fmt.Errorf("enterprise sync not available")
	}
	if c.disabled {
		return c.Status(), fmt.Errorf("enterprise sync disabled")
	}
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return c.Status(), enterpriseknowledge.ErrSyncInProgress
	}
	c.running = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.running = false
		c.lastRunAt = time.Now().UTC()
		c.mu.Unlock()
	}()

	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
	}

	users, err := c.svc.ListAllUsers(ctx, agentservice.ListAllUsersAdminInput{})
	if err != nil {
		c.setErr(err)
		return c.Status(), err
	}
	synced := 0
	eligible := 0
	var firstErr error
	for i, u := range users {
		select {
		case <-c.stopCh:
			return c.Status(), nil
		case <-ctx.Done():
			err := ctx.Err()
			c.setErr(err)
			return c.Status(), err
		default:
		}
		if u.Status != agentservice.UserStatusActive {
			continue
		}
		if i > 0 {
			// Bounded stagger between users (interruptible).
			select {
			case <-c.stopCh:
				return c.Status(), nil
			case <-ctx.Done():
				err := ctx.Err()
				c.setErr(err)
				return c.Status(), err
			case <-time.After(500 * time.Millisecond):
			}
		}
		ok, hadCreds, syncErr := c.syncUser(ctx, u)
		if hadCreds {
			eligible++
		}
		if ok {
			synced++
		}
		if syncErr != nil {
			c.setErr(syncErr)
			if firstErr == nil {
				firstErr = syncErr
			}
		}
	}
	c.mu.Lock()
	c.lastSynced = synced
	c.lastEligible = eligible
	if firstErr == nil {
		c.lastError = ""
	}
	c.mu.Unlock()
	if synced > 0 || eligible > 0 {
		log.Printf("[enterprise-sync] cycle finished: synced %d/%d user(s)", synced, eligible)
	}
	return c.Status(), firstErr
}

// SyncUser runs one sync for a single principal.
func (c *enterpriseSyncCoordinator) SyncUser(ctx context.Context, p agentservice.Principal) error {
	if c == nil {
		return fmt.Errorf("enterprise sync not available")
	}
	if c.disabled {
		return fmt.Errorf("enterprise sync disabled")
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
	}
	user := agentservice.User{ID: p.UserID, TenantID: p.TenantID, Status: agentservice.UserStatusActive}
	if got, err := c.svc.GetUser(ctx, p.TenantID, p.UserID); err == nil && got != nil {
		user = *got
	}
	if user.Status != agentservice.UserStatusActive && user.Status != "" {
		return fmt.Errorf("user is not active")
	}
	ok, hadCreds, syncErr := c.syncUser(ctx, user)
	if !hadCreds {
		return fmt.Errorf("hub credentials not configured (RemoteHubURL + RemoteViewerToken)")
	}
	if syncErr != nil {
		return syncErr
	}
	if !ok {
		return fmt.Errorf("sync did not complete")
	}
	return nil
}

// ListLibraries returns local enterprise libraries for a principal.
// Does not create meta/knowledge DBs when the user has never synced.
func (c *enterpriseSyncCoordinator) ListLibraries(p agentservice.Principal) ([]enterpriseUserLibraryView, error) {
	if c == nil || c.svc == nil {
		return nil, fmt.Errorf("enterprise sync not available")
	}
	dataDir := c.svc.UserDataRoot(p)
	if !enterpriseknowledge.MetaDBExists(dataDir) {
		return []enterpriseUserLibraryView{}, nil
	}
	lease, err := enterpriseknowledge.LeaseMeta(dataDir)
	if err != nil {
		return nil, err
	}
	defer lease.Release()
	libs, err := lease.Client.ListLibraries()
	if err != nil {
		return nil, err
	}
	out := make([]enterpriseUserLibraryView, 0, len(libs))
	for _, lib := range libs {
		out = append(out, enterpriseUserLibraryView{
			LibraryID:       lib.LibraryID,
			Name:            lib.Name,
			LastRev:         lib.LastRev,
			AccessState:     lib.AccessState,
			ACLFingerprint:  lib.ACLFingerprint,
			LastSyncAt:      lib.LastSyncAt,
			LastError:       lib.LastError,
			UserSyncEnabled: lib.UserSyncEnabled,
			HubSyncEnabled:  lib.HubSyncEnabled,
		})
	}
	return out, nil
}

// SetUserSync toggles per-library pull preference for a principal.
func (c *enterpriseSyncCoordinator) SetUserSync(p agentservice.Principal, libraryID string, enabled bool) error {
	if c == nil || c.svc == nil {
		return fmt.Errorf("enterprise sync not available")
	}
	mu := c.userMu(p)
	mu.Lock()
	defer mu.Unlock()
	dataDir := c.svc.UserDataRoot(p)
	// Avoid OpenMetaOnly creating empty state for users who never synced.
	if !enterpriseknowledge.MetaDBExists(dataDir) {
		return fmt.Errorf("library not found: %s", strings.TrimSpace(libraryID))
	}
	lease, err := enterpriseknowledge.LeaseMeta(dataDir)
	if err != nil {
		return err
	}
	defer lease.Release()
	return lease.Client.SetUserSync(libraryID, enabled)
}

func (c *enterpriseSyncCoordinator) userMu(p agentservice.Principal) *sync.Mutex {
	key := strings.TrimSpace(p.TenantID) + "/" + strings.TrimSpace(p.UserID)
	v, _ := c.perUser.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// PurgeLibrary deletes local enterprise cache for one library for a principal.
func (c *enterpriseSyncCoordinator) PurgeLibrary(p agentservice.Principal, libraryID string) error {
	if c == nil || c.svc == nil {
		return fmt.Errorf("enterprise sync not available")
	}
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return fmt.Errorf("library_id required")
	}
	dataDir := c.svc.UserDataRoot(p)
	if strings.TrimSpace(dataDir) == "" {
		return fmt.Errorf("empty data dir")
	}
	// Do not Open() when meta is absent: Open creates empty DBs for typo tenant/user paths.
	if !enterpriseknowledge.MetaDBExists(dataDir) {
		return fmt.Errorf("library not found: %s", libraryID)
	}
	mu := c.userMu(p)
	mu.Lock()
	defer mu.Unlock()
	// Drop pooled clients so purge sees a clean store and subsequent readers re-open.
	enterpriseknowledge.InvalidateCache(dataDir)
	client, err := enterpriseknowledge.Open(dataDir)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.PurgeLibrary(libraryID); err != nil {
		return err
	}
	enterpriseknowledge.InvalidateCache(dataDir)
	return nil
}

// progressUserRow is one deep-scanned user plus rollup-only counters for summary mode
// (when libraries[] is omitted from the public row).
type progressUserRow struct {
	tenantID          string
	row               enterpriseUserProgressRow
	syncDisabledCount int
	revokedCount      int
}

// TenantProgress scans user enterprise caches and rolls up by tenant.
// includeUsers attaches per-user detail (heavier). tenantFilter limits to one tenant_id.
//
// Cost controls:
//   - Inactive users without a local meta DB: head-count only (no GetUserConfig).
//   - Active users or anyone with meta: hub config (active only) + library meta.
//   - Deep scans use a small worker pool (bounded parallelism).
func (c *enterpriseSyncCoordinator) TenantProgress(ctx context.Context, tenantFilter string, includeUsers bool) (enterpriseTenantProgressReport, error) {
	rep := enterpriseTenantProgressReport{
		Coordinator: c.Status(),
		Tenants:     []enterpriseTenantProgress{},
	}
	if c == nil || c.svc == nil {
		return rep, fmt.Errorf("enterprise sync not available")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tenantFilter = strings.TrimSpace(tenantFilter)
	users, err := c.svc.ListAllUsers(ctx, agentservice.ListAllUsersAdminInput{TenantID: tenantFilter})
	if err != nil {
		return rep, err
	}
	nameByID := map[string]string{}
	if tenants, terr := c.svc.ListTenants(ctx, agentservice.ListTenantsInput{}); terr == nil {
		for _, tn := range tenants {
			nameByID[tn.ID] = tn.Name
		}
	}

	byTenant := map[string]*enterpriseTenantProgress{}
	var deep []agentservice.User
	for _, u := range users {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		rep.TotalUsers++
		tid := u.TenantID
		tp := byTenant[tid]
		if tp == nil {
			tp = &enterpriseTenantProgress{
				TenantID:   tid,
				TenantName: nameByID[tid],
				Users:      []enterpriseUserProgressRow{},
			}
			byTenant[tid] = tp
		}
		tp.UsersTotal++
		active := u.Status == agentservice.UserStatusActive
		if active {
			tp.UsersActive++
		}
		p := agentservice.Principal{TenantID: u.TenantID, UserID: u.ID}
		hasMeta := enterpriseknowledge.MetaDBExists(c.svc.UserDataRoot(p))
		// Deep-scan active users (hub eligibility) or anyone with a local cache (purge UI).
		if active || hasMeta {
			deep = append(deep, u)
			continue
		}
		if includeUsers {
			tp.Users = append(tp.Users, enterpriseUserProgressRow{
				UserID: u.ID,
				Name:   u.Name,
				Email:  u.Email,
				Status: string(u.Status),
			})
		}
	}

	scans, err := c.scanTenantProgressUsers(ctx, deep, includeUsers)
	if err != nil {
		return rep, err
	}
	for _, sc := range scans {
		tp := byTenant[sc.tenantID]
		if tp == nil {
			continue
		}
		row := sc.row
		if row.HubConfigured {
			tp.UsersHubConfigured++
		}
		if row.LibraryCount > 0 {
			tp.UsersWithLibraries++
		}
		tp.LibraryCount += row.LibraryCount
		tp.ActiveLibraries += row.ActiveCount
		tp.SyncDisabledLibraries += sc.syncDisabledCount
		tp.RevokedLibraries += sc.revokedCount
		if row.LastSyncAt != "" && (tp.LastSyncAt == "" || row.LastSyncAt > tp.LastSyncAt) {
			tp.LastSyncAt = row.LastSyncAt
		}
		if row.HasError {
			tp.UsersWithError++
		}
		if includeUsers {
			tp.Users = append(tp.Users, row)
		}
	}

	for _, tp := range byTenant {
		if !includeUsers {
			tp.Users = nil
		} else if len(tp.Users) > 1 {
			sort.Slice(tp.Users, func(i, j int) bool {
				return tp.Users[i].UserID < tp.Users[j].UserID
			})
		}
		rep.Tenants = append(rep.Tenants, *tp)
	}
	sort.Slice(rep.Tenants, func(i, j int) bool {
		return rep.Tenants[i].TenantID < rep.Tenants[j].TenantID
	})
	return rep, nil
}

func (c *enterpriseSyncCoordinator) scanTenantProgressUsers(ctx context.Context, users []agentservice.User, includeUsers bool) ([]progressUserRow, error) {
	if len(users) == 0 {
		return nil, nil
	}
	const maxWorkers = 8
	workers := maxWorkers
	if len(users) < workers {
		workers = len(users)
	}
	// Buffer jobs/out to reduce send stalls under uneven per-user IO.
	jobs := make(chan agentservice.User, workers*2)
	out := make(chan progressUserRow, workers*2)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for u := range jobs {
				// Drain remaining jobs after cancel without more IO; producer closes jobs on cancel.
				if ctx.Err() != nil {
					continue
				}
				out <- c.scanUserProgressRow(ctx, u, includeUsers)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, u := range users {
			select {
			case <-ctx.Done():
				return
			case jobs <- u:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(out)
	}()
	results := make([]progressUserRow, 0, len(users))
	for row := range out {
		results = append(results, row)
	}
	if err := ctx.Err(); err != nil {
		// Prefer complete partial results over hard-fail when the client already
		// disconnected mid-scan; callers with cancelled ctx usually discard anyway.
		return results, err
	}
	return results, nil
}

func (c *enterpriseSyncCoordinator) scanUserProgressRow(ctx context.Context, u agentservice.User, includeUsers bool) progressUserRow {
	sc := progressUserRow{
		tenantID: u.TenantID,
		row: enterpriseUserProgressRow{
			UserID: u.ID,
			Name:   u.Name,
			Email:  u.Email,
			Status: string(u.Status),
		},
	}
	p := agentservice.Principal{TenantID: u.TenantID, UserID: u.ID}
	// Hub config only for active users (inactive cannot sync).
	if u.Status == agentservice.UserStatusActive {
		if cfg, err := c.svc.GetUserConfig(ctx, p); err == nil && cfg != nil {
			hubURL := strings.TrimRight(strings.TrimSpace(cfg.AppConfig.RemoteHubURL), "/")
			token := strings.TrimSpace(cfg.AppConfig.RemoteViewerToken)
			if includeUsers {
				sc.row.HubURL = hubURL
			}
			sc.row.HubConfigured = hubURL != "" && token != ""
		}
	}
	if !enterpriseknowledge.MetaDBExists(c.svc.UserDataRoot(p)) {
		return sc
	}
	libs, err := c.ListLibraries(p)
	if err != nil {
		return sc
	}
	sc.row.LibraryCount = len(libs)
	var latest string
	for _, lib := range libs {
		switch lib.AccessState {
		case "active":
			sc.row.ActiveCount++
		case "sync_disabled":
			sc.syncDisabledCount++
		case "revoked":
			sc.revokedCount++
		}
		if lib.LastError != "" {
			sc.row.HasError = true
			if sc.row.LastError == "" {
				sc.row.LastError = lib.LastError
			}
		}
		if lib.LastSyncAt != "" && (latest == "" || lib.LastSyncAt > latest) {
			latest = lib.LastSyncAt
		}
	}
	sc.row.LastSyncAt = latest
	if includeUsers {
		sc.row.Libraries = libs
	}
	return sc
}

// UserStatus returns libraries + hub config summary for a principal.
func (c *enterpriseSyncCoordinator) UserStatus(ctx context.Context, p agentservice.Principal) (enterpriseUserSyncStatus, error) {
	st := enterpriseUserSyncStatus{
		Coordinator: c.Status(),
		Libraries:   []enterpriseUserLibraryView{},
	}
	if c == nil || c.svc == nil {
		return st, fmt.Errorf("enterprise sync not available")
	}
	cfg, err := c.svc.GetUserConfig(ctx, p)
	if err == nil && cfg != nil {
		hubURL := strings.TrimRight(strings.TrimSpace(cfg.AppConfig.RemoteHubURL), "/")
		token := strings.TrimSpace(cfg.AppConfig.RemoteViewerToken)
		// Expose hub base URL only (no token); omit filesystem paths.
		st.HubURL = hubURL
		st.HubConfigured = hubURL != "" && token != ""
	}
	libs, err := c.ListLibraries(p)
	if err != nil {
		return st, err
	}
	st.Libraries = libs
	st.LibraryCount = len(libs)
	return st, nil
}

func (c *enterpriseSyncCoordinator) syncUser(ctx context.Context, u agentservice.User) (ok bool, hadCreds bool, err error) {
	p := agentservice.Principal{TenantID: u.TenantID, UserID: u.ID}
	cfg, err := c.svc.GetUserConfig(ctx, p)
	if err != nil || cfg == nil {
		return false, false, nil
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.AppConfig.RemoteHubURL), "/")
	token := strings.TrimSpace(cfg.AppConfig.RemoteViewerToken)
	if hubURL == "" || token == "" {
		return false, false, nil
	}
	dataDir := c.svc.UserDataRoot(p)
	if dataDir == "" {
		return false, true, fmt.Errorf("empty data dir")
	}
	// Serialize with admin/user purge on the same data dir.
	mu := c.userMu(p)
	mu.Lock()
	defer mu.Unlock()
	client, err := enterpriseknowledge.Open(dataDir)
	if err != nil {
		log.Printf("[enterprise-sync] open %s/%s: %v", u.TenantID, u.ID, err)
		return false, true, err
	}
	defer client.Close()

	agent := enterpriseknowledge.NewSyncAgent(client, func() (string, string, error) {
		return hubURL, token, nil
	}, c.deviceID+"-"+u.ID)
	if agent == nil {
		return false, true, fmt.Errorf("sync agent unavailable")
	}
	if err := agent.RunOnce(ctx); err != nil {
		log.Printf("[enterprise-sync] user %s/%s: %v", u.TenantID, u.ID, err)
		return false, true, err
	}
	// Ensure search/auto-recall leases re-open after a full pull wrote new revs.
	enterpriseknowledge.InvalidateCache(dataDir)
	return true, true, nil
}

func (c *enterpriseSyncCoordinator) setErr(err error) {
	if c == nil || err == nil {
		return
	}
	c.mu.Lock()
	c.lastError = err.Error()
	c.mu.Unlock()
}
