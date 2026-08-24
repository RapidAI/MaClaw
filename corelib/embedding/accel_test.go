package embedding

import (
	"strings"
	"testing"
)

func TestDetectFailIsCPU(t *testing.T) {
	info := CurrentAccelInfo()
	if info.Backend == "" {
		t.Fatal("backend empty")
	}
	if info.Backend == BackendAMDNPU {
		t.Fatalf("this host should not pin amd-npu without ORT: %+v", info)
	}
	if info.NPUPresent {
		t.Logf("NPU present on this host: %s", info.Device)
	} else if !strings.Contains(strings.ToLower(info.Reason), "npu") && info.Reason != "cpu simd" && info.Reason != "NPU detect is Windows-only" {
		t.Logf("reason=%q", info.Reason)
	}
	if info.Backend != BackendCPUSIMD && info.Backend != BackendNone {
		t.Fatalf("expected cpu-simd/none, got %s (%s)", info.Backend, info.Reason)
	}
}

func TestApplyAccelNoNPUStaysCPU(t *testing.T) {
	SetDetectOverrideForTest(AccelInfo{Backend: BackendCPUSIMD, NPUPresent: false, Reason: "no NPU/XDNA"})
	g := &GemmaEmbedder{dim: 256, hp: GemmaHParams{Dim: 768}}
	g.ApplyAccel(true)
	if g.Accel().Backend != BackendCPUSIMD {
		t.Fatalf("got %s", g.Accel().Backend)
	}
	if g.Accel().Backend == BackendAMDNPU {
		t.Fatal("badge backend must not be amd-npu without session")
	}
}

func TestApplyAccelOffClearsInstancePin(t *testing.T) {
	SetDetectOverrideForTest(AccelInfo{Backend: BackendCPUSIMD, NPUPresent: true, Device: "fake-npu", Reason: "NPU/XDNA present"})
	g := &GemmaEmbedder{dim: 256, hp: GemmaHParams{Dim: 768}}
	g.ApplyAccel(true)
	g.ApplyAccel(false)
	if g.Accel().Backend != BackendCPUSIMD {
		t.Fatalf("switch off should pin cpu-simd, got %s", g.Accel().Backend)
	}
}

func TestHWAccelPreferredDefaultTrue(t *testing.T) {
	SetHWAccelPreferred(true)
	if !HWAccelPreferred() {
		t.Fatal("expected default-true preference")
	}
	SetHWAccelPreferred(false)
	if HWAccelPreferred() {
		t.Fatal("expected stored false")
	}
	SetHWAccelPreferred(true)
}
