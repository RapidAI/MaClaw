package llmservice

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestImportProviderArraysUpsertKeepsKeyAndSharedBilling(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.SaveRegistry(ctx, &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "a", Name: "A", APIURL: "https://a.example/v1", APIKey: "secret-a", Protocol: "openai", Models: []string{"m1"}, ArrayID: "pool", CreditMultiplier: 2, TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 3, OutputCreditsPer10K: 6}},
			{ID: "b", Name: "B", APIURL: "https://b.example/v1", APIKey: "secret-b", Protocol: "openai", Models: []string{"m1"}, ArrayID: "pool", CreditMultiplier: 2, TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 3, OutputCreditsPer10K: 6}},
		},
		ProviderArrays: []llmpool.ProviderArray{{
			ID: "pool", Name: "Pool", MemberIDs: []string{"a", "b"}, CreditMultiplier: 2,
			TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 3, OutputCreditsPer10K: 6},
		}},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	updated, err := svc.ImportProviderArrays(ctx, []ProviderArrayImport{{
		ID: "pool", Name: "Pool",
		Providers: []llmpool.ProviderConfig{{
			ID: "a", Name: "A renamed", APIURL: "https://a.example/v1", Protocol: "openai", CreditMultiplier: 9,
		}},
	}})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(updated.Arrays) != 1 || len(updated.Arrays[0].Updated) != 1 || updated.Arrays[0].Updated[0] != "a" {
		t.Fatalf("update result = %+v", updated.Arrays)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := findProvider(reg, "a")
	if got == nil || got.APIKey != "secret-a" || len(got.Models) != 1 || got.Models[0] != "m1" || got.Name != "A renamed" {
		t.Fatalf("updated provider = %+v", got)
	}
	if got.CreditMultiplier != 2 || got.TokenPricing.InputCreditsPer10K != 3 {
		t.Fatalf("existing array billing changed: multiplier=%v price=%v", got.CreditMultiplier, got.TokenPricing.InputCreditsPer10K)
	}

	cleared, err := svc.ImportProviderArrays(ctx, []ProviderArrayImport{{
		ID: "pool",
		Providers: []llmpool.ProviderConfig{{
			ID: "a", Name: "A renamed", APIURL: "https://a.example/v1", Models: []string{},
		}},
	}})
	if err != nil {
		t.Fatalf("clear models: %v", err)
	}
	if len(cleared.Arrays[0].Updated) != 1 {
		t.Fatalf("clear result = %+v", cleared.Arrays)
	}
	reg, err = svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got = findProvider(reg, "a")
	if got == nil || got.Models != nil && len(got.Models) != 0 {
		t.Fatalf("models = %#v", got)
	}

	created, err := svc.ImportProviderArrays(ctx, []ProviderArrayImport{{
		ID: "fresh", Name: "Fresh",
		Providers: []llmpool.ProviderConfig{
			{ID: "c", Name: "C", APIURL: "https://c.example/v1", APIKey: "secret-c", CreditMultiplier: 4, TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 2}},
			{ID: "d", Name: "D", APIURL: "https://d.example/v1", APIKey: "secret-d", CreditMultiplier: 8, TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 9, OutputCreditsPer10K: 9}},
		},
	}})
	if err != nil {
		t.Fatalf("create array: %v", err)
	}
	if len(created.Arrays[0].Created) != 2 {
		t.Fatalf("created = %+v", created.Arrays[0])
	}
	reg, err = svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"c", "d"} {
		member := findProvider(reg, id)
		if member == nil || member.CreditMultiplier != 4 || member.TokenPricing.InputCreditsPer10K != 1 || member.TokenPricing.OutputCreditsPer10K != 2 {
			t.Fatalf("member %s billing = %+v", id, member)
		}
		if member.ArrayID != "fresh" {
			t.Fatalf("member %s array = %s", id, member.ArrayID)
		}
	}
}

func TestImportProviderArraysRejectsForeignIDAndBadPrice(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.SaveRegistry(ctx, &Registry{
		Providers: []llmpool.ProviderConfig{
			{ID: "alpha", Name: "Alpha", APIURL: "https://alpha.example/v1", APIKey: "k", ArrayID: "pool-x"},
			{ID: "keep", Name: "Keep", APIURL: "https://keep.example/v1", APIKey: "k", ArrayID: "pool-x"},
		},
		ProviderArrays: []llmpool.ProviderArray{{ID: "pool-x", Name: "Pool X", MemberIDs: []string{"alpha", "keep"}}},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err := svc.ImportProviderArrays(ctx, []ProviderArrayImport{{
		ID:        "alpha",
		Providers: []llmpool.ProviderConfig{{ID: "beta", Name: "Beta", APIURL: "https://beta.example/v1", APIKey: "k"}},
	}})
	if err == nil || !strings.Contains(err.Error(), "include that provider") {
		t.Fatalf("collision error = %v", err)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if findProvider(reg, "beta") != nil || len(reg.Providers) != 2 {
		t.Fatalf("rejected import wrote providers: %+v", reg.Providers)
	}

	_, err = svc.ImportProviderArrays(ctx, []ProviderArrayImport{{
		ID:           "priced",
		TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: -1, OutputCreditsPer10K: 1},
		Providers:    []llmpool.ProviderConfig{{ID: "e", Name: "E", APIURL: "https://e.example/v1", APIKey: "k"}},
	}})
	if err == nil || !strings.Contains(err.Error(), "non-negative") {
		t.Fatalf("price error = %v", err)
	}
	_, err = svc.ImportProviderArrays(ctx, []ProviderArrayImport{{
		ID:        "bad-proto",
		Providers: []llmpool.ProviderConfig{{ID: "e", Name: "E", APIURL: "https://e.example/v1", APIKey: "k", Protocol: "grpc"}},
	}})
	if err == nil || !strings.Contains(err.Error(), "protocol") {
		t.Fatalf("protocol error = %v", err)
	}
}

func TestImportProviderArraysLeavesPauseToPatch(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.SaveRegistry(ctx, &Registry{
		Providers: []llmpool.ProviderConfig{{
			ID: "a", Name: "A", APIURL: "https://a.example/v1", APIKey: "secret-a", ArrayID: "pool", Paused: true,
		}},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.ImportProviderArrays(ctx, []ProviderArrayImport{{
		ID: "pool",
		Providers: []llmpool.ProviderConfig{{
			ID: "a", Name: "A", APIURL: "https://a.example/v1", Paused: false,
		}},
	}}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := svc.ImportProviderArrays(ctx, []ProviderArrayImport{{
		ID: "pool",
		Providers: []llmpool.ProviderConfig{{
			ID: "b", Name: "B", APIURL: "https://b.example/v1", APIKey: "secret-b", Paused: true,
		}},
	}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatal(err)
	}
	kept := findProvider(reg, "a")
	created := findProvider(reg, "b")
	if kept == nil || !kept.Paused || created == nil || created.Paused {
		t.Fatalf("pause kept=%v created=%v", kept, created)
	}
}
