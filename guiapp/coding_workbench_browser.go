package guiapp

import (
	"archive/tar"
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// CodingWorkbenchDirectoryEntry is one lazily-loaded entry in a coding task's
// working directory. Paths are always relative to the task root so the UI can
// never navigate outside the selected workspace.
type CodingWorkbenchDirectoryEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
}

type CodingWorkbenchDirectoryResponse struct {
	Root      string                          `json:"root"`
	Path      string                          `json:"path"`
	Entries   []CodingWorkbenchDirectoryEntry `json:"entries"`
	Truncated bool                            `json:"truncated"`
}

type CodingWorkbenchFilePreview struct {
	Path      string `json:"path"`
	AbsPath   string `json:"abs_path"`
	Content   string `json:"content"`
	Language  string `json:"language"`
	Truncated bool   `json:"truncated"`
}

// CodingWorkbenchEntryProperties contains inexpensive metadata for a selected
// explorer entry. Directory size is deliberately omitted: recursively walking
// a large local tree or remote repository would make a simple Properties click
// unexpectedly expensive.
type CodingWorkbenchEntryProperties struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	AbsPath    string `json:"abs_path"`
	IsDir      bool   `json:"is_dir"`
	Size       int64  `json:"size"`
	SizeKnown  bool   `json:"size_known"`
	ModifiedAt int64  `json:"modified_at"`
	Mode       string `json:"mode"`
	Extension  string `json:"extension"`
}

type codingWorkbenchRemoteDirectoryRecord struct {
	Name      string `json:"name"`
	IsDir     bool   `json:"is_dir"`
	Truncated *bool  `json:"truncated"`
}

const codingWorkbenchBrowserMaxEntries = 500
const codingWorkbenchBrowserMaxScanEntries = codingWorkbenchBrowserMaxEntries * 4
const codingWorkbenchBrowserMaxRunes = 400000
const codingWorkbenchBrowserMaxReadBytes = codingWorkbenchBrowserMaxRunes * utf8.UTFMax

// A file opened from a remote explorer is copied locally before VS Code starts.
// Keep that convenience bounded so a forged frontend request cannot turn a
// context-menu action into an unexpectedly huge desktop download.
const codingWorkbenchVSCodeRemoteMaxFileBytes int64 = 64 * 1024 * 1024
const codingWorkbenchVSCodeRemoteSnapshotRetention = 7 * 24 * time.Hour

func cleanCodingWorkbenchBrowserPath(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" || value == "." {
		return "", nil
	}
	if strings.HasPrefix(value, "/") || strings.Contains(value, ":") {
		return "", fmt.Errorf("path must be relative to the working directory")
	}
	value = path.Clean(value)
	if value == "." {
		return "", nil
	}
	if value == ".." || strings.HasPrefix(value, "../") {
		return "", fmt.Errorf("path outside the working directory")
	}
	return value, nil
}

func codingWorkbenchBrowserLocalRoot(a *App, projectPath string) (string, error) {
	root := ""
	if a != nil {
		if cloudDir := strings.TrimSpace(a.cloudWorkspaceExecutionDir(projectPath)); cloudDir != "" {
			root = cloudDir
		} else if a.projectPathIsLocalCodingWorkbench(projectPath) {
			root = strings.TrimSpace(a.codingWorkbenchLocalExecDirOrDesktop(projectPath))
		} else {
			root = strings.TrimSpace(a.recentTaskExecutionProjectPath(projectPath))
		}
	}
	if root == "" {
		return "", fmt.Errorf("working directory is unavailable")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("working directory is not a directory")
	}
	resolved, err := filepath.EvalSymlinks(absRoot)
	if err == nil {
		absRoot = resolved
	}
	return absRoot, nil
}

func codingWorkbenchBrowserLocalPath(root, relative string) (string, error) {
	relative, err := cleanCodingWorkbenchBrowserPath(relative)
	if err != nil {
		return "", err
	}
	target := filepath.Join(root, filepath.FromSlash(relative))
	if !isPathInsideRoot(root, target) {
		return "", fmt.Errorf("path outside the working directory")
	}
	// Existing symlinks must resolve inside the root as well. This closes the
	// otherwise subtle escape where a harmless-looking relative path points out
	// of the project tree.
	if resolved, resolveErr := filepath.EvalSymlinks(target); resolveErr == nil && !isPathInsideRoot(root, resolved) {
		return "", fmt.Errorf("path resolves outside the working directory")
	}
	return target, nil
}

func sortCodingWorkbenchDirectoryEntries(entries []CodingWorkbenchDirectoryEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return codingWorkbenchDirectoryEntryLess(entries[i], entries[j])
	})
}

func parseCodingWorkbenchRemoteDirectoryRecords(raw, relativePath string) ([]CodingWorkbenchDirectoryEntry, bool, bool) {
	entries := make([]CodingWorkbenchDirectoryEntry, 0)
	truncated := false
	sawMarker := false
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		var item codingWorkbenchRemoteDirectoryRecord
		if json.Unmarshal([]byte(line), &item) != nil {
			continue
		}
		if item.Truncated != nil {
			truncated = *item.Truncated
			sawMarker = true
			continue
		}
		if item.Name == "" || isCodingWorkbenchHiddenBrowserName(item.Name) {
			continue
		}
		if len(entries) >= codingWorkbenchBrowserMaxEntries {
			truncated = true
			continue
		}
		entries = append(entries, CodingWorkbenchDirectoryEntry{
			Name: item.Name, Path: path.Join(relativePath, item.Name), IsDir: item.IsDir,
		})
	}
	sortCodingWorkbenchDirectoryEntries(entries)
	return entries, truncated, sawMarker
}

func codingWorkbenchDirectoryEntryLess(left, right CodingWorkbenchDirectoryEntry) bool {
	if left.IsDir != right.IsDir {
		return left.IsDir
	}
	leftName, rightName := strings.ToLower(left.Name), strings.ToLower(right.Name)
	if leftName != rightName {
		return leftName < rightName
	}
	return left.Name < right.Name
}

func isCodingWorkbenchHiddenBrowserName(name string) bool {
	return strings.HasPrefix(strings.TrimSpace(name), ".")
}

func cloudWorkspaceBrowserRoot(root string) bool {
	return cloudWorkspaceIDFromPathString(root) != "" || cloudWorkspaceIDFromReadOnlyCachePath(root) != ""
}

// codingWorkbenchDirectoryFromListing projects one directory level from the
// browse index and merges names that exist only on disk. A missing index, or
// a path the index does not contain, leaves listing to the local directory.
func codingWorkbenchDirectoryFromListing(root, relativePath string) (CodingWorkbenchDirectoryResponse, bool, error) {
	if !cloudWorkspaceBrowserRoot(root) {
		return CodingWorkbenchDirectoryResponse{}, false, nil
	}
	listing, err := readCloudWorkspaceListing(root)
	if err != nil {
		// A damaged index must not hide files already on disk. The next sync
		// rewrites the index.
		log.Printf("[cloud_workspace] browse index unreadable, listing disk: %v", err)
		return CodingWorkbenchDirectoryResponse{}, false, nil
	}
	if listing == nil {
		return CodingWorkbenchDirectoryResponse{}, false, nil
	}
	prefix, err := cleanCodingWorkbenchBrowserPath(relativePath)
	if err != nil {
		return CodingWorkbenchDirectoryResponse{}, false, err
	}
	if !cloudWorkspaceListingContainsDir(listing.Entries, prefix) {
		return CodingWorkbenchDirectoryResponse{}, false, nil
	}
	entries := cloudWorkspaceManifestChildren(listing.Entries, prefix)
	// The replica treats NFC-lowercase as one path. On a case-insensitive
	// volume the disk name and the manifest name are the same file.
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		seen[cloudWorkspacePortablePathKey(entry.Name)] = struct{}{}
	}
	if dir, dirErr := codingWorkbenchBrowserLocalPath(root, prefix); dirErr == nil {
		if handle, openErr := os.Open(dir); openErr == nil {
			disk, _, collectErr := collectCodingWorkbenchDirectoryEntries(handle, prefix)
			handle.Close()
			if collectErr != nil {
				// Keep the names already projected from the browse index.
				log.Printf("[cloud_workspace] disk listing failed, using browse index: %v", collectErr)
			} else {
				for _, entry := range disk {
					if _, ok := seen[cloudWorkspacePortablePathKey(entry.Name)]; ok {
						continue
					}
					entries = append(entries, entry)
				}
			}
		}
	}
	sortCodingWorkbenchDirectoryEntries(entries)
	truncated := false
	if len(entries) > codingWorkbenchBrowserMaxEntries {
		entries = entries[:codingWorkbenchBrowserMaxEntries]
		truncated = true
	}
	return CodingWorkbenchDirectoryResponse{
		Root:      omitCloudWorkspaceAbsPath(root),
		Path:      prefix,
		Entries:   entries,
		Truncated: truncated,
	}, true, nil
}

func codingWorkbenchListingProperties(root, relativePath string) (CodingWorkbenchEntryProperties, bool) {
	if !cloudWorkspaceBrowserRoot(root) {
		return CodingWorkbenchEntryProperties{}, false
	}
	listing, err := readCloudWorkspaceListing(root)
	if err != nil || listing == nil {
		return CodingWorkbenchEntryProperties{}, false
	}
	cleaned, ok := cloudWorkspaceSafeRelPath(relativePath)
	if !ok {
		return CodingWorkbenchEntryProperties{}, false
	}
	for _, entry := range listing.Entries {
		if entry.Path != cleaned {
			continue
		}
		name := path.Base(cleaned)
		return CodingWorkbenchEntryProperties{
			Name:      name,
			Path:      cleaned,
			IsDir:     false,
			Size:      entry.Size,
			SizeKnown: true,
			Extension: strings.TrimPrefix(path.Ext(name), "."),
		}, true
	}
	if cleaned != "" && cloudWorkspaceListingContainsDir(listing.Entries, cleaned) {
		return CodingWorkbenchEntryProperties{
			Name:  path.Base(cleaned),
			Path:  cleaned,
			IsDir: true,
		}, true
	}
	return CodingWorkbenchEntryProperties{}, false
}

// collectCodingWorkbenchDirectoryEntries reads one small page of visible
// entries. Dot-prefixed names are skipped so they do not consume the page or
// appear in the explorer. A scan budget keeps a directory of only hidden
// entries from walking the whole tree on a network share.
func collectCodingWorkbenchDirectoryEntries(dir *os.File, relativePath string) ([]CodingWorkbenchDirectoryEntry, bool, error) {
	entries := make([]CodingWorkbenchDirectoryEntry, 0, codingWorkbenchBrowserMaxEntries)
	truncated := false
	scanned := 0
	more := false
	for scanned < codingWorkbenchBrowserMaxScanEntries {
		want := codingWorkbenchBrowserMaxEntries - len(entries) + 1
		if remain := codingWorkbenchBrowserMaxScanEntries - scanned; want > remain {
			want = remain
		}
		if want < 1 {
			break
		}
		items, err := dir.ReadDir(want)
		if err != nil && err != io.EOF {
			return nil, false, err
		}
		scanned += len(items)
		more = err != io.EOF && len(items) == want
		for _, item := range items {
			if isCodingWorkbenchHiddenBrowserName(item.Name()) {
				continue
			}
			if len(entries) >= codingWorkbenchBrowserMaxEntries {
				truncated = true
				break
			}
			entries = append(entries, CodingWorkbenchDirectoryEntry{
				Name: item.Name(), Path: path.Join(relativePath, item.Name()), IsDir: item.IsDir(),
			})
		}
		if truncated || !more {
			break
		}
	}
	if !truncated && more && len(entries) >= codingWorkbenchBrowserMaxEntries {
		truncated = true
	}
	sortCodingWorkbenchDirectoryEntries(entries)
	return entries, truncated, nil
}

// readCodingWorkbenchBrowserTextFile reads only the bounded preview window.
// Reading the whole file before truncating would make a click on a multi-GB
// text or log file consume unbounded memory in the desktop process.
func readCodingWorkbenchBrowserTextFile(absPath string) (string, bool, error) {
	file, err := os.Open(absPath)
	if err != nil {
		return "", false, err
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, int64(codingWorkbenchBrowserMaxReadBytes)+1))
	if err != nil {
		return "", false, err
	}
	readWasLimited := len(content) > codingWorkbenchBrowserMaxReadBytes
	// A bounded byte read can end halfway through a valid UTF-8 sequence. Trim
	// at most that incomplete tail (three bytes); invalid data earlier in the
	// file remains a binary/invalid-text rejection below.
	if !utf8.Valid(content) && readWasLimited {
		for trim := 1; trim < utf8.UTFMax && trim < len(content); trim++ {
			if utf8.Valid(content[:len(content)-trim]) {
				content = content[:len(content)-trim]
				break
			}
		}
	}
	if !isCodePreviewTextContent(content) {
		return "", false, fmt.Errorf("binary files cannot be previewed")
	}
	runes := []rune(string(content))
	truncated := len(runes) > codingWorkbenchBrowserMaxRunes || readWasLimited
	if len(runes) > codingWorkbenchBrowserMaxRunes {
		runes = runes[:codingWorkbenchBrowserMaxRunes]
	}
	return string(runes), truncated, nil
}

// GetCodingWorkbenchDirectory lists one directory level for the source-preview
// explorer. Remote tasks use their live SSH session; local tasks use the actual
// execution directory rather than the task metadata directory.
func (a *App) GetCodingWorkbenchDirectory(projectPath, relativePath string) (CodingWorkbenchDirectoryResponse, error) {
	projectPath = normalizeProjectSessionPath(projectPath)
	if a == nil || projectPath == "" {
		return CodingWorkbenchDirectoryResponse{}, fmt.Errorf("project path is required")
	}
	relativePath, err := cleanCodingWorkbenchBrowserPath(relativePath)
	if err != nil {
		return CodingWorkbenchDirectoryResponse{}, err
	}
	status := a.GetCodingWorkbenchStatus(projectPath)
	if status.Kind == "remote" {
		return a.getRemoteCodingWorkbenchDirectory(projectPath, relativePath)
	}
	root, err := codingWorkbenchBrowserLocalRoot(a, projectPath)
	if err != nil {
		return CodingWorkbenchDirectoryResponse{}, err
	}
	if listed, ok, listErr := codingWorkbenchDirectoryFromListing(root, relativePath); ok || listErr != nil {
		if listErr != nil {
			return CodingWorkbenchDirectoryResponse{}, listErr
		}
		return listed, nil
	}
	dir, err := codingWorkbenchBrowserLocalPath(root, relativePath)
	if err != nil {
		return CodingWorkbenchDirectoryResponse{}, err
	}
	dirHandle, err := os.Open(dir)
	if err != nil {
		return CodingWorkbenchDirectoryResponse{}, err
	}
	defer dirHandle.Close()
	entries, truncated, err := collectCodingWorkbenchDirectoryEntries(dirHandle, relativePath)
	if err != nil {
		return CodingWorkbenchDirectoryResponse{}, err
	}
	return CodingWorkbenchDirectoryResponse{Root: omitCloudWorkspaceAbsPath(root), Path: relativePath, Entries: entries, Truncated: truncated}, nil
}

func (a *App) getRemoteCodingWorkbenchDirectory(projectPath, relativePath string) (CodingWorkbenchDirectoryResponse, error) {
	sessionID, root, err := a.acpRemoteSSHSession(projectPath)
	if err != nil {
		return CodingWorkbenchDirectoryResponse{}, err
	}
	absPath := codingWorkbenchBrowserRemotePath(root, relativePath)
	if !remotePathWithinDir(absPath, root) {
		return CodingWorkbenchDirectoryResponse{}, fmt.Errorf("path outside remote work_dir")
	}
	// Resolve the requested directory remotely before listing it. The lexical
	// client-side check above is not sufficient for a symlink inside work_dir
	// pointing outside it.
	script := fmt.Sprintf(`import json, os, stat, sys
root = os.path.realpath(sys.argv[1])
target = os.path.realpath(sys.argv[2])
if not (root == os.sep or target == root or target.startswith(root + os.sep)):
    raise SystemExit("path outside remote work_dir")
try:
    info = os.stat(target)
except OSError as exc:
    raise SystemExit(str(exc))
if not stat.S_ISDIR(info.st_mode):
    raise SystemExit("path is not a directory")
limit = %d
rows = []
scan_budget = limit * 4
scanned = 0
try:
    with os.scandir(target) as scan:
        for entry in scan:
            scanned += 1
            if scanned > scan_budget:
                break
            if entry.name.strip().startswith("."):
                continue
            is_dir = False
            try:
                is_dir = bool(entry.is_dir(follow_symlinks=False))
            except OSError:
                is_dir = False
            rows.append((entry.name, is_dir))
            if len(rows) > limit:
                break
except OSError as exc:
    raise SystemExit(str(exc))
truncated = len(rows) > limit
print(json.dumps({"truncated": truncated}))
for name, is_dir in rows[:limit]:
    try:
        print(json.dumps({"name": name, "is_dir": is_dir}, ensure_ascii=True))
    except (TypeError, ValueError, UnicodeError):
        continue
`, codingWorkbenchBrowserMaxEntries)
	// Dedicated exec channel: the login PTY is shared with the coding agent.
	// Listing through it captured the agent's ssh_read_file dump and the next
	// shell prompt, which the preview then showed as the directory error.
	raw, err := a.codingWorkbenchRemoteExec(sessionID, fmt.Sprintf("%s %s %s", remotePythonCommand(script), remoteShellQuote(root), remoteShellQuote(absPath)), 15*time.Second)
	if err != nil {
		return CodingWorkbenchDirectoryResponse{}, err
	}
	// A real listing always prints {"truncated": ...}, including an empty
	// directory. A substring match is not enough: file text can contain the
	// word. Without the JSON record, showing an empty folder repeats the bug.
	entries, truncated, sawMarker := parseCodingWorkbenchRemoteDirectoryRecords(raw, relativePath)
	if !sawMarker {
		detail := compactRemoteSSHError(raw)
		log.Printf("[coding-workbench] remote directory listing had no marker session=%s detail=%q", sessionID, truncateRunesV2(detail, 240))
		return CodingWorkbenchDirectoryResponse{}, fmt.Errorf("%s", detail)
	}
	return CodingWorkbenchDirectoryResponse{Root: root, Path: relativePath, Entries: entries, Truncated: truncated}, nil
}

// codingWorkbenchRemoteExec runs one preview command on its own SSH channel.
// Stdout is the command's own output. A non-zero exit becomes a short error;
// PTY prompts and numbered file dumps are not forwarded to the preview.
func (a *App) codingWorkbenchRemoteExec(sessionID, command string, timeout time.Duration) (string, error) {
	if a == nil {
		return "", fmt.Errorf("AI assistant not initialized")
	}
	hub := a.ensureHubClient()
	if hub == nil || hub.ensureIMHandler() == nil {
		return "", fmt.Errorf("AI assistant not initialized")
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	result, err := hub.ensureIMHandler().runRemoteSetupCommand(context.Background(), sessionID, command, timeout)
	if err != nil {
		log.Printf("[coding-workbench] remote exec failed session=%s err=%v", sessionID, err)
		return "", fmt.Errorf("%s", compactRemoteSSHError(err.Error()))
	}
	if result.ExitCode != 0 {
		detail := compactRemoteSSHError(codingWorkbenchRemoteExecDetail(result.Stdout, result.Stderr))
		log.Printf("[coding-workbench] remote exec exit=%d session=%s detail=%q", result.ExitCode, sessionID, truncateRunesV2(detail, 240))
		return "", fmt.Errorf("%s", detail)
	}
	return result.Stdout, nil
}

func codingWorkbenchRemoteExecDetail(stdout, stderr string) string {
	stderr = strings.TrimSpace(stderr)
	stdout = strings.TrimSpace(stdout)
	if stderr == "" {
		return stdout
	}
	if stdout == "" {
		return stderr
	}
	return stderr + "\n" + stdout
}

// codingWorkbenchBrowserRemotePath resolves a browser path within root. An
// empty relative path denotes the browser's root itself; acpResolveRemotePath
// intentionally treats it as invalid for file-oriented ACP requests.
func codingWorkbenchBrowserRemotePath(root, relativePath string) string {
	if strings.TrimSpace(relativePath) == "" {
		return remoteCleanPath(root)
	}
	return acpResolveRemotePath(relativePath, root)
}

// GetCodingWorkbenchFilePreview reads a bounded text preview for a file chosen
// from the directory explorer. As with directory listing, all paths are scoped
// to the coding task's local or remote working directory.
func (a *App) GetCodingWorkbenchFilePreview(projectPath, relativePath string) (CodingWorkbenchFilePreview, error) {
	projectPath = normalizeProjectSessionPath(projectPath)
	if a == nil || projectPath == "" {
		return CodingWorkbenchFilePreview{}, fmt.Errorf("project path is required")
	}
	relativePath, err := cleanCodingWorkbenchBrowserPath(relativePath)
	if err != nil || relativePath == "" {
		if err == nil {
			err = fmt.Errorf("file path is required")
		}
		return CodingWorkbenchFilePreview{}, err
	}
	status := a.GetCodingWorkbenchStatus(projectPath)
	if status.Kind == "remote" {
		return a.getRemoteCodingWorkbenchFilePreview(projectPath, relativePath)
	}
	root, err := codingWorkbenchBrowserLocalRoot(a, projectPath)
	if err != nil {
		return CodingWorkbenchFilePreview{}, err
	}
	if err := a.materializeCloudWorkspaceListedFile(root, relativePath); err != nil {
		return CodingWorkbenchFilePreview{}, err
	}
	absPath, err := codingWorkbenchBrowserLocalPath(root, relativePath)
	if err != nil {
		return CodingWorkbenchFilePreview{}, err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return CodingWorkbenchFilePreview{}, err
	}
	if info.IsDir() {
		return CodingWorkbenchFilePreview{}, fmt.Errorf("path is a directory")
	}
	content, truncated, err := readCodingWorkbenchBrowserTextFile(absPath)
	if err != nil {
		return CodingWorkbenchFilePreview{}, err
	}
	return CodingWorkbenchFilePreview{Path: relativePath, AbsPath: omitCloudWorkspaceAbsPath(absPath), Content: content, Language: detectLanguageFromExt(absPath), Truncated: truncated}, nil
}

// GetCodingWorkbenchEntryProperties returns safe, bounded metadata for a
// directory-tree entry without reading its contents.
func (a *App) GetCodingWorkbenchEntryProperties(projectPath, relativePath string) (CodingWorkbenchEntryProperties, error) {
	projectPath = normalizeProjectSessionPath(projectPath)
	if a == nil || projectPath == "" {
		return CodingWorkbenchEntryProperties{}, fmt.Errorf("project path is required")
	}
	relativePath, err := cleanCodingWorkbenchBrowserPath(relativePath)
	if err != nil {
		return CodingWorkbenchEntryProperties{}, err
	}
	if a.GetCodingWorkbenchStatus(projectPath).Kind == "remote" {
		return a.getRemoteCodingWorkbenchEntryProperties(projectPath, relativePath)
	}
	root, err := codingWorkbenchBrowserLocalRoot(a, projectPath)
	if err != nil {
		return CodingWorkbenchEntryProperties{}, err
	}
	absPath, err := codingWorkbenchBrowserLocalPath(root, relativePath)
	if err != nil {
		return CodingWorkbenchEntryProperties{}, err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			if props, ok := codingWorkbenchListingProperties(root, relativePath); ok {
				return props, nil
			}
		}
		return CodingWorkbenchEntryProperties{}, err
	}
	return codingWorkbenchEntryProperties(relativePath, absPath, info.Name(), info.IsDir(), info.Size(), info.ModTime().Unix(), fmt.Sprintf("%04o", info.Mode().Perm())), nil
}

// IsCodingWorkbenchVSCodeAvailable reports whether this desktop can launch VS
// Code. The explorer uses it to avoid presenting an action that cannot work.
func (a *App) IsCodingWorkbenchVSCodeAvailable() bool {
	_, err := findVSCodeCLI()
	return err == nil
}

// OpenCodingWorkbenchFileInVSCode opens a file chosen from the source explorer
// in the locally installed VS Code. For remote tasks the file is first copied
// through the existing SFTP session into a task-scoped local temporary cache;
// VS Code Remote-SSH is deliberately not used because it requires a working
// VS Code Server on the remote host. The return value tells the UI whether the
// opened file is that local, non-synchronizing copy.
func (a *App) OpenCodingWorkbenchFileInVSCode(projectPath, relativePath string) (bool, error) {
	projectPath = normalizeProjectSessionPath(projectPath)
	if a == nil || projectPath == "" {
		return false, fmt.Errorf("project path is required")
	}
	relativePath, err := cleanCodingWorkbenchBrowserPath(relativePath)
	if err != nil || relativePath == "" {
		if err == nil {
			err = fmt.Errorf("file path is required")
		}
		return false, err
	}
	codeCLI, err := findVSCodeCLI()
	if err != nil {
		return false, fmt.Errorf("VS Code is not available: %w", err)
	}
	if a.GetCodingWorkbenchStatus(projectPath).Kind == "remote" {
		// Resolve and stat through the live SSH session before downloading. This
		// applies the explorer's same symlink/root containment check and rejects a
		// directory supplied by a forged frontend call.
		properties, propertiesErr := a.getRemoteCodingWorkbenchEntryProperties(projectPath, relativePath)
		if propertiesErr != nil {
			return false, propertiesErr
		}
		if properties.IsDir {
			return false, fmt.Errorf("path is a directory")
		}
		localPath, discardSnapshot, downloadErr := a.downloadRemoteCodingWorkbenchFileForVSCode(projectPath, relativePath, properties)
		if downloadErr != nil {
			return false, downloadErr
		}
		if err := launchVSCodeWithArgs(codeCLI, []string{"-n", localPath}); err != nil {
			discardSnapshot()
			return false, err
		}
		return true, nil
	}
	root, err := codingWorkbenchBrowserLocalRoot(a, projectPath)
	if err != nil {
		return false, err
	}
	if err := a.materializeCloudWorkspaceListedFile(root, relativePath); err != nil {
		return false, err
	}
	absPath, err := codingWorkbenchBrowserLocalPath(root, relativePath)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return false, err
	}
	if info.IsDir() {
		return false, fmt.Errorf("path is a directory")
	}
	return false, launchVSCodeWithArgs(codeCLI, []string{"-n", absPath})
}

// OpenCodingWorkbenchFileLocally opens a workbench file with the OS default
// application using the local cache. It never returns that absolute path to
// the frontend. Remote SSH workspaces are rejected.
func (a *App) OpenCodingWorkbenchFileLocally(projectPath, relativePath string) error {
	projectPath = normalizeProjectSessionPath(projectPath)
	if a == nil || projectPath == "" {
		return fmt.Errorf("project path is required")
	}
	if a.codingWorkbenchRejectsLocalOpen(projectPath) {
		return fmt.Errorf("open locally is not available for remote workspaces")
	}
	absPath, err := a.codingWorkbenchLocalFileToOpen(projectPath, relativePath)
	if err != nil {
		return err
	}
	return a.OpenFileOrShowInFolder(absPath)
}

// DeleteCodingWorkbenchEntry removes a cloud-workspace file or folder from the
// local cache and the matching remote objects. Unrelated local edits stay unsynced.
func (a *App) DeleteCodingWorkbenchEntry(projectPath, relativePath string) error {
	projectPath = normalizeProjectSessionPath(projectPath)
	if a == nil || projectPath == "" {
		return fmt.Errorf("project path is required")
	}
	workspaceID := strings.TrimSpace(a.lookupCloudWorkspaceIDForProject(projectPath))
	root, rootErr := codingWorkbenchBrowserLocalRoot(a, projectPath)
	if workspaceID == "" && rootErr == nil {
		workspaceID = lookupCloudWorkspaceIDByLocalPath(root)
		if workspaceID == "" {
			workspaceID = cloudWorkspaceIDFromPathString(root)
		}
		if workspaceID == "" {
			workspaceID = cloudWorkspaceIDFromReadOnlyCachePath(root)
		}
	}
	if workspaceID == "" {
		return fmt.Errorf("delete is only available for cloud workspaces")
	}
	if rootErr != nil {
		return rootErr
	}
	cacheRoot, err := a.cloudWorkspaceDeleteCacheRoot(workspaceID, root)
	if err != nil {
		return err
	}
	relativePath, err = cleanCodingWorkbenchBrowserPath(relativePath)
	if err != nil {
		return err
	}
	if relativePath == "" {
		return fmt.Errorf("file path is required")
	}
	absPath, err := codingWorkbenchBrowserLocalPath(root, relativePath)
	if err != nil {
		return err
	}
	cacheRel, err := cacheRelativeCloudWorkspacePath(cacheRoot, absPath)
	if err != nil {
		return err
	}
	if codingWorkbenchEntryProtected(relativePath) || codingWorkbenchEntryProtected(cacheRel) {
		return fmt.Errorf("entry cannot be deleted")
	}
	info, err := os.Lstat(absPath)
	missing := err != nil && os.IsNotExist(err)
	if err != nil && !missing {
		return err
	}
	isDir := false
	if !missing {
		if !info.Mode().IsRegular() && !info.IsDir() {
			return fmt.Errorf("entry cannot be deleted")
		}
		isDir = info.IsDir()
	} else {
		isDir, err = inferCloudWorkspaceDeleteDir(cacheRoot, cacheRel)
		if err != nil {
			return err
		}
	}
	paths, err := collectCloudWorkspaceDeletePaths(cacheRoot, cacheRel, isDir)
	if err != nil {
		return err
	}
	removeLocal := func() error {
		if missing {
			return nil
		}
		if isDir {
			return os.RemoveAll(absPath)
		}
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	ctx, cancel := a.cloudWorkspaceSyncContext()
	defer cancel()
	proto := a.cloudWorkspaceProtocol(workspaceID)
	// Hold the replica lock around the manifest replace and the local remove.
	// A watcher push that already scanned this path would otherwise write it back.
	if mount := lookupHeldCloudWorkspace(workspaceID); mount != nil {
		mount.syncMu.Lock()
		defer mount.syncMu.Unlock()
		mount.mu.Lock()
		releasing := mount.releasing
		mount.mu.Unlock()
		if releasing {
			return fmt.Errorf("cloud workspace is releasing")
		}
	}
	// The file manager browses without a writer lease. PutManifest still
	// requires one. The borrow runs only after DeletePaths has seen that the
	// remote tree changes, and an open writable mount keeps its own lease.
	deleteCtx := withCloudWorkspaceWriteLease(ctx, func(ctx context.Context) (context.Context, func(), error) {
		return a.cloudWorkspaceDeleteLease(ctx, workspaceID)
	})
	// v1 delete replaces the manifest with every remote entry except the
	// requested paths. It does not scan the cache, so unrelated local edits
	// stay local, and the cache file is removed only after the manifest lands.
	if err := proto.DeletePaths(deleteCtx, cacheRoot, paths); err != nil {
		return err
	}
	return removeLocal()
}

// cloudWorkspaceDeleteCacheRoot is the directory that owns .maclaw-cloud/state.json.
func (a *App) cloudWorkspaceDeleteCacheRoot(workspaceID, listingRoot string) (string, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	listingRoot = normalizeProjectSessionPath(listingRoot)
	if path, ok := heldCloudWorkspacePath(workspaceID); ok {
		if listingRoot == "" || cloudWorkspaceListingBelongsToCache(path, listingRoot) {
			return path, nil
		}
	}
	candidates := make([]string, 0, 4)
	if parsed := cloudWorkspaceCacheRootFromPath(listingRoot); parsed != "" {
		candidates = append(candidates, parsed)
	}
	if found := cloudWorkspaceCacheRootFromStateDir(listingRoot); found != "" {
		candidates = append(candidates, found)
	}
	if a != nil && workspaceID != "" {
		candidates = append(candidates, normalizeProjectSessionPath(a.cloudWorkspaceCachePath(a.cloudWorkspaceTenantID(), workspaceID)))
	}
	if listingRoot != "" {
		candidates = append(candidates, listingRoot)
	}
	seen := make(map[string]struct{}, len(candidates))
	for _, cand := range candidates {
		cand = normalizeProjectSessionPath(cand)
		if cand == "" {
			continue
		}
		if _, ok := seen[cand]; ok {
			continue
		}
		seen[cand] = struct{}{}
		if id := cloudWorkspaceIDFromPathString(cand); id != "" && workspaceID != "" && id != workspaceID {
			continue
		}
		if listingRoot != "" && !cloudWorkspaceListingBelongsToCache(cand, listingRoot) {
			continue
		}
		return cand, nil
	}
	return "", fmt.Errorf("working directory is unavailable")
}

func cloudWorkspaceListingBelongsToCache(cacheRoot, listingRoot string) bool {
	cacheRoot = resolveExistingCloudWorkspacePath(cacheRoot)
	listingRoot = resolveExistingCloudWorkspacePath(listingRoot)
	return cacheRoot != "" && listingRoot != "" && cloudWorkspacePathInsideRoot(cacheRoot, listingRoot)
}

func resolveExistingCloudWorkspacePath(path string) string {
	path = normalizeProjectSessionPath(path)
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return normalizeProjectSessionPath(abs)
}

func cloudWorkspaceCacheRootFromStateDir(dir string) string {
	dir = normalizeProjectSessionPath(dir)
	for n := 0; n < 8 && dir != ""; n++ {
		info, err := os.Stat(filepath.Join(dir, cloudWorkspaceCacheStateDir))
		if err == nil && info.IsDir() {
			return dir
		}
		parent := normalizeProjectSessionPath(filepath.Dir(dir))
		if parent == "" || parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func cacheRelativeCloudWorkspacePath(cacheRoot, absPath string) (string, error) {
	cacheRoot = resolveExistingCloudWorkspacePath(cacheRoot)
	absPath = resolveExistingCloudWorkspacePath(absPath)
	rel, err := filepath.Rel(cacheRoot, absPath)
	if err != nil {
		return "", fmt.Errorf("path outside the working directory")
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("path outside the working directory")
	}
	cleaned, ok := cloudWorkspaceSafeRelPath(rel)
	if !ok {
		return "", fmt.Errorf("path outside the working directory")
	}
	return cleaned, nil
}

func inferCloudWorkspaceDeleteDir(root, cacheRel string) (bool, error) {
	state, err := readCloudWorkspaceLocalState(root)
	if err != nil {
		return false, err
	}
	prefix := cacheRel + "/"
	consider := func(path string) (bool, bool) {
		if path == cacheRel {
			return true, false
		}
		if strings.HasPrefix(path, prefix) {
			return true, true
		}
		return false, false
	}
	for _, entry := range state.LastEntries {
		if hit, dir := consider(entry.Path); hit {
			return dir, nil
		}
	}
	listing, err := readCloudWorkspaceListing(root)
	if err != nil || listing == nil {
		return false, err
	}
	for _, entry := range listing.Entries {
		if hit, dir := consider(entry.Path); hit {
			return dir, nil
		}
	}
	return false, nil
}

func collectCloudWorkspaceDeletePaths(root, cacheRel string, isDir bool) ([]string, error) {
	covered := func(path string) bool {
		if path == cacheRel {
			return !isDir
		}
		return strings.HasPrefix(path, cacheRel+"/")
	}
	seen := map[string]struct{}{}
	var paths []string
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || !covered(path) || codingWorkbenchEntryProtected(path) {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	local, err := listCloudWorkspaceFilesUnder(root, cacheRel, isDir)
	if err != nil {
		return nil, err
	}
	for _, path := range local {
		add(path)
	}
	if isDir {
		state, err := readCloudWorkspaceLocalState(root)
		if err != nil {
			return nil, err
		}
		for _, entry := range state.LastEntries {
			add(entry.Path)
		}
	}
	listing, err := readCloudWorkspaceListing(root)
	if err != nil {
		return nil, err
	}
	if listing != nil {
		for _, entry := range listing.Entries {
			add(entry.Path)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func codingWorkbenchEntryProtected(relativePath string) bool {
	for _, seg := range strings.Split(relativePath, "/") {
		if skipCodingWorkbenchDownloadName(seg) {
			return true
		}
	}
	return false
}

func (a *App) codingWorkbenchRejectsLocalOpen(projectPath string) bool {
	if a.projectRecordCodingMode(projectPath) == taskRemoteCodingDevTag {
		return true
	}
	return a.interactionInfraReady() && a.GetCodingWorkbenchStatus(projectPath).Kind == "remote"
}

func (a *App) codingWorkbenchLocalFileToOpen(projectPath, relativePath string) (string, error) {
	root, err := codingWorkbenchBrowserLocalRoot(a, projectPath)
	if err != nil {
		return "", err
	}
	if err := a.materializeCloudWorkspaceListedFile(root, relativePath); err != nil {
		return "", err
	}
	return codingWorkbenchLocalFileAbsPath(root, relativePath)
}

func codingWorkbenchLocalFileAbsPath(root, relativePath string) (string, error) {
	relativePath, err := cleanCodingWorkbenchBrowserPath(relativePath)
	if err != nil || relativePath == "" {
		if err == nil {
			err = fmt.Errorf("file path is required")
		}
		return "", err
	}
	absPath, err := codingWorkbenchBrowserLocalPath(root, relativePath)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory")
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("entry cannot be opened locally")
	}
	return absPath, nil
}

// downloadRemoteCodingWorkbenchFileForVSCode writes a remote source file to a
// task-specific temporary snapshot. A temp file plus rename prevents VS Code
// from ever observing a partially downloaded file. Each launch gets its own
// snapshot, so refreshing a remote file cannot overwrite unsaved local edits in
// a VS Code window. The cache is local-only: edits are intentionally not
// uploaded to the remote task.
func (a *App) downloadRemoteCodingWorkbenchFileForVSCode(projectPath, relativePath string, properties CodingWorkbenchEntryProperties) (string, func(), error) {
	if strings.TrimSpace(properties.AbsPath) == "" {
		return "", nil, fmt.Errorf("remote file path is unavailable")
	}
	if properties.SizeKnown && properties.Size > codingWorkbenchVSCodeRemoteMaxFileBytes {
		return "", nil, fmt.Errorf("remote file is too large to open locally with VS Code (limit %d MB)", codingWorkbenchVSCodeRemoteMaxFileBytes/(1024*1024))
	}
	sessionID, _, err := a.acpRemoteSSHSession(projectPath)
	if err != nil {
		return "", nil, err
	}
	hub := a.ensureHubClient()
	if hub == nil || hub.ensureIMHandler() == nil {
		return "", nil, fmt.Errorf("AI assistant not initialized")
	}

	digest := sha256.Sum256([]byte(projectPath))
	cacheRoot := filepath.Join(os.TempDir(), "maclaw-vscode", fmt.Sprintf("%x", digest[:]))
	// Snapshot directories must not grow forever. This cache contains only
	// generated, local copies and cleanup is deliberately best-effort: a locked
	// file or a still-open VS Code window is never allowed to block the current
	// open request.
	cleanupCodingWorkbenchVSCodeRemoteSnapshots(cacheRoot, time.Now())
	snapshotsRoot := filepath.Join(cacheRoot, "snapshots")
	if err := os.MkdirAll(snapshotsRoot, 0o700); err != nil {
		return "", nil, fmt.Errorf("create local VS Code cache: %w", err)
	}
	snapshotRoot, err := os.MkdirTemp(snapshotsRoot, "snapshot-")
	if err != nil {
		return "", nil, fmt.Errorf("create local VS Code snapshot: %w", err)
	}
	keepSnapshot := false
	defer func() {
		if !keepSnapshot {
			_ = os.RemoveAll(snapshotRoot)
		}
	}()
	localPath := filepath.Join(snapshotRoot, filepath.FromSlash(relativePath))
	if !isPathInsideRoot(cacheRoot, localPath) {
		return "", nil, fmt.Errorf("local cache path is invalid")
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o700); err != nil {
		return "", nil, fmt.Errorf("create local VS Code cache: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(localPath), "."+filepath.Base(localPath)+".download-*")
	if err != nil {
		return "", nil, fmt.Errorf("create local VS Code download: %w", err)
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return "", nil, fmt.Errorf("prepare local VS Code download: %w", err)
	}
	defer os.Remove(temporaryPath)

	if _, err := hub.ensureIMHandler().ensureSSHManager().SFTPDownloadFileLimited(sessionID, temporaryPath, properties.AbsPath, codingWorkbenchVSCodeRemoteMaxFileBytes); err != nil {
		return "", nil, fmt.Errorf("download remote file for VS Code: %w", err)
	}
	if downloaded, err := os.Stat(temporaryPath); err != nil {
		return "", nil, fmt.Errorf("verify local VS Code download: %w", err)
	} else if downloaded.Size() > codingWorkbenchVSCodeRemoteMaxFileBytes {
		return "", nil, fmt.Errorf("remote file exceeds the %d MB VS Code download limit", codingWorkbenchVSCodeRemoteMaxFileBytes/(1024*1024))
	} else if properties.SizeKnown && downloaded.Size() != properties.Size {
		return "", nil, fmt.Errorf("remote file changed while downloading; please try again")
	}
	_ = os.Chmod(temporaryPath, 0o600)
	if err := os.Rename(temporaryPath, localPath); err != nil {
		return "", nil, fmt.Errorf("finalize local VS Code download: %w", err)
	}
	keepSnapshot = true
	return localPath, func() { _ = os.RemoveAll(snapshotRoot) }, nil
}

func cleanupCodingWorkbenchVSCodeRemoteSnapshots(cacheRoot string, now time.Time) {
	snapshotsRoot := filepath.Join(cacheRoot, "snapshots")
	entries, err := os.ReadDir(snapshotsRoot)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || !now.After(info.ModTime().Add(codingWorkbenchVSCodeRemoteSnapshotRetention)) {
			continue
		}
		candidate := filepath.Join(snapshotsRoot, entry.Name())
		if !isPathInsideRoot(snapshotsRoot, candidate) {
			continue
		}
		_ = os.RemoveAll(candidate)
	}
}

func omitCloudWorkspaceAbsPath(absPath string) string {
	normalized := strings.ToLower(strings.ReplaceAll(absPath, "\\", "/"))
	if cloudCacheText(normalized) {
		return ""
	}
	return absPath
}

func codingWorkbenchEntryProperties(relativePath, absPath, name string, isDir bool, size, modifiedAt int64, mode string) CodingWorkbenchEntryProperties {
	if name == "" {
		name = filepath.Base(absPath)
	}
	extension := ""
	if !isDir {
		extension = strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	}
	return CodingWorkbenchEntryProperties{
		Name: name, Path: relativePath, AbsPath: omitCloudWorkspaceAbsPath(absPath), IsDir: isDir,
		Size: size, SizeKnown: !isDir, ModifiedAt: modifiedAt, Mode: mode, Extension: extension,
	}
}

func (a *App) getRemoteCodingWorkbenchEntryProperties(projectPath, relativePath string) (CodingWorkbenchEntryProperties, error) {
	sessionID, root, err := a.acpRemoteSSHSession(projectPath)
	if err != nil {
		return CodingWorkbenchEntryProperties{}, err
	}
	absPath := codingWorkbenchBrowserRemotePath(root, relativePath)
	if !remotePathWithinDir(absPath, root) {
		return CodingWorkbenchEntryProperties{}, fmt.Errorf("path outside remote work_dir")
	}
	script := `import json, os, stat, sys
root = os.path.realpath(sys.argv[1])
target = os.path.realpath(sys.argv[2])
if not (root == os.sep or target == root or target.startswith(root + os.sep)):
    raise SystemExit("path outside remote work_dir")
try:
    info = os.stat(target)
except OSError as exc:
    raise SystemExit(str(exc))
is_dir = stat.S_ISDIR(info.st_mode)
name = os.path.basename(target.rstrip(os.sep)) or target
print(json.dumps({"name": name, "abs_path": target, "is_dir": is_dir, "size": info.st_size, "size_known": (not is_dir), "modified_at": int(info.st_mtime), "mode": format(stat.S_IMODE(info.st_mode), "04o")}, ensure_ascii=True))`
	raw, err := a.codingWorkbenchRemoteExec(sessionID, fmt.Sprintf("%s %s %s", remotePythonCommand(script), remoteShellQuote(root), remoteShellQuote(absPath)), 15*time.Second)
	if err != nil {
		return CodingWorkbenchEntryProperties{}, err
	}
	var result CodingWorkbenchEntryProperties
	for _, line := range strings.Split(raw, "\n") {
		if json.Unmarshal([]byte(line), &result) == nil && result.AbsPath != "" {
			result.Path = relativePath
			if !result.IsDir {
				result.Extension = strings.TrimPrefix(strings.ToLower(filepath.Ext(result.Name)), ".")
			}
			return result, nil
		}
	}
	return CodingWorkbenchEntryProperties{}, fmt.Errorf("remote properties response invalid")
}

func remoteWorkbenchFilePreviewCommand(root, path string) string {
	script := strings.NewReplacer(
		"@@ROOT@@", base64EncodeString(root),
		"@@PATH@@", base64EncodeString(path),
	).Replace(`import base64, os, pathlib, stat, sys
root = os.path.realpath(base64.b64decode('@@ROOT@@').decode('utf-8'))
requested = base64.b64decode('@@PATH@@').decode('utf-8')
target = os.path.realpath(requested)
def inside(path):
    return root == os.sep or path == root or path.startswith(root + os.sep)
if not inside(target):
    raise SystemExit("path outside remote work_dir")
try:
    info = os.stat(target)
except OSError as exc:
    raise SystemExit(str(exc))
if not stat.S_ISREG(info.st_mode):
    raise SystemExit("path is not a file")
p = pathlib.Path(target)
start = 1
limit = 2000
shown = 0
last_lineno = 0
try:
    with p.open('r', encoding='utf-8', errors='strict') as handle:
        live = target
        fd_path = '/proc/self/fd/' + str(handle.fileno())
        # lexists does not follow the link. exists() stats the target and
        # returns false when that stat is denied, which would skip this check.
        if os.path.lexists(fd_path):
            try:
                live = os.path.realpath(fd_path)
            except OSError:
                live = target
        if not inside(live):
            raise SystemExit("path outside remote work_dir")
        for lineno, line in enumerate(handle, start=1):
            last_lineno = lineno
            if lineno < start:
                continue
            if shown >= limit:
                sys.stdout.write('\n[remote read_file truncated: showing lines %d-%d; call again with offset=%d]\n' % (start, lineno - 1, lineno))
                break
            sys.stdout.write(f'{lineno}\t{line}')
            shown += 1
except UnicodeDecodeError:
    sys.stdout.write('[remote read_file binary/non-UTF8: %d bytes; text line range unavailable for offset=%d limit=%d]\n' % (info.st_size, start, limit))
    sys.exit(0)
if shown == 0 and start > last_lineno:
    sys.stdout.write('[remote read_file EOF: offset %d is beyond scanned file length %d]\n' % (start, last_lineno))
`)
	return remotePythonCommand(script)
}

func (a *App) getRemoteCodingWorkbenchFilePreview(projectPath, relativePath string) (CodingWorkbenchFilePreview, error) {
	sessionID, root, err := a.acpRemoteSSHSession(projectPath)
	if err != nil {
		return CodingWorkbenchFilePreview{}, err
	}
	absPath := acpResolveRemotePath(relativePath, root)
	if !remotePathWithinDir(absPath, root) {
		return CodingWorkbenchFilePreview{}, fmt.Errorf("path outside remote work_dir")
	}
	// One Python process resolves the path, confirms the opened file is still
	// inside work_dir, and reads it. Two processes joined by && could follow
	// a symlink that was swapped in between them.
	raw, err := a.codingWorkbenchRemoteExec(sessionID, remoteWorkbenchFilePreviewCommand(root, absPath), 20*time.Second)
	if err != nil {
		return CodingWorkbenchFilePreview{}, err
	}
	content := extractRemoteReadPreviewContent(raw)
	// The marker is also a legal string inside a text file. Treat it as binary
	// only when the read produced no numbered source lines.
	if strings.TrimSpace(content) == "" && strings.Contains(raw, "[remote read_file binary/non-UTF8:") {
		return CodingWorkbenchFilePreview{}, fmt.Errorf("binary files cannot be previewed")
	}
	// Only protocol markers indicate truncation. A source file may legitimately
	// contain the word "truncated" and must not receive a misleading preview
	// warning just because of its contents.
	truncated := remotePreviewOutputIsTruncated(raw)
	if utf8.RuneCountInString(content) > codingWorkbenchBrowserMaxRunes {
		content = string([]rune(content)[:codingWorkbenchBrowserMaxRunes])
		truncated = true
	}
	return CodingWorkbenchFilePreview{Path: relativePath, AbsPath: omitCloudWorkspaceAbsPath(absPath), Content: content, Language: detectLanguageFromExt(absPath), Truncated: truncated}, nil
}

const (
	codingWorkbenchDownloadMaxFileBytes    int64 = 512 << 20
	codingWorkbenchDownloadMaxArchiveBytes int64 = 1 << 30
	codingWorkbenchDownloadMaxFiles              = 20000
)

var codingWorkbenchSaveDialog = func(a *App, title, defaultName string, filters []runtime.FileFilter) (string, error) {
	if a == nil || a.ctx == nil {
		return "", nil
	}
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           title,
		DefaultFilename: defaultName,
		Filters:         filters,
	})
}

func sanitizeCodingWorkbenchDownloadName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "download"
	}
	replacer := strings.NewReplacer(`\`, "_", `/`, "_", `:`, "_", `*`, "_", `?`, "_", `"`, "_", `<`, "_", `>`, "_", `|`, "_")
	name = strings.TrimSpace(replacer.Replace(name))
	if name == "" {
		return "download"
	}
	return name
}

func skipCodingWorkbenchDownloadName(name string) bool {
	n := strings.TrimSpace(name)
	return n == cloudWorkspaceCacheStateDir || n == ".maclaw-cloud"
}

// latexSubmissionSkipName drops TeX build products and editor backups from a
// submission archive. Sources, figures, bibliographies and the compiled PDF stay.
func latexSubmissionSkipName(name string) bool {
	n := strings.TrimSpace(name)
	if n == "" || n == "." || n == ".." {
		return true
	}
	lower := strings.ToLower(n)
	if strings.HasPrefix(lower, ".") || skipCodingWorkbenchDownloadName(n) {
		return true
	}
	if strings.HasSuffix(lower, ".maclaw-bak") ||
		strings.HasSuffix(lower, ".synctex.gz") ||
		strings.HasSuffix(lower, ".synctex(busy)") ||
		strings.HasSuffix(lower, ".fdb_latexmk") ||
		strings.HasSuffix(lower, ".run.xml") {
		return true
	}
	switch strings.ToLower(filepath.Ext(n)) {
	case ".aux", ".log", ".out", ".toc", ".lof", ".lot", ".fls", ".nav", ".snm", ".vrb", ".xdv", ".dvi", ".blg", ".bcf":
		return true
	default:
		return false
	}
}

func latexSubmissionArchiveBase(taskPath, root string) string {
	base := filepath.Base(strings.TrimRight(taskPath, `\/`))
	if strings.EqualFold(filepath.Base(root), "workspace") {
		parent := filepath.Base(filepath.Dir(root))
		if parent != "" && parent != "." && parent != string(filepath.Separator) {
			base = parent
		}
	}
	name := sanitizeCodingWorkbenchDownloadName(base)
	if name == "" || name == "download" {
		return "paper-submission"
	}
	return name
}

// ExportLatexSubmissionZip writes the paper workspace to a zip the user can
// upload. The save dialog's empty result means the user cancelled.
func (a *App) ExportLatexSubmissionZip(projectPath string) (string, error) {
	projectPath = normalizeProjectSessionPath(projectPath)
	if a == nil || projectPath == "" {
		return "", fmt.Errorf("project path is required")
	}
	if a.GetCodingWorkbenchStatus(projectPath).Kind == "remote" {
		return "", fmt.Errorf("download is not available for remote workspaces")
	}
	root, err := codingWorkbenchBrowserLocalRoot(a, projectPath)
	if err != nil {
		return "", err
	}
	base := latexSubmissionArchiveBase(projectPath, root)
	dest, err := codingWorkbenchSaveDialog(a, "导出投稿包", base+"-submission.zip", []runtime.FileFilter{
		{DisplayName: "Zip (*.zip)", Pattern: "*.zip"},
	})
	if err != nil {
		return "", err
	}
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return "", nil
	}
	if !strings.HasSuffix(strings.ToLower(dest), ".zip") {
		dest += ".zip"
	}
	if isPathInsideRoot(root, dest) {
		return "", fmt.Errorf("cannot save the archive inside the folder being downloaded")
	}
	return writeLatexSubmissionZip(root, dest, base)
}

// ExportLatexSourceBundle zips the directory that holds a paper artifact.
// The anchor is the compiled PDF or a source file. The archive contains the
// main file, figures and the other sources a compiler needs, and omits TeX
// build products. An empty save path means the user cancelled.
func (a *App) ExportLatexSourceBundle(anchorPath string) (string, error) {
	anchorPath = strings.TrimSpace(anchorPath)
	if a == nil || anchorPath == "" {
		return "", fmt.Errorf("file path is required")
	}
	info, err := os.Stat(anchorPath)
	if err != nil {
		return "", err
	}
	root := anchorPath
	if !info.IsDir() {
		root = filepath.Dir(anchorPath)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	root = latexPaperRoot(root)
	if parent := filepath.Dir(root); parent == root || !dirHasLatexSource(root) {
		return "", fmt.Errorf("folder has no latex source")
	}
	base := latexSubmissionArchiveBase(root, root)
	dest, err := codingWorkbenchSaveDialog(a, "导出 LaTeX 源码包", base+"-source.zip", []runtime.FileFilter{
		{DisplayName: "Zip (*.zip)", Pattern: "*.zip"},
	})
	if err != nil {
		return "", err
	}
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return "", nil
	}
	if !strings.HasSuffix(strings.ToLower(dest), ".zip") {
		dest += ".zip"
	}
	if isPathInsideRoot(root, dest) {
		return "", fmt.Errorf("cannot save the archive inside the folder being downloaded")
	}
	return writeLatexSubmissionZip(root, dest, base)
}

func writeLatexSubmissionZip(root, dest, base string) (string, error) {
	if err := zipLatexSubmissionDir(root, dest, base); err != nil {
		_ = os.Remove(dest)
		return "", err
	}
	return dest, nil
}

func dirHasLatexSource(root string) bool {
	found := false
	stop := fmt.Errorf("latex source found")
	_ = filepath.Walk(root, func(p string, info os.FileInfo, walkErr error) error {
		if found || walkErr != nil || info == nil {
			return walkErr
		}
		if p != root && latexSubmissionSkipName(info.Name()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(info.Name())) {
		case ".tex", ".ltx", ".latex":
			found = true
			return stop
		default:
			return nil
		}
	})
	return found
}

// latexPaperRoot is the directory that holds the main file, figures and the
// other sources. A chapter path is lifted to the nearest parent whose top-level
// .tex declares \documentclass. A figure PDF sitting next to a chapter does not
// count as the paper. Without a document class, the nearest directory that has
// both a top-level .tex and a .pdf is used. A filesystem root is never used.
func latexPaperRoot(start string) string {
	start = filepath.Clean(strings.TrimSpace(start))
	if start == "" || filepath.Dir(start) == start {
		return start
	}
	bestPair := ""
	bestTex := ""
	current := start
	for i := 0; i < 4; i++ {
		if filepath.Dir(current) == current {
			break
		}
		if dirHasDocumentClass(current) {
			return current
		}
		hasTex := dirHasTopLevelExt(current, ".tex", ".ltx", ".latex")
		hasPDF := dirHasTopLevelExt(current, ".pdf")
		if hasTex && hasPDF && bestPair == "" {
			bestPair = current
		}
		if hasTex && bestTex == "" {
			bestTex = current
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	if bestPair != "" {
		return bestPair
	}
	if bestTex != "" {
		return bestTex
	}
	return start
}

func dirHasDocumentClass(root string) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		switch strings.ToLower(filepath.Ext(name)) {
		case ".tex", ".ltx", ".latex":
		default:
			continue
		}
		if latexSubmissionSkipName(name) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		f, err := os.Open(filepath.Join(root, name))
		if err != nil {
			continue
		}
		buf := make([]byte, 16<<10)
		n, _ := f.Read(buf)
		_ = f.Close()
		if latexSourceDeclaresDocument(buf[:n]) {
			return true
		}
	}
	return false
}

func texCodePrefix(line string) string {
	var b strings.Builder
	backslashes := 0
	for _, r := range line {
		if r == '%' && backslashes%2 == 0 {
			break
		}
		b.WriteRune(r)
		if r == '\\' {
			backslashes++
		} else {
			backslashes = 0
		}
	}
	return strings.TrimSpace(b.String())
}

func lineStartsDocument(code string) bool {
	return strings.HasPrefix(code, `\documentclass`) || strings.HasPrefix(code, `\documentstyle`)
}

func latexSourceDeclaresDocument(raw []byte) bool {
	line := make([]byte, 0, 256)
	flush := func() bool {
		text := string(line)
		line = line[:0]
		return lineStartsDocument(texCodePrefix(text))
	}
	for _, b := range raw {
		if b == '\n' || b == '\r' {
			if flush() {
				return true
			}
			continue
		}
		if len(line) < 4096 {
			line = append(line, b)
		}
	}
	return flush()
}

func dirHasTopLevelExt(root string, exts ...string) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	want := map[string]bool{}
	for _, ext := range exts {
		want[strings.ToLower(ext)] = true
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if want[strings.ToLower(filepath.Ext(entry.Name()))] {
			return true
		}
	}
	return false
}

func zipLatexSubmissionDir(srcDir, dest, archiveRoot string) error {
	archiveRoot = strings.Trim(strings.ReplaceAll(archiveRoot, "\\", "/"), "/")
	if archiveRoot == "" || archiveRoot == "." || strings.Contains(archiveRoot, "..") {
		archiveRoot = "paper-submission"
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	var written int64
	var files int
	err = filepath.Walk(srcDir, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if p != srcDir && latexSubmissionSkipName(info.Name()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if p == srcDir || info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		files++
		if files > codingWorkbenchDownloadMaxFiles {
			return fmt.Errorf("archive exceeds file count limit")
		}
		if written+info.Size() > codingWorkbenchDownloadMaxArchiveBytes {
			return fmt.Errorf("archive exceeds download size limit")
		}
		rel, err := filepath.Rel(srcDir, p)
		if err != nil {
			return err
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		hdr.Name = path.Join(archiveRoot, filepath.ToSlash(rel))
		hdr.Method = zip.Deflate
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		n, copyErr := io.Copy(w, f)
		_ = f.Close()
		if copyErr != nil {
			return copyErr
		}
		written += n
		return nil
	})
	closeErr := zw.Close()
	if err != nil {
		return err
	}
	if files == 0 {
		return fmt.Errorf("the working directory has no files to export")
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}

// DownloadCodingWorkbenchEntry copies a workbench file, or tars a directory, to a
// path chosen in the save dialog. Empty dest means the user cancelled.
func (a *App) DownloadCodingWorkbenchEntry(projectPath, relativePath string) (string, error) {
	projectPath = normalizeProjectSessionPath(projectPath)
	if a == nil || projectPath == "" {
		return "", fmt.Errorf("project path is required")
	}
	if a.GetCodingWorkbenchStatus(projectPath).Kind == "remote" {
		return "", fmt.Errorf("download is not available for remote workspaces")
	}
	relativePath, err := cleanCodingWorkbenchBrowserPath(relativePath)
	if err != nil {
		return "", err
	}
	root, err := codingWorkbenchBrowserLocalRoot(a, projectPath)
	if err != nil {
		return "", err
	}
	if err := a.materializeCloudWorkspaceListedTree(root, relativePath); err != nil {
		return "", err
	}
	src, err := codingWorkbenchBrowserLocalPath(root, relativePath)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(src)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return "", fmt.Errorf("entry cannot be downloaded")
	}
	name := sanitizeCodingWorkbenchDownloadName(info.Name())
	var dest string
	if info.IsDir() {
		dest, err = codingWorkbenchSaveDialog(a, "下载目录", name+".tar", []runtime.FileFilter{
			{DisplayName: "Tar (*.tar)", Pattern: "*.tar"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		})
	} else {
		dest, err = codingWorkbenchSaveDialog(a, "下载文件", name, []runtime.FileFilter{
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		})
	}
	if err != nil {
		return "", err
	}
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return "", nil
	}
	if info.IsDir() && isPathInsideRoot(src, dest) {
		return "", fmt.Errorf("cannot save the archive inside the folder being downloaded")
	}
	if info.IsDir() {
		if err := tarCodingWorkbenchDownload(src, dest, name); err != nil {
			_ = os.Remove(dest)
			return "", err
		}
		return dest, nil
	}
	if err := copyCodingWorkbenchDownload(src, dest, codingWorkbenchDownloadMaxFileBytes); err != nil {
		_ = os.Remove(dest)
		return "", err
	}
	return dest, nil
}

func copyCodingWorkbenchDownload(src, dest string, maxBytes int64) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	n, err := io.Copy(out, io.LimitReader(in, maxBytes+1))
	if err != nil {
		return err
	}
	if n > maxBytes {
		return fmt.Errorf("file exceeds download size limit")
	}
	return out.Close()
}

func tarCodingWorkbenchDownload(srcDir, dest, archiveRoot string) error {
	archiveRoot = strings.Trim(strings.ReplaceAll(archiveRoot, "\\", "/"), "/")
	if archiveRoot == "" || archiveRoot == "." || strings.Contains(archiveRoot, "..") {
		archiveRoot = "download"
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	tw := tar.NewWriter(out)
	var written int64
	var files int
	err = filepath.Walk(srcDir, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if skipCodingWorkbenchDownloadName(info.Name()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() && !info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, p)
		if err != nil {
			return err
		}
		name := archiveRoot
		if rel != "." {
			name = path.Join(archiveRoot, filepath.ToSlash(rel))
		}
		if info.IsDir() {
			hdr := &tar.Header{
				Name:     strings.TrimSuffix(name, "/") + "/",
				Mode:     int64(info.Mode().Perm()),
				ModTime:  info.ModTime(),
				Typeflag: tar.TypeDir,
			}
			return tw.WriteHeader(hdr)
		}
		files++
		if files > codingWorkbenchDownloadMaxFiles {
			return fmt.Errorf("archive exceeds file count limit")
		}
		if written+info.Size() > codingWorkbenchDownloadMaxArchiveBytes {
			return fmt.Errorf("archive exceeds download size limit")
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = name
		hdr.ModTime = info.ModTime()
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		n, copyErr := io.Copy(tw, f)
		_ = f.Close()
		if copyErr != nil {
			return copyErr
		}
		written += n
		return nil
	})
	closeErr := tw.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}
