package jobs

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"clawmatemaker/internal/flash"
	"clawmatemaker/internal/partition"
)

func TestProbeResultSerializesReadOnlyDiagnosticEvidence(t *testing.T) {
	secureBoot, encryption := false, false
	secureVersion := 0
	result := ProbeResult{
		Security:       flash.SecurityInfo{SecureBoot: &secureBoot, FlashEncryption: &encryption, SecureVersion: &secureVersion, Raw: "private raw tool output"},
		Layout:         &partition.Table{Fingerprint: "sha256:layout", Entries: []partition.Entry{{Label: "factory", Offset: 0x10000, Size: 0x100000}}},
		AppDescription: &flash.ESPAppDescription{ProjectName: "clawmate", Version: "v1", ELFSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"secureBoot":false`, `"fingerprint":"sha256:layout"`, `"projectName":"clawmate"`, `"elfSha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"`} {
		if !strings.Contains(string(raw), expected) {
			t.Fatalf("probe result missing %s: %s", expected, raw)
		}
	}
	if strings.Contains(string(raw), "private raw tool output") {
		t.Fatalf("probe result exposed raw tool output: %s", raw)
	}
}

func TestDeviceBindingIsStableAndDoesNotExposeROMMAC(t *testing.T) {
	const mac = "B4:3A:45:A1:E5:84"
	const expected = "3d7db9c93eb0c7c08860de80f196319679595ade1f572454d659f83d84f02556"
	if got := deviceBinding(mac); got != expected {
		t.Fatalf("device binding = %q, want %q", got, expected)
	}
	result := ProbeResult{Chip: flash.ChipInfo{MAC: mac}, DeviceBinding: deviceBinding(mac)}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), mac) || strings.Contains(string(raw), strings.ToLower(mac)) {
		t.Fatalf("probe result exposed ROM MAC: %s", raw)
	}
	if !strings.Contains(string(raw), `"deviceBinding":"`+expected+`"`) {
		t.Fatalf("probe result omitted device binding: %s", raw)
	}
}

func TestProbeMissingToolIsReported(t *testing.T) {
	t.Setenv("CLAWMATE_ESPTOOL", "C:\\no-such-esptool.exe")
	t.Setenv("PATH", t.TempDir())
	j, err := NewProbeJob(t.TempDir(), "COM4", nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := j.Run(context.Background())
	if err == nil || r.ErrorCode != "TOOL_UNAVAILABLE" {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}

func TestReadOnlyProbeMetadataParsersSupportLayoutAndAppDescription(t *testing.T) {
	table, err := partition.Parse(testPartitionTable(t), 16*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if table.Fingerprint == "" {
		t.Fatal("partition fingerprint missing")
	}
	description, err := flash.ParseESPAppDescription(testAppDescription(t))
	if err != nil {
		t.Fatal(err)
	}
	if description.ProjectName != "maclaw_esp32s3_client" {
		t.Fatalf("project=%q", description.ProjectName)
	}
}

func TestSecurityBaselineRequiresKnownDisabledState(t *testing.T) {
	baseline := false
	version := 0
	if !securityBaseline(flash.SecurityInfo{SecureBoot: &baseline, FlashEncryption: &baseline, SecureVersion: &version}) {
		t.Fatal("known baseline was rejected")
	}
	if securityBaseline(flash.SecurityInfo{SecureBoot: &baseline, FlashEncryption: &baseline}) {
		t.Fatal("unknown secure version was accepted")
	}
	enabled := true
	if securityBaseline(flash.SecurityInfo{SecureBoot: &enabled, FlashEncryption: &baseline, SecureVersion: &version}) {
		t.Fatal("enabled secure boot was accepted")
	}
}

func TestWaitForApplicationIdentityHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForApplicationIdentity(ctx, time.Second); err != context.Canceled {
		t.Fatalf("cancelled identity wait error = %v", err)
	}
}

func TestWaitForApplicationIdentityWaitsForRequestedDelay(t *testing.T) {
	started := time.Now()
	if err := waitForApplicationIdentity(context.Background(), 15*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 10*time.Millisecond {
		t.Fatalf("identity stabilization wait was too short: %s", elapsed)
	}
}

func testPartitionTable(t *testing.T) []byte {
	t.Helper()
	raw, err := partition.Encode([]partition.Entry{{Type: 1, Subtype: 2, Offset: 0x9000, Size: 0x6000, Label: "nvs"}, {Type: 0, Subtype: 0, Offset: 0x10000, Size: 0x3a0000, Label: "factory"}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func testAppDescription(t *testing.T) []byte {
	t.Helper()
	raw := make([]byte, 4096)
	raw[0x20], raw[0x21], raw[0x22], raw[0x23] = 0x32, 0x54, 0xcd, 0xab
	copy(raw[0x30:0x50], "V6.6.3")
	copy(raw[0x50:0x70], "maclaw_esp32s3_client")
	return raw
}
