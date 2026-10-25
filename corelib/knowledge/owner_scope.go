package knowledge

import (
	"fmt"
	"strings"
)

// ownerScope is the owner predicate for one knowledge query.
// Lineage matches ID itself and IDs that continue with a colon, so a stable
// owner still sees rows stamped with a session-scoped owner. IncludeEmpty
// also matches a blank owner_id. Both stay off for an exact owner match.
// UseMembership reads knowledge_memberships and ignores IncludeEmpty.
type ownerScope struct {
	ID            string
	Lineage       bool
	IncludeEmpty  bool
	UseMembership bool
}

func ownerScopeFromSearch(opts SearchOptions) ownerScope {
	return ownerScope{
		ID:            opts.OwnerID,
		Lineage:       opts.OwnerLineage,
		IncludeEmpty:  opts.IncludeEmptyOwner,
		UseMembership: opts.UseMembership,
	}
}

func appendSourceOwnershipFilters(where []string, args []interface{}, sourceAlias string, scope ownerScope, searchScope, projectPath string) ([]string, []interface{}) {
	idExpr, ownerColumn := "knowledge_sources.id", "owner_id"
	if alias := strings.TrimSpace(sourceAlias); alias != "" {
		idExpr = alias + ".id"
		ownerColumn = alias + ".owner_id"
	}
	if scope.UseMembership && strings.TrimSpace(scope.ID) != "" {
		return appendMembershipOwnershipFilters(where, args, idExpr, ownerColumn, scope, searchScope, projectPath)
	}
	where, args = appendOwnerPredicate(where, args, ownerColumn, scope)
	switch strings.ToLower(strings.TrimSpace(searchScope)) {
	case SaveScopePersonal, SaveScopeLocalOnly, "local":
		where = append(where, "COALESCE("+strings.TrimSuffix(ownerColumn, "owner_id")+"project_path, '') = ''")
	case SaveScopeProject:
		if projectPath = strings.TrimSpace(projectPath); projectPath != "" {
			where = append(where, strings.TrimSuffix(ownerColumn, "owner_id")+"project_path = ?")
			args = append(args, projectPath)
		}
	default:
		if projectPath = strings.TrimSpace(projectPath); projectPath != "" {
			where = append(where, strings.TrimSuffix(ownerColumn, "owner_id")+"project_path = ?")
			args = append(args, projectPath)
		}
	}
	return where, args
}

func appendMembershipOwnershipFilters(where []string, args []interface{}, idExpr, ownerColumn string, scope ownerScope, searchScope, projectPath string) ([]string, []interface{}) {
	id := strings.TrimSpace(scope.ID)
	key := MembershipUserKey(id)
	parts := []string{
		fmt.Sprintf("EXISTS (SELECT 1 FROM knowledge_memberships um WHERE um.source_id = %s AND um.kind = ? AND um.scope_key = ?)", idExpr),
	}
	args = append(args, MembershipKindUser, key)
	if scope.Lineage {
		parts = append(parts, ownerColumn+" = ?", ownerColumn+" LIKE ? ESCAPE '\\'")
		args = append(args, id, escapeSQLiteLikePattern(id)+":%")
	} else {
		parts = append(parts, ownerColumn+" = ?")
		args = append(args, id)
	}
	if strings.EqualFold(strings.TrimSpace(searchScope), "all") {
		parts = append(parts, fmt.Sprintf("EXISTS (SELECT 1 FROM knowledge_memberships lm WHERE lm.source_id = %s AND lm.kind = ?)", idExpr))
		args = append(args, MembershipKindLibrary)
	}
	where = append(where, "("+strings.Join(parts, " OR ")+")")
	if strings.EqualFold(strings.TrimSpace(searchScope), SaveScopeProject) {
		if path := strings.TrimSpace(projectPath); path != "" {
			where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM knowledge_memberships pm WHERE pm.source_id = %s AND pm.kind = ? AND pm.scope_key = ?)", idExpr))
			args = append(args, MembershipKindProject, path)
		}
	}
	return where, args
}

// sourceOwnerMatchSQL matches the owner a later write should treat as the same
// source. A desktop owner shares one library with desktop-user, desktop-user:<task>,
// and a row whose owner is still empty. Any other owner, including empty, matches
// that value only.
func sourceOwnerMatchSQL(ownerID string) (string, []any) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == desktopMembershipUser || strings.HasPrefix(ownerID, desktopMembershipUser+":") {
		return "(COALESCE(owner_id, '') = ? OR COALESCE(owner_id, '') LIKE ? ESCAPE '\\' OR COALESCE(owner_id, '') = '')",
			[]any{desktopMembershipUser, escapeSQLiteLikePattern(desktopMembershipUser) + ":%"}
	}
	return "COALESCE(owner_id, '') = ?", []any{ownerID}
}

// sourceOwnerMatchOrder prefers the owner this write asked for, then any other
// stored owner, then a still-empty row.
const sourceOwnerMatchOrder = `CASE WHEN COALESCE(owner_id, '') = ? THEN 0 WHEN TRIM(COALESCE(owner_id, '')) = '' THEN 2 ELSE 1 END, updated_at DESC`

// contentHashOwnerFilter limits a content-hash lookup to the owner this import
// will store. An empty owner keeps the lookup unscoped.
func contentHashOwnerFilter(ownerID string) (string, []any) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return "", nil
	}
	return sourceOwnerMatchSQL(ownerID)
}

// projectPathMatchSQL matches a source this project already keeps. The
// project_path column is the row's home project. A project membership counts
// too, so a shared body is updated in place instead of stored again.
func projectPathMatchSQL(projectPath string) (string, []any) {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return "COALESCE(project_path, '') = ?", []any{""}
	}
	return "(COALESCE(project_path, '') = ? OR EXISTS (SELECT 1 FROM knowledge_memberships m WHERE m.source_id = knowledge_sources.id AND m.kind = ? AND m.scope_key = ?))",
		[]any{projectPath, MembershipKindProject, projectPath}
}

// contentHashProjectFilter limits an unscoped import to one project. A named
// owner already identifies the body, so another project of that owner is not a
// second copy.
func contentHashProjectFilter(ownerID, projectPath, saveScope string) (string, []any) {
	if strings.TrimSpace(ownerID) != "" {
		return "", nil
	}
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" || saveScope != SaveScopeProject {
		return "", nil
	}
	return "COALESCE(project_path, '') = ?", []any{projectPath}
}

func ownerMatchRank(stored, wanted string) int {
	stored = strings.TrimSpace(stored)
	wanted = strings.TrimSpace(wanted)
	if stored == wanted {
		return 0
	}
	if stored == "" {
		return 2
	}
	return 1
}

// hashOwnerRank treats desktop-user and desktop-user:<task> as one owner.
// A content-hash choice can then prefer the row already in this project
// before it prefers the exact owner string.
func hashOwnerRank(stored, wanted string) int {
	stored = strings.TrimSpace(stored)
	wanted = strings.TrimSpace(wanted)
	if wanted == "" {
		if stored == "" {
			return 0
		}
		return 1
	}
	if desktopLineageOwner(wanted) {
		if stored == "" {
			return 2
		}
		if desktopLineageOwner(stored) {
			return 0
		}
		return 1
	}
	if stored == wanted {
		return 0
	}
	if stored == "" {
		return 2
	}
	return 1
}

func desktopLineageOwner(owner string) bool {
	owner = strings.TrimSpace(owner)
	return owner == desktopMembershipUser || strings.HasPrefix(owner, desktopMembershipUser+":")
}

func appendOwnerPredicate(where []string, args []interface{}, column string, scope ownerScope) ([]string, []interface{}) {
	column = strings.TrimSpace(column)
	if column == "" {
		column = "owner_id"
	}
	id := strings.TrimSpace(scope.ID)
	if id == "" {
		if scope.IncludeEmpty {
			where = append(where, "COALESCE("+column+", '') = ''")
		}
		return where, args
	}
	if !scope.Lineage && !scope.IncludeEmpty {
		where = append(where, column+" = ?")
		args = append(args, id)
		return where, args
	}
	parts := []string{column + " = ?"}
	args = append(args, id)
	if scope.Lineage {
		parts = append(parts, column+" LIKE ? ESCAPE '\\'")
		args = append(args, escapeSQLiteLikePattern(id)+":%")
	}
	if scope.IncludeEmpty {
		parts = append(parts, "COALESCE("+column+", '') = ''")
	}
	where = append(where, "("+strings.Join(parts, " OR ")+")")
	return where, args
}
