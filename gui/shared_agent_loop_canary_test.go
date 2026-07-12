package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/doctor"
)

func TestPreviewSharedLoopCanary_Binding(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "50")
	app := &App{}
	out := app.PreviewSharedLoopCanary("sticky-user-xyz", -1)
	if out["ok"] != true {
		t.Fatalf("%#v", out)
	}
	if out["user_id"] != "sticky-user-xyz" {
		t.Fatalf("%#v", out)
	}
	if out["percent"] != 50 {
		t.Fatalf("percent=%v", out["percent"])
	}
	bucket, ok := out["bucket"].(int)
	if !ok {
		t.Fatalf("bucket type %#v", out["bucket"])
	}
	if bucket != doctor.SharedLoopCanaryBucket("sticky-user-xyz") {
		t.Fatalf("bucket mismatch %d", bucket)
	}
	allows, ok := out["allows"].(bool)
	if !ok {
		t.Fatalf("allows type %#v", out["allows"])
	}
	if allows != doctor.SharedLoopCanaryAllows("sticky-user-xyz", 50) {
		t.Fatalf("allows mismatch %v", allows)
	}
	sum := fmt.Sprint(out["summary"])
	if !strings.Contains(sum, "canary user=") {
		t.Fatalf("summary=%q", sum)
	}
}

// TestPreviewSharedLoopCanary_UsesConfigWhenEnvUnset ensures System Doctor
// percent=-1 matches runtime (env > config > 100), not env-only default.
func TestPreviewSharedLoopCanary_UsesConfigWhenEnvUnset(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "")
	corelib.SetMaclawBaseDir(tempHome)
	t.Cleanup(func() { corelib.SetMaclawBaseDir("") })

	pct := 35
	app := &App{testHomeDir: tempHome}
	if _, err := app.PatchConfigIfChanged(func(cfg *corelib.AppConfig) bool {
		cfg.SharedAgentLoopCanaryPercent = &pct
		cfg.SharedAgentLoopMigrated = true
		return true
	}); err != nil {
		t.Fatal(err)
	}

	out := app.PreviewSharedLoopCanary("sticky-user-xyz", -1)
	if out["percent"] != 35 {
		t.Fatalf("expected config percent 35, got %#v (status=%#v)", out["percent"], app.GetSharedAgentLoopStatus().Percent)
	}
	wantAllows := doctor.SharedLoopCanaryAllows("sticky-user-xyz", 35)
	if out["allows"] != wantAllows {
		t.Fatalf("allows=%v want %v", out["allows"], wantAllows)
	}
}

func TestSharedLoopCanaryAllows_MatchesDoctor(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "35")
	for _, uid := range []string{"", "a", "user-42", "sticky-user-xyz"} {
		if sharedLoopCanaryAllows(uid) != doctor.SharedLoopCanaryAllows(uid, 35) {
			t.Fatalf("mismatch for %q", uid)
		}
	}
}
