package guiapp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/guiapp/wruntime"
)

// This file backs the cloud-agent file browser: a live directory tree inside
// one instance's workspace, fetched through remotecontrol cloud ops. Paths are
// jail-relative — the srv enforces containment and secret-file exclusion, and
// no absolute server path ever reaches the frontend (abs_path stays empty).

// cloudAgentDownloadMaxFileBytes matches the srv read/pack cap: the bytes
// cross the Hub base64'd inside an exec result capped at 8MB, and a bigger
// download would arrive truncated rather than complete.
const cloudAgentDownloadMaxFileBytes int64 = 4 << 20

// Upload goes the other way: every op body crosses two 1MB request readers
// (desktop→hub /op, hub→srv control), so one write carries 512KiB — under the
// srv's own 640KiB chunk cap with base64 headroom to spare. The batch limits
// stop an accidental huge pick before thousands of round trips start.
const (
	cloudAgentUploadChunkBytes = 512 << 10
	cloudAgentUploadMaxFiles   = 20000
	cloudAgentUploadMaxBytes   = int64(1) << 30
)

// cloudAgentFilesChangedEvent tells an open files dialog to re-list after the
// workspace mutated through a path it cannot see (the task context menu).
const cloudAgentFilesChangedEvent = "cloud-agent-files-changed"

// The control grant for an instance is stable (the Hub keeps the same-account
// grant alive); cache it per instance so a tree walk is one round trip per
// directory instead of grant+op per row.
var cloudAgentGrantCache = struct {
	sync.Mutex
	byID map[string]string
}{byID: map[string]string{}}

// cloudAgentOp runs one cloud op on the instance through the Hub grant
// channel. A failed call retries once with a fresh grant in case the cached
// grant was revoked or the Hub restarted.
func (a *App) cloudAgentOp(instanceID, op string, body map[string]any) (string, error) {
	raw, err := a.cloudAgentOpGrant(instanceID, op, body, false)
	if err == nil {
		return raw, nil
	}
	return a.cloudAgentOpGrant(instanceID, op, body, true)
}

func (a *App) cloudAgentOpGrant(instanceID, op string, body map[string]any, freshGrant bool) (string, error) {
	grant := ""
	if !freshGrant {
		cloudAgentGrantCache.Lock()
		grant = cloudAgentGrantCache.byID[instanceID]
		cloudAgentGrantCache.Unlock()
	}
	if grant == "" {
		var err error
		grant, err = a.OpenCloudAgentGrant(instanceID)
		if err != nil {
			return "", err
		}
		cloudAgentGrantCache.Lock()
		cloudAgentGrantCache.byID[instanceID] = grant
		cloudAgentGrantCache.Unlock()
	}
	payload := "{}"
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return "", err
		}
		payload = string(raw)
	}
	return a.RemoteControlOp(grant, op, payload)
}

// cloudAgentBrowseInstanceID resolves a "cloud-agent:<id>" project path to
// the instance id, or "" for non-cloud paths.
func cloudAgentBrowseInstanceID(projectPath string) string {
	return cloudAgentInstanceFromProjectPath(projectPath)
}

func (a *App) getCloudAgentWorkbenchDirectory(instanceID, relativePath string) (CodingWorkbenchDirectoryResponse, error) {
	raw, err := a.cloudAgentOp(instanceID, "cloud_list_dir", map[string]any{"path": relativePath})
	if err != nil {
		return CodingWorkbenchDirectoryResponse{}, err
	}
	var payload struct {
		Path    string `json:"path"`
		Entries []struct {
			Name  string `json:"name"`
			Path  string `json:"path"`
			IsDir bool   `json:"is_dir"`
		} `json:"entries"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return CodingWorkbenchDirectoryResponse{}, fmt.Errorf("云端目录结果无法读取")
	}
	entries := make([]CodingWorkbenchDirectoryEntry, 0, len(payload.Entries))
	for _, e := range payload.Entries {
		if e.Name == "" || isCodingWorkbenchHiddenBrowserName(e.Name) {
			continue
		}
		entries = append(entries, CodingWorkbenchDirectoryEntry{Name: e.Name, Path: e.Path, IsDir: e.IsDir})
	}
	sortCodingWorkbenchDirectoryEntries(entries)
	return CodingWorkbenchDirectoryResponse{
		Root:      "cloud-agent",
		Path:      relativePath,
		Entries:   entries,
		Truncated: payload.Truncated,
	}, nil
}

// cloudAgentReadFile fetches raw file bytes from the instance jail.
func (a *App) cloudAgentReadFile(instanceID, relativePath string, maxBytes int64) (name string, size int64, truncated bool, data []byte, err error) {
	raw, err := a.cloudAgentOp(instanceID, "cloud_read_file", map[string]any{
		"path":      relativePath,
		"max_bytes": maxBytes,
	})
	if err != nil {
		return "", 0, false, nil, err
	}
	var payload struct {
		Name      string `json:"name"`
		Size      int64  `json:"size"`
		Truncated bool   `json:"truncated"`
		Data      string `json:"data_base64"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", 0, false, nil, fmt.Errorf("云端文件结果无法读取")
	}
	data, err = base64.StdEncoding.DecodeString(payload.Data)
	if err != nil {
		return "", 0, false, nil, fmt.Errorf("云端文件结果无法读取")
	}
	return payload.Name, payload.Size, payload.Truncated, data, nil
}

func (a *App) getCloudAgentWorkbenchFilePreview(instanceID, relativePath string) (CodingWorkbenchFilePreview, error) {
	_, _, capped, data, err := a.cloudAgentReadFile(instanceID, relativePath, codingWorkbenchBrowserMaxReadBytes+int64(utf8.UTFMax))
	if err != nil {
		return CodingWorkbenchFilePreview{}, err
	}
	content := data
	if !utf8.Valid(content) && capped {
		for trim := 1; trim < utf8.UTFMax && trim < len(content); trim++ {
			if utf8.Valid(content[:len(content)-trim]) {
				content = content[:len(content)-trim]
				break
			}
		}
	}
	if !isCodePreviewTextContent(content) {
		return CodingWorkbenchFilePreview{}, fmt.Errorf("binary files cannot be previewed")
	}
	runes := []rune(string(content))
	truncated := capped || len(runes) > codingWorkbenchBrowserMaxRunes
	if len(runes) > codingWorkbenchBrowserMaxRunes {
		runes = runes[:codingWorkbenchBrowserMaxRunes]
	}
	return CodingWorkbenchFilePreview{
		Path:      relativePath,
		Content:   string(runes),
		Language:  detectLanguageFromExt(relativePath),
		Truncated: truncated,
	}, nil
}

func (a *App) getCloudAgentWorkbenchEntryProperties(instanceID, relativePath string) (CodingWorkbenchEntryProperties, error) {
	raw, err := a.cloudAgentOp(instanceID, "cloud_stat", map[string]any{"path": relativePath})
	if err != nil {
		return CodingWorkbenchEntryProperties{}, err
	}
	var payload struct {
		Name       string `json:"name"`
		Path       string `json:"path"`
		IsDir      bool   `json:"is_dir"`
		Size       int64  `json:"size"`
		SizeKnown  bool   `json:"size_known"`
		ModifiedAt int64  `json:"modified_at"`
		Mode       string `json:"mode"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return CodingWorkbenchEntryProperties{}, fmt.Errorf("云端属性结果无法读取")
	}
	if strings.TrimSpace(payload.Path) != "" && payload.Path != "/" {
		relativePath = payload.Path
	}
	return codingWorkbenchEntryProperties(relativePath, "", payload.Name, payload.IsDir, payload.Size, payload.ModifiedAt, payload.Mode), nil
}

// openCloudAgentWorkbenchFileLocally downloads the file into a temp folder and
// opens it with the OS default app — the cloud twin of the remote-workspace
// "open locally" snapshot flow.
func (a *App) openCloudAgentWorkbenchFileLocally(instanceID, relativePath string) error {
	props, err := a.getCloudAgentWorkbenchEntryProperties(instanceID, relativePath)
	if err != nil {
		return err
	}
	if props.IsDir {
		return fmt.Errorf("path is a directory")
	}
	if props.SizeKnown && props.Size > cloudAgentDownloadMaxFileBytes {
		return fmt.Errorf("文件太大（%d MB 上限）", cloudAgentDownloadMaxFileBytes/(1<<20))
	}
	name, _, truncated, data, err := a.cloudAgentReadFile(instanceID, relativePath, cloudAgentDownloadMaxFileBytes)
	if err != nil {
		return err
	}
	if truncated {
		return fmt.Errorf("文件太大（%d MB 上限）", cloudAgentDownloadMaxFileBytes/(1<<20))
	}
	if name = filepath.Base(strings.TrimSpace(name)); name == "" || name == "." {
		name = filepath.Base(relativePath)
	}
	dir := filepath.Join(os.TempDir(), "maclaw-cloud-open", instanceID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, "*-"+name)
	if err != nil {
		return err
	}
	local := file.Name()
	if _, err := file.Write(data); err != nil {
		file.Close()
		_ = os.Remove(local)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(local)
		return err
	}
	return a.OpenFileOrShowInFolder(local)
}

// downloadCloudAgentWorkbenchEntry mirrors DownloadCodingWorkbenchEntry: a
// directory arrives as a .tar.gz (srv-packed), a file as its raw bytes, both
// through the same save dialog. displayName replaces the generic root name
// ("workspace") so a whole-workspace pack is named after its task.
func (a *App) downloadCloudAgentWorkbenchEntry(instanceID, relativePath string, displayName ...string) (string, error) {
	props, err := a.getCloudAgentWorkbenchEntryProperties(instanceID, relativePath)
	if err != nil {
		return "", err
	}
	name := sanitizeCodingWorkbenchDownloadName(props.Name)
	if len(displayName) > 0 && props.IsDir && (relativePath == "" || relativePath == "/") {
		// sanitize never returns "" (blank becomes "download"), so gate on the
		// raw input instead — a whitespace name should keep "workspace".
		if strings.TrimSpace(displayName[0]) != "" {
			name = sanitizeCodingWorkbenchDownloadName(displayName[0])
		}
	}
	var dest string
	if props.IsDir {
		dest, err = codingWorkbenchSaveDialog(a, "下载目录", name+".tar.gz", []runtime.FileFilter{
			{DisplayName: "Tar (*.tar.gz)", Pattern: "*.tar.gz"},
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
	if dest = strings.TrimSpace(dest); dest == "" {
		return "", nil
	}
	var op string
	var maxBytes int64
	if props.IsDir {
		op = "cloud_pack_dir"
	} else {
		op = "cloud_read_file"
		maxBytes = cloudAgentDownloadMaxFileBytes
	}
	raw, err := a.cloudAgentOp(instanceID, op, map[string]any{
		"path":      relativePath,
		"max_bytes": maxBytes,
	})
	if err != nil {
		return "", err
	}
	var payload struct {
		Name      string `json:"name"`
		Truncated bool   `json:"truncated"`
		Data      string `json:"data_base64"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", fmt.Errorf("云端文件结果无法读取")
	}
	if payload.Truncated {
		return "", fmt.Errorf("文件太大（%d MB 上限）", cloudAgentDownloadMaxFileBytes/(1<<20))
	}
	data, err := base64.StdEncoding.DecodeString(payload.Data)
	if err != nil {
		return "", fmt.Errorf("云端文件结果无法读取")
	}
	if err := os.WriteFile(dest, data, 0o600); err != nil {
		_ = os.Remove(dest)
		return "", err
	}
	return dest, nil
}

// openCloudAgentWorkbenchFileInVSCode mirrors the remote flow: download to a
// task-scoped temp snapshot, then open in VS Code. Local edits do not sync
// back. Returns true to tell the UI the opened file is a local copy.
func (a *App) openCloudAgentWorkbenchFileInVSCode(instanceID, relativePath, codeCLI string) (bool, error) {
	props, err := a.getCloudAgentWorkbenchEntryProperties(instanceID, relativePath)
	if err != nil {
		return false, err
	}
	if props.IsDir {
		return false, fmt.Errorf("path is a directory")
	}
	// The remotecontrol channel caps the read at cloudAgentDownloadMaxFileBytes
	// — well under the SSH path's 64MB — so the effective limit is the channel's.
	maxBytes := codingWorkbenchVSCodeRemoteMaxFileBytes
	if cloudAgentDownloadMaxFileBytes < maxBytes {
		maxBytes = cloudAgentDownloadMaxFileBytes
	}
	if props.SizeKnown && props.Size > maxBytes {
		return false, fmt.Errorf("remote file is too large to open locally with VS Code (limit %d MB)", maxBytes/(1024*1024))
	}
	name, _, truncated, data, err := a.cloudAgentReadFile(instanceID, relativePath, maxBytes)
	if err != nil {
		return false, err
	}
	if truncated {
		return false, fmt.Errorf("remote file exceeds the %d MB VS Code download limit", maxBytes/(1024*1024))
	}
	dir := filepath.Join(os.TempDir(), "maclaw-cloud-vscode", instanceID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, fmt.Errorf("create local VS Code cache: %w", err)
	}
	base := filepath.Base(strings.TrimSpace(name))
	if base == "" || base == "." {
		base = filepath.Base(relativePath)
	}
	file, err := os.CreateTemp(dir, "*-"+base)
	if err != nil {
		return false, fmt.Errorf("create local VS Code download: %w", err)
	}
	local := file.Name()
	if _, err := file.Write(data); err != nil {
		file.Close()
		_ = os.Remove(local)
		return false, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(local)
		return false, err
	}
	if err := launchVSCodeWithArgs(codeCLI, []string{"-n", local}); err != nil {
		_ = os.Remove(local)
		return false, err
	}
	return true, nil
}

func (a *App) deleteCloudAgentWorkbenchEntry(instanceID, relativePath string) error {
	if strings.TrimSpace(relativePath) == "" || codingWorkbenchEntryProtected(relativePath) {
		return fmt.Errorf("该路径不允许删除")
	}
	if _, err := a.cloudAgentOp(instanceID, "cloud_delete_file", map[string]any{"path": relativePath}); err != nil {
		return err
	}
	a.emitEvent(cloudAgentFilesChangedEvent, map[string]string{"instance_id": instanceID})
	return nil
}

// DownloadCloudAgentWorkspace packs the whole workspace of one instance and
// saves it as "<name>.tar.gz" — the context-menu "打包下载" entry.
func (a *App) DownloadCloudAgentWorkspace(instanceID, displayName string) (string, error) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return "", fmt.Errorf("缺少云端任务")
	}
	return a.downloadCloudAgentWorkbenchEntry(instanceID, "/", displayName)
}

// CloudAgentUploadResult reports one upload batch picked in the dialog.
type CloudAgentUploadResult struct {
	Files int   `json:"files"`
	Dirs  int   `json:"dirs"`
	Bytes int64 `json:"bytes"`
}

// UploadFilesToCloudAgent copies picked files into the workspace root.
func (a *App) UploadFilesToCloudAgent(instanceID string) (CloudAgentUploadResult, error) {
	result := CloudAgentUploadResult{}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return result, fmt.Errorf("缺少云端任务")
	}
	picks, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "上传文件到云端任务",
	})
	if err != nil || len(picks) == 0 {
		return result, err
	}
	err = a.uploadCloudAgentPicks(instanceID, picks, &result)
	// Partial progress landed too — an open files dialog must re-list either way.
	if result.Files+result.Dirs > 0 {
		a.emitEvent(cloudAgentFilesChangedEvent, map[string]string{"instance_id": instanceID})
	}
	return result, err
}

// UploadDirectoryToCloudAgent copies a picked directory under the workspace
// root as "<dirname>/", preserving its tree (empty directories included).
func (a *App) UploadDirectoryToCloudAgent(instanceID string) (CloudAgentUploadResult, error) {
	result := CloudAgentUploadResult{}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return result, fmt.Errorf("缺少云端任务")
	}
	pick, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "上传文件夹到云端任务",
	})
	if err != nil || strings.TrimSpace(pick) == "" {
		return result, err
	}
	cleaned := filepath.Clean(pick)
	base := filepath.Base(cleaned)
	// A filesystem root has no usable name ("C:" or "\") and uploading a whole
	// drive is never the intent; the file/dir caps would only trip after
	// thousands of wasted ops.
	isRoot := filepath.Dir(cleaned) == cleaned
	if base == "" || base == "." || base == ".." || isRoot || strings.ContainsAny(base, `/\`) || codingWorkbenchEntryProtected(filepath.ToSlash(base)) {
		return result, fmt.Errorf("这个目录不能上传")
	}
	if err := a.mkdirCloudAgent(instanceID, base); err != nil {
		return result, err
	}
	result.Dirs++
	err = a.uploadCloudAgentDir(instanceID, cleaned, base, &result)
	if result.Files+result.Dirs > 0 {
		a.emitEvent(cloudAgentFilesChangedEvent, map[string]string{"instance_id": instanceID})
	}
	return result, err
}

func (a *App) uploadCloudAgentPicks(instanceID string, picks []string, result *CloudAgentUploadResult) error {
	for _, pick := range picks {
		info, err := os.Stat(pick)
		if err != nil {
			return fmt.Errorf("无法读取 %s", filepath.Base(pick))
		}
		if info.IsDir() {
			return fmt.Errorf("%s 是目录，请用上传文件夹", filepath.Base(pick))
		}
		if !info.Mode().IsRegular() {
			continue
		}
		target := filepath.Base(pick)
		if codingWorkbenchEntryProtected(filepath.ToSlash(target)) {
			return fmt.Errorf("%s 是保留路径，不能上传", target)
		}
		if err := a.uploadCloudAgentFile(instanceID, pick, target, result); err != nil {
			return err
		}
	}
	return nil
}

// uploadCloudAgentDir walks src and uploads every regular file under relBase.
// Symlinks and special files are skipped — the channel has no way to carry them.
func (a *App) uploadCloudAgentDir(instanceID, src, relBase string, result *CloudAgentUploadResult) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == src {
			return nil
		}
		if d.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.ToSlash(filepath.Join(relBase, rel))
		if codingWorkbenchEntryProtected(target) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if err := a.mkdirCloudAgent(instanceID, target); err != nil {
				return err
			}
			result.Dirs++
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return a.uploadCloudAgentFile(instanceID, path, target, result)
	})
}

func (a *App) uploadCloudAgentFile(instanceID, localPath, relTarget string, result *CloudAgentUploadResult) error {
	if result.Files >= cloudAgentUploadMaxFiles || result.Bytes >= cloudAgentUploadMaxBytes {
		return fmt.Errorf("上传内容太大，已上传 %d 个文件后停止", result.Files)
	}
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("无法读取 %s", filepath.Base(localPath))
	}
	defer file.Close()
	if info, statErr := file.Stat(); statErr == nil && info.Size()+result.Bytes > cloudAgentUploadMaxBytes {
		return fmt.Errorf("上传内容太大，已上传 %d 个文件后停止", result.Files)
	}
	buf := make([]byte, cloudAgentUploadChunkBytes)
	offset := int64(0)
	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			// A file that keeps growing while it is read must not stream past
			// the batch budget — the upfront size check only saw the first stat.
			if result.Bytes+offset+int64(n) > cloudAgentUploadMaxBytes {
				return fmt.Errorf("上传内容太大，已上传 %d 个文件后停止", result.Files)
			}
			if err := a.writeCloudAgentChunk(instanceID, relTarget, buf[:n], offset); err != nil {
				return fmt.Errorf("上传 %s 失败：%w", filepath.Base(localPath), err)
			}
			offset += int64(n)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("无法读取 %s", filepath.Base(localPath))
		}
	}
	if offset == 0 {
		// An empty file still needs its create op or it never exists remotely.
		if err := a.writeCloudAgentChunk(instanceID, relTarget, nil, 0); err != nil {
			return fmt.Errorf("上传 %s 失败：%w", filepath.Base(localPath), err)
		}
	}
	result.Files++
	result.Bytes += offset
	return nil
}

func (a *App) writeCloudAgentChunk(instanceID, relTarget string, data []byte, offset int64) error {
	_, err := a.cloudAgentOp(instanceID, "cloud_write_file", map[string]any{
		"path":        relTarget,
		"data_base64": base64.StdEncoding.EncodeToString(data),
		"offset":      offset,
	})
	return err
}

func (a *App) mkdirCloudAgent(instanceID, rel string) error {
	_, err := a.cloudAgentOp(instanceID, "cloud_mkdir", map[string]any{"path": rel})
	return err
}
