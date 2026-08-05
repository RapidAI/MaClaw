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

var ErrUnsupportedSchema = errors.New("unsupported firmware manifest schema")

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
type SignatureEnvelope struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyId"`
	Signature string `json:"signature"`
}
type TrustStore map[string]ed25519.PublicKey

// Verify rejects archive traversal, unlisted entries, invalid hashes and ambiguous paths.
// Signature verification is intentionally separate: this package never treats an unsigned archive as a release package.
func Verify(pathname string) (Verified, error) {
	r, err := zip.OpenReader(pathname)
	if err != nil {
		return Verified{}, fmt.Errorf("open .clawfw: %w", err)
	}
	defer r.Close()
	if len(r.File) > MaxEntries {
		return Verified{}, fmt.Errorf("archive has %d entries, maximum is %d", len(r.File), MaxEntries)
	}
	files := map[string]*zip.File{}
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
	return v, nil
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
	return io.ReadAll(io.LimitReader(r, int64(max)+1))
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
