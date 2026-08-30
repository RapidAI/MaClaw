package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// CloudWorkspaceEntitlementWorkspace is one active row in the entitlement payload.
type CloudWorkspaceEntitlementWorkspace struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	UsedBytes   int64  `json:"used_bytes"`
	UpdatedAt   string `json:"updated_at"`
	TaskName    string `json:"task_name,omitempty"`
	TaskMode    string `json:"task_mode,omitempty"`
	LeaseInUse  bool   `json:"lease_in_use,omitempty"`
	LeaseHolder string `json:"lease_holder,omitempty"`
}

type cloudWorkspaceHubLease struct {
	Held        bool   `json:"held"`
	MachineID   string `json:"machine_id"`
	MachineName string `json:"machine_name"`
	IsSelf      bool   `json:"is_self"`
	ExpiresAt   string `json:"expires_at"`
}

func applyCloudWorkspaceEntitlementLease(ws *CloudWorkspaceEntitlementWorkspace, lease *cloudWorkspaceHubLease) {
	if ws == nil {
		return
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
	ID         string `json:"id"`
	Name       string `json:"name"`
	UsedBytes  int64  `json:"used_bytes"`
	UpdatedAt  string `json:"updated_at"`
	DeletedAt  string `json:"deleted_at"`
	PurgeAfter string `json:"purge_after"`
}

// CloudWorkspaceEntitlement is the Wails projection of GET /api/v1/cloud-workspaces/entitlement.
// HubUnavailable/Banner are client-side: network/5xx must not fake Enabled=false.
type CloudWorkspaceEntitlement struct {
	Enabled           bool                                 `json:"enabled"`
	Quota             int                                  `json:"quota"`
	Used              int                                  `json:"used"`
	MaxWorkspaceBytes int64                                `json:"max_workspace_bytes"`
	Workspaces        []CloudWorkspaceEntitlementWorkspace `json:"workspaces"`
	Deleted           []CloudWorkspaceDeletedWorkspace     `json:"deleted"`
	Reason            string                               `json:"reason,omitempty"`
	HubUnavailable    bool                                 `json:"hub_unavailable"`
	Banner            string                               `json:"banner"`
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
		Workspaces: []CloudWorkspaceEntitlementWorkspace{},
		Deleted:    []CloudWorkspaceDeletedWorkspace{},
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
		return hubUnavailableEntitlement(cached, hasCache)
	}
	ent.HubUnavailable = false
	ent.Banner = ""
	storeCloudWorkspaceEntitlementCache(ent)
	return ent
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
