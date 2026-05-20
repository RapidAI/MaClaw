package memory

import (
	"context"
	"testing"
)

func TestGeneratedWriteFacadesForHost(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	projectResult, err := store.UpsertProjectKnowledgeForHost(ProjectKnowledgeUpsertOptions{Title: "Host knowledge", Content: "host project knowledge", Tags: []string{"project-a"}})
	if err != nil || projectResult.EntryID == "" {
		t.Fatalf("UpsertProjectKnowledgeForHost = %+v, %v", projectResult, err)
	}
	artifactResult, err := store.UpsertTaskArtifactForHost(TaskArtifactUpsertOptions{Title: "Host artifact", Content: "host task artifact", Tags: []string{"artifact-a"}})
	if err != nil || artifactResult.EntryID == "" {
		t.Fatalf("UpsertTaskArtifactForHost = %+v, %v", artifactResult, err)
	}
	summaryResult, err := store.UpsertConversationSummaryForHost(ConversationSummaryUpsertOptions{Content: "host conversation summary", Tags: []string{"summary-a"}})
	if err != nil || summaryResult.EntryID == "" {
		t.Fatalf("UpsertConversationSummaryForHost = %+v, %v", summaryResult, err)
	}
	insightResult, err := store.UpsertGeneratedInsightForHost(GeneratedInsightUpsertOptions{Title: "Host insight", Content: "host generated insight", Tags: []string{"insight-a"}})
	if err != nil || insightResult.EntryID == "" {
		t.Fatalf("UpsertGeneratedInsightForHost = %+v, %v", insightResult, err)
	}
	if store.StorePathForHost() == "" {
		t.Fatalf("StorePathForHost should expose the backing store path")
	}
	_ = store.PendingDedupCountForHost()
	_ = store.ProcessPendingDedupForHost(context.Background())

	var nilStore *Store
	if got := nilStore.StorePathForHost(); got != "" {
		t.Fatalf("nil StorePathForHost = %q", got)
	}
	if got := nilStore.ProcessPendingDedupForHost(context.Background()); got != 0 {
		t.Fatalf("nil ProcessPendingDedupForHost = %d", got)
	}
}
