package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"clawmatemaker/internal/firmware"
	"clawmatemaker/internal/partition"
)

func TestRunBuildsSignedPackage(t *testing.T) {
	dir := t.TempDir()
	image, table, project, output := filepath.Join(dir, "image.bin"), filepath.Join(dir, "partition-table.bin"), filepath.Join(dir, "project.json"), filepath.Join(dir, "firmware.clawfw")
	if err := os.WriteFile(image, []byte("firmware"), 0600); err != nil {
		t.Fatal(err)
	}
	raw, err := partition.Encode([]partition.Entry{{Label: "factory", Type: 0, Subtype: 0, Offset: 0x10000, Size: 0x3a0000}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(table, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, []byte(`{"project_name":"client","project_version":"1.0.0","app_elf_sha256":"abc"}`), 0600); err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := run(input{Board: "bread-compact", FirmwareBoard: "bread-compact-wifi-lcd-v1", ReleaseVersion: "v1", ImagePath: image, PartitionTablePath: table, ProjectDescriptionPath: project, OutputPath: output, KeyID: "test", PrivateKey: base64.StdEncoding.EncodeToString(priv), FlashBytes: 16 * 1024 * 1024}); err != nil {
		t.Fatal(err)
	}
	if _, err := firmware.VerifyRelease(output, firmware.TrustStore{"test": pub}); err != nil {
		t.Fatal(err)
	}
}
