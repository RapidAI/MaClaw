// Package petpack implements the MaClaw desktop pet pack registry, install,
// frame resolution, and motion/sound effective rules (design MVP PR-A…F).
package petpack

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"
)

// Official pack IDs shipped as bundled defaults.
var OfficialPackIDs = []string{"clawmate", "mini-claw", "dev-claw", "focus-claw"}

const (
	DefaultPackID     = "clawmate"
	VariantClassic    = "classic"
	VariantDefault    = "default" // figurative
	StatusOK          = "ok"
	StatusInvalid     = "invalid"
	StatusUnsupported = "unsupported"
	ScopeBundled      = "bundled"
	ScopeUser         = "user"
	RendererNative    = "native-raster"
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
	Preview string            `yaml:"preview" json:"preview"`
	Native  map[string]string `yaml:"native" json:"native"`
}

// PetPackVariant is a classic/figurative (or custom) presentation variant.
type PetPackVariant struct {
	ID          string        `yaml:"id" json:"id"`
	Tier        string        `yaml:"tier" json:"tier"`
	Renderer    string        `yaml:"renderer" json:"renderer"`
	FaceOverlay *bool         `yaml:"face_overlay" json:"face_overlay"`
	Assets      PetPackAssets `yaml:"assets" json:"assets"`
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
}

// PetPackIntegrity optional hash map.
type PetPackIntegrity struct {
	Algorithm string            `yaml:"algorithm" json:"algorithm"`
	Files     map[string]string `yaml:"files" json:"files"`
}

// PackInfo is the public list item for settings UI.
type PackInfo struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Author      string            `json:"author"`
	Scope       string            `json:"scope"`
	Status      string            `json:"status"`
	Error       string            `json:"error,omitempty"`
	Tier        string            `json:"tier"`
	Tone        string            `json:"tone"`
	Label       map[string]string `json:"label,omitempty"`
	Description map[string]string `json:"description_i18n,omitempty"`
	Variants    []string          `json:"variants"`
	DefaultSize int               `json:"default_size"`
	FaceOverlay bool              `json:"face_overlay"`
	PreviewPath string            `json:"preview_path,omitempty"`
	Dir         string            `json:"dir,omitempty"`
	// CanUninstall is true when a user-scoped install directory exists for this id
	// (including a user override of an official pack). Bundled-only packs are false.
	CanUninstall bool `json:"can_uninstall"`
	// HasPreview hints the client can call GetPetPackPreviewDataURL for a thumb.
	HasPreview bool `json:"has_preview"`
}

// ResolvedPack is the runtime selection for a skin + variant.
type ResolvedPack struct {
	Manifest   *PetPackManifest
	VariantID  string
	Renderer   string
	FaceOverlay bool
	// Native rel paths keyed by state (idle, listening, …)
	Native map[string]string
	// FS for reading assets; may be embed.FS or os.DirFS
	AssetFS fs.FS
	// Base path when using OS FS (empty for embed)
	AssetRoot string
	Motion    PetPackMotion
}

// ValidateManifest performs structural validation.
func ValidateManifest(m *PetPackManifest) error {
	if m == nil {
		return fmt.Errorf("nil manifest")
	}
	if m.SchemaVersion != 0 && m.SchemaVersion != 1 {
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
	case "", RendererNative, RendererProcedural:
		if m.Renderer == "" {
			m.Renderer = RendererNative
		}
	default:
		return fmt.Errorf("unsupported renderer %q", m.Renderer)
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
