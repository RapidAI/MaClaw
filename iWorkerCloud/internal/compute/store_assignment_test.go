package compute

import (
	"context"
	"testing"
)

func TestListEnabledProviders(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Create one enabled and one disabled provider.
	p1 := sampleProvider()
	p1.Name = "Enabled"
	p1.Enabled = true
	s.CreateProvider(ctx, p1)

	p2 := sampleProvider()
	p2.Name = "Disabled"
	p2.Enabled = true
	s.CreateProvider(ctx, p2)
	s.ToggleProvider(ctx, p2.ID) // now disabled

	list, err := s.ListEnabledProviders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 enabled provider, got %d", len(list))
	}
	if list[0].Name != "Enabled" {
		t.Errorf("expected 'Enabled', got %q", list[0].Name)
	}
}

func TestListEnabledProviders_Empty(t *testing.T) {
	s := newTestStore(t)
	list, err := s.ListEnabledProviders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 providers, got %d", len(list))
	}
}

func TestAssignAndListAssignments(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p1 := sampleProvider()
	p1.Name = "P1"
	s.CreateProvider(ctx, p1)

	p2 := sampleProvider()
	p2.Name = "P2"
	s.CreateProvider(ctx, p2)

	// No assignments initially.
	ids, err := s.ListAssignments(ctx, "center-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected 0 assignments, got %d", len(ids))
	}

	// Assign p1 to center-1.
	if err := s.AssignProvider(ctx, "center-1", p1.ID); err != nil {
		t.Fatal(err)
	}

	ids, err = s.ListAssignments(ctx, "center-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != p1.ID {
		t.Fatalf("expected [%s], got %v", p1.ID, ids)
	}

	// Assign p2 to center-1.
	if err := s.AssignProvider(ctx, "center-1", p2.ID); err != nil {
		t.Fatal(err)
	}

	ids, err = s.ListAssignments(ctx, "center-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 assignments, got %d", len(ids))
	}
}

func TestAssignProvider_Idempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p := sampleProvider()
	s.CreateProvider(ctx, p)

	// Assign twice — should not error (INSERT OR IGNORE).
	if err := s.AssignProvider(ctx, "center-1", p.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.AssignProvider(ctx, "center-1", p.ID); err != nil {
		t.Fatal(err)
	}

	ids, _ := s.ListAssignments(ctx, "center-1")
	if len(ids) != 1 {
		t.Fatalf("expected 1 assignment after duplicate insert, got %d", len(ids))
	}
}

func TestUnassignProvider(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p := sampleProvider()
	s.CreateProvider(ctx, p)
	s.AssignProvider(ctx, "center-1", p.ID)

	if err := s.UnassignProvider(ctx, "center-1", p.ID); err != nil {
		t.Fatal(err)
	}

	ids, _ := s.ListAssignments(ctx, "center-1")
	if len(ids) != 0 {
		t.Fatalf("expected 0 assignments after unassign, got %d", len(ids))
	}
}

func TestUnassignProvider_Nonexistent(t *testing.T) {
	s := newTestStore(t)
	// Unassigning a non-existent assignment should not error.
	if err := s.UnassignProvider(context.Background(), "center-1", "no-such-provider"); err != nil {
		t.Fatal(err)
	}
}

func TestListAssignedProviders_NoAssignments_FallsBackToAllEnabled(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p1 := sampleProvider()
	p1.Name = "Enabled1"
	s.CreateProvider(ctx, p1)

	p2 := sampleProvider()
	p2.Name = "Enabled2"
	s.CreateProvider(ctx, p2)

	p3 := sampleProvider()
	p3.Name = "Disabled"
	p3.Enabled = true
	s.CreateProvider(ctx, p3)
	s.ToggleProvider(ctx, p3.ID) // disabled

	// No assignments for center-1 → should return all enabled.
	list, err := s.ListAssignedProviders(ctx, "center-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 enabled providers (fallback), got %d", len(list))
	}
}

func TestListAssignedProviders_WithAssignments_OnlyAssignedEnabled(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p1 := sampleProvider()
	p1.Name = "Assigned-Enabled"
	s.CreateProvider(ctx, p1)

	p2 := sampleProvider()
	p2.Name = "Not-Assigned"
	s.CreateProvider(ctx, p2)

	p3 := sampleProvider()
	p3.Name = "Assigned-Disabled"
	p3.Enabled = true
	s.CreateProvider(ctx, p3)
	s.ToggleProvider(ctx, p3.ID) // disabled

	// Assign p1 and p3 to center-1.
	s.AssignProvider(ctx, "center-1", p1.ID)
	s.AssignProvider(ctx, "center-1", p3.ID)

	list, err := s.ListAssignedProviders(ctx, "center-1")
	if err != nil {
		t.Fatal(err)
	}
	// Only p1 should be returned (assigned AND enabled).
	if len(list) != 1 {
		t.Fatalf("expected 1 assigned+enabled provider, got %d", len(list))
	}
	if list[0].Name != "Assigned-Enabled" {
		t.Errorf("expected 'Assigned-Enabled', got %q", list[0].Name)
	}
}

func TestListAssignedProviders_IncludesFullAPIKey(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p := sampleProvider()
	p.APIKey = "sk-secret-key-for-center"
	s.CreateProvider(ctx, p)

	// No assignments → fallback returns all enabled with full key.
	list, err := s.ListAssignedProviders(ctx, "center-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(list))
	}
	if list[0].APIKey != "sk-secret-key-for-center" {
		t.Errorf("expected full api_key, got %q", list[0].APIKey)
	}
}
