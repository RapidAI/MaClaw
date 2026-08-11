package security

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/google/uuid"
)

const settingsKey = "security_settings"
const groupTreeCacheTTL = 10 * time.Second
const settingsCacheTTL = 10 * time.Second

// SecurityService provides business logic for security group management,
// policy computation, and system settings.
type SecurityService struct {
	store  *SecurityStore
	system store.SystemSettingsRepository
	audit  store.AdminAuditRepository
	users  store.UserRepository // optional; used to discover bound users not yet in any security group
	cache  sync.Map             // tenantID + userEmail -> *EffectivePolicy

	// skillSourcesProvider resolves per-user skill source restrictions from the
	// independent three-level control (global/tenant/user). When set, GetHeartbeatPolicy
	// intersects its result with the security group policy's SkillSourcesAllowed.
	skillSourcesProvider SkillSourcesResolver

	groupTreeMu          sync.RWMutex
	groupTreeCached      *GroupTreeNode
	groupTreeCachedUntil time.Time

	settingsMu          sync.RWMutex
	settingsCached      *SecuritySettings
	settingsCachedUntil time.Time
}

// NewSecurityService creates a new SecurityService.
func NewSecurityService(
	secStore *SecurityStore,
	system store.SystemSettingsRepository,
	audit store.AdminAuditRepository,
	users ...store.UserRepository,
) *SecurityService {
	svc := &SecurityService{
		store:  secStore,
		system: system,
		audit:  audit,
	}
	if len(users) > 0 && users[0] != nil {
		svc.users = users[0]
	}
	return svc
}

// SkillSourcesResolver resolves per-runtime-user skill source restrictions.
// Defined here (consumer side) to avoid import cycles with hub/internal/skill.
type SkillSourcesResolver interface {
	ResolveForUser(ctx context.Context, userID, tenantID string) []string
}

// SetSkillSourcesProvider injects the skill source control provider.
// Called during hub initialization after both SecurityService and SourceControlService
// are created.
func (s *SecurityService) SetSkillSourcesProvider(p SkillSourcesResolver) {
	s.skillSourcesProvider = p
}

// --- Group Management ---

// CreateGroup creates a new child group under the given parent.
// Validates that the parent exists and tree depth does not exceed 10.
func (s *SecurityService) CreateGroup(ctx context.Context, name, parentID string) (*SecurityGroup, error) {
	parent, err := s.store.GetGroupByID(ctx, parentID)
	if err != nil {
		return nil, fmt.Errorf("lookup parent: %w", err)
	}
	if parent == nil {
		return nil, fmt.Errorf("parent group not found")
	}

	parentDepth, err := s.store.GetGroupDepth(ctx, parentID)
	if err != nil {
		return nil, fmt.Errorf("get parent depth: %w", err)
	}
	if parentDepth+1 >= 10 {
		return nil, fmt.Errorf("group tree depth exceeds maximum (10)")
	}

	now := time.Now().UTC()
	group := &SecurityGroup{
		TenantID:  tenantIDFromContext(ctx),
		ID:        uuid.New().String(),
		Name:      name,
		ParentID:  parentID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.CreateGroup(ctx, group); err != nil {
		return nil, fmt.Errorf("create group: %w", err)
	}
	s.invalidateGroupTreeCache()
	return group, nil
}

// RenameGroup renames an existing group.
func (s *SecurityService) RenameGroup(ctx context.Context, id, name string) error {
	if err := s.store.UpdateGroupName(ctx, id, name); err != nil {
		return err
	}
	s.invalidateGroupTreeCache()
	return nil
}

// DeleteGroup deletes a group and all its descendants, moving their users to Root_Group.
// Refuses to delete the root group.
func (s *SecurityService) DeleteGroup(ctx context.Context, id string) error {
	if err := s.ensureRootGroup(ctx); err != nil {
		return err
	}
	root, err := s.store.GetRootGroup(ctx)
	if err != nil {
		return fmt.Errorf("get root group: %w", err)
	}
	if root == nil {
		return fmt.Errorf("root group not found")
	}
	if id == root.ID {
		return fmt.Errorf("cannot delete root group")
	}

	// Collect all descendant group IDs (including the target group itself)
	allGroups, err := s.store.ListGroups(ctx)
	if err != nil {
		return fmt.Errorf("list groups: %w", err)
	}

	descendantIDs := collectDescendants(allGroups, id)
	descendantIDs = append(descendantIDs, id)

	// Move all users from these groups to root
	if err := s.store.MoveUsersToRoot(ctx, descendantIDs, root.ID); err != nil {
		return fmt.Errorf("move users to root: %w", err)
	}

	// Delete all descendant groups + the group itself
	for _, gid := range descendantIDs {
		if err := s.store.DeleteGroup(ctx, gid); err != nil {
			return fmt.Errorf("delete group %s: %w", gid, err)
		}
	}
	s.invalidateGroupTreeCache()
	return nil
}

// collectDescendants returns all descendant group IDs of the given group (not including itself).
func collectDescendants(groups []*SecurityGroup, parentID string) []string {
	childMap := make(map[string][]string)
	for _, g := range groups {
		if g.ParentID != "" {
			childMap[g.ParentID] = append(childMap[g.ParentID], g.ID)
		}
	}
	var result []string
	queue := childMap[parentID]
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)
		queue = append(queue, childMap[current]...)
	}
	return result
}

// GetGroupTree builds the complete tree structure starting from the root group.
func (s *SecurityService) GetGroupTree(ctx context.Context) (*GroupTreeNode, error) {
	if err := s.ensureRootGroup(ctx); err != nil {
		return nil, err
	}
	if tenantIDFromContext(ctx) != store.DefaultTenantID {
		return s.getGroupTreeUncached(ctx)
	}
	now := time.Now()
	s.groupTreeMu.RLock()
	if s.groupTreeCached != nil && now.Before(s.groupTreeCachedUntil) {
		cached := cloneGroupTreeNode(s.groupTreeCached)
		s.groupTreeMu.RUnlock()
		return cached, nil
	}
	s.groupTreeMu.RUnlock()

	root, err := s.getGroupTreeUncached(ctx)
	if err != nil {
		return nil, err
	}
	cloned := cloneGroupTreeNode(root)
	s.groupTreeMu.Lock()
	s.groupTreeCached = cloneGroupTreeNode(root)
	s.groupTreeCachedUntil = time.Now().Add(groupTreeCacheTTL)
	s.groupTreeMu.Unlock()
	return cloned, nil
}

func (s *SecurityService) getGroupTreeUncached(ctx context.Context) (*GroupTreeNode, error) {
	groups, err := s.store.ListGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	if len(groups) == 0 {
		return nil, nil
	}

	// Build node map and count members
	counts, err := s.store.CountGroupMembersMap(ctx)
	if err != nil {
		return nil, fmt.Errorf("count group members: %w", err)
	}
	nodeMap := make(map[string]*GroupTreeNode, len(groups))
	for _, g := range groups {
		nodeMap[g.ID] = &GroupTreeNode{
			TenantID:    g.TenantID,
			ID:          g.ID,
			Name:        g.Name,
			ParentID:    g.ParentID,
			MemberCount: counts[g.ID],
			Children:    []*GroupTreeNode{},
		}
	}

	// Link children to parents, find root
	var root *GroupTreeNode
	for _, node := range nodeMap {
		if node.ParentID == "" {
			root = node
			continue
		}
		if parent, ok := nodeMap[node.ParentID]; ok {
			parent.Children = append(parent.Children, node)
		}
	}

	// Augment root member count with bound users not in any security group.
	if root != nil && s.users != nil {
		extra := s.countUnassignedUsers(ctx)
		root.MemberCount += extra
	}
	rollupGroupMemberCounts(root)
	return root, nil
}

func (s *SecurityService) GetRootGroupNode(ctx context.Context) (*GroupTreeNode, error) {
	root, err := s.GetGroupTree(ctx)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, nil
	}
	return &GroupTreeNode{
		TenantID:    root.TenantID,
		ID:          root.ID,
		Name:        root.Name,
		ParentID:    root.ParentID,
		MemberCount: root.MemberCount,
		HasChildren: len(root.Children) > 0,
		Children:    []*GroupTreeNode{},
	}, nil
}

func cloneGroupTreeNode(node *GroupTreeNode) *GroupTreeNode {
	if node == nil {
		return nil
	}
	cloned := &GroupTreeNode{
		TenantID:    node.TenantID,
		ID:          node.ID,
		Name:        node.Name,
		ParentID:    node.ParentID,
		MemberCount: node.MemberCount,
		Children:    make([]*GroupTreeNode, 0, len(node.Children)),
	}
	for _, child := range node.Children {
		cloned.Children = append(cloned.Children, cloneGroupTreeNode(child))
	}
	return cloned
}

func rollupGroupMemberCounts(node *GroupTreeNode) int {
	if node == nil {
		return 0
	}
	total := node.MemberCount
	for _, child := range node.Children {
		total += rollupGroupMemberCounts(child)
	}
	node.MemberCount = total
	return total
}

func (s *SecurityService) invalidateGroupTreeCache() {
	s.groupTreeMu.Lock()
	defer s.groupTreeMu.Unlock()
	s.groupTreeCached = nil
	s.groupTreeCachedUntil = time.Time{}
}

func (s *SecurityService) cacheSettings(ctx context.Context, settings *SecuritySettings) {
	if tenantIDFromContext(ctx) != store.DefaultTenantID {
		return
	}
	if settings == nil {
		settings = &SecuritySettings{}
	}
	cached := *settings
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	s.settingsCached = &cached
	s.settingsCachedUntil = time.Now().Add(settingsCacheTTL)
}

func (s *SecurityService) invalidateSettingsCache() {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	s.settingsCached = nil
	s.settingsCachedUntil = time.Time{}
}

// --- User Assignment ---

// AssignUser assigns a user to a group. The UPSERT in the store handles
// the single-group invariant (automatically removes from old group).
func (s *SecurityService) AssignUser(ctx context.Context, email, groupID string) error {
	if err := s.store.AssignUser(ctx, email, groupID); err != nil {
		return err
	}
	s.cache.Delete(securityPolicyCacheKey(ctx, email))
	s.invalidateGroupTreeCache()
	return nil
}

// ListGroupMembers returns the member emails for a given group.
func (s *SecurityService) ListGroupMembers(ctx context.Context, groupID string) ([]string, error) {
	if err := s.ensureRootGroup(ctx); err != nil {
		return nil, err
	}
	members, err := s.store.ListGroupMembers(ctx, groupID)
	if err != nil {
		return nil, err
	}

	// When listing root group members, also include bound users that are not
	// in any security group (legacy users enrolled before security management).
	if s.users != nil {
		root, rerr := s.store.GetRootGroup(ctx)
		if rerr == nil && root != nil && root.ID == groupID {
			unassigned := s.listUnassignedEmails(ctx)
			if unassigned != nil {
				members = append(members, unassigned...)
			}
		}
	}

	return dedupeEmails(members), nil
}

// countUnassignedUsers returns the number of active bound users that have no
// record in security_group_members. Returns 0 on any error.
func (s *SecurityService) countUnassignedUsers(ctx context.Context) int {
	return len(s.listUnassignedEmails(ctx))
}

// listUnassignedEmails returns emails of active bound users not in any security group.
// Returns nil on any error to avoid polluting results with incorrect data.
func (s *SecurityService) listUnassignedEmails(ctx context.Context) []string {
	if s.users == nil {
		return nil
	}
	allUsers, err := s.users.ListByTenant(ctx, tenantIDFromContext(ctx))
	if err != nil {
		return nil
	}
	allAssigned, err := s.store.ListAllAssignedEmails(ctx)
	if err != nil {
		// Cannot determine who is assigned; return nil to avoid false positives.
		return nil
	}
	assigned := make(map[string]struct{}, len(allAssigned))
	for _, e := range allAssigned {
		key := strings.TrimSpace(strings.ToLower(e))
		if key == "" {
			continue
		}
		assigned[key] = struct{}{}
	}
	seen := map[string]struct{}{}
	var result []string
	for _, u := range allUsers {
		if u == nil {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(u.Email))
		if key == "" {
			continue
		}
		if _, ok := assigned[key]; ok {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, u.Email)
	}
	return result
}

// GetGroupChildren returns the direct child groups of a given group with member counts.
func (s *SecurityService) GetGroupChildren(ctx context.Context, parentID string) ([]*GroupTreeNode, error) {
	allGroups, err := s.store.ListGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	counts, err := s.store.CountGroupMembersMap(ctx)
	if err != nil {
		return nil, fmt.Errorf("count group members: %w", err)
	}
	childMap := make(map[string][]string, len(allGroups))
	for _, g := range allGroups {
		childMap[g.ParentID] = append(childMap[g.ParentID], g.ID)
	}
	var subtreeCount func(string) int
	subtreeCount = func(groupID string) int {
		total := counts[groupID]
		for _, childID := range childMap[groupID] {
			total += subtreeCount(childID)
		}
		return total
	}
	var children []*GroupTreeNode
	for _, g := range allGroups {
		if g.ParentID == parentID {
			hasChildren := false
			for _, g2 := range allGroups {
				if g2.ParentID == g.ID {
					hasChildren = true
					break
				}
			}
			children = append(children, &GroupTreeNode{
				TenantID:    g.TenantID,
				ID:          g.ID,
				Name:        g.Name,
				ParentID:    g.ParentID,
				MemberCount: subtreeCount(g.ID),
				HasChildren: hasChildren,
			})
		}
	}
	return children, nil
}

// RemoveUser removes a user from their assigned group.
func (s *SecurityService) RemoveUser(ctx context.Context, groupID, email string) error {
	if err := s.store.RemoveUser(ctx, email); err != nil {
		return err
	}
	s.invalidateGroupTreeCache()
	return nil
}

// --- Policy Management ---

// GetGroupPolicy returns the policy view for a group, annotating each item
// with its source ("self" or "inherited").
func (s *SecurityService) GetGroupPolicy(ctx context.Context, groupID string) (*GroupPolicyView, error) {
	if err := s.ensureRootGroup(ctx); err != nil {
		return nil, err
	}

	// Get the path from root to this group
	path, err := s.getPathToRoot(ctx, groupID)
	if err != nil {
		return nil, err
	}

	// Build a map of groupID -> group for name lookups
	allGroups, err := s.store.ListGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	groupNames := make(map[string]string, len(allGroups))
	for _, g := range allGroups {
		groupNames[g.ID] = g.Name
	}

	// Get the group's own policy
	selfPolicy, err := s.store.GetGroupPolicy(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("get self policy: %w", err)
	}

	// Compute effective values and track sources
	type sourceInfo struct {
		value       interface{}
		sourceGroup string
	}
	effectiveMap := make(map[string]sourceInfo)

	// Start with defaults (from root)
	defaultMap := policyToMap(DefaultPolicy)
	root, _ := s.store.GetRootGroup(ctx)
	rootID := ""
	if root != nil {
		rootID = root.ID
	}
	for k, v := range defaultMap {
		effectiveMap[k] = sourceInfo{value: v, sourceGroup: rootID}
	}

	// Walk path from root to target group, applying each group's policy
	for _, gid := range path {
		gPolicy, err := s.store.GetGroupPolicy(ctx, gid)
		if err != nil {
			continue
		}
		for k, v := range gPolicy {
			effectiveMap[k] = sourceInfo{value: v, sourceGroup: gid}
		}
	}

	// Build the view
	view := &GroupPolicyView{
		GroupID: groupID,
		Items:   make(map[string]PolicyItemView),
	}
	for k, info := range effectiveMap {
		source := "inherited"
		if _, ok := selfPolicy[k]; ok {
			source = "self"
		}
		view.Items[k] = PolicyItemView{
			Value:       info.value,
			Source:      source,
			SourceGroup: info.sourceGroup,
			SourceName:  groupNames[info.sourceGroup],
		}
	}
	return view, nil
}

// UpdateGroupPolicy updates the sparse policy for a group and invalidates
// the cache for the subtree.
func (s *SecurityService) UpdateGroupPolicy(ctx context.Context, groupID string, policy map[string]interface{}) error {
	if err := validatePolicyOverrides(policy); err != nil {
		return err
	}
	if err := s.store.SetGroupPolicy(ctx, groupID, policy); err != nil {
		return fmt.Errorf("set group policy: %w", err)
	}
	s.InvalidateCacheForSubtree(ctx, groupID)
	return nil
}

func validatePolicyOverrides(policy map[string]interface{}) error {
	for key, value := range policy {
		switch key {
		case "file_outbound_enabled", "image_outbound_enabled", "gossip_enabled", "yolo_mode_allowed", "smart_route_enabled":
			if _, ok := toBool(value); !ok {
				return fmt.Errorf("%s must be boolean", key)
			}
		case "guardrail_mode":
			if !stringIn(value, "none", "standard", "relaxed", "strict", "developer") {
				return fmt.Errorf("%s has invalid value", key)
			}
		case "sandbox_mode":
			if !stringIn(value, "none", "os", "docker") {
				return fmt.Errorf("%s has invalid value", key)
			}
		case "network_level":
			if !stringIn(value, "none", "intranet", "allowlist", "full") {
				return fmt.Errorf("%s has invalid value", key)
			}
		case "network_allowlist":
			if _, ok := toStringSlice(value); !ok {
				return fmt.Errorf("%s must be a string array", key)
			}
		case "skill_sources_allowed":
			arr, ok := toStringSlice(value)
			if !ok {
				return fmt.Errorf("%s must be a string array", key)
			}
			for _, source := range arr {
				if !isAllowedSkillSourceValue(source) {
					return fmt.Errorf("%s contains invalid source %q", key, source)
				}
			}
		default:
			return fmt.Errorf("unknown policy field %s", key)
		}
	}
	return nil
}

func isAllowedSkillSourceValue(source string) bool {
	switch canonicalSource(source) {
	case "skillhub", "clawhub", "github", "enterprise_hub", "local":
		return true
	default:
		return false
	}
}

func stringIn(value interface{}, allowed ...string) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	s = strings.TrimSpace(strings.ToLower(s))
	for _, item := range allowed {
		if s == item {
			return true
		}
	}
	return false
}

// --- Effective Policy Computation ---

// GetEffectivePolicy computes the effective policy for a user by walking
// from Root_Group to the user's group, merging policies along the way.
func (s *SecurityService) GetEffectivePolicy(ctx context.Context, email string) (*EffectivePolicy, error) {
	cacheKey := securityPolicyCacheKey(ctx, email)
	if cached, ok := s.cache.Load(cacheKey); ok {
		return cached.(*EffectivePolicy), nil
	}

	groupID, err := s.resolveUserPolicyGroup(ctx, email)
	if err != nil {
		return nil, err
	}

	policy, err := s.computeEffectiveForGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}

	s.cache.Store(cacheKey, policy)
	return policy, nil
}

// GetUserPolicyView returns the effective user policy plus annotated source
// information for the security group that supplied it.
func (s *SecurityService) GetUserPolicyView(ctx context.Context, email string) (string, []PolicyGroupPathItem, *EffectivePolicy, *GroupPolicyView, error) {
	groupID, err := s.resolveUserPolicyGroup(ctx, email)
	if err != nil {
		return "", nil, nil, nil, err
	}
	policy, err := s.GetEffectivePolicy(ctx, email)
	if err != nil {
		return "", nil, nil, nil, err
	}
	view, err := s.GetGroupPolicy(ctx, groupID)
	if err != nil {
		return "", nil, nil, nil, err
	}
	path, err := s.getGroupPathItems(ctx, groupID)
	if err != nil {
		return "", nil, nil, nil, err
	}
	return groupID, path, policy, view, nil
}

func (s *SecurityService) getGroupPathItems(ctx context.Context, groupID string) ([]PolicyGroupPathItem, error) {
	ids, err := s.getPathToRoot(ctx, groupID)
	if err != nil {
		return nil, err
	}
	items := make([]PolicyGroupPathItem, 0, len(ids))
	for _, id := range ids {
		group, err := s.store.GetGroupByID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("get group %s: %w", id, err)
		}
		if group == nil {
			items = append(items, PolicyGroupPathItem{ID: id})
			continue
		}
		items = append(items, PolicyGroupPathItem{TenantID: group.TenantID, ID: group.ID, Name: group.Name, ParentID: group.ParentID})
	}
	return items, nil
}

// GetUserGroupID returns the security group the email is assigned to.
// Empty string means the user is not in any group (no root fallback).
// Used by approval-role runtime resolution for applicant_department scopes.
func (s *SecurityService) GetUserGroupID(ctx context.Context, email string) (string, error) {
	if s == nil || s.store == nil {
		return "", nil
	}
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return "", nil
	}
	return s.store.GetUserGroup(ctx, email)
}

// GetGroupByID returns a security group by id within the tenant from context.
// Used by digital asset ACL ancestor walks.
func (s *SecurityService) GetGroupByID(ctx context.Context, id string) (*SecurityGroup, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	return s.store.GetGroupByID(ctx, id)
}

func (s *SecurityService) resolveUserPolicyGroup(ctx context.Context, email string) (string, error) {
	groupID, err := s.store.GetUserGroup(ctx, email)
	if err != nil {
		return "", fmt.Errorf("get user group: %w", err)
	}
	if groupID != "" {
		return groupID, nil
	}
	if err := s.ensureRootGroup(ctx); err != nil {
		return "", err
	}
	root, err := s.store.GetRootGroup(ctx)
	if err != nil {
		return "", fmt.Errorf("get root group: %w", err)
	}
	if root == nil {
		return "", fmt.Errorf("root group not found")
	}
	return root.ID, nil
}

// GetGroupEffectivePolicy computes the effective policy for a specific group
// (preview, not for a user).
func (s *SecurityService) GetGroupEffectivePolicy(ctx context.Context, groupID string) (*EffectivePolicy, error) {
	return s.computeEffectiveForGroup(ctx, groupID)
}

// computeEffectiveForGroup walks from root to the given group, merging policies.
func (s *SecurityService) computeEffectiveForGroup(ctx context.Context, groupID string) (*EffectivePolicy, error) {
	path, err := s.getPathToRoot(ctx, groupID)
	if err != nil {
		return nil, err
	}

	// Start with default policy
	result := DefaultPolicy

	// Apply each group's policy along the path
	for _, gid := range path {
		gPolicy, err := s.store.GetGroupPolicy(ctx, gid)
		if err != nil {
			continue
		}
		applyPolicyOverrides(&result, gPolicy)
	}

	return &result, nil
}

// getPathToRoot returns the path from root to the given group (root first).
func (s *SecurityService) getPathToRoot(ctx context.Context, groupID string) ([]string, error) {
	var path []string
	currentID := groupID
	for {
		path = append(path, currentID)
		group, err := s.store.GetGroupByID(ctx, currentID)
		if err != nil {
			return nil, fmt.Errorf("get group %s: %w", currentID, err)
		}
		if group == nil {
			break
		}
		if group.ParentID == "" {
			// Reached root
			break
		}
		currentID = group.ParentID
	}
	// Reverse so root is first
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path, nil
}

// applyPolicyOverrides applies sparse policy overrides onto an EffectivePolicy.
func applyPolicyOverrides(p *EffectivePolicy, overrides map[string]interface{}) {
	for k, v := range overrides {
		switch k {
		case "file_outbound_enabled":
			if b, ok := toBool(v); ok {
				p.FileOutboundEnabled = b
			}
		case "image_outbound_enabled":
			if b, ok := toBool(v); ok {
				p.ImageOutboundEnabled = b
			}
		case "gossip_enabled":
			if b, ok := toBool(v); ok {
				p.GossipEnabled = b
			}
		case "guardrail_mode":
			if s, ok := v.(string); ok {
				p.GuardrailMode = s
			}
		case "sandbox_mode":
			if s, ok := v.(string); ok {
				p.SandboxMode = s
			}
		case "network_level":
			if s, ok := v.(string); ok {
				p.NetworkLevel = s
			}
		case "network_allowlist":
			if arr, ok := toStringSlice(v); ok {
				p.NetworkAllowlist = arr
			}
		case "yolo_mode_allowed":
			if b, ok := toBool(v); ok {
				p.YoloModeAllowed = b
			}
		case "smart_route_enabled":
			if b, ok := toBool(v); ok {
				p.SmartRouteEnabled = b
			}
		case "skill_sources_allowed":
			if arr, ok := toStringSlice(v); ok {
				p.SkillSourcesAllowed = arr
				p.SkillSourcesRestricted = true
			}
		case "skill_sources_restricted":
			if b, ok := toBool(v); ok {
				p.SkillSourcesRestricted = b
			}
		}
	}
}

// toBool converts an interface{} to bool, handling JSON number booleans.
func toBool(v interface{}) (bool, bool) {
	switch b := v.(type) {
	case bool:
		return b, true
	case float64:
		return b != 0, true
	}
	return false, false
}

// toStringSlice converts an interface{} to []string, handling JSON arrays.
func toStringSlice(v interface{}) ([]string, bool) {
	switch arr := v.(type) {
	case []interface{}:
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			s = strings.TrimSpace(s)
			if s != "" {
				result = append(result, s)
			}
		}
		return result, true
	case []string:
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			item = strings.TrimSpace(item)
			if item != "" {
				result = append(result, item)
			}
		}
		return result, true
	}
	return nil, false
}

func dedupeEmails(emails []string) []string {
	seen := make(map[string]struct{}, len(emails))
	result := make([]string, 0, len(emails))
	for _, email := range emails {
		trimmed := strings.TrimSpace(email)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

// policyToMap converts an EffectivePolicy to a map[string]interface{}.
func policyToMap(p EffectivePolicy) map[string]interface{} {
	m := map[string]interface{}{
		"file_outbound_enabled":  p.FileOutboundEnabled,
		"image_outbound_enabled": p.ImageOutboundEnabled,
		"gossip_enabled":         p.GossipEnabled,
		"guardrail_mode":         p.GuardrailMode,
		"sandbox_mode":           p.SandboxMode,
		"network_level":          p.NetworkLevel,
		"yolo_mode_allowed":      p.YoloModeAllowed,
		"smart_route_enabled":    p.SmartRouteEnabled,
	}
	if p.SkillSourcesRestricted || len(p.SkillSourcesAllowed) > 0 {
		// Convert to []interface{} for JSON compatibility in sparse override storage.
		arr := make([]interface{}, len(p.SkillSourcesAllowed))
		for i, s := range p.SkillSourcesAllowed {
			arr[i] = s
		}
		m["skill_sources_allowed"] = arr
		m["skill_sources_restricted"] = true
	}
	if len(p.NetworkAllowlist) > 0 {
		arr := make([]interface{}, len(p.NetworkAllowlist))
		for i, s := range p.NetworkAllowlist {
			arr[i] = s
		}
		m["network_allowlist"] = arr
	}
	return m
}

// --- System Settings ---

// GetSettings reads the security settings from the system settings store.
func (s *SecurityService) GetSettings(ctx context.Context) (*SecuritySettings, error) {
	if tenantIDFromContext(ctx) != store.DefaultTenantID {
		return s.getSettingsUncached(ctx)
	}
	now := time.Now()
	s.settingsMu.RLock()
	if s.settingsCached != nil && now.Before(s.settingsCachedUntil) {
		cached := *s.settingsCached
		s.settingsMu.RUnlock()
		return &cached, nil
	}
	s.settingsMu.RUnlock()

	settings, err := s.getSettingsUncached(ctx)
	if err != nil {
		return nil, err
	}
	settingsCopy := *settings
	s.cacheSettings(ctx, &settingsCopy)
	return settings, nil
}

func (s *SecurityService) getSettingsUncached(ctx context.Context) (*SecuritySettings, error) {
	if s.system == nil {
		return &SecuritySettings{}, nil
	}
	raw, err := s.system.Get(ctx, settingsStorageKey(ctx))
	if err != nil || raw == "" {
		return &SecuritySettings{}, nil
	}
	var settings SecuritySettings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return nil, fmt.Errorf("unmarshal security settings: %w", err)
	}
	return &settings, nil
}

// UpdateSettings writes the security settings and records audit logs
// when centralized_security_enabled or org_structure_enabled changes.
func (s *SecurityService) UpdateSettings(ctx context.Context, settings *SecuritySettings, adminUserID string) error {
	old, err := s.GetSettings(ctx)
	if err != nil {
		old = &SecuritySettings{}
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal security settings: %w", err)
	}
	if err := s.system.Set(ctx, settingsStorageKey(ctx), string(data)); err != nil {
		return fmt.Errorf("save security settings: %w", err)
	}
	s.cacheSettings(ctx, settings)

	// Audit log for centralized_security_enabled change
	if old.CentralizedSecurityEnabled != settings.CentralizedSecurityEnabled {
		action := "centralized_security_disabled"
		if settings.CentralizedSecurityEnabled {
			action = "centralized_security_enabled"
		}
		s.writeAuditLog(ctx, adminUserID, action, map[string]interface{}{
			"old_value": old.CentralizedSecurityEnabled,
			"new_value": settings.CentralizedSecurityEnabled,
		})
	}

	// Audit log for org_structure_enabled change
	if old.OrgStructureEnabled != settings.OrgStructureEnabled {
		action := "org_structure_disabled"
		if settings.OrgStructureEnabled {
			action = "org_structure_enabled"
		}
		s.writeAuditLog(ctx, adminUserID, action, map[string]interface{}{
			"old_value": old.OrgStructureEnabled,
			"new_value": settings.OrgStructureEnabled,
		})
	}

	s.invalidateGroupTreeCache()
	return nil
}

// SetDefaultGroup sets the default group for new users.
// Validates that the target group exists.
func (s *SecurityService) SetDefaultGroup(ctx context.Context, groupID string) error {
	group, err := s.store.GetGroupByID(ctx, groupID)
	if err != nil {
		return fmt.Errorf("lookup group: %w", err)
	}
	if group == nil {
		return fmt.Errorf("default group not found")
	}

	settings, err := s.GetSettings(ctx)
	if err != nil {
		settings = &SecuritySettings{}
	}
	settings.DefaultGroupID = groupID

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	if err := s.system.Set(ctx, settingsStorageKey(ctx), string(data)); err != nil {
		return err
	}
	s.cacheSettings(ctx, settings)
	s.invalidateGroupTreeCache()
	return nil
}

func settingsStorageKey(ctx context.Context) string {
	tenantID := tenantIDFromContext(ctx)
	if tenantID == store.DefaultTenantID {
		return settingsKey
	}
	return "tenant:" + tenantID + ":" + settingsKey
}

// --- Root Group Helper ---

// GetRootGroupID returns the ID of the root group.
func (s *SecurityService) GetRootGroupID(ctx context.Context) (string, error) {
	if err := s.ensureRootGroup(ctx); err != nil {
		return "", err
	}
	root, err := s.store.GetRootGroup(ctx)
	if err != nil {
		return "", fmt.Errorf("get root group: %w", err)
	}
	if root == nil {
		return "", fmt.Errorf("root group not found")
	}
	return root.ID, nil
}

func (s *SecurityService) ensureRootGroup(ctx context.Context) error {
	if s == nil || s.store == nil {
		return nil
	}
	if err := s.store.InitRootGroup(ctx); err != nil {
		return fmt.Errorf("init root group: %w", err)
	}
	return nil
}

// AssignNewUser assigns a newly enrolled user to the appropriate group based on
// org_structure_enabled, the user's selected group, and the default_group_id setting.
//
// Logic:
//  1. org_structure_enabled == false → Root_Group
//  2. org_structure_enabled == true && selectedGroupID != "" → selectedGroupID
//  3. org_structure_enabled == true && selectedGroupID == "" && default_group_id set → default_group_id
//  4. Otherwise → Root_Group
func (s *SecurityService) AssignNewUser(ctx context.Context, email, selectedGroupID string) error {
	settings, err := s.GetSettings(ctx)
	if err != nil {
		settings = &SecuritySettings{}
	}

	rootID, err := s.GetRootGroupID(ctx)
	if err != nil {
		return fmt.Errorf("get root group id: %w", err)
	}

	var targetGroupID string

	if !settings.OrgStructureEnabled {
		// Case 1: org_structure disabled → Root_Group
		targetGroupID = rootID
	} else if selectedGroupID != "" {
		// Case 2: org_structure enabled and user selected a group
		// Validate the selected group exists
		group, err := s.store.GetGroupByID(ctx, selectedGroupID)
		if err != nil || group == nil {
			// Selected group doesn't exist, fall back to default or root
			if settings.DefaultGroupID != "" {
				targetGroupID = settings.DefaultGroupID
			} else {
				targetGroupID = rootID
			}
		} else {
			targetGroupID = selectedGroupID
		}
	} else if settings.DefaultGroupID != "" {
		// Case 3: org_structure enabled, no selection, but default_group_id is set
		targetGroupID = settings.DefaultGroupID
	} else {
		// Case 4: fallback to Root_Group
		targetGroupID = rootID
	}

	return s.AssignUser(ctx, email, targetGroupID)
}

// --- Cache Invalidation ---

// InvalidateCache removes the cached effective policy for a specific user.
func (s *SecurityService) InvalidateCache(email string) {
	s.cache.Delete(securityPolicyCacheKey(context.Background(), email))
}

func securityPolicyCacheKey(ctx context.Context, email string) string {
	tenantID := tenantIDFromContext(ctx)
	return tenantID + "\x00" + strings.TrimSpace(strings.ToLower(email))
}

// InvalidateCacheForSubtree invalidates the cache for all members in the
// given group and all its descendant groups.
func (s *SecurityService) InvalidateCacheForSubtree(ctx context.Context, groupID string) {
	ctx = WithTenant(context.Background(), tenantIDFromContext(ctx))
	allGroups, err := s.store.ListGroups(ctx)
	if err != nil {
		return
	}

	// Collect group + all descendants
	groupIDs := append(collectDescendants(allGroups, groupID), groupID)

	// Delete cache for all members in these groups
	for _, gid := range groupIDs {
		members, err := s.store.ListGroupMembers(ctx, gid)
		if err != nil {
			continue
		}
		for _, email := range members {
			s.cache.Delete(securityPolicyCacheKey(ctx, email))
		}
	}
}

// --- SecurityPolicyProvider interface ---

// GetHeartbeatPolicy returns the heartbeat security payload for a user.
// If centralized security is disabled, returns {centralized_security: false}.
// If enabled, returns the user's effective policy with skill source restrictions
// merged from both the security group policy and the independent source control.
func (s *SecurityService) GetHeartbeatPolicy(ctx context.Context, userID string) (*HeartbeatSecurityPayload, error) {
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("get settings: %w", err)
	}
	policyUser := s.resolvePolicyUserIdentifier(ctx, userID)

	if !settings.CentralizedSecurityEnabled {
		// Even when centralized security is off, we still merge skill source
		// control if configured (it's an independent control plane).
		if s.skillSourcesProvider != nil {
			tenantID := s.resolveUserTenantID(ctx, userID)
			srcAllowed := s.skillSourcesProvider.ResolveForUser(ctx, userID, tenantID)
			if srcAllowed != nil {
				// Push skill source restriction even without centralized security.
				return &HeartbeatSecurityPayload{
					CentralizedSecurity:    false,
					SkillSourcesAllowed:    canonicalSourceList(srcAllowed),
					SkillSourcesRestricted: true,
				}, nil
			}
		}
		return &HeartbeatSecurityPayload{
			CentralizedSecurity: false,
		}, nil
	}

	policy, err := s.GetEffectivePolicy(ctx, policyUser)
	if err != nil {
		return nil, fmt.Errorf("get effective policy: %w", err)
	}

	// Merge independent skill source control into the policy.
	// The final SkillSourcesAllowed is the intersection of:
	//   - Security group policy's SkillSourcesAllowed
	//   - Independent source control's ResolveForUser result
	//
	// IMPORTANT: copy the policy before mutating to avoid polluting the
	// EffectivePolicy cache (sync.Map stores pointers).
	if s.skillSourcesProvider != nil {
		tenantID := s.resolveUserTenantID(ctx, userID)
		srcAllowed := s.skillSourcesProvider.ResolveForUser(ctx, userID, tenantID)
		groupRestricted := policy.SkillSourcesRestricted || len(policy.SkillSourcesAllowed) > 0
		if srcAllowed != nil || groupRestricted {
			policyCopy := *policy
			policyCopy.SkillSourcesAllowed, policyCopy.SkillSourcesRestricted = mergeSkillSourceRestrictions(policy.SkillSourcesAllowed, groupRestricted, srcAllowed)
			policy = &policyCopy
		}
	}

	return &HeartbeatSecurityPayload{
		CentralizedSecurity: true,
		Policy:              policy,
	}, nil
}

// IsCentralizedEnabled returns whether centralized security is enabled.
func (s *SecurityService) IsCentralizedEnabled(ctx context.Context) (bool, error) {
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return false, err
	}
	return settings.CentralizedSecurityEnabled, nil
}

// resolveUserTenantID looks up the user's security group ID (tenant).
// Returns empty string if the user has no group assignment.
func (s *SecurityService) resolveUserTenantID(ctx context.Context, email string) string {
	return tenantIDFromContext(ctx)
}

func (s *SecurityService) resolvePolicyUserIdentifier(ctx context.Context, userID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" || strings.Contains(userID, "@") || s.users == nil {
		return userID
	}
	user, err := s.users.GetByID(ctx, userID)
	if err != nil || user == nil || strings.TrimSpace(user.Email) == "" {
		return userID
	}
	return strings.TrimSpace(strings.ToLower(user.Email))
}

// intersectSources computes the intersection of two allowed-sources lists.
// nil/empty means "all allowed". The intersection of nil with X is X.
// NOTE: duplicated from hub/internal/skill.IntersectSources to avoid import cycle.
func intersectSources(a, b []string) []string {
	if len(a) == 0 {
		return canonicalSourceList(b)
	}
	if len(b) == 0 {
		return canonicalSourceList(a)
	}
	set := make(map[string]bool, len(b))
	for _, s := range b {
		set[canonicalSource(s)] = true
	}
	var result []string
	for _, s := range a {
		canon := canonicalSource(s)
		if set[canon] {
			result = append(result, canon)
		}
	}
	return result
}

func mergeSkillSourceRestrictions(groupAllowed []string, groupRestricted bool, sourceAllowed []string) ([]string, bool) {
	sourceRestricted := sourceAllowed != nil
	if !groupRestricted && !sourceRestricted {
		return nil, false
	}
	if groupRestricted && sourceRestricted {
		return intersectRestrictedSources(groupAllowed, sourceAllowed), true
	}
	if groupRestricted {
		return canonicalSourceList(groupAllowed), true
	}
	return canonicalSourceList(sourceAllowed), true
}

func intersectRestrictedSources(a, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return []string{}
	}
	return intersectSources(a, b)
}

func canonicalSourceList(sources []string) []string {
	if len(sources) == 0 {
		return sources
	}
	result := make([]string, 0, len(sources))
	seen := make(map[string]bool, len(sources))
	for _, source := range sources {
		canon := canonicalSource(source)
		if canon == "" || seen[canon] {
			continue
		}
		seen[canon] = true
		result = append(result, canon)
	}
	return result
}

func canonicalSource(source string) string {
	switch strings.TrimSpace(strings.ToLower(source)) {
	case "skillmarket", "market", "hubcenter", "hub_center", "skill_hub":
		return "skillhub"
	case "enterprise", "hub", "enterprisehub", "enterprise_hub":
		return "enterprise_hub"
	case "claw_hub":
		return "clawhub"
	case "git_hub":
		return "github"
	default:
		return strings.TrimSpace(strings.ToLower(source))
	}
}

// GetEffectivePolicyByUserID is an alias for GetEffectivePolicy,
// satisfying the SecurityPolicyProvider interface.
func (s *SecurityService) GetEffectivePolicyByUserID(ctx context.Context, userID string) (*EffectivePolicy, error) {
	return s.GetEffectivePolicy(ctx, s.resolvePolicyUserIdentifier(ctx, userID))
}

// --- Helpers ---

func (s *SecurityService) writeAuditLog(ctx context.Context, adminUserID, action string, payload map[string]interface{}) {
	if s.audit == nil {
		return
	}
	payloadJSON, _ := json.Marshal(payload)
	_ = s.audit.Create(ctx, &store.AdminAuditLog{
		ID:          uuid.New().String(),
		TenantID:    TenantIDFromContext(ctx),
		AdminUserID: adminUserID,
		Action:      action,
		PayloadJSON: string(payloadJSON),
		CreatedAt:   time.Now().UTC(),
	})
}
