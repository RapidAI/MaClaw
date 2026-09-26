package guiapp

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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

const deliveredWorkspaceDocsFile = "delivered-workspace-docs.txt"

type workspaceDocumentStamp struct {
	size int64
	mod  int64
}

func isWorkspaceDocumentName(name string) bool {
	return workspaceDocumentExts[strings.ToLower(filepath.Ext(name))]
}

func isHostDeliveryCopyName(name string) bool {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return hostDeliveryCopyName.MatchString(base)
}

func snapshotWorkspaceDocuments(root string) map[string]workspaceDocumentStamp {
	out := map[string]workspaceDocumentStamp{}
	root = strings.TrimSpace(root)
	if root == "" {
		return out
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		if entry.IsDir() || !isWorkspaceDocumentName(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > semanticOfficeArtifactMaxBytes {
			continue
		}
		out[filepath.Join(root, entry.Name())] = workspaceDocumentStamp{
			size: info.Size(),
			mod:  info.ModTime().UnixNano(),
		}
	}
	return out
}

// changedWorkspaceDocuments returns documents created or rewritten since the
// baseline, newest first. The host observes the workspace; the model does not
// name a path.
func changedWorkspaceDocuments(before, after map[string]workspaceDocumentStamp) []string {
	var paths []string
	for path, stamp := range after {
		prev, ok := before[path]
		if ok && prev == stamp {
			continue
		}
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool {
		return after[paths[i]].mod > after[paths[j]].mod
	})
	return paths
}

func deliveredWorkspaceDocsPath(root string) string {
	return filepath.Join(root, ".maclaw-tmp", deliveredWorkspaceDocsFile)
}

func loadDeliveredWorkspaceDocs(root string) map[string]bool {
	out := map[string]bool{}
	body, err := os.ReadFile(deliveredWorkspaceDocsPath(root))
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = filepath.Clean(strings.TrimSpace(line))
		if line != "" && line != "." {
			out[line] = true
		}
	}
	return out
}

func rememberDeliveredWorkspaceDocs(root string, paths []string) error {
	if strings.TrimSpace(root) == "" || len(paths) == 0 {
		return nil
	}
	known := loadDeliveredWorkspaceDocs(root)
	for _, path := range paths {
		if strings.TrimSpace(path) != "" {
			known[filepath.Clean(path)] = true
		}
	}
	list := make([]string, 0, len(known))
	for path := range known {
		list = append(list, path)
	}
	sort.Strings(list)
	dir := filepath.Join(root, ".maclaw-tmp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(deliveredWorkspaceDocsPath(root), []byte(strings.Join(list, "\n")+"\n"), 0o644)
}

// undeliveredWorkspaceDocuments lists root documents this workspace has never
// handed to the current channel. Host delivery copies are skipped: they are
// the channel's own materialization of a renderer payload, not a second product.
func undeliveredWorkspaceDocuments(root string) []string {
	after := snapshotWorkspaceDocuments(root)
	known := loadDeliveredWorkspaceDocs(root)
	var paths []string
	for path := range after {
		if !strings.EqualFold(filepath.Ext(path), ".pdf") {
			continue
		}
		clean := filepath.Clean(path)
		if known[clean] || hostAlreadyMaterialized(path, after) {
			continue
		}
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool {
		return after[paths[i]].mod > after[paths[j]].mod
	})
	return paths
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
	c.workspaceDocRoot = root
	c.workspaceDocBaseline = snapshotWorkspaceDocuments(root)
}

// attachProducedWorkspaceDocuments puts workspace documents the turn produced,
// and documents never handed to this channel, onto deliveredPaths. Delivery
// stays host-bound: the model still cannot pass a path to send_file. A shell
// export becomes visible the same way an office write already does.
func (c *sharedAgentLoopCallbacks) attachProducedWorkspaceDocuments() {
	if c == nil {
		return
	}
	root := resolveWorkspaceDocumentRoot(c.workspaceDocRoot)
	if root == "" {
		root = resolveWorkspaceDocumentRoot(c.boundWorkspaceRoot())
	}
	if root == "" {
		return
	}
	c.workspaceDocRoot = root
	after := snapshotWorkspaceDocuments(root)
	var produced []string
	if c.workspaceDocBaseline != nil {
		produced = changedWorkspaceDocuments(c.workspaceDocBaseline, after)
	}
	delivering := len(produced) > 0 || len(c.deliveredPaths) > 0 || strings.TrimSpace(c.semanticDeliveryFileData) != ""
	if delivering && workspaceIsDesktopTask(root) {
		seen := map[string]bool{}
		for _, path := range produced {
			seen[filepath.Clean(path)] = true
		}
		for _, path := range undeliveredWorkspaceDocuments(root) {
			clean := filepath.Clean(path)
			if !seen[clean] {
				produced = append(produced, path)
				seen[clean] = true
			}
		}
	}
	if len(produced) == 0 {
		return
	}
	produced = preferToolWrittenDocuments(produced)
	sortDeliveryDocuments(produced, after)
	c.deliveredPaths = mergeDeliveredPaths(produced, c.deliveredPaths)
	if err := rememberDeliveredWorkspaceDocs(root, produced); err != nil {
		return
	}
}

func hostCopyStem(name string) (string, bool) {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	loc := hostDeliveryCopyName.FindStringIndex(base)
	if loc == nil {
		return "", false
	}
	return base[:loc[0]], true
}

func hostAlreadyMaterialized(path string, present map[string]workspaceDocumentStamp) bool {
	name := filepath.Base(path)
	if _, copy := hostCopyStem(name); copy {
		return true
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for other := range present {
		otherName := filepath.Base(other)
		if !strings.EqualFold(filepath.Ext(otherName), ext) {
			continue
		}
		if otherStem, ok := hostCopyStem(otherName); ok && otherStem == stem {
			return true
		}
	}
	return false
}

func sortDeliveryDocuments(paths []string, stamps map[string]workspaceDocumentStamp) {
	sort.SliceStable(paths, func(i, j int) bool {
		iCopy := isHostDeliveryCopyName(filepath.Base(paths[i]))
		jCopy := isHostDeliveryCopyName(filepath.Base(paths[j]))
		if iCopy != jCopy {
			return !iCopy
		}
		return stamps[paths[i]].mod > stamps[paths[j]].mod
	})
}

func mergeDeliveredPaths(front, existing []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(front)+len(existing))
	for _, path := range append(append([]string{}, front...), existing...) {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "" || path == "." || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}

func workspaceIsDesktopTask(root string) bool {
	return looksLikeManagedTaskIdentity(root)
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

func preferToolWrittenDocuments(paths []string) []string {
	real := make([]string, 0, len(paths))
	for _, path := range paths {
		if !isHostDeliveryCopyName(filepath.Base(path)) {
			real = append(real, path)
		}
	}
	if len(real) == 0 {
		return paths
	}
	return real
}
