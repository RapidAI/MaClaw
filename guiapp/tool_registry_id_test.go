package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/toolid"
)

func TestToolRegistryIDDerivation(t *testing.T) {
	reg := NewToolRegistry()
	for _, name := range []string{"web_search", "Glob", "FileRead"} {
		if err := reg.Register(RegisteredTool{Name: name}); err != nil {
			t.Fatalf("register %q: %v", name, err)
		}
		entry, ok := reg.Get(name)
		if !ok {
			t.Fatalf("Get(%q) missing", name)
		}
		want := toolid.MustParse("core:" + name)
		if entry.ID != want {
			t.Fatalf("ID(%q) = %q, want %q", name, entry.ID, want)
		}
	}
}

func TestToolRegistryExplicitIDPreserved(t *testing.T) {
	reg := NewToolRegistry()
	explicit := toolid.MustParse("host:custom")
	if err := reg.Register(RegisteredTool{Name: "custom_lookup", ID: explicit}); err != nil {
		t.Fatalf("register: %v", err)
	}
	entry, ok := reg.Get("custom_lookup")
	if !ok || entry.ID != explicit {
		t.Fatalf("explicit ID not preserved: entry=%+v ok=%v", entry, ok)
	}
	found, ok := reg.FindByID(explicit)
	if !ok || found.Name != "custom_lookup" {
		t.Fatalf("FindByID(%q) = %+v,%v", explicit, found, ok)
	}
}

func TestToolRegistryInvalidNameLeavesZeroID(t *testing.T) {
	reg := NewToolRegistry()
	bad := "bad name:with space"
	if err := reg.Register(RegisteredTool{Name: bad}); err != nil {
		t.Fatalf("register must not fail for ID-derivation problems: %v", err)
	}
	entry, ok := reg.Get(bad)
	if !ok {
		t.Fatalf("tool %q must stay registered (name-addressable)", bad)
	}
	if !entry.ID.IsZero() {
		t.Fatalf("ID = %q, want zero for unparseable name", entry.ID)
	}
	if _, found := reg.FindByID(entry.ID); found {
		t.Fatal("zero-ID entry must not be FindByID-addressable")
	}
}

func TestToolRegistryFindByIDRoundTrip(t *testing.T) {
	reg := NewToolRegistry()
	for _, name := range []string{"read_file", "ripgrep"} {
		if err := reg.Register(RegisteredTool{Name: name}); err != nil {
			t.Fatalf("register %q: %v", name, err)
		}
	}
	entry, ok := reg.Get("ripgrep")
	if !ok {
		t.Fatal("Get(ripgrep) missing")
	}
	found, ok := reg.FindByID(entry.ID)
	if !ok || found.Name != "ripgrep" {
		t.Fatalf("FindByID round trip = %+v,%v", found, ok)
	}
	if _, ok := reg.FindByID(toolid.MustParse("core:never_registered")); ok {
		t.Fatal("FindByID must miss unknown IDs")
	}
	if _, ok := reg.FindByID(toolid.ToolID{}); ok {
		t.Fatal("FindByID(zero) must miss")
	}
}

func TestToolRegistryRipgrepRegisteredWithCanonicalID(t *testing.T) {
	reg := NewToolRegistry()
	registerBuiltinTools(reg, &IMMessageHandler{})
	entry, ok := reg.Get("ripgrep")
	if !ok {
		t.Fatal("ripgrep must be registered by registerBuiltinTools")
	}
	if entry.ID != toolid.MustParse("core:ripgrep") {
		t.Fatalf("ripgrep ID = %q, want core:ripgrep", entry.ID)
	}
	found, ok := reg.FindByID(toolid.MustParse("core:ripgrep"))
	if !ok || found.Name != "ripgrep" {
		t.Fatalf("FindByID(core:ripgrep) = %+v,%v", found, ok)
	}
	if entry.HandlerCtx == nil {
		t.Fatal("ripgrep registration must carry the context-aware handler")
	}
}

func TestToolRegistryCodingKnowledgeSearchRegisteredWithCanonicalID(t *testing.T) {
	reg := NewToolRegistry()
	app := &App{}
	registerKnowledgeTools(reg, app)
	entry, ok := reg.Get("coding_knowledge_search")
	if !ok {
		t.Fatal("coding_knowledge_search must be registered by registerKnowledgeTools")
	}
	if entry.ID != toolid.MustParse("core:coding_knowledge_search") {
		t.Fatalf("coding_knowledge_search ID = %q, want core:coding_knowledge_search", entry.ID)
	}
	found, ok := reg.FindByID(toolid.MustParse("core:coding_knowledge_search"))
	if !ok || found.Name != "coding_knowledge_search" {
		t.Fatalf("FindByID(core:coding_knowledge_search) = %+v,%v", found, ok)
	}
}
