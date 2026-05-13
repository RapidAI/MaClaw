package skill

import (
	"context"
	"reflect"
	"sync"
	"testing"

	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// mockSystemSettings is a simple in-memory implementation for testing.
type mockSystemSettings struct {
	mu   sync.Mutex
	data map[string]string
}

func newMockSystem() *mockSystemSettings {
	return &mockSystemSettings{data: make(map[string]string)}
}

func (m *mockSystemSettings) Set(_ context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if value == "" {
		delete(m.data, key)
	} else {
		m.data[key] = value
	}
	return nil
}

func (m *mockSystemSettings) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data[key], nil
}

func TestSourceControl_GlobalSetGet(t *testing.T) {
	svc := NewSourceControlService(newMockSystem())
	ctx := context.Background()

	// Initially nil.
	cfg, err := svc.GetGlobal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Fatalf("expected nil, got %+v", cfg)
	}

	// Set global.
	err = svc.SetGlobal(ctx, &SourceControlConfig{
		Enabled:        true,
		AllowedSources: []string{"skillhub", "clawhub"},
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg, err = svc.GetGlobal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled {
		t.Fatal("expected enabled")
	}
	if !reflect.DeepEqual(cfg.AllowedSources, []string{"skillhub", "clawhub"}) {
		t.Fatalf("unexpected sources: %v", cfg.AllowedSources)
	}
}

func TestSourceControl_TenantSetGetDelete(t *testing.T) {
	svc := NewSourceControlService(newMockSystem())
	ctx := context.Background()

	err := svc.SetTenant(ctx, "tenant-1", &SourceControlConfig{
		Enabled:        true,
		AllowedSources: []string{"skillhub"},
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg, _ := svc.GetTenant(ctx, "tenant-1")
	if !cfg.Enabled || len(cfg.AllowedSources) != 1 || cfg.AllowedSources[0] != "skillhub" {
		t.Fatalf("unexpected: %+v", cfg)
	}

	// Delete.
	if err := svc.DeleteTenant(ctx, "tenant-1"); err != nil {
		t.Fatal(err)
	}
	cfg, _ = svc.GetTenant(ctx, "tenant-1")
	if cfg != nil {
		t.Fatalf("expected nil after delete, got %+v", cfg)
	}
}

func TestSourceControl_UserSetGetDelete(t *testing.T) {
	svc := NewSourceControlService(newMockSystem())
	ctx := context.Background()

	err := svc.SetUser(ctx, "user@test.com", &SourceControlConfig{
		Enabled:        true,
		AllowedSources: []string{"github"},
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg, _ := svc.GetUser(ctx, "user@test.com")
	if !cfg.Enabled || cfg.AllowedSources[0] != "github" {
		t.Fatalf("unexpected: %+v", cfg)
	}

	if err := svc.DeleteUser(ctx, "user@test.com"); err != nil {
		t.Fatal(err)
	}
	cfg, _ = svc.GetUser(ctx, "user@test.com")
	if cfg != nil {
		t.Fatalf("expected nil after delete")
	}
}

func TestSourceControl_ResolveForUser_Priority(t *testing.T) {
	svc := NewSourceControlService(newMockSystem())
	ctx := context.Background()

	// Default: all sources.
	resolved := svc.ResolveForUser(ctx, "user@test.com", "tenant-1")
	if resolved != nil {
		t.Fatalf("expected nil (all allowed), got %v", resolved)
	}

	// Set global -> only skillhub.
	svc.SetGlobal(ctx, &SourceControlConfig{Enabled: true, AllowedSources: []string{"skillhub"}})
	resolved = svc.ResolveForUser(ctx, "user@test.com", "tenant-1")
	if !reflect.DeepEqual(resolved, []string{"skillhub"}) {
		t.Fatalf("expected [skillhub], got %v", resolved)
	}

	// Set tenant -> skillhub + clawhub (overrides global).
	svc.SetTenant(ctx, "tenant-1", &SourceControlConfig{Enabled: true, AllowedSources: []string{"skillhub", "clawhub"}})
	resolved = svc.ResolveForUser(ctx, "user@test.com", "tenant-1")
	if !reflect.DeepEqual(resolved, []string{"skillhub", "clawhub"}) {
		t.Fatalf("expected [skillhub, clawhub], got %v", resolved)
	}

	// Set user -> only github (overrides tenant).
	svc.SetUser(ctx, "user@test.com", &SourceControlConfig{Enabled: true, AllowedSources: []string{"github"}})
	resolved = svc.ResolveForUser(ctx, "user@test.com", "tenant-1")
	if !reflect.DeepEqual(resolved, []string{"github"}) {
		t.Fatalf("expected [github], got %v", resolved)
	}

	// Disable user config -> falls back to tenant.
	svc.SetUser(ctx, "user@test.com", &SourceControlConfig{Enabled: false, AllowedSources: []string{"github"}})
	resolved = svc.ResolveForUser(ctx, "user@test.com", "tenant-1")
	if !reflect.DeepEqual(resolved, []string{"skillhub", "clawhub"}) {
		t.Fatalf("expected tenant fallback [skillhub, clawhub], got %v", resolved)
	}
}

func TestSourceControl_ResolveForUser_NoTenant(t *testing.T) {
	svc := NewSourceControlService(newMockSystem())
	ctx := context.Background()

	svc.SetGlobal(ctx, &SourceControlConfig{Enabled: true, AllowedSources: []string{"clawhub"}})

	// No tenant -> skips tenant level, uses global.
	resolved := svc.ResolveForUser(ctx, "user@test.com", "")
	if !reflect.DeepEqual(resolved, []string{"clawhub"}) {
		t.Fatalf("expected [clawhub], got %v", resolved)
	}
}

func TestSourceControl_ValidateSources_Invalid(t *testing.T) {
	svc := NewSourceControlService(newMockSystem())
	ctx := context.Background()

	err := svc.SetGlobal(ctx, &SourceControlConfig{
		Enabled:        true,
		AllowedSources: []string{"skillhub", "invalid_source"},
	})
	if err == nil {
		t.Fatal("expected error for invalid source")
	}
}

func TestSourceControl_ValidateSources_Empty(t *testing.T) {
	svc := NewSourceControlService(newMockSystem())
	ctx := context.Background()

	// Empty allowed_sources is valid (means "block all" when enabled).
	err := svc.SetGlobal(ctx, &SourceControlConfig{
		Enabled:        true,
		AllowedSources: []string{},
	})
	if err != nil {
		t.Fatalf("empty sources should be valid: %v", err)
	}
}

func TestIntersectSources(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		want []string
	}{
		{"nil+nil", nil, nil, nil},
		{"nil+list", nil, []string{"skillhub"}, []string{"skillhub"}},
		{"list+nil", []string{"clawhub"}, nil, []string{"clawhub"}},
		{"overlap", []string{"skillhub", "clawhub"}, []string{"clawhub", "github"}, []string{"clawhub"}},
		{"no_overlap", []string{"skillhub"}, []string{"github"}, nil},
		{"identical", []string{"skillhub", "clawhub"}, []string{"skillhub", "clawhub"}, []string{"skillhub", "clawhub"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cskill.IntersectSources(tt.a, tt.b)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("IntersectSources(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestSourceControl_CacheHit(t *testing.T) {
	mock := newMockSystem()
	svc := NewSourceControlService(mock)
	ctx := context.Background()

	// Set and read global (populates cache).
	svc.SetGlobal(ctx, &SourceControlConfig{Enabled: true, AllowedSources: []string{"skillhub"}})
	cfg1, _ := svc.GetGlobal(ctx)

	// Directly modify the underlying store (bypass service).
	mock.Set(ctx, "skill_source_control_global", `{"enabled":true,"allowed_sources":["github"]}`)

	// Should still return cached value.
	cfg2, _ := svc.GetGlobal(ctx)
	if !reflect.DeepEqual(cfg1, cfg2) {
		t.Fatalf("expected cache hit, got different values: %+v vs %+v", cfg1, cfg2)
	}
}
