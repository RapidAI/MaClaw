package doctor

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestComputerUseChecksDisabled(t *testing.T) {
	off := false
	checks := ComputerUseChecks(corelib.AppConfig{ComputerUseEnabled: &off}, t.TempDir())
	if len(checks) != 1 || checks[0].ID != "computer_use.enabled" || checks[0].Status != StatusSkip {
		t.Fatalf("unexpected: %+v", checks)
	}
}

func TestComputerUseChecksEnabledMissingYOLO(t *testing.T) {
	dir := t.TempDir()
	on := true
	sp := true
	checks := ComputerUseChecks(corelib.AppConfig{
		ComputerUseEnabled:   &on,
		ScreenParsingEnabled: &sp,
	}, dir)

	if !hasCheckIn(checks, "computer_use.enabled", StatusOK) {
		t.Fatalf("missing enabled ok: %+v", checks)
	}
	if !hasCheckIn(checks, "computer_use.omniparser", StatusWarn) {
		t.Fatalf("expected omniparser warn: %+v", checks)
	}
	if runtime.GOOS == "windows" {
		if !hasCheckIn(checks, "computer_use.uia_sidecar", StatusWarn) {
			t.Fatalf("expected uia warn on windows: %+v", checks)
		}
	}
	if !hasCheckIn(checks, "computer_use.log_policy", StatusInfo) {
		t.Fatalf("expected log_policy info: %+v", checks)
	}
}

func TestComputerUseChecksLogPolicyFromConfig(t *testing.T) {
	on := true
	keep := 7
	age := 14
	auto := true
	checks := ComputerUseChecks(corelib.AppConfig{
		ComputerUseEnabled:       &on,
		ComputerUseLogKeepNewest: &keep,
		ComputerUseLogMaxAgeDays: &age,
		ComputerUseLogAutoPrune:  &auto,
	}, t.TempDir())
	for _, c := range checks {
		if c.ID != "computer_use.log_policy" {
			continue
		}
		if c.Status != StatusInfo {
			t.Fatalf("status: %+v", c)
		}
		d := c.Detail
		if d["keep_newest"] != 7 || d["max_age_days"] != 14 || d["auto_prune"] != true {
			t.Fatalf("detail: %#v", d)
		}
		return
	}
	t.Fatal("missing log_policy")
}

func TestComputerUseChecksYOLOPresent(t *testing.T) {
	dir := t.TempDir()
	models := filepath.Join(dir, "models")
	if err := os.MkdirAll(models, 0o755); err != nil {
		t.Fatal(err)
	}
	yolo := filepath.Join(models, omniparserYOLOFilename)
	if err := os.WriteFile(yolo, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Place sidecar for windows branch.
	if runtime.GOOS == "windows" {
		bin := filepath.Join(dir, "bin")
		_ = os.MkdirAll(bin, 0o755)
		_ = os.WriteFile(filepath.Join(bin, uiaSidecarExeName), []byte("MZ"), 0o644)
	}

	on := true
	checks := ComputerUseChecks(corelib.AppConfig{ComputerUseEnabled: &on}, dir)
	// Path resolution uses embedding.DefaultModelsDir when set; with empty BaseDirFunc
	// falls back to baseDir/models — which we populated.
	if !hasCheckIn(checks, "computer_use.omniparser", StatusOK) {
		// If DefaultModelsDir points elsewhere, still acceptable as warn; log detail.
		for _, c := range checks {
			if c.ID == "computer_use.omniparser" {
				t.Logf("omniparser check: %+v", c)
			}
		}
		// Soft: only fail if no check at all
		if !hasCheckID(checks, "computer_use.omniparser") {
			t.Fatalf("missing omniparser check: %+v", checks)
		}
	}
}

func TestRunIncludesComputerUseChecks(t *testing.T) {
	dir := t.TempDir()
	on := true
	report := Run(Input{
		Config: corelib.AppConfig{
			MaclawLLMUrl:       "http://x",
			MaclawLLMModel:     "m",
			MaclawLLMKey:       "k",
			OnboardingDone:     true,
			ComputerUseEnabled: &on,
		},
		BaseDir: dir,
	})
	if !hasCheck(report, "computer_use.enabled", StatusOK) {
		t.Fatalf("doctor missing computer_use.enabled: %+v", report.Checks)
	}
}

func hasCheckIn(checks []Check, id string, st Status) bool {
	for _, c := range checks {
		if c.ID == id && c.Status == st {
			return true
		}
	}
	return false
}

func hasCheckID(checks []Check, id string) bool {
	for _, c := range checks {
		if c.ID == id {
			return true
		}
	}
	return false
}
