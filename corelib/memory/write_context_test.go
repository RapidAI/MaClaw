package memory

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestToolSaveBindsProjectBoundaryAndIsolatesRecall(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	projectA := `D:\work\project-a`
	projectB := `D:\work\project-b`
	content := "Project API endpoint is https://api.example.com and timeout is 30 seconds"

	out := HandleTool(store, map[string]interface{}{
		"action":   "save",
		"content":  content,
		"category": "project_knowledge",
		"tags":     []string{"api.example.com"},
	}, ToolOptions{
		OwnerID:     "desktop-user",
		ProjectPath: projectA,
	})
	if out == "" || len(store.List(CategoryProjectKnowledge, "")) != 1 {
		t.Fatalf("tool save failed: %q", out)
	}
	entry := store.List(CategoryProjectKnowledge, "")[0]
	if entry.OwnerID != "desktop-user" || entry.Scope != ScopeProject {
		t.Fatalf("unexpected write identity: %+v", entry)
	}
	if entry.Boundary == nil || entry.Boundary.ProjectPath != projectA || entry.Boundary.OwnerID != "desktop-user" {
		t.Fatalf("missing authoritative project boundary: %+v", entry.Boundary)
	}

	if got := store.RecallDynamicForTool("API endpoint", CategoryProjectKnowledge, projectA, "desktop-user"); len(got) != 1 {
		t.Fatalf("owning project recall = %+v, want one entry", got)
	}
	if got := store.RecallDynamicForTool("API endpoint", CategoryProjectKnowledge, projectB, "desktop-user"); len(got) != 0 {
		t.Fatalf("other project recalled bound memory: %+v", got)
	}
	if got := store.RecallDynamicStrict("API endpoint", CategoryProjectKnowledge, projectA); len(got) != 1 {
		t.Fatalf("strict owning-project recall = %+v, want one entry", got)
	}
	if got := store.RecallDynamicStrict("API endpoint", CategoryProjectKnowledge, projectB); len(got) != 0 {
		t.Fatalf("strict other-project recall returned bound memory: %+v", got)
	}
}

func TestWriteContextLeavesGlobalPreferenceUnbound(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	entry := Entry{Content: "User prefers concise Chinese answers for every explanation", Category: CategoryPreference}
	if err := store.SaveWithWriteContext(entry, WriteContext{OwnerID: "desktop-user", ProjectPath: `D:\work\project-a`}, ""); err != nil {
		t.Fatal(err)
	}
	got := store.List(CategoryPreference, "")[0]
	if got.Scope != ScopeGlobal || got.Boundary != nil {
		t.Fatalf("global preference received a project boundary: %+v", got)
	}
}

func TestOnlineExtractionUsesAuthoritativeProjectBoundary(t *testing.T) {
	store := newTestStore(t)
	defer store.Stop()
	llm := &mockLLMForExtraction{extractResponse: `[{"content":"Project API endpoint is https://api.example.com and timeout is 30 seconds","category":"project_knowledge","entities":["entity:api.example.com","relation:uses","entity:api"]}]`}
	extractor := NewOnlineExtractor(store, llm)
	extractor.SetCooldown(0)
	projectPath := `D:\work\project-a`
	result := extractor.ExtractAndIntegrate(context.Background(), []ConversationMessage{
		{Role: "user", Content: "The project API endpoint is https://api.example.com and timeout is 30 seconds."},
		{Role: "assistant", Content: "Noted."},
	}, "", time.Now(), "desktop-user", WriteContext{OwnerID: "desktop-user", ProjectPath: projectPath, Source: "test"})
	if result.Added != 1 {
		t.Fatalf("extraction result = %+v", result)
	}
	entries := store.List(CategoryProjectKnowledge, "api.example.com")
	if len(entries) != 1 || entries[0].Boundary == nil || entries[0].Boundary.ProjectPath != projectPath {
		t.Fatalf("online extraction lost project boundary: %+v", entries)
	}
}

func TestSameContentRemainsDistinctAcrossProjects(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	content := "Project database uses PostgreSQL 16 with the same shared configuration"
	for _, projectPath := range []string{`D:\work\project-a`, `D:\work\project-b`} {
		if err := store.SaveWithWriteContext(Entry{Content: content, Category: CategoryProjectKnowledge}, WriteContext{OwnerID: "desktop-user", ProjectPath: projectPath}, ""); err != nil {
			t.Fatal(err)
		}
	}
	entries := store.List(CategoryProjectKnowledge, "PostgreSQL")
	if len(entries) != 2 {
		t.Fatalf("same content across projects was merged: %+v", entries)
	}
	paths := map[string]bool{}
	for _, entry := range entries {
		if entry.Boundary != nil {
			paths[entry.Boundary.ProjectPath] = true
		}
	}
	if len(paths) != 2 {
		t.Fatalf("distinct project identities = %+v", paths)
	}
}

func TestVerifiedFactDoesNotSupersedeAnotherProject(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	projectA := `D:\work\project-a`
	projectB := `D:\work\project-b`
	for _, projectPath := range []string{projectA, projectB} {
		if err := store.SaveWithWriteContext(Entry{
			Content:  "API 地址是 https://old.example.com",
			Category: CategoryProjectKnowledge,
		}, WriteContext{OwnerID: "desktop-user", ProjectPath: projectPath}, ""); err != nil {
			t.Fatal(err)
		}
	}
	result := store.ApplyVerifiedFact(VerifiedFact{
		Claim:        "API 地址是 https://new.example.com",
		WriteContext: WriteContext{OwnerID: "desktop-user", ProjectPath: projectA},
	}, "desktop-user")
	if result.Superseded != 1 || !result.Saved {
		t.Fatalf("verified fact result = %+v", result)
	}
	for _, entry := range store.List(CategoryProjectKnowledge, "old.example.com") {
		if entry.Boundary != nil && entry.Boundary.ProjectPath == projectA && entry.Status != StatusSuperseded {
			t.Fatalf("owning project entry was not superseded: %+v", entry)
		}
		if entry.Boundary != nil && entry.Boundary.ProjectPath == projectB && entry.Status != StatusActive {
			t.Fatalf("other project entry was superseded: %+v", entry)
		}
	}
	for _, entry := range store.List(CategoryProjectKnowledge, "new.example.com") {
		if entry.Boundary == nil || entry.Boundary.ProjectPath != projectA {
			t.Fatalf("verified replacement lost project boundary: %+v", entry)
		}
	}
}
