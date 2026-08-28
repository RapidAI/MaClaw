package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/cloudworkspaceignore"
)

const (
	cloudWorkspaceCacheStateDir  = ".maclaw-cloud"
	cloudWorkspaceCacheStateFile = "state.json"

	cloudWorkspaceAcquiredGranted = "granted"
	cloudWorkspaceAcquiredRenewed = "renewed"
)

// cloudWorkspaceLocalState is {cache}/.maclaw-cloud/state.json.
type cloudWorkspaceLocalState struct {
	LastPushedRevision string `json:"last_pushed_revision"`
}

func cloudWorkspaceSafeRelPath(p string) (string, bool) {
	p = strings.TrimSpace(p)
	if p == "" || strings.ContainsAny(p, `\:`) || strings.ContainsRune(p, 0) {
		return "", false
	}
	if path.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return "", false
	}
	cleaned := path.Clean("/" + p)
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "" || cleaned == "." || cleaned == ".." || cleaned != p {
		return "", false
	}
	for _, seg := range strings.Split(cleaned, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", false
		}
	}
	return cleaned, true
}

type cloudWorkspaceManifestEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type cloudWorkspaceManifest struct {
	Revision string                        `json:"revision"`
	Entries  []cloudWorkspaceManifestEntry `json:"entries"`
}

type cloudWorkspaceSyncTransport interface {
	GetManifest(ctx context.Context) (*cloudWorkspaceManifest, error)
	PutManifest(ctx context.Context, ifMatch string, entries []cloudWorkspaceManifestEntry) (*cloudWorkspaceManifest, error)
	GetObject(ctx context.Context, sha256hex string, sizeBytes int64) ([]byte, error)
	PutObject(ctx context.Context, sha256hex string, data []byte) error
	PutChunk(ctx context.Context, sha256hex string, index int, data []byte) error
	CompleteObject(ctx context.Context, sha256hex string) error
}

type cloudWorkspaceProtocol struct {
	Transport    cloudWorkspaceSyncTransport
	MaxDirectPut int64
	MaxChunk     int64
}

func (p *cloudWorkspaceProtocol) maxDirectPut() int64 {
	if p != nil && p.MaxDirectPut > 0 {
		return p.MaxDirectPut
	}
	return cloudWorkspaceChunkBytes
}

func (p *cloudWorkspaceProtocol) maxChunk() int64 {
	if p != nil && p.MaxChunk > 0 {
		return p.MaxChunk
	}
	return cloudWorkspaceChunkBytes
}

func hashCloudWorkspaceFile(path string) (string, int64, error) {
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

func cloudWorkspaceStatePath(root string) string {
	return filepath.Join(root, cloudWorkspaceCacheStateDir, cloudWorkspaceCacheStateFile)
}

func readCloudWorkspaceLocalState(root string) (cloudWorkspaceLocalState, error) {
	raw, err := os.ReadFile(cloudWorkspaceStatePath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return cloudWorkspaceLocalState{}, nil
		}
		return cloudWorkspaceLocalState{}, err
	}
	var st cloudWorkspaceLocalState
	if err := json.Unmarshal(raw, &st); err != nil {
		return cloudWorkspaceLocalState{}, err
	}
	return st, nil
}

func writeCloudWorkspaceLocalState(root, revision string) error {
	dir := filepath.Join(root, cloudWorkspaceCacheStateDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(cloudWorkspaceLocalState{LastPushedRevision: revision})
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(dir, cloudWorkspaceCacheStateFile), raw)
}

// scanCloudWorkspaceLocal walks root with corelib ignore rules and returns slash paths.
func scanCloudWorkspaceLocal(root string) ([]cloudWorkspaceManifestEntry, error) {
	cloudignore, err := cloudworkspaceignore.ReadCloudignore(root)
	if err != nil {
		return nil, err
	}
	matcher := cloudworkspaceignore.NewMatcher(cloudignore)
	var entries []cloudWorkspaceManifestEntry
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
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		sum, size, err := hashCloudWorkspaceFile(p)
		if err != nil {
			return err
		}
		entries = append(entries, cloudWorkspaceManifestEntry{Path: rel, SHA256: sum, Size: size})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if entries == nil {
		entries = []cloudWorkspaceManifestEntry{}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func cloudWorkspaceTreesEqual(local []cloudWorkspaceManifestEntry, remote []cloudWorkspaceManifestEntry) bool {
	if local == nil {
		local = []cloudWorkspaceManifestEntry{}
	}
	if remote == nil {
		remote = []cloudWorkspaceManifestEntry{}
	}
	if len(local) != len(remote) {
		return false
	}
	type key struct {
		path string
		sha  string
		size int64
	}
	have := make(map[key]struct{}, len(remote))
	for _, e := range remote {
		have[key{path: e.Path, sha: e.SHA256, size: e.Size}] = struct{}{}
	}
	for _, e := range local {
		if _, ok := have[key{path: e.Path, sha: e.SHA256, size: e.Size}]; !ok {
			return false
		}
	}
	return true
}

func cloudWorkspaceCacheDirty(root string, remote *cloudWorkspaceManifest, afterSteal bool) (bool, error) {
	state, err := readCloudWorkspaceLocalState(root)
	if err != nil {
		return false, err
	}
	last := strings.TrimSpace(state.LastPushedRevision)
	serverRev := ""
	if remote != nil {
		serverRev = strings.TrimSpace(remote.Revision)
	}
	if last != "" && last != serverRev {
		return true, nil
	}
	if !afterSteal {
		return false, nil
	}
	local, err := scanCloudWorkspaceLocal(root)
	if err != nil {
		return false, err
	}
	var remoteEntries []cloudWorkspaceManifestEntry
	if remote != nil {
		remoteEntries = remote.Entries
	}
	return !cloudWorkspaceTreesEqual(local, remoteEntries), nil
}

func (p *cloudWorkspaceProtocol) putBytes(ctx context.Context, sha string, data []byte) error {
	if p == nil || p.Transport == nil {
		return fmt.Errorf("cloud workspace sync unavailable")
	}
	if int64(len(data)) > cloudWorkspaceObjectMaxBytes {
		return fmt.Errorf("cloud workspace object exceeds %d bytes", cloudWorkspaceObjectMaxBytes)
	}
	if int64(len(data)) <= p.maxDirectPut() {
		return p.Transport.PutObject(ctx, sha, data)
	}
	chunk := int(p.maxChunk())
	if chunk <= 0 {
		chunk = int(cloudWorkspaceChunkBytes)
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

func (p *cloudWorkspaceProtocol) Push(ctx context.Context, root string) (*cloudWorkspaceManifest, error) {
	if p == nil || p.Transport == nil {
		return nil, fmt.Errorf("cloud workspace sync unavailable")
	}
	entries, err := scanCloudWorkspaceLocal(root)
	if err != nil {
		return nil, err
	}
	var cancel context.CancelFunc
	ctx, cancel = bindCloudWorkspaceTimeout(ctx, cloudWorkspaceEntriesTimeout(entries))
	defer cancel()
	remote, err := p.Transport.GetManifest(ctx)
	if err != nil {
		return nil, err
	}
	if remote == nil {
		remote = &cloudWorkspaceManifest{Entries: []cloudWorkspaceManifestEntry{}}
	}
	have := make(map[string]struct{}, len(remote.Entries))
	for _, e := range remote.Entries {
		have[e.SHA256] = struct{}{}
	}
	if cloudWorkspaceTreesEqual(entries, remote.Entries) {
		if err := writeCloudWorkspaceLocalState(root, remote.Revision); err != nil {
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
		if err := writeCloudWorkspaceLocalState(root, out.Revision); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (p *cloudWorkspaceProtocol) Pull(ctx context.Context, root string) (*cloudWorkspaceManifest, error) {
	if p == nil || p.Transport == nil {
		return nil, fmt.Errorf("cloud workspace sync unavailable")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	remote, err := p.Transport.GetManifest(ctx)
	if err != nil {
		return nil, err
	}
	if remote == nil {
		remote = &cloudWorkspaceManifest{Entries: []cloudWorkspaceManifestEntry{}}
	}
	var cancel context.CancelFunc
	ctx, cancel = bindCloudWorkspaceTimeout(ctx, cloudWorkspaceEntriesTimeout(remote.Entries))
	defer cancel()
	keep := make(map[string]struct{}, len(remote.Entries))
	for _, e := range remote.Entries {
		cleaned, ok := cloudWorkspaceSafeRelPath(e.Path)
		if !ok {
			return nil, fmt.Errorf("invalid cloud workspace path %q", e.Path)
		}
		keep[cleaned] = struct{}{}
		dest := filepath.Join(root, filepath.FromSlash(cleaned))
		if info, err := os.Stat(dest); err == nil && info.Mode().IsRegular() {
			sum, size, err := hashCloudWorkspaceFile(dest)
			if err == nil && sum == e.SHA256 && size == e.Size {
				continue
			}
		}
		data, err := p.Transport.GetObject(ctx, e.SHA256, e.Size)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != e.SHA256 {
			return nil, fmt.Errorf("cloud workspace object hash mismatch")
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return nil, err
		}
		if err := atomicWriteFile(dest, data); err != nil {
			return nil, err
		}
	}
	cloudignore, err := cloudworkspaceignore.ReadCloudignore(root)
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
	if err := writeCloudWorkspaceLocalState(root, remote.Revision); err != nil {
		return nil, err
	}
	return remote, nil
}

type cloudWorkspaceHTTPTransport struct {
	app         *App
	workspaceID string
}

func (t *cloudWorkspaceHTTPTransport) GetManifest(ctx context.Context) (*cloudWorkspaceManifest, error) {
	if t == nil || t.app == nil {
		return nil, fmt.Errorf("cloud workspace sync unavailable")
	}
	data, status, err := t.app.cloudWorkspaceHubDo(ctx, http.MethodGet, cloudWorkspaceManifestPath(t.workspaceID), cloudWorkspaceHTTPOptions{
		timeout: 60 * time.Second,
		maxRead: cloudWorkspaceManifestMaxSize,
		accept:  "application/json",
	})
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, cloudWorkspaceAPIError(status, data)
	}
	var out cloudWorkspaceManifest
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("invalid cloud workspace manifest: %w", err)
	}
	if out.Entries == nil {
		out.Entries = []cloudWorkspaceManifestEntry{}
	}
	return &out, nil
}

func (t *cloudWorkspaceHTTPTransport) PutManifest(ctx context.Context, ifMatch string, entries []cloudWorkspaceManifestEntry) (*cloudWorkspaceManifest, error) {
	if t == nil || t.app == nil {
		return nil, fmt.Errorf("cloud workspace sync unavailable")
	}
	if entries == nil {
		entries = []cloudWorkspaceManifestEntry{}
	}
	body := map[string]any{
		"if_match_revision": ifMatch,
		"entries":           entries,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	data, status, err := t.app.cloudWorkspaceHubDo(ctx, http.MethodPut, cloudWorkspaceManifestPath(t.workspaceID), cloudWorkspaceHTTPOptions{
		timeout:     cloudWorkspaceTransferTimeout(int64(len(raw))),
		maxRead:     cloudWorkspaceManifestMaxSize,
		accept:      "application/json",
		contentType: "application/json",
		rawBody:     raw,
	})
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, cloudWorkspaceAPIError(status, data)
	}
	var out cloudWorkspaceManifest
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("invalid cloud workspace manifest: %w", err)
	}
	if out.Entries == nil {
		out.Entries = []cloudWorkspaceManifestEntry{}
	}
	return &out, nil
}

func (t *cloudWorkspaceHTTPTransport) GetObject(ctx context.Context, sha256hex string, sizeBytes int64) ([]byte, error) {
	if t == nil || t.app == nil {
		return nil, fmt.Errorf("cloud workspace sync unavailable")
	}
	data, status, err := t.app.cloudWorkspaceHubDo(ctx, http.MethodGet, cloudWorkspaceObjectPath(t.workspaceID, sha256hex), cloudWorkspaceHTTPOptions{
		timeout: cloudWorkspaceTransferTimeout(sizeBytes),
		maxRead: cloudWorkspaceObjectMaxBytes,
		accept:  "application/octet-stream",
	})
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, cloudWorkspaceAPIError(status, data)
	}
	if sizeBytes > 0 && int64(len(data)) != sizeBytes {
		return nil, fmt.Errorf("cloud workspace object size mismatch")
	}
	return data, nil
}

func (t *cloudWorkspaceHTTPTransport) PutObject(ctx context.Context, sha256hex string, data []byte) error {
	if t == nil || t.app == nil {
		return fmt.Errorf("cloud workspace sync unavailable")
	}
	resp, status, err := t.app.cloudWorkspaceHubDo(ctx, http.MethodPut, cloudWorkspaceObjectPath(t.workspaceID, sha256hex), cloudWorkspaceHTTPOptions{
		timeout:     cloudWorkspaceTransferTimeout(int64(len(data))),
		maxRead:     cloudWorkspaceResponseMaxSize,
		accept:      "application/json",
		contentType: "application/octet-stream",
		rawBody:     data,
	})
	if err != nil {
		return err
	}
	if status >= 300 {
		return cloudWorkspaceAPIError(status, resp)
	}
	return nil
}

func (t *cloudWorkspaceHTTPTransport) PutChunk(ctx context.Context, sha256hex string, index int, data []byte) error {
	if t == nil || t.app == nil {
		return fmt.Errorf("cloud workspace sync unavailable")
	}
	resp, status, err := t.app.cloudWorkspaceHubDo(ctx, http.MethodPut, cloudWorkspaceObjectChunkPath(t.workspaceID, sha256hex, index), cloudWorkspaceHTTPOptions{
		timeout:     cloudWorkspaceChunkTimeout,
		maxRead:     cloudWorkspaceResponseMaxSize,
		accept:      "application/json",
		contentType: "application/octet-stream",
		rawBody:     data,
	})
	if err != nil {
		return err
	}
	if status >= 300 {
		return cloudWorkspaceAPIError(status, resp)
	}
	return nil
}

func (t *cloudWorkspaceHTTPTransport) CompleteObject(ctx context.Context, sha256hex string) error {
	if t == nil || t.app == nil {
		return fmt.Errorf("cloud workspace sync unavailable")
	}
	resp, status, err := t.app.cloudWorkspaceHubDo(ctx, http.MethodPost, cloudWorkspaceObjectCompletePath(t.workspaceID, sha256hex), cloudWorkspaceHTTPOptions{
		timeout: 60 * time.Second,
		maxRead: cloudWorkspaceResponseMaxSize,
		accept:  "application/json",
	})
	if err != nil {
		return err
	}
	if status >= 300 {
		return cloudWorkspaceAPIError(status, resp)
	}
	return nil
}

func (a *App) cloudWorkspaceProtocol(workspaceID string) *cloudWorkspaceProtocol {
	return &cloudWorkspaceProtocol{
		Transport:    &cloudWorkspaceHTTPTransport{app: a, workspaceID: workspaceID},
		MaxDirectPut: cloudWorkspaceChunkBytes,
		MaxChunk:     cloudWorkspaceChunkBytes,
	}
}
