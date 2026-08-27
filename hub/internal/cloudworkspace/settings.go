package cloudworkspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	SettingsKey = "cloud_workspace"

	ModeOff         = "off"
	ModeAllUsers    = "all_users"
	ModeDepartments = "departments"

	defaultQuota = 5
	minQuota     = 1
	maxQuota     = 10

	defaultMaxWorkspaceBytes = 2 << 30   // 2GiB
	minMaxWorkspaceBytes     = 256 << 20 // 256MiB
	maxMaxWorkspaceBytes     = 8 << 30   // 8GiB

	defaultTenantMaxTotalBytes = 50 << 30 // 50GiB
	minTenantMaxTotalBytes     = 1 << 30  // 1GiB
	maxTenantMaxTotalBytes     = 1 << 40  // 1TiB
)

var (
	ErrInvalidInput        = errors.New("invalid cloud workspace settings")
	ErrSettingsUnavailable = errors.New("cloud workspace settings store is unavailable")
)

// Settings is the per-tenant cloud-workspace configuration stored in system_settings.
type Settings struct {
	Mode                string   `json:"mode"`
	Quota               int      `json:"quota"`
	DepartmentIDs       []string `json:"department_ids"`
	MaxWorkspaceBytes   int64    `json:"max_workspace_bytes"`
	TenantMaxTotalBytes int64    `json:"tenant_max_total_bytes"`
	UpdatedAt           string   `json:"updated_at"`
}

// Preview is the admin GET payload's live org-tree stats.
type Preview struct {
	DepartmentCount int      `json:"department_count"`
	UserCount       int      `json:"user_count"`
	OverQuotaUsers  []string `json:"over_quota_users"`
	UsedBytes       int64    `json:"used_bytes"`
}

// SettingsView is the admin GET/PUT response (settings plus preview).
type SettingsView struct {
	Settings
	Preview Preview `json:"preview"`
}

// UserDirectory looks up a bound user by ID (store.UserRepository satisfies this).
type UserDirectory interface {
	GetByID(ctx context.Context, id string) (*store.User, error)
}

// GroupLookup resolves a user's security group and group ancestry within a tenant.
type GroupLookup interface {
	GetUserGroupID(ctx context.Context, email string) (string, error)
	GetGroupByID(ctx context.Context, id string) (*security.SecurityGroup, error)
}

// OrgPreviewer walks the org tree for admin settings preview counts.
type OrgPreviewer interface {
	GetGroupTree(ctx context.Context) (*security.GroupTreeNode, error)
	ListGroupMembers(ctx context.Context, groupID string) ([]string, error)
}

// Service loads tenant settings and evaluates cloud-workspace grants.
type Service struct {
	System store.SystemSettingsRepository
	Users  UserDirectory
	Groups GroupLookup
	Org    OrgPreviewer
}

// NewService wires system settings, users, and security group lookups.
func NewService(system store.SystemSettingsRepository, users UserDirectory, sec *security.SecurityService) *Service {
	s := &Service{System: system, Users: users}
	if sec != nil {
		s.Groups = sec
		s.Org = sec
	}
	return s
}

func tenantSettingsStorageKey(tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" || tenantID == store.DefaultTenantID {
		return SettingsKey
	}
	return "tenant:" + tenantID + ":" + SettingsKey
}

// DefaultSettings returns product defaults (feature off, quota 5).
func DefaultSettings() Settings {
	return Settings{
		Mode:                ModeOff,
		Quota:               defaultQuota,
		DepartmentIDs:       []string{},
		MaxWorkspaceBytes:   defaultMaxWorkspaceBytes,
		TenantMaxTotalBytes: defaultTenantMaxTotalBytes,
	}
}

func emptyPreview() Preview {
	return Preview{OverQuotaUsers: []string{}}
}

func clampQuota(q int) int {
	if q < minQuota {
		return minQuota
	}
	if q > maxQuota {
		return maxQuota
	}
	return q
}

func clampInt64(v, defaultV, minV, maxV int64) int64 {
	if v <= 0 {
		return defaultV
	}
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func normalizeDepartmentIDs(ids []string) []string {
	if len(ids) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if out == nil {
		return []string{}
	}
	return out
}

func normalizeMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case ModeAllUsers:
		return ModeAllUsers
	case ModeDepartments:
		return ModeDepartments
	default:
		return ModeOff
	}
}

func fillSettingsDefaults(s Settings) Settings {
	out := s
	out.Mode = normalizeMode(out.Mode)
	if out.Quota <= 0 {
		out.Quota = defaultQuota
	} else {
		out.Quota = clampQuota(out.Quota)
	}
	out.DepartmentIDs = normalizeDepartmentIDs(out.DepartmentIDs)
	out.MaxWorkspaceBytes = clampInt64(out.MaxWorkspaceBytes, defaultMaxWorkspaceBytes, minMaxWorkspaceBytes, maxMaxWorkspaceBytes)
	out.TenantMaxTotalBytes = clampInt64(out.TenantMaxTotalBytes, defaultTenantMaxTotalBytes, minTenantMaxTotalBytes, maxTenantMaxTotalBytes)
	out.UpdatedAt = strings.TrimSpace(out.UpdatedAt)
	return out
}

func prepareForWrite(s Settings) (Settings, error) {
	mode := strings.TrimSpace(s.Mode)
	if mode == "" {
		mode = ModeOff
	}
	if mode != ModeOff && mode != ModeAllUsers && mode != ModeDepartments {
		return Settings{}, fmt.Errorf("%w: mode must be off, all_users, or departments", ErrInvalidInput)
	}
	out := s
	out.Mode = mode
	out.Quota = clampQuota(out.Quota)
	out.DepartmentIDs = normalizeDepartmentIDs(out.DepartmentIDs)
	out.MaxWorkspaceBytes = clampInt64(out.MaxWorkspaceBytes, defaultMaxWorkspaceBytes, minMaxWorkspaceBytes, maxMaxWorkspaceBytes)
	out.TenantMaxTotalBytes = clampInt64(out.TenantMaxTotalBytes, defaultTenantMaxTotalBytes, minTenantMaxTotalBytes, maxTenantMaxTotalBytes)
	if out.Mode == ModeDepartments && len(out.DepartmentIDs) == 0 {
		return Settings{}, fmt.Errorf("%w: department_ids required when mode is departments", ErrInvalidInput)
	}
	out.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return out, nil
}

// LoadTenantSettings returns effective cloud-workspace settings for a tenant.
func (s *Service) LoadTenantSettings(ctx context.Context, tenantID string) Settings {
	base := DefaultSettings()
	if s == nil || s.System == nil {
		return base
	}
	raw, err := s.System.Get(ctx, tenantSettingsStorageKey(tenantID))
	if err != nil || strings.TrimSpace(raw) == "" {
		return base
	}
	var stored Settings
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return base
	}
	return fillSettingsDefaults(stored)
}

// SaveTenantSettings clamps, validates, and persists tenant settings.
func (s *Service) SaveTenantSettings(ctx context.Context, tenantID string, settings Settings) (Settings, error) {
	if s == nil || s.System == nil {
		return Settings{}, ErrSettingsUnavailable
	}
	prepared, err := prepareForWrite(settings)
	if err != nil {
		return Settings{}, err
	}
	b, err := json.Marshal(prepared)
	if err != nil {
		return Settings{}, err
	}
	if err := s.System.Set(ctx, tenantSettingsStorageKey(tenantID), string(b)); err != nil {
		return Settings{}, err
	}
	return prepared, nil
}

// BuildPreview computes department/user counts from the org tree when mode=departments.
func (s *Service) BuildPreview(ctx context.Context, tenantID string, settings Settings) Preview {
	preview := emptyPreview()
	if s == nil || s.Org == nil || settings.Mode != ModeDepartments || len(settings.DepartmentIDs) == 0 {
		return preview
	}
	ctx = security.WithTenant(ctx, tenantID)
	tree, err := s.Org.GetGroupTree(ctx)
	if err != nil || tree == nil {
		return preview
	}
	idSet := collectSelectedAndDescendants(tree, settings.DepartmentIDs)
	preview.DepartmentCount = len(idSet)
	emails := make(map[string]struct{})
	for id := range idSet {
		members, err := s.Org.ListGroupMembers(ctx, id)
		if err != nil {
			continue
		}
		for _, email := range members {
			email = strings.ToLower(strings.TrimSpace(email))
			if email == "" {
				continue
			}
			// ListGroupMembers(root) also appends unassigned users; skip those so
			// user_count matches Granted (empty GetUserGroupID is always deny).
			if s.Groups != nil {
				gid, err := s.Groups.GetUserGroupID(ctx, email)
				if err != nil {
					continue
				}
				gid = strings.TrimSpace(gid)
				if gid == "" {
					continue
				}
				if _, ok := idSet[gid]; !ok {
					continue
				}
			}
			emails[email] = struct{}{}
		}
	}
	preview.UserCount = len(emails)
	return preview
}

func collectSelectedAndDescendants(root *security.GroupTreeNode, selected []string) map[string]struct{} {
	byID := map[string]*security.GroupTreeNode{}
	var index func(*security.GroupTreeNode)
	index = func(n *security.GroupTreeNode) {
		if n == nil {
			return
		}
		if id := strings.TrimSpace(n.ID); id != "" {
			byID[id] = n
		}
		for _, c := range n.Children {
			index(c)
		}
	}
	index(root)

	set := map[string]struct{}{}
	var add func(*security.GroupTreeNode)
	add = func(n *security.GroupTreeNode) {
		if n == nil {
			return
		}
		id := strings.TrimSpace(n.ID)
		if id == "" {
			return
		}
		if _, ok := set[id]; ok {
			return
		}
		set[id] = struct{}{}
		for _, c := range n.Children {
			add(c)
		}
	}
	for _, id := range selected {
		id = strings.TrimSpace(id)
		if n, ok := byID[id]; ok {
			add(n)
		}
	}
	return set
}
