package compute

import (
	"context"
	"crypto/rand"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func testEncKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, AES256KeyLen)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return key
}

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestStore(t *testing.T) *ProviderStore {
	t.Helper()
	db := newTestDB(t)
	s := NewProviderStore(db, testEncKey(t))
	if err := s.CreateTable(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func sampleProvider() *ComputeProvider {
	return &ComputeProvider{
		Name:                 "Test OpenAI",
		BaseURL:              "https://api.openai.com/v1",
		APIKey:               "sk-test-key-12345",
		Protocol:             ProtocolOpenAI,
		UserAgent:            "openclaw",
		ComputeType:          "general",
		Model:                "gpt-4",
		Enabled:              true,
		Priority:             10,
		Description:          "Test provider",
		InputPricePerMToken:  2.5,
		OutputPricePerMToken: 10.0,
	}
}

func TestCreateTable_Idempotent(t *testing.T) {
	s := newTestStore(t)
	// Calling CreateTable again should not error.
	if err := s.CreateTable(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCreateAndGetProvider(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p := sampleProvider()

	if err := s.CreateProvider(ctx, p); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	if p.ID == "" {
		t.Fatal("expected ID to be set")
	}

	got, err := s.GetProvider(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if got == nil {
		t.Fatal("expected provider, got nil")
	}
	if got.Name != p.Name {
		t.Errorf("Name: got %q, want %q", got.Name, p.Name)
	}
	if got.BaseURL != p.BaseURL {
		t.Errorf("BaseURL: got %q, want %q", got.BaseURL, p.BaseURL)
	}
	if got.APIKey != "sk-test-key-12345" {
		t.Errorf("APIKey: got %q, want %q", got.APIKey, "sk-test-key-12345")
	}
	if !got.HasAPIKey {
		t.Error("expected HasAPIKey to be true")
	}
	if got.Protocol != ProtocolOpenAI {
		t.Errorf("Protocol: got %q, want %q", got.Protocol, ProtocolOpenAI)
	}
	if got.UserAgent != "openclaw" {
		t.Errorf("UserAgent: got %q, want %q", got.UserAgent, "openclaw")
	}
	if got.ComputeType != "general" {
		t.Errorf("ComputeType: got %q, want %q", got.ComputeType, "general")
	}
	if got.Model != "gpt-4" {
		t.Errorf("Model: got %q, want %q", got.Model, "gpt-4")
	}
	if !got.Enabled {
		t.Error("expected Enabled to be true")
	}
	if got.Priority != 10 {
		t.Errorf("Priority: got %d, want 10", got.Priority)
	}
	if got.InputPricePerMToken != 2.5 {
		t.Errorf("InputPricePerMToken: got %f, want 2.5", got.InputPricePerMToken)
	}
	if got.OutputPricePerMToken != 10.0 {
		t.Errorf("OutputPricePerMToken: got %f, want 10.0", got.OutputPricePerMToken)
	}
}

func TestGetProvider_NotFound(t *testing.T) {
	s := newTestStore(t)
	got, err := s.GetProvider(context.Background(), "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatal("expected nil for nonexistent provider")
	}
}

func TestListProviders(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Empty list initially.
	list, err := s.ListProviders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 providers, got %d", len(list))
	}

	p1 := sampleProvider()
	p1.Name = "Provider A"
	p1.Priority = 5
	s.CreateProvider(ctx, p1)

	p2 := sampleProvider()
	p2.Name = "Provider B"
	p2.Priority = 20
	s.CreateProvider(ctx, p2)

	list, err = s.ListProviders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(list))
	}
	// Higher priority first.
	if list[0].Name != "Provider B" {
		t.Errorf("expected first provider to be 'Provider B', got %q", list[0].Name)
	}
}

func TestUpdateProvider_WithNewAPIKey(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p := sampleProvider()
	s.CreateProvider(ctx, p)

	p.Name = "Updated Name"
	p.APIKey = "sk-new-key-99999"
	p.Model = "gpt-4o"
	if err := s.UpdateProvider(ctx, p); err != nil {
		t.Fatalf("UpdateProvider: %v", err)
	}

	got, _ := s.GetProvider(ctx, p.ID)
	if got.Name != "Updated Name" {
		t.Errorf("Name: got %q, want %q", got.Name, "Updated Name")
	}
	if got.APIKey != "sk-new-key-99999" {
		t.Errorf("APIKey: got %q, want %q", got.APIKey, "sk-new-key-99999")
	}
	if got.Model != "gpt-4o" {
		t.Errorf("Model: got %q, want %q", got.Model, "gpt-4o")
	}
}

func TestUpdateProvider_PreserveAPIKey(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p := sampleProvider()
	s.CreateProvider(ctx, p)

	// Update without changing API key (empty APIKey field).
	p.Name = "New Name"
	p.APIKey = ""
	if err := s.UpdateProvider(ctx, p); err != nil {
		t.Fatalf("UpdateProvider: %v", err)
	}

	got, _ := s.GetProvider(ctx, p.ID)
	if got.Name != "New Name" {
		t.Errorf("Name: got %q, want %q", got.Name, "New Name")
	}
	// Original key should be preserved.
	if got.APIKey != "sk-test-key-12345" {
		t.Errorf("APIKey should be preserved, got %q", got.APIKey)
	}
}

func TestDeleteProvider(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p := sampleProvider()
	s.CreateProvider(ctx, p)

	if err := s.DeleteProvider(ctx, p.ID); err != nil {
		t.Fatalf("DeleteProvider: %v", err)
	}

	got, _ := s.GetProvider(ctx, p.ID)
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestDeleteProvider_CleansUpAssignments(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p := sampleProvider()
	s.CreateProvider(ctx, p)

	// Create an assignment.
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO center_provider_assignments (center_id, provider_id) VALUES (?, ?)`,
		"center-1", p.ID)
	if err != nil {
		t.Fatal(err)
	}

	s.DeleteProvider(ctx, p.ID)

	var count int
	s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM center_provider_assignments WHERE provider_id = ?`, p.ID).Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 assignments after delete, got %d", count)
	}
}

func TestDeleteProvider_SetsForceSyncForAffectedCenters(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p := sampleProvider()
	s.CreateProvider(ctx, p)

	if err := s.AssignProvider(ctx, "center-1", p.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.AssignProvider(ctx, "center-2", p.ID); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteProvider(ctx, p.ID); err != nil {
		t.Fatalf("DeleteProvider: %v", err)
	}

	for _, centerID := range []string{"center-1", "center-2"} {
		forceSync, err := s.GetForceSync(ctx, centerID)
		if err != nil {
			t.Fatalf("GetForceSync(%s): %v", centerID, err)
		}
		if !forceSync {
			t.Fatalf("expected force_sync for %s after provider deletion", centerID)
		}
	}
}

func TestToggleProvider(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p := sampleProvider()
	p.Enabled = true
	s.CreateProvider(ctx, p)

	// Toggle off.
	if err := s.ToggleProvider(ctx, p.ID); err != nil {
		t.Fatalf("ToggleProvider: %v", err)
	}
	got, _ := s.GetProvider(ctx, p.ID)
	if got.Enabled {
		t.Error("expected Enabled to be false after toggle")
	}

	// Toggle back on.
	if err := s.ToggleProvider(ctx, p.ID); err != nil {
		t.Fatalf("ToggleProvider: %v", err)
	}
	got, _ = s.GetProvider(ctx, p.ID)
	if !got.Enabled {
		t.Error("expected Enabled to be true after second toggle")
	}
}

func TestCreateProvider_EmptyAPIKey(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p := sampleProvider()
	p.APIKey = ""

	if err := s.CreateProvider(ctx, p); err != nil {
		t.Fatalf("CreateProvider with empty key: %v", err)
	}

	got, _ := s.GetProvider(ctx, p.ID)
	if got.APIKey != "" {
		t.Errorf("expected empty APIKey, got %q", got.APIKey)
	}
	if got.HasAPIKey {
		t.Error("expected HasAPIKey to be false for empty key")
	}
}
