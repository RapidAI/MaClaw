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
	cache  sync.Map             // userEmail -> *EffectivePolicy

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
	now := time.Now()
	s.groupTreeMu.RLock()
	if s.groupTreeCached != nil && now.Before(s.groupTreeCachedUntil) {
		cached := cloneGroupTreeNode(s.groupTreeCached)
		s.groupTreeMu.RUnlock()
		return cached, nil
	}
	s.groupTreeMu.RUnlock()

	groups, err := s.store.ListGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	if len(groups) == 0 {
		s.groupTreeMu.Lock()
		s.groupTreeCached = nil
		s.groupTreeCachedUntil = now.Add(groupTreeCacheTTL)
		s.groupTreeMu.Unlock()
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

	cloned := cloneGroupTreeNode(root)
	s.groupTreeMu.Lock()
	s.groupTreeCached = cloneGroupTreeNode(root)
	s.groupTreeCachedUntil = time.Now().Add(groupTreeCacheTTL)
	s.groupTreeMu.Unlock()
	return cloned, nil
}

func (s *SecurityService) GetRootGroupNode(ctx context.Context) (*GroupTreeNode, error) {
	root, err := s.store.GetRootGroup(ctx)
	if err != nil {
		return nil, fmt.Errorf("get root group: %w", err)
	}
	if root == nil {
		return nil, nil
	}
	count, err := s.store.CountGroupMembers(ctx, root.ID)
	if err != nil {
		return nil, fmt.Errorf("count root members: %w", err)
	}
	if s.users != nil {
		count += s.countUnassignedUsers(ctx)
	}
	children, err := s.GetGroupChildren(ctx, root.ID)
	if err != nil {
		return nil, err
	}
	return &GroupTreeNode{
		ID:          root.ID,
		Name:        root.Name,
		ParentID:    root.ParentID,
		MemberCount: count,
		HasChildren: len(children) > 0,
		Children:    []*GroupTreeNode{},
	}, nil
}

func cloneGroupTreeNode(node *GroupTreeNode) *GroupTreeNode {
	if node == nil {
		return nil
	}
	cloned := &GroupTreeNode{
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

func (s *SecurityService) invalidateGroupTreeCache() {
	s.groupTreeMu.Lock()
	defer s.groupTreeMu.Unlock()
	s.groupTreeCached = nil
	s.groupTreeCachedUntil = time.Time{}
}

func (s *SecurityService) cacheSettings(settings *SecuritySettings) {
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
	s.invalidateGroupTreeCache()
	return nil
}

// ListGroupMembers returns the member emails for a given group.
func (s *SecurityService) ListGroupMembers(ctx context.Context, groupID string) ([]string, error) {
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
	allUsers, err := s.users.List(ctx)
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
		if u == nil || u.Status != "active" {
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
	var children []*GroupTreeNode
	for _, g := range allGroups {
		if g.ParentID == parentID {
			count, err := s.store.CountGroupMembers(ctx, g.ID)
			if err != nil {
				return nil, fmt.Errorf("count members for %s: %w", g.ID, err)
			}
			hasChildren := false
			for _, g2 := range allGroups {
				if g2.ParentID == g.ID {
					hasChildren = true
					break
				}
			}
			children = append(children, &GroupTreeNode{
				ID:          g.ID,
				Name:        g.Name,
				ParentID:    g.ParentID,
				MemberCount: count,
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
	if err := s.store.SetGroupPolicy(ctx, groupID, policy); err != nil {
		return fmt.Errorf("set group policy: %w", err)
	}
	s.InvalidateCacheForSubtree(groupID)
	return nil
}

// --- Effective Policy Computation ---

// GetEffectivePolicy computes the effective policy for a user by walking
// from Root_Group to the user's group, merging policies along the way.
func (s *SecurityService) GetEffectivePolicy(ctx context.Context, email string) (*EffectivePolicy, error) {
	// Check cache first
	if cached, ok := s.cache.Load(email); ok {
		return cached.(*EffectivePolicy), nil
	}

	// Get user's group (unassigned users default to root)
	groupID, err := s.store.GetUserGroup(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("get user group: %w", err)
	}
	if groupID == "" {
		root, err := s.store.GetRootGroup(ctx)
		if err != nil {
			return nil, fmt.Errorf("get root group: %w", err)
		}
		if root != nil {
			groupID = root.ID
		}
	}

	policy, err := s.computeEffectiveForGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}

	s.cache.Store(email, policy)
	return policy, nil
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
		case "yolo_mode_allowed":
			if b, ok := toBool(v); ok {
				p.YoloModeAllowed = b
			}
		case "smart_route_enabled":
			if b, ok := toBool(v); ok {
				p.SmartRouteEnabled = b
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
	return map[string]interface{}{
		"file_outbound_enabled":  p.FileOutboundEnabled,
		"image_outbound_enabled": p.ImageOutboundEnabled,
		"gossip_enabled":         p.GossipEnabled,
		"guardrail_mode":         p.GuardrailMode,
		"sandbox_mode":           p.SandboxMode,
		"network_level":          p.NetworkLevel,
		"yolo_mode_allowed":      p.YoloModeAllowed,
		"smart_route_enabled":    p.SmartRouteEnabled,
	}
}

// --- System Settings ---

// GetSettings reads the security settings from the system settings store.
func (s *SecurityService) GetSettings(ctx context.Context) (*SecuritySettings, error) {
	now := time.Now()
	s.settingsMu.RLock()
	if s.settingsCached != nil && now.Before(s.settingsCachedUntil) {
		cached := *s.settingsCached
		s.settingsMu.RUnlock()
		return &cached, nil
	}
	s.settingsMu.RUnlock()

	raw, err := s.system.Get(ctx, settingsKey)
	if err != nil || raw == "" {
		settings := &SecuritySettings{}
		s.cacheSettings(settings)
		return settings, nil
	}
	var settings SecuritySettings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return nil, fmt.Errorf("unmarshal security settings: %w", err)
	}
	settingsCopy := settings
	s.cacheSettings(&settingsCopy)
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
	if err := s.system.Set(ctx, settingsKey, string(data)); err != nil {
		return fmt.Errorf("save security settings: %w", err)
	}
	s.cacheSettings(settings)

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
	if err := s.system.Set(ctx, settingsKey, string(data)); err != nil {
		return err
	}
	s.cacheSettings(settings)
	s.invalidateGroupTreeCache()
	return nil
}

// --- Root Group Helper ---

// GetRootGroupID returns the ID of the root group.
func (s *SecurityService) GetRootGroupID(ctx context.Context) (string, error) {
	root, err := s.store.GetRootGroup(ctx)
	if err != nil {
		return "", fmt.Errorf("get root group: %w", err)
	}
	if root == nil {
		return "", fmt.Errorf("root group not found")
	}
	return root.ID, nil
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
	s.cache.Delete(email)
}

// InvalidateCacheForSubtree invalidates the cache for all members in the
// given group and all its descendant groups.
func (s *SecurityService) InvalidateCacheForSubtree(groupID string) {
	ctx := context.Background()
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
			s.cache.Delete(email)
		}
	}
}

// --- SecurityPolicyProvider interface ---

// GetHeartbeatPolicy returns the heartbeat security payload for a user.
// If centralized security is disabled, returns {centralized_security: false}.
// If enabled, returns the user's effective policy.
func (s *SecurityService) GetHeartbeatPolicy(ctx context.Context, userID string) (*HeartbeatSecurityPayload, error) {
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("get settings: %w", err)
	}

	if !settings.CentralizedSecurityEnabled {
		return &HeartbeatSecurityPayload{
			CentralizedSecurity: false,
		}, nil
	}

	policy, err := s.GetEffectivePolicy(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get effective policy: %w", err)
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

// GetEffectivePolicyByUserID is an alias for GetEffectivePolicy,
// satisfying the SecurityPolicyProvider interface.
func (s *SecurityService) GetEffectivePolicyByUserID(ctx context.Context, userID string) (*EffectivePolicy, error) {
	return s.GetEffectivePolicy(ctx, userID)
}

// --- Helpers ---

func (s *SecurityService) writeAuditLog(ctx context.Context, adminUserID, action string, payload map[string]interface{}) {
	if s.audit == nil {
		return
	}
	payloadJSON, _ := json.Marshal(payload)
	_ = s.audit.Create(ctx, &store.AdminAuditLog{
		ID:          uuid.New().String(),
		AdminUserID: adminUserID,
		Action:      action,
		PayloadJSON: string(payloadJSON),
		CreatedAt:   time.Now().UTC(),
	})
}
