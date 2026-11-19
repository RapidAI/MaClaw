package petpack

import "fmt"

// RearingMinAfterMS is the shortest loaded-runtime gap a stage may declare.
// A shorter gap would change the pet on a flicker instead of companionship.
const RearingMinAfterMS int64 = 60_000

// PetPackAdornment is one named prop worn on an existing bone. The slots are
// ordinary rig slots. They stay hidden until a rearing stage lists this id.
// A later stage replaces the worn set, so a kept prop is written again.
type PetPackAdornment struct {
	ID    string   `yaml:"id" json:"id"`
	Slots []string `yaml:"slots" json:"slots"`
}

const (
	maxAdornments     = 8
	maxAdornmentSlots = 4
)

// PetPackRearing is the optional companionship block. Packs that omit it
// keep one appearance for their whole life.
type PetPackRearing struct {
	Stages []PetPackRearingStage `yaml:"stages" json:"stages,omitempty"`
}

// PetPackRearingStage is one snapshot of the pet after it has been the live
// skin for AfterMS milliseconds. A later stage replaces the earlier one,
// including the adornment list. Variant empty keeps the user's presentation.
// Scale 0 means 1. An empty adornment list wears nothing.
type PetPackRearingStage struct {
	ID         string   `yaml:"id" json:"id"`
	AfterMS    int64    `yaml:"after_ms" json:"after_ms"`
	Variant    string   `yaml:"variant,omitempty" json:"variant,omitempty"`
	Scale      float64  `yaml:"scale,omitempty" json:"scale,omitempty"`
	Cue        string   `yaml:"cue,omitempty" json:"cue,omitempty"`
	Adornments []string `yaml:"adornments,omitempty" json:"adornments,omitempty"`
}

// VariantSelectable reports whether a variant is a user-facing presentation.
// Omitted selectable means yes. Rearing-only forms set it false.
func VariantSelectable(v PetPackVariant) bool {
	return v.Selectable == nil || *v.Selectable
}

// StageScale returns the stage's display multiplier. Zero and a nil stage are 1.
func StageScale(stage *PetPackRearingStage) float64 {
	if stage == nil || stage.Scale == 0 {
		return 1
	}
	return stage.Scale
}

// ActiveRearingStage returns the last stage whose threshold the loaded time
// has reached. loadedMS is wall time while this pack was the live pet.
func ActiveRearingStage(stages []PetPackRearingStage, loadedMS int64) (PetPackRearingStage, bool) {
	var best PetPackRearingStage
	found := false
	for _, stage := range stages {
		if stage.AfterMS > loadedMS {
			continue
		}
		if !found || stage.AfterMS >= best.AfterMS {
			best = stage
			found = true
		}
	}
	return best, found
}

// AllowedRearingCue reports whether cue may mark a stage crossing.
// Empty is silent. The names are existing host tones.
func AllowedRearingCue(cue string) bool {
	switch cue {
	case "", "notice", "focus", "celebrate", "concern":
		return true
	default:
		return false
	}
}

func validateRearing(m *PetPackManifest) error {
	if m == nil {
		return nil
	}
	if len(m.Rearing.Stages) == 0 {
		return validateAdornmentCatalog(m)
	}
	selectable := 0
	ids := make(map[string]PetPackVariant, len(m.Variants))
	for _, variant := range m.Variants {
		ids[variant.ID] = variant
		if VariantSelectable(variant) {
			selectable++
		}
	}
	if len(m.Variants) > 0 && selectable == 0 {
		return fmt.Errorf("rearing pack requires a selectable variant")
	}
	seen := make(map[string]bool, len(m.Rearing.Stages))
	var previous int64
	for i, stage := range m.Rearing.Stages {
		id := stage.ID
		if !validVariantID.MatchString(id) {
			return fmt.Errorf("rearing stage %q has an invalid id", stage.ID)
		}
		if seen[id] {
			return fmt.Errorf("duplicate rearing stage %q", id)
		}
		seen[id] = true
		if stage.AfterMS < RearingMinAfterMS {
			return fmt.Errorf("rearing stage %q after_ms must be at least %d", id, RearingMinAfterMS)
		}
		if i > 0 && stage.AfterMS <= previous {
			return fmt.Errorf("rearing stage %q after_ms must increase", id)
		}
		previous = stage.AfterMS
		if stage.Scale != 0 && (stage.Scale < 0.5 || stage.Scale > 1.5) {
			return fmt.Errorf("rearing stage %q scale must be from 0.5 to 1.5", id)
		}
		if !AllowedRearingCue(stage.Cue) {
			return fmt.Errorf("rearing stage %q has unknown cue %q", id, stage.Cue)
		}
		if stage.Variant == "" {
			continue
		}
		variant, ok := ids[stage.Variant]
		if !ok {
			return fmt.Errorf("rearing stage %q references unknown variant %q", id, stage.Variant)
		}
		if VariantSelectable(variant) {
			return fmt.Errorf("rearing stage %q variant %q must set selectable: false", id, stage.Variant)
		}
	}
	return validateAdornmentCatalog(m)
}

func validateAdornmentCatalog(m *PetPackManifest) error {
	if m == nil {
		return nil
	}
	if len(m.Adornments) == 0 {
		for _, stage := range m.Rearing.Stages {
			if len(stage.Adornments) > 0 {
				return fmt.Errorf("rearing stage %q lists adornments but the pack declares none", stage.ID)
			}
		}
		return nil
	}
	if len(m.Adornments) > maxAdornments {
		return fmt.Errorf("a pack may declare at most %d adornments", maxAdornments)
	}
	byID := make(map[string]PetPackAdornment, len(m.Adornments))
	usedSlots := make(map[string]string)
	for _, item := range m.Adornments {
		if !validVariantID.MatchString(item.ID) {
			return fmt.Errorf("adornment %q has an invalid id", item.ID)
		}
		if _, ok := byID[item.ID]; ok {
			return fmt.Errorf("duplicate adornment %q", item.ID)
		}
		if len(item.Slots) == 0 || len(item.Slots) > maxAdornmentSlots {
			return fmt.Errorf("adornment %q must name 1-%d slots", item.ID, maxAdornmentSlots)
		}
		seen := make(map[string]bool, len(item.Slots))
		for _, slot := range item.Slots {
			if !validRigName(slot) || seen[slot] {
				return fmt.Errorf("adornment %q has invalid slot %q", item.ID, slot)
			}
			seen[slot] = true
			if owner, ok := usedSlots[slot]; ok {
				return fmt.Errorf("slot %q is in both adornments %q and %q", slot, owner, item.ID)
			}
			usedSlots[slot] = item.ID
		}
		byID[item.ID] = item
	}
	for _, stage := range m.Rearing.Stages {
		if len(stage.Adornments) > maxAdornments {
			return fmt.Errorf("rearing stage %q lists too many adornments", stage.ID)
		}
		seen := make(map[string]bool, len(stage.Adornments))
		for _, id := range stage.Adornments {
			if seen[id] {
				return fmt.Errorf("rearing stage %q repeats adornment %q", stage.ID, id)
			}
			seen[id] = true
			if _, ok := byID[id]; !ok {
				return fmt.Errorf("rearing stage %q references unknown adornment %q", stage.ID, id)
			}
		}
	}
	return nil
}

// ValidateAdornmentSlots checks that every declared prop names a slot on this
// rig. Desktop install and the Pet Store both call it after the rig parses.
func ValidateAdornmentSlots(m *PetPackManifest, rig *Rig) error {
	if m == nil || rig == nil || len(m.Adornments) == 0 {
		return nil
	}
	names := make(map[string]bool, len(rig.Slots))
	for _, slot := range rig.Slots {
		names[slot.Name] = true
	}
	for _, item := range m.Adornments {
		for _, slot := range item.Slots {
			if !names[slot] {
				return fmt.Errorf("adornment %q references missing slot %q", item.ID, slot)
			}
		}
	}
	return nil
}

// ActiveAdornmentSlots is the rig slots worn at loadedMS. No stage, or a
// stage with an empty list, wears nothing. The order follows the catalog.
func ActiveAdornmentSlots(m *PetPackManifest, loadedMS int64) []string {
	if m == nil || len(m.Adornments) == 0 {
		return nil
	}
	stage, ok := ActiveRearingStage(m.Rearing.Stages, loadedMS)
	if !ok || len(stage.Adornments) == 0 {
		return nil
	}
	want := make(map[string]bool, len(stage.Adornments))
	for _, id := range stage.Adornments {
		want[id] = true
	}
	var out []string
	seen := make(map[string]bool)
	for _, item := range m.Adornments {
		if !want[item.ID] {
			continue
		}
		for _, slot := range item.Slots {
			if seen[slot] {
				continue
			}
			seen[slot] = true
			out = append(out, slot)
		}
	}
	return out
}
