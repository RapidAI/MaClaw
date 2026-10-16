package permission

import (
	"github.com/RapidAI/CodeClaw/corelib/database"
)

// DatabaseWritePredicate returns the args predicate expressing the legacy
// "database write actions are denied" policy (light-prompt gate, read-only
// database_query surface, workflow phases) as a permission rule When clause.
//
// The action list comes from database.WriteActions — the single source of
// truth — so a rule built from this predicate can never drift from the
// legacy database.IsWriteAction gate it mirrors:
//
//	Rule{Tool: "database", Effect: EffectDeny, When: DatabaseWritePredicate()}
func DatabaseWritePredicate() *ArgsPredicate {
	// InFold (not In): database.IsWriteAction compares case-insensitively on
	// trimmed input, and the predicate must agree with that gate exactly.
	return &ArgsPredicate{Field: "action", InFold: database.WriteActions()}
}
