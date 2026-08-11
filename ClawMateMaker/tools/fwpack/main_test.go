package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawmatemaker/internal/firmware"
	"clawmatemaker/internal/partition"
)

const testELFSHA256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestRunBuildsSignedPackage(t *testing.T) {
	dir := t.TempDir()
	image, table, project, output := filepath.Join(dir, "image.bin"), filepath.Join(dir, "partition-table.bin"), filepath.Join(dir, "project.json"), filepath.Join(dir, "firmware.clawfw")
	raw, err := partition.Encode([]partition.Entry{{Label: "factory", Type: 0, Subtype: 0, Offset: 0x10000, Size: 0x3a0000}})
	if err != nil {
		t.Fatal(err)
	}
	fullImage := make([]byte, 0x8000+len(raw))
	copy(fullImage[0x8000:], raw)
	if err := os.WriteFile(image, fullImage, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(table, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, []byte(`{"project_name":"client","project_version":"1.0.0","app_elf_sha256":"`+testELFSHA256+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := run(input{Board: "bread-compact", FirmwareBoard: "bread-compact-wifi-lcd-v1", LayoutID: "maclaw-s3-16m-factory-v2", ReleaseVersion: "v1", ReleaseSequence: 1, ImagePath: image, PartitionTablePath: table, ProjectDescriptionPath: project, OutputPath: output, KeyID: "test", PrivateKey: base64.StdEncoding.EncodeToString(priv), FlashBytes: 16 * 1024 * 1024}); err != nil {
		t.Fatal(err)
	}
	if _, err := firmware.VerifyRelease(output, firmware.TrustStore{"test": pub}); err != nil {
		t.Fatal(err)
	}
}

func TestRunBuildsSignedSplitFullPackageFromFlasherArgs(t *testing.T) {
	dir := t.TempDir()
	table, err := partition.Encode([]partition.Entry{
		{Label: "factory", Type: 0, Subtype: 0, Offset: 0x10000, Size: 0x3a0000},
		{Label: "storage", Type: 1, Subtype: 0x82, Offset: 0x3b0000, Size: 0x300000},
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, payload := range map[string][]byte{
		"bootloader.bin": {1, 2, 3}, "partition-table.bin": table, "app.bin": {4, 5, 6}, "storage.bin": {7, 8, 9},
	} {
		if err := os.WriteFile(filepath.Join(dir, name), payload, 0600); err != nil {
			t.Fatal(err)
		}
	}
	args, err := json.Marshal(flasherArgs{FlashFiles: map[string]string{"0x0": "bootloader.bin", "0x8000": "partition-table.bin", "0x10000": "app.bin", "0x3b0000": "storage.bin"}})
	if err != nil {
		t.Fatal(err)
	}
	argsPath := filepath.Join(dir, "flasher_args.json")
	sdkconfig := filepath.Join(dir, "sdkconfig.h")
	project := filepath.Join(dir, "project.json")
	output := filepath.Join(dir, "firmware.clawfw")
	if err := os.WriteFile(argsPath, args, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sdkconfig, []byte("#define CONFIG_MACLAW_BOARD_ID \"bread-compact-wifi-lcd-v1\"\n#define CONFIG_MACLAW_COMPAT_ID \"maclaw-clawmate:bread-compact-wifi-lcd-v1:maclaw-s3-16m-factory-v2\"\n#define CONFIG_MACLAW_RELEASE_SEQUENCE 1\n#define CONFIG_ESPTOOLPY_FLASHSIZE_16MB 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, []byte(`{"project_name":"client","project_version":"1.0.0","app_elf_sha256":"`+testELFSHA256+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := run(input{Board: "bread-compact", FirmwareBoard: "bread-compact-wifi-lcd-v1", LayoutID: "maclaw-s3-16m-factory-v2", ReleaseVersion: "v1", ReleaseSequence: 1, FlasherArgsPath: argsPath, PartitionTablePath: filepath.Join(dir, "partition-table.bin"), ProjectDescriptionPath: project, SDKConfigHeaderPath: sdkconfig, OutputPath: output, KeyID: "test", PrivateKey: base64.StdEncoding.EncodeToString(priv), FlashBytes: 16 * 1024 * 1024}); err != nil {
		t.Fatal(err)
	}
	verified, err := firmware.VerifyRelease(output, firmware.TrustStore{"test": pub})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(verified.Manifest.WriteOrder, ","), "storage,app,partition-table,bootloader"; got != want {
		t.Fatalf("write order=%q want=%q", got, want)
	}
	if len(verified.Manifest.Files) != 5 {
		t.Fatalf("file specs=%d", len(verified.Manifest.Files))
	}
}

func TestValidateSDKConfigHeaderRequiresExact32MiBDeclaration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sdkconfig.h")
	const board = "waveshare-s3-touch-amoled-1.75c-v1"
	const layout = "maclaw-s3-32m-factory-v1"
	valid := "#define CONFIG_MACLAW_BOARD_ID \"" + board + "\"\n" +
		"#define CONFIG_MACLAW_COMPAT_ID \"maclaw-clawmate:" + board + ":" + layout + "\"\n" +
		"#define CONFIG_MACLAW_RELEASE_SEQUENCE 7\n" +
		"#define CONFIG_ESPTOOLPY_FLASHSIZE_32MB 1\n"
	if err := os.WriteFile(path, []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateSDKConfigHeader(path, board, layout, 7, 32*1024*1024); err != nil {
		t.Fatalf("valid 32 MiB sdkconfig rejected: %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(valid, "#define CONFIG_ESPTOOLPY_FLASHSIZE_32MB 1\n", "", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateSDKConfigHeader(path, board, layout, 7, 32*1024*1024); err == nil || !strings.Contains(err.Error(), "FLASHSIZE_32MB") {
		t.Fatalf("missing 32 MiB declaration was accepted: %v", err)
	}
}

func TestRunRejectsSplitPackageWhenGeneratedSDKConfigDoesNotMatchManifestIdentity(t *testing.T) {
	dir := t.TempDir()
	table, err := partition.Encode([]partition.Entry{{Label: "factory", Type: 0, Subtype: 0, Offset: 0x10000, Size: 0x3a0000}})
	if err != nil {
		t.Fatal(err)
	}
	for name, payload := range map[string][]byte{"bootloader.bin": {1}, "partition-table.bin": table, "app.bin": {2}} {
		if err := os.WriteFile(filepath.Join(dir, name), payload, 0600); err != nil {
			t.Fatal(err)
		}
	}
	args, err := json.Marshal(flasherArgs{FlashFiles: map[string]string{"0x0": "bootloader.bin", "0x8000": "partition-table.bin", "0x10000": "app.bin"}})
	if err != nil {
		t.Fatal(err)
	}
	argsPath, project, sdkconfig := filepath.Join(dir, "flasher_args.json"), filepath.Join(dir, "project.json"), filepath.Join(dir, "sdkconfig.h")
	if err := os.WriteFile(argsPath, args, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, []byte(`{"project_name":"client","project_version":"1.0.0","app_elf_sha256":"`+testELFSHA256+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sdkconfig, []byte("#define CONFIG_MACLAW_BOARD_ID \"wrong-board\"\n#define CONFIG_MACLAW_COMPAT_ID \"maclaw-clawmate:wrong-board:maclaw-s3-16m-factory-v2\"\n#define CONFIG_MACLAW_RELEASE_SEQUENCE 1\n#define CONFIG_ESPTOOLPY_FLASHSIZE_16MB 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	err = run(input{Board: "bread-compact", FirmwareBoard: "bread-compact-wifi-lcd-v1", LayoutID: "maclaw-s3-16m-factory-v2", ReleaseVersion: "v1", ReleaseSequence: 1, FlasherArgsPath: argsPath, PartitionTablePath: filepath.Join(dir, "partition-table.bin"), ProjectDescriptionPath: project, SDKConfigHeaderPath: sdkconfig, OutputPath: filepath.Join(dir, "firmware.clawfw"), KeyID: "test", PrivateKey: base64.StdEncoding.EncodeToString(priv), FlashBytes: 16 * 1024 * 1024})
	if err == nil || !strings.Contains(err.Error(), "does not match --firmware-board") {
		t.Fatalf("mismatched generated sdkconfig was packaged: %v", err)
	}
}

func TestRunRejectsInvalidProjectELFSHA256(t *testing.T) {
	dir := t.TempDir()
	image, table, project, output := filepath.Join(dir, "image.bin"), filepath.Join(dir, "partition-table.bin"), filepath.Join(dir, "project.json"), filepath.Join(dir, "firmware.clawfw")
	raw, err := partition.Encode([]partition.Entry{{Label: "factory", Type: 0, Subtype: 0, Offset: 0x10000, Size: 0x3a0000}})
	if err != nil {
		t.Fatal(err)
	}
	fullImage := make([]byte, 0x8000+len(raw))
	copy(fullImage[0x8000:], raw)
	if err := os.WriteFile(image, fullImage, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(table, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, []byte(`{"project_name":"client","project_version":"1.0.0","app_elf_sha256":"abc"}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	err = run(input{Board: "bread-compact", FirmwareBoard: "bread-compact-wifi-lcd-v1", LayoutID: "maclaw-s3-16m-factory-v2", ReleaseVersion: "v1", ReleaseSequence: 1, ImagePath: image, PartitionTablePath: table, ProjectDescriptionPath: project, OutputPath: output, KeyID: "test", PrivateKey: base64.StdEncoding.EncodeToString(private), FlashBytes: 16 * 1024 * 1024})
	if err == nil || !strings.Contains(err.Error(), "app_elf_sha256") {
		t.Fatalf("invalid project ELF digest was packaged: %v", err)
	}
}

func TestRunRejectsFullImageWithDifferentPartitionTable(t *testing.T) {
	dir := t.TempDir()
	image, table, project, output := filepath.Join(dir, "image.bin"), filepath.Join(dir, "partition-table.bin"), filepath.Join(dir, "project.json"), filepath.Join(dir, "firmware.clawfw")
	raw, err := partition.Encode([]partition.Entry{{Label: "factory", Type: 0, Subtype: 0, Offset: 0x10000, Size: 0x3a0000}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(table, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(image, make([]byte, 0x8000+len(raw)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, []byte(`{"project_name":"client","project_version":"1.0.0","app_elf_sha256":"`+testELFSHA256+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	err = run(input{Board: "bread-compact", FirmwareBoard: "bread-compact-wifi-lcd-v1", LayoutID: "maclaw-s3-16m-factory-v2", ReleaseVersion: "v1", ReleaseSequence: 1, ImagePath: image, PartitionTablePath: table, ProjectDescriptionPath: project, OutputPath: output, KeyID: "test", PrivateKey: base64.StdEncoding.EncodeToString(private), FlashBytes: 16 * 1024 * 1024})
	if err == nil || !strings.Contains(err.Error(), "partition table") {
		t.Fatalf("mismatched full image was packaged: %v", err)
	}
}

func TestRunBuildsSignedAppOnlyPackageAtFactoryOffset(t *testing.T) {
	dir := t.TempDir()
	image, table, project, output := filepath.Join(dir, "app.bin"), filepath.Join(dir, "partition-table.bin"), filepath.Join(dir, "project.json"), filepath.Join(dir, "firmware.clawfw")
	if err := os.WriteFile(image, []byte("application firmware"), 0600); err != nil {
		t.Fatal(err)
	}
	raw, err := partition.Encode([]partition.Entry{{Label: "factory", Type: 0, Subtype: 0, Offset: 0x10000, Size: 0x3a0000}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(table, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, []byte(`{"project_name":"client","project_version":"1.0.0","app_elf_sha256":"`+testELFSHA256+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := run(input{Board: "bread-compact", FirmwareBoard: "bread-compact-wifi-lcd-v1", LayoutID: "maclaw-s3-16m-factory-v2", ReleaseVersion: "v1", ReleaseSequence: 1, Mode: firmware.ModeAppOnly, ImagePath: image, PartitionTablePath: table, ProjectDescriptionPath: project, OutputPath: output, KeyID: "test", PrivateKey: base64.StdEncoding.EncodeToString(priv), FlashBytes: 16 * 1024 * 1024}); err != nil {
		t.Fatal(err)
	}
	verified, err := firmware.VerifyRelease(output, firmware.TrustStore{"test": pub})
	if err != nil {
		t.Fatal(err)
	}
	if verified.Manifest.Mode != firmware.ModeAppOnly || len(verified.Manifest.Files) != 2 {
		t.Fatalf("unexpected manifest: %#v", verified.Manifest)
	}
	imageSpec := verified.Manifest.Files[0]
	if imageSpec.Path != "images/app.bin" || imageSpec.Region != "app" || imageSpec.Offset == nil || *imageSpec.Offset != 0x10000 {
		t.Fatalf("unexpected app-only image spec: %#v", imageSpec)
	}
	plan, err := firmware.InstallPlanFor(verified.Manifest)
	if err != nil || !plan.PreservesUserData {
		t.Fatalf("app-only plan = %#v, err=%v", plan, err)
	}
}
