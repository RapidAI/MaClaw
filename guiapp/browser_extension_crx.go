package guiapp

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const companionDevKeyName = "companion-paper-dev.pem"
const companionLocalCRXName = "companion-paper.crx"
const companionUnpackedDirName = "companion-paper"

// companionExtensionDirHook overrides extension source discovery in tests.
var companionExtensionDirHook func() (string, error)

// companionLocalPackage is an unpacked copy of the extension, signed in the
// manifest by this user's dev public key. The store private key is not in the
// repo, so a no-store build cannot reuse the published extension id. A packed
// CRX registered with path+version or external_crx is force-disabled
// (DISABLE_NOT_VERIFIED); the user loads this folder with developer mode.
type companionLocalPackage struct {
	Ready   bool
	Dir     string
	ID      string
	Version string
}

func prepareCompanionLocalPackage(layout companionBrowserLayout) (companionLocalPackage, error) {
	if !companionLocalSideloadNeeded(layout) {
		return companionLocalPackage{}, nil
	}
	dir, err := browserExtensionStateDir()
	if err != nil {
		return companionLocalPackage{}, err
	}
	return buildCompanionLocalPackage(dir)
}

func existingCompanionLocalPackage() companionLocalPackage {
	dir, err := browserExtensionStateDir()
	if err != nil {
		return companionLocalPackage{}
	}
	unpacked := filepath.Join(dir, companionUnpackedDirName)
	if !companionPathIsFile(filepath.Join(unpacked, "manifest.json")) || !companionPathIsFile(filepath.Join(unpacked, "background.js")) {
		return companionLocalPackage{}
	}
	key, err := loadCompanionDevKey(dir)
	if err != nil || key == nil {
		return companionLocalPackage{}
	}
	id, err := companionDevKeyID(key)
	if err != nil || id == "" {
		return companionLocalPackage{}
	}
	version, err := readCompanionExtensionVersion(os.DirFS(unpacked))
	if err != nil || strings.TrimSpace(version) == "" {
		return companionLocalPackage{}
	}
	abs, err := filepath.Abs(unpacked)
	if err != nil {
		abs = unpacked
	}
	return companionLocalPackage{Ready: true, Dir: abs, ID: id, Version: version}
}

func companionLocalSideloadNeeded(layout companionBrowserLayout) bool {
	for _, browser := range []string{"chrome", "edge"} {
		present := browser == "chrome" && strings.TrimSpace(layout.ChromeExe) != "" || browser == "edge" && strings.TrimSpace(layout.EdgeExe) != ""
		if !present {
			continue
		}
		if layout.GOOS == "linux" && browser == "chrome" {
			return true
		}
		if layout.GOOS == "linux" && browser == "edge" && companionPathIsFile(strings.TrimSpace(layout.CRX)) {
			continue
		}
		if companionStoreURL(browser) != "" {
			continue
		}
		return true
	}
	return false
}

func buildCompanionLocalPackage(dir string) (companionLocalPackage, error) {
	source, err := companionExtensionSource()
	if err != nil {
		return companionLocalPackage{}, err
	}
	key, err := loadOrCreateCompanionDevKey(dir)
	if err != nil {
		return companionLocalPackage{}, err
	}
	spki, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return companionLocalPackage{}, err
	}
	id, err := chromeExtensionIDFromManifestKey(base64.StdEncoding.EncodeToString(spki))
	if err != nil {
		return companionLocalPackage{}, err
	}
	version, err := readCompanionExtensionVersion(source)
	if err != nil {
		return companionLocalPackage{}, err
	}
	unpacked := filepath.Join(dir, companionUnpackedDirName)
	if err := writeCompanionUnpacked(unpacked, source, spki); err != nil {
		return companionLocalPackage{}, err
	}
	// A previous card click may have left the packed file that Edge disables.
	_ = os.Remove(filepath.Join(dir, companionLocalCRXName))
	abs, err := filepath.Abs(unpacked)
	if err != nil {
		abs = unpacked
	}
	return companionLocalPackage{Ready: true, Dir: abs, ID: id, Version: version}, nil
}

// companionExtensionSource prefers the live extensions/ folder in a dev tree
// so edits need no rebuild, then falls back to the copy embedded in the binary.
// An installed build has neither a repo tree nor a shipped extensions/ folder.
func companionExtensionSource() (fs.FS, error) {
	if dir, err := companionExtensionSourceDir(); err == nil && strings.TrimSpace(dir) != "" {
		return os.DirFS(dir), nil
	}
	return companionEmbeddedExtensionFS()
}

func companionExtensionSourceDir() (string, error) {
	if companionExtensionDirHook != nil {
		return companionExtensionDirHook()
	}
	var roots []string
	if wd, err := os.Getwd(); err == nil && wd != "" {
		roots = append(roots, wd)
	}
	if exe, err := os.Executable(); err == nil && exe != "" {
		roots = append(roots, filepath.Dir(exe))
	}
	seen := map[string]bool{}
	for _, root := range roots {
		dir := root
		for i := 0; i < 8; i++ {
			if dir == "" || seen[dir] {
				break
			}
			seen[dir] = true
			manifest := filepath.Join(dir, "extensions", "companion-paper", "manifest.json")
			if companionPathIsFile(manifest) {
				return filepath.Dir(manifest), nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return "", nil
}

func readCompanionExtensionVersion(source fs.FS) (string, error) {
	body, err := fs.ReadFile(source, "manifest.json")
	if err != nil {
		return "", err
	}
	var doc struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", err
	}
	version := strings.TrimSpace(doc.Version)
	if version == "" {
		version = browserExtensionVersion
	}
	return version, nil
}

func loadOrCreateCompanionDevKey(dir string) (*rsa.PrivateKey, error) {
	if key, err := loadCompanionDevKey(dir); err != nil {
		return nil, err
	} else if key != nil {
		return key, nil
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	body := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := writeCompanionFile(filepath.Join(dir, companionDevKeyName), body); err != nil {
		return nil, err
	}
	return key, nil
}

func loadCompanionDevKey(dir string) (*rsa.PrivateKey, error) {
	body, err := os.ReadFile(filepath.Join(dir, companionDevKeyName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	block, _ := pem.Decode(body)
	if block == nil {
		return nil, os.ErrInvalid
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func companionDevExtensionID() string {
	dir, err := browserExtensionStateDir()
	if err != nil {
		return ""
	}
	key, err := loadCompanionDevKey(dir)
	if err != nil || key == nil {
		return ""
	}
	id, err := companionDevKeyID(key)
	if err != nil {
		return ""
	}
	return id
}

func companionDevKeyID(key *rsa.PrivateKey) (string, error) {
	spki, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", err
	}
	return chromeExtensionIDFromManifestKey(base64.StdEncoding.EncodeToString(spki))
}

func writeCompanionUnpacked(dest string, source fs.FS, spki []byte) error {
	files, err := companionExtensionFiles(source, spki)
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for rel, body := range files {
		keep[rel] = true
		if err := writeCompanionFile(filepath.Join(dest, filepath.FromSlash(rel)), body); err != nil {
			return err
		}
	}
	return pruneCompanionUnpacked(dest, keep)
}

func companionExtensionFiles(source fs.FS, spki []byte) (map[string][]byte, error) {
	manifest, err := fs.ReadFile(source, "manifest.json")
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(manifest, &doc); err != nil {
		return nil, err
	}
	doc["key"] = base64.StdEncoding.EncodeToString(spki)
	rewritten, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{"manifest.json": append(rewritten, '\n')}
	err = fs.WalkDir(source, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if name == "manifest.json" || strings.HasPrefix(name, ".") {
			return nil
		}
		if strings.HasSuffix(name, ".mjs") || strings.Contains(name, "_test") {
			return nil
		}
		body, err := fs.ReadFile(source, path)
		if err != nil {
			return err
		}
		out[path] = body
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func pruneCompanionUnpacked(dest string, keep map[string]bool) error {
	if !companionPathIsDir(dest) {
		return nil
	}
	var stale []string
	err := filepath.WalkDir(dest, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dest, path)
		if err != nil {
			return err
		}
		if !keep[filepath.ToSlash(rel)] {
			stale = append(stale, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, path := range stale {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
