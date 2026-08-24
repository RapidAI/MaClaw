package llmservice

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestAddProviderAssignsNextSequence(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{ID: "a", Name: "A", APIURL: "https://a.example/v1"}); err != nil {
		t.Fatalf("add a: %v", err)
	}
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{ID: "b", Name: "B", APIURL: "https://b.example/v1"}); err != nil {
		t.Fatalf("add b: %v", err)
	}
	a, err := svc.GetProvider(ctx, "a")
	if err != nil || a == nil || a.Sequence != 1 {
		t.Fatalf("provider a = %#v err=%v, want sequence 1", a, err)
	}
	b, err := svc.GetProvider(ctx, "b")
	if err != nil || b == nil || b.Sequence != 2 {
		t.Fatalf("provider b = %#v err=%v, want sequence 2", b, err)
	}
}

func TestSetProviderSequenceAndUpdatePreservesSequence(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{ID: "deepseek", Name: "DeepSeek", APIURL: "https://api.deepseek.com/v1"}); err != nil {
		t.Fatalf("add provider: %v", err)
	}
	if err := svc.SetProviderSequence(ctx, "deepseek", 3); err != nil {
		t.Fatalf("set sequence: %v", err)
	}
	got, err := svc.GetProvider(ctx, "deepseek")
	if err != nil || got == nil || got.Sequence != 3 {
		t.Fatalf("provider = %#v err=%v, want sequence 3", got, err)
	}
	if err := svc.UpdateProvider(ctx, llmpool.ProviderConfig{
		ID:       "deepseek",
		Name:     "DeepSeek Chat",
		APIURL:   "https://api.deepseek.com/v1",
		Sequence: 0,
	}); err != nil {
		t.Fatalf("update provider: %v", err)
	}
	got, err = svc.GetProvider(ctx, "deepseek")
	if err != nil || got == nil || got.Sequence != 3 || got.Name != "DeepSeek Chat" {
		t.Fatalf("updated provider = %#v err=%v, want preserved sequence 3", got, err)
	}
}

func TestSetProviderSequenceRejectsInvalid(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{ID: "deepseek", Name: "DeepSeek", APIURL: "https://api.deepseek.com/v1"}); err != nil {
		t.Fatalf("add provider: %v", err)
	}
	err := svc.SetProviderSequence(ctx, "deepseek", 0)
	if err == nil || !strings.Contains(err.Error(), "sequence") {
		t.Fatalf("error = %v, want sequence validation", err)
	}
	err = svc.SetProviderSequence(ctx, "missing", 1)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v, want provider not found", err)
	}
}

func TestNormalizeAssignsSequencesWhenUnset(t *testing.T) {
	reg := &Registry{Providers: []llmpool.ProviderConfig{
		{ID: "b", Name: "B"},
		{ID: "a", Name: "A"},
	}}
	normalizeProviderSequences(reg)
	if reg.Providers[0].Sequence != 1 || reg.Providers[1].Sequence != 2 {
		t.Fatalf("sequences = %d,%d, want 1,2", reg.Providers[0].Sequence, reg.Providers[1].Sequence)
	}
}

func TestNormalizeLeavesExistingSequences(t *testing.T) {
	reg := &Registry{Providers: []llmpool.ProviderConfig{
		{ID: "b", Name: "B", Sequence: 5},
		{ID: "a", Name: "A"},
	}}
	normalizeProviderSequences(reg)
	if reg.Providers[0].Sequence != 5 || reg.Providers[1].Sequence != 0 {
		t.Fatalf("sequences = %d,%d, want 5,0", reg.Providers[0].Sequence, reg.Providers[1].Sequence)
	}
}

func TestSetProviderSequencesSwapsAtomically(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{ID: "a", Name: "A", APIURL: "https://a.example/v1", Sequence: 1}); err != nil {
		t.Fatalf("add a: %v", err)
	}
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{ID: "b", Name: "B", APIURL: "https://b.example/v1", Sequence: 2}); err != nil {
		t.Fatalf("add b: %v", err)
	}
	if err := svc.SetProviderSequences(ctx, map[string]int{"a": 2, "b": 1}); err != nil {
		t.Fatalf("swap: %v", err)
	}
	a, err := svc.GetProvider(ctx, "a")
	if err != nil || a == nil || a.Sequence != 2 {
		t.Fatalf("a = %#v err=%v, want sequence 2", a, err)
	}
	b, err := svc.GetProvider(ctx, "b")
	if err != nil || b == nil || b.Sequence != 1 {
		t.Fatalf("b = %#v err=%v, want sequence 1", b, err)
	}
	err = svc.SetProviderSequences(ctx, map[string]int{"a": 3, "missing": 4})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v, want missing provider", err)
	}
	a, err = svc.GetProvider(ctx, "a")
	if err != nil || a == nil || a.Sequence != 2 {
		t.Fatalf("partial apply a = %#v err=%v, want unchanged sequence 2", a, err)
	}
	err = svc.SetProviderSequences(ctx, map[string]int{})
	if err == nil || !strings.Contains(err.Error(), "sequences") {
		t.Fatalf("error = %v, want sequences required", err)
	}
}
