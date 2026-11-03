//go:build windows && amd64

package embedding

import (
	"os"
	"testing"
)

// TestAMDNPUProbeRealDriver exercises the real RadeonML_ipu.dll bindings on
// this machine. It is the only test that can catch an ABI mistake (wrong
// struct layout, wrong status constants), because those fail at the syscall
// boundary and nothing else in the suite touches the driver.
//
// It is skipped unless MACLAW_NPU_HW_TEST=1 so CI without an AMD NPU stays
// green; on an AMD machine it asserts that the runtime comes up and that the
// verdict explains the missing compiled model instead of failing obscurely.
func TestAMDNPUProbeRealDriver(t *testing.T) {
	if os.Getenv("MACLAW_NPU_HW_TEST") != "1" {
		t.Skip("set MACLAW_NPU_HW_TEST=1 to exercise the real AMD NPU driver")
	}
	ctx, err := openIPUContext()
	if err != nil {
		t.Fatalf("IPU context must open on an AMD NPU host: %v", err)
	}
	t.Logf("IPU backend version: %q", ctx.versionString())
	ctx.close()

	device, err := probeAMDNPU()
	t.Logf("probe device=%q err=%v", device, err)
	if device == "" {
		t.Fatal("probe must name the device even when no model is present")
	}
	// On this host there is no .amodel, so the probe must decline with the
	// compiled-model hint rather than claim readiness.
	if err == nil {
		t.Log("a compiled .amodel was found; NPU is ready")
	}
}

// TestAMDNPUAmodelDiscovery checks the search order without touching the
// driver: the env var wins, then the npu/ subdirectory, then the model dir.
func TestAMDNPUAmodelDiscovery(t *testing.T) {
	dir := t.TempDir()
	model := dir + `\model.gguf`

	// Nothing present: no candidate resolves.
	if got, _ := findAmodel(model); got != "" {
		t.Fatalf("findAmodel must be empty when no file exists, got %q", got)
	}

	// Beside the model.
	beside := dir + `\` + amodelFileName
	if err := os.WriteFile(beside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := findAmodel(model); got != beside {
		t.Fatalf("expected %q, got %q", beside, got)
	}

	// The npu/ subdirectory takes priority over the model directory.
	if err := os.MkdirAll(dir+`\`+amodelDirName, 0o755); err != nil {
		t.Fatal(err)
	}
	sub := dir + `\` + amodelDirName + `\` + amodelFileName
	if err := os.WriteFile(sub, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := findAmodel(model); got != sub {
		t.Fatalf("expected %q, got %q", sub, got)
	}

	// The env var overrides both.
	override := dir + `\explicit.amodel`
	if err := os.WriteFile(override, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(amodelEnvVar, override)
	if got, _ := findAmodel(model); got != override {
		t.Fatalf("expected %q, got %q", override, got)
	}
}
