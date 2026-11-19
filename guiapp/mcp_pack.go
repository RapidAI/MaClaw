package guiapp

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// mcpPackBudgetBytes is the uncompressed payload cap for one archive.
// The download cap stays the same; tests shrink this so they do not write 16MiB.
var mcpPackBudgetBytes int64 = mcpArtifactMaxDownload

var errMCPPackTooLarge = errors.New("打包结果超过下载上限")

type mcpPackFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type mcpPackOmission struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

type mcpPackResult struct {
	ProjectPath   string            `json:"project_path"`
	Files         []mcpPackFile     `json:"files"`
	ArchiveSHA256 string            `json:"archive_sha256"`
	ArchiveSize   int64             `json:"archive_size"`
	Encoding      string            `json:"encoding,omitempty"`
	Content       string            `json:"content,omitempty"`
	Omitted       []mcpPackOmission `json:"omitted,omitempty"`
}

func (a *App) mcpPackArtifacts(ctx context.Context, projectPath string, paths []string) (mcpPackResult, error) {
	_ = ctx
	if len(paths) == 0 {
		return mcpPackResult{}, errors.New("要指定要打包的文件")
	}
	root, view, err := a.mcpArtifactRoot(projectPath)
	if err != nil {
		return mcpPackResult{}, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	seen := map[string]struct{}{}
	files := make([]mcpPackFile, 0, len(paths))
	var omitted []mcpPackOmission
	var used int64
	sensitive := mcpSensitiveInfos()
	for _, raw := range paths {
		if len(files) >= mcpArtifactMaxFiles {
			omitted = append(omitted, mcpPackOmission{Path: raw, Error: "文件太多"})
			continue
		}
		rel := strings.TrimSpace(raw)
		cleaned, packErr := mcpPackOne(root, rel, sensitive, seen, mcpPackBudgetBytes-used)
		if errors.Is(packErr, errMCPPackTooLarge) {
			return mcpPackResult{}, errMCPPackTooLarge
		}
		if packErr != nil {
			omitted = append(omitted, mcpPackOmission{Path: filepath.ToSlash(strings.TrimSpace(raw)), Error: packErr.Error()})
			continue
		}
		writer, createErr := zw.Create(cleaned.Path)
		if createErr != nil {
			omitted = append(omitted, mcpPackOmission{Path: cleaned.Path, Error: "打不成这个文件"})
			continue
		}
		if _, copyErr := writer.Write(cleaned.body); copyErr != nil {
			omitted = append(omitted, mcpPackOmission{Path: cleaned.Path, Error: "打不成这个文件"})
			continue
		}
		files = append(files, mcpPackFile{Path: cleaned.Path, Size: cleaned.size, SHA256: cleaned.sum})
		used += cleaned.size
	}
	if err := zw.Close(); err != nil {
		return mcpPackResult{}, errors.New("打不成压缩包")
	}
	if len(files) == 0 {
		if len(omitted) == 0 {
			return mcpPackResult{}, errors.New("要指定要打包的文件")
		}
		return mcpPackResult{}, errors.New(omitted[0].Error)
	}
	raw := buf.Bytes()
	if int64(len(raw)) > mcpArtifactMaxDownload {
		return mcpPackResult{}, errMCPPackTooLarge
	}
	sum := sha256.Sum256(raw)
	out := mcpPackResult{
		ProjectPath:   view.ProjectPath,
		Files:         files,
		ArchiveSHA256: hex.EncodeToString(sum[:]),
		ArchiveSize:   int64(len(raw)),
		Encoding:      "base64",
		Content:       base64.StdEncoding.EncodeToString(raw),
		Omitted:       omitted,
	}
	return out, nil
}

type mcpPackedFile struct {
	Path string
	body []byte
	size int64
	sum  string
}

func mcpPackOne(root, rel string, sensitive []os.FileInfo, seen map[string]struct{}, room int64) (mcpPackedFile, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return mcpPackedFile{}, errors.New("要指定要打包的文件")
	}
	if mcpReservedDevice(filepath.Base(rel)) {
		return mcpPackedFile{}, errors.New("只能读取任务工作目录里的文件")
	}
	if mcpArtifactSecretName(filepath.Base(rel)) {
		return mcpPackedFile{}, errors.New("不能读取密钥文件")
	}
	full, err := mcpResolveInside(root, rel)
	if err != nil {
		return mcpPackedFile{}, err
	}
	info, err := os.Lstat(full)
	if err != nil {
		return mcpPackedFile{}, errors.New("工作目录里没有这个文件")
	}
	slash := filepath.ToSlash(mcpRelativeInside(root, full))
	if _, ok := seen[slash]; ok {
		return mcpPackedFile{}, errors.New("这个文件已经列入清单")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		if info.IsDir() {
			return mcpPackedFile{}, errors.New("这是目录")
		}
		return mcpPackedFile{}, errors.New("不能读取链接")
	}
	if info.IsDir() {
		return mcpPackedFile{}, errors.New("这是目录")
	}
	if mcpArtifactSecretName(filepath.Base(full)) || mcpInfoIsSensitive(info, sensitive) {
		return mcpPackedFile{}, errors.New("不能读取密钥文件")
	}
	if info.Size() > mcpArtifactMaxDownload {
		return mcpPackedFile{}, errors.New("文件超过读取上限")
	}
	if info.Size() > room {
		return mcpPackedFile{}, errMCPPackTooLarge
	}
	f, err := os.Open(full)
	if err != nil {
		return mcpPackedFile{}, errors.New("读不到这个文件")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || mcpInfoIsSensitive(opened, sensitive) {
		return mcpPackedFile{}, errors.New("读不到这个文件")
	}
	body, err := io.ReadAll(io.LimitReader(f, room+1))
	if err != nil {
		return mcpPackedFile{}, errors.New("读不到这个文件")
	}
	if int64(len(body)) > mcpArtifactMaxDownload {
		return mcpPackedFile{}, errors.New("文件超过读取上限")
	}
	if int64(len(body)) > room {
		return mcpPackedFile{}, errMCPPackTooLarge
	}
	sum := sha256.Sum256(body)
	seen[slash] = struct{}{}
	return mcpPackedFile{Path: slash, body: body, size: int64(len(body)), sum: hex.EncodeToString(sum[:])}, nil
}
