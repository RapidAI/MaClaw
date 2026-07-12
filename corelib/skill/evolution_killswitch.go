package skill

import (
	"os"
	"strings"
)

// EvolutionEnvDisabled reports the process-wide environment kill switch:
//
//	MACLAW_DISABLE_SKILL_EVOLUTION=1|true|yes|on
//
// When true, GUI and TUI should skip feeding runs into EvolutionPipeline
// (self-repair / optimize / promote). Session-level opt-out (TUI only) is
// layered on top by the TUI commands package.
func EvolutionEnvDisabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("MACLAW_DISABLE_SKILL_EVOLUTION")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}
