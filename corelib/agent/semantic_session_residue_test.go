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
			EvidenceIDs: []string{"intent:baseline_workspace"},
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
	if len(got.Needs[0].EvidenceIDs) != 1 || got.Needs[0].EvidenceIDs[0] != "intent:baseline_workspace" {
		t.Fatalf("evidence=%#v", got.Needs[0].EvidenceIDs)
	}
	reloaded.ClearSemanticSessionResidue("desktop-user")
	if _, ok := reloaded.SemanticSessionResidue("desktop-user"); ok {
		t.Fatal("cleared residue still loaded")
	}
}

func TestParentExecutionToolsSurviveRestart(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	cm := NewPersistentConversationMemory(storePath)
	cm.SetParentExecutionTools("desktop-user:task", []string{"bash", "ssh", "bash"})
	cm.SetParentExecutionTools("desktop-user:cleared", []string{"ssh"})
	cm.ClearParentExecutionTools("desktop-user:cleared")
	cm.Stop()

	reloaded := NewPersistentConversationMemory(storePath)
	defer reloaded.Stop()
	names, known := reloaded.ParentExecutionTools("desktop-user:task")
	if !known || len(names) != 2 || names[0] != "bash" || names[1] != "ssh" {
		t.Fatalf("carry=%v known=%v", names, known)
	}
	cleared, known := reloaded.ParentExecutionTools("desktop-user:cleared")
	if !known || len(cleared) != 0 {
		t.Fatalf("cleared=%v known=%v", cleared, known)
	}
	if _, known := reloaded.ParentExecutionTools("desktop-user:never"); known {
		t.Fatal("an unrecorded session must stay unknown")
	}
}
