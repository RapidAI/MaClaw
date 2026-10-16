package agent

import (
	"path/filepath"
	"testing"
)

func TestProducedDocumentSurvivesRestart(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	cm := NewPersistentConversationMemory(storePath)
	cm.SetProducedDocument("desktop-user:weather", PersistedProducedDocument{
		CanonicalPath: filepath.Join("workspace", "崇州天气与风土人情.pdf"),
		Format:        "pdf",
		MIMEType:      "application/pdf",
		Size:          24,
		ModTimeNS:     42,
		Digest:        "0123456789abcdef0123456789abcdef",
	})
	cm.SetProducedDocument("desktop-user:weather", PersistedProducedDocument{
		CanonicalPath: "short",
		Digest:        "too-short",
	})
	cm.Stop()

	reloaded := NewPersistentConversationMemory(storePath)
	defer reloaded.Stop()
	got, ok := reloaded.ProducedDocument("desktop-user:weather")
	if !ok || got.Size != 24 || got.ModTimeNS != 42 || got.Digest != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("reloaded=%#v ok=%v", got, ok)
	}
	if filepath.Base(got.CanonicalPath) != "崇州天气与风土人情.pdf" {
		t.Fatalf("path=%q", got.CanonicalPath)
	}
	reloaded.ClearProducedDocument("desktop-user:weather")
	if _, ok := reloaded.ProducedDocument("desktop-user:weather"); ok {
		t.Fatal("cleared document still loaded")
	}
}
