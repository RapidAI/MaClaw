package doctor

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestSharedLoopCheck_OnFromConfig(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "")
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_SHADOW", "")
	cfg := corelib.AppConfig{SharedAgentLoopEnabled: true, SharedAgentLoopMigrated: true}
	c := SharedLoopCheck(cfg)
	if c.ID != "agent.shared_loop" || c.Status != StatusOK {
		t.Fatalf("%+v", c)
	}
	if ResolveSharedLoopEnv(cfg).Mode != "on" {
		t.Fatal("mode on")
	}
}

func TestSharedLoopCheck_EnvShadow(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "shadow")
	c := SharedLoopCheck(corelib.AppConfig{SharedAgentLoopEnabled: true})
	if c.Status != StatusInfo || ResolveSharedLoopEnv(corelib.AppConfig{}).Mode != "shadow" {
		t.Fatalf("%+v", c)
	}
}

func TestSharedLoopCheck_EnvOff(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "0")
	c := SharedLoopCheck(corelib.AppConfig{SharedAgentLoopEnabled: true})
	if ResolveSharedLoopEnv(corelib.AppConfig{SharedAgentLoopEnabled: true}).Mode != "off" {
		t.Fatal("env wins")
	}
	if c.Status != StatusInfo {
		t.Fatalf("%+v", c)
	}
}

func TestResolveSharedLoopPercent_ConfigAndEnv(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "")
	p25 := 25
	cfg := corelib.AppConfig{SharedAgentLoopCanaryPercent: &p25}
	n, fromEnv := ResolveSharedLoopPercent(cfg)
	if n != 25 || fromEnv {
		t.Fatalf("config percent got %d fromEnv=%v", n, fromEnv)
	}
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "10")
	n, fromEnv = ResolveSharedLoopPercent(cfg)
	if n != 10 || !fromEnv {
		t.Fatalf("env should win got %d fromEnv=%v", n, fromEnv)
	}
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "")
	zero := 0
	cfg.SharedAgentLoopCanaryPercent = &zero
	n, _ = ResolveSharedLoopPercent(cfg)
	if n != 0 {
		t.Fatalf("explicit 0 canary got %d", n)
	}
}

func TestResolveSharedLoopWorkflowPilot_ConfigAndEnv(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_WORKFLOW", "")
	on, fromEnv := ResolveSharedLoopWorkflowPilot(corelib.AppConfig{SharedAgentLoopWorkflow: true})
	if !on || fromEnv {
		t.Fatalf("config workflow got on=%v fromEnv=%v", on, fromEnv)
	}
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_WORKFLOW", "off")
	on, fromEnv = ResolveSharedLoopWorkflowPilot(corelib.AppConfig{SharedAgentLoopWorkflow: true})
	if on || !fromEnv {
		t.Fatalf("env off should win got on=%v fromEnv=%v", on, fromEnv)
	}
}

func TestSharedLoopPercentFromEnv(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "25")
	if sharedLoopPercentFromEnv() != 25 {
		t.Fatal("25")
	}
	n, fromEnv := SharedLoopPercentFromEnv()
	if n != 25 || !fromEnv {
		t.Fatalf("exported 25 fromEnv got %d %v", n, fromEnv)
	}
}

func TestFormatSharedLoopLine_ShadowShowsCanary(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "shadow")
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "25")
	env := ResolveSharedLoopEnv(corelib.AppConfig{})
	line := FormatSharedLoopLine(env)
	if !strings.Contains(line, "shadow") || !strings.Contains(line, "canary 25%") {
		t.Fatalf("shadow canary line=%q", line)
	}
}

func TestSharedLoopCanaryAllows_StickyAndBounds(t *testing.T) {
	if !SharedLoopCanaryAllows("anyone", 100) {
		t.Fatal("100 always allows")
	}
	if SharedLoopCanaryAllows("anyone", 0) {
		t.Fatal("0 never allows")
	}
	if !SharedLoopCanaryAllows("", 50) {
		t.Fatal("empty user allows when percent>0")
	}
	a1 := SharedLoopCanaryAllows("sticky-user-xyz", 50)
	a2 := SharedLoopCanaryAllows("sticky-user-xyz", 50)
	if a1 != a2 {
		t.Fatal("must be sticky")
	}
	b := SharedLoopCanaryBucket("sticky-user-xyz")
	if b < 0 || b > 99 {
		t.Fatalf("bucket=%d", b)
	}
	// Bucket must match allows at threshold boundary.
	if SharedLoopCanaryAllows("sticky-user-xyz", b) {
		// allows when bucket < percent; at percent==bucket should be false
	}
	if SharedLoopCanaryAllows("sticky-user-xyz", b) {
		t.Fatalf("bucket %d should not allow at percent=%d", b, b)
	}
	if !SharedLoopCanaryAllows("sticky-user-xyz", b+1) {
		t.Fatalf("bucket %d should allow at percent=%d", b, b+1)
	}
	p := PreviewSharedLoopCanary("sticky-user-xyz", 50)
	if p.UserID != "sticky-user-xyz" || p.Percent != 50 || p.Bucket != b {
		t.Fatalf("%+v", p)
	}
	if p.Allows != SharedLoopCanaryAllows("sticky-user-xyz", 50) {
		t.Fatalf("preview allows mismatch %+v", p)
	}
}

func TestFormatSharedLoopLine(t *testing.T) {
	line := FormatSharedLoopLine(SharedLoopEnv{
		Mode:          "on",
		Percent:       25,
		ConfigEnabled: true,
	})
	if line != "shared-loop: on (canary 25%; config enabled)" {
		t.Fatalf("got %q", line)
	}
	locked := FormatSharedLoopLine(SharedLoopEnv{
		Mode:        "shadow",
		Percent:     100,
		EnvOverride: "shadow",
	})
	if locked != "shared-loop: shadow (env MACLAW_SHARED_AGENT_LOOP locks mode)" {
		t.Fatalf("got %q", locked)
	}
}
