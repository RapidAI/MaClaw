// fwpack creates the immutable, signed firmware container consumed by
// ClawMate Maker. It is a CI-only tool: end-user desktops never compile or
// package ESP-IDF projects.
package main

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"clawmatemaker/internal/firmware"
	"clawmatemaker/internal/partition"
)

const maxInputImage = 32 * 1024 * 1024

type input struct {
	Board, FirmwareBoard, ReleaseVersion, PackageID, ImagePath, PartitionTablePath, ProjectDescriptionPath, OutputPath, KeyID, PrivateKey string
	FlashBytes                                                                                                                            int64
}

func main() {
	var v input
	flag.StringVar(&v.Board, "board", "", "official board ID")
	flag.StringVar(&v.FirmwareBoard, "firmware-board", "", "firmware target board ID emitted in BOOT_STATUS")
	flag.StringVar(&v.ReleaseVersion, "release", "", "release tag/version")
	flag.StringVar(&v.PackageID, "package-id", "", "stable package identifier")
	flag.StringVar(&v.ImagePath, "image", "", "merged flash image at offset 0")
	flag.StringVar(&v.PartitionTablePath, "partition-table", "", "ESP-IDF partition-table.bin")
	flag.StringVar(&v.ProjectDescriptionPath, "project-description", "", "ESP-IDF project_description.json")
	flag.StringVar(&v.OutputPath, "output", "", "output .clawfw path")
	flag.StringVar(&v.KeyID, "key-id", "", "Ed25519 release signing key ID")
	flag.StringVar(&v.PrivateKey, "private-key-base64", os.Getenv("CLAWMATE_FIRMWARE_SIGNING_KEY"), "Ed25519 private key, base64")
	flag.Int64Var(&v.FlashBytes, "flash-bytes", 16*1024*1024, "target flash capacity")
	flag.Parse()
	if err := run(v); err != nil {
		fmt.Fprintln(os.Stderr, "fwpack:", err)
		os.Exit(1)
	}
}

func run(v input) error {
	if v.Board == "" || v.FirmwareBoard == "" || v.ReleaseVersion == "" || v.ImagePath == "" || v.PartitionTablePath == "" || v.ProjectDescriptionPath == "" || v.OutputPath == "" || v.KeyID == "" || v.PrivateKey == "" || v.FlashBytes <= 0 {
		return errors.New("board, firmware-board, release, image, partition-table, project-description, output, key-id, private key and flash capacity are required")
	}
	for _, value := range []string{v.Board, v.FirmwareBoard, v.ReleaseVersion, v.KeyID} {
		if strings.ContainsAny(value, "\\/\x00") {
			return fmt.Errorf("invalid identifier %q", value)
		}
	}
	keyRaw, err := base64.StdEncoding.DecodeString(v.PrivateKey)
	if err != nil {
		return fmt.Errorf("decode signing key: %w", err)
	}
	if len(keyRaw) != ed25519.PrivateKeySize {
		return fmt.Errorf("invalid Ed25519 private key length: %d", len(keyRaw))
	}
	image, err := readBounded(v.ImagePath, maxInputImage)
	if err != nil {
		return fmt.Errorf("read merged image: %w", err)
	}
	table, err := readBounded(v.PartitionTablePath, 64*1024)
	if err != nil {
		return fmt.Errorf("read partition table: %w", err)
	}
	layout, err := partition.Parse(table, uint64(v.FlashBytes))
	if err != nil {
		return fmt.Errorf("parse partition table: %w", err)
	}
	projectRaw, err := readBounded(v.ProjectDescriptionPath, 2*1024*1024)
	if err != nil {
		return fmt.Errorf("read project description: %w", err)
	}
	var project map[string]any
	if err := json.Unmarshal(projectRaw, &project); err != nil {
		return fmt.Errorf("parse project description: %w", err)
	}
	projectName, _ := project["project_name"].(string)
	version, _ := project["project_version"].(string)
	elfSHA, _ := project["app_elf_sha256"].(string)
	if elfSHA == "" {
		if app, ok := project["app_desc"].(map[string]any); ok {
			elfSHA, _ = app["elf_sha256"].(string)
		}
	}
	if projectName == "" || version == "" || elfSHA == "" {
		return errors.New("project description must contain project_name, project_version and app_elf_sha256")
	}
	if v.PackageID == "" {
		v.PackageID = v.Board + "-" + v.ReleaseVersion
	}
	imageOffset := uint64(0)
	manifest := firmware.Manifest{SchemaVersion: 1, PackageID: v.PackageID, ReleaseVersion: v.ReleaseVersion, Board: firmware.Board{ID: v.FirmwareBoard, ProfileHash: "catalog:" + v.Board}, Chip: firmware.Chip{Family: "esp32s3", FlashBytes: v.FlashBytes}, SecurityBaseline: firmware.SecurityBaseline{SecureVersion: 0}, Layout: firmware.Layout{Fingerprint: layout.Fingerprint, PartitionTablePath: "metadata/partition-table.bin"}, Mode: "full", AppIdentity: firmware.AppIdentity{ProjectName: projectName, AppVersion: version, ELFSHA256: elfSHA, PSRAMBytes: 8 * 1024 * 1024}, BootVerification: firmware.BootVerification{Baud: 115200, TimeoutSeconds: 30, RequiredSelfTests: []string{"local_ready"}}, Files: []firmware.FileSpec{fileSpec("images/full-flash.bin", image, &imageOffset, "flash"), fileSpec("metadata/partition-table.bin", table, nil, "metadata")}}
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	signature := ed25519.Sign(ed25519.PrivateKey(keyRaw), manifestRaw)
	sigRaw, err := json.Marshal(firmware.SignatureEnvelope{Algorithm: "ed25519", KeyID: v.KeyID, Signature: base64.StdEncoding.EncodeToString(signature)})
	if err != nil {
		return err
	}
	return writeArchive(v.OutputPath, map[string][]byte{"manifest.json": manifestRaw, "manifest.sig": sigRaw, "images/full-flash.bin": image, "metadata/partition-table.bin": table})
}

func fileSpec(path string, raw []byte, offset *uint64, region string) firmware.FileSpec {
	sum := sha256.Sum256(raw)
	return firmware.FileSpec{Path: path, Size: int64(len(raw)), SHA256: "sha256:" + hex.EncodeToString(sum[:]), Offset: offset, Region: region}
}
func readBounded(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > max {
		return nil, errors.New("input exceeds maximum size")
	}
	return raw, nil
}
func writeArchive(path string, files map[string][]byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp := path + ".part"
	defer os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	z := zip.NewWriter(f)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetModTime(time.Unix(0, 0).UTC())
		out, err := z.CreateHeader(h)
		if err != nil {
			_ = z.Close()
			_ = f.Close()
			return err
		}
		if _, err = out.Write(files[name]); err != nil {
			_ = z.Close()
			_ = f.Close()
			return err
		}
	}
	if err = z.Close(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
