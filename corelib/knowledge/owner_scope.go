package knowledge

import "strings"

// ownerScope is the owner predicate for one knowledge query.
// Lineage matches ID itself and IDs that continue with a colon, so a stable
// owner still sees rows stamped with a session-scoped owner. IncludeEmpty
// also matches a blank owner_id. Both stay off for an exact owner match.
type ownerScope struct {
	ID           string
	Lineage      bool
	IncludeEmpty bool
}

func ownerScopeFromSearch(opts SearchOptions) ownerScope {
	return ownerScope{
		ID:           opts.OwnerID,
		Lineage:      opts.OwnerLineage,
		IncludeEmpty: opts.IncludeEmptyOwner,
	}
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
