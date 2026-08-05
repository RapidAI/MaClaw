package flash

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

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
