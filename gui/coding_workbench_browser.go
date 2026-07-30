package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
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
	root := strings.TrimSpace(a.recentTaskExecutionProjectPath(projectPath))
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

func parseCodingWorkbenchRemoteDirectoryRecords(raw, relativePath string) ([]CodingWorkbenchDirectoryEntry, bool) {
	entries := make([]CodingWorkbenchDirectoryEntry, 0)
	truncated := false
	for _, line := range strings.Split(raw, "\n") {
		var item codingWorkbenchRemoteDirectoryRecord
		if json.Unmarshal([]byte(line), &item) != nil {
			continue
		}
		if item.Truncated != nil {
			truncated = *item.Truncated
			continue
		}
		if item.Name == "" {
			continue
		}
		entries = append(entries, CodingWorkbenchDirectoryEntry{
			Name: item.Name, Path: path.Join(relativePath, item.Name), IsDir: item.IsDir,
		})
	}
	sortCodingWorkbenchDirectoryEntries(entries)
	return entries, truncated
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

// collectCodingWorkbenchDirectoryEntries intentionally reads one small page
// only. Scanning every entry merely to select a globally sorted first page can
// make a network share or generated directory take seconds to open.
func collectCodingWorkbenchDirectoryEntries(dir *os.File, relativePath string) ([]CodingWorkbenchDirectoryEntry, bool, error) {
	items, err := dir.ReadDir(codingWorkbenchBrowserMaxEntries + 1)
	if err != nil && err != io.EOF {
		return nil, false, err
	}
	truncated := len(items) > codingWorkbenchBrowserMaxEntries
	if truncated {
		items = items[:codingWorkbenchBrowserMaxEntries]
	}
	entries := make([]CodingWorkbenchDirectoryEntry, 0, len(items))
	for _, item := range items {
		entries = append(entries, CodingWorkbenchDirectoryEntry{
			Name: item.Name(), Path: path.Join(relativePath, item.Name()), IsDir: item.IsDir(),
		})
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
	return CodingWorkbenchDirectoryResponse{Root: root, Path: relativePath, Entries: entries, Truncated: truncated}, nil
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
	hub := a.ensureHubClient()
	if hub == nil || hub.ensureIMHandler() == nil {
		return CodingWorkbenchDirectoryResponse{}, fmt.Errorf("AI assistant not initialized")
	}
	// Resolve the requested directory remotely before listing it. The lexical
	// client-side check above is not sufficient for a symlink inside work_dir
	// pointing outside it.
	script := `import json,os,sys; root=os.path.realpath(sys.argv[1]); target=os.path.realpath(sys.argv[2]); ok=(root==os.sep or target==root or target.startswith(root+os.sep));
if not ok: raise SystemExit("path outside remote work_dir")
if not os.path.isdir(target): raise SystemExit("path is not a directory")
limit=500; xs=[]
with os.scandir(target) as scan:
 for e in scan:
  xs.append(e)
  if len(xs)>limit: break
truncated=len(xs)>limit; xs=xs[:limit]; print(json.dumps({"truncated":truncated})); [print(json.dumps({"name":e.name,"is_dir":e.is_dir(follow_symlinks=False)})) for e in xs]`
	raw := hub.ensureIMHandler().sshExec(map[string]interface{}{
		"session_id": sessionID,
		// Use the shared base64-backed launcher: raw multi-line `python -c`
		// commands can be normalized by the SSH/PTTY transport.
		"command":      fmt.Sprintf("%s %s %s", remotePythonCommand(script), remoteShellQuote(root), remoteShellQuote(absPath)),
		"wait_seconds": float64(15),
	})
	if remoteCodingToolOutcome(raw) != "success" {
		return CodingWorkbenchDirectoryResponse{}, fmt.Errorf("%s", compactRemoteSSHError(raw))
	}
	// JSON output does not match the `ls -l` envelope heuristic. Parse the raw
	// SSH transcript directly so a normal successful response is not discarded.
	entries, truncated := parseCodingWorkbenchRemoteDirectoryRecords(raw, relativePath)
	return CodingWorkbenchDirectoryResponse{Root: root, Path: relativePath, Entries: entries, Truncated: truncated}, nil
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
	return CodingWorkbenchFilePreview{Path: relativePath, AbsPath: absPath, Content: content, Language: detectLanguageFromExt(absPath), Truncated: truncated}, nil
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

func codingWorkbenchEntryProperties(relativePath, absPath, name string, isDir bool, size, modifiedAt int64, mode string) CodingWorkbenchEntryProperties {
	if name == "" {
		name = filepath.Base(absPath)
	}
	extension := ""
	if !isDir {
		extension = strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	}
	return CodingWorkbenchEntryProperties{
		Name: name, Path: relativePath, AbsPath: absPath, IsDir: isDir,
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
	hub := a.ensureHubClient()
	if hub == nil || hub.ensureIMHandler() == nil {
		return CodingWorkbenchEntryProperties{}, fmt.Errorf("AI assistant not initialized")
	}
	script := `import json,os,stat,sys; root=os.path.realpath(sys.argv[1]); target=os.path.realpath(sys.argv[2]); ok=(root==os.sep or target==root or target.startswith(root+os.sep));
if not ok: raise SystemExit("path outside remote work_dir")
s=os.stat(target); isdir=stat.S_ISDIR(s.st_mode); name=os.path.basename(target.rstrip(os.sep)) or target; print(json.dumps({"name":name,"abs_path":target,"is_dir":isdir,"size":s.st_size,"size_known":not isdir,"modified_at":int(s.st_mtime),"mode":format(stat.S_IMODE(s.st_mode),"04o")}))`
	raw := hub.ensureIMHandler().sshExec(map[string]interface{}{
		"session_id":   sessionID,
		"command":      fmt.Sprintf("%s %s %s", remotePythonCommand(script), remoteShellQuote(root), remoteShellQuote(absPath)),
		"wait_seconds": float64(15),
	})
	if remoteCodingToolOutcome(raw) != "success" {
		return CodingWorkbenchEntryProperties{}, fmt.Errorf("%s", compactRemoteSSHError(raw))
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

func (a *App) getRemoteCodingWorkbenchFilePreview(projectPath, relativePath string) (CodingWorkbenchFilePreview, error) {
	sessionID, root, err := a.acpRemoteSSHSession(projectPath)
	if err != nil {
		return CodingWorkbenchFilePreview{}, err
	}
	absPath := acpResolveRemotePath(relativePath, root)
	if !remotePathWithinDir(absPath, root) {
		return CodingWorkbenchFilePreview{}, fmt.Errorf("path outside remote work_dir")
	}
	hub := a.ensureHubClient()
	if hub == nil || hub.ensureIMHandler() == nil {
		return CodingWorkbenchFilePreview{}, fmt.Errorf("AI assistant not initialized")
	}
	// Verify the real path before delegating the bounded read to the shared
	// helper. Without this check a symlink under work_dir could expose a file
	// outside the workspace.
	verifyScript := `import os,sys; root=os.path.realpath(sys.argv[1]); target=os.path.realpath(sys.argv[2]); ok=(root==os.sep or target==root or target.startswith(root+os.sep));
if not ok: raise SystemExit("path outside remote work_dir")
if not os.path.isfile(target): raise SystemExit("path is not a file")`
	verify := hub.ensureIMHandler().sshExec(map[string]interface{}{
		"session_id":   sessionID,
		"command":      fmt.Sprintf("%s %s %s", remotePythonCommand(verifyScript), remoteShellQuote(root), remoteShellQuote(absPath)),
		"wait_seconds": float64(15),
	})
	if remoteCodingToolOutcome(verify) != "success" {
		return CodingWorkbenchFilePreview{}, fmt.Errorf("%s", compactRemoteSSHError(verify))
	}
	raw := hub.ensureIMHandler().sshExec(map[string]interface{}{
		"session_id":   sessionID,
		"command":      remoteReadFileRangePythonCommand(absPath, 1, 2000),
		"wait_seconds": float64(20),
	})
	if remoteCodingToolOutcome(raw) != "success" {
		return CodingWorkbenchFilePreview{}, fmt.Errorf("%s", compactRemoteSSHError(raw))
	}
	content := extractRemoteReadPreviewContent(raw)
	// Only protocol markers indicate truncation. A source file may legitimately
	// contain the word "truncated" and must not receive a misleading preview
	// warning just because of its contents.
	truncated := remotePreviewOutputIsTruncated(raw)
	if utf8.RuneCountInString(content) > codingWorkbenchBrowserMaxRunes {
		content = string([]rune(content)[:codingWorkbenchBrowserMaxRunes])
		truncated = true
	}
	return CodingWorkbenchFilePreview{Path: relativePath, AbsPath: absPath, Content: content, Language: detectLanguageFromExt(absPath), Truncated: truncated}, nil
}
