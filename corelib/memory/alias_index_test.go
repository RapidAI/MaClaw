package memory

import (
	"testing"
)

func TestAliasIndex_Register_Bidirectional(t *testing.T) {
	ai := NewAliasIndex()
	ai.Register("4090服务器", []string{"api.rapidai.tech"})

	// Forward: 4090服务器 → api.rapidai.tech
	aliases := ai.Expand([]string{"4090服务器"})
	if len(aliases) != 1 || aliases[0] != "api.rapidai.tech" {
		t.Errorf("expected [api.rapidai.tech], got %v", aliases)
	}

	// Reverse: api.rapidai.tech → 4090服务器
	aliases = ai.Expand([]string{"api.rapidai.tech"})
	if len(aliases) != 1 || aliases[0] != "4090服务器" {
		t.Errorf("expected [4090服务器], got %v", aliases)
	}
}

func TestAliasIndex_Register_MultipleAliases(t *testing.T) {
	ai := NewAliasIndex()
	ai.Register("gpu-server", []string{"4090服务器", "api.rapidai.tech"})

	aliases := ai.Expand([]string{"gpu-server"})
	if len(aliases) != 2 {
		t.Fatalf("expected 2 aliases, got %d: %v", len(aliases), aliases)
	}
	// Both aliases should be present.
	found := map[string]bool{}
	for _, a := range aliases {
		found[a] = true
	}
	if !found["4090服务器"] || !found["api.rapidai.tech"] {
		t.Errorf("missing expected aliases in %v", aliases)
	}
}

func TestAliasIndex_Expand_Deduplicated(t *testing.T) {
	ai := NewAliasIndex()
	ai.Register("server", []string{"host"})
	ai.Register("server", []string{"host"}) // duplicate registration

	aliases := ai.Expand([]string{"server"})
	if len(aliases) != 1 {
		t.Errorf("expected 1 deduplicated alias, got %d: %v", len(aliases), aliases)
	}
}

func TestAliasIndex_Expand_ExcludesInputEntities(t *testing.T) {
	ai := NewAliasIndex()
	ai.Register("alpha", []string{"beta", "gamma"})

	// When both "alpha" and "beta" are input, beta should not appear in results
	// (it's already in the input set).
	aliases := ai.Expand([]string{"alpha", "beta"})
	if len(aliases) != 1 || aliases[0] != "gamma" {
		t.Errorf("expected [gamma], got %v", aliases)
	}
}

func TestAliasIndex_Expand_CaseInsensitive(t *testing.T) {
	ai := NewAliasIndex()
	ai.Register("SSH Server", []string{"api.rapidai.tech"})

	aliases := ai.Expand([]string{"ssh server"})
	if len(aliases) != 1 || aliases[0] != "api.rapidai.tech" {
		t.Errorf("expected [api.rapidai.tech], got %v", aliases)
	}
}

func TestAliasIndex_Expand_EmptyInput(t *testing.T) {
	ai := NewAliasIndex()
	ai.Register("a", []string{"b"})

	if aliases := ai.Expand(nil); aliases != nil {
		t.Errorf("expected nil for nil input, got %v", aliases)
	}
	if aliases := ai.Expand([]string{}); aliases != nil {
		t.Errorf("expected nil for empty input, got %v", aliases)
	}
}

func TestAliasIndex_Expand_NoMatch(t *testing.T) {
	ai := NewAliasIndex()
	ai.Register("a", []string{"b"})

	aliases := ai.Expand([]string{"unknown"})
	if len(aliases) != 0 {
		t.Errorf("expected no aliases for unknown entity, got %v", aliases)
	}
}

func TestAliasIndex_FIFO_Eviction(t *testing.T) {
	ai := &AliasIndex{
		aliases:  make(map[string][]string),
		order:    make([]string, 0, 5),
		capacity: 5, // small capacity for testing
	}

	// Register 5 terms to fill capacity.
	ai.Register("a", []string{"x"})
	ai.Register("b", []string{"y"})
	ai.Register("c", []string{"z"})
	// Note: each Register creates 2 entries (bidirectional), so after 3
	// Register calls we might have up to 6 keys. Let's check with exact control.

	// Use a fresh index with capacity 5 and register terms directly.
	ai2 := &AliasIndex{
		aliases:  make(map[string][]string),
		order:    make([]string, 0, 5),
		capacity: 5,
	}
	// Add exactly 5 keys by using single-direction adds.
	ai2.mu.Lock()
	ai2.addMappingLocked("term1", "alias1")
	ai2.addMappingLocked("term2", "alias2")
	ai2.addMappingLocked("term3", "alias3")
	ai2.addMappingLocked("term4", "alias4")
	ai2.addMappingLocked("term5", "alias5")
	ai2.mu.Unlock()

	if ai2.Len() != 5 {
		t.Fatalf("expected 5 entries, got %d", ai2.Len())
	}

	// Adding one more should evict term1 (FIFO).
	ai2.mu.Lock()
	ai2.addMappingLocked("term6", "alias6")
	ai2.mu.Unlock()

	if ai2.Len() != 5 {
		t.Errorf("expected 5 entries after eviction, got %d", ai2.Len())
	}

	// term1 should be evicted.
	aliases := ai2.Expand([]string{"term1"})
	if len(aliases) != 0 {
		t.Errorf("expected term1 to be evicted, got aliases: %v", aliases)
	}

	// term6 should be present.
	aliases = ai2.Expand([]string{"term6"})
	if len(aliases) != 1 || aliases[0] != "alias6" {
		t.Errorf("expected [alias6] for term6, got %v", aliases)
	}
}

func TestAliasIndex_Rebuild_FromEntries(t *testing.T) {
	ai := NewAliasIndex()

	entries := []Entry{
		{
			Tags:   []string{"4090服务器", "api.rapidai.tech", "ssh"},
			Status: StatusActive,
		},
		{
			Tags:   []string{"deepseek", "llm-provider"},
			Status: StatusActive,
		},
		{
			// Inactive entry should be skipped.
			Tags:   []string{"old-server", "deprecated.host"},
			Status: StatusSuperseded,
		},
		{
			// Single tag: no pairs to form aliases.
			Tags:   []string{"single-tag"},
			Status: StatusActive,
		},
	}

	ai.Rebuild(entries)

	// Entry 1 has 3 tags: 3 pairs → 6 bidirectional mappings.
	// 4090服务器 ↔ api.rapidai.tech
	// 4090服务器 ↔ ssh
	// api.rapidai.tech ↔ ssh
	aliases := ai.Expand([]string{"4090服务器"})
	if len(aliases) != 2 {
		t.Errorf("expected 2 aliases for 4090服务器, got %d: %v", len(aliases), aliases)
	}

	// Entry 2: deepseek ↔ llm-provider
	aliases = ai.Expand([]string{"deepseek"})
	if len(aliases) != 1 || aliases[0] != "llm-provider" {
		t.Errorf("expected [llm-provider] for deepseek, got %v", aliases)
	}

	// Entry 3 (inactive): should not be indexed.
	aliases = ai.Expand([]string{"old-server"})
	if len(aliases) != 0 {
		t.Errorf("expected no aliases for inactive entry, got %v", aliases)
	}
}

func TestAliasIndex_Register_SelfAlias(t *testing.T) {
	ai := NewAliasIndex()
	ai.Register("same", []string{"SAME", "same", "  Same  "})

	// Should not store self-aliases (case-insensitive).
	aliases := ai.Expand([]string{"same"})
	if len(aliases) != 0 {
		t.Errorf("expected no aliases for self-alias, got %v", aliases)
	}
}

func TestAliasIndex_Register_EmptyInputs(t *testing.T) {
	ai := NewAliasIndex()
	ai.Register("", []string{"alias"})   // empty term
	ai.Register("term", nil)             // nil aliases
	ai.Register("term", []string{})      // empty aliases
	ai.Register("term", []string{""})    // empty alias string

	if ai.Len() != 0 {
		t.Errorf("expected empty index after invalid inputs, got %d entries", ai.Len())
	}
}

func TestAliasMatchBoost_Constant(t *testing.T) {
	// Verify the boost value is positioned correctly relative to other boosts.
	if AliasMatchBoost <= 0 {
		t.Error("AliasMatchBoost must be positive")
	}
	if AliasMatchBoost >= 5.0 {
		t.Error("AliasMatchBoost must be below tagExactMatchBoost (+5.0)")
	}
	if AliasMatchBoost != 2.0 {
		t.Errorf("AliasMatchBoost should be 2.0, got %f", AliasMatchBoost)
	}
}
