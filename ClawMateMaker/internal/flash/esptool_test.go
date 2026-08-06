package flash

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestManagedToolRequiresMatchingHash(t *testing.T) {
	binName := "esptool"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	dir := filepath.Join(t.TempDir(), "tools")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, binName)
	contents := []byte("sidecar")
	if err := os.WriteFile(binary, contents, 0700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(contents)
	manifest, err := json.Marshal(sidecarManifest{SchemaVersion: 1, Tools: []sidecarRecord{{Name: "esptool", Path: binName, SHA256: "sha256:" + hex.EncodeToString(sum[:]), Version: "5.3.1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sidecar-manifest.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	tool, err := managedTool(filepath.Join(filepath.Dir(dir), "ClawMateMaker"))
	if err != nil || tool.Path != binary {
		t.Fatalf("tool=%+v err=%v", tool, err)
	}
	if err := os.WriteFile(binary, []byte("modified"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := managedTool(filepath.Join(filepath.Dir(dir), "ClawMateMaker")); err == nil {
		t.Fatal("tampered sidecar accepted")
	}
}

func TestOfficialManagedToolRequiresValidManifestSignature(t *testing.T) {
	binName := "esptool"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	dir := filepath.Join(t.TempDir(), "tools")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, binName)
	contents := []byte("signed-sidecar")
	if err := os.WriteFile(binary, contents, 0700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(contents)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := sidecarManifest{SchemaVersion: 1, Tools: []sidecarRecord{{Name: "esptool", Path: binName, SHA256: "sha256:" + hex.EncodeToString(sum[:]), Version: "5.3.1"}}}
	payload, err := sidecarManifestPayload(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Signature = &sidecarSignature{Algorithm: "ed25519", KeyID: "release-test", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload))}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sidecar-manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	config := sidecarConfig{executable: filepath.Join(filepath.Dir(dir), "ClawMateMaker"), production: true, keyID: "release-test", publicKeyBase64: base64.StdEncoding.EncodeToString(pub)}
	if tool, err := managedToolForConfig(config); err != nil || tool.Path != binary {
		t.Fatalf("tool=%+v err=%v", tool, err)
	}
	manifest.Tools[0].Version = "modified"
	raw, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sidecar-manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := managedToolForConfig(config); err == nil {
		t.Fatal("tampered manifest accepted by official build")
	}
}

func TestPortValidation(t *testing.T) {
	for _, p := range []string{"COM4", "/dev/ttyUSB0", "/dev/cu.usbmodem1"} {
		if !validPort(p) {
			t.Fatalf("valid port rejected: %s", p)
		}
	}
	for _, p := range []string{"COM4; erase_flash", "../../x", "COM0"} {
		if validPort(p) {
			t.Fatalf("unsafe port accepted: %s", p)
		}
	}
}
func TestParseFlashID(t *testing.T) {
	f := ParseFlashID("Manufacturer: 20\nDevice: 4019\nDetected flash size: 16MB")
	if f.SizeBytes != 16*1024*1024 {
		t.Fatalf("size: %d", f.SizeBytes)
	}
}
func TestParseChipIDCapturesMACOnlyForInMemoryBinding(t *testing.T) {
	chip := ParseChipID("Chip type: ESP32-S3\nMAC: b4:3a:45:a1:e5:84")
	if chip.MAC != "b4:3a:45:a1:e5:84" {
		t.Fatalf("MAC = %q", chip.MAC)
	}
}
func TestWritePlanRejectsInvalidOrOverlappingImages(t *testing.T) {
	p := filepath.Join(t.TempDir(), "app.bin")
	if err := os.WriteFile(p, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	tool := Tool{}
	if _, err := tool.WriteImages(context.Background(), "COM4", 115200, []WriteImage{{Offset: 0x1000, Path: p, Size: 1}, {Offset: 0x1000, Path: p, Size: 1}}); err == nil {
		t.Fatal("expected overlap rejection")
	}
}
func TestSupportedWriteBaudsAreHighToLowAndValidated(t *testing.T) {
	want := []int{921600, 460800, 115200}
	if len(SupportedWriteBauds) != len(want) {
		t.Fatalf("bauds=%v", SupportedWriteBauds)
	}
	for i, baud := range want {
		if SupportedWriteBauds[i] != baud || !validWriteBaud(baud) {
			t.Fatalf("bauds=%v", SupportedWriteBauds)
		}
	}
	if validWriteBaud(74880) {
		t.Fatal("unsupported baud accepted")
	}
}
func TestLowerBaudFallbackRequiresProvenPreWriteFailure(t *testing.T) {
	if !CanRetryWriteAtLowerBaud(Result{Output: "Failed to connect to ESP32-S3: No serial data received."}, errors.New("esptool write_flash: exit status 2")) {
		t.Fatal("ROM sync failure should permit a lower-baud retry")
	}
	if CanRetryWriteAtLowerBaud(Result{Output: "Writing at 0x00010000...\nA fatal error occurred: Write timeout"}, errors.New("esptool write_flash: exit status 2")) {
		t.Fatal("a possibly partial write must not permit automatic retry")
	}
}
func TestVerifyPlanRejectsInvalidImage(t *testing.T) {
	tool := Tool{}
	if _, err := tool.VerifyImages(context.Background(), "COM4", 115200, []WriteImage{{Offset: 1, Path: "x", Size: 1}}); err == nil {
		t.Fatal("expected invalid alignment rejection")
	}
}
func TestParseSecurityInfoFailsClosedForUnknownValues(t *testing.T) {
	s := ParseSecurityInfo("Secure Boot: Disabled\nFlash Encryption: Disabled\nSecure version: 0")
	if s.SecureBoot == nil || *s.SecureBoot || s.FlashEncryption == nil || *s.FlashEncryption || s.SecureVersion == nil || *s.SecureVersion != 0 {
		t.Fatalf("bad parse: %#v", s)
	}
	unknown := ParseSecurityInfo("no details")
	if unknown.SecureBoot != nil || unknown.FlashEncryption != nil || unknown.SecureVersion != nil {
		t.Fatal("unknown must remain unknown")
	}
}

func TestParseSecurityInfoTreatsNotEnabledAsDisabled(t *testing.T) {
	s := ParseSecurityInfo("Secure Boot: Not enabled\nFlash Encryption: Not enabled\nSecure version: 0")
	if s.SecureBoot == nil || *s.SecureBoot || s.FlashEncryption == nil || *s.FlashEncryption || s.SecureVersion == nil || *s.SecureVersion != 0 {
		t.Fatalf("not-enabled baseline parsed incorrectly: %#v", s)
	}
}
func TestParseSecurityInfoRecognizesESPTargetZeroFlagsBaseline(t *testing.T) {
	s := ParseSecurityInfo("Security Information:\nFlags: 0x00000000 (0b0)\nSecure Boot: Disabled\nFlash Encryption: Disabled")
	if s.SecureVersion == nil || *s.SecureVersion != 0 {
		t.Fatalf("zero eFuse flags must establish baseline secure version: %#v", s)
	}
	unknown := ParseSecurityInfo("Flags: 0x00000001\nSecure Boot: Disabled\nFlash Encryption: Disabled")
	if unknown.SecureVersion != nil {
		t.Fatal("non-zero eFuse flags must remain unknown and fail closed")
	}
}

func TestRunWithCancellationTerminatesSidecar(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("covered by the Windows build; this test uses a POSIX sleep helper")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.Command("sh", "-c", "sleep 30")
	if err := prepareProcessTree(cmd); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- runWithCancellation(ctx, cmd) }()
	deadline := time.Now().Add(2 * time.Second)
	for cmd.Process == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if cmd.Process == nil {
		t.Fatal("sidecar did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("cancellation error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("sidecar cancellation did not complete")
	}
}
