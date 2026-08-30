package cloudworkspace

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/archiveutil"
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

var sidecarCodecMagic = [4]byte{'M', 'C', 'S', '1'}
var sidecarWriteMu sync.Mutex

func mergeSessionJSON(existing, incoming []byte) []byte {
	var old, next struct {
		Conversation []json.RawMessage `json:"conversation,omitempty"`
		InputText    string            `json:"input_text,omitempty"`
	}
	if json.Unmarshal(existing, &old) != nil || json.Unmarshal(incoming, &next) != nil {
		return incoming
	}
	seen := make(map[[32]byte]struct{}, len(old.Conversation)+len(next.Conversation))
	merged := make([]json.RawMessage, 0, len(old.Conversation)+len(next.Conversation))
	for _, item := range append(old.Conversation, next.Conversation...) {
		key := sha256.Sum256(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, item)
	}
	if len(merged) > 4000 {
		merged = merged[len(merged)-4000:]
	}
	out := map[string]any{"conversation": merged}
	if next.InputText != "" {
		out["input_text"] = next.InputText
	} else if old.InputText != "" {
		out["input_text"] = old.InputText
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return incoming
	}
	return raw
}

func encodeSidecar(plain []byte) ([]byte, error) {
	if int64(len(plain)) > MaxSidecarBytes {
		return nil, ErrBlobTooLarge
	}
	stored, compression, _ := compressObject(plain)
	out := make([]byte, 13+len(stored))
	copy(out[:4], sidecarCodecMagic[:])
	if compression == "zstd" {
		out[4] = 1
	}
	binary.BigEndian.PutUint64(out[5:13], uint64(len(plain)))
	copy(out[13:], stored)
	return out, nil
}

func decodeSidecar(data []byte) ([]byte, error) {
	if len(data) < 13 || string(data[:4]) != string(sidecarCodecMagic[:]) {
		return data, nil
	}
	if data[4] != 0 && data[4] != 1 {
		return nil, ErrBlobCorrupt
	}
	plainSize := int64(binary.BigEndian.Uint64(data[5:13]))
	if plainSize < 0 || plainSize > MaxSidecarBytes {
		return nil, ErrBlobTooLarge
	}
	compression := "none"
	if data[4] == 1 {
		compression = "zstd"
	}
	if compression == "none" && int64(len(data[13:])) != plainSize {
		return nil, ErrBlobCorrupt
	}
	return decompressObject(data[13:], compression, plainSize)
}

const (
	sidecarDirName = "sidecars"

	SidecarSession    = "session.json"
	SidecarTask       = "task.json"
	SidecarWorkbench  = "coding_workbench.json"
	SidecarCheckpoint = "coding_exec_checkpoint.json"
)

// MaxSidecarBytes is the plaintext cap for one named sidecar (not the file tree).
const MaxSidecarBytes int64 = MaxObjectBytes

// TaskSidecar is the Hub task.json payload used to restore the GUI task list.
type TaskSidecar struct {
	Name string `json:"name"`
	Mode string `json:"mode"`
	Tag  string `json:"tag"`
}

func ParseTaskSidecar(data []byte) TaskSidecar {
	var task TaskSidecar
	if len(data) == 0 {
		return task
	}
	_ = json.Unmarshal(data, &task)
	task.Name = strings.TrimSpace(task.Name)
	task.Mode = strings.TrimSpace(task.Mode)
	task.Tag = strings.TrimSpace(task.Tag)
	return task
}

func (s *Service) taskSidecarFor(ctx context.Context, tenantID, userID, workspaceID string) TaskSidecar {
	blobs, err := s.blobs()
	if err != nil {
		return TaskSidecar{}
	}
	data, err := blobs.GetSidecar(ctx, tenantID, userID, workspaceID, SidecarTask)
	if err != nil || len(data) == 0 {
		return TaskSidecar{}
	}
	return ParseTaskSidecar(data)
}

func sidecarAAD(tenantID, userID, workspaceID, name string) []byte {
	return []byte(tenantID + "|" + userID + "|" + workspaceID + "|sidecar|" + name)
}

// ValidateSidecarName allowlists the four session-continuity blobs.
func ValidateSidecarName(name string) (string, error) {
	switch name {
	case SidecarSession, SidecarTask, SidecarWorkbench, SidecarCheckpoint:
		return name, nil
	default:
		return "", ErrInvalidSidecarName
	}
}

func (s *BlobStore) SidecarsDir(tenantID, userID, workspaceID string) (string, error) {
	base, err := s.workspaceDir(tenantID, userID, workspaceID)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, sidecarDirName), nil
}

func (s *BlobStore) SidecarPath(tenantID, userID, workspaceID, name string) (string, error) {
	name, err := ValidateSidecarName(name)
	if err != nil {
		return "", err
	}
	dir, err := s.SidecarsDir(tenantID, userID, workspaceID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+objectFileExt), nil
}

// PutSidecar seals a named blob (not content-addressed) with the workspace DEK.
func (s *BlobStore) PutSidecar(_ context.Context, tenantID, userID, workspaceID, name string, plaintext []byte) error {
	if s == nil {
		return ErrUnavailable
	}
	if int64(len(plaintext)) > MaxSidecarBytes {
		return ErrBlobTooLarge
	}
	path, err := s.SidecarPath(tenantID, userID, workspaceID, name)
	if err != nil {
		return err
	}
	if name == SidecarSession {
		sidecarWriteMu.Lock()
		defer sidecarWriteMu.Unlock()
		if raw, readErr := os.ReadFile(path); readErr == nil {
			if master, keyErr := loadMasterKey(s.keyDir()); keyErr == nil {
				dek := deriveDEK(master, tenantID, userID, workspaceID)
				if oldEncoded, openErr := open(dek, sidecarAAD(tenantID, userID, workspaceID, name), raw); openErr == nil {
					plaintext = mergeSessionJSON(mustDecodeSidecar(oldEncoded), plaintext)
				}
			}
		}
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	master, err := loadMasterKey(s.keyDir())
	if err != nil {
		return err
	}
	dek := deriveDEK(master, tenantID, userID, workspaceID)
	encoded, err := encodeSidecar(plaintext)
	if err != nil {
		return err
	}
	sealed, err := seal(dek, sidecarAAD(tenantID, userID, workspaceID, name), encoded)
	if err != nil {
		return err
	}
	if avail, err := archiveutil.AvailableBytes(dir); err == nil && avail < int64(len(sealed))+4096 {
		return ErrDiskFull
	}
	return fileutil.AtomicWriteFile(path, sealed, 0o600)
}

func mustDecodeSidecar(data []byte) []byte {
	decoded, err := decodeSidecar(data)
	if err != nil {
		return data
	}
	return decoded
}

// GetSidecar decrypts a named sidecar. Missing files are ErrBlobNotFound.
func (s *BlobStore) GetSidecar(_ context.Context, tenantID, userID, workspaceID, name string) ([]byte, error) {
	if s == nil {
		return nil, ErrUnavailable
	}
	path, err := s.SidecarPath(tenantID, userID, workspaceID, name)
	if err != nil {
		return nil, err
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrBlobNotFound
		}
		return nil, err
	}
	// Sidecars carry a small codec header in addition to the plaintext cap.
	if int64(len(blob)) > s.maxCiphertextBytes()+13 {
		return nil, ErrBlobTooLarge
	}
	master, err := loadMasterKey(s.keyDir())
	if err != nil {
		return nil, err
	}
	dek := deriveDEK(master, tenantID, userID, workspaceID)
	plain, err := open(dek, sidecarAAD(tenantID, userID, workspaceID, name), blob)
	if err != nil {
		return nil, err
	}
	return decodeSidecar(plain)
}

// PutSidecar admits a named sidecar. Grant is enforced at HTTP; owner+lease here.
func (s *Service) PutSidecar(ctx context.Context, principal auth.MachinePrincipal, workspaceID, name string, plaintext []byte) error {
	blobs, err := s.blobs()
	if err != nil {
		return err
	}
	name, err = ValidateSidecarName(name)
	if err != nil {
		return err
	}
	if int64(len(plaintext)) > MaxSidecarBytes {
		return ErrBlobTooLarge
	}
	// Only conversation history is multi-writer. Task identity and runtime
	// sidecars remain lease-protected to preserve the existing task lifecycle
	// semantics and avoid concurrent renames/restores racing with a mount.
	if name != SidecarSession {
		if _, err := s.Workspaces.RequireLease(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, s.now()); err != nil {
			return err
		}
	} else if _, err := s.Workspaces.GetOwned(ctx, principal.TenantID, principal.UserID, workspaceID); err != nil {
		return err
	}
	dir, err := blobs.SidecarsDir(principal.TenantID, principal.UserID, workspaceID)
	if err != nil {
		return err
	}
	if err := s.checkVolume(dir, int64(len(plaintext))); err != nil {
		return err
	}
	return blobs.PutSidecar(ctx, principal.TenantID, principal.UserID, workspaceID, name, plaintext)
}

// GetSidecar returns sidecar plaintext for the owner. Reads do not take the
// exclusive write lease so a new machine can restore task.json into the list.
func (s *Service) GetSidecar(ctx context.Context, principal auth.MachinePrincipal, workspaceID, name string) ([]byte, error) {
	blobs, err := s.blobs()
	if err != nil {
		return nil, err
	}
	name, err = ValidateSidecarName(name)
	if err != nil {
		return nil, err
	}
	if s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	ws, err := s.Workspaces.GetOwned(ctx, principal.TenantID, principal.UserID, workspaceID)
	if err != nil {
		return nil, err
	}
	if ws == nil || ws.Status != StatusActive {
		return nil, ErrNotFound
	}
	return blobs.GetSidecar(ctx, principal.TenantID, principal.UserID, workspaceID, name)
}
