package firmware

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyArchive(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "ok.clawfw")
	contents := []byte("firmware bytes")
	sum := sha256.Sum256(contents)
	manifest := fmt.Sprintf(`{"schemaVersion":1,"packageId":"bread-v1","releaseVersion":"1","mode":"app-only","files":[{"path":"images/app.bin","size":%d,"sha256":"sha256:%s","offset":65536,"region":"app"}]}`, len(contents), hex.EncodeToString(sum[:]))
	makeZip(t, archive, map[string]string{"manifest.json": manifest, "images/app.bin": string(contents)})
	v, err := Verify(archive)
	if err != nil {
		t.Fatal(err)
	}
	if v.Manifest.PackageID != "bread-v1" || v.ArchiveSHA256 == "" {
		t.Fatalf("unexpected: %#v", v)
	}
}
func TestVerifyReleaseRequiresValidSignature(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "signed.clawfw")
	contents := []byte("firmware bytes")
	sum := sha256.Sum256(contents)
	manifest := fmt.Sprintf(`{"schemaVersion":1,"packageId":"bread-v1","releaseVersion":"1","mode":"app-only","files":[{"path":"images/app.bin","size":%d,"sha256":"sha256:%s","offset":65536,"region":"app"}]}`, len(contents), hex.EncodeToString(sum[:]))
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(manifest)))
	makeZip(t, archive, map[string]string{"manifest.json": manifest, "manifest.sig": fmt.Sprintf(`{"algorithm":"ed25519","keyId":"test","signature":"%s"}`, signature), "images/app.bin": string(contents)})
	if _, err := VerifyRelease(archive, TrustStore{"test": pub}); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyRelease(archive, TrustStore{}); err == nil {
		t.Fatal("expected untrusted key rejection")
	}
}
func TestVerifyRejectsTraversalAndUnlisted(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "bad.clawfw")
	makeZip(t, archive, map[string]string{"manifest.json": `{"schemaVersion":1,"packageId":"p","files":[{"path":"x.bin","size":1,"sha256":"sha256:00"}]}`, "../x.bin": "x"})
	if _, err := Verify(archive); err == nil {
		t.Fatal("expected traversal rejection")
	}
}

func TestVerifyRejectsAmbiguousFlashFileSpec(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "bad-spec.clawfw")
	contents := []byte("firmware bytes")
	sum := sha256.Sum256(contents)
	manifest := fmt.Sprintf(`{"schemaVersion":1,"packageId":"bread-v1","files":[{"path":"images/app.bin","size":%d,"sha256":"sha256:%s","offset":1,"region":"app"}]}`, len(contents), hex.EncodeToString(sum[:]))
	makeZip(t, archive, map[string]string{"manifest.json": manifest, "images/app.bin": string(contents)})
	if _, err := Verify(archive); err == nil {
		t.Fatal("unaligned flash image offset accepted")
	}
}

func TestVerifyRejectsOversizedFileSpec(t *testing.T) {
	offset := uint64(0x10000)
	if err := validateFileSpec(FileSpec{Path: "images/app.bin", Size: MaxFileBytes + 1, Offset: &offset, Region: "app"}); err == nil {
		t.Fatal("oversized image specification was accepted")
	}
}
func TestInstallPlanValidatesModeAndDataImpact(t *testing.T) {
	appOffset := uint64(0x10000)
	appOnly, err := InstallPlanFor(Manifest{Mode: ModeAppOnly, Files: []FileSpec{{Path: "images/app.bin", Region: "app", Offset: &appOffset}}})
	if err != nil || !appOnly.PreservesUserData || !appOnly.RequiresRecovery {
		t.Fatalf("plan=%+v err=%v", appOnly, err)
	}
	if _, err := InstallPlanFor(Manifest{Mode: ModeAppOnly, Files: []FileSpec{{Path: "images/boot.bin", Region: "bootloader", Offset: &appOffset}}}); err == nil {
		t.Fatal("app-only non-app image accepted")
	}
	if _, err := InstallPlanFor(Manifest{Mode: ModeAppOnly, Files: []FileSpec{{Path: "images/app.bin", Region: "app"}}}); err == nil {
		t.Fatal("app-only image without offset accepted")
	}
	fullOffset := uint64(0)
	full, err := InstallPlanFor(Manifest{Mode: ModeFull, Files: []FileSpec{{Path: "images/full-flash.bin", Region: "flash", Offset: &fullOffset}}})
	if err != nil || full.PreservesUserData || !full.RequiresRecovery {
		t.Fatalf("plan=%+v err=%v", full, err)
	}
	if _, err := InstallPlanFor(Manifest{Mode: ModeFull, Files: []FileSpec{{Path: "images/full-flash.bin", Region: "flash", Offset: &appOffset}}}); err == nil {
		t.Fatal("full image with non-zero offset accepted")
	}
	if _, err := InstallPlanFor(Manifest{Mode: "factory-reset"}); err == nil {
		t.Fatal("unknown mode accepted")
	}
}
func makeZip(t *testing.T, name string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for n, v := range entries {
		w, err := z.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(v)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
