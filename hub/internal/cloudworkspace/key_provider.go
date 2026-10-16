package cloudworkspace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"
)

const (
	keyringEnv        = "MACLAW_CWS_KEYRING"
	masterKeyringFile = "master-keyring.json"
	keyringVersion    = 1
	maxKeyVersions    = 64
)

var (
	ErrKeyUnavailable = errors.New("cloud workspace encryption key unavailable")
	fileKeyringMu     sync.Mutex
)

// KeyMaterial is returned only inside the Hub process. Key IDs are stable
// SHA-256 fingerprints; Bytes must never be logged or serialized in API
// responses.
type KeyMaterial struct {
	ID    string
	Bytes []byte
}

// KeyProvider is the injection boundary for file, environment/secret, and
// external KMS-backed implementations. Providers must return the active key
// first from AllKeys so legacy v1 ciphertext can be tried deterministically.
type KeyProvider interface {
	ProviderName() string
	ActiveKey(context.Context) (KeyMaterial, error)
	Key(context.Context, string) (KeyMaterial, error)
	AllKeys(context.Context) ([]KeyMaterial, error)
	Descriptor(context.Context) (MasterKeyDescriptor, error)
}

// MasterKeyDescriptor is safe backup/status metadata. KeyID identifies the
// active key; KeyCount proves whether all versions required by a
// mixed-generation ciphertext tree were captured.
type MasterKeyDescriptor struct {
	Provider      string `json:"provider"`
	KeyID         string `json:"key_id"`
	KeyCount      int    `json:"key_count"`
	Envelope      string `json:"envelope"`
	RotationKeyID string `json:"rotation_key_id,omitempty"`
}

type keyringFile struct {
	Version       int            `json:"version"`
	ActiveKeyID   string         `json:"active_key_id"`
	RotationKeyID string         `json:"rotation_key_id,omitempty"`
	Keys          []keyringEntry `json:"keys"`
}

type keyringEntry struct {
	ID        string `json:"id"`
	Material  string `json:"material"`
	CreatedAt string `json:"created_at"`
}

// FileKeyProvider keeps all still-readable key versions in one atomic 0600
// keyring. master.key remains as the legacy v1 recovery key and is imported on
// first use; it is never overwritten.
type FileKeyProvider struct {
	Dir string
}

func (p *FileKeyProvider) ProviderName() string { return "file-keyring" }

func (p *FileKeyProvider) ActiveKey(_ context.Context) (KeyMaterial, error) {
	state, err := p.read(true)
	if err != nil {
		return KeyMaterial{}, err
	}
	return materialByID(state, effectiveActiveKeyID(state))
}

func (p *FileKeyProvider) Key(_ context.Context, id string) (KeyMaterial, error) {
	state, err := p.read(false)
	if err != nil {
		return KeyMaterial{}, err
	}
	return materialByID(state, strings.ToLower(strings.TrimSpace(id)))
}

func (p *FileKeyProvider) AllKeys(_ context.Context) ([]KeyMaterial, error) {
	state, err := p.read(false)
	if err != nil {
		return nil, err
	}
	return orderedMaterials(state)
}

func (p *FileKeyProvider) Descriptor(_ context.Context) (MasterKeyDescriptor, error) {
	state, err := p.read(false)
	if err != nil {
		return MasterKeyDescriptor{}, err
	}
	provider := p.ProviderName()
	if _, statErr := os.Stat(filepath.Join(strings.TrimSpace(p.Dir), masterKeyringFile)); errors.Is(statErr, os.ErrNotExist) {
		provider = "file-legacy"
	}
	return MasterKeyDescriptor{Provider: provider, KeyID: effectiveActiveKeyID(state), KeyCount: len(state.Keys), Envelope: "aes-gcm-v2", RotationKeyID: state.RotationKeyID}, nil
}

func (p *FileKeyProvider) read(create bool) (*keyringFile, error) {
	fileKeyringMu.Lock()
	defer fileKeyringMu.Unlock()
	return p.readUnlocked(create)
}

func (p *FileKeyProvider) readUnlocked(create bool) (*keyringFile, error) {
	dir := strings.TrimSpace(p.Dir)
	if dir == "" {
		return nil, fmt.Errorf("cloud workspace master key directory is required")
	}
	path := filepath.Join(dir, masterKeyringFile)
	if info, statErr := os.Lstat(path); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("cloud workspace keyring must not be a symlink: %s", path)
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	raw, err := os.ReadFile(path)
	if err == nil {
		if err := ensurePrivateKeyFile(path); err != nil {
			return nil, err
		}
		var state keyringFile
		if err := json.Unmarshal(raw, &state); err != nil {
			return nil, fmt.Errorf("decode cloud workspace keyring: %w", err)
		}
		if err := validateKeyring(&state); err != nil {
			return nil, err
		}
		return &state, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	legacy, err := readLegacyFileKey(dir, create)
	if err != nil {
		return nil, err
	}
	id := encryptionKeyID(legacy)
	state := &keyringFile{Version: keyringVersion, ActiveKeyID: id, Keys: []keyringEntry{{ID: id, Material: base64.RawStdEncoding.EncodeToString(legacy), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}}}
	if create {
		if err := p.writeUnlocked(state); err != nil {
			return nil, err
		}
	}
	return state, nil
}

func (p *FileKeyProvider) writeUnlocked(state *keyringFile) error {
	if err := validateKeyring(state); err != nil {
		return err
	}
	dir := strings.TrimSpace(p.Dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("restrict cloud workspace key directory permissions: %w", err)
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return fileutil.AtomicWriteFile(filepath.Join(dir, masterKeyringFile), raw, 0o600)
}

type environmentKeyProvider struct {
	keyringRaw string
	legacyRaw  string
}

func (p *environmentKeyProvider) ProviderName() string {
	if strings.TrimSpace(p.keyringRaw) != "" {
		return "environment-keyring"
	}
	return "environment"
}

func (p *environmentKeyProvider) ActiveKey(_ context.Context) (KeyMaterial, error) {
	keys, active, err := p.load()
	if err != nil {
		return KeyMaterial{}, err
	}
	return findMaterial(keys, active)
}

func (p *environmentKeyProvider) Key(_ context.Context, id string) (KeyMaterial, error) {
	keys, _, err := p.load()
	if err != nil {
		return KeyMaterial{}, err
	}
	return findMaterial(keys, strings.ToLower(strings.TrimSpace(id)))
}

func (p *environmentKeyProvider) AllKeys(_ context.Context) ([]KeyMaterial, error) {
	keys, active, err := p.load()
	if err != nil {
		return nil, err
	}
	return activeFirst(keys, active), nil
}

func (p *environmentKeyProvider) Descriptor(_ context.Context) (MasterKeyDescriptor, error) {
	keys, active, err := p.load()
	if err != nil {
		return MasterKeyDescriptor{}, err
	}
	return MasterKeyDescriptor{Provider: p.ProviderName(), KeyID: active, KeyCount: len(keys), Envelope: "aes-gcm-v2"}, nil
}

func (p *environmentKeyProvider) load() ([]KeyMaterial, string, error) {
	if raw := strings.TrimSpace(p.keyringRaw); raw != "" {
		var payload struct {
			Active   string   `json:"active"`
			Previous []string `json:"previous"`
		}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return nil, "", fmt.Errorf("%s must be JSON with active/previous base64 keys", keyringEnv)
		}
		if len(payload.Previous) >= maxKeyVersions {
			return nil, "", fmt.Errorf("%s contains too many key versions", keyringEnv)
		}
		activeKey, err := decodeKey(payload.Active, keyringEnv+".active")
		if err != nil {
			return nil, "", err
		}
		keys := []KeyMaterial{{ID: encryptionKeyID(activeKey), Bytes: activeKey}}
		for index, encoded := range payload.Previous {
			key, err := decodeKey(encoded, fmt.Sprintf("%s.previous[%d]", keyringEnv, index))
			if err != nil {
				return nil, "", err
			}
			id := encryptionKeyID(key)
			if _, err := findMaterial(keys, id); err != nil {
				keys = append(keys, KeyMaterial{ID: id, Bytes: key})
			}
		}
		return keys, keys[0].ID, nil
	}
	key, err := decodeKey(p.legacyRaw, masterKeyEnv)
	if err != nil {
		return nil, "", err
	}
	id := encryptionKeyID(key)
	return []KeyMaterial{{ID: id, Bytes: key}}, id, nil
}

func defaultKeyProvider(dir string) KeyProvider {
	if raw := strings.TrimSpace(os.Getenv(keyringEnv)); raw != "" {
		return &environmentKeyProvider{keyringRaw: raw}
	}
	if raw := strings.TrimSpace(os.Getenv(masterKeyEnv)); raw != "" {
		return &environmentKeyProvider{legacyRaw: raw}
	}
	return &FileKeyProvider{Dir: dir}
}

// NewKeyProvider returns the provider selected by the process environment or
// the file keyring fallback. Callers that integrate an external KMS can pass
// their own KeyProvider through BlobStore.Keys instead.
func NewKeyProvider(dir string) KeyProvider {
	return defaultKeyProvider(dir)
}

func readLegacyFileKey(dir string, create bool) ([]byte, error) {
	path := filepath.Join(dir, masterKeyFile)
	if info, statErr := os.Lstat(path); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("cloud workspace master key must not be a symlink: %s", path)
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	if key, err := os.ReadFile(path); err == nil {
		if len(key) != 32 {
			return nil, fmt.Errorf("invalid cloud workspace master key")
		}
		if err := ensurePrivateKeyFile(path); err != nil {
			return nil, err
		}
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) || !create {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return readLegacyFileKey(dir, false)
	}
	if err != nil {
		return nil, err
	}
	if _, err := file.Write(key); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return key, nil
}

func ensurePrivateKeyFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("cloud workspace key file must not be a symlink: %s", path)
	}
	if info.Mode().Perm()&0o077 == 0 {
		return nil
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("restrict cloud workspace key file permissions: %w", err)
	}
	return nil
}

func validateKeyring(state *keyringFile) error {
	if state == nil || state.Version != keyringVersion || len(state.Keys) == 0 || len(state.Keys) > maxKeyVersions || state.ActiveKeyID == "" {
		return fmt.Errorf("invalid cloud workspace keyring")
	}
	seen := make(map[string]struct{}, len(state.Keys))
	for index := range state.Keys {
		entry := &state.Keys[index]
		entry.ID = strings.ToLower(strings.TrimSpace(entry.ID))
		key, err := decodeKey(entry.Material, fmt.Sprintf("keyring key %d", index))
		if err != nil || encryptionKeyID(key) != entry.ID {
			return fmt.Errorf("invalid cloud workspace keyring key %d", index)
		}
		if _, duplicate := seen[entry.ID]; duplicate {
			return fmt.Errorf("duplicate cloud workspace key id")
		}
		seen[entry.ID] = struct{}{}
	}
	state.ActiveKeyID = strings.ToLower(strings.TrimSpace(state.ActiveKeyID))
	state.RotationKeyID = strings.ToLower(strings.TrimSpace(state.RotationKeyID))
	if _, ok := seen[state.ActiveKeyID]; !ok {
		return fmt.Errorf("cloud workspace active key is missing")
	}
	// A rotation marker can survive from a pre-11.31 binary that crashed
	// mid-rotation. It must name a known key; the effective active key then
	// follows the marker so new writes continue with the rotation target while
	// every mixed-generation file stays readable through AllKeys.
	if state.RotationKeyID != "" {
		if _, ok := seen[state.RotationKeyID]; !ok {
			return fmt.Errorf("cloud workspace rotation key is missing")
		}
	}
	sort.Slice(state.Keys, func(i, j int) bool { return state.Keys[i].ID < state.Keys[j].ID })
	return nil
}

// effectiveActiveKeyID resolves an interrupted pre-11.31 rotation marker to
// the key new writes should use. Without a marker the stored active key wins.
func effectiveActiveKeyID(state *keyringFile) string {
	if state.RotationKeyID != "" {
		return state.RotationKeyID
	}
	return state.ActiveKeyID
}

func orderedMaterials(state *keyringFile) ([]KeyMaterial, error) {
	all := make([]KeyMaterial, 0, len(state.Keys))
	for _, entry := range state.Keys {
		key, err := decodeKey(entry.Material, entry.ID)
		if err != nil {
			return nil, err
		}
		all = append(all, KeyMaterial{ID: entry.ID, Bytes: key})
	}
	return activeFirst(all, effectiveActiveKeyID(state)), nil
}

func materialByID(state *keyringFile, id string) (KeyMaterial, error) {
	materials, err := orderedMaterials(state)
	if err != nil {
		return KeyMaterial{}, err
	}
	return findMaterial(materials, id)
}

func activeFirst(keys []KeyMaterial, active string) []KeyMaterial {
	out := make([]KeyMaterial, 0, len(keys))
	if key, err := findMaterial(keys, active); err == nil {
		out = append(out, cloneMaterial(key))
	}
	for _, key := range keys {
		if key.ID != active {
			out = append(out, cloneMaterial(key))
		}
	}
	return out
}

func findMaterial(keys []KeyMaterial, id string) (KeyMaterial, error) {
	for _, key := range keys {
		if key.ID == id {
			return cloneMaterial(key), nil
		}
	}
	return KeyMaterial{}, fmt.Errorf("%w: %s", ErrKeyUnavailable, id)
}

func cloneMaterial(key KeyMaterial) KeyMaterial {
	return KeyMaterial{ID: key.ID, Bytes: append([]byte(nil), key.Bytes...)}
}

func decodeKey(encoded, label string) ([]byte, error) {
	encoded = strings.TrimSpace(encoded)
	key, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		// Accept both unpadded and conventional padded base64. Environment
		// secret managers commonly emit the latter, while the keyring writer
		// deliberately uses the compact raw form.
		key, err = base64.StdEncoding.DecodeString(encoded)
	}
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("%s must be a base64 32-byte key", label)
	}
	return key, nil
}

func encryptionKeyID(key []byte) string {
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:])
}
