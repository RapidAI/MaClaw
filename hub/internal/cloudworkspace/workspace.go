package cloudworkspace

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	StatusActive  = "active"
	StatusDeleted = "deleted"

	ReasonMachineUnbound = "machine_unbound"
	ReasonNotGranted     = "not_granted"

	idPrefix          = "cws_"
	maxNameRunes      = 64
	defaultNamePrefix = "工作区 "
	RestoreWindow     = 7 * 24 * time.Hour
)

var (
	ErrUnavailable        = errors.New("cloud workspace store is unavailable")
	ErrNotFound           = errors.New("cloud workspace not found")
	ErrQuota              = errors.New("cloud workspace quota exceeded")
	ErrTenantDisk         = errors.New("cloud workspace tenant disk exceeded")
	ErrNameTaken          = errors.New("cloud workspace name taken")
	ErrInvalidName        = errors.New("invalid cloud workspace name")
	ErrRestoreWindow      = errors.New("cloud workspace restore window expired")
	ErrInUse              = errors.New("cloud workspace in use")
	ErrLeaseRequired      = errors.New("cloud workspace lease required")
	ErrWorkspaceSize      = errors.New("cloud workspace size exceeded")
	ErrVolumeFull         = errors.New("cloud workspace volume full")
	ErrRevisionConflict   = errors.New("cloud workspace revision conflict")
	ErrInvalidPath        = errors.New("invalid cloud workspace path")
	ErrBlobHashMismatch   = errors.New("cloud workspace object hash mismatch")
	ErrObjectMissing      = errors.New("cloud workspace object missing")
	ErrTooManyEntries     = errors.New("cloud workspace file count exceeded")
	ErrIncompleteChunks   = errors.New("cloud workspace object chunks incomplete")
	ErrInvalidChunkIndex  = errors.New("invalid cloud workspace chunk index")
	ErrContentLength      = errors.New("cloud workspace content-length required")
	ErrInvalidSidecarName = errors.New("invalid cloud workspace sidecar name")

	defaultNamePattern = regexp.MustCompile(`^工作区 ([1-9][0-9]*)$`)
)

// Workspace is one cloud_workspaces row.
type Workspace struct {
	ID               string
	TenantID         string
	UserID           string
	Name             string
	NameNorm         string
	Status           string
	UsedBytes        int64
	FileCount        int
	ManifestRevision string
	CreatedAt        string
	UpdatedAt        string
	DeletedAt        string
}

func newWorkspaceID() string {
	return idPrefix + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func normalizeName(name string) string {
	return strings.ToLower(norm.NFC.String(strings.TrimSpace(name)))
}

func validateDisplayName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("%w: name is required", ErrInvalidName)
	}
	n := utf8.RuneCountInString(name)
	if n < 1 || n > maxNameRunes {
		return "", fmt.Errorf("%w: name must be 1–%d characters", ErrInvalidName, maxNameRunes)
	}
	return name, nil
}

func nextDefaultName(existing []string) string {
	used := make(map[int]struct{}, len(existing))
	for _, name := range existing {
		m := defaultNamePattern.FindStringSubmatch(name)
		if len(m) != 2 {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 {
			continue
		}
		used[n] = struct{}{}
	}
	for i := 1; ; i++ {
		if _, ok := used[i]; !ok {
			return defaultNamePrefix + strconv.Itoa(i)
		}
	}
}

func purgeAfter(deletedAt string) string {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(deletedAt))
	if err != nil {
		return ""
	}
	return t.UTC().Add(RestoreWindow).Format(time.RFC3339)
}

func restoreDeadline(deletedAt string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(deletedAt))
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC().Add(RestoreWindow), true
}

// EntitlementLease is the exclusive-lease snapshot on an entitlement workspace.
type EntitlementLease struct {
	Held        bool   `json:"held"`
	MachineID   string `json:"machine_id"`
	MachineName string `json:"machine_name"`
	IsSelf      bool   `json:"is_self"`
	ExpiresAt   string `json:"expires_at"`
}

// EntitlementWorkspace is one active row in the entitlement payload.
type EntitlementWorkspace struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	UsedBytes   int64             `json:"used_bytes"`
	UpdatedAt   string            `json:"updated_at"`
	TaskName    string            `json:"task_name,omitempty"`
	TaskMode    string            `json:"task_mode,omitempty"`
	Lease       *EntitlementLease `json:"lease,omitempty"`
	LeaseInUse  bool              `json:"lease_in_use,omitempty"`
	LeaseHolder string            `json:"lease_holder,omitempty"`
}

// EntitlementDeletedWorkspace is one soft-deleted row in the entitlement payload.
type EntitlementDeletedWorkspace struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	UsedBytes  int64  `json:"used_bytes"`
	UpdatedAt  string `json:"updated_at"`
	DeletedAt  string `json:"deleted_at"`
	PurgeAfter string `json:"purge_after"`
}

// Entitlement is GET /api/v1/cloud-workspaces/entitlement.
type Entitlement struct {
	Enabled           bool                          `json:"enabled"`
	Quota             int                           `json:"quota"`
	Used              int                           `json:"used"`
	MaxWorkspaceBytes int64                         `json:"max_workspace_bytes"`
	Workspaces        []EntitlementWorkspace        `json:"workspaces"`
	Deleted           []EntitlementDeletedWorkspace `json:"deleted"`
	Reason            string                        `json:"reason,omitempty"`
}

func emptyEntitlement(settings Settings) Entitlement {
	return Entitlement{
		Quota:             settings.Quota,
		MaxWorkspaceBytes: settings.MaxWorkspaceBytes,
		Workspaces:        []EntitlementWorkspace{},
		Deleted:           []EntitlementDeletedWorkspace{},
	}
}

// EntitlementFor returns the caller's grant plus their own workspace rows.
func (s *Service) EntitlementFor(ctx context.Context, principal auth.MachinePrincipal) (Entitlement, error) {
	if s == nil {
		return emptyEntitlement(DefaultSettings()), nil
	}
	tenantID := store.NormalizeTenantID(principal.TenantID)
	settings := s.LoadTenantSettings(ctx, tenantID)
	out := emptyEntitlement(settings)
	granted, err := s.granted(ctx, principal, settings)
	if err != nil {
		return Entitlement{}, err
	}
	out.Enabled = granted
	if !granted {
		if strings.TrimSpace(principal.UserID) == "" {
			out.Reason = ReasonMachineUnbound
		} else {
			out.Reason = ReasonNotGranted
		}
	}
	userID := strings.TrimSpace(principal.UserID)
	if s.Workspaces == nil || userID == "" {
		return out, nil
	}
	rows, err := s.Workspaces.ListOwned(ctx, tenantID, userID)
	if err != nil {
		return Entitlement{}, err
	}
	leases, err := s.Workspaces.ListActiveLeases(ctx, tenantID, userID)
	if err != nil {
		return Entitlement{}, err
	}
	now := s.now()
	for _, ws := range rows {
		if ws == nil {
			continue
		}
		switch ws.Status {
		case StatusActive:
			out.Used++
			item := EntitlementWorkspace{
				ID:        ws.ID,
				Name:      ws.Name,
				UsedBytes: ws.UsedBytes,
				UpdatedAt: ws.UpdatedAt,
			}
			if task := s.taskSidecarFor(ctx, tenantID, userID, ws.ID); task.Name != "" || task.Mode != "" {
				item.TaskName = task.Name
				item.TaskMode = task.Mode
			}
			if lease := leases[ws.ID]; lease != nil {
				item.Lease = &EntitlementLease{
					Held:        !leaseExpired(lease.ExpiresAt, now),
					MachineID:   lease.MachineID,
					MachineName: lease.MachineName,
					IsSelf:      lease.MachineID == principal.MachineID,
					ExpiresAt:   lease.ExpiresAt,
				}
				projectEntitlementLease(&item)
			}
			out.Workspaces = append(out.Workspaces, item)
		case StatusDeleted:
			out.Deleted = append(out.Deleted, EntitlementDeletedWorkspace{
				ID:         ws.ID,
				Name:       ws.Name,
				UsedBytes:  ws.UsedBytes,
				UpdatedAt:  ws.UpdatedAt,
				DeletedAt:  ws.DeletedAt,
				PurgeAfter: purgeAfter(ws.DeletedAt),
			})
		}
	}
	return out, nil
}

// projectEntitlementLease fills the GUI-facing occupied fields. Nested
// lease stays the source of truth; lease_in_use is only set for a live
// holder that is not this machine.
func projectEntitlementLease(item *EntitlementWorkspace) {
	if item == nil || item.Lease == nil || !item.Lease.Held || item.Lease.IsSelf {
		return
	}
	item.LeaseInUse = true
	holder := strings.TrimSpace(item.Lease.MachineName)
	if holder == "" {
		holder = strings.TrimSpace(item.Lease.MachineID)
	}
	item.LeaseHolder = holder
}

// CreateWorkspace inserts an active workspace under quota in one IMMEDIATE transaction.
func (s *Service) CreateWorkspace(ctx context.Context, principal auth.MachinePrincipal, name string) (*Workspace, error) {
	if s == nil || s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	settings := s.LoadTenantSettings(ctx, principal.TenantID)
	return s.Workspaces.Create(ctx, CreateParams{
		TenantID:            principal.TenantID,
		UserID:              principal.UserID,
		Name:                name,
		Quota:               settings.Quota,
		TenantMaxTotalBytes: settings.TenantMaxTotalBytes,
	}, s.now())
}

// RenameWorkspace changes the display name of an active owned workspace.
func (s *Service) RenameWorkspace(ctx context.Context, principal auth.MachinePrincipal, id, name string) (*Workspace, error) {
	if s == nil || s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	name, err := validateDisplayName(name)
	if err != nil {
		return nil, err
	}
	return s.Workspaces.Rename(ctx, principal.TenantID, principal.UserID, id, name, normalizeName(name), s.now())
}

// SoftDeleteWorkspace marks an active owned workspace deleted and frees quota.
func (s *Service) SoftDeleteWorkspace(ctx context.Context, principal auth.MachinePrincipal, id string) (*Workspace, error) {
	if s == nil || s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	return s.Workspaces.SoftDelete(ctx, principal.TenantID, principal.UserID, principal.MachineID, id, s.now())
}

// RestoreWorkspace undeletes a workspace within 7 days if quota allows.
func (s *Service) RestoreWorkspace(ctx context.Context, principal auth.MachinePrincipal, id string) (*Workspace, error) {
	if s == nil || s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	settings := s.LoadTenantSettings(ctx, principal.TenantID)
	return s.Workspaces.Restore(ctx, principal.TenantID, principal.UserID, id, settings.Quota, s.now())
}

// HardDeleteDeletedWorkspace permanently removes an owned soft-deleted workspace.
func (s *Service) HardDeleteDeletedWorkspace(ctx context.Context, principal auth.MachinePrincipal, id string) error {
	if s == nil || s.Workspaces == nil {
		return ErrUnavailable
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrNotFound
	}
	// Remove encrypted objects and sidecars before dropping the database row.
	// Keep the row when filesystem cleanup fails so the operation can be retried.
	if s.Blobs == nil {
		return ErrUnavailable
	}
	{
		rows, err := s.Workspaces.ListOwned(ctx, principal.TenantID, principal.UserID)
		if err != nil {
			return err
		}
		found := false
		for _, row := range rows {
			if row != nil && row.ID == id {
				if row.Status != StatusDeleted {
					return ErrNotFound
				}
				found = true
				if err := s.Blobs.RemoveWorkspace(row.TenantID, row.UserID, row.ID); err != nil {
					return err
				}
				break
			}
		}
		if !found {
			return ErrNotFound
		}
	}
	return s.Workspaces.HardDeleteDeleted(ctx, principal.TenantID, principal.UserID, id)
}
