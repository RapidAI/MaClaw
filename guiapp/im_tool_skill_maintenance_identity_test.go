package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestMaintenanceEntryByNameMatchesStableAlias(t *testing.T) {
	entries := []corelib.NLSkillEntry{{
		Name:       "Paper PDF Translator",
		HubSkillID: "paper_pdf_translator",
	}}
	entry := maintenanceEntryByName(entries, "paper_pdf_translator")
	if entry == nil || entry.Name != "Paper PDF Translator" {
		t.Fatalf("maintenanceEntryByName alias lookup = %#v, want display entry", entry)
	}
}

func TestMaintenanceEntryByNameEmptyQueryReturnsNil(t *testing.T) {
	entries := []corelib.NLSkillEntry{{Name: "skill"}}
	if got := maintenanceEntryByName(entries, " "); got != nil {
		t.Fatalf("empty maintenance identity resolved to %#v", got)
	}
}
