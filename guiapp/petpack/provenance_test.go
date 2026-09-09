package petpack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryPackSourceDefaultsToCreatedAndPersistsInstallSources(t *testing.T) {
	userRoot := t.TempDir()
	packDir := filepath.Join(userRoot, "creator-pet")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "pet-pack.yaml"), []byte("schema_version: 1\nid: creator-pet\nname: Creator pet\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(userRoot, BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	if pack, ok := reg.Get("creator-pet"); !ok || pack.Scope != ScopeUser || packSourceForDir(pack.Dir) != SourceCreated {
		t.Fatalf("unmarked user pack should be creator-owned: pack=%+v ok=%v", pack, ok)
	}
	if !reg.IsPackCreatorOwned("creator-pet") {
		t.Fatal("unmarked user pack should be creator-owned")
	}
	if err := reg.SetPackSource("creator-pet", SourceImported); err != nil {
		t.Fatal(err)
	}
	if reg.IsPackCreatorOwned("creator-pet") {
		t.Fatal("imported pack should not be creator-owned")
	}
	pack, ok := reg.Get("creator-pet")
	if !ok || packSourceForDir(pack.Dir) != SourceImported {
		t.Fatalf("source = %q, want imported", packSourceForDir(pack.Dir))
	}
	if err := reg.SetPackSource("creator-pet", SourceMarket); err != nil {
		t.Fatal(err)
	}
	pack, ok = reg.Get("creator-pet")
	if !ok || packSourceForDir(pack.Dir) != SourceMarket {
		t.Fatalf("source = %q, want market", packSourceForDir(pack.Dir))
	}
}

func TestIsPackSourceMarker(t *testing.T) {
	if !IsPackSourceMarker(".maclaw-pet-source") || !IsPackSourceMarker(filepath.Join("nested", ".maclaw-pet-source")) {
		t.Fatal("expected provenance marker to be recognized")
	}
	if IsPackSourceMarker("pet-pack.yaml") {
		t.Fatal("manifest must not be treated as provenance metadata")
	}
}
