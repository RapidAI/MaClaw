// Package firmware validates immutable .clawfw firmware archives before any device operation.
package firmware

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"clawmatemaker/internal/partition"
)

const MaxManifestBytes = 1024 * 1024
const MaxEntries = 64

// Release packages can contain a <=32 MiB complete image plus small metadata.
// These caps cover every supported profile while rejecting offline ZIP bombs
// before decompression can consume host storage or memory.
const MaxArchiveBytes int64 = 128 * 1024 * 1024
const MaxUncompressedBytes uint64 = 64 * 1024 * 1024
const MaxFileBytes int64 = 32 * 1024 * 1024
const partitionTableFlashOffset = 0x8000

var ErrUnsupportedSchema = errors.New("unsupported firmware manifest schema")

const (
	ModeFull      = "full"
	ModeAppOnly   = "app-only"
	ChannelStable = "stable"
	ChannelBeta   = "beta"
)

type Manifest struct {
	SchemaVersion    int              `json:"schemaVersion"`
	PackageID        string           `json:"packageId"`
	ReleaseVersion   string           `json:"releaseVersion"`
	Channel          string           `json:"channel"`
	Board            Board            `json:"board"`
	Chip             Chip             `json:"chip"`
	SecurityBaseline SecurityBaseline `json:"securityBaseline"`
	Layout           Layout           `json:"layout"`
	Mode             string           `json:"mode"`
	// Recovery is a signed assertion about interruption behavior.  It is
	// intentionally distinct from InstallPlan.RequiresRecovery: the latter
	// tells the UI whether recovery must be offered, while this field states
	// whether an interrupted installation is expected to remain bootable.
	Recovery Recovery `json:"recovery"`
	// WriteOrder is the signed order for independent full-install images. It
	// is intentionally a list of immutable file names rather than archive
	// order: ZIP ordering is neither a deployment contract nor safe around
	// boot-critical regions. Legacy one-file full images leave this empty.
	WriteOrder       []string         `json:"writeOrder,omitempty"`
	AppIdentity      AppIdentity      `json:"appIdentity"`
	BootVerification BootVerification `json:"bootVerification"`
	Files            []FileSpec       `json:"files"`
}
type Layout struct {
	ID                 string `json:"id"`
	Fingerprint        string `json:"fingerprint"`
	PartitionTablePath string `json:"partitionTablePath"`
}
type AppIdentity struct {
	ProjectName     string `json:"projectName"`
	AppVersion      string `json:"appVersion"`
	ELFSHA256       string `json:"elfSha256"`
	ReleaseSequence int64  `json:"releaseSequence"`
	PSRAMBytes      int64  `json:"psramBytes"`
}
type BootVerification struct {
	Baud              int      `json:"baud"`
	RequiredSelfTests []string `json:"requiredSelfTests"`
	TimeoutSeconds    int      `json:"timeoutSeconds"`
}
type Recovery struct {
	PowerLossBootable bool `json:"powerLossBootable"`
}

type Board struct {
	ID          string `json:"id"`
	ProfileHash string `json:"profileHash"`
}
type Chip struct {
	Family     string `json:"family"`
	FlashBytes int64  `json:"flashBytes"`
}
type SecurityBaseline struct {
	SecureBoot      bool `json:"secureBoot"`
	FlashEncryption bool `json:"flashEncryption"`
	SecureVersion   int  `json:"secureVersion"`
}
type FileSpec struct {
	Name   string  `json:"name,omitempty"`
	Path   string  `json:"path"`
	Size   int64   `json:"size"`
	SHA256 string  `json:"sha256"`
	Offset *uint64 `json:"offset,omitempty"`
	Region string  `json:"region,omitempty"`
}

// NormalizeSHA256 accepts a SHA-256 digest with an optional sha256: prefix
// and returns its canonical lower-case hexadecimal form. Firmware runtime
// status frames use the bare form, while archive file checksums keep their
// historical sha256: prefix; callers choose the wire format they need.
func NormalizeSHA256(raw string) (string, bool) {
	value := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), "sha256:")
	if len(value) != sha256.Size*2 {
		return "", false
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", false
	}
	return value, true
}

type Verified struct {
	Manifest       Manifest `json:"manifest"`
	ManifestSHA256 string   `json:"manifestSha256"`
	ArchiveSHA256  string   `json:"archiveSha256"`
}

// InstallPlan is the signed package's user-visible impact summary. It is
// derived only after archive and signature validation; the frontend never
// supplies the mode or decides what is safe to preserve.
type InstallPlan struct {
	Mode              string `json:"mode"`
	RequiresRecovery  bool   `json:"requiresRecovery"`
	PreservesUserData bool   `json:"preservesUserData"`
	Summary           string `json:"summary"`
	Warning           string `json:"warning,omitempty"`
}
type SignatureEnvelope struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyId"`
	Signature string `json:"signature"`
}
type TrustStore map[string]ed25519.PublicKey

// Verify rejects archive traversal, unlisted entries, invalid hashes and ambiguous paths.
// Signature verification is intentionally separate: this package never treats an unsigned archive as a release package.
func Verify(pathname string) (Verified, error) {
	info, err := os.Stat(pathname)
	if err != nil {
		return Verified{}, fmt.Errorf("stat .clawfw: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > MaxArchiveBytes {
		return Verified{}, errors.New("firmware archive has an invalid size or file type")
	}
	r, err := zip.OpenReader(pathname)
	if err != nil {
		return Verified{}, fmt.Errorf("open .clawfw: %w", err)
	}
	defer r.Close()
	if len(r.File) > MaxEntries {
		return Verified{}, fmt.Errorf("archive has %d entries, maximum is %d", len(r.File), MaxEntries)
	}
	files := map[string]*zip.File{}
	var uncompressed uint64
	for _, f := range r.File {
		name, err := safePath(f.Name)
		if err != nil {
			return Verified{}, err
		}
		key := strings.ToLower(name)
		if _, ok := files[key]; ok {
			return Verified{}, fmt.Errorf("duplicate or case-colliding entry: %s", name)
		}
		if f.FileInfo().IsDir() {
			return Verified{}, fmt.Errorf("directory entries are not allowed: %s", name)
		}
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			return Verified{}, fmt.Errorf("symbolic-link entries are not allowed: %s", name)
		}
		if f.UncompressedSize64 > MaxUncompressedBytes-uncompressed {
			return Verified{}, errors.New("firmware archive exceeds uncompressed size limit")
		}
		uncompressed += f.UncompressedSize64
		files[key] = f
	}
	mf, ok := files["manifest.json"]
	if !ok {
		return Verified{}, errors.New("manifest.json is missing")
	}
	if mf.UncompressedSize64 > MaxManifestBytes {
		return Verified{}, errors.New("manifest exceeds size limit")
	}
	raw, err := readZip(mf, MaxManifestBytes)
	if err != nil {
		return Verified{}, err
	}
	if len(raw) > MaxManifestBytes {
		return Verified{}, errors.New("manifest exceeds size limit")
	}
	if !json.Valid(raw) || hasUTF8BOM(raw) {
		return Verified{}, errors.New("manifest is not valid JSON")
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return Verified{}, fmt.Errorf("manifest JSON is ambiguous: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Verified{}, fmt.Errorf("decode manifest: %w", err)
	}
	if m.SchemaVersion != 1 {
		return Verified{}, ErrUnsupportedSchema
	}
	if m.PackageID == "" || len(m.Files) == 0 {
		return Verified{}, errors.New("manifest packageId and files are required")
	}
	listed := map[string]bool{}
	for _, spec := range m.Files {
		name, err := safePath(spec.Path)
		if err != nil {
			return Verified{}, fmt.Errorf("invalid manifest file: %w", err)
		}
		key := strings.ToLower(name)
		if listed[key] {
			return Verified{}, fmt.Errorf("duplicate manifest path: %s", name)
		}
		listed[key] = true
		f, ok := files[key]
		if !ok {
			return Verified{}, fmt.Errorf("manifest file missing from archive: %s", name)
		}
		if int64(f.UncompressedSize64) != spec.Size {
			return Verified{}, fmt.Errorf("size mismatch for %s", name)
		}
		// Validate declared bounds before opening/decompressing the entry. A
		// signed manifest is still untrusted until its signature is checked,
		// and Verify is also used by tooling that inspects unsigned archives.
		if err := validateFileSpec(spec); err != nil {
			return Verified{}, fmt.Errorf("invalid manifest file %s: %w", name, err)
		}
		sum, err := hashZip(f, spec.Size)
		if err != nil {
			return Verified{}, err
		}
		if !hashEqual(sum, spec.SHA256) {
			return Verified{}, fmt.Errorf("sha256 mismatch for %s", name)
		}
	}
	for key := range files {
		if key == "manifest.json" || key == "manifest.sig" {
			continue
		}
		if !listed[key] {
			return Verified{}, fmt.Errorf("unlisted archive entry: %s", files[key].Name)
		}
	}
	manifestHash := sha256.Sum256(raw)
	archiveHash, err := fileHash(pathname)
	if err != nil {
		return Verified{}, err
	}
	return Verified{Manifest: m, ManifestSHA256: "sha256:" + hex.EncodeToString(manifestHash[:]), ArchiveSHA256: "sha256:" + archiveHash}, nil
}

func validateFileSpec(spec FileSpec) error {
	if spec.Size <= 0 || spec.Size > MaxFileBytes || spec.Region == "" {
		return errors.New("positive size and region are required")
	}
	if spec.Region == "metadata" {
		if spec.Offset != nil {
			return errors.New("metadata file must not declare a flash offset")
		}
		return nil
	}
	if spec.Offset == nil {
		return errors.New("flash image offset is required")
	}
	if *spec.Offset%0x1000 != 0 {
		return errors.New("flash image offset must be 4 KiB aligned")
	}
	return nil
}

// VerifyRelease performs all archive checks and verifies manifest.sig against an
// application-provided public-key allow-list. There is no unsigned release path.
func VerifyRelease(pathname string, trust TrustStore) (Verified, error) {
	v, err := Verify(pathname)
	if err != nil {
		return Verified{}, err
	}
	r, err := zip.OpenReader(pathname)
	if err != nil {
		return Verified{}, err
	}
	defer r.Close()
	var sigFile *zip.File
	for _, f := range r.File {
		if f.Name == "manifest.sig" {
			sigFile = f
			break
		}
	}
	if sigFile == nil {
		return Verified{}, errors.New("manifest.sig is required for a release package")
	}
	rawSig, err := readZip(sigFile, 16*1024)
	if err != nil {
		return Verified{}, err
	}
	var env SignatureEnvelope
	if err := json.Unmarshal(rawSig, &env); err != nil {
		return Verified{}, fmt.Errorf("decode manifest.sig: %w", err)
	}
	if env.Algorithm != "ed25519" || env.KeyID == "" || env.Signature == "" {
		return Verified{}, errors.New("unsupported or incomplete manifest signature")
	}
	key, ok := trust[env.KeyID]
	if !ok || len(key) != ed25519.PublicKeySize {
		return Verified{}, fmt.Errorf("untrusted manifest signing key: %s", env.KeyID)
	}
	sig, err := base64.StdEncoding.DecodeString(env.Signature)
	if err != nil {
		return Verified{}, errors.New("invalid manifest signature encoding")
	}
	var manifestRaw []byte
	for _, f := range r.File {
		if f.Name == "manifest.json" {
			manifestRaw, err = readZip(f, MaxManifestBytes)
			break
		}
	}
	if err != nil {
		return Verified{}, err
	}
	if err := validateReleaseManifestSecurityBaseline(manifestRaw); err != nil {
		return Verified{}, err
	}
	if !ed25519.Verify(key, manifestRaw, sig) {
		return Verified{}, errors.New("manifest signature verification failed")
	}
	if err := ValidateReleaseManifest(v.Manifest, manifestRaw); err != nil {
		return Verified{}, err
	}
	if err := validateReleaseImageLayout(v.Manifest, r); err != nil {
		return Verified{}, err
	}
	if err := validateReleasePartitionLayout(v.Manifest, r); err != nil {
		return Verified{}, err
	}
	return v, nil
}

// validateReleaseImageLayout binds the separately described partition-table
// metadata to the bytes which a complete package will actually write at the
// ESP-IDF partition-table flash offset. Without this check a valid signature
// could describe one layout while its full-flash image installs another.
// App-only packages deliberately do not carry a partition-table write, and
// their preservation boundary is verified against the device's existing table
// by FlashJob immediately before writing.
func validateReleaseImageLayout(m Manifest, archive *zip.ReadCloser) error {
	if m.Mode != ModeFull {
		return nil
	}
	var fullImage, table, splitTable *zip.File
	for _, file := range m.Files {
		for _, entry := range archive.File {
			if entry.Name != file.Path {
				continue
			}
			if file.Path == m.Layout.PartitionTablePath {
				table = entry
			}
			if file.Region == "flash" && file.Offset != nil && *file.Offset == 0 {
				fullImage = entry
			}
			if file.Name == "partition-table" && file.Offset != nil && *file.Offset == partitionTableFlashOffset {
				splitTable = entry
			}
		}
	}
	if fullImage == nil {
		// Split packages carry the partition table as the actual named image;
		// its bounds and order have already been validated by InstallPlanFor.
		// They do not contain a merged byte stream at offset 0, so there is no
		// second copy at 0x8000 to compare. Its standalone partition-table
		// image must nevertheless match the signed metadata exactly.
		if table == nil || splitTable == nil {
			return errors.New("split full release partition table image or metadata is missing")
		}
		metadata, err := readZip(table, uint64(MaxFileBytes))
		if err != nil {
			return fmt.Errorf("read split release partition table metadata: %w", err)
		}
		written, err := readZip(splitTable, uint64(MaxFileBytes))
		if err != nil {
			return fmt.Errorf("read split release partition table image: %w", err)
		}
		if !bytes.Equal(metadata, written) {
			return errors.New("split release partition table image differs from declared metadata")
		}
		return nil
	}
	if table == nil {
		return errors.New("release full image or partition table metadata is missing")
	}
	tableRaw, err := readZip(table, uint64(MaxFileBytes))
	if err != nil {
		return fmt.Errorf("read release partition table metadata: %w", err)
	}
	if len(tableRaw) == 0 || int64(len(tableRaw)) > MaxFileBytes-partitionTableFlashOffset {
		return errors.New("release partition table metadata has an invalid size")
	}
	imageRaw, err := readZip(fullImage, uint64(MaxFileBytes))
	if err != nil {
		return fmt.Errorf("read release full image: %w", err)
	}
	end := partitionTableFlashOffset + len(tableRaw)
	if len(imageRaw) < end {
		return errors.New("release full image does not contain its partition table")
	}
	if !bytes.Equal(imageRaw[partitionTableFlashOffset:end], tableRaw) {
		return errors.New("release full image partition table differs from declared metadata")
	}
	return nil
}

// validateReleasePartitionLayout makes the signed region labels executable
// policy rather than merely UI text.  A package manifest can be correctly
// signed yet still be operationally unsafe if (for example) an image called
// "storage" reaches outside the storage partition.  Parse the exact table
// included in the package and bind every modern split-image range to it while
// the archive is being accepted, before it can enter the download cache or an
// offline-package capability.
//
// The legacy merged full image has no independently addressable partition
// ranges; it remains supported only through the explicitly narrow legacy
// branch in InstallPlanFor.  New CI output always uses the split plan checked
// below.
func validateReleasePartitionLayout(m Manifest, archive *zip.ReadCloser) error {
	// App-only packages deliberately retain the installed partition table. The
	// metadata copy is used by FlashJob to compare the live layout immediately
	// before the write, but it need not be an independently flashable full
	// table in older app-only packages. Split/full packages, in contrast,
	// execute regions against this table and must be bound here.
	if m.Mode != ModeFull || len(m.WriteOrder) == 0 {
		return nil
	}
	var tableFile *zip.File
	for _, file := range m.Files {
		if file.Path != m.Layout.PartitionTablePath {
			continue
		}
		for _, entry := range archive.File {
			if entry.Name == file.Path {
				tableFile = entry
				break
			}
		}
		break
	}
	if tableFile == nil {
		return errors.New("release partition table metadata is missing")
	}
	raw, err := readZip(tableFile, uint64(MaxFileBytes))
	if err != nil {
		return fmt.Errorf("read release partition table metadata: %w", err)
	}
	table, err := partition.Parse(raw, uint64(m.Chip.FlashBytes))
	if err != nil {
		return fmt.Errorf("parse release partition table metadata: %w", err)
	}
	if table.Fingerprint != m.Layout.Fingerprint {
		return errors.New("release partition table metadata does not match declared layout fingerprint")
	}

	flashImages := make([]FileSpec, 0, len(m.Files))
	for _, file := range m.Files {
		if file.Region != "metadata" {
			flashImages = append(flashImages, file)
		}
	}
	if m.Mode == ModeFull && len(flashImages) == 1 && flashImages[0].Region == "flash" && flashImages[0].Offset != nil && *flashImages[0].Offset == 0 && len(m.WriteOrder) == 0 {
		return nil
	}

	factory, hasFactory := partition.Find(table.Entries, "factory")
	for _, file := range flashImages {
		if file.Offset == nil || file.Size <= 0 {
			return errors.New("release flash image has no valid range")
		}
		start := *file.Offset
		size := uint64(file.Size)
		if size > ^uint64(0)-start {
			return fmt.Errorf("release image %s range overflows", file.Path)
		}
		end := start + size
		if end > uint64(m.Chip.FlashBytes) {
			return fmt.Errorf("release image %s exceeds declared flash capacity", file.Path)
		}
		switch file.Region {
		case "bootloader":
			if file.Name != "bootloader" || start != 0 || end > partitionTableFlashOffset {
				return errors.New("release bootloader image must occupy only the ROM bootloader range")
			}
		case "partition-table":
			if file.Name != "partition-table" || start != partitionTableFlashOffset || end > partitionTableFlashOffset+partition.EntrySize*128 {
				return errors.New("release partition-table image must occupy the ESP-IDF partition table range")
			}
		case "app":
			if file.Name != "app" || !hasFactory || start < uint64(factory.Offset) || end > uint64(factory.Offset)+uint64(factory.Size) {
				return errors.New("release app image must be contained in the factory app partition")
			}
		default:
			entry, found := partition.Find(table.Entries, file.Region)
			if !found || start < uint64(entry.Offset) || end > uint64(entry.Offset)+uint64(entry.Size) {
				return fmt.Errorf("release image %s is outside declared %s partition", file.Path, file.Region)
			}
		}
	}
	return nil
}

// ValidateReleaseManifest is the semantic acceptance boundary for a signed
// release package. VerifyRelease calls it before a download/import can mint a
// firmware capability; FlashJob repeats device-dependent checks immediately
// before writing. Keep ordinary Verify deliberately structural so archive
// inspection tooling can still report malformed or unsigned containers.
func ValidateReleaseManifest(m Manifest, raw []byte) error {
	if strings.TrimSpace(m.PackageID) == "" || strings.TrimSpace(m.ReleaseVersion) == "" || !ValidReleaseChannel(m.Channel) {
		return errors.New("release manifest packageId, releaseVersion and supported channel are required")
	}
	if strings.TrimSpace(m.Board.ID) == "" || strings.TrimSpace(m.Board.ProfileHash) == "" {
		return errors.New("release manifest board identity is required")
	}
	if strings.TrimSpace(m.Chip.Family) == "" || m.Chip.FlashBytes <= 0 {
		return errors.New("release manifest chip family and flash capacity are required")
	}
	if strings.TrimSpace(m.Layout.ID) == "" || strings.TrimSpace(m.Layout.Fingerprint) == "" || strings.TrimSpace(m.Layout.PartitionTablePath) == "" {
		return errors.New("release manifest layout identity and partition table are required")
	}
	if strings.TrimSpace(m.AppIdentity.ProjectName) == "" || strings.TrimSpace(m.AppIdentity.AppVersion) == "" || m.AppIdentity.ReleaseSequence <= 0 || m.AppIdentity.PSRAMBytes <= 0 {
		return errors.New("release manifest application identity is incomplete")
	}
	canonicalELF, validELF := NormalizeSHA256(m.AppIdentity.ELFSHA256)
	if !validELF || m.AppIdentity.ELFSHA256 != canonicalELF {
		return errors.New("release manifest appIdentity.elfSha256 must be a canonical lower-case SHA-256 digest")
	}
	if m.BootVerification.Baud <= 0 || m.BootVerification.TimeoutSeconds <= 0 || len(m.BootVerification.RequiredSelfTests) == 0 {
		return errors.New("release manifest boot verification policy is incomplete")
	}
	seenSelfTests := make(map[string]struct{}, len(m.BootVerification.RequiredSelfTests))
	for _, name := range m.BootVerification.RequiredSelfTests {
		name = strings.TrimSpace(name)
		if name == "" {
			return errors.New("release manifest boot verification contains an empty self-test")
		}
		if _, exists := seenSelfTests[name]; exists {
			return fmt.Errorf("release manifest boot verification repeats self-test %q", name)
		}
		seenSelfTests[name] = struct{}{}
	}
	if err := validateReleaseManifestSecurityBaseline(raw); err != nil {
		return err
	}
	if err := validateReleaseManifestRecovery(m, raw); err != nil {
		return err
	}
	metadataFound := false
	partitionImageFound := false
	for _, file := range m.Files {
		if file.Path == m.Layout.PartitionTablePath {
			if file.Region != "metadata" || file.Offset != nil {
				return errors.New("release manifest partition table must be metadata without a flash offset")
			}
			metadataFound = true
		}
		if m.Mode == ModeFull && file.Name == "partition-table" && file.Offset != nil && *file.Offset == partitionTableFlashOffset {
			partitionImageFound = true
		}
	}
	if !metadataFound {
		return errors.New("release manifest does not list the declared partition table metadata")
	}
	if m.Mode == ModeFull && len(m.WriteOrder) > 0 && !partitionImageFound {
		return errors.New("split full release manifest has no partition-table image at offset 0x8000")
	}
	_, err := InstallPlanFor(m)
	return err
}

// validateReleaseManifestRecovery makes an interruption guarantee part of
// the signed package rather than an optimistic client-side assumption.  The
// current official profiles use one factory App slot, so no split full
// package may claim that a power loss leaves a bootable prior installation.
// Legacy merged archives retain their narrow compatibility path; every new
// CI-produced split full archive must state this explicitly.
func validateReleaseManifestRecovery(m Manifest, raw []byte) error {
	if m.Mode != ModeFull || len(m.WriteOrder) == 0 {
		return nil
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("decode release manifest: %w", err)
	}
	recoveryRaw, ok := root["recovery"]
	if !ok {
		return errors.New("split full release manifest recovery is required")
	}
	var recovery map[string]json.RawMessage
	if err := json.Unmarshal(recoveryRaw, &recovery); err != nil {
		return errors.New("release manifest recovery must be an object")
	}
	if _, ok := recovery["powerLossBootable"]; !ok {
		return errors.New("release manifest recovery.powerLossBootable is required")
	}
	if m.Recovery.PowerLossBootable {
		return errors.New("single-slot split full release must declare powerLossBootable=false")
	}
	return nil
}

// ValidReleaseChannel accepts only the two public channels represented in the
// protected release workflow. The signed package is the final authority for
// this field, so a stable/beta mirror path cannot relabel a package.
func ValidReleaseChannel(channel string) bool {
	switch channel {
	case ChannelStable, ChannelBeta:
		return true
	default:
		return false
	}
}

func hasUTF8BOM(raw []byte) bool {
	return len(raw) >= 3 && raw[0] == 0xef && raw[1] == 0xbb && raw[2] == 0xbf
}

// rejectDuplicateJSONKeys rejects duplicate object members at every nesting
// depth before decoding into Go structs. encoding/json otherwise silently
// accepts the last spelling, which would make a signed manifest visually
// ambiguous to release reviewers and desktop clients.
func rejectDuplicateJSONKeys(raw []byte) error {
	d := json.NewDecoder(strings.NewReader(string(raw)))
	if err := walkJSONValue(d); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}

func walkJSONValue(d *json.Decoder) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for d.More() {
			keyToken, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate key %q", key)
			}
			seen[key] = struct{}{}
			if err := walkJSONValue(d); err != nil {
				return err
			}
		}
		_, err := d.Token()
		return err
	case '[':
		for d.More() {
			if err := walkJSONValue(d); err != nil {
				return err
			}
		}
		_, err := d.Token()
		return err
	default:
		return errors.New("unexpected JSON delimiter")
	}
}

// validateReleaseManifestSecurityBaseline distinguishes absent JSON members
// from Go's bool/int zero values. Official release packages must state the
// baseline explicitly, so neither old manifests nor a typo can silently mean
// "secure boot disabled, encryption disabled, anti-rollback zero".
func validateReleaseManifestSecurityBaseline(raw []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("decode release manifest: %w", err)
	}
	baselineRaw, ok := root["securityBaseline"]
	if !ok {
		return errors.New("release manifest securityBaseline is required")
	}
	var baseline map[string]json.RawMessage
	if err := json.Unmarshal(baselineRaw, &baseline); err != nil {
		return errors.New("release manifest securityBaseline must be an object")
	}
	for _, field := range []string{"secureBoot", "flashEncryption", "secureVersion"} {
		if _, ok := baseline[field]; !ok {
			return fmt.Errorf("release manifest securityBaseline.%s is required", field)
		}
	}
	var value SecurityBaseline
	if err := json.Unmarshal(baselineRaw, &value); err != nil {
		return fmt.Errorf("decode release manifest securityBaseline: %w", err)
	}
	return ValidateSecurityBaseline(value)
}

// InstallPlanFor validates the release-defined install mode and gives the UI
// the exact, conservative impact text. Full mode may change boot-critical
// regions and requires ROM recovery after interruption. App-only must contain
// only an app image and promises to preserve NVS/storage by not writing them.
func InstallPlanFor(m Manifest) (InstallPlan, error) {
	if err := ValidateSecurityBaseline(m.SecurityBaseline); err != nil {
		return InstallPlan{}, err
	}
	switch m.Mode {
	case ModeFull:
		flashImages := make([]FileSpec, 0, len(m.Files))
		for _, file := range m.Files {
			if file.Region == "metadata" {
				continue
			}
			if file.Offset == nil {
				return InstallPlan{}, errors.New("full package image has no flash offset")
			}
			flashImages = append(flashImages, file)
		}
		if len(flashImages) == 0 {
			return InstallPlan{}, errors.New("full package contains no flash image")
		}
		// Backward-compatible support for the already published single merged
		// image. New CI packages use named independent images and WriteOrder.
		if len(flashImages) == 1 && flashImages[0].Region == "flash" && *flashImages[0].Offset == 0 && len(m.WriteOrder) == 0 {
			return InstallPlan{Mode: m.Mode, RequiresRecovery: true, PreservesUserData: false, Summary: "完整刷写：会按已签名的包写入系统镜像，可能覆盖启动、分区、模型或存储区域。", Warning: "刷写中断后必须进入 ROM 下载模式并使用完整恢复包；当前单 factory App 布局不保证旧版本仍可启动。"}, nil
		}
		if err := validateSplitFullPlan(flashImages, m.WriteOrder); err != nil {
			return InstallPlan{}, err
		}
		return InstallPlan{Mode: m.Mode, RequiresRecovery: true, PreservesUserData: false, Summary: "完整刷写：会按已签名的包写入系统镜像，可能覆盖启动、分区、模型或存储区域。", Warning: "刷写中断后必须进入 ROM 下载模式并使用完整恢复包；当前单 factory App 布局不保证旧版本仍可启动。"}, nil
	case ModeAppOnly:
		appImages := 0
		for _, file := range m.Files {
			if file.Region == "metadata" {
				continue
			}
			if file.Region != "app" {
				return InstallPlan{}, fmt.Errorf("app-only package contains non-app region %s", file.Region)
			}
			if file.Offset == nil {
				return InstallPlan{}, errors.New("app-only package contains an app image without an offset")
			}
			appImages++
		}
		if appImages == 0 {
			return InstallPlan{}, errors.New("app-only package contains no app image")
		}
		return InstallPlan{Mode: m.Mode, RequiresRecovery: true, PreservesUserData: true, Summary: "仅更新应用：只写入 App 分区，保留现有 NVS、配对、Wi-Fi 和 storage 数据。", Warning: "当前为单 factory App 布局。若 App 写入中断或启动未验证，仍需通过 ROM 下载模式执行完整恢复。"}, nil
	default:
		return InstallPlan{}, fmt.Errorf("unsupported firmware install mode %q", m.Mode)
	}
}

func validateSplitFullPlan(images []FileSpec, order []string) error {
	if len(order) != len(images) {
		return errors.New("split full package writeOrder must name every flash image exactly once")
	}
	byName := make(map[string]FileSpec, len(images))
	for _, image := range images {
		name := strings.TrimSpace(image.Name)
		if name == "" {
			return errors.New("split full package image name is required")
		}
		if _, exists := byName[name]; exists {
			return fmt.Errorf("split full package repeats image name %q", name)
		}
		byName[name] = image
	}
	seen := make(map[string]bool, len(order))
	for _, name := range order {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return errors.New("split full package writeOrder contains an empty or duplicate image name")
		}
		if _, exists := byName[name]; !exists {
			return fmt.Errorf("split full package writeOrder references unknown image %q", name)
		}
		seen[name] = true
	}
	// Committing partition interpretation and bootloader data ahead of their
	// dependent App/data images makes power loss harder to recover from. The
	// CI-produced plan always puts them last; enforce that policy here rather
	// than trusting a package author to remember it.
	for index, name := range order {
		if name != "partition-table" && name != "bootloader" {
			continue
		}
		for _, later := range order[index+1:] {
			if later != "partition-table" && later != "bootloader" {
				return fmt.Errorf("boot-critical image %q must appear after non-boot-critical image %q", name, later)
			}
		}
	}
	return nil
}

// ValidateSecurityBaseline fixes the first official package profile to the
// only eFuse state that the desktop installer can safely reason about. Keep
// this at the signed-package boundary as well as the live-device boundary:
// accepting a package that claims a different security posture and rejecting
// it only later creates an unsafe-looking download/install experience.
func ValidateSecurityBaseline(baseline SecurityBaseline) error {
	if baseline.SecureBoot || baseline.FlashEncryption || baseline.SecureVersion != 0 {
		return errors.New("unsupported firmware security baseline")
	}
	return nil
}
func safePath(name string) (string, error) {
	if name == "" || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("unsafe archive path: %q", name)
	}
	clean := path.Clean(name)
	if clean == "." || clean != name || strings.HasPrefix(clean, "../") || strings.Contains(clean, "//") {
		return "", fmt.Errorf("unsafe archive path: %q", name)
	}
	return clean, nil
}
func readZip(f *zip.File, max uint64) ([]byte, error) {
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	raw, err := io.ReadAll(io.LimitReader(r, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(raw)) > max {
		return nil, fmt.Errorf("archive entry %s exceeds size limit", f.Name)
	}
	return raw, nil
}
func hashZip(f *zip.File, size int64) (string, error) {
	r, err := f.Open()
	if err != nil {
		return "", err
	}
	defer r.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(r, size+1))
	if err != nil {
		return "", err
	}
	if n != size {
		return "", fmt.Errorf("unexpected data length for %s", f.Name)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
func hashEqual(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
func fileHash(pathname string) (string, error) {
	f, err := os.Open(pathname)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func SortedPaths(m Manifest) []string {
	v := make([]string, 0, len(m.Files))
	for _, f := range m.Files {
		v = append(v, f.Path)
	}
	sort.Strings(v)
	return v
}
