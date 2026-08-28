package cloudworkspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/cloudworkspaceignore"
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

const (
	cacheStateDir  = ".maclaw-cloud"
	cacheStateFile = "state.json"
)

// LocalSyncState is {cache}/.maclaw-cloud/state.json.
type LocalSyncState struct {
	LastPushedRevision string `json:"last_pushed_revision"`
}

// SyncTransport is the Hub-facing side of Pull/Push. HTTP is one implementation.
type SyncTransport interface {
	GetManifest(ctx context.Context) (*Manifest, error)
	PutManifest(ctx context.Context, ifMatch string, entries []ManifestEntry) (*Manifest, error)
	GetObject(ctx context.Context, sha256hex string) ([]byte, error)
	PutObject(ctx context.Context, sha256hex string, data []byte) error
	PutChunk(ctx context.Context, sha256hex string, index int, data []byte) error
	CompleteObject(ctx context.Context, sha256hex string) error
}

// Protocol is the pure-Go Pull/Push client (no GUI).
type Protocol struct {
	Transport    SyncTransport
	MaxDirectPut int64
	MaxChunk     int64
}

func (p *Protocol) maxDirectPut() int64 {
	if p != nil && p.MaxDirectPut > 0 {
		return p.MaxDirectPut
	}
	return MaxChunkBytes
}

func (p *Protocol) maxChunk() int64 {
	if p != nil && p.MaxChunk > 0 {
		return p.MaxChunk
	}
	return MaxChunkBytes
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// ScanLocal walks root, applies ignore, and returns the full file tree.
func ScanLocal(root string) ([]ManifestEntry, error) {
	cloudignore, err := ReadCloudignore(root)
	if err != nil {
		return nil, err
	}
	var entries []ManifestEntry
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if ShouldIgnore(rel, d.IsDir(), cloudignore) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		sum, size, err := hashFile(p)
		if err != nil {
			return err
		}
		entries = append(entries, ManifestEntry{Path: rel, SHA256: sum, Size: size})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if entries == nil {
		entries = []ManifestEntry{}
	}
	return entries, nil
}

func writeLocalState(root, revision string) error {
	dir := filepath.Join(root, cacheStateDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(LocalSyncState{LastPushedRevision: revision})
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(filepath.Join(dir, cacheStateFile), raw, 0o600)
}

// ReadLocalState returns last_pushed_revision, or empty if missing.
func ReadLocalState(root string) (LocalSyncState, error) {
	raw, err := os.ReadFile(filepath.Join(root, cacheStateDir, cacheStateFile))
	if err != nil {
		if os.IsNotExist(err) {
			return LocalSyncState{}, nil
		}
		return LocalSyncState{}, err
	}
	var st LocalSyncState
	if err := json.Unmarshal(raw, &st); err != nil {
		return LocalSyncState{}, err
	}
	return st, nil
}

func (p *Protocol) putBytes(ctx context.Context, sha string, data []byte) error {
	if p == nil || p.Transport == nil {
		return ErrUnavailable
	}
	if int64(len(data)) <= p.maxDirectPut() {
		return p.Transport.PutObject(ctx, sha, data)
	}
	chunk := int(p.maxChunk())
	if chunk <= 0 {
		chunk = int(MaxChunkBytes)
	}
	idx := 0
	for off := 0; off < len(data); off += chunk {
		end := off + chunk
		if end > len(data) {
			end = len(data)
		}
		if err := p.Transport.PutChunk(ctx, sha, idx, data[off:end]); err != nil {
			return err
		}
		idx++
	}
	return p.Transport.CompleteObject(ctx, sha)
}

// Push uploads missing objects then replaces the remote tree from local scan.
func (p *Protocol) Push(ctx context.Context, root string) (*Manifest, error) {
	if p == nil || p.Transport == nil {
		return nil, ErrUnavailable
	}
	entries, err := ScanLocal(root)
	if err != nil {
		return nil, err
	}
	remote, err := p.Transport.GetManifest(ctx)
	if err != nil {
		return nil, err
	}
	if remote == nil {
		remote = &Manifest{Entries: []ManifestEntry{}}
	}
	have := make(map[string]struct{}, len(remote.Entries))
	for _, e := range remote.Entries {
		have[e.SHA256] = struct{}{}
	}
	if manifestTreesEqual(entries, remote.Entries) {
		if err := writeLocalState(root, remote.Revision); err != nil {
			return nil, err
		}
		return remote, nil
	}
	uploaded := map[string]struct{}{}
	for _, e := range entries {
		if _, ok := have[e.SHA256]; ok {
			continue
		}
		if _, ok := uploaded[e.SHA256]; ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(e.Path)))
		if err != nil {
			return nil, err
		}
		if err := p.putBytes(ctx, e.SHA256, data); err != nil {
			return nil, err
		}
		uploaded[e.SHA256] = struct{}{}
	}
	out, err := p.Transport.PutManifest(ctx, remote.Revision, entries)
	if err != nil {
		return nil, err
	}
	if out != nil {
		if err := writeLocalState(root, out.Revision); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Pull makes the local tree match the server. It never uploads.
func (p *Protocol) Pull(ctx context.Context, root string) (*Manifest, error) {
	if p == nil || p.Transport == nil {
		return nil, ErrUnavailable
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	remote, err := p.Transport.GetManifest(ctx)
	if err != nil {
		return nil, err
	}
	if remote == nil {
		remote = &Manifest{Entries: []ManifestEntry{}}
	}
	keep := make(map[string]struct{}, len(remote.Entries))
	for _, e := range remote.Entries {
		cleaned, err := ValidateManifestPath(e.Path)
		if err != nil {
			return nil, err
		}
		keep[cleaned] = struct{}{}
		dest := filepath.Join(root, filepath.FromSlash(cleaned))
		if info, err := os.Stat(dest); err == nil && info.Mode().IsRegular() {
			sum, size, err := hashFile(dest)
			if err == nil && sum == e.SHA256 && size == e.Size {
				continue
			}
		}
		data, err := p.Transport.GetObject(ctx, e.SHA256)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != e.SHA256 {
			return nil, ErrBlobHashMismatch
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return nil, err
		}
		if err := fileutil.AtomicWriteFile(dest, data, 0o600); err != nil {
			return nil, err
		}
	}
	cloudignore, err := ReadCloudignore(root)
	if err != nil {
		return nil, err
	}
	matcher := cloudworkspaceignore.NewMatcher(cloudignore)
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if matcher.ShouldIgnore(rel, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if _, ok := keep[rel]; ok {
			return nil
		}
		return os.Remove(p)
	})
	if err != nil {
		return nil, err
	}
	if err := writeLocalState(root, remote.Revision); err != nil {
		return nil, err
	}
	return remote, nil
}

// SyncAfterAcquire runs Push on same-machine renew and Pull for a new holder.
func (p *Protocol) SyncAfterAcquire(ctx context.Context, root, acquired string) (*Manifest, error) {
	if strings.TrimSpace(acquired) == AcquiredRenewed {
		return p.Push(ctx, root)
	}
	return p.Pull(ctx, root)
}

// ServiceTransport talks to Service in-process (Hub tests; not GUI).
type ServiceTransport struct {
	Service     *Service
	Principal   auth.MachinePrincipal
	WorkspaceID string
}

func (t *ServiceTransport) GetManifest(ctx context.Context) (*Manifest, error) {
	if t == nil || t.Service == nil {
		return nil, ErrUnavailable
	}
	return t.Service.GetManifest(ctx, t.Principal, t.WorkspaceID)
}

func (t *ServiceTransport) PutManifest(ctx context.Context, ifMatch string, entries []ManifestEntry) (*Manifest, error) {
	if t == nil || t.Service == nil {
		return nil, ErrUnavailable
	}
	return t.Service.PutManifest(ctx, t.Principal, t.WorkspaceID, ifMatch, entries)
}

func (t *ServiceTransport) GetObject(ctx context.Context, sha256hex string) ([]byte, error) {
	if t == nil || t.Service == nil {
		return nil, ErrUnavailable
	}
	return t.Service.GetObject(ctx, t.Principal, t.WorkspaceID, sha256hex)
}

func (t *ServiceTransport) PutObject(ctx context.Context, sha256hex string, data []byte) error {
	if t == nil || t.Service == nil {
		return ErrUnavailable
	}
	_, err := t.Service.PutObject(ctx, t.Principal, t.WorkspaceID, sha256hex, data)
	return err
}

func (t *ServiceTransport) PutChunk(ctx context.Context, sha256hex string, index int, data []byte) error {
	if t == nil || t.Service == nil {
		return ErrUnavailable
	}
	return t.Service.PutObjectChunk(ctx, t.Principal, t.WorkspaceID, sha256hex, index, data)
}

func (t *ServiceTransport) CompleteObject(ctx context.Context, sha256hex string) error {
	if t == nil || t.Service == nil {
		return ErrUnavailable
	}
	_, err := t.Service.CompleteObject(ctx, t.Principal, t.WorkspaceID, sha256hex)
	return err
}
