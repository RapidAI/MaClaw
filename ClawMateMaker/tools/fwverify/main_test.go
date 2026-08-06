package main

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"clawmatemaker/internal/firmware"
)

func TestVerifyRequiresSignedCatalogBoundPackage(t *testing.T) {
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "bread.clawfw")
	if err := writeTestPackage(archive, private, "bread-compact-wifi-lcd-v1", "catalog:bread-compact"); err != nil {
		t.Fatal(err)
	}
	result, err := verify(archive, "bread-compact", "test", base64.StdEncoding.EncodeToString(pub))
	if err != nil || result.BoardID != "bread-compact" || result.ArchiveSHA256 == "" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if _, err := verify(archive, "echoear-2st", "test", base64.StdEncoding.EncodeToString(pub)); err == nil {
		t.Fatal("cross-board package accepted")
	}
}

func writeTestPackage(path string, private ed25519.PrivateKey, boardID, profileHash string) error {
	image := []byte("full image")
	sum := sha256.Sum256(image)
	offset := uint64(0)
	manifest := firmware.Manifest{
		SchemaVersion:  1,
		PackageID:      "test-package",
		ReleaseVersion: "v1",
		Board:          firmware.Board{ID: boardID, ProfileHash: profileHash},
		Chip:           firmware.Chip{Family: "esp32s3", FlashBytes: 16 * 1024 * 1024},
		Mode:           firmware.ModeFull,
		Files:          []firmware.FileSpec{{Path: "images/full-flash.bin", Size: int64(len(image)), SHA256: "sha256:" + hex.EncodeToString(sum[:]), Offset: &offset, Region: "flash"}},
	}
	rawManifest, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	signature, err := json.Marshal(firmware.SignatureEnvelope{Algorithm: "ed25519", KeyID: "test", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, rawManifest))})
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	z := zip.NewWriter(f)
	for name, contents := range map[string][]byte{"manifest.json": rawManifest, "manifest.sig": signature, "images/full-flash.bin": image} {
		out, err := z.Create(name)
		if err != nil {
			return err
		}
		if _, err := out.Write(contents); err != nil {
			return err
		}
	}
	return z.Close()
}
