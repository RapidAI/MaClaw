package llmservice

import (
	"context"
	"errors"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestCopyProviderClonesConfigIntoSameArray(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})
	cacheRead := 0.1
	source := llmpool.ProviderConfig{
		ID:                    "OpenCode-1",
		Name:                  "OpenCode-1",
		APIURL:                "https://opencode.ai/zen/go/v1",
		APIKey:                "sk-secret",
		Protocol:              "openai",
		Paused:                true,
		Sequence:              3,
		Models:                []string{"longcat-2.5-preview-free"},
		CapabilityTags:        []string{"tools", "vision"},
		AllowedNodeIDs:        []string{"HC-2"},
		AuthKind:              llmpool.ProviderAuthWorkBuddy,
		WorkBuddyEdition:      "global",
		WorkBuddyRefreshToken: "refresh-token",
		ServeWindows:          []llmpool.TokenBankShareWindow{{Days: []int{1, 2, 3, 4, 5}, Start: "22:00", End: "24:00"}},
		ModelMap:              map[string]string{"alias": "upstream-model"},
		TokenBankExtraKeys:    []string{"sk-extra"},
		TokenPricing: llmpool.TokenPricing{
			InputCreditsPer10K:     1,
			OutputCreditsPer10K:    4,
			CacheReadCreditsPer10K: &cacheRead,
		},
		MaxConcurrency:     4,
		UpstreamTimeoutSec: 90,
		DispatchWeight:     2,
	}
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{ID: "pool", Name: "OpenCode", APIURL: "https://opencode.ai/zen/go/v1"}); err != nil {
		t.Fatalf("add array anchor: %v", err)
	}
	source.ArrayID = "pool"
	if err := svc.AddProvider(ctx, source); err != nil {
		t.Fatalf("add source: %v", err)
	}
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID: "tail", Name: "tail", APIURL: "https://tail.example/v1", Sequence: 10,
	}); err != nil {
		t.Fatalf("add tail: %v", err)
	}

	copied, err := svc.CopyProvider(ctx, "OpenCode-1")
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if copied.ID != "OpenCode-1-copy" || copied.Name != "OpenCode-1（副本）" {
		t.Fatalf("identity = %s %q", copied.ID, copied.Name)
	}
	if copied.ArrayID != "pool" {
		t.Fatalf("array = %q, want pool", copied.ArrayID)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	arr := findProviderArray(reg, "pool")
	if arr == nil {
		t.Fatal("pool array missing")
	}
	sourceAt, copyAt := -1, -1
	for i, id := range arr.MemberIDs {
		if id == "OpenCode-1" {
			sourceAt = i
		}
		if id == copied.ID {
			copyAt = i
		}
	}
	if sourceAt < 0 || copyAt != sourceAt+1 {
		t.Fatalf("members = %v, want copy immediately after OpenCode-1", arr.MemberIDs)
	}
	if copied.APIKey != "sk-secret" || copied.APIURL != source.APIURL || copied.Protocol != "openai" || !copied.Paused {
		t.Fatalf("upstream config = %#v", copied)
	}
	if copied.WorkBuddyRefreshToken != "refresh-token" || copied.WorkBuddyEdition != "global" || copied.AuthKind != llmpool.ProviderAuthWorkBuddy {
		t.Fatalf("vendor auth = kind %q edition %q token %q", copied.AuthKind, copied.WorkBuddyEdition, copied.WorkBuddyRefreshToken)
	}
	if len(copied.Models) != 1 || copied.Models[0] != "longcat-2.5-preview-free" {
		t.Fatalf("models = %#v", copied.Models)
	}
	if len(copied.AllowedNodeIDs) != 1 || copied.AllowedNodeIDs[0] != "HC-2" {
		t.Fatalf("nodes = %#v", copied.AllowedNodeIDs)
	}
	if len(copied.ServeWindows) != 1 || copied.ServeWindows[0].Start != "22:00" || copied.ServeWindows[0].End != "24:00" {
		t.Fatalf("serve windows = %#v", copied.ServeWindows)
	}
	if copied.ModelMap["alias"] != "upstream-model" || len(copied.TokenBankExtraKeys) != 1 || copied.TokenBankExtraKeys[0] != "sk-extra" {
		t.Fatalf("map/keys = %#v %#v", copied.ModelMap, copied.TokenBankExtraKeys)
	}
	if copied.TokenPricing.CacheReadCreditsPer10K == nil || *copied.TokenPricing.CacheReadCreditsPer10K != 0.1 {
		t.Fatalf("pricing = %#v", copied.TokenPricing)
	}
	if copied.Sequence != source.Sequence+1 {
		t.Fatalf("copy sequence = %d, want %d", copied.Sequence, source.Sequence+1)
	}
	tail, err := svc.GetProvider(ctx, "tail")
	if err != nil || tail == nil || tail.Sequence != 11 {
		t.Fatalf("tail sequence = %#v err=%v, want 11", tail, err)
	}

	copied.Models[0] = "mutated"
	copied.TokenBankExtraKeys[0] = "mutated"
	*copied.TokenPricing.CacheReadCreditsPer10K = 99
	stored, err := svc.GetProvider(ctx, "OpenCode-1")
	if err != nil || stored == nil {
		t.Fatalf("source after copy: %#v err=%v", stored, err)
	}
	if stored.APIKey != "sk-secret" || stored.Models[0] != "longcat-2.5-preview-free" || stored.TokenBankExtraKeys[0] != "sk-extra" {
		t.Fatalf("source changed: %#v", stored)
	}
	if stored.TokenPricing.CacheReadCreditsPer10K == nil || *stored.TokenPricing.CacheReadCreditsPer10K != 0.1 {
		t.Fatalf("source price aliased: %#v", stored.TokenPricing)
	}

	again, err := svc.CopyProvider(ctx, "OpenCode-1")
	if err != nil {
		t.Fatalf("second copy: %v", err)
	}
	if again.ID != "OpenCode-1-copy-2" || again.Name != "OpenCode-1（副本-2）" || again.ArrayID != "pool" || again.APIKey != "sk-secret" {
		t.Fatalf("second copy = id %s name %q array %q key %q", again.ID, again.Name, again.ArrayID, again.APIKey)
	}

	fromCopy, err := svc.CopyProvider(ctx, again.ID)
	if err != nil {
		t.Fatalf("copy of copy: %v", err)
	}
	if fromCopy.Name != "OpenCode-1（副本-3）" || fromCopy.ID != "OpenCode-1-copy-3" {
		t.Fatalf("copy of copy = id %s name %q", fromCopy.ID, fromCopy.Name)
	}
}

func TestCopyProviderMissing(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	_, err := svc.CopyProvider(context.Background(), "missing")
	if !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestCopyProviderSkipsArrayIDAndFullwidthCopyName(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID: "OpenCode-1", Name: "OpenCode-1（副本－2）", APIURL: "https://opencode.ai/zen/go/v1", APIKey: "sk-secret",
	}); err != nil {
		t.Fatalf("add source: %v", err)
	}
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID: "taken-copy", Name: "OpenCode-1（副本）", APIURL: "https://opencode.ai/zen/go/v1",
	}); err != nil {
		t.Fatalf("add taken name: %v", err)
	}
	if err := svc.AddProviderArray(ctx, "OpenCode-1-copy", "Reserved", ProviderArrayBilling{CreditMultiplier: 1}); err != nil {
		t.Fatalf("add reserved array: %v", err)
	}
	copied, err := svc.CopyProvider(ctx, "OpenCode-1")
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if copied.ID != "OpenCode-1-copy-2" {
		t.Fatalf("id = %s, want OpenCode-1-copy-2", copied.ID)
	}
	if copied.Name != "OpenCode-1（副本-3）" {
		t.Fatalf("name = %q, want fullwidth （副本－2） to occupy slot 2", copied.Name)
	}
	if copied.ArrayID != "OpenCode-1" || copied.APIKey != "sk-secret" {
		t.Fatalf("copy left its array or key: %#v", copied)
	}
}

func TestCopyProviderRejectsTokenBank(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("arrays: %v", err)
	}
	memberID := TokenBankMemberID("share-1", "gpt-4o")
	if memberID == "" || !IsTokenBankMemberID(memberID) {
		t.Fatalf("member id = %q", memberID)
	}
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID: memberID, Name: "gpt-4o", APIURL: "https://api.example/v1", APIKey: "sk-bank", ArrayID: TokenBankArrayMid,
	}); err != nil {
		t.Fatalf("add bank member: %v", err)
	}
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID: "plain-in-bank", Name: "plain", APIURL: "https://api.example/v1", APIKey: "sk-plain", ArrayID: TokenBankArrayMid,
	}); err != nil {
		t.Fatalf("add plain member: %v", err)
	}
	before, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	count := len(before.Providers)
	for _, id := range []string{memberID, "plain-in-bank"} {
		_, err := svc.CopyProvider(ctx, id)
		if !errors.Is(err, ErrProviderCopyProtected) {
			t.Fatalf("copy %s err = %v", id, err)
		}
	}
	after, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(after.Providers) != count {
		t.Fatalf("providers = %d, want %d", len(after.Providers), count)
	}
}

func TestCopyProviderKeepsKeyWhenCacheLostIt(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID: "OpenCode-1", Name: "OpenCode-1", APIURL: "https://opencode.ai/zen/go/v1", APIKey: "sk-secret",
	}); err != nil {
		t.Fatalf("add source: %v", err)
	}
	svc.mu.Lock()
	for i := range svc.cached.Providers {
		if svc.cached.Providers[i].ID == "OpenCode-1" {
			svc.cached.Providers[i].APIKey = ""
		}
	}
	svc.mu.Unlock()

	copied, err := svc.CopyProvider(ctx, "OpenCode-1")
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if copied.APIKey != "sk-secret" {
		t.Fatalf("copy key = %q, want the stored key", copied.APIKey)
	}
	stored, err := svc.GetProvider(ctx, "OpenCode-1")
	if err != nil || stored == nil || stored.APIKey != "sk-secret" {
		t.Fatalf("source key after copy = %#v err=%v", stored, err)
	}
}
