package flash

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const maxSidecarManifestBytes = 256 * 1024

var ErrSidecarInvalid = errors.New("packaged esptool sidecar is unavailable or invalid")

func ConfigureSidecarTrust(keyID, publicKeyBase64 string) {
	configuredSidecar.Lock()
	defer configuredSidecar.Unlock()
	configuredSidecar.keyID = keyID
	configuredSidecar.publicKeyBase64 = publicKeyBase64
}

type sidecarConfig struct {
	executable      string
	production      bool
	keyID           string
	publicKeyBase64 string
}

var configuredSidecar struct {
	sync.RWMutex
	sidecarConfig
}

type sidecarManifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	Tools         []sidecarRecord   `json:"tools"`
	Signature     *sidecarSignature `json:"signature,omitempty"`
}

type sidecarSignature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyId"`
	Signature string `json:"signature"`
}

type sidecarRecord struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Version string `json:"version"`
}

func ConfigureSidecar(executable string, production bool) {
	configuredSidecar.Lock()
	defer configuredSidecar.Unlock()
	configuredSidecar.executable = executable
	configuredSidecar.production = production
}

func currentSidecarConfig() sidecarConfig {
	configuredSidecar.RLock()
	defer configuredSidecar.RUnlock()
	return configuredSidecar.sidecarConfig
}

func managedTool(executable string) (Tool, error) {
	return managedToolForConfig(sidecarConfig{executable: executable})
}

func managedToolForConfig(config sidecarConfig) (Tool, error) {
	executable := config.executable
	if executable == "" {
		return Tool{}, fmt.Errorf("%w: application executable path is unavailable", ErrSidecarInvalid)
	}
	root := filepath.Join(filepath.Dir(executable), "tools")
	manifestPath := filepath.Join(root, "sidecar-manifest.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return Tool{}, fmt.Errorf("%w: read manifest: %v", ErrSidecarInvalid, err)
	}
	if len(raw) == 0 || len(raw) > maxSidecarManifestBytes {
		return Tool{}, fmt.Errorf("%w: manifest has invalid size", ErrSidecarInvalid)
	}
	var manifest sidecarManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return Tool{}, fmt.Errorf("%w: decode manifest: %v", ErrSidecarInvalid, err)
	}
	if manifest.SchemaVersion != 1 {
		return Tool{}, fmt.Errorf("%w: unsupported manifest schema", ErrSidecarInvalid)
	}
	if config.production {
		if err := verifySidecarManifest(manifest, config); err != nil {
			return Tool{}, fmt.Errorf("%w: manifest signature: %v", ErrSidecarInvalid, err)
		}
	}
	expected := "esptool"
	if runtime.GOOS == "windows" {
		expected += ".exe"
	}
	for _, record := range manifest.Tools {
		if record.Name != "esptool" || record.Path != expected || !validSidecarHash(record.SHA256) {
			continue
		}
		candidate := filepath.Join(root, record.Path)
		if filepath.Clean(candidate) != candidate {
			continue
		}
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || (info.Mode()&0111 == 0 && runtime.GOOS != "windows") {
			return Tool{}, fmt.Errorf("%w: binary is missing or not executable", ErrSidecarInvalid)
		}
		sum, err := fileSHA256(candidate)
		if err != nil || !strings.EqualFold(record.SHA256, "sha256:"+sum) {
			return Tool{}, fmt.Errorf("%w: binary hash does not match manifest", ErrSidecarInvalid)
		}
		return Tool{Path: candidate, Version: record.Version}, nil
	}
	return Tool{}, fmt.Errorf("%w: no matching esptool record", ErrSidecarInvalid)
}

func verifySidecarManifest(manifest sidecarManifest, config sidecarConfig) error {
	if manifest.Signature == nil || manifest.Signature.Algorithm != "ed25519" || manifest.Signature.KeyID == "" || manifest.Signature.Signature == "" {
		return errors.New("missing or invalid signature envelope")
	}
	if config.keyID == "" || manifest.Signature.KeyID != config.keyID {
		return errors.New("signature key ID does not match the official release")
	}
	publicRaw, err := base64.StdEncoding.DecodeString(config.publicKeyBase64)
	if err != nil || len(publicRaw) != ed25519.PublicKeySize {
		return errors.New("official release public key is unavailable")
	}
	signature, err := base64.StdEncoding.DecodeString(manifest.Signature.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return errors.New("invalid signature encoding")
	}
	payload, err := sidecarManifestPayload(manifest)
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(publicRaw), payload, signature) {
		return errors.New("signature verification failed")
	}
	return nil
}

func sidecarManifestPayload(manifest sidecarManifest) ([]byte, error) {
	return json.Marshal(struct {
		SchemaVersion int             `json:"schemaVersion"`
		Tools         []sidecarRecord `json:"tools"`
	}{SchemaVersion: manifest.SchemaVersion, Tools: manifest.Tools})
}

func validSidecarHash(value string) bool {
	if !strings.HasPrefix(strings.ToLower(value), "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	_, err := hex.DecodeString(value[len("sha256:"):])
	return err == nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
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
