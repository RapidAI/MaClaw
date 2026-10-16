package guiapp

import (
	"errors"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestValidateSemanticToolScopeRejectsMissingSelectedDefinition(t *testing.T) {
	plan := tool.ToolPlan{
		ID:                "plan:test",
		CatalogDigest:     "catalog:test",
		CatalogGeneration: 1,
		Selections: []tool.PlannedSelection{{
			ID: "selection:write", AdapterName: "write_file",
		}},
	}
	scope, result := validateSemanticToolScope(plan, map[string]map[string]interface{}{}, "root", "session", "turn")
	if result.Valid || result.DependencyClosed {
		t.Fatalf("missing definition accepted: scope=%#v result=%#v", scope, result)
	}
	if len(result.MissingNames) != 1 || result.MissingNames[0] != "write_file" {
		t.Fatalf("missing diagnostics = %#v", result)
	}
	var typed semanticScopeSurfaceIncompleteError
	if err := semanticScopeSurfaceError(scope, result); !errors.As(err, &typed) {
		t.Fatalf("error type = %T (%v)", err, err)
	}
}

func TestValidateSemanticToolScopeIgnoresConfirmationDependency(t *testing.T) {
	plan := tool.ToolPlan{
		ID:            "plan:test-confirm",
		CatalogDigest: "catalog:test-confirm",
		Selections: []tool.PlannedSelection{{
			ID: "selection:write", AdapterName: "write_file", Requires: []string{"confirmation:write"}, RequiresConfirm: true,
		}},
	}
	defs := map[string]map[string]interface{}{
		"write_file": {"type": "function", "function": map[string]interface{}{"name": "write_file"}},
	}
	_, result := validateSemanticToolScope(plan, defs, "root", "session", "turn")
	if !result.Valid {
		t.Fatalf("confirmation dependency made scope invalid: %#v", result)
	}
}
