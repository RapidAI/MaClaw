package agent

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/permission"
)

// R4 invariant tests: the light allowlist and the fail-closed core fallback
// carry a classification-backed safety story. Every allowlisted tool must be
// classified by permission.BuiltinKind (no unclassified names silently
// entering the light surface), and — the load-bearing assertion — no
// execute-kind tool may sit on the light allowlist: light turns must never
// see shell/process tools, which are the whole reason the profile exists.
func TestLightAllowlistClassificationInvariants(t *testing.T) {
	for name := range LightTurnToolAllowlist {
		kind := permission.BuiltinKind(name)
		if kind == "" {
			t.Fatalf("allowlisted tool %q has no BuiltinKind classification", name)
		}
		if kind == permission.KindExecute {
			t.Fatalf("execute-kind tool %q must not be on the light allowlist", name)
		}
	}
}

func TestLightCoreFallbackClassificationInvariants(t *testing.T) {
	for name := range LightTurnCoreFallbackTools {
		if !LightTurnToolAllowlist[name] {
			t.Fatalf("core fallback tool %q must also be allowlisted", name)
		}
		if kind := permission.BuiltinKind(name); kind != permission.KindRead {
			t.Fatalf("core fallback tool %q must be read-kind, got %q", name, kind)
		}
	}
}
