// Package petpack implements the MaClaw desktop pet pack registry, install,
// frame resolution, and motion/sound effective rules (design MVP PR-A…F).
package petpack

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"
)

// Official pack IDs shipped with the product. ClawMate is intentionally the
// sole maintained official visual reference; every other appearance is a
// user-created/imported/market pack.
var OfficialPackIDs = []string{"clawmate"}

const (
	DefaultPackID     = "clawmate"
	VariantClassic    = "classic"
	VariantDefault    = "default" // figurative
	StatusOK          = "ok"
	StatusInvalid     = "invalid"
	StatusUnsupported = "unsupported"
	ScopeBundled      = "bundled"
	ScopeUser         = "user"
	SourceCreated     = "created"
	SourceImported    = "imported"
	SourceMarket      = "market"
	RendererNative    = "native-raster"
	// RendererSkeleton is MaClaw's declarative 2D bone/slot renderer. It only
	// consumes local JSON plus raster textures; it never executes pack code.
	RendererSkeleton = "native-skeleton"
	// RendererCharacter is the v3 declarative performance renderer. It layers
	// behavior selection, state transitions, expression and secondary motion on
	// the same local bone/slot rig used by native-skeleton. Packs never execute
	// authored code.
	RendererCharacter  = "native-character"
	RendererProcedural = "procedural-fallback"
)

// PetRuntimeState is the semantic state shown by the native pet window.
type PetRuntimeState string

const (
	StateIdle      PetRuntimeState = "idle"
	StateListening PetRuntimeState = "listening"
	StateThinking  PetRuntimeState = "thinking"
	StateSpeaking  PetRuntimeState = "speaking"
	StateDone      PetRuntimeState = "done"
	StateAlert     PetRuntimeState = "alert"
	StateQuiet     PetRuntimeState = "quiet"
)

var validPackID = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)

// Variant IDs are intentionally a little more permissive than pack IDs:
// built-in values such as "default" and "classic" are valid single-word
// selections, while the same stable lowercase grammar keeps pack resolution
// unambiguous across the desktop and Pet Store.
var validVariantID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// IsValidPackID reports whether id matches the pet-pack id grammar.
func IsValidPackID(id string) bool {
	return validPackID.MatchString(strings.TrimSpace(id))
}

// PetPackManifest is the declarative pack descriptor (pet-pack.yaml).
type PetPackManifest struct {
	SchemaVersion    int               `yaml:"schema_version" json:"schema_version"`
	ID               string            `yaml:"id" json:"id"`
	Name             string            `yaml:"name" json:"name"`
	Version          string            `yaml:"version" json:"version"`
	Author           string            `yaml:"author" json:"author"`
	License          string            `yaml:"license" json:"license"`
	Description      string            `yaml:"description" json:"description"`
	Tier             string            `yaml:"tier" json:"tier"`
	MinMaclawVersion string            `yaml:"min_maclaw_version" json:"min_maclaw_version"`
	Label            map[string]string `yaml:"label" json:"label"`
	DescriptionI18n  map[string]string `yaml:"description_i18n" json:"description_i18n"`
	Preview          string            `yaml:"preview" json:"preview"`
	DefaultSize      int               `yaml:"default_size" json:"default_size"`
	Tone             string            `yaml:"tone" json:"tone"`
	Tags             []string          `yaml:"tags" json:"tags"`
	Renderer         string            `yaml:"renderer" json:"renderer"`
	FaceOverlay      bool              `yaml:"face_overlay" json:"face_overlay"`
	Assets           PetPackAssets     `yaml:"assets" json:"assets"`
	Fallback         PetPackFallback   `yaml:"fallback" json:"fallback,omitempty"`
	Variants         []PetPackVariant  `yaml:"variants" json:"variants"`
	Motion           PetPackMotion     `yaml:"motion" json:"motion"`
	Capabilities     PetPackCaps       `yaml:"capabilities" json:"capabilities"`
	Integrity        *PetPackIntegrity `yaml:"integrity" json:"integrity"`

	// Runtime fields (not in yaml)
	Dir    string `yaml:"-" json:"dir,omitempty"`
	Scope  string `yaml:"-" json:"scope,omitempty"`
	Status string `yaml:"-" json:"status,omitempty"`
	Error  string `yaml:"-" json:"error,omitempty"`
}

// PetPackAssets lists relative asset paths.
type PetPackAssets struct {
	Preview   string                  `yaml:"preview" json:"preview"`
	Native    map[string]string       `yaml:"native" json:"native"`
	Rig       *PetPackRigAssets       `yaml:"rig" json:"rig,omitempty"`
	Character *PetPackCharacterAssets `yaml:"character" json:"character,omitempty"`
}

// PetPackRigAssets describes the local, declarative assets used by a v2
// native-skeleton pack. Textures are intentionally an allowlisted list so
// package validation does not discover or load arbitrary files at runtime.
type PetPackRigAssets struct {
	Definition string   `yaml:"definition" json:"definition"`
	Textures   []string `yaml:"textures" json:"textures,omitempty"`
}

// PetPackCharacterAssets describes the v3 declarative performance definition.
// Definition is an allowlisted local JSON file, just like Rig.Definition.
type PetPackCharacterAssets struct {
	Definition string `yaml:"definition" json:"definition"`
}

// PetPackFallback declares the local static compatibility path used when a
// client lacks the v3 capability or the character renderer cannot load.
type PetPackFallback struct {
	Renderer string `yaml:"renderer" json:"renderer,omitempty"`
	Idle     string `yaml:"idle" json:"idle,omitempty"`
}

// PetPackVariant is a classic/figurative (or custom) presentation variant.
type PetPackVariant struct {
	ID          string        `yaml:"id" json:"id"`
	Tier        string        `yaml:"tier" json:"tier"`
	Renderer    string        `yaml:"renderer" json:"renderer"`
	FaceOverlay *bool         `yaml:"face_overlay" json:"face_overlay"`
	Assets      PetPackAssets `yaml:"assets" json:"assets"`
}

// DefaultPresentation returns the renderer and static idle path selected by
// the sole runtime presentation. It centralizes the legacy/default-variant
// inheritance rule so the desktop scanner and Pet Store validate the exact
// same fallback resource the runtime may display.
func DefaultPresentation(m *PetPackManifest) (renderer, idlePath string) {
	if m == nil {
		return "", ""
	}
	renderer = m.Renderer
	var native map[string]string = m.Assets.Native
	fallback := defaultPresentationVariant(m)
	if fallback != nil {
		variant := fallback
		if variant.Renderer != "" {
			renderer = variant.Renderer
		}
		if variant.Assets.Native != nil {
			native = variant.Assets.Native
		}
	}
	if native != nil {
		idlePath = strings.TrimSpace(native["idle"])
	}
	return renderer, idlePath
}

// DefaultNativeAssets returns the exact native state map selected by the
// default runtime presentation. A variant native map replaces—not merges—the
// top-level map, matching Registry.Resolve and keeping preview fallbacks
// faithful to the desktop pet.
func DefaultNativeAssets(m *PetPackManifest) map[string]string {
	if m == nil {
		return nil
	}
	if variant := defaultPresentationVariant(m); variant != nil && variant.Assets.Native != nil {
		return variant.Assets.Native
	}
	return m.Assets.Native
}

func defaultPresentationVariant(m *PetPackManifest) *PetPackVariant {
	if m == nil {
		return nil
	}
	for i := range m.Variants {
		if m.Variants[i].ID == VariantDefault {
			return &m.Variants[i]
		}
	}
	for i := range m.Variants {
		if m.Variants[i].Tier == "figurative" {
			return &m.Variants[i]
		}
	}
	return nil
}

// SkeletonRigAssets returns every rig that can be selected by a declared
// native-skeleton presentation. It is shared by desktop scanning and Pet Store
// review so neither side accepts a malformed non-default variant that the
// other side would reject.
func SkeletonRigAssets(m *PetPackManifest) []*PetPackRigAssets {
	if m == nil {
		return nil
	}
	assets := make([]*PetPackRigAssets, 0, 1+len(m.Variants))
	if m.Renderer == RendererSkeleton || m.Renderer == RendererCharacter {
		assets = append(assets, m.Assets.Rig)
	}
	for i := range m.Variants {
		variant := &m.Variants[i]
		renderer := variant.Renderer
		if renderer == "" {
			renderer = m.Renderer
		}
		if renderer == RendererSkeleton || renderer == RendererCharacter {
			assets = append(assets, variant.Assets.Rig)
		}
	}
	return assets
}

// PetPackMotion holds base motion timings and pack sound recommendation.
type PetPackMotion struct {
	IdleMs       int     `yaml:"idle_ms" json:"idle_ms"`
	ListeningMs  int     `yaml:"listening_ms" json:"listening_ms"`
	ThinkingMs   int     `yaml:"thinking_ms" json:"thinking_ms"`
	SpeakingMs   int     `yaml:"speaking_ms" json:"speaking_ms"`
	Amplitude    float64 `yaml:"amplitude" json:"amplitude"`
	SoundProfile string  `yaml:"sound_profile" json:"sound_profile"`
	Pitch        float64 `yaml:"pitch" json:"pitch"`
}

// PetPackCaps are asset-presence hints only.
type PetPackCaps struct {
	SupportsDoneState  bool `yaml:"supports_done_state" json:"supports_done_state"`
	SupportsAlertState bool `yaml:"supports_alert_state" json:"supports_alert_state"`
	PetPerformanceV3   bool `yaml:"pet_performance_v3" json:"pet_performance_v3"`
}

// PetPackIntegrity optional hash map.
type PetPackIntegrity struct {
	Algorithm string            `yaml:"algorithm" json:"algorithm"`
	Files     map[string]string `yaml:"files" json:"files"`
}

// PackInfo is the public list item for settings UI.
type PackInfo struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Version string            `json:"version"`
	Author  string            `json:"author"`
	Scope   string            `json:"scope"`
	Status  string            `json:"status"`
	Error   string            `json:"error,omitempty"`
	Tier    string            `json:"tier"`
	Tone    string            `json:"tone"`
	Label   map[string]string `json:"label,omitempty"`
	// DescriptionText preserves the manifest's non-localized description for
	// callers such as Pet Store publishing. Description remains the localized
	// map for compatibility with existing settings clients.
	DescriptionText string            `json:"description,omitempty"`
	Description     map[string]string `json:"description_i18n,omitempty"`
	Variants        []string          `json:"variants"`
	DefaultSize     int               `json:"default_size"`
	FaceOverlay     bool              `json:"face_overlay"`
	PreviewPath     string            `json:"preview_path,omitempty"`
	Dir             string            `json:"dir,omitempty"`
	// CanUninstall is true when a user-scoped install directory exists for this id
	// (including a user override of an official pack). Bundled-only packs are false.
	CanUninstall bool `json:"can_uninstall"`
	// Source distinguishes an author-created folder from an imported or market
	// installation. Only created packs can be listed by the local user.
	Source string `json:"source"`
	// HasPreview hints the client can call GetPetPackPreviewDataURL for a thumb.
	HasPreview bool `json:"has_preview"`
	// Renderer lets clients distinguish continuous skeleton motion from the
	// compatible static/raster path without duplicating manifest parsing.
	Renderer string `json:"renderer,omitempty"`
}

// ResolvedPack is the runtime selection for a skin + variant.
type ResolvedPack struct {
	Manifest    *PetPackManifest
	VariantID   string
	Renderer    string
	FaceOverlay bool
	// Native rel paths keyed by state (idle, listening, …)
	Native map[string]string
	// FS for reading assets; may be embed.FS or os.DirFS
	AssetFS fs.FS
	// Base path when using OS FS (empty for embed)
	AssetRoot string
	Motion    PetPackMotion
	Rig       *PetPackRigAssets
	Character *PetPackCharacterAssets
}

// ValidateManifest performs structural validation.
func ValidateManifest(m *PetPackManifest) error {
	if m == nil {
		return fmt.Errorf("nil manifest")
	}
	if m.SchemaVersion != 0 && m.SchemaVersion != 1 && m.SchemaVersion != 2 && m.SchemaVersion != 3 {
		return fmt.Errorf("unsupported schema_version %d", m.SchemaVersion)
	}
	id := strings.TrimSpace(m.ID)
	if !IsValidPackID(id) {
		return fmt.Errorf("invalid pack id %q", m.ID)
	}
	m.ID = id
	if strings.TrimSpace(m.Name) == "" {
		m.Name = id
	}
	if m.DefaultSize == 0 {
		m.DefaultSize = 88
	}
	switch m.Tone {
	case "", "balanced", "compact", "developer", "focus":
		if m.Tone == "" {
			m.Tone = "balanced"
		}
	default:
		m.Tone = "balanced"
	}
	switch m.Renderer {
	case "", RendererNative, RendererSkeleton, RendererCharacter, RendererProcedural:
		if m.Renderer == "" {
			m.Renderer = RendererNative
		}
	default:
		return fmt.Errorf("unsupported renderer %q", m.Renderer)
	}
	if err := validateSkeletonAssets(m.SchemaVersion, m.Renderer, m.Assets, m.Assets); err != nil {
		return err
	}
	if err := validateCharacterAssets(m.SchemaVersion, m.Renderer, m.Assets, m.Assets); err != nil {
		return err
	}
	if m.Renderer == RendererCharacter {
		if !m.Capabilities.PetPerformanceV3 {
			return fmt.Errorf("native-character requires capabilities.pet_performance_v3")
		}
		if m.Fallback.Renderer != "" && m.Fallback.Renderer != RendererSkeleton && m.Fallback.Renderer != RendererNative {
			return fmt.Errorf("native-character has unsupported fallback renderer %q", m.Fallback.Renderer)
		}
		if m.Fallback.Idle != "" && !safeAssetRel(m.Fallback.Idle) {
			return fmt.Errorf("native-character has unsafe fallback idle")
		}
	}
	variantIDs := make(map[string]bool, len(m.Variants))
	for i := range m.Variants {
		variant := &m.Variants[i]
		variant.ID = strings.TrimSpace(variant.ID)
		// An empty id is a legacy/default declaration. Keep it compatible, but
		// never permit two declarations that could resolve to the same fallback.
		if variant.ID != "" && !validVariantID.MatchString(variant.ID) {
			return fmt.Errorf("invalid variant id %q", variant.ID)
		}
		if variantIDs[variant.ID] {
			return fmt.Errorf("duplicate variant id %q", variant.ID)
		}
		variantIDs[variant.ID] = true
		renderer := variant.Renderer
		if renderer == "" {
			renderer = m.Renderer
		}
		switch renderer {
		case RendererNative, RendererSkeleton, RendererCharacter, RendererProcedural:
		default:
			return fmt.Errorf("variant %q: unsupported renderer %q", variant.ID, variant.Renderer)
		}
		if err := validateSkeletonAssets(m.SchemaVersion, renderer, variant.Assets, m.Assets); err != nil {
			return fmt.Errorf("variant %q: %w", variant.ID, err)
		}
		if err := validateCharacterAssets(m.SchemaVersion, renderer, variant.Assets, m.Assets); err != nil {
			return fmt.Errorf("variant %q: %w", variant.ID, err)
		}
	}
	if m.Motion.Pitch == 0 {
		m.Motion.Pitch = 1
	}
	if m.Motion.Amplitude == 0 {
		m.Motion.Amplitude = 0.85
	}
	if m.Motion.IdleMs == 0 {
		m.Motion.IdleMs = 4000
	}
	if m.Motion.ListeningMs == 0 {
		m.Motion.ListeningMs = 1200
	}
	if m.Motion.ThinkingMs == 0 {
		m.Motion.ThinkingMs = 1800
	}
	if m.Motion.SpeakingMs == 0 {
		m.Motion.SpeakingMs = 950
	}
	switch m.Motion.SoundProfile {
	case "", "classic", "bubble", "chime", "synth", "soft":
		if m.Motion.SoundProfile == "" {
			m.Motion.SoundProfile = "classic"
		}
	default:
		m.Motion.SoundProfile = "classic"
	}
	return nil
}

func safeAssetRel(path string) bool {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	return path != "" && !strings.HasPrefix(path, "/") && !strings.Contains(path, "..") && !strings.Contains(path, ":")
}

func validateCharacterAssets(schemaVersion int, renderer string, assets, fallback PetPackAssets) error {
	if renderer != RendererCharacter {
		return nil
	}
	if schemaVersion != 3 {
		return fmt.Errorf("native-character requires schema_version 3")
	}
	if assets.Rig == nil || strings.TrimSpace(assets.Rig.Definition) == "" || len(assets.Rig.Textures) == 0 {
		return fmt.Errorf("native-character requires assets.rig definition and textures")
	}
	if assets.Character == nil || strings.TrimSpace(assets.Character.Definition) == "" {
		return fmt.Errorf("native-character requires assets.character.definition")
	}
	idle := ""
	if assets.Native != nil {
		idle = strings.TrimSpace(assets.Native["idle"])
	}
	if idle == "" && fallback.Native != nil {
		idle = strings.TrimSpace(fallback.Native["idle"])
	}
	if idle == "" {
		return fmt.Errorf("native-character requires assets.native.idle fallback")
	}
	return nil
}

// validateSkeletonAssets accepts either a top-level skeleton pack or a
// skeleton presentation variant. Variant assets may inherit native fallbacks
// from the top-level declaration, but never inherit a rig silently.
func validateSkeletonAssets(schemaVersion int, renderer string, assets, fallback PetPackAssets) error {
	if renderer != RendererSkeleton {
		return nil
	}
	if schemaVersion != 2 {
		return fmt.Errorf("native-skeleton requires schema_version 2")
	}
	if assets.Rig == nil || strings.TrimSpace(assets.Rig.Definition) == "" {
		return fmt.Errorf("native-skeleton requires assets.rig.definition")
	}
	if len(assets.Rig.Textures) == 0 {
		return fmt.Errorf("native-skeleton requires assets.rig.textures")
	}
	idle := ""
	if assets.Native != nil {
		idle = strings.TrimSpace(assets.Native["idle"])
	}
	if idle == "" && fallback.Native != nil {
		idle = strings.TrimSpace(fallback.Native["idle"])
	}
	if idle == "" {
		return fmt.Errorf("native-skeleton requires assets.native.idle fallback")
	}
	return nil
}

// NormalizeState maps free-form state strings to known PetRuntimeState.
func NormalizeState(s string) PetRuntimeState {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "listening", "listen":
		return StateListening
	case "thinking", "think", "transcribe":
		return StateThinking
	case "speaking", "speak":
		return StateSpeaking
	case "done", "complete", "completed":
		return StateDone
	case "alert", "warning":
		return StateAlert
	case "quiet":
		return StateQuiet
	default:
		return StateIdle
	}
}
