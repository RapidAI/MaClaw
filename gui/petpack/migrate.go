package petpack

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// MigratePetVariant applies K18:
// - Pre-existing configs (PetVariantMigrated == false) with empty/missing variant
//   → persist classic + optional upgrade prompt; mark migrated.
// - Brand-new installs use AppConfigDefaults (PetVariant=default, Migrated=true).
// - Resolve empty variant always as classic (never pack figurative default).
// Returns true when the config was mutated and should be written to disk.
func MigratePetVariant(cfg *corelib.AppConfig) bool {
	if cfg == nil {
		return false
	}
	if cfg.PetVariantMigrated {
		// Still normalize unknown variant strings for safety.
		before := cfg.PetVariant
		cfg.PetVariant = NormalizeVariantID(cfg.PetVariant)
		// Empty with migrated=true is invalid; force classic (no figurative silent upgrade).
		if strings.TrimSpace(before) == "" {
			cfg.PetVariant = VariantClassic
		}
		return cfg.PetVariant != before
	}
	// Existing install path: empty → classic, no silent figurative upgrade.
	v := strings.TrimSpace(cfg.PetVariant)
	if v == "" {
		cfg.PetVariant = VariantClassic
		cfg.PetFigurativeUpgradePromptPending = true
	} else {
		cfg.PetVariant = NormalizeVariantID(v)
	}
	cfg.PetVariantMigrated = true
	return true
}

// NormalizeVariantID maps free-form variant to classic|default (or passthrough custom).
func NormalizeVariantID(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	switch v {
	case "", VariantClassic, "legacy", "line", "procedural":
		return VariantClassic
	case VariantDefault, "figurative", "fig":
		return VariantDefault
	default:
		if v == "" {
			return VariantClassic
		}
		return v
	}
}

// ResolveVariantForRuntime returns the variant used for frame selection.
// Empty always maps to classic (K18 Resolve rule).
func ResolveVariantForRuntime(stored string) string {
	stored = strings.TrimSpace(stored)
	if stored == "" {
		return VariantClassic
	}
	return NormalizeVariantID(stored)
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
