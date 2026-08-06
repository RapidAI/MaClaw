// Package firmware validates immutable .clawfw firmware archives before any device operation.
package firmware

import (
	"archive/zip"
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
)

const MaxManifestBytes = 1024 * 1024
const MaxEntries = 64

// Release packages currently contain a <=16 MiB complete image plus small
// metadata. These caps leave room for future profiles while rejecting offline
// ZIP bombs before decompression can consume host storage or memory.
const MaxArchiveBytes int64 = 128 * 1024 * 1024
const MaxUncompressedBytes uint64 = 64 * 1024 * 1024
const MaxFileBytes int64 = 32 * 1024 * 1024

var ErrUnsupportedSchema = errors.New("unsupported firmware manifest schema")

const (
	ModeFull    = "full"
	ModeAppOnly = "app-only"
)

type Manifest struct {
	SchemaVersion    int              `json:"schemaVersion"`
	PackageID        string           `json:"packageId"`
	ReleaseVersion   string           `json:"releaseVersion"`
	Board            Board            `json:"board"`
	Chip             Chip             `json:"chip"`
	SecurityBaseline SecurityBaseline `json:"securityBaseline"`
	Layout           Layout           `json:"layout"`
	Mode             string           `json:"mode"`
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
	Path   string  `json:"path"`
	Size   int64   `json:"size"`
	SHA256 string  `json:"sha256"`
	Offset *uint64 `json:"offset,omitempty"`
	Region string  `json:"region,omitempty"`
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
	if !json.Valid(raw) {
		return Verified{}, errors.New("manifest is not valid JSON")
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
	if !ed25519.Verify(key, manifestRaw, sig) {
		return Verified{}, errors.New("manifest signature verification failed")
	}
	if _, err := InstallPlanFor(v.Manifest); err != nil {
		return Verified{}, err
	}
	return v, nil
}

// InstallPlanFor validates the release-defined install mode and gives the UI
// the exact, conservative impact text. Full mode may change boot-critical
// regions and requires ROM recovery after interruption. App-only must contain
// only an app image and promises to preserve NVS/storage by not writing them.
func InstallPlanFor(m Manifest) (InstallPlan, error) {
	switch m.Mode {
	case ModeFull:
		flashImages := 0
		for _, file := range m.Files {
			if file.Region == "metadata" {
				continue
			}
			if file.Region != "flash" || file.Offset == nil || *file.Offset != 0 {
				return InstallPlan{}, errors.New("full package must contain exactly one complete flash image at offset 0")
			}
			flashImages++
		}
		if flashImages != 1 {
			return InstallPlan{}, errors.New("full package must contain exactly one complete flash image")
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
