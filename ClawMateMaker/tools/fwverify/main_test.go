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
	if result.InstallMode != firmware.ModeFull || result.ImageCount != 1 || len(result.WriteOrder) != 0 {
		t.Fatalf("legacy evidence=%#v", result)
	}
	if _, err := verify(archive, "echoear-2st", "test", base64.StdEncoding.EncodeToString(pub)); err == nil {
		t.Fatal("cross-board package accepted")
	}
	if _, err := verifyWithPolicy(archive, "bread-compact", "test", base64.StdEncoding.EncodeToString(pub), true); err == nil {
		t.Fatal("official split-full policy accepted a legacy merged package")
	}
}

func writeTestPackage(path string, private ed25519.PrivateKey, boardID, profileHash string) error {
	table := []byte("partition table metadata")
	image := make([]byte, 0x8000+len(table))
	copy(image[0x8000:], table)
	sum := sha256.Sum256(image)
	tableSum := sha256.Sum256(table)
	offset := uint64(0)
	manifest := firmware.Manifest{
		SchemaVersion:  1,
		PackageID:      "test-package",
		ReleaseVersion: "v1",
		Channel:        firmware.ChannelStable,
		Board:          firmware.Board{ID: boardID, ProfileHash: profileHash},
		Chip:           firmware.Chip{Family: "esp32s3", FlashBytes: 16 * 1024 * 1024},
		SecurityBaseline: firmware.SecurityBaseline{
			SecureBoot: false, FlashEncryption: false, SecureVersion: 0,
		},
		Layout: firmware.Layout{
			ID: "factory-layout-v1", Fingerprint: "sha256:test-layout", PartitionTablePath: "metadata/partition-table.bin",
		},
		Mode: firmware.ModeFull,
		AppIdentity: firmware.AppIdentity{
			ProjectName: "test-app", AppVersion: "v1", ELFSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", ReleaseSequence: 1, PSRAMBytes: 8 * 1024 * 1024,
		},
		BootVerification: firmware.BootVerification{Baud: 115200, TimeoutSeconds: 30, RequiredSelfTests: []string{"local_ready"}},
		Files: []firmware.FileSpec{
			{Path: "images/full-flash.bin", Size: int64(len(image)), SHA256: "sha256:" + hex.EncodeToString(sum[:]), Offset: &offset, Region: "flash"},
			{Path: "metadata/partition-table.bin", Size: int64(len(table)), SHA256: "sha256:" + hex.EncodeToString(tableSum[:]), Region: "metadata"},
		},
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
	for name, contents := range map[string][]byte{"manifest.json": rawManifest, "manifest.sig": signature, "images/full-flash.bin": image, "metadata/partition-table.bin": table} {
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
