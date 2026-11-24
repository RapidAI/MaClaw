package cloudagent

import (
	"archive/tar"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// browseListLimit matches the desktop coding-workbench listing cap so a
// cloud tree behaves identically to a local one in the same dialog.
const browseListLimit = 500

// browseReadMaxBytes caps one cloud_read_file payload. The bytes travel to
// the desktop base64'd inside the remote-control exec result, whose reader
// stops at 8MB — the same 4MB raw convention as remoteControlFetchFileMaxBytes
// keeps every response comfortably under the ceiling. Previews ask for fewer
// bytes through max_bytes; a download may take the full cap.
const browseReadMaxBytes = 4 << 20

// browsePack limits bound a directory download: the tar travels inside the
// same channel as cloud_read_file, so its output is capped at the same 4MB
// (counted on the encoded tar, not the summed file sizes — per-file headers
// and padding inflate the archive well past the payload for many small files).
const (
	browsePackMaxBytes = 4 << 20
	browsePackMaxFiles = 20000
)

var errBrowsePackLimit = errors.New("目录太大，暂不支持打包下载")

// browseJail resolves the instance jail root for file-browser ops. A coding
// instance without a checkout has no browsable tree — the error reaches the
// dialog instead of a fake empty listing.
func (rt *Runtime) browseJail(inst *instance) (string, error) {
	return jailRoot(inst.dir, inst.meta.Kind)
}

// browseHidden mirrors the desktop workbench listing: dot-prefixed names
// and the instance secret files never appear in listings or archive packs.
func browseHidden(name string) bool {
	name = strings.TrimSpace(name)
	return name == "" || strings.HasPrefix(name, ".") || secretBase(name)
}

// browseRel returns the slash-separated path clients pass back in later
// ops. The jail root itself reports "/" so it is still addressable.
func browseRel(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return ""
	}
	if rel == "." {
		return "/"
	}
	return filepath.ToSlash(rel)
}

func browseMode(info fs.FileInfo) string {
	return fmt.Sprintf("%04o", uint32(info.Mode().Perm()))
}

// browseTarget resolves rel inside the jail, where "" and "/" address the
// jail root itself (resolveInJail refuses both). Anything else must round-
// trip through resolveInJail so symlink escapes stay impossible.
func browseTarget(root, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "/" {
		return filepath.Clean(root), nil
	}
	return resolveInJail(root, rel)
}

func (rt *Runtime) browseListDir(inst *instance, rel string) (json.RawMessage, error) {
	root, err := rt.browseJail(inst)
	if err != nil {
		return nil, err
	}
	target, err := browseTarget(root, rel)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, errors.New("没有找到这个目录")
	}
	type row struct {
		Name       string `json:"name"`
		Path       string `json:"path"`
		IsDir      bool   `json:"is_dir"`
		Size       int64  `json:"size"`
		ModifiedAt int64  `json:"modified_at"`
		Mode       string `json:"mode"`
	}
	rows := make([]row, 0, len(entries))
	truncated := false
	for _, entry := range entries {
		if len(rows) >= browseListLimit {
			truncated = true
			break
		}
		if browseHidden(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(target, entry.Name())
		rows = append(rows, row{
			Name:       entry.Name(),
			Path:       browseRel(root, path),
			IsDir:      entry.IsDir(),
			Size:       info.Size(),
			ModifiedAt: info.ModTime().Unix(),
			Mode:       browseMode(info),
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].IsDir != rows[j].IsDir {
			return rows[i].IsDir
		}
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})
	return json.Marshal(map[string]any{
		"path":      browseRel(root, target),
		"entries":   rows,
		"truncated": truncated,
	})
}

func (rt *Runtime) browseReadFile(inst *instance, rel string, maxBytes int64) (json.RawMessage, error) {
	root, err := rt.browseJail(inst)
	if err != nil {
		return nil, err
	}
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "/" {
		return nil, errors.New("缺少文件路径")
	}
	path, err := resolveInJail(root, rel)
	if err != nil {
		return nil, err
	}
	// Lstat like serveFile: a path swapped for a symlink or special file
	// must not leak an unrelated target.
	info, err := os.Lstat(path)
	if err != nil {
		return nil, errors.New("这个文件已经不在了")
	}
	if info.IsDir() {
		return nil, errors.New("这是一个目录，不能按文件读取")
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("这个文件已经不在了")
	}
	limit := maxBytes
	if limit <= 0 || limit > browseReadMaxBytes {
		limit = browseReadMaxBytes
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("这个文件暂时读取失败")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, limit))
	if err != nil {
		return nil, errors.New("这个文件暂时读取失败")
	}
	return json.Marshal(map[string]any{
		"name":        info.Name(),
		"size":        info.Size(),
		"truncated":   info.Size() > limit,
		"data_base64": base64.StdEncoding.EncodeToString(raw),
	})
}

func (rt *Runtime) browseStat(inst *instance, rel string) (json.RawMessage, error) {
	root, err := rt.browseJail(inst)
	if err != nil {
		return nil, err
	}
	target, err := browseTarget(root, rel)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(target)
	if err != nil {
		return nil, errors.New("没有找到这个路径")
	}
	name := info.Name()
	relPath := browseRel(root, target)
	if relPath == "/" {
		name = "workspace"
	}
	return json.Marshal(map[string]any{
		"name":        name,
		"path":        relPath,
		"is_dir":      info.IsDir(),
		"size":        info.Size(),
		"size_known":  !info.IsDir(),
		"modified_at": info.ModTime().Unix(),
		"mode":        browseMode(info),
	})
}

func (rt *Runtime) browseDelete(inst *instance, rel string) (json.RawMessage, error) {
	root, err := rt.browseJail(inst)
	if err != nil {
		return nil, err
	}
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "/" {
		return nil, errors.New("不能删除整个工作区")
	}
	path, err := resolveInJail(root, rel)
	if err != nil {
		return nil, err
	}
	if filepath.Clean(path) == filepath.Clean(root) {
		return nil, errors.New("不能删除整个工作区")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, errors.New("没有找到这个路径")
	}
	if info.IsDir() {
		err = os.RemoveAll(path)
	} else {
		err = os.Remove(path)
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"ok": true})
}

func (rt *Runtime) browsePackDir(inst *instance, rel string) (json.RawMessage, error) {
	root, err := rt.browseJail(inst)
	if err != nil {
		return nil, err
	}
	target, err := browseTarget(root, rel)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		return nil, errors.New("没有找到这个目录")
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	files := 0
	walkErr := filepath.WalkDir(target, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if int64(buf.Len()) > browsePackMaxBytes {
			return errBrowsePackLimit
		}
		if path != target && browseHidden(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		entry, err := d.Info()
		if err != nil || (!entry.IsDir() && !entry.Mode().IsRegular()) {
			return nil
		}
		name, err := filepath.Rel(target, path)
		if err != nil || name == "." {
			return nil
		}
		name = filepath.ToSlash(name)
		if entry.IsDir() {
			return tw.WriteHeader(&tar.Header{Name: name + "/", Mode: int64(entry.Mode().Perm()), ModTime: entry.ModTime(), Typeflag: tar.TypeDir})
		}
		files++
		if files > browsePackMaxFiles {
			return errBrowsePackLimit
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: int64(entry.Mode().Perm()), Size: entry.Size(), ModTime: entry.ModTime(), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		_, copyErr := io.Copy(tw, file)
		file.Close()
		return copyErr
	})
	if walkErr != nil {
		_ = tw.Close()
		return nil, walkErr
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if int64(buf.Len()) > browsePackMaxBytes {
		return nil, errBrowsePackLimit
	}
	return json.Marshal(map[string]any{
		"name":        info.Name() + ".tar",
		"size":        int64(buf.Len()),
		"data_base64": base64.StdEncoding.EncodeToString(buf.Bytes()),
	})
}
