package petpack

import "testing"

func TestValidateManifestRejectsAmbiguousVariantIDs(t *testing.T) {
	manifest := &PetPackManifest{
		SchemaVersion: 1,
		ID:            "variant-test",
		Renderer:      RendererNative,
		Variants: []PetPackVariant{
			{ID: "default"},
			{ID: " default "},
		},
	}
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("expected duplicate variant ID rejection")
	}
}

func TestValidateManifestRejectsInvalidVariantRenderer(t *testing.T) {
	manifest := &PetPackManifest{
		SchemaVersion: 1,
		ID:            "variant-test",
		Renderer:      RendererNative,
		Variants:      []PetPackVariant{{ID: "night", Renderer: "canvas-script"}},
	}
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("expected invalid variant renderer rejection")
	}
}

func TestValidateManifestRejectsInvalidVariantID(t *testing.T) {
	manifest := &PetPackManifest{
		SchemaVersion: 1,
		ID:            "variant-test",
		Renderer:      RendererNative,
		Variants:      []PetPackVariant{{ID: "night mode"}},
	}
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("expected invalid variant ID rejection")
	}
}

func TestValidateManifestAllowsSkeletonVariantToInheritRenderer(t *testing.T) {
	manifest := &PetPackManifest{
		SchemaVersion: 2,
		ID:            "variant-test",
		Renderer:      RendererSkeleton,
		Assets: PetPackAssets{
			Native: map[string]string{"idle": "native/idle.png"},
			Rig:    &PetPackRigAssets{Definition: "rig/pet-rig.json", Textures: []string{"rig/body.png"}},
		},
		Variants: []PetPackVariant{{ID: "default", Assets: PetPackAssets{
			Rig: &PetPackRigAssets{Definition: "rig/default.json", Textures: []string{"rig/default.png"}},
		}}},
	}
	if err := ValidateManifest(manifest); err != nil {
		t.Fatalf("inherited skeleton variant should be valid: %v", err)
	}
}

func TestDefaultPresentationPrefersRuntimeDefaultVariant(t *testing.T) {
	manifest := &PetPackManifest{
		Renderer: RendererNative,
		Assets:   PetPackAssets{Native: map[string]string{"idle": "native/top.png", "thinking": "native/top-thinking.png"}},
		Variants: []PetPackVariant{
			{ID: "other", Tier: "figurative", Assets: PetPackAssets{Native: map[string]string{"idle": "native/other.png"}}},
			{ID: VariantDefault, Assets: PetPackAssets{Native: map[string]string{"idle": "native/default.png"}}},
		},
	}
	_, idle := DefaultPresentation(manifest)
	if idle != "native/default.png" {
		t.Fatalf("idle = %q, want default variant", idle)
	}
	if native := DefaultNativeAssets(manifest); native["thinking"] != "" || native["idle"] != "native/default.png" {
		t.Fatalf("native assets = %#v, want unmerged default variant map", native)
	}
}

func TestValidateManifestRequiresCompleteV3CharacterAssets(t *testing.T) {
	manifest := &PetPackManifest{
		SchemaVersion: 3,
		ID:            "v3-character",
		Renderer:      RendererCharacter,
		Assets: PetPackAssets{
			Native:    map[string]string{"idle": "native/idle.png"},
			Rig:       &PetPackRigAssets{Definition: "rig/character.json", Textures: []string{"rig/body.png"}},
			Character: &PetPackCharacterAssets{Definition: "character/performer.json"},
		},
		Capabilities: PetPackCaps{PetPerformanceV3: true},
	}
	if err := ValidateManifest(manifest); err != nil {
		t.Fatalf("valid v3 manifest: %v", err)
	}
	manifest.Capabilities.PetPerformanceV3 = false
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("missing v3 capability accepted")
	}
}
