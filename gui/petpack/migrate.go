package petpack

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// MigratePetVariant normalizes the retired quality-style fields so old settings
// render the selected pack with its default native presentation.
func MigratePetVariant(cfg *corelib.AppConfig) bool {
	if cfg == nil {
		return false
	}
	changed := cfg.PetVariant != VariantDefault ||
		!cfg.PetVariantMigrated ||
		cfg.PetFigurativeUpgradePromptPending
	cfg.PetVariant = VariantDefault
	cfg.PetVariantMigrated = true
	cfg.PetFigurativeUpgradePromptPending = false
	return changed
}

// NormalizeVariantID preserves the legacy field for config compatibility. Pet packs now
// always render their default native presentation, so every legacy value resolves there.
func NormalizeVariantID(v string) string {
	return VariantDefault
}

// ResolveVariantForRuntime returns the sole runtime presentation. The removed
// quality-style selector used to map "classic" to the built-in procedural pet,
// which meant a selected custom pack could disappear from the desktop.
func ResolveVariantForRuntime(stored string) string {
	return VariantDefault
}

// SanitizeSkinID applies allowlist rules (design B.4).
//
// registryReady=false: keep official IDs and any well-formed pack id (do not wipe user packs).
// registryReady=true: keep ids present in allowlist (ok + installed invalid); unknown → clawmate.
func SanitizeSkinID(skin string, registryReady bool, allowlist map[string]bool) string {
	skin = strings.TrimSpace(skin)
	if skin == "" {
		return DefaultPackID
	}
	if !registryReady {
		if IsOfficialPackID(skin) || IsValidPackID(skin) {
			return skin
		}
		return DefaultPackID
	}
	if allowlist != nil && allowlist[skin] {
		return skin
	}
	// Always accept official even if scan missed them (builtin fallback).
	if IsOfficialPackID(skin) {
		return skin
	}
	return DefaultPackID
}

// IsOfficialPackID reports whether id is one of the four bundled skins.
func IsOfficialPackID(id string) bool {
	switch strings.TrimSpace(id) {
	case "clawmate", "mini-claw", "dev-claw", "focus-claw":
		return true
	default:
		return false
	}
}

// OfficialAllowlist returns a map of the four official pack ids.
func OfficialAllowlist() map[string]bool {
	m := make(map[string]bool, len(OfficialPackIDs))
	for _, id := range OfficialPackIDs {
		m[id] = true
	}
	return m
}
