package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
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
	LastPushedRevision  string                        `json:"last_pushed_revision"`
	LastEventSeq        int64                         `json:"last_event_seq,omitempty"`
	PendingOperationIDs []string                      `json:"pending_operation_ids,omitempty"`
	FileRevisions       map[string]string             `json:"file_revisions,omitempty"`
	LastEntries         []cloudWorkspaceManifestEntry `json:"last_entries,omitempty"`
}

var errCloudWorkspaceV2Unavailable = errors.New("cloud workspace v2 operations unavailable")

// PushOperations uploads only changed files and appends per-file operations.
// It is intentionally additive: callers can fall back to Push when talking to
// an older Hub that does not expose the v2 endpoints.
func (p *cloudWorkspaceProtocol) PushOperations(ctx context.Context, root string) error {
	if p == nil || p.Transport == nil {
		return fmt.Errorf("cloud workspace sync unavailable")
	}
	if _, ok := p.Transport.(cloudWorkspaceV2Transport); !ok {
		return errCloudWorkspaceV2Unavailable
	}
	state, err := readCloudWorkspaceLocalState(root)
	if err != nil {
		return err
	}
	local, err := scanCloudWorkspaceLocal(root)
	if err != nil {
		return err
	}
	old := make(map[string]cloudWorkspaceManifestEntry, len(state.LastEntries))
	for _, e := range state.LastEntries {
		old[e.Path] = e
	}
	cur := make(map[string]cloudWorkspaceManifestEntry, len(local))
	for _, e := range local {
		cur[e.Path] = e
	}
	_, _, clientID, _ := p.clientIdentity()
	if clientID == "" {
		clientID = "maclaw-gui"
	}
	paths := make([]string, 0, len(cur))
	for path := range cur {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		e := cur[path]
		if prev, ok := old[path]; ok && prev.SHA256 == e.SHA256 && prev.Size == e.Size {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return err
		}
		if err := p.putBytes(ctx, e.SHA256, data); err != nil {
			return err
		}
		op := cloudWorkspaceOperation{OpID: operationID("put", path, state.FileRevisions[path], e.SHA256), Path: path, Kind: "put", BaseFileRevision: state.FileRevisions[path], ObjectSHA256: e.SHA256, PlainSize: e.Size, ClientInstanceID: clientID}
		res, err := p.SubmitOperation(ctx, op)
		if err != nil {
			return err
		}
		if res.Accepted {
			if state.FileRevisions == nil {
				state.FileRevisions = map[string]string{}
			}
			state.FileRevisions[path] = res.FileRevision
		} else {
			return p.materializeConflict(ctx, root, op, res)
		}
	}
	deleted := make([]string, 0)
	for path := range old {
		if _, ok := cur[path]; !ok {
			deleted = append(deleted, path)
		}
	}
	sort.Strings(deleted)
	for _, path := range deleted {
		op := cloudWorkspaceOperation{OpID: operationID("delete", path, state.FileRevisions[path], ""), Path: path, Kind: "delete", BaseFileRevision: state.FileRevisions[path], ClientInstanceID: clientID}
		res, err := p.SubmitOperation(ctx, op)
		if err != nil {
			return err
		}
		if res.Accepted {
			delete(state.FileRevisions, path)
		} else {
			return p.materializeConflict(ctx, root, op, res)
		}
	}
	state.LastEntries = local
	return writeCloudWorkspaceState(root, state)
}

// PullEvents applies remote operations newer than the local cursor. Unmodified
// files are updated in place; locally edited files are preserved and the
// remote version is written as a conflict copy.
func (p *cloudWorkspaceProtocol) PullEvents(ctx context.Context, root string) error {
	if p == nil || p.Transport == nil {
		return fmt.Errorf("cloud workspace sync unavailable")
	}
	state, err := readCloudWorkspaceLocalState(root)
	if err != nil {
		return err
	}
	events, err := p.GetEvents(ctx, state.LastEventSeq, 500)
	if err != nil {
		return err
	}
	_, _, clientID, _ := p.clientIdentity()
	known := make(map[string]cloudWorkspaceManifestEntry, len(state.LastEntries))
	for _, e := range state.LastEntries {
		known[e.Path] = e
	}
	if state.LastEntries == nil {
		// Older caches did not persist a local baseline. Treat the current tree
		// as that baseline so the first event poll can update clean files instead
		// of producing spurious conflict copies.
		baseline, scanErr := scanCloudWorkspaceLocal(root)
		if scanErr != nil {
			return scanErr
		}
		for _, e := range baseline {
			known[e.Path] = e
		}
	}
	for _, ev := range events {
		if ev.Seq > state.LastEventSeq {
			state.LastEventSeq = ev.Seq
		}
		if ev.ClientInstanceID == clientID {
			continue
		}
		localPath := filepath.Join(root, filepath.FromSlash(ev.Path))
		localChanged := false
		if old, ok := known[ev.Path]; ok {
			if sum, size, e := hashCloudWorkspaceFile(localPath); e == nil && (sum != old.SHA256 || size != old.Size) {
				localChanged = true
			}
		} else if _, e := os.Stat(localPath); e == nil {
			localChanged = true
		}
		if ev.Kind == "delete" {
			if localChanged {
				continue
			}
			_ = os.Remove(localPath)
			delete(known, ev.Path)
			delete(state.FileRevisions, ev.Path)
			continue
		}
		if ev.ObjectSHA256 == "" {
			continue
		}
		data, e := p.Transport.GetObject(ctx, ev.ObjectSHA256, 0)
		if e != nil {
			return e
		}
		if localChanged {
			conflict := conflictCopyPath(root, ev.Path, time.Now())
			if conflict == "" {
				return fmt.Errorf("invalid cloud workspace conflict path %q", ev.Path)
			}
			if e := atomicWriteFile(conflict, data); e != nil {
				return e
			}
			continue
		}
		if e := os.MkdirAll(filepath.Dir(localPath), 0o700); e != nil {
			return e
		}
		if e := atomicWriteFile(localPath, data); e != nil {
			return e
		}
		known[ev.Path] = cloudWorkspaceManifestEntry{Path: ev.Path, SHA256: ev.ObjectSHA256, Size: int64(len(data))}
		if state.FileRevisions == nil {
			state.FileRevisions = map[string]string{}
		}
		state.FileRevisions[ev.Path] = ev.NewFileRevision
	}
	// If the page was full, continue until caught up. This avoids leaving a
	// large workspace partially stale when a watcher fires once.
	if len(events) == 500 {
		state.LastEntries = state.LastEntries[:0]
		for _, e := range known {
			state.LastEntries = append(state.LastEntries, e)
		}
		sort.Slice(state.LastEntries, func(i, j int) bool { return state.LastEntries[i].Path < state.LastEntries[j].Path })
		if err := writeCloudWorkspaceState(root, state); err != nil {
			return err
		}
		return p.PullEvents(ctx, root)
	}
	state.LastEntries = state.LastEntries[:0]
	for _, e := range known {
		state.LastEntries = append(state.LastEntries, e)
	}
	sort.Slice(state.LastEntries, func(i, j int) bool { return state.LastEntries[i].Path < state.LastEntries[j].Path })
	return writeCloudWorkspaceState(root, state)
}

func conflictCopyPath(root, rel string, now time.Time) string {
	clean, ok := cloudWorkspaceSafeRelPath(rel)
	if !ok {
		return ""
	}
	base := filepath.Join(root, filepath.FromSlash(clean))
	stamp := now.UTC().Format("20060102-150405.000000000")
	return base + ".conflict-" + stamp
}

func operationID(kind, path, base, sha string) string {
	h := sha256.Sum256([]byte(kind + "\x00" + path + "\x00" + base + "\x00" + sha))
	return "op_" + hex.EncodeToString(h[:])
}

func (p *cloudWorkspaceProtocol) materializeConflict(ctx context.Context, root string, op cloudWorkspaceOperation, res *cloudWorkspaceOperationResult) error {
	if res == nil {
		return fmt.Errorf("cloud workspace operation rejected")
	}
	events, err := p.GetEvents(ctx, res.ConflictSeq-1, 1)
	if err != nil || len(events) == 0 {
		return fmt.Errorf("cloud workspace conflict for %s", op.Path)
	}
	ev := events[0]
	if ev.ObjectSHA256 == "" || op.Kind == "delete" {
		return fmt.Errorf("cloud workspace conflict for %s", op.Path)
	}
	data, err := p.Transport.GetObject(ctx, ev.ObjectSHA256, 0)
	if err != nil {
		return err
	}
	dest := conflictCopyPath(root, op.Path, time.Now())
	if dest == "" {
		return fmt.Errorf("invalid cloud workspace conflict path %q", op.Path)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	if err := atomicWriteFile(dest, data); err != nil {
		return err
	}
	return fmt.Errorf("cloud workspace conflict for %s (copy saved as %s)", op.Path, filepath.Base(dest))
}

func (p *cloudWorkspaceProtocol) clientIdentity() (string, string, string, error) {
	if p == nil || p.Transport == nil {
		return "", "", "", fmt.Errorf("transport unavailable")
	}
	if t, ok := p.Transport.(*cloudWorkspaceHTTPTransport); ok && t.app != nil {
		return t.app.virtualRepositorySyncClient()
	}
	return "", "", "", nil
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

// cloudWorkspaceOperation mirrors the Hub v2 multi-writer operation contract.
type cloudWorkspaceOperation struct {
	OpID             string `json:"op_id"`
	Path             string `json:"path"`
	Kind             string `json:"kind"`
	BaseFileRevision string `json:"base_file_revision,omitempty"`
	ObjectSHA256     string `json:"object_sha256,omitempty"`
	PlainSize        int64  `json:"plain_size,omitempty"`
	ClientInstanceID string `json:"client_instance_id"`
}

type cloudWorkspaceOperationResult struct {
	Accepted     bool   `json:"accepted"`
	WorkspaceSeq int64  `json:"workspace_seq"`
	FileRevision string `json:"file_revision"`
	Merge        string `json:"merge"`
	ConflictSeq  int64  `json:"conflict_seq,omitempty"`
}

type cloudWorkspaceEvent struct {
	Seq              int64  `json:"seq"`
	OpID             string `json:"op_id"`
	Path             string `json:"path"`
	Kind             string `json:"kind"`
	BaseFileRevision string `json:"base_file_revision,omitempty"`
	NewFileRevision  string `json:"new_file_revision"`
	ObjectSHA256     string `json:"object_sha256,omitempty"`
	ClientInstanceID string `json:"client_instance_id"`
	ConflictOfSeq    int64  `json:"conflict_of_seq,omitempty"`
	CreatedAt        string `json:"created_at"`
}

type cloudWorkspaceV2Transport interface {
	SubmitOperation(context.Context, cloudWorkspaceOperation) (*cloudWorkspaceOperationResult, error)
	GetEvents(context.Context, int64, int64) ([]cloudWorkspaceEvent, error)
}

func (p *cloudWorkspaceProtocol) SubmitOperation(ctx context.Context, op cloudWorkspaceOperation) (*cloudWorkspaceOperationResult, error) {
	t, ok := p.Transport.(cloudWorkspaceV2Transport)
	if !ok {
		return nil, errCloudWorkspaceV2Unavailable
	}
	return t.SubmitOperation(ctx, op)
}

func (p *cloudWorkspaceProtocol) GetEvents(ctx context.Context, after, limit int64) ([]cloudWorkspaceEvent, error) {
	t, ok := p.Transport.(cloudWorkspaceV2Transport)
	if !ok {
		return nil, fmt.Errorf("cloud workspace v2 events unavailable")
	}
	return t.GetEvents(ctx, after, limit)
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
	state, err := readCloudWorkspaceLocalState(root)
	if err != nil {
		return err
	}
	state.LastPushedRevision = revision
	if state.FileRevisions == nil {
		state.FileRevisions = map[string]string{}
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(dir, cloudWorkspaceCacheStateFile), raw)
}

func writeCloudWorkspaceState(root string, state cloudWorkspaceLocalState) error {
	dir := filepath.Join(root, cloudWorkspaceCacheStateDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(state)
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
	local, err := scanCloudWorkspaceLocal(root)
	if err != nil {
		return false, err
	}
	var remoteEntries []cloudWorkspaceManifestEntry
	if remote != nil {
		remoteEntries = remote.Entries
	}
	if cloudWorkspaceTreesEqual(local, remoteEntries) {
		return false, nil
	}
	state, err := readCloudWorkspaceLocalState(root)
	if err != nil {
		return false, err
	}
	last := strings.TrimSpace(state.LastPushedRevision)
	// Brand-new cache: pull without a prompt. Any existing cache (or a steal)
	// with a different tree must confirm before Pull wipes local files.
	if last == "" && !afterSteal {
		return false, nil
	}
	return true, nil
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
	if err := pruneCloudWorkspaceEmptyDirs(root, matcher); err != nil {
		return nil, err
	}
	if err := writeCloudWorkspaceLocalState(root, remote.Revision); err != nil {
		return nil, err
	}
	return remote, nil
}

func pruneCloudWorkspaceEmptyDirs(root string, matcher *cloudworkspaceignore.Matcher) error {
	if matcher == nil {
		matcher = cloudworkspaceignore.NewMatcher("")
	}
	var dirs []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if matcher.ShouldIgnore(rel, true) {
			return filepath.SkipDir
		}
		dirs = append(dirs, p)
		return nil
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		entries, readErr := os.ReadDir(dirs[i])
		if readErr != nil || len(entries) != 0 {
			continue
		}
		if err := os.Remove(dirs[i]); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
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

func (t *cloudWorkspaceHTTPTransport) SubmitOperation(ctx context.Context, op cloudWorkspaceOperation) (*cloudWorkspaceOperationResult, error) {
	raw, err := json.Marshal(op)
	if err != nil {
		return nil, err
	}
	data, status, err := t.app.cloudWorkspaceHubDo(ctx, http.MethodPost, cloudWorkspaceItemPath(t.workspaceID)+"/operations", cloudWorkspaceHTTPOptions{timeout: cloudWorkspaceRequestTimeout, maxRead: cloudWorkspaceResponseMaxSize, accept: "application/json", contentType: "application/json", rawBody: raw})
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, cloudWorkspaceAPIError(status, data)
	}
	var out cloudWorkspaceOperationResult
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (t *cloudWorkspaceHTTPTransport) GetEvents(ctx context.Context, after, limit int64) ([]cloudWorkspaceEvent, error) {
	urlPath := cloudWorkspaceItemPath(t.workspaceID) + "/events?after_seq=" + strconv.FormatInt(after, 10) + "&limit=" + strconv.FormatInt(limit, 10)
	data, status, err := t.app.cloudWorkspaceHubDo(ctx, http.MethodGet, urlPath, cloudWorkspaceHTTPOptions{timeout: cloudWorkspaceRequestTimeout, maxRead: cloudWorkspaceResponseMaxSize, accept: "application/json"})
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, cloudWorkspaceAPIError(status, data)
	}
	var payload struct {
		Events []cloudWorkspaceEvent `json:"events"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload.Events, nil
}

func (a *App) cloudWorkspaceProtocol(workspaceID string) *cloudWorkspaceProtocol {
	return &cloudWorkspaceProtocol{
		Transport:    &cloudWorkspaceHTTPTransport{app: a, workspaceID: workspaceID},
		MaxDirectPut: cloudWorkspaceChunkBytes,
		MaxChunk:     cloudWorkspaceChunkBytes,
	}
}
