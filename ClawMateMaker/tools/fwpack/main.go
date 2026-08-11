// fwpack creates the immutable, signed firmware container consumed by
// ClawMate Maker. It is a CI-only tool: end-user desktops never compile or
// package ESP-IDF projects.
package main

import (
	"archive/zip"
	"bytes"
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
	"strconv"
	"strings"
	"time"

	"clawmatemaker/internal/firmware"
	"clawmatemaker/internal/partition"
)

const maxInputImage = 32 * 1024 * 1024

type input struct {
	Board, FirmwareBoard, LayoutID, ReleaseVersion, PackageID, ImagePath, FlasherArgsPath, PartitionTablePath, ProjectDescriptionPath, SDKConfigHeaderPath, OutputPath, KeyID, PrivateKey string
	Mode, Channel                                                                                                                                                                         string
	FlashBytes, ReleaseSequence                                                                                                                                                           int64
}

func main() {
	var v input
	flag.StringVar(&v.Board, "board", "", "official board ID")
	flag.StringVar(&v.FirmwareBoard, "firmware-board", "", "firmware target board ID emitted in BOOT_STATUS")
	flag.StringVar(&v.LayoutID, "layout-id", "", "runtime layout ID emitted in BOOT_STATUS")
	flag.StringVar(&v.ReleaseVersion, "release", "", "release tag/version")
	flag.StringVar(&v.PackageID, "package-id", "", "stable package identifier")
	flag.StringVar(&v.ImagePath, "image", "", "merged flash image at offset 0")
	flag.StringVar(&v.FlasherArgsPath, "flasher-args", "", "ESP-IDF flasher_args.json; creates a signed split full-install plan")
	flag.StringVar(&v.PartitionTablePath, "partition-table", "", "ESP-IDF partition-table.bin")
	flag.StringVar(&v.ProjectDescriptionPath, "project-description", "", "ESP-IDF project_description.json")
	flag.StringVar(&v.SDKConfigHeaderPath, "sdkconfig-header", "", "ESP-IDF generated config/sdkconfig.h; required for split official packages")
	flag.StringVar(&v.OutputPath, "output", "", "output .clawfw path")
	flag.StringVar(&v.KeyID, "key-id", "", "Ed25519 release signing key ID")
	flag.StringVar(&v.PrivateKey, "private-key-base64", os.Getenv("CLAWMATE_FIRMWARE_SIGNING_KEY"), "Ed25519 private key, base64")
	flag.StringVar(&v.Mode, "mode", firmware.ModeFull, "signed install mode: full or app-only")
	flag.StringVar(&v.Channel, "channel", firmware.ChannelStable, "signed release channel: stable or beta")
	flag.Int64Var(&v.FlashBytes, "flash-bytes", 16*1024*1024, "target flash capacity")
	flag.Int64Var(&v.ReleaseSequence, "release-sequence", 0, "monotonic release sequence emitted by firmware")
	flag.Parse()
	if err := run(v); err != nil {
		fmt.Fprintln(os.Stderr, "fwpack:", err)
		os.Exit(1)
	}
}

func run(v input) error {
	if v.Mode == "" {
		v.Mode = firmware.ModeFull
	}
	if v.Channel == "" {
		v.Channel = firmware.ChannelStable
	}
	if v.Board == "" || v.FirmwareBoard == "" || v.LayoutID == "" || v.ReleaseVersion == "" || (v.ImagePath == "" && v.FlasherArgsPath == "") || v.PartitionTablePath == "" || v.ProjectDescriptionPath == "" || v.OutputPath == "" || v.KeyID == "" || v.PrivateKey == "" || v.FlashBytes <= 0 || v.ReleaseSequence <= 0 {
		return errors.New("board, firmware-board, layout-id, release, image or flasher-args, partition-table, project-description, output, key-id, private key, flash capacity and positive release sequence are required")
	}
	if v.Mode != firmware.ModeFull && v.Mode != firmware.ModeAppOnly {
		return fmt.Errorf("unsupported install mode %q", v.Mode)
	}
	v.Channel = strings.ToLower(strings.TrimSpace(v.Channel))
	if !firmware.ValidReleaseChannel(v.Channel) {
		return fmt.Errorf("unsupported release channel %q", v.Channel)
	}
	for _, value := range []string{v.Board, v.FirmwareBoard, v.LayoutID, v.ReleaseVersion, v.KeyID} {
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
	var image []byte
	if v.ImagePath != "" {
		image, err = readBounded(v.ImagePath, maxInputImage)
		if err != nil {
			return fmt.Errorf("read merged image: %w", err)
		}
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
	elfSHA, validELFSHA := firmware.NormalizeSHA256(elfSHA)
	if !validELFSHA {
		return errors.New("project description app_elf_sha256 must be a SHA-256 digest")
	}
	if v.PackageID == "" {
		v.PackageID = v.Board + "-" + v.ReleaseVersion
	}
	imageOffset := uint64(0)
	imagePathInArchive, imageRegion := "images/full-flash.bin", "flash"
	var splitImages []packImage
	var writeOrder []string
	if v.Mode == firmware.ModeAppOnly {
		factory, found := partition.Find(layout.Entries, "factory")
		if !found {
			for _, entry := range layout.Entries {
				if entry.Type == 0 && entry.Subtype == 0 {
					factory, found = entry, true
					break
				}
			}
		}
		if !found {
			return errors.New("app-only package requires a factory application partition")
		}
		imageOffset = uint64(factory.Offset)
		imagePathInArchive, imageRegion = "images/app.bin", "app"
		if uint64(len(image)) > uint64(factory.Size) {
			return fmt.Errorf("app-only image size %d exceeds factory partition size %d", len(image), factory.Size)
		}
	} else if v.FlasherArgsPath != "" {
		if err := validateSDKConfigHeader(v.SDKConfigHeaderPath, v.FirmwareBoard, v.LayoutID, v.ReleaseSequence, v.FlashBytes); err != nil {
			return err
		}
		splitImages, writeOrder, err = splitImagesFromFlasherArgs(v.FlasherArgsPath, layout, table)
		if err != nil {
			return err
		}
	} else {
		const partitionTableOffset = 0x8000
		if len(image) < partitionTableOffset+len(table) {
			return errors.New("full flash image does not contain the partition table at offset 0x8000")
		}
		if !bytes.Equal(image[partitionTableOffset:partitionTableOffset+len(table)], table) {
			return errors.New("full flash image partition table at offset 0x8000 differs from --partition-table")
		}
	}
	files := []firmware.FileSpec{fileSpec(imagePathInArchive, image, &imageOffset, imageRegion)}
	archiveFiles := map[string][]byte{imagePathInArchive: image}
	if len(splitImages) != 0 {
		files = make([]firmware.FileSpec, 0, len(splitImages)+1)
		archiveFiles = make(map[string][]byte, len(splitImages)+2)
		for _, item := range splitImages {
			files = append(files, namedFileSpec(item.Name, item.ArchivePath, item.Raw, item.Offset, item.Region))
			archiveFiles[item.ArchivePath] = item.Raw
		}
	}
	files = append(files, fileSpec("metadata/partition-table.bin", table, nil, "metadata"))
	archiveFiles["metadata/partition-table.bin"] = table
	manifest := firmware.Manifest{SchemaVersion: 1, PackageID: v.PackageID, ReleaseVersion: v.ReleaseVersion, Channel: v.Channel, Board: firmware.Board{ID: v.FirmwareBoard, ProfileHash: "catalog:" + v.Board}, Chip: firmware.Chip{Family: "esp32s3", FlashBytes: v.FlashBytes}, SecurityBaseline: firmware.SecurityBaseline{SecureVersion: 0}, Layout: firmware.Layout{ID: v.LayoutID, Fingerprint: layout.Fingerprint, PartitionTablePath: "metadata/partition-table.bin"}, Mode: v.Mode, Recovery: firmware.Recovery{PowerLossBootable: false}, WriteOrder: writeOrder, AppIdentity: firmware.AppIdentity{ProjectName: projectName, AppVersion: version, ELFSHA256: elfSHA, ReleaseSequence: v.ReleaseSequence, PSRAMBytes: 8 * 1024 * 1024}, BootVerification: firmware.BootVerification{Baud: 115200, TimeoutSeconds: 30, RequiredSelfTests: []string{"local_ready"}}, Files: files}
	if _, err := firmware.InstallPlanFor(manifest); err != nil {
		return fmt.Errorf("validate signed install plan: %w", err)
	}
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	signature := ed25519.Sign(ed25519.PrivateKey(keyRaw), manifestRaw)
	sigRaw, err := json.Marshal(firmware.SignatureEnvelope{Algorithm: "ed25519", KeyID: v.KeyID, Signature: base64.StdEncoding.EncodeToString(signature)})
	if err != nil {
		return err
	}
	archiveFiles["manifest.json"] = manifestRaw
	archiveFiles["manifest.sig"] = sigRaw
	return writeArchive(v.OutputPath, archiveFiles)
}

// validateSDKConfigHeader binds the signed release metadata to the exact
// generated ESP-IDF configuration used to compile the firmware.  Workflow
// matrix values are useful routing input, but must never be sufficient to
// relabel a build whose CONFIG_MACLAW_* identity or release sequence differs
// from the manifest that the desktop will later expect in BOOT_STATUS.
func validateSDKConfigHeader(pathname, firmwareBoard, layoutID string, releaseSequence, flashBytes int64) error {
	if strings.TrimSpace(pathname) == "" {
		return errors.New("split official package requires --sdkconfig-header")
	}
	raw, err := readBounded(pathname, 2*1024*1024)
	if err != nil {
		return fmt.Errorf("read sdkconfig header: %w", err)
	}
	values := make(map[string]string)
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#define CONFIG_") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		values[parts[1]] = strings.Trim(strings.Join(parts[2:], " "), `"`)
	}
	require := func(key string) (string, error) {
		value := strings.TrimSpace(values[key])
		if value == "" {
			return "", fmt.Errorf("sdkconfig header is missing %s", key)
		}
		return value, nil
	}
	board, err := require("CONFIG_MACLAW_BOARD_ID")
	if err != nil {
		return err
	}
	if board != firmwareBoard {
		return fmt.Errorf("sdkconfig board %q does not match --firmware-board %q", board, firmwareBoard)
	}
	compat, err := require("CONFIG_MACLAW_COMPAT_ID")
	if err != nil {
		return err
	}
	expectedCompat := "maclaw-clawmate:" + firmwareBoard + ":" + layoutID
	if compat != expectedCompat {
		return fmt.Errorf("sdkconfig compat ID %q does not match expected %q", compat, expectedCompat)
	}
	sequence, err := require("CONFIG_MACLAW_RELEASE_SEQUENCE")
	if err != nil {
		return err
	}
	actualSequence, err := strconv.ParseInt(sequence, 10, 64)
	if err != nil || actualSequence <= 0 || actualSequence != releaseSequence {
		return fmt.Errorf("sdkconfig release sequence %q does not match --release-sequence %d", sequence, releaseSequence)
	}
	if flashBytes == 16*1024*1024 && values["CONFIG_ESPTOOLPY_FLASHSIZE_16MB"] != "1" {
		return errors.New("sdkconfig must declare CONFIG_ESPTOOLPY_FLASHSIZE_16MB for the official 16 MiB profile")
	}
	if flashBytes == 32*1024*1024 && values["CONFIG_ESPTOOLPY_FLASHSIZE_32MB"] != "1" {
		return errors.New("sdkconfig must declare CONFIG_ESPTOOLPY_FLASHSIZE_32MB for the official 32 MiB profile")
	}
	return nil
}

func fileSpec(path string, raw []byte, offset *uint64, region string) firmware.FileSpec {
	sum := sha256.Sum256(raw)
	return firmware.FileSpec{Path: path, Size: int64(len(raw)), SHA256: "sha256:" + hex.EncodeToString(sum[:]), Offset: offset, Region: region}
}

func namedFileSpec(name, path string, raw []byte, offset uint64, region string) firmware.FileSpec {
	spec := fileSpec(path, raw, &offset, region)
	spec.Name = name
	return spec
}

type packImage struct {
	Name, ArchivePath, Region string
	Offset                    uint64
	Raw                       []byte
}

type flasherArgs struct {
	FlashFiles map[string]string `json:"flash_files"`
}

func splitImagesFromFlasherArgs(path string, layout partition.Table, expectedTable []byte) ([]packImage, []string, error) {
	raw, err := readBounded(path, 2*1024*1024)
	if err != nil {
		return nil, nil, fmt.Errorf("read flasher args: %w", err)
	}
	var args flasherArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, nil, fmt.Errorf("parse flasher args: %w", err)
	}
	if len(args.FlashFiles) == 0 {
		return nil, nil, errors.New("flasher args has no flash_files")
	}
	base := filepath.Dir(path)
	images := make([]packImage, 0, len(args.FlashFiles))
	seenNames := make(map[string]bool)
	for rawOffset, source := range args.FlashFiles {
		offset, parseErr := strconv.ParseUint(strings.TrimSpace(rawOffset), 0, 64)
		if parseErr != nil || offset%0x1000 != 0 {
			return nil, nil, fmt.Errorf("flasher args has invalid offset %q", rawOffset)
		}
		if !filepath.IsAbs(source) {
			source = filepath.Join(base, source)
		}
		payload, readErr := readBounded(source, maxInputImage)
		if readErr != nil {
			return nil, nil, fmt.Errorf("read flasher image at %#x: %w", offset, readErr)
		}
		name, region := splitImageIdentity(offset, layout)
		if seenNames[name] {
			return nil, nil, fmt.Errorf("flasher args produces duplicate signed image name %q", name)
		}
		seenNames[name] = true
		if name == "partition-table" && !bytes.Equal(payload, expectedTable) {
			return nil, nil, errors.New("flasher args partition-table image differs from --partition-table")
		}
		images = append(images, packImage{Name: name, ArchivePath: "images/" + name + ".bin", Region: region, Offset: offset, Raw: payload})
	}
	if !seenNames["bootloader"] || !seenNames["partition-table"] || !seenNames["app"] {
		return nil, nil, errors.New("flasher args must contain bootloader, partition-table, and application images")
	}
	sort.Slice(images, func(i, j int) bool { return images[i].Offset < images[j].Offset })
	normal := make([]string, 0, len(images))
	bootCritical := make([]string, 0, 2)
	for _, image := range images {
		if image.Name == "bootloader" || image.Name == "partition-table" {
			bootCritical = append(bootCritical, image.Name)
			continue
		}
		normal = append(normal, image.Name)
	}
	// Put the factory App after data partitions and only then commit the
	// boot-critical interpretation metadata. This is the only supported split
	// full-install sequence for the current single-slot hardware profile.
	sort.SliceStable(normal, func(i, j int) bool {
		if normal[i] == "app" {
			return false
		}
		if normal[j] == "app" {
			return true
		}
		return normal[i] < normal[j]
	})
	return images, append(normal, "partition-table", "bootloader"), nil
}

func splitImageIdentity(offset uint64, layout partition.Table) (string, string) {
	switch offset {
	case 0:
		return "bootloader", "bootloader"
	case 0x8000:
		return "partition-table", "partition-table"
	}
	for _, entry := range layout.Entries {
		if offset >= uint64(entry.Offset) && offset < uint64(entry.Offset)+uint64(entry.Size) {
			if entry.Type == 0 {
				return "app", "app"
			}
			return entry.Label, entry.Label
		}
	}
	return fmt.Sprintf("flash-%x", offset), "flash"
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
