package cloudagent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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

// browsePack limits bound a directory download: the archive is a gzipped
// tar travelling inside the same channel as cloud_read_file, so its output
// is capped at the same 4MB — counted on the compressed stream, not the
// summed file sizes.
const (
	browsePackMaxBytes = 4 << 20
	browsePackMaxFiles = 20000
)

// browseWriteChunkMaxBytes bounds one cloud_write_file payload. The op body
// crosses two 1MB request readers — desktop→hub /op and hub→srv control —
// so 640KiB of raw bytes (~873KiB base64) stays comfortably under both.
// Larger files upload as a sequence of chunked writes from the desktop.
const browseWriteChunkMaxBytes = 640 << 10

var (
	errBrowsePackLimit   = errors.New("目录太大，暂不支持打包下载")
	errBrowseWriteOffset = errors.New("写入位置与文件大小不一致，请重新上传")
	errBrowseWriteTooBig = errors.New("单次写入的数据太大")
)

// browseJail resolves the instance jail root for file-browser ops. A coding
// instance without a checkout browses the bare workspace — the same root a
// repo-less turn writes into.
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
	if inst != nil {
		inst.jailMu.Lock()
		defer inst.jailMu.Unlock()
	}
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
	if inst != nil {
		inst.jailMu.Lock()
		defer inst.jailMu.Unlock()
	}
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
	// must not leak an unrelated target. A miss inside the checkout can
	// still be a file written beside it before the jail root moved.
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		if alt, altInfo, ok := strandedRegular(inst.dir, root, canonicalArtifactRel(root, path)); ok {
			path, info = alt, altInfo
		} else if err != nil || !info.IsDir() {
			return nil, errors.New("这个文件已经不在了")
		} else {
			return nil, errors.New("这是一个目录，不能按文件读取")
		}
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
	if inst != nil {
		inst.jailMu.Lock()
		defer inst.jailMu.Unlock()
	}
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
	var root string
	var err error
	end := inst.beginJailWork(func() {
		root, err = rt.browseJail(inst)
	})
	defer end()
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
	var root string
	var err error
	// Packing walks the tree outside the lock. Counting it in flight keeps
	// ConnectGit from renaming that tree until the archive is finished.
	end := inst.beginJailWork(func() {
		root, err = rt.browseJail(inst)
	})
	defer end()
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
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := 0
	seen := map[string]bool{}
	packTree := func(nameRoot, walkRoot string, skipDir string) error {
		return filepath.WalkDir(walkRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if skipDir != "" && filepath.Clean(path) == skipDir {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if path != walkRoot && browseHidden(d.Name()) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			entry, err := d.Info()
			if err != nil || (!entry.IsDir() && !entry.Mode().IsRegular()) {
				return nil
			}
			name, err := filepath.Rel(nameRoot, path)
			if err != nil || name == "." {
				return nil
			}
			name = filepath.ToSlash(name)
			if !entry.IsDir() && seen[name] {
				return nil
			}
			if entry.IsDir() {
				if seen[name] {
					return nil
				}
				seen[name] = true
				return tw.WriteHeader(&tar.Header{Name: name + "/", Mode: int64(entry.Mode().Perm()), ModTime: entry.ModTime(), Typeflag: tar.TypeDir})
			}
			seen[name] = true
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
			if copyErr != nil {
				// Mid-pack size changes surface raw archive/tar errors — translate.
				return errors.New("打包时文件发生变化，请重试")
			}
			// Flush both writers so buf.Len() reflects the real compressed size —
			// the cap must trip on what actually travels, not the uncompressed sum.
			if err := tw.Flush(); err != nil {
				return err
			}
			if err := gz.Flush(); err != nil {
				return err
			}
			if int64(buf.Len()) > browsePackMaxBytes {
				return errBrowsePackLimit
			}
			return nil
		})
	}
	walkErr := packTree(target, target, "")
	// Packing the checkout root also takes files a turn wrote beside it
	// while the jail was still the bare workspace. Names stay relative to
	// the checkout, which is how the book recorded them.
	if walkErr == nil && filepath.Clean(target) == filepath.Clean(root) {
		if workspace, ok := looseWorkspace(inst.dir, root); ok {
			walkErr = packTree(workspace, workspace, filepath.Clean(root))
		}
	}
	if walkErr != nil {
		_ = tw.Close()
		_ = gz.Close()
		return nil, walkErr
	}
	if err := tw.Close(); err != nil {
		_ = gz.Close()
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	if int64(buf.Len()) > browsePackMaxBytes {
		return nil, errBrowsePackLimit
	}
	return json.Marshal(map[string]any{
		"name":        info.Name() + ".tar.gz",
		"format":      "tar.gz",
		"size":        int64(buf.Len()),
		"data_base64": base64.StdEncoding.EncodeToString(buf.Bytes()),
	})
}

// browseWriteFile appends one chunk to a file inside the jail. offset == 0
// creates or truncates; a larger offset must match the current size so a
// retried chunk cannot silently corrupt the file — the desktop resends from
// scratch on mismatch. Missing parents are created inside the jail.
func (rt *Runtime) browseWriteFile(inst *instance, rel, dataBase64 string, offset int64) (json.RawMessage, error) {
	var root string
	var err error
	end := inst.beginJailWork(func() {
		root, err = rt.browseJail(inst)
	})
	defer end()
	if err != nil {
		return nil, err
	}
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "/" {
		return nil, errors.New("缺少文件路径")
	}
	if offset < 0 {
		return nil, errBrowseWriteOffset
	}
	data, err := base64.StdEncoding.DecodeString(dataBase64)
	if err != nil {
		return nil, errors.New("上传数据无法解析")
	}
	if len(data) > browseWriteChunkMaxBytes {
		return nil, errBrowseWriteTooBig
	}
	path, err := resolveInJail(root, rel)
	if err != nil {
		return nil, err
	}
	if info, statErr := os.Lstat(path); statErr == nil {
		if info.IsDir() {
			return nil, errors.New("这是一个目录，不能按文件写入")
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("该路径不能写入")
		}
		if offset > 0 && info.Size() != offset {
			return nil, errBrowseWriteOffset
		}
	} else if offset > 0 {
		return nil, errBrowseWriteOffset
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, errors.New("目录创建失败")
	}
	flags := os.O_WRONLY | os.O_CREATE
	if offset == 0 {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return nil, errors.New("这个文件暂时写入失败")
	}
	if offset > 0 {
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			file.Close()
			return nil, errBrowseWriteOffset
		}
	}
	written, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return nil, errors.New("这个文件暂时写入失败")
	}
	if closeErr != nil {
		return nil, errors.New("这个文件暂时写入失败")
	}
	return json.Marshal(map[string]any{
		"size":    offset + int64(written),
		"written": written,
	})
}

// browseMkdir creates a directory (and any missing parents) inside the jail.
func (rt *Runtime) browseMkdir(inst *instance, rel string) (json.RawMessage, error) {
	var root string
	var err error
	end := inst.beginJailWork(func() {
		root, err = rt.browseJail(inst)
	})
	defer end()
	if err != nil {
		return nil, err
	}
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "/" {
		return nil, errors.New("缺少目录路径")
	}
	path, err := resolveInJail(root, rel)
	if err != nil {
		return nil, err
	}
	if info, statErr := os.Lstat(path); statErr == nil && !info.IsDir() {
		return nil, errors.New("同名文件已存在")
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, errors.New("目录创建失败")
	}
	return json.Marshal(map[string]any{"ok": true})
}
