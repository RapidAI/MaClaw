package agent

import (
	"path/filepath"
	"testing"
)

func TestSemanticSessionResidueSurvivesRestart(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	cm := NewPersistentConversationMemory(storePath)
	cm.SetSemanticSessionResidue("desktop-user", SemanticSessionResidue{
		Generation:  2,
		Status:      "open",
		Summary:     "做一份项目周报",
		LookupFacts: true,
		Needs: []SemanticSessionResidueNeed{{
			ID: "need:office", Capability: "document.write.office", Required: true,
		}},
		Remaining: map[string]int{"document.write.office": 3},
	})
	cm.Stop()

	reloaded := NewPersistentConversationMemory(storePath)
	defer reloaded.Stop()
	got, ok := reloaded.SemanticSessionResidue("desktop-user")
	if !ok || got.Summary != "做一份项目周报" || !got.LookupFacts || got.Remaining["document.write.office"] != 3 {
		t.Fatalf("reloaded=%#v ok=%v", got, ok)
	}
	if len(got.Needs) != 1 || got.Needs[0].Capability != "document.write.office" || !got.Needs[0].Required {
		t.Fatalf("needs=%#v", got.Needs)
	}
	reloaded.ClearSemanticSessionResidue("desktop-user")
	if _, ok := reloaded.SemanticSessionResidue("desktop-user"); ok {
		t.Fatal("cleared residue still loaded")
	}
}
