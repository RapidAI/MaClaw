package guiapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/cloudworkspaceignore"
	"golang.org/x/text/unicode/norm"
)

const (
	cloudWorkspaceCacheStateDir    = ".maclaw-cloud"
	cloudWorkspaceCacheStateFile   = "state.json"
	cloudWorkspaceHashIndexFile    = "hash-index.json"
	cloudWorkspaceHashIndexVersion = 1

	cloudWorkspaceAcquiredGranted = "granted"
	cloudWorkspaceAcquiredRenewed = "renewed"
)

// cloudWorkspaceHashIndexEntry is the local, cache-owned observation for one
// regular file.  It is only an optimisation: a missing/corrupt index always
// falls back to hashing the file, and the manifest remains the correctness
// boundary.  FileID is best-effort (inode/dev on Unix, file-index fields on
// Windows) and prevents reusing a hash after an atomic replace with unchanged
// size/mtime.
type cloudWorkspaceHashIndexEntry struct {
	SHA256          string `json:"sha256"`
	Size            int64  `json:"size"`
	ModTimeUnixNano int64  `json:"mod_time_unix_nano"`
	FileID          string `json:"file_id,omitempty"`
}

type cloudWorkspaceHashIndex struct {
	Version int                                     `json:"version"`
	Entries map[string]cloudWorkspaceHashIndexEntry `json:"entries"`
}

type cloudWorkspaceScanProgress struct {
	FilesScanned int
	FilesReused  int
	BytesScanned int64
}

type cloudWorkspaceScanOptions struct {
	Progress func(cloudWorkspaceScanProgress)
	// Observed, when non-nil, receives the stat identity captured alongside
	// each manifest entry.  Push uses it to detect a replacement that happens
	// after the scan even when the remote tree is otherwise a no-op.
	Observed map[string]cloudWorkspaceObservedFile
	// DisableIndex is used only by tests or a one-shot recovery path.  Normal
	// scans always use the index and refresh it atomically after success.
	DisableIndex bool
}

// cloudWorkspaceLocalState is {cache}/.maclaw-cloud/state.json.
type cloudWorkspaceLocalState struct {
	LastPushedRevision  string                        `json:"last_pushed_revision"`
	BaselineInitialized bool                          `json:"baseline_initialized,omitempty"`
	LastEventSeq        int64                         `json:"last_event_seq,omitempty"`
	PendingOperationIDs []string                      `json:"pending_operation_ids,omitempty"`
	FileRevisions       map[string]string             `json:"file_revisions,omitempty"`
	LastEntries         []cloudWorkspaceManifestEntry `json:"last_entries,omitempty"`
	// ReconcileRequired is durable because fsnotify errors/queue overflows can
	// happen immediately before a GUI restart.  A revision alone cannot prove
	// that every local path was observed, so the next writer must perform a
	// full scan before the marker is cleared.
	ReconcileRequired bool   `json:"reconcile_required,omitempty"`
	ReconcileReason   string `json:"reconcile_reason,omitempty"`
	ReconcileAt       string `json:"reconcile_at,omitempty"`
}

var errCloudWorkspaceV2Unavailable = errors.New("cloud workspace v2 operations unavailable")
var errCloudWorkspaceManifestDeltaUnavailable = errors.New("cloud workspace manifest delta unavailable")

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
	local, err := scanCloudWorkspaceLocalContext(ctx, root)
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

// DeletePaths submits per-file remote deletes and drops them from local sync
// state. It does not upload unrelated local edits.
func (p *cloudWorkspaceProtocol) DeletePaths(ctx context.Context, root string, paths []string) error {
	if p == nil || p.Transport == nil {
		return fmt.Errorf("cloud workspace sync unavailable")
	}
	if _, ok := p.Transport.(cloudWorkspaceV2Transport); !ok {
		return errCloudWorkspaceV2Unavailable
	}
	uniq := uniqueCloudWorkspacePaths(paths)
	if len(uniq) == 0 {
		return nil
	}
	state, err := readCloudWorkspaceLocalState(root)
	if err != nil {
		return err
	}
	_, _, clientID, _ := p.clientIdentity()
	if clientID == "" {
		clientID = "maclaw-gui"
	}
	drop := make(map[string]struct{}, len(uniq))
	persist := func() error {
		kept := make([]cloudWorkspaceManifestEntry, 0, len(state.LastEntries))
		for _, entry := range state.LastEntries {
			if _, ok := drop[entry.Path]; ok {
				continue
			}
			kept = append(kept, entry)
		}
		state.LastEntries = kept
		return writeCloudWorkspaceState(root, state)
	}
	for _, path := range uniq {
		if _, ok := cloudWorkspaceSafeRelPath(path); !ok || codingWorkbenchEntryProtected(path) {
			continue
		}
		op := cloudWorkspaceOperation{
			OpID:             operationID("delete", path, state.FileRevisions[path], ""),
			Path:             path,
			Kind:             "delete",
			BaseFileRevision: state.FileRevisions[path],
			ClientInstanceID: clientID,
		}
		res, err := p.SubmitOperation(ctx, op)
		if err != nil {
			if len(drop) > 0 {
				_ = persist()
			}
			return err
		}
		if res.Accepted {
			drop[path] = struct{}{}
			delete(state.FileRevisions, path)
			continue
		}
		if len(drop) > 0 {
			_ = persist()
		}
		return p.materializeConflict(ctx, root, op, res)
	}
	return persist()
}

func uniqueCloudWorkspacePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

// PullEvents applies remote operations newer than the local cursor. Unmodified
// files are updated in place; locally edited files are preserved and the
// remote version is written as a conflict copy.
func (p *cloudWorkspaceProtocol) PullEvents(ctx context.Context, root string) error {
	if p == nil || p.Transport == nil {
		return fmt.Errorf("cloud workspace sync unavailable")
	}
	if err := ensureCloudWorkspaceCacheDirectory(root, root); err != nil {
		return err
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
		baseline, scanErr := scanCloudWorkspaceLocalContext(ctx, root)
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
		// Conflict events describe a rejected candidate, not the canonical
		// file state. Never apply their object to the working tree; preserve it
		// as a conflict copy so another machine's edit remains inspectable.
		if ev.ConflictOfSeq != 0 {
			if ev.ObjectSHA256 != "" {
				data, getErr := p.Transport.GetObject(ctx, ev.ObjectSHA256, 0)
				if getErr != nil {
					return getErr
				}
				dest := conflictCopyPath(root, ev.Path, time.Now())
				if dest == "" {
					return fmt.Errorf("invalid cloud workspace conflict path %q", ev.Path)
				}
				if err := ensureCloudWorkspaceCacheDirectory(root, filepath.Dir(dest)); err != nil {
					return err
				}
				if writeErr := atomicWriteFile(dest, data); writeErr != nil {
					return writeErr
				}
			}
			continue
		}
		cleaned, ok := cloudWorkspaceSafeRelPath(ev.Path)
		if !ok {
			return fmt.Errorf("invalid cloud workspace event path %q", ev.Path)
		}
		ev.Path = cleaned
		localPath := filepath.Join(root, filepath.FromSlash(ev.Path))
		if err := ensureCloudWorkspaceCacheDirectory(root, filepath.Dir(localPath)); err != nil {
			return err
		}
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
	if err := ensureCloudWorkspaceCacheDirectory(root, filepath.Dir(dest)); err != nil {
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
	if p == "" || strings.TrimSpace(p) != p || strings.ContainsAny(p, `\:`) || strings.ContainsRune(p, 0) {
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
		if seg == "" || seg == "." || seg == ".." || !portableCloudWorkspaceSegment(seg) {
			return "", false
		}
	}
	return cleaned, true
}

func portableCloudWorkspaceSegment(seg string) bool {
	if strings.HasSuffix(seg, ".") || strings.HasSuffix(seg, " ") {
		return false
	}
	base := strings.TrimRight(seg, " .")
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return false
	default:
		return true
	}
}

func cloudWorkspacePortablePathKey(p string) string {
	return strings.ToLower(norm.NFC.String(p))
}

type cloudWorkspaceManifestEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type cloudWorkspaceManifest struct {
	Revision     string                        `json:"revision"`
	ManifestHash string                        `json:"manifest_hash,omitempty"`
	Entries      []cloudWorkspaceManifestEntry `json:"entries"`
}

func cloudWorkspaceManifestHash(entries []cloudWorkspaceManifestEntry) string {
	entries = append([]cloudWorkspaceManifestEntry(nil), entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.Path)
		b.WriteByte(0)
		b.WriteString(e.SHA256)
		b.WriteByte(0)
		b.WriteString(strconv.FormatInt(e.Size, 10))
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

type cloudWorkspaceSyncTransport interface {
	GetManifest(ctx context.Context) (*cloudWorkspaceManifest, error)
	PutManifest(ctx context.Context, ifMatch string, entries []cloudWorkspaceManifestEntry) (*cloudWorkspaceManifest, error)
	GetObject(ctx context.Context, sha256hex string, sizeBytes int64) ([]byte, error)
	PutObject(ctx context.Context, sha256hex string, data []byte) error
	PutChunk(ctx context.Context, sha256hex string, index int, data []byte) error
	CompleteObject(ctx context.Context, sha256hex string) error
}

type cloudWorkspaceManifestDeltaTransport interface {
	PutManifestDelta(ctx context.Context, delta cloudWorkspaceManifestDelta) (*cloudWorkspaceManifest, error)
}

type cloudWorkspaceManifestDelta struct {
	IfMatchRevision string                        `json:"if_match_revision"`
	Puts            []cloudWorkspaceManifestEntry `json:"puts,omitempty"`
	Deletes         []string                      `json:"deletes,omitempty"`
}

func cloudWorkspaceIdempotencyKey(op string, payload []byte) string {
	sum := sha256.Sum256(append([]byte(op+":"), payload...))
	return "cwid_" + hex.EncodeToString(sum[:16])
}

type cloudWorkspaceProtocol struct {
	Transport    cloudWorkspaceSyncTransport
	MaxDirectPut int64
	MaxChunk     int64
}

// A single Hub can serve several GUI workspaces at once.  Bound object
// transfers globally so one large workspace cannot monopolise sockets/IO; the
// per-workspace syncMu still preserves ordering, while unrelated workspaces
// can make progress through the two transfer slots.
var cloudWorkspaceUploadSlots = make(chan struct{}, 2)

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
		return nil, errCloudWorkspaceV2Unavailable
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
	return hashCloudWorkspaceFileContext(context.Background(), path)
}

// hashCloudWorkspaceFileContext hashes in bounded chunks and checks ctx
// between reads.  io.Copy cannot be interrupted while a large buffered read
// is in progress, which used to make a cancelled reconcile continue burning
// CPU/IO until the whole file had been consumed.
func hashCloudWorkspaceFileContext(ctx context.Context, filePath string) (string, int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	f, err := os.Open(filePath)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 256*1024)
	var n int64
	for {
		select {
		case <-ctx.Done():
			return "", n, ctx.Err()
		default:
		}
		readN, readErr := f.Read(buf)
		if readN > 0 {
			if _, err := h.Write(buf[:readN]); err != nil {
				return "", n, err
			}
			n += int64(readN)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return "", n, readErr
		}
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func cloudWorkspaceHashIndexPath(root string) string {
	return filepath.Join(root, cloudWorkspaceCacheStateDir, cloudWorkspaceHashIndexFile)
}

func readCloudWorkspaceHashIndex(root string) cloudWorkspaceHashIndex {
	idx := cloudWorkspaceHashIndex{Version: cloudWorkspaceHashIndexVersion, Entries: map[string]cloudWorkspaceHashIndexEntry{}}
	if err := cloudWorkspaceCacheDirectoryExistsAndSafe(root, filepath.Join(root, cloudWorkspaceCacheStateDir)); err != nil {
		return idx
	}
	raw, err := os.ReadFile(cloudWorkspaceHashIndexPath(root))
	if err != nil {
		return idx
	}
	var decoded cloudWorkspaceHashIndex
	if json.Unmarshal(raw, &decoded) != nil || decoded.Version != cloudWorkspaceHashIndexVersion || decoded.Entries == nil {
		return idx
	}
	for p, entry := range decoded.Entries {
		if _, ok := cloudWorkspaceSafeRelPath(p); !ok || !validCloudWorkspaceSHA256(entry.SHA256) || entry.Size < 0 || entry.ModTimeUnixNano <= 0 {
			continue
		}
		idx.Entries[p] = entry
	}
	return idx
}

func writeCloudWorkspaceHashIndex(root string, idx cloudWorkspaceHashIndex) error {
	if idx.Version == 0 {
		idx.Version = cloudWorkspaceHashIndexVersion
	}
	if idx.Entries == nil {
		idx.Entries = map[string]cloudWorkspaceHashIndexEntry{}
	}
	raw, err := json.Marshal(idx)
	if err != nil {
		return err
	}
	dir := filepath.Join(root, cloudWorkspaceCacheStateDir)
	if err := ensureCloudWorkspaceCacheDirectory(root, dir); err != nil {
		return err
	}
	return atomicWriteFile(cloudWorkspaceHashIndexPath(root), raw)
}

func validCloudWorkspaceSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// cloudWorkspaceFileID returns a stable best-effort identity without using
// platform-specific syscall types.  The reflected fields cover os.Stat's
// inode/device values on Unix and file-index values on Windows; when a
// platform exposes neither, an empty id simply disables that optimisation.
func cloudWorkspaceFileID(info os.FileInfo) string {
	if info == nil || info.Sys() == nil {
		return ""
	}
	v := reflect.ValueOf(info.Sys())
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return ""
	}
	fields := []string{"Dev", "Ino", "FileIndexHigh", "FileIndexLow", "VolumeSerialNumber"}
	parts := make([]string, 0, len(fields))
	for _, name := range fields {
		field := v.FieldByName(name)
		if !field.IsValid() || !field.CanInterface() {
			continue
		}
		parts = append(parts, name+"="+fmt.Sprint(field.Interface()))
	}
	return strings.Join(parts, ",")
}

// cloudWorkspaceObservedFile is a small, portable file identity used to make
// the scan/transfer boundary fail closed.  A manifest entry is derived from a
// particular inode (or best-effort file index) and metadata observation; if an
// editor replaces or mutates that file before it is uploaded/downloaded, the
// operation must stop instead of publishing or overwriting a mixed generation.
type cloudWorkspaceObservedFile struct {
	Exists  bool
	Regular bool
	Size    int64
	ModTime int64
	FileID  string
}

func observeCloudWorkspaceFile(filePath string) (cloudWorkspaceObservedFile, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return cloudWorkspaceObservedFile{}, nil
		}
		return cloudWorkspaceObservedFile{}, err
	}
	return cloudWorkspaceObservedFile{
		Exists:  true,
		Regular: info.Mode().IsRegular(),
		Size:    info.Size(),
		ModTime: info.ModTime().UnixNano(),
		FileID:  cloudWorkspaceFileID(info),
	}, nil
}

func sameCloudWorkspaceObservedFile(a, b cloudWorkspaceObservedFile) bool {
	if a.Exists != b.Exists || a.Regular != b.Regular {
		return false
	}
	if !a.Exists {
		return true
	}
	return a.Size == b.Size && a.ModTime == b.ModTime && a.FileID == b.FileID
}

func cloudWorkspaceStatePath(root string) string {
	return filepath.Join(root, cloudWorkspaceCacheStateDir, cloudWorkspaceCacheStateFile)
}

func readCloudWorkspaceLocalState(root string) (cloudWorkspaceLocalState, error) {
	if err := cloudWorkspaceCacheDirectoryExistsAndSafe(root, filepath.Join(root, cloudWorkspaceCacheStateDir)); err != nil {
		if os.IsNotExist(err) {
			return cloudWorkspaceLocalState{}, nil
		}
		return cloudWorkspaceLocalState{}, err
	}
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
	if err := ensureCloudWorkspaceCacheDirectory(root, dir); err != nil {
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

// writeCloudWorkspaceManifestState persists the complete v1 baseline. A
// revision alone cannot tell whether local files changed after a restart.
func writeCloudWorkspaceManifestState(root string, manifest *cloudWorkspaceManifest) error {
	state, err := readCloudWorkspaceLocalState(root)
	if err != nil {
		return err
	}
	if manifest == nil {
		manifest = &cloudWorkspaceManifest{}
	}
	state.LastPushedRevision = manifest.Revision
	state.BaselineInitialized = true
	state.LastEntries = append(state.LastEntries[:0], manifest.Entries...)
	state.LastEventSeq = 0
	state.PendingOperationIDs = nil
	state.FileRevisions = map[string]string{}
	// A successful full Pull/Push establishes a complete baseline.  Clearing
	// the marker here keeps all commit paths consistent, including no-op pushes.
	state.ReconcileRequired = false
	state.ReconcileReason = ""
	state.ReconcileAt = ""
	return writeCloudWorkspaceState(root, state)
}

// markCloudWorkspaceLocalReconcileRequired persists a watcher self-healing
// marker.  The marker is intentionally best-effort: the in-memory mount still
// fails closed when state.json cannot be written, while a later successful
// manifest commit can clear it.
func markCloudWorkspaceLocalReconcileRequired(root, reason string) (bool, error) {
	root = normalizeProjectSessionPath(root)
	if root == "" {
		return false, nil
	}
	state, err := readCloudWorkspaceLocalState(root)
	if err != nil {
		return false, err
	}
	wasRequired := state.ReconcileRequired
	state.ReconcileRequired = true
	if reason = strings.TrimSpace(reason); reason != "" {
		state.ReconcileReason = reason
	}
	state.ReconcileAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeCloudWorkspaceState(root, state); err != nil {
		return !wasRequired, err
	}
	return !wasRequired, nil
}

func writeCloudWorkspaceState(root string, state cloudWorkspaceLocalState) error {
	dir := filepath.Join(root, cloudWorkspaceCacheStateDir)
	if err := ensureCloudWorkspaceCacheDirectory(root, dir); err != nil {
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
	return scanCloudWorkspaceLocalContext(context.Background(), root)
}

// scanCloudWorkspaceLocalContext is the cancellable scanner used by active
// Push/reconcile paths.  Hashing a large cache must honor the caller's
// request/lease shutdown context instead of running to completion after the
// writer has already been fenced or the user cancelled the operation.
func scanCloudWorkspaceLocalContext(ctx context.Context, root string) ([]cloudWorkspaceManifestEntry, error) {
	return scanCloudWorkspaceLocalContextWithOptions(ctx, root, cloudWorkspaceScanOptions{})
}

func scanCloudWorkspaceLocalContextWithOptions(ctx context.Context, root string, options cloudWorkspaceScanOptions) ([]cloudWorkspaceManifestEntry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ensureCloudWorkspaceCacheDirectory(root, root); err != nil {
		return nil, err
	}
	cloudignore, err := cloudworkspaceignore.ReadCloudignore(root)
	if err != nil {
		return nil, err
	}
	matcher := cloudworkspaceignore.NewMatcher(cloudignore)
	var entries []cloudWorkspaceManifestEntry
	index := cloudWorkspaceHashIndex{Version: cloudWorkspaceHashIndexVersion, Entries: map[string]cloudWorkspaceHashIndexEntry{}}
	if !options.DisableIndex {
		index = readCloudWorkspaceHashIndex(root)
	}
	nextIndex := cloudWorkspaceHashIndex{Version: cloudWorkspaceHashIndexVersion, Entries: make(map[string]cloudWorkspaceHashIndexEntry)}
	progress := cloudWorkspaceScanProgress{}
	portablePaths := make(map[string]string)
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
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
		if len(entries) >= cloudWorkspaceMaxManifestEntries {
			return fmt.Errorf("cloud workspace file count exceeds %d", cloudWorkspaceMaxManifestEntries)
		}
		if info.Size() > cloudWorkspaceObjectMaxBytes {
			return fmt.Errorf("cloud workspace object exceeds %d bytes: %q", cloudWorkspaceObjectMaxBytes, rel)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		cleaned, ok := cloudWorkspaceSafeRelPath(rel)
		if !ok {
			return fmt.Errorf("invalid cloud workspace path %q", rel)
		}
		portableKey := cloudWorkspacePortablePathKey(cleaned)
		if previous, exists := portablePaths[portableKey]; exists && previous != cleaned {
			return fmt.Errorf("cloud workspace path collision between %q and %q", previous, cleaned)
		}
		portablePaths[portableKey] = cleaned
		fileID := cloudWorkspaceFileID(info)
		indexed, reused := index.Entries[cleaned]
		var sum string
		var size int64
		if !options.DisableIndex && reused && indexed.Size == info.Size() && indexed.ModTimeUnixNano == info.ModTime().UnixNano() && indexed.FileID == fileID {
			sum, size = indexed.SHA256, indexed.Size
			progress.FilesReused++
			progress.BytesScanned += size
		} else {
			// Retry once if an editor atomically replaced the file while it was
			// being hashed.  Returning a mixed-generation manifest is worse than
			// a bounded retry and lets the watcher schedule another pass.
			for attempt := 0; attempt < 2; attempt++ {
				sum, size, err = hashCloudWorkspaceFileContext(ctx, p)
				if err != nil {
					return err
				}
				post, statErr := os.Stat(p)
				if statErr != nil {
					if os.IsNotExist(statErr) {
						return statErr
					}
					return statErr
				}
				if post.Size() == info.Size() && post.ModTime().UnixNano() == info.ModTime().UnixNano() && cloudWorkspaceFileID(post) == fileID {
					break
				}
				if attempt == 1 {
					return fmt.Errorf("cloud workspace file changed while scanning %q", rel)
				}
				info = post
				fileID = cloudWorkspaceFileID(info)
			}
			progress.FilesScanned++
			progress.BytesScanned += size
		}
		if options.Observed != nil {
			options.Observed[cleaned] = cloudWorkspaceObservedFile{Exists: true, Regular: true, Size: info.Size(), ModTime: info.ModTime().UnixNano(), FileID: fileID}
		}
		nextIndex.Entries[cleaned] = cloudWorkspaceHashIndexEntry{SHA256: sum, Size: size, ModTimeUnixNano: info.ModTime().UnixNano(), FileID: fileID}
		entries = append(entries, cloudWorkspaceManifestEntry{Path: rel, SHA256: sum, Size: size})
		if options.Progress != nil {
			options.Progress(progress)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if entries == nil {
		entries = []cloudWorkspaceManifestEntry{}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	if !options.DisableIndex {
		if err := writeCloudWorkspaceHashIndex(root, nextIndex); err != nil {
			// Hash index is an optimisation and must never turn a valid sync into
			// an error (e.g. a read-only cache or a transient disk-full condition).
			log.Printf("[cloud_workspace] persist hash index failed root=%q err=%v", root, err)
		}
	}
	return entries, nil
}

// listCloudWorkspaceFilesUnder returns cache-relative slash paths of regular
// files covered by cacheRel. Directories are walked in place; the whole
// workspace is not hashed.
func listCloudWorkspaceFilesUnder(root, cacheRel string, isDir bool) ([]string, error) {
	cacheRel = strings.TrimSpace(cacheRel)
	if cacheRel == "" {
		return nil, nil
	}
	if !isDir {
		return []string{cacheRel}, nil
	}
	cloudignore, err := cloudworkspaceignore.ReadCloudignore(root)
	if err != nil {
		return nil, err
	}
	matcher := cloudworkspaceignore.NewMatcher(cloudignore)
	target := filepath.Join(root, filepath.FromSlash(cacheRel))
	var paths []string
	err = filepath.WalkDir(target, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || rel == cacheRel {
			if d.IsDir() {
				return nil
			}
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
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if rel == cacheRel || strings.HasPrefix(rel, cacheRel+"/") {
			paths = append(paths, rel)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return paths, nil
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

type cloudWorkspaceCacheSyncKind int

const (
	cloudWorkspaceSyncNone cloudWorkspaceCacheSyncKind = iota
	cloudWorkspaceSyncPull
	cloudWorkspaceSyncPush
	cloudWorkspaceSyncConflict
)

func (k cloudWorkspaceCacheSyncKind) String() string {
	switch k {
	case cloudWorkspaceSyncPull:
		return "pull"
	case cloudWorkspaceSyncPush:
		return "push"
	case cloudWorkspaceSyncConflict:
		return "conflict"
	default:
		return "none"
	}
}

func cloudWorkspaceTreeDiff(local, remote []cloudWorkspaceManifestEntry) (localOnly, remoteOnly, changed int) {
	type meta struct {
		sha  string
		size int64
	}
	rem := make(map[string]meta, len(remote))
	for _, e := range remote {
		rem[e.Path] = meta{sha: e.SHA256, size: e.Size}
	}
	loc := make(map[string]meta, len(local))
	for _, e := range local {
		loc[e.Path] = meta{sha: e.SHA256, size: e.Size}
	}
	for path, l := range loc {
		r, ok := rem[path]
		if !ok {
			localOnly++
			continue
		}
		if l.sha != r.sha || l.size != r.size {
			changed++
		}
	}
	for path := range rem {
		if _, ok := loc[path]; !ok {
			remoteOnly++
		}
	}
	return localOnly, remoteOnly, changed
}

func cloudWorkspaceCacheSyncPlan(root string, remote *cloudWorkspaceManifest, afterSteal bool) (cloudWorkspaceCacheSyncKind, error) {
	return cloudWorkspaceCacheSyncPlanContext(context.Background(), root, remote, afterSteal)
}

func cloudWorkspaceCacheSyncPlanContext(ctx context.Context, root string, remote *cloudWorkspaceManifest, afterSteal bool) (cloudWorkspaceCacheSyncKind, error) {
	local, err := scanCloudWorkspaceLocalContext(ctx, root)
	if err != nil {
		return cloudWorkspaceSyncNone, err
	}
	var remoteEntries []cloudWorkspaceManifestEntry
	remoteRev := ""
	if remote != nil {
		remoteEntries = remote.Entries
		remoteRev = strings.TrimSpace(remote.Revision)
	}
	if cloudWorkspaceTreesEqual(local, remoteEntries) {
		return cloudWorkspaceSyncNone, nil
	}
	state, err := readCloudWorkspaceLocalState(root)
	if err != nil {
		return cloudWorkspaceSyncNone, err
	}
	last := strings.TrimSpace(state.LastPushedRevision)
	baselineKnown := state.BaselineInitialized || state.LastEntries != nil
	// Brand-new cache: pull without a prompt. A steal of a leftover cache must
	// still confirm before overwriting local files.
	if last == "" && !baselineKnown && !afterSteal {
		return cloudWorkspaceSyncPull, nil
	}
	localOnly, remoteOnly, changed := cloudWorkspaceTreeDiff(local, remoteEntries)
	if afterSteal {
		return cloudWorkspaceSyncConflict, nil
	}
	// Same revision as last successful sync: this machine is the writer.
	// Unpushed extras or edits must be pushed. Pulling would roll files back.
	if baselineKnown && last == remoteRev {
		if localOnly == 0 && changed == 0 {
			return cloudWorkspaceSyncPull, nil
		}
		return cloudWorkspaceSyncPush, nil
	}
	if localOnly == 0 && changed == 0 {
		return cloudWorkspaceSyncPull, nil
	}
	if remoteOnly == 0 && changed == 0 {
		return cloudWorkspaceSyncPush, nil
	}
	return cloudWorkspaceSyncConflict, nil
}

func cloudWorkspaceCacheDirty(root string, remote *cloudWorkspaceManifest, afterSteal bool) (bool, error) {
	kind, err := cloudWorkspaceCacheSyncPlan(root, remote, afterSteal)
	if err != nil {
		return false, err
	}
	return kind == cloudWorkspaceSyncConflict, nil
}

func (p *cloudWorkspaceProtocol) putBytes(ctx context.Context, sha string, data []byte) error {
	if p == nil || p.Transport == nil {
		return fmt.Errorf("cloud workspace sync unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if int64(len(data)) > cloudWorkspaceObjectMaxBytes {
		return fmt.Errorf("cloud workspace object exceeds %d bytes", cloudWorkspaceObjectMaxBytes)
	}
	select {
	case cloudWorkspaceUploadSlots <- struct{}{}:
		defer func() { <-cloudWorkspaceUploadSlots }()
	case <-ctx.Done():
		return ctx.Err()
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
	return p.pushWithProgress(ctx, root, nil)
}

func (p *cloudWorkspaceProtocol) PushWithProgress(ctx context.Context, root string, progress func(cloudWorkspaceSyncProgress)) (*cloudWorkspaceManifest, error) {
	return p.pushWithProgress(ctx, root, progress)
}

type cloudWorkspaceSyncProgress struct {
	Phase      string `json:"phase"`
	FilesDone  int    `json:"files_done"`
	FilesTotal int    `json:"files_total,omitempty"`
	BytesDone  int64  `json:"bytes_done"`
	BytesTotal int64  `json:"bytes_total,omitempty"`
	Cancelable bool   `json:"cancelable"`
}

func (p *cloudWorkspaceProtocol) pushWithProgress(ctx context.Context, root string, progress func(cloudWorkspaceSyncProgress)) (*cloudWorkspaceManifest, error) {
	if p == nil || p.Transport == nil {
		return nil, fmt.Errorf("cloud workspace sync unavailable")
	}
	observed := make(map[string]cloudWorkspaceObservedFile)
	entries, err := scanCloudWorkspaceLocalContextWithOptions(ctx, root, cloudWorkspaceScanOptions{Observed: observed, Progress: func(sc cloudWorkspaceScanProgress) {
		if progress != nil {
			progress(cloudWorkspaceSyncProgress{Phase: "scan", FilesDone: sc.FilesScanned + sc.FilesReused, BytesDone: sc.BytesScanned, Cancelable: true})
		}
	}})
	if err != nil {
		return nil, err
	}
	if progress != nil {
		progress(cloudWorkspaceSyncProgress{Phase: "upload", FilesTotal: len(entries), BytesTotal: totalEntryBytes(entries), Cancelable: true})
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
	if len(remote.Entries) > cloudWorkspaceMaxManifestEntries {
		return nil, fmt.Errorf("cloud workspace file count exceeds %d", cloudWorkspaceMaxManifestEntries)
	}
	have := make(map[string]struct{}, len(remote.Entries))
	for _, e := range remote.Entries {
		if e.Size < 0 || e.Size > cloudWorkspaceObjectMaxBytes || !validCloudWorkspaceSHA256(e.SHA256) {
			return nil, fmt.Errorf("invalid cloud workspace object metadata for %q", e.Path)
		}
		have[e.SHA256] = struct{}{}
	}
	if cloudWorkspaceTreesEqual(entries, remote.Entries) {
		for _, e := range entries {
			cleaned, ok := cloudWorkspaceSafeRelPath(e.Path)
			if !ok {
				return nil, fmt.Errorf("invalid cloud workspace path %q", e.Path)
			}
			before := observed[cleaned]
			current, statErr := observeCloudWorkspaceFile(filepath.Join(root, filepath.FromSlash(e.Path)))
			if statErr != nil {
				return nil, statErr
			}
			if !sameCloudWorkspaceObservedFile(before, current) {
				return nil, fmt.Errorf("cloud workspace file changed while uploading %q", e.Path)
			}
		}
		if err := writeCloudWorkspaceManifestState(root, remote); err != nil {
			return nil, err
		}
		return remote, nil
	}
	uploaded := map[string]struct{}{}
	var uploadedFiles int
	var uploadedBytes int64
	for _, e := range entries {
		filePath := filepath.Join(root, filepath.FromSlash(e.Path))
		before, err := observeCloudWorkspaceFile(filePath)
		if err != nil {
			return nil, err
		}
		if !before.Exists || !before.Regular || before.Size != e.Size {
			return nil, fmt.Errorf("cloud workspace file changed while uploading %q", e.Path)
		}
		if _, ok := have[e.SHA256]; ok {
			// Even when the object already exists remotely, the path still enters
			// the new manifest.  Re-check its identity so a delete/replace racing
			// the scan cannot silently publish a stale path.
			after, statErr := observeCloudWorkspaceFile(filePath)
			if statErr != nil {
				return nil, statErr
			}
			if !sameCloudWorkspaceObservedFile(before, after) {
				return nil, fmt.Errorf("cloud workspace file changed while uploading %q", e.Path)
			}
			continue
		}
		if _, ok := uploaded[e.SHA256]; ok {
			after, statErr := observeCloudWorkspaceFile(filePath)
			if statErr != nil {
				return nil, statErr
			}
			if !sameCloudWorkspaceObservedFile(before, after) {
				return nil, fmt.Errorf("cloud workspace file changed while uploading %q", e.Path)
			}
			continue
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, err
		}
		// The scan hash is the commit candidate.  Re-hash the bytes actually
		// read so a file replaced between scan and ReadFile cannot be uploaded
		// under the old digest (which would otherwise make the manifest point at
		// the wrong content).
		actualSum := sha256.Sum256(data)
		if int64(len(data)) != e.Size || hex.EncodeToString(actualSum[:]) != e.SHA256 {
			return nil, fmt.Errorf("cloud workspace file changed while uploading %q", e.Path)
		}
		afterRead, err := observeCloudWorkspaceFile(filePath)
		if err != nil {
			return nil, err
		}
		if !sameCloudWorkspaceObservedFile(before, afterRead) {
			return nil, fmt.Errorf("cloud workspace file changed while uploading %q", e.Path)
		}
		if err := p.putBytes(ctx, e.SHA256, data); err != nil {
			return nil, err
		}
		afterUpload, err := observeCloudWorkspaceFile(filePath)
		if err != nil {
			return nil, err
		}
		if !sameCloudWorkspaceObservedFile(before, afterUpload) {
			return nil, fmt.Errorf("cloud workspace file changed while uploading %q", e.Path)
		}
		uploaded[e.SHA256] = struct{}{}
		uploadedFiles++
		uploadedBytes += int64(len(data))
		if progress != nil {
			progress(cloudWorkspaceSyncProgress{Phase: "upload", FilesDone: uploadedFiles, FilesTotal: len(entries), BytesDone: uploadedBytes, BytesTotal: totalEntryBytes(entries), Cancelable: true})
		}
	}
	var out *cloudWorkspaceManifest
	if deltaTransport, ok := p.Transport.(cloudWorkspaceManifestDeltaTransport); ok {
		oldByPath := make(map[string]cloudWorkspaceManifestEntry, len(remote.Entries))
		for _, e := range remote.Entries {
			oldByPath[e.Path] = e
		}
		newByPath := make(map[string]cloudWorkspaceManifestEntry, len(entries))
		for _, e := range entries {
			newByPath[e.Path] = e
		}
		delta := cloudWorkspaceManifestDelta{IfMatchRevision: remote.Revision}
		for p := range oldByPath {
			if _, ok := newByPath[p]; !ok {
				delta.Deletes = append(delta.Deletes, p)
			}
		}
		for p, e := range newByPath {
			old, ok := oldByPath[p]
			if !ok || old.SHA256 != e.SHA256 || old.Size != e.Size {
				delta.Puts = append(delta.Puts, e)
			}
		}
		sort.Strings(delta.Deletes)
		sort.Slice(delta.Puts, func(i, j int) bool { return delta.Puts[i].Path < delta.Puts[j].Path })
		out, err = deltaTransport.PutManifestDelta(ctx, delta)
		if errors.Is(err, errCloudWorkspaceManifestDeltaUnavailable) {
			out, err = p.Transport.PutManifest(ctx, remote.Revision, entries)
		}
	} else {
		out, err = p.Transport.PutManifest(ctx, remote.Revision, entries)
	}
	if err != nil {
		return nil, err
	}
	if out != nil {
		if err := writeCloudWorkspaceManifestState(root, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func totalEntryBytes(entries []cloudWorkspaceManifestEntry) int64 {
	var total int64
	for _, e := range entries {
		total += e.Size
	}
	return total
}

func (p *cloudWorkspaceProtocol) Pull(ctx context.Context, root string) (*cloudWorkspaceManifest, error) {
	return p.pullWithProgress(ctx, root, nil)
}

func (p *cloudWorkspaceProtocol) PullWithProgress(ctx context.Context, root string, progress func(cloudWorkspaceSyncProgress)) (*cloudWorkspaceManifest, error) {
	return p.pullWithProgress(ctx, root, progress)
}

func (p *cloudWorkspaceProtocol) pullWithProgress(ctx context.Context, root string, progress func(cloudWorkspaceSyncProgress)) (*cloudWorkspaceManifest, error) {
	if p == nil || p.Transport == nil {
		return nil, fmt.Errorf("cloud workspace sync unavailable")
	}
	if err := ensureCloudWorkspaceCacheDirectory(root, root); err != nil {
		return nil, err
	}
	cloudignore, err := cloudworkspaceignore.ReadCloudignore(root)
	if err != nil {
		return nil, err
	}
	matcher := cloudworkspaceignore.NewMatcher(cloudignore)
	// Capture the local tree before any network I/O.  Pull is allowed to
	// overwrite only files that stayed byte-for-byte at the observed identity;
	// otherwise a concurrent editor change would be silently discarded.
	initialLocal := make(map[string]cloudWorkspaceObservedFile)
	if err := filepath.WalkDir(root, func(pth string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(root, pth)
		if relErr != nil {
			return relErr
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
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		if info.Mode().IsRegular() {
			initialLocal[rel] = cloudWorkspaceObservedFile{Exists: true, Regular: true, Size: info.Size(), ModTime: info.ModTime().UnixNano(), FileID: cloudWorkspaceFileID(info)}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	remote, err := p.Transport.GetManifest(ctx)
	if err != nil {
		return nil, err
	}
	if remote == nil {
		remote = &cloudWorkspaceManifest{Entries: []cloudWorkspaceManifestEntry{}}
	}
	if len(remote.Entries) > cloudWorkspaceMaxManifestEntries {
		return nil, fmt.Errorf("cloud workspace file count exceeds %d", cloudWorkspaceMaxManifestEntries)
	}
	if progress != nil {
		progress(cloudWorkspaceSyncProgress{Phase: "download", FilesTotal: len(remote.Entries), BytesTotal: totalEntryBytes(remote.Entries), Cancelable: true})
	}
	var cancel context.CancelFunc
	ctx, cancel = bindCloudWorkspaceTimeout(ctx, cloudWorkspaceEntriesTimeout(remote.Entries))
	defer cancel()
	keep := make(map[string]struct{}, len(remote.Entries))
	portablePaths := make(map[string]string, len(remote.Entries))
	var downloadedFiles int
	var downloadedBytes int64
	for _, e := range remote.Entries {
		if e.Size < 0 || e.Size > cloudWorkspaceObjectMaxBytes || !validCloudWorkspaceSHA256(e.SHA256) {
			return nil, fmt.Errorf("invalid cloud workspace object metadata for %q", e.Path)
		}
		downloadedFiles++
		downloadedBytes += e.Size
		emitDownloadProgress := func() {
			if progress != nil {
				progress(cloudWorkspaceSyncProgress{Phase: "download", FilesDone: downloadedFiles, FilesTotal: len(remote.Entries), BytesDone: downloadedBytes, BytesTotal: totalEntryBytes(remote.Entries), Cancelable: true})
			}
		}
		cleaned, ok := cloudWorkspaceSafeRelPath(e.Path)
		if !ok {
			return nil, fmt.Errorf("invalid cloud workspace path %q", e.Path)
		}
		portableKey := cloudWorkspacePortablePathKey(cleaned)
		if previous, exists := portablePaths[portableKey]; exists && previous != cleaned {
			return nil, fmt.Errorf("cloud workspace path collision between %q and %q", previous, cleaned)
		}
		portablePaths[portableKey] = cleaned
		keep[cleaned] = struct{}{}
		dest := filepath.Join(root, filepath.FromSlash(cleaned))
		if err := ensureCloudWorkspaceCacheDirectory(root, filepath.Dir(dest)); err != nil {
			return nil, err
		}
		before, existed := initialLocal[cleaned]
		current, statErr := observeCloudWorkspaceFile(dest)
		if statErr != nil {
			return nil, statErr
		}
		if existed {
			if !sameCloudWorkspaceObservedFile(before, current) {
				return nil, fmt.Errorf("cloud workspace file changed while downloading %q", cleaned)
			}
		} else if current.Exists {
			// A file appeared after the initial snapshot.  It may be a user's
			// concurrent edit or a stale artifact; do not overwrite it.
			return nil, fmt.Errorf("cloud workspace file changed while downloading %q", cleaned)
		}
		if info, err := os.Stat(dest); err == nil && info.Mode().IsRegular() {
			sum, size, err := hashCloudWorkspaceFileContext(ctx, dest)
			if err == nil && sum == e.SHA256 && size == e.Size {
				afterHash, statErr := observeCloudWorkspaceFile(dest)
				if statErr != nil {
					return nil, statErr
				}
				if !sameCloudWorkspaceObservedFile(before, afterHash) {
					return nil, fmt.Errorf("cloud workspace file changed while downloading %q", cleaned)
				}
				emitDownloadProgress()
				continue
			}
		}
		data, err := p.Transport.GetObject(ctx, e.SHA256, e.Size)
		if err != nil {
			return nil, err
		}
		afterFetch, statErr := observeCloudWorkspaceFile(dest)
		if statErr != nil {
			return nil, statErr
		}
		if existed {
			if !sameCloudWorkspaceObservedFile(before, afterFetch) {
				return nil, fmt.Errorf("cloud workspace file changed while downloading %q", cleaned)
			}
		} else if afterFetch.Exists {
			return nil, fmt.Errorf("cloud workspace file changed while downloading %q", cleaned)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != e.SHA256 {
			return nil, fmt.Errorf("cloud workspace object hash mismatch")
		}
		if err := atomicWriteFile(dest, data); err != nil {
			return nil, err
		}
		emitDownloadProgress()
	}
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
		before, existed := initialLocal[rel]
		current, statErr := observeCloudWorkspaceFile(p)
		if statErr != nil {
			return statErr
		}
		if !existed || !sameCloudWorkspaceObservedFile(before, current) {
			return fmt.Errorf("cloud workspace file changed while downloading %q", rel)
		}
		return os.Remove(p)
	})
	if err != nil {
		return nil, err
	}
	if err := pruneCloudWorkspaceEmptyDirs(root, matcher); err != nil {
		return nil, err
	}
	if err := writeCloudWorkspaceManifestState(root, remote); err != nil {
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
	if strings.TrimSpace(out.ManifestHash) != "" && out.ManifestHash != cloudWorkspaceManifestHash(out.Entries) {
		return nil, fmt.Errorf("cloud workspace manifest hash mismatch")
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
		headers:     map[string]string{"Idempotency-Key": cloudWorkspaceIdempotencyKey("manifest-put", raw)},
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
	if strings.TrimSpace(out.ManifestHash) != "" && out.ManifestHash != cloudWorkspaceManifestHash(out.Entries) {
		return nil, fmt.Errorf("cloud workspace manifest hash mismatch")
	}
	return &out, nil
}

func (t *cloudWorkspaceHTTPTransport) PutManifestDelta(ctx context.Context, delta cloudWorkspaceManifestDelta) (*cloudWorkspaceManifest, error) {
	if t == nil || t.app == nil {
		return nil, fmt.Errorf("cloud workspace sync unavailable")
	}
	raw, err := json.Marshal(delta)
	if err != nil {
		return nil, err
	}
	data, status, err := t.app.cloudWorkspaceHubDo(ctx, http.MethodPost, cloudWorkspaceItemPath(t.workspaceID)+"/manifest-delta", cloudWorkspaceHTTPOptions{
		timeout: cloudWorkspaceTransferTimeout(int64(len(raw))), maxRead: cloudWorkspaceManifestMaxSize,
		accept: "application/json", contentType: "application/json", rawBody: raw,
		headers: map[string]string{"Idempotency-Key": cloudWorkspaceIdempotencyKey("manifest-delta", raw)},
	})
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		if status == http.StatusNotFound || status == http.StatusMethodNotAllowed {
			return nil, errCloudWorkspaceManifestDeltaUnavailable
		}
		return nil, cloudWorkspaceAPIError(status, data)
	}
	var out cloudWorkspaceManifest
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("invalid cloud workspace manifest delta response: %w", err)
	}
	if out.Entries == nil {
		out.Entries = []cloudWorkspaceManifestEntry{}
	}
	if strings.TrimSpace(out.ManifestHash) != "" && out.ManifestHash != cloudWorkspaceManifestHash(out.Entries) {
		return nil, fmt.Errorf("cloud workspace manifest hash mismatch")
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
		headers:     map[string]string{"Idempotency-Key": cloudWorkspaceIdempotencyKey("object-put-"+sha256hex, data)},
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
		headers:     map[string]string{"Idempotency-Key": cloudWorkspaceIdempotencyKey("object-chunk-"+sha256hex+"-"+strconv.Itoa(index), data)},
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
		headers: map[string]string{"Idempotency-Key": cloudWorkspaceIdempotencyKey("object-complete-"+sha256hex, nil)},
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
