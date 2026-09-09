package guiapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// CloudWorkspaceEntitlementWorkspace is one active row in the entitlement payload.
type CloudWorkspaceEntitlementWorkspace struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Status             string `json:"status,omitempty"`
	UsedBytes          int64  `json:"used_bytes"`
	LogicalBytes       int64  `json:"logical_bytes"`
	RetainedBytes      int64  `json:"retained_bytes"`
	SnapshotBytes      int64  `json:"snapshot_retained_bytes"`
	StagingBytes       int64  `json:"staging_bytes"`
	UnreferencedBytes  int64  `json:"unreferenced_retained_bytes"`
	UpdatedAt          string `json:"updated_at"`
	SyncProtocol       string `json:"sync_protocol"`
	ServerRevision     string `json:"server_revision"`
	TaskName           string `json:"task_name,omitempty"`
	TaskMode           string `json:"task_mode,omitempty"`
	CloudTaskID        string `json:"cloud_task_id,omitempty"`
	BindingVersion     int64  `json:"binding_version,omitempty"`
	ProvisionState     string `json:"provision_state,omitempty"`
	ProvisionOperation string `json:"provision_operation_id,omitempty"`
	LeaseInUse         bool   `json:"lease_in_use,omitempty"`
	LeaseHolder        string `json:"lease_holder,omitempty"`
	HandoffRequestedAt string `json:"handoff_requested_at,omitempty"`
	// ReconcileRequired is local writer-cache state, overlaid by the GUI on
	// top of Hub entitlement.  It is intentionally omitted when false so older
	// Hub/frontend payloads remain wire-compatible.
	ReconcileRequired bool   `json:"reconcile_required,omitempty"`
	ReconcileReason   string `json:"reconcile_reason,omitempty"`
}

type cloudWorkspaceHubLease struct {
	Held               bool   `json:"held"`
	MachineID          string `json:"machine_id"`
	MachineName        string `json:"machine_name"`
	IsSelf             bool   `json:"is_self"`
	ExpiresAt          string `json:"expires_at"`
	HandoffRequestedAt string `json:"handoff_requested_at,omitempty"`
}

func applyCloudWorkspaceEntitlementLease(ws *CloudWorkspaceEntitlementWorkspace, lease *cloudWorkspaceHubLease) {
	if ws == nil {
		return
	}
	if lease != nil {
		ws.HandoffRequestedAt = lease.HandoffRequestedAt
	}
	if ws.LeaseInUse || strings.TrimSpace(ws.LeaseHolder) != "" {
		return
	}
	if lease == nil || !lease.Held || lease.IsSelf {
		return
	}
	ws.LeaseInUse = true
	holder := strings.TrimSpace(lease.MachineName)
	if holder == "" {
		holder = strings.TrimSpace(lease.MachineID)
	}
	ws.LeaseHolder = holder
}

// UnmarshalJSON accepts Hub's nested lease plus the flat GUI fields.
func (ws *CloudWorkspaceEntitlementWorkspace) UnmarshalJSON(data []byte) error {
	type workspaceAlias CloudWorkspaceEntitlementWorkspace
	aux := struct {
		workspaceAlias
		Lease *cloudWorkspaceHubLease `json:"lease"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*ws = CloudWorkspaceEntitlementWorkspace(aux.workspaceAlias)
	applyCloudWorkspaceEntitlementLease(ws, aux.Lease)
	return nil
}

// CloudWorkspaceDeletedWorkspace is one soft-deleted row in the entitlement payload.
type CloudWorkspaceDeletedWorkspace struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	UsedBytes         int64  `json:"used_bytes"`
	LogicalBytes      int64  `json:"logical_bytes"`
	RetainedBytes     int64  `json:"retained_bytes"`
	SnapshotBytes     int64  `json:"snapshot_retained_bytes"`
	StagingBytes      int64  `json:"staging_bytes"`
	UnreferencedBytes int64  `json:"unreferenced_retained_bytes"`
	UpdatedAt         string `json:"updated_at"`
	DeletedAt         string `json:"deleted_at"`
	PurgeAfter        string `json:"purge_after"`
}

// CloudWorkspaceEntitlement is the Wails projection of GET /api/v1/cloud-workspaces/entitlement.
// HubUnavailable/Banner are client-side: network/5xx must not fake Enabled=false.
type CloudWorkspaceEntitlement struct {
	Enabled             bool                                 `json:"enabled"`
	SyncProtocol        string                               `json:"sync_protocol"`
	ReadOnlyAllowed     bool                                 `json:"read_only_allowed"`
	Quota               int                                  `json:"quota"`
	Used                int                                  `json:"used"`
	MaxWorkspaceBytes   int64                                `json:"max_workspace_bytes"`
	TenantMaxTotalBytes int64                                `json:"tenant_max_total_bytes"`
	LogicalBytes        int64                                `json:"logical_bytes"`
	RetainedBytes       int64                                `json:"retained_bytes"`
	SnapshotBytes       int64                                `json:"snapshot_retained_bytes"`
	StagingBytes        int64                                `json:"staging_bytes"`
	UnreferencedBytes   int64                                `json:"unreferenced_retained_bytes"`
	Workspaces          []CloudWorkspaceEntitlementWorkspace `json:"workspaces"`
	Deleted             []CloudWorkspaceDeletedWorkspace     `json:"deleted"`
	Reason              string                               `json:"reason,omitempty"`
	HubUnavailable      bool                                 `json:"hub_unavailable"`
	Banner              string                               `json:"banner"`
}

var (
	cloudWorkspaceEntitlementMu       sync.Mutex
	cloudWorkspaceEntitlementCache    CloudWorkspaceEntitlement
	cloudWorkspaceEntitlementHasCache bool
)

func resetCloudWorkspaceEntitlementCache() {
	cloudWorkspaceEntitlementMu.Lock()
	defer cloudWorkspaceEntitlementMu.Unlock()
	cloudWorkspaceEntitlementCache = CloudWorkspaceEntitlement{}
	cloudWorkspaceEntitlementHasCache = false
}

func loadCloudWorkspaceEntitlementCache() (CloudWorkspaceEntitlement, bool) {
	cloudWorkspaceEntitlementMu.Lock()
	defer cloudWorkspaceEntitlementMu.Unlock()
	if !cloudWorkspaceEntitlementHasCache {
		return CloudWorkspaceEntitlement{}, false
	}
	return cloneCloudWorkspaceEntitlement(cloudWorkspaceEntitlementCache), true
}

func storeCloudWorkspaceEntitlementCache(ent CloudWorkspaceEntitlement) {
	cloudWorkspaceEntitlementMu.Lock()
	defer cloudWorkspaceEntitlementMu.Unlock()
	stored := cloneCloudWorkspaceEntitlement(ent)
	stored.HubUnavailable = false
	stored.Banner = ""
	cloudWorkspaceEntitlementCache = stored
	cloudWorkspaceEntitlementHasCache = true
}

func cloneCloudWorkspaceEntitlement(src CloudWorkspaceEntitlement) CloudWorkspaceEntitlement {
	dst := src
	if src.Workspaces != nil {
		dst.Workspaces = append([]CloudWorkspaceEntitlementWorkspace(nil), src.Workspaces...)
	} else {
		dst.Workspaces = []CloudWorkspaceEntitlementWorkspace{}
	}
	if src.Deleted != nil {
		dst.Deleted = append([]CloudWorkspaceDeletedWorkspace(nil), src.Deleted...)
	} else {
		dst.Deleted = []CloudWorkspaceDeletedWorkspace{}
	}
	return dst
}

func emptyCloudWorkspaceEntitlement() CloudWorkspaceEntitlement {
	return CloudWorkspaceEntitlement{
		SyncProtocol:    "v1-sequential",
		ReadOnlyAllowed: true,
		Workspaces:      []CloudWorkspaceEntitlementWorkspace{},
		Deleted:         []CloudWorkspaceDeletedWorkspace{},
	}
}

func hubUnavailableEntitlement(cached CloudWorkspaceEntitlement, hasCache bool) CloudWorkspaceEntitlement {
	out := emptyCloudWorkspaceEntitlement()
	if hasCache {
		out = cached
	}
	out.HubUnavailable = true
	out.Banner = cloudWorkspaceHubUnavailableBanner
	return out
}

// CloudWorkspaceEntitlement fetches the caller's cloud-workspace grant from Hub.
// Network and 5xx errors keep the last successful process-session result and
// set HubUnavailable; they never write Enabled=false. 4xx means Hub answered
// (not granted / unauthorized) and must not look like an outage.
func (a *App) CloudWorkspaceEntitlement() CloudWorkspaceEntitlement {
	ent, err := a.fetchCloudWorkspaceEntitlement()
	if err != nil {
		cached, hasCache := loadCloudWorkspaceEntitlementCache()
		out := hubUnavailableEntitlement(cached, hasCache)
		a.decorateCloudWorkspaceEntitlementLocalState(&out)
		return out
	}
	ent.HubUnavailable = false
	ent.Banner = ""
	storeCloudWorkspaceEntitlementCache(ent)
	a.decorateCloudWorkspaceEntitlementLocalState(&ent)
	a.purgeCloudWorkspaceDeletedCaches(ent.Deleted)
	return ent
}

// purgeCloudWorkspaceDeletedCaches applies the revocation side of the local
// cache policy.  A successful entitlement response is authoritative; when Hub
// reports a workspace in the soft-deleted set, keeping a readable writer or
// browse cache locally would outlive the user's access decision.  Network and
// 5xx failures never call this path, so an outage cannot cause destructive
// local cleanup.
func (a *App) purgeCloudWorkspaceDeletedCaches(deleted []CloudWorkspaceDeletedWorkspace) {
	for _, ws := range deleted {
		id := strings.TrimSpace(ws.ID)
		if !validCloudWorkspaceCacheID(id) {
			continue
		}
		hadLocalCopy := a.cloudWorkspaceLocalCachesPresent(id)
		if err := a.purgeCloudWorkspaceLocalCaches(id); err != nil {
			a.recordCloudWorkspaceAudit(cloudWorkspaceAuditEvent{WorkspaceID: id, Operation: "cache_purge", Outcome: "failed", Detail: err.Error()})
			continue
		}
		if hadLocalCopy {
			a.recordCloudWorkspaceAudit(cloudWorkspaceAuditEvent{WorkspaceID: id, Operation: "cache_purge", Outcome: "revoked", Detail: "revoked"})
		}
	}
}

// decorateCloudWorkspaceEntitlementLocalState overlays durable watcher
// reconciliation markers from this process's writer caches.  Hub remains the
// authority for workspace metadata; these fields only make a local
// fail-closed condition visible in the UI after a restart (when the in-memory
// mount no longer exists).
func (a *App) decorateCloudWorkspaceEntitlementLocalState(ent *CloudWorkspaceEntitlement) {
	if a == nil || ent == nil {
		return
	}
	for i := range ent.Workspaces {
		ws := &ent.Workspaces[i]
		if strings.TrimSpace(ws.ID) == "" {
			continue
		}
		root := normalizeProjectSessionPath(a.cloudWorkspaceCachePath(a.cloudWorkspaceTenantID(), ws.ID))
		state, err := readCloudWorkspaceLocalState(root)
		if err != nil || !state.ReconcileRequired {
			continue
		}
		ws.ReconcileRequired = true
		ws.ReconcileReason = state.ReconcileReason
	}
}

func (a *App) fetchCloudWorkspaceEntitlement() (CloudWorkspaceEntitlement, error) {
	ctx, cancel := a.cloudWorkspaceRequestContext()
	defer cancel()
	data, status, err := a.cloudWorkspaceHubRequest(ctx, http.MethodGet, cloudWorkspaceEntitlementPath, nil)
	if err != nil {
		return CloudWorkspaceEntitlement{}, err
	}
	if status >= 500 || status == http.StatusRequestTimeout || status == http.StatusTooManyRequests {
		return CloudWorkspaceEntitlement{}, fmt.Errorf("hub returned %d", status)
	}
	if status >= 300 {
		// Hub answered (401/403/404): grant is off, not an outage.
		return emptyCloudWorkspaceEntitlement(), nil
	}
	out := emptyCloudWorkspaceEntitlement()
	if err := json.Unmarshal(data, &out); err != nil {
		return CloudWorkspaceEntitlement{}, fmt.Errorf("invalid entitlement response: %w", err)
	}
	if out.Workspaces == nil {
		out.Workspaces = []CloudWorkspaceEntitlementWorkspace{}
	}
	if out.Deleted == nil {
		out.Deleted = []CloudWorkspaceDeletedWorkspace{}
	}
	return out, nil
}
