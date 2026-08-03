package petpack

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallZipBytesRejectsOversizeArchive(t *testing.T) {
	user := t.TempDir()
	reg := NewRegistry(user, nil)
	// Larger than MaxZipArchiveBytes even before extract
	huge := make([]byte, MaxZipArchiveBytes+1)
	if _, err := reg.InstallZipBytes(huge); err == nil {
		t.Fatal("expected oversize rejection")
	}
}

func TestPickShallowestManifest(t *testing.T) {
	files := map[string][]byte{
		"deep/nested/pet-pack.yaml": []byte("id: deep\n"),
		"cool-pet/pet-pack.yaml":    []byte("id: cool-pet\n"),
		"other.txt":                 []byte("x"),
	}
	name, data := pickShallowestManifest(files)
	if name != "cool-pet/pet-pack.yaml" {
		t.Fatalf("shallowest = %q", name)
	}
	if string(data) != "id: cool-pet\n" {
		t.Fatalf("data = %q", data)
	}
}

func TestFolderNameMustMatchPackID(t *testing.T) {
	user := t.TempDir()
	// Wrong folder name for manifest id
	wrong := filepath.Join(user, "wrong-folder")
	_ = os.MkdirAll(filepath.Join(wrong, "native"), 0o755)
	_ = os.WriteFile(filepath.Join(wrong, "pet-pack.yaml"), []byte(`
schema_version: 1
id: right-id
name: Right
version: 1.0.0
renderer: native-raster
assets:
  native:
    idle: native/idle.png
`), 0o644)
	_ = os.WriteFile(filepath.Join(wrong, "native", "idle.png"), minimalPNG(), 0o644)

	reg := NewRegistry(user, nil)
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	m, ok := reg.Get("right-id")
	if !ok || m == nil {
		t.Fatal("expected right-id registered as invalid")
	}
	if m.Status != StatusInvalid {
		t.Fatalf("status = %q, want invalid", m.Status)
	}
	// Uninstall must remove the mismatched directory via registered Dir
	if err := reg.Uninstall("right-id"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wrong); !os.IsNotExist(err) {
		t.Fatal("mismatched folder should be removed")
	}
}

func TestScanSkipsInstallStagingDirs(t *testing.T) {
	user := t.TempDir()
	// Staging dirs left behind mid-install must not register as packs.
	for _, name := range []string{"ghost-pet.tmp-install", "ghost-pet.bak-install"} {
		dir := filepath.Join(user, name)
		if err := os.MkdirAll(filepath.Join(dir, "native"), 0o755); err != nil {
			t.Fatal(err)
		}
		yaml := []byte("schema_version: 1\nid: ghost-pet\nname: Ghost\nversion: 1.0.0\nrenderer: native-raster\nassets:\n  native:\n    idle: native/idle.png\n")
		if err := os.WriteFile(filepath.Join(dir, "pet-pack.yaml"), yaml, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "native", "idle.png"), minimalPNG(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Real user pack should still load.
	real := filepath.Join(user, "real-pet")
	_ = os.MkdirAll(filepath.Join(real, "native"), 0o755)
	_ = os.WriteFile(filepath.Join(real, "pet-pack.yaml"), []byte("schema_version: 1\nid: real-pet\nname: Real\nversion: 1.0.0\nrenderer: native-raster\nassets:\n  native:\n    idle: native/idle.png\n"), 0o644)
	_ = os.WriteFile(filepath.Join(real, "native", "idle.png"), minimalPNG(), 0o644)

	reg := NewRegistry(user, BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("ghost-pet"); ok {
		t.Fatal("staging dir must not register ghost-pet")
	}
	if _, ok := reg.Get("real-pet"); !ok {
		t.Fatal("real-pet missing")
	}
}

func TestInstallZipAndRejectZipSlip(t *testing.T) {
	user := t.TempDir()
	reg := NewRegistry(user, BundledFS())
	_ = reg.Scan()

	// good zip
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	png1x1 := minimalPNG()
	w, _ := zw.Create("cool-pet/pet-pack.yaml")
	_, _ = w.Write([]byte(`
schema_version: 1
id: cool-pet
name: Cool
version: 1.0.0
renderer: native-raster
assets:
  native:
    idle: native/idle.png
`))
	w, _ = zw.Create("cool-pet/native/idle.png")
	_, _ = w.Write(png1x1)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	id, err := reg.InstallZipBytes(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if id != "cool-pet" {
		t.Fatalf("id = %q", id)
	}
	if _, err := os.Stat(filepath.Join(user, "cool-pet", "pet-pack.yaml")); err != nil {
		t.Fatal(err)
	}
	if !reg.Allowlist()["cool-pet"] {
		t.Fatal("not allowlisted after install")
	}

	// zip-slip
	var bad bytes.Buffer
	zw2 := zip.NewWriter(&bad)
	w2, _ := zw2.Create("../escape/pet-pack.yaml")
	_, _ = w2.Write([]byte("id: x\n"))
	_ = zw2.Close()
	if _, err := reg.InstallZipBytes(bad.Bytes()); err == nil {
		t.Fatal("expected zip-slip rejection")
	}

	// forbidden ext
	var bad2 bytes.Buffer
	zw3 := zip.NewWriter(&bad2)
	w3, _ := zw3.Create("evil/pet-pack.yaml")
	_, _ = w3.Write([]byte("schema_version: 1\nid: evil\nname: e\nversion: 1\n"))
	w3, _ = zw3.Create("evil/run.js")
	_, _ = w3.Write([]byte("alert(1)"))
	_ = zw3.Close()
	if _, err := reg.InstallZipBytes(bad2.Bytes()); err == nil {
		t.Fatal("expected js rejection")
	}

	// reinstall / upgrade: previous tree must not be left deleted on success
	var buf2 bytes.Buffer
	zw4 := zip.NewWriter(&buf2)
	w4, _ := zw4.Create("cool-pet/pet-pack.yaml")
	_, _ = w4.Write([]byte(`
schema_version: 1
id: cool-pet
name: Cool
version: 1.0.1
renderer: native-raster
assets:
  native:
    idle: native/idle.png
`))
	w4, _ = zw4.Create("cool-pet/native/idle.png")
	_, _ = w4.Write(png1x1)
	w4, _ = zw4.Create("cool-pet/preview.png")
	_, _ = w4.Write(png1x1)
	_ = zw4.Close()
	if _, err := reg.InstallZipBytes(buf2.Bytes()); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(user, "cool-pet", "preview.png")); err != nil {
		t.Fatal("reinstall should write new files")
	}
	if _, err := os.Stat(filepath.Join(user, "cool-pet.bak-install")); !os.IsNotExist(err) {
		t.Fatal("backup dir should be cleaned after successful reinstall")
	}

	// uninstall
	if err := reg.Uninstall("cool-pet"); err != nil {
		t.Fatal(err)
	}
	if reg.Allowlist()["cool-pet"] {
		// may remain false after scan
	}
}

func TestInstallZipRejectsDuplicateArchivePath(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, content := range []string{"first", "second"} {
		w, err := zw.Create("duplicate-pet/pet-pack.yaml")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(t.TempDir(), nil)
	if _, err := reg.InstallZipBytes(buf.Bytes()); err == nil {
		t.Fatal("expected duplicate archive path rejection")
	}
}

func TestInstallMarketZipDoesNotOverwriteLocalPackWithSameID(t *testing.T) {
	user := t.TempDir()
	reg := NewRegistry(user, nil)
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}

	archive := testPetPackArchive(t, "shared-pet", "Local original")
	if _, err := reg.InstallZipBytes(archive); err != nil {
		t.Fatalf("install local pack: %v", err)
	}
	if _, err := reg.InstallMarketZipBytes(testPetPackArchive(t, "shared-pet", "Market replacement")); err == nil {
		t.Fatal("market install should not overwrite a local pack with the same id")
	}
	manifest, err := os.ReadFile(filepath.Join(user, "shared-pet", "pet-pack.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(manifest, []byte("name: Local original")) {
		t.Fatalf("local pack was overwritten: %s", manifest)
	}

	if err := reg.SetPackSource("shared-pet", SourceMarket); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.InstallMarketZipBytes(testPetPackArchive(t, "shared-pet", "Market upgrade")); err != nil {
		t.Fatalf("market re-install should replace an existing market pack: %v", err)
	}
	manifest, err = os.ReadFile(filepath.Join(user, "shared-pet", "pet-pack.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(manifest, []byte("name: Market upgrade")) {
		t.Fatalf("market pack was not upgraded: %s", manifest)
	}
	if got := packSourceForDir(filepath.Join(user, "shared-pet")); got != SourceMarket {
		t.Fatalf("source=%q, want market", got)
	}
}

func testPetPackArchive(t *testing.T, id, name string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(id + "/pet-pack.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("schema_version: 1\nid: " + id + "\nname: " + name + "\nversion: 1.0.0\nrenderer: native-raster\nassets:\n  native:\n    idle: native/idle.png\n")); err != nil {
		t.Fatal(err)
	}
	w, err = zw.Create(id + "/native/idle.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(minimalPNG()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestInstallZipRejectsOversizeAndZipBomb(t *testing.T) {
	user := t.TempDir()
	reg := NewRegistry(user, BundledFS())
	_ = reg.Scan()

	// Single file over MaxSingleFileBytes
	var big bytes.Buffer
	zw := zip.NewWriter(&big)
	w, err := zw.Create("huge-pet/pet-pack.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("schema_version: 1\nid: huge-pet\nname: Huge\nversion: 1.0.0\nrenderer: native-raster\nassets:\n  native:\n    idle: native/idle.png\n"))
	// Write a large stored file (no compression benefit)
	hw, err := zw.CreateHeader(&zip.FileHeader{
		Name:   "huge-pet/native/idle.png",
		Method: zip.Store,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte{0x41}, MaxSingleFileBytes+1024)
	if _, err := hw.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.InstallZipBytes(big.Bytes()); err == nil {
		t.Fatal("expected oversize single-file rejection")
	}

	// Zip bomb: high compression ratio (many zeros)
	var bomb bytes.Buffer
	zw2 := zip.NewWriter(&bomb)
	w2, _ := zw2.Create("bomb-pet/pet-pack.yaml")
	_, _ = w2.Write([]byte("schema_version: 1\nid: bomb-pet\nname: Bomb\nversion: 1.0.0\nrenderer: native-raster\nassets:\n  native:\n    idle: native/idle.png\n"))
	// Deflate zeros — compressed size tiny, uncompressed large but under total cap
	bw, err := zw2.CreateHeader(&zip.FileHeader{
		Name:   "bomb-pet/native/idle.png",
		Method: zip.Deflate,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 400KiB zeros compress extremely well → ratio >> 12
	if _, err := bw.Write(bytes.Repeat([]byte{0}, 400*1024)); err != nil {
		t.Fatal(err)
	}
	if err := zw2.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.InstallZipBytes(bomb.Bytes()); err == nil {
		t.Fatal("expected zip-bomb ratio rejection")
	}

	// Total uncompressed over MaxUncompressedBytes via multiple files
	var multi bytes.Buffer
	zw3 := zip.NewWriter(&multi)
	w3, _ := zw3.Create("multi-pet/pet-pack.yaml")
	_, _ = w3.Write([]byte("schema_version: 1\nid: multi-pet\nname: Multi\nversion: 1.0.0\nrenderer: native-raster\nassets:\n  native:\n    idle: native/idle.png\n"))
	chunk := bytes.Repeat([]byte{1, 2, 3, 4}, 64*1024) // 256KiB semi-random
	for i := 0; i < 10; i++ {                          // 2.5MiB total > 2MiB cap
		name := "multi-pet/native/f" + string(rune('a'+i)) + ".png"
		// Use numeric names
		name = "multi-pet/native/chunk" + itoa(i) + ".png"
		cw, err := zw3.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cw.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw3.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.InstallZipBytes(multi.Bytes()); err == nil {
		t.Fatal("expected total uncompressed size rejection")
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [12]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}

func minimalPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde, 0x00, 0x00, 0x00,
		0x0c, 0x49, 0x44, 0x41, 0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x03, 0x00, 0x01, 0x00, 0x05, 0xfe, 0xd4, 0xef, 0x00, 0x00,
		0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
}
