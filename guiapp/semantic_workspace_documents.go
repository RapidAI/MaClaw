package guiapp

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

// workspaceDocumentExts are the files the current-channel delivery already
// treats as documents. A shell-produced PDF is the same kind of product as an
// office write; it was previously invisible because delivery only consumed the
// artifact a renderer had registered.
var workspaceDocumentExts = map[string]bool{
	".pdf":  true,
	".pptx": true,
	".docx": true,
	".xlsx": true,
}

// hostDeliveryCopyName matches the suffix uniqueLocalDeliveryPath adds when
// the workspace already has the producer's real file: name_HHMMSS_mmm.ext.
// Those copies were already handed to the chat. The file the tool wrote,
// without that suffix, is the one the channel missed.
var hostDeliveryCopyName = regexp.MustCompile(`_\d{6}_\d{3}$`)

type workspaceDocumentStamp struct {
	size int64
	mod  int64
	path string
}

func isWorkspaceDocumentName(name string) bool {
	return workspaceDocumentExts[strings.ToLower(filepath.Ext(name))]
}

func isHostDeliveryCopyName(name string) bool {
	_, _, ok := hostDeliveryCopyStem(name)
	return ok
}

func hostDeliveryCopyStem(name string) (stem, ext string, ok bool) {
	ext = filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	loc := hostDeliveryCopyName.FindStringIndex(base)
	if loc == nil {
		return "", "", false
	}
	return base[:loc[0]], ext, true
}

func snapshotWorkspaceDocuments(root string) map[string]workspaceDocumentStamp {
	snap, ok := snapshotWorkspaceDocumentsIfReadable(root)
	if !ok || snap == nil {
		return map[string]workspaceDocumentStamp{}
	}
	return snap
}

// LaTeX compiles next to the .tex (elsarticle/*.pdf), not the workspace root.
// A root-only list left that PDF off the turn, so the chat had no preview card.
//
// The walk is breadth-first and directory names are sorted, so a shallow
// paper is recorded before a deep tree can spend the directory budget, and
// both scans stop on the same cutoff. Symlinks are not followed, so a
// junction cannot pull documents in from outside the workspace.
const (
	workspaceDocumentMaxDepth = 4
	workspaceDocumentMaxDirs  = 1024
)

func workspaceDocumentSkipDir(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") {
		return true
	}
	switch strings.ToLower(name) {
	case "node_modules", "vendor", "__pycache__", "venv", "coverage":
		return true
	default:
		return false
	}
}

type workspaceDocumentScan struct {
	files     map[string]workspaceDocumentStamp
	truncated bool
}

// snapshotWorkspaceDocumentsIfReadable reports ok only when the directory
// could be listed. A failed read must not look like an empty baseline, or the
// next successful list treats every existing document as created this turn.
func snapshotWorkspaceDocumentsIfReadable(root string) (map[string]workspaceDocumentStamp, bool) {
	scan, ok := scanWorkspaceDocuments(root, workspaceDocumentMaxDirs)
	if !ok {
		return nil, false
	}
	return scan.files, true
}

func scanWorkspaceDocuments(root string, dirBudget int) (workspaceDocumentScan, bool) {
	root = strings.TrimSpace(root)
	if root == "" || dirBudget < 1 {
		return workspaceDocumentScan{}, false
	}
	root = filepath.Clean(root)
	scan := workspaceDocumentScan{files: map[string]workspaceDocumentStamp{}}
	type dirItem struct {
		path  string
		depth int
	}
	queue := []dirItem{{path: root, depth: 0}}
	seen := 0
	for head := 0; head < len(queue); head++ {
		if seen >= dirBudget {
			scan.truncated = true
			break
		}
		cur := queue[head]
		entries, err := os.ReadDir(cur.path)
		if err != nil {
			if cur.depth == 0 {
				return workspaceDocumentScan{}, false
			}
			// A child that cannot be listed must not look like an empty
			// directory. The other scan may still see the files inside it.
			scan.truncated = true
			continue
		}
		seen++
		var children []string
		for _, entry := range entries {
			name := entry.Name()
			// Type bits come from the listing. A symlink or junction is not
			// followed; a regular file has no type bit and falls through.
			if entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			if entry.IsDir() {
				if cur.depth >= workspaceDocumentMaxDepth || workspaceDocumentSkipDir(name) {
					continue
				}
				children = append(children, name)
				continue
			}
			if !isWorkspaceDocumentName(name) {
				continue
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
				continue
			}
			full := filepath.Join(cur.path, name)
			scan.files[deliveredWorkspaceDocKey(full)] = workspaceDocumentStamp{
				size: info.Size(),
				mod:  info.ModTime().UnixNano(),
				path: full,
			}
		}
		// Same name order on both scans, so a budget cutoff drops the same
		// tail instead of a different set of old files each time.
		sort.Strings(children)
		for _, name := range children {
			if len(queue) >= dirBudget {
				scan.truncated = true
				break
			}
			queue = append(queue, dirItem{path: filepath.Join(cur.path, name), depth: cur.depth + 1})
		}
	}
	return scan, true
}

// changedWorkspaceDocuments returns documents created or rewritten since the
// baseline, newest first. The host observes the workspace; the model does not
// name a path.
func changedWorkspaceDocuments(before, after map[string]workspaceDocumentStamp) []string {
	var paths []string
	for key, stamp := range after {
		prev, ok := before[key]
		if ok && prev.size == stamp.size && prev.mod == stamp.mod {
			continue
		}
		if stamp.path == "" {
			continue
		}
		paths = append(paths, stamp.path)
	}
	sort.Slice(paths, func(i, j int) bool {
		return after[deliveredWorkspaceDocKey(paths[i])].mod > after[deliveredWorkspaceDocKey(paths[j])].mod
	})
	return paths
}

// deliveredWorkspaceDocKey is the snapshot identity. Windows paths differ by
// drive-letter case; two turns must still see the same file as unchanged.
func deliveredWorkspaceDocKey(path string) string {
	path = normalizeProjectSessionPath(path)
	if path == "" || path == "." {
		return ""
	}
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

func (c *sharedAgentLoopCallbacks) boundWorkspaceRoot() string {
	if c == nil || c.handler == nil {
		return ""
	}
	if root := trustedPrincipalBoundWorkspace(c.handler, c.semanticPrincipalID()); root != "" {
		return root
	}
	return trustedPrincipalBoundWorkspace(c.handler, c.userID)
}

func (c *sharedAgentLoopCallbacks) noteWorkspaceDocumentBaseline() {
	if c == nil || c.workspaceDocBaseline != nil {
		return
	}
	root := resolveWorkspaceDocumentRoot(c.boundWorkspaceRoot())
	if root == "" {
		return
	}
	scan, ok := scanWorkspaceDocuments(root, workspaceDocumentMaxDirs)
	if !ok {
		return
	}
	if scan.files == nil {
		scan.files = map[string]workspaceDocumentStamp{}
	}
	c.workspaceDocRoot = root
	c.workspaceDocBaseline = scan.files
	c.workspaceDocBaselineTruncated = scan.truncated
	c.workspaceDocBaselineAt = time.Now()
}

// attachProducedWorkspaceDocuments puts documents created or rewritten since
// the turn baseline onto deliveredPaths. A file already in the workspace when
// the turn started stays on the earlier round. Delivery stays host-bound.
func (c *sharedAgentLoopCallbacks) attachProducedWorkspaceDocuments() {
	if c == nil {
		return
	}
	baselineRoot := c.workspaceDocRoot
	root := c.resolvedWorkspaceDocumentRoot()
	if root == "" {
		return
	}
	c.workspaceDocRoot = root
	if c.workspaceDocBaseline == nil {
		return
	}
	afterScan, ok := scanWorkspaceDocuments(root, workspaceDocumentMaxDirs)
	after := map[string]workspaceDocumentStamp{}
	if ok {
		after = afterScan.files
	}
	produced := changedWorkspaceDocuments(c.workspaceDocBaseline, after)
	// A different baseline directory means files already in the child belong
	// to an earlier round. A walk that hit its directory budget can disagree
	// with the other side about which old files it saw; mtime keeps those off
	// the card and still accepts a file written during this turn.
	if (baselineRoot != "" && deliveredWorkspaceDocKey(baselineRoot) != deliveredWorkspaceDocKey(root)) ||
		c.workspaceDocBaselineTruncated || afterScan.truncated {
		produced = documentsModifiedSince(produced, after, c.workspaceDocBaselineAt)
	}
	if len(produced) == 0 {
		return
	}
	produced = preferToolWrittenDocuments(produced)
	sortDeliveryDocuments(produced, after)
	c.deliveredPaths = mergeDeliveredPaths(produced, c.deliveredPaths)
}

func documentsModifiedSince(paths []string, after map[string]workspaceDocumentStamp, since time.Time) []string {
	if since.IsZero() {
		return paths
	}
	cutoff := since.UnixNano()
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if after[deliveredWorkspaceDocKey(path)].mod >= cutoff {
			out = append(out, path)
		}
	}
	return out
}

func (c *sharedAgentLoopCallbacks) resolvedWorkspaceDocumentRoot() string {
	if c == nil {
		return ""
	}
	if root := resolveWorkspaceDocumentRoot(c.workspaceDocRoot); root != "" {
		return root
	}
	return resolveWorkspaceDocumentRoot(c.boundWorkspaceRoot())
}

func sortDeliveryDocuments(paths []string, stamps map[string]workspaceDocumentStamp) {
	sort.SliceStable(paths, func(i, j int) bool {
		iCopy := isHostDeliveryCopyName(filepath.Base(paths[i]))
		jCopy := isHostDeliveryCopyName(filepath.Base(paths[j]))
		if iCopy != jCopy {
			return !iCopy
		}
		return stamps[deliveredWorkspaceDocKey(paths[i])].mod > stamps[deliveredWorkspaceDocKey(paths[j])].mod
	})
}

func mergeDeliveredPaths(front, existing []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(front)+len(existing))
	for _, path := range append(append([]string{}, front...), existing...) {
		path = filepath.Clean(strings.TrimSpace(path))
		key := deliveredWorkspaceDocKey(path)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, path)
	}
	return out
}

// resolveWorkspaceDocumentRoot returns the directory tools actually write.
// A desktop task's identity is …/data/tasks/<slug>; the files land in its
// workspace child. A path that is already that workspace stays put.
func resolveWorkspaceDocumentRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	root = filepath.Clean(root)
	if strings.EqualFold(filepath.Base(root), "workspace") {
		return root
	}
	child := filepath.Join(root, "workspace")
	info, err := os.Stat(child)
	if err != nil || !info.IsDir() || !looksLikeManagedTaskIdentity(root) {
		return root
	}
	return child
}

// preferToolWrittenDocuments drops a host delivery copy only when this same
// delivery already contains the unsuffixed file. A timed name for a different
// document stays.
func preferToolWrittenDocuments(paths []string) []string {
	stems := map[string]struct{}{}
	for _, path := range paths {
		name := filepath.Base(path)
		if _, _, ok := hostDeliveryCopyStem(name); ok {
			continue
		}
		ext := filepath.Ext(name)
		stems[documentStemKey(strings.TrimSuffix(name, ext), ext)] = struct{}{}
	}
	if len(stems) == 0 {
		return paths
	}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		name := filepath.Base(path)
		if stem, ext, ok := hostDeliveryCopyStem(name); ok {
			if _, dup := stems[documentStemKey(stem, ext)]; dup {
				continue
			}
		}
		out = append(out, path)
	}
	if len(out) == 0 {
		return paths
	}
	return out
}

func documentStemKey(stem, ext string) string {
	return strings.ToLower(stem + ext)
}
