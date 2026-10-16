package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func databaseSelection() tool.PlannedSelection {
	return tool.PlannedSelection{AdapterName: "database"}
}

func databaseQuerySelection() tool.PlannedSelection {
	return tool.PlannedSelection{AdapterName: "database_query"}
}

func TestDatabaseQuerySurfaceCannotWrite(t *testing.T) {
	for _, action := range managedDatabaseWriteActions {
		reason, refused := semanticManagedInvocationRefusal(databaseQuerySelection(), misRequest(`{"action":"`+action+`"}`))
		if !refused {
			t.Errorf("database_query admitted write %q", action)
			continue
		}
		if reason != "database_action_outside_read_surface" {
			t.Errorf("action %q refused as %q", action, reason)
		}
	}
	for _, action := range managedDatabaseReadActions {
		if _, refused := semanticManagedInvocationRefusal(databaseQuerySelection(), misRequest(`{"action":"`+action+`"}`)); refused {
			t.Errorf("database_query refused read %q", action)
		}
	}
}

func TestDatabaseWriteSurfaceStillServesReadsAndWrites(t *testing.T) {
	for _, action := range append(append([]string{}, managedDatabaseReadActions...), managedDatabaseWriteActions...) {
		if _, refused := semanticManagedInvocationRefusal(databaseSelection(), misRequest(`{"action":"`+action+`"}`)); refused {
			t.Errorf("database refused %q", action)
		}
	}
}

func TestDatabaseQueryRefusalNamesReadActions(t *testing.T) {
	text := semanticManagedDatabaseRefusalText("database_query", "database_action_outside_read_surface")
	if !strings.Contains(text, "query") || !strings.Contains(text, "inspect") {
		t.Fatalf("refusal text = %q", text)
	}
	if strings.Contains(text, "execute") && !strings.Contains(text, "need the database tool") {
		t.Fatalf("read-only refusal advertised execute: %q", text)
	}
}
