package petpack

import "testing"

func rearingManifest() *PetPackManifest {
	hidden := false
	return &PetPackManifest{
		SchemaVersion: 1,
		ID:            "tadpole",
		Renderer:      RendererNative,
		Variants: []PetPackVariant{
			{ID: "default"},
			{ID: "frog", Selectable: &hidden},
		},
		Rearing: PetPackRearing{Stages: []PetPackRearingStage{
			{ID: "coat", AfterMS: 3_600_000, Scale: 1.1, Cue: "notice"},
			{ID: "frog", AfterMS: 86_400_000, Variant: "frog", Scale: 1.25, Cue: "celebrate"},
		}},
	}
}

func TestValidateRearingAcceptsHiddenForm(t *testing.T) {
	if err := ValidateManifest(rearingManifest()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRearingRejectsShortGapAndSelectableTarget(t *testing.T) {
	manifest := rearingManifest()
	manifest.Rearing.Stages[0].AfterMS = 1000
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("short after_ms accepted")
	}
	manifest = rearingManifest()
	manifest.Rearing.Stages[1].Variant = "default"
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("selectable rearing target accepted")
	}
}

func TestActiveRearingStageUsesHighestReached(t *testing.T) {
	stages := rearingManifest().Rearing.Stages
	if _, ok := ActiveRearingStage(stages, 0); ok {
		t.Fatal("zero time selected a stage")
	}
	stage, ok := ActiveRearingStage(stages, 3_600_000)
	if !ok || stage.ID != "coat" || StageScale(&stage) != 1.1 {
		t.Fatalf("coat = %+v ok=%v", stage, ok)
	}
	stage, ok = ActiveRearingStage(stages, 90_000_000)
	if !ok || stage.ID != "frog" || stage.Variant != "frog" {
		t.Fatalf("frog = %+v ok=%v", stage, ok)
	}
}

func TestAdornmentCatalogRejectsUnknownAndSharedSlots(t *testing.T) {
	manifest := rearingManifest()
	manifest.Rearing.Stages[0].Adornments = []string{"charm"}
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("stage adornment without a catalog was accepted")
	}
	manifest = rearingManifest()
	manifest.Adornments = []PetPackAdornment{
		{ID: "charm", Slots: []string{"hand"}},
		{ID: "charm", Slots: []string{"other"}},
	}
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("duplicate adornment id was accepted")
	}
	manifest = rearingManifest()
	manifest.Adornments = []PetPackAdornment{
		{ID: "charm", Slots: []string{"hand"}},
		{ID: "ribbon", Slots: []string{"hand"}},
	}
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("one slot in two adornments was accepted")
	}
	manifest = rearingManifest()
	manifest.Adornments = []PetPackAdornment{{ID: "charm", Slots: []string{"hand"}}}
	manifest.Rearing.Stages[1].Adornments = []string{"missing"}
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("unknown adornment id was accepted")
	}
}

func TestAdornmentSlotsMustExistOnTheRig(t *testing.T) {
	manifest := &PetPackManifest{Adornments: []PetPackAdornment{{ID: "charm", Slots: []string{"mouse_charm"}}}}
	rig := &Rig{Slots: []RigSlot{{Name: "arm_l", Bone: "arm_l", Texture: "rig/arm.png"}}}
	if err := ValidateAdornmentSlots(manifest, rig); err == nil {
		t.Fatal("missing rig slot was accepted")
	}
	rig.Slots = append(rig.Slots, RigSlot{Name: "mouse_charm", Bone: "arm_l", Texture: "rig/charm.png"})
	if err := ValidateAdornmentSlots(manifest, rig); err != nil {
		t.Fatal(err)
	}
}

func TestActiveAdornmentSlotsReplaceThePreviousSet(t *testing.T) {
	manifest := rearingManifest()
	manifest.Adornments = []PetPackAdornment{
		{ID: "charm", Slots: []string{"hand"}},
		{ID: "shoes", Slots: []string{"foot_l", "foot_r"}},
	}
	manifest.Rearing.Stages[0].Adornments = []string{"charm"}
	manifest.Rearing.Stages[1].Adornments = []string{"shoes"}
	if err := ValidateManifest(manifest); err != nil {
		t.Fatal(err)
	}
	if slots := ActiveAdornmentSlots(manifest, 0); len(slots) != 0 {
		t.Fatalf("before the first stage wears %v", slots)
	}
	charm := ActiveAdornmentSlots(manifest, 3_600_000)
	if len(charm) != 1 || charm[0] != "hand" {
		t.Fatalf("first stage = %v", charm)
	}
	shoes := ActiveAdornmentSlots(manifest, 86_400_000)
	if len(shoes) != 2 || shoes[0] != "foot_l" || shoes[1] != "foot_r" {
		t.Fatalf("later stage kept the earlier prop: %v", shoes)
	}
}

func TestTaskNeedsYouIsAnAllowedPerformerEvent(t *testing.T) {
	if !IsAllowedPerformerEvent("task_needs_you") {
		t.Fatal("task_needs_you was rejected")
	}
	if IsAllowedPerformerEvent("task_custom") {
		t.Fatal("unknown task event was accepted")
	}
}
