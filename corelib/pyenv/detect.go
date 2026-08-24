// Package pyenv 提供 Python 环境检测、自动安装和 uv 虚拟环境管理。
// 安装目录为 ~/.maclaw/python/，使用 python-build-standalone 提供私有 Python，
// 通过 uv 创建和管理虚拟环境。
package pyenv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/archiveutil"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// UseChinaMirror 控制是否使用国内镜像下载 Python/uv。
// 由宿主程序在启动时根据用户语言设置调用 SetUseChinaMirror。
var useChinaMirror atomic.Bool

var errDownloadIncomplete = errors.New("download incomplete")

// EnsureEnvironment mutates one shared private runtime tree. Serialize callers
// from GUI startup, CLI checks, and tool setup so their archive/rename steps
// cannot race each other.
var ensureEnvironmentMu sync.Mutex

// SetUseChinaMirror 设置是否使用国内镜像。可在任意时机调用，立即生效。
func SetUseChinaMirror(use bool) {
	useChinaMirror.Store(use)
}

// MinPythonMajor 最低 Python 主版本。
const MinPythonMajor = 3

// MinPythonMinor 最低 Python 次版本。
const MinPythonMinor = 10

// Status 表示 Python 环境的状态。
type Status struct {
	Available   bool   `json:"available"`     // Python 是否可用
	PythonPath  string `json:"python_path"`   // python 可执行文件路径
	Version     string `json:"version"`       // 版本字符串，如 "3.12.3"
	UVAvailable bool   `json:"uv_available"`  // uv 是否可用
	UVPath      string `json:"uv_path"`       // uv 可执行文件路径
	UVIsPrivate bool   `json:"uv_is_private"` // 是否为 maclaw 私有 uv
	VenvPath    string `json:"venv_path"`     // 虚拟环境路径
	VenvReady   bool   `json:"venv_ready"`    // 虚拟环境是否就绪
	IsPrivate   bool   `json:"is_private"`    // 是否为 maclaw 私有安装
	Error       string `json:"error"`         // 错误信息
}

// ProgressFunc 安装进度回调。
type ProgressFunc func(stage string, pct int, msg string)

// baseDir returns the private Python installation directory under the active base dir.
func baseDir() (string, error) {
	return filepath.Join(corelib.MaclawBaseDir(), "python"), nil
}

// VenvDir returns the private Python virtual environment directory.
func VenvDir() (string, error) {
	base, err := baseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "venv"), nil
}

// privatePythonPath 返回私有 Python 可执行文件的预期路径。
func privatePythonPath() (string, error) {
	base, err := baseDir()
	if err != nil {
		return "", err
	}
	return pythonPathInInstallDir(filepath.Join(base, "install")), nil
}

func pythonPathInInstallDir(installDir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(installDir, "python.exe")
	}
	return filepath.Join(installDir, "bin", "python3")
}

// privateUVPath 返回私有 uv 可执行文件的预期路径。
func privateUVPath() (string, error) {
	base, err := baseDir()
	if err != nil {
		return "", err
	}
	name := "uv"
	if runtime.GOOS == "windows" {
		name = "uv.exe"
	}
	return filepath.Join(base, "bin", name), nil
}

// parseVersion 从 "Python 3.12.3" 格式中提取版本号。
var versionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)
var http4xxRe = regexp.MustCompile(`http 4\d\d`)

func parseVersion(output string) (major, minor, patch int, raw string, ok bool) {
	m := versionRe.FindStringSubmatch(output)
	if len(m) < 4 {
		return 0, 0, 0, "", false
	}
	major, _ = strconv.Atoi(m[1])
	minor, _ = strconv.Atoi(m[2])
	patch, _ = strconv.Atoi(m[3])
	return major, minor, patch, m[0], true
}

// meetsMinVersion 检查版本是否满足最低要求。
func meetsMinVersion(major, minor int) bool {
	if major > MinPythonMajor {
		return true
	}
	return major == MinPythonMajor && minor >= MinPythonMinor
}

// checkPython 检查指定路径的 Python 是否可用且版本满足要求。
// 设置 10 秒超时防止进程挂住。
func checkPython(pythonPath string) (version string, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := coretool.CommandContext(ctx, pythonPath, "--version")
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	major, minor, _, raw, parsed := parseVersion(string(out))
	if !parsed {
		return "", false
	}
	if !meetsMinVersion(major, minor) {
		return raw, false
	}
	return raw, true
}

// Detect 检测当前系统的 Python 环境状态。
// 优先检查私有安装，然后检查系统 PATH。
func Detect() Status {
	var st Status

	// 1. 检查私有安装
	pp, err := privatePythonPath()
	if err == nil {
		if ver, ok := checkPython(pp); ok {
			st.Available = true
			st.PythonPath = pp
			st.Version = ver
			st.IsPrivate = true
		}
	}

	// 2. 如果私有安装不可用，检查系统 Python
	if !st.Available {
		for _, name := range []string{"python3", "python"} {
			if p, err := exec.LookPath(name); err == nil {
				if ver, ok := checkPython(p); ok {
					st.Available = true
					st.PythonPath = p
					st.Version = ver
					st.IsPrivate = false
					break
				}
			}
		}
	}

	// 3. 检查 uv
	uvp, err := privateUVPath()
	if err == nil {
		if _, serr := os.Stat(uvp); serr == nil {
			st.UVAvailable = true
			st.UVPath = uvp
			st.UVIsPrivate = true
		}
	}
	if !st.UVAvailable {
		if p, err := exec.LookPath("uv"); err == nil {
			st.UVAvailable = true
			st.UVPath = p
		}
	}

	// 4. 检查 venv
	venvDir, err := VenvDir()
	if err == nil {
		venvPy := venvPythonPath(venvDir)
		if _, serr := os.Stat(venvPy); serr == nil {
			st.VenvReady = true
			st.VenvPath = venvDir
		}
	}

	return st
}

// venvPythonPath 返回 venv 中 python 可执行文件路径。
func venvPythonPath(venvDir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(venvDir, "Scripts", "python.exe")
	}
	return filepath.Join(venvDir, "bin", "python3")
}

// VenvPython 返回虚拟环境中的 Python 路径（供外部使用）。
func VenvPython() (string, error) {
	venvDir, err := VenvDir()
	if err != nil {
		return "", err
	}
	p := venvPythonPath(venvDir)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("venv python 不存在: %s", p)
	}
	return p, nil
}

// standalonePythonURL 返回 python-build-standalone 的下载 URL 列表（优先级从高到低）。
func standalonePythonURLs() ([]string, error) {
	arch, err := resolveArch()
	if err != nil {
		return nil, err
	}

	var filename string
	switch runtime.GOOS {
	case "windows":
		filename = fmt.Sprintf("cpython-3.12.8+20250106-%s-pc-windows-msvc-install_only_stripped.tar.gz", arch)
	case "darwin":
		filename = fmt.Sprintf("cpython-3.12.8+20250106-%s-apple-darwin-install_only_stripped.tar.gz", arch)
	case "linux":
		filename = fmt.Sprintf("cpython-3.12.8+20250106-%s-unknown-linux-gnu-install_only_stripped.tar.gz", arch)
	default:
		return nil, fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
	}

	const githubBase = "https://github.com/astral-sh/python-build-standalone/releases/download/20250106"
	// ghproxy.net 是可透传 GitHub Release 的镜像。python-build-standalone
	// 并不在 npmmirror/cnb 的对应路径上，不能把它们作为下载源，否则会
	// 无谓地得到 404 后才回退到 GitHub。
	const ghproxyBase = "https://ghproxy.net/https://github.com/astral-sh/python-build-standalone/releases/download/20250106"

	if useChinaMirror.Load() {
		return []string{
			ghproxyBase + "/" + filename,
			githubBase + "/" + filename,
		}, nil
	}
	return []string{
		githubBase + "/" + filename,
		ghproxyBase + "/" + filename,
	}, nil
}

// standalonePythonURL 返回单个 URL（兼容旧调用，优先级最高的源）。
func standalonePythonURL() (string, error) {
	urls, err := standalonePythonURLs()
	if err != nil {
		return "", err
	}
	return urls[0], nil
}

// uvInstallURL 返回 uv 的下载 URL 列表（优先级从高到低）。
func uvInstallURLs() ([]string, error) {
	arch, err := resolveArch()
	if err != nil {
		return nil, err
	}

	var filename string
	switch runtime.GOOS {
	case "windows":
		filename = fmt.Sprintf("uv-%s-pc-windows-msvc.zip", arch)
	case "darwin":
		filename = fmt.Sprintf("uv-%s-apple-darwin.tar.gz", arch)
	case "linux":
		filename = fmt.Sprintf("uv-%s-unknown-linux-gnu.tar.gz", arch)
	default:
		return nil, fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
	}

	const githubBase = "https://github.com/astral-sh/uv/releases/latest/download"
	// astral.sh 官方 CDN（不走 GitHub 域名，对中国大陆更友好）
	const astralCDN = "https://releases.astral.sh/github/uv/releases/latest/download"
	// cnb.cool（码云 GitBoat）GitHub Release 镜像（中国大陆可达）
	const cnbBase = "https://cnb.cool/astral-sh/uv/-/releases/latest/download"
	// npmmirror CDN（阿里云中国节点，稳定性高）
	const npmmirrorBase = "https://cdn.npmmirror.com/binaries/uv/latest"
	// ghfast.top GitHub 加速代理（中国大陆可达）
	const ghfastBase = "https://ghfast.top/https://github.com/astral-sh/uv/releases/latest/download"

	if useChinaMirror.Load() {
		return []string{
			npmmirrorBase + "/" + filename,
			astralCDN + "/" + filename,
			ghfastBase + "/" + filename,
			cnbBase + "/" + filename,
			githubBase + "/" + filename,
		}, nil
	}
	return []string{
		githubBase + "/" + filename,
		astralCDN + "/" + filename,
		npmmirrorBase + "/" + filename,
	}, nil
}

// resolveArch 将 Go 架构名映射为下载 URL 中使用的架构名。
func resolveArch() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64", nil
	case "arm64":
		return "aarch64", nil
	default:
		return "", fmt.Errorf("不支持的架构: %s", runtime.GOARCH)
	}
}

// downloadToFile 下载 URL 到本地文件，带进度回调。
// 使用临时文件 + rename 保证原子性。
func downloadToFile(url, destPath string, emit ProgressFunc) error {
	return downloadWithFallback([]string{url}, destPath, emit)
}

// downloadWithFallback 依次尝试多个 URL 下载，支持断点续传。
// 当下载有进展（新数据写入）时允许更多重试，充分利用断点续传能力。
// 切换到下一个源时清理临时文件（不同 CDN 的 offset 不互通）。
// 全部失败后返回最后一个错误。
func downloadWithFallback(urls []string, destPath string, emit ProgressFunc) error {
	if len(urls) == 0 {
		return fmt.Errorf("无下载地址")
	}

	tmpPath := destPath + ".download"
	var lastErr error
	for i, url := range urls {
		const baseRetries = 2 // 零进展时的最大重试次数
		const maxRetries = 6  // 有进展时的最大重试次数（断点续传场景）
		retryBudget := baseRetries

		for attempt := 0; attempt <= retryBudget; attempt++ {
			if attempt > 0 {
				// 指数退避: 3s, 6s, 12s...（上限 30s）
				backoffSec := 3 * (1 << (attempt - 1))
				if backoffSec > 30 {
					backoffSec = 30
				}
				backoff := time.Duration(backoffSec) * time.Second
				if emit != nil {
					emit("retry", 0, fmt.Sprintf("下载失败，%ds 后重试 (%d/%d)...", backoffSec, attempt, retryBudget))
				}
				time.Sleep(backoff)
			}

			// 记录本次尝试前的临时文件大小
			var sizeBefore int64
			if fi, statErr := os.Stat(tmpPath); statErr == nil {
				sizeBefore = fi.Size()
			}

			err := downloadSingleURL(url, destPath, emit)
			if err == nil {
				return nil
			}
			lastErr = err

			// 不可恢复的错误（DNS/连接拒绝/4xx）：立即换源
			// 但仅当本次尝试无进展时才换源——有进展说明连接可达，中断是暂时性的
			var sizeAfter int64
			if fi, statErr := os.Stat(tmpPath); statErr == nil {
				sizeAfter = fi.Size()
			}
			madeProgress := sizeAfter > sizeBefore

			// 对普通连接中断保留断点续传；但服务端在声明 Content-Length 后
			// 提前结束响应时，临时文件已确定不完整，应立即改用下一个源。
			// 不完整下载使用哨兵错误标识，避免依赖面向用户的错误文案。
			fallback := shouldFallbackToNextSource(err) && !madeProgress
			if (fallback || isIncompleteDownloadError(err)) && i < len(urls)-1 {
				if emit != nil {
					emit("fallback", 0, fmt.Sprintf("源 %d 不可用，切换到备用源...", i+1))
				}
				break // 跳到下一个 URL
			}

			// 有进展——扩展重试预算（但不超过 maxRetries）
			if madeProgress {
				if retryBudget < maxRetries {
					retryBudget = maxRetries
				}
			}
		}

		// 切换到下一个源前清理临时文件——不同 CDN 的已下载部分不互通
		if i < len(urls)-1 {
			os.Remove(tmpPath)
		}
	}

	// 全部源失败：清理残留的临时文件，避免下次启动时对第一个源断点续传损坏的数据
	os.Remove(tmpPath)
	return lastErr
}

// shouldFallbackToNextSource 判断错误是否应该立即切换到下一个源（不再重试当前源）。
// 包括：网络不可达（连接拒绝/DNS/重置）、HTTP 4xx（路径错误/CDN 配置问题）。
// 不包括：i/o timeout（GFW 随机丢包，重试可能成功）。
//
// 注意：调用方（downloadWithFallback）在"本次尝试有进展"时不会调用此函数——
// 有进展说明连接可达且有数据流，中断是暂时性的，应该续传而非换源。
func shouldFallbackToNextSource(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())

	// 网络层不可达
	networkPatterns := []string{
		"connection refused",
		"connection reset",
		"no such host",
		"network is unreachable",
		"tls handshake timeout",
		"connectex:",
		"wsarecv:",
		"certificate",
		"host unreachable",
		"no route to host",
	}
	for _, pat := range networkPatterns {
		if strings.Contains(msg, pat) {
			return true
		}
	}

	// HTTP 4xx — 资源在此源上不存在或被拒绝，重试没有意义
	if http4xxRe.MatchString(msg) {
		return true
	}

	return false
}

func isIncompleteDownloadError(err error) bool {
	return errors.Is(err, errDownloadIncomplete)
}

// downloadSingleURL 执行单次 HTTP 下载，支持断点续传。
// 如果 destPath+".download" 临时文件已存在且服务器支持 Range 请求，则从断点继续。
func downloadSingleURL(url, destPath string, emit ProgressFunc) error {
	return downloadSingleURLInner(url, destPath, emit, false)
}

// downloadSingleURLInner 是 downloadSingleURL 的内部实现，retried 防止 416 递归。
func downloadSingleURLInner(url, destPath string, emit ProgressFunc, retried bool) error {
	// 连接超时 30 秒（覆盖中国大陆 GitHub 的慢连接场景），总超时 15 分钟
	transport := &http.Transport{
		ResponseHeaderTimeout: 30 * time.Second,
	}
	client := &http.Client{
		Timeout:   15 * time.Minute,
		Transport: transport,
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	tmpPath := destPath + ".download"

	// 检查是否有上次未完成的下载（断点续传）
	var existingSize int64
	if fi, err := os.Stat(tmpPath); err == nil {
		existingSize = fi.Size()
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("构建请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) MaClaw/1.0")

	// 断点续传：如果临时文件有数据且 >1KB（太小不值得续传），发送 Range 请求
	resuming := existingSize > 1024
	if resuming {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", existingSize))
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()

	var totalSize int64
	var startOffset int64

	switch resp.StatusCode {
	case http.StatusOK:
		// 服务器不支持 Range 或忽略了 Range 请求——从头下载
		totalSize = resp.ContentLength
		startOffset = 0
		// 清理旧临时文件（将被 truncate 重建）
		if existingSize > 0 {
			os.Remove(tmpPath)
		}
	case http.StatusPartialContent:
		// 服务器支持断点续传，返回了部分内容
		startOffset = existingSize
		// 从 Content-Range 头解析总大小和起始位置: "bytes 12345-99999/100000"
		serverStart, parsedTotal, ok := parseContentRange(resp.Header.Get("Content-Range"))
		if !ok || serverStart != existingSize {
			// 206 必须带有能确认偏移和总长度的 Content-Range。否则追加
			// 到已有临时文件会静默损坏归档，改为删除断点并从头重新请求。
			io.Copy(io.Discard, resp.Body)
			os.Remove(tmpPath)
			if retried {
				return fmt.Errorf("断点续传响应无效: 请求偏移 %d，Content-Range=%q (%s)", existingSize, resp.Header.Get("Content-Range"), url)
			}
			return downloadSingleURLInner(url, destPath, emit, true)
		}
		totalSize = parsedTotal
		if emit != nil {
			mb := float64(startOffset) / (1024 * 1024)
			emit("resuming", int(startOffset*100/max64(totalSize, startOffset+1)), fmt.Sprintf("断点续传，已有 %.1f MB", mb))
		}
	case http.StatusRequestedRangeNotSatisfiable:
		// 文件可能已经完整下载了，或者服务端文件变了
		io.Copy(io.Discard, resp.Body)
		os.Remove(tmpPath)
		if retried {
			return fmt.Errorf("下载失败: HTTP 416 Range Not Satisfiable (%s)", url)
		}
		// 重新从头下载（仅递归一次）
		return downloadSingleURLInner(url, destPath, emit, true)
	default:
		io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("下载失败: HTTP %d (%s)", resp.StatusCode, url)
	}

	// 打开临时文件：续传用 Append，全新下载用 Create
	var outFile *os.File
	if startOffset > 0 {
		outFile, err = os.OpenFile(tmpPath, os.O_WRONLY|os.O_APPEND, 0644)
	} else {
		outFile, err = os.Create(tmpPath)
	}
	if err != nil {
		return fmt.Errorf("创建/打开临时文件失败: %w", err)
	}

	written := startOffset
	buf := make([]byte, 64*1024)
	lastPct := -1

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := outFile.Write(buf[:n]); wErr != nil {
				outFile.Close()
				// 不删除临时文件——下次重试可断点续传
				return fmt.Errorf("写入失败: %w", wErr)
			}
			written += int64(n)
			if totalSize > 0 && emit != nil {
				pct := int(written * 100 / totalSize)
				if pct != lastPct {
					lastPct = pct
					mb := float64(written) / (1024 * 1024)
					totalMB := float64(totalSize) / (1024 * 1024)
					emit("downloading", pct, fmt.Sprintf("%.1f / %.1f MB (%d%%)", mb, totalMB, pct))
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			outFile.Close()
			// 不删除临时文件——保留已下载部分，下次断点续传
			if errors.Is(readErr, io.ErrUnexpectedEOF) {
				return fmt.Errorf("%w: 已下载 %d bytes，预期 %d bytes: %v", errDownloadIncomplete, written, totalSize, readErr)
			}
			return fmt.Errorf("下载中断 (已下载 %d bytes): %w", written, readErr)
		}
	}
	if err := outFile.Sync(); err != nil {
		outFile.Close()
		return fmt.Errorf("sync 失败: %w", err)
	}
	if err := outFile.Close(); err != nil {
		return fmt.Errorf("close 失败: %w", err)
	}
	if err := verifyDownloadSize(written, totalSize); err != nil {
		// 保留临时文件以便诊断；下载器在遇到该错误时会切换到下一源，
		// 并在切换前清理这份不完整的数据。
		return err
	}

	// 原子替换
	os.Remove(destPath)
	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename 失败: %w", err)
	}
	return nil
}

// parseContentRange validates a 206 Content-Range value such as
// "bytes 12345-99999/100000" and returns its start offset and total size.
func parseContentRange(value string) (start, total int64, ok bool) {
	const prefix = "bytes "
	if !strings.HasPrefix(value, prefix) {
		return 0, 0, false
	}
	parts := strings.Split(strings.TrimPrefix(value, prefix), "/")
	if len(parts) != 2 || parts[1] == "*" {
		return 0, 0, false
	}
	rangeParts := strings.Split(parts[0], "-")
	if len(rangeParts) != 2 {
		return 0, 0, false
	}
	start, startErr := strconv.ParseInt(rangeParts[0], 10, 64)
	end, endErr := strconv.ParseInt(rangeParts[1], 10, 64)
	total, totalErr := strconv.ParseInt(parts[1], 10, 64)
	if startErr != nil || endErr != nil || totalErr != nil || start < 0 || end < start || total <= end {
		return 0, 0, false
	}
	return start, total, true
}

func verifyDownloadSize(written, totalSize int64) error {
	// Content-Length 未知时不能据此断言；归档解压仍会校验文件完整性。
	if totalSize <= 0 || written == totalSize {
		return nil
	}
	return fmt.Errorf("%w: 已下载 %d bytes，预期 %d bytes", errDownloadIncomplete, written, totalSize)
}

// max64 returns the larger of a and b.
func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// extractTarGz 解压 tar.gz 到目标目录。
func extractTarGz(archivePath, destDir string) error {
	return extractRuntimeArchive(archivePath, destDir)
}

// extractZip 解压 zip 到目标目录（Windows uv 用）。
func extractZip(archivePath, destDir string) error {
	return extractRuntimeArchive(archivePath, destDir)
}

func extractRuntimeArchive(archivePath, destDir string) error {
	result := archiveutil.ExtractToDirectoryWithPolicy(archivePath, destDir, archiveutil.DefaultLimits(), archiveutil.ExtractionPolicy{AllowSymlinks: true})
	if result.OK {
		return nil
	}
	return fmt.Errorf("解压运行时归档失败 (%s): %s", result.Code, result.Message)
}

func resetRuntimeExtractDir(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return nil
}

// errFound 是 findAndMoveBinary 内部用于提前终止 Walk 的哨兵错误。
var errFound = fmt.Errorf("found")

// findAndMoveBinary 在目录树中查找指定二进制文件并复制到目标目录。
func findAndMoveBinary(searchDir, binName, destDir string) error {
	var found string
	if err := filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Name() == binName {
			found = path
			return errFound // 提前终止遍历
		}
		return nil
	}); err != nil && err != errFound {
		return fmt.Errorf("查找 %s 失败: %w", binName, err)
	}
	if found == "" {
		return fmt.Errorf("在 %s 中未找到 %s", searchDir, binName)
	}
	destPath := filepath.Join(destDir, binName)
	data, err := os.ReadFile(found)
	if err != nil {
		return fmt.Errorf("读取 %s 失败: %w", found, err)
	}
	if err := os.WriteFile(destPath, data, 0755); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", destPath, err)
	}
	return nil
}

// EnsureEnvironment 确保 Python + uv + venv 环境就绪。
// 如果缺少任何组件，会自动下载安装。
// emit 回调用于报告进度，可以为 nil。
// 返回最终的环境状态。
func EnsureEnvironment(emit ProgressFunc) Status {
	if emit == nil {
		emit = func(string, int, string) {}
	}
	ensureEnvironmentMu.Lock()
	defer ensureEnvironmentMu.Unlock()

	st := Detect()

	// 已经全部就绪
	if st.Available && st.IsPrivate && st.UVAvailable && st.UVIsPrivate && st.VenvReady {
		emit("done", 100, fmt.Sprintf("Python 环境就绪: %s (v%s)", st.PythonPath, st.Version))
		return st
	}

	base, err := baseDir()
	if err != nil {
		st.Error = err.Error()
		return st
	}

	// --- 步骤 1: 安装私有 Python ---
	// 即使系统 Python 可用，也优先补齐 app 私有 Python。桌面环境里的
	// WindowsApps/PATH Python 可能版本漂移，不能作为可复现 runtime 的基础。
	installedPrivatePython := false
	if !st.Available || !st.IsPrivate {
		emit("python", 0, "正在安装 Python 3.12 ...")
		pyURLs, err := standalonePythonURLs()
		if err != nil {
			st.Error = fmt.Sprintf("获取 Python 下载地址失败: %v", err)
			return st
		}

		archivePath := filepath.Join(base, "python-standalone.tar.gz")
		if err := downloadWithFallback(pyURLs, archivePath, func(stage string, pct int, msg string) {
			emit("python-download", pct, msg)
		}); err != nil {
			st.Error = fmt.Sprintf("下载 Python 失败: %v", err)
			return st
		}

		emit("python-extract", 50, "正在解压 Python ...")
		installDir := filepath.Join(base, "install")
		extractDir := filepath.Join(base, "python-extract")
		// 归档文件位于 base 中，不能直接解压到 base：安全解压器要求其
		// 目标目录为空。使用独立临时目录也能清除上次失败的残留输出。
		if err := resetRuntimeExtractDir(extractDir); err != nil {
			st.Error = fmt.Sprintf("清理 Python 临时目录失败: %v", err)
			return st
		}
		if err := extractTarGz(archivePath, extractDir); err != nil {
			os.RemoveAll(extractDir)
			os.Remove(archivePath)
			st.Error = fmt.Sprintf("解压 Python 失败: %v", err)
			return st
		}
		// python-build-standalone 解压后目录名为 "python"，重命名为 "install"
		extractedDir := filepath.Join(extractDir, "python")
		if _, serr := os.Stat(extractedDir); serr != nil {
			os.RemoveAll(extractDir)
			os.Remove(archivePath)
			st.Error = fmt.Sprintf("Python 解压后目录不存在: %v", serr)
			return st
		}
		// 在替换当前安装前验证新运行时，避免损坏或不兼容归档清空已存在的
		// Python 环境。后续的最终验证仍覆盖移动后的实际安装路径。
		if _, ok := checkPython(pythonPathInInstallDir(extractedDir)); !ok {
			os.RemoveAll(extractDir)
			os.Remove(archivePath)
			st.Error = "Python 解压后验证失败，请检查下载来源或重试"
			return st
		}
		if err := replacePrivatePythonInstall(installDir, extractedDir); err != nil {
			os.RemoveAll(extractDir)
			os.Remove(archivePath)
			st.Error = fmt.Sprintf("替换 Python 安装失败: %v", err)
			return st
		}
		os.RemoveAll(extractDir)
		os.Remove(archivePath)

		// 验证安装
		pp, _ := privatePythonPath()
		if ver, ok := checkPython(pp); ok {
			st.Available = true
			st.PythonPath = pp
			st.Version = ver
			st.IsPrivate = true
			installedPrivatePython = true
			st.VenvReady = false
			st.VenvPath = ""
			message := fmt.Sprintf("Python %s 安装完成", ver)
			if err := commitPrivatePythonInstall(installDir); err != nil {
				// 新运行时已通过最终验证；备份清理失败不应把成功安装
				// 误报为失败。保留 install.previous，供下一次安装时再清理。
				message += fmt.Sprintf("（旧安装备份待清理: %v）", err)
			}
			emit("python", 100, message)
		} else {
			st.Error = "Python 安装后验证失败，请检查网络或手动安装"
			if err := rollbackPrivatePythonInstall(installDir); err != nil {
				st.Error += fmt.Sprintf("；恢复旧安装失败: %v", err)
			}
			return st
		}
	}

	// --- 步骤 2: 安装 uv ---
	if !st.UVAvailable || !st.UVIsPrivate {
		emit("uv", 0, "正在安装 uv ...")
		uvURLs, err := uvInstallURLs()
		if err != nil {
			st.Error = fmt.Sprintf("获取 uv 下载地址失败: %v", err)
			return st
		}

		isZip := strings.HasSuffix(uvURLs[0], ".zip")
		ext := ".tar.gz"
		if isZip {
			ext = ".zip"
		}
		archivePath := filepath.Join(base, "uv-archive"+ext)
		if err := downloadWithFallback(uvURLs, archivePath, func(stage string, pct int, msg string) {
			emit("uv-download", pct, msg)
		}); err != nil {
			st.Error = fmt.Sprintf("下载 uv 失败: %v", err)
			return st
		}

		emit("uv-extract", 50, "正在解压 uv ...")
		uvExtractDir := filepath.Join(base, "uv-extract")
		if err := resetRuntimeExtractDir(uvExtractDir); err != nil {
			os.Remove(archivePath)
			st.Error = fmt.Sprintf("清理 uv 临时目录失败: %v", err)
			return st
		}
		if isZip {
			if err := extractZip(archivePath, uvExtractDir); err != nil {
				os.Remove(archivePath)
				st.Error = fmt.Sprintf("解压 uv 失败: %v", err)
				return st
			}
		} else {
			if err := extractTarGz(archivePath, uvExtractDir); err != nil {
				os.Remove(archivePath)
				st.Error = fmt.Sprintf("解压 uv 失败: %v", err)
				return st
			}
		}

		// 找到 uv 二进制并复制到 ~/.maclaw/python/bin/
		binDir := filepath.Join(base, "bin")
		if err := os.MkdirAll(binDir, 0755); err != nil {
			os.RemoveAll(uvExtractDir)
			os.Remove(archivePath)
			st.Error = fmt.Sprintf("创建 uv bin 目录失败: %v", err)
			return st
		}
		uvBinName := "uv"
		if runtime.GOOS == "windows" {
			uvBinName = "uv.exe"
		}
		if err := findAndMoveBinary(uvExtractDir, uvBinName, binDir); err != nil {
			os.RemoveAll(uvExtractDir)
			os.Remove(archivePath)
			st.Error = fmt.Sprintf("安装 uv 二进制失败: %v", err)
			return st
		}
		os.RemoveAll(uvExtractDir)
		os.Remove(archivePath)

		uvp, _ := privateUVPath()
		if _, serr := os.Stat(uvp); serr == nil {
			st.UVAvailable = true
			st.UVPath = uvp
			st.UVIsPrivate = true
			if runtime.GOOS != "windows" {
				os.Chmod(uvp, 0755)
			}
			emit("uv", 100, "uv 安装完成")
		} else {
			st.Error = "uv 安装后验证失败"
			return st
		}
	}

	// --- 步骤 3: 创建 venv ---
	if installedPrivatePython || !st.VenvReady {
		emit("venv", 0, "正在创建虚拟环境 ...")
		venvDir, err := VenvDir()
		if err != nil {
			st.Error = fmt.Sprintf("获取 venv 目录失败: %v", err)
			return st
		}
		os.RemoveAll(venvDir)

		cmd := coretool.Command(st.UVPath, "venv", "--python", st.PythonPath, venvDir)
		cmd.Env = append(os.Environ(), "UV_PYTHON_PREFERENCE=only-system")
		out, err := cmd.CombinedOutput()
		if err != nil {
			st.Error = fmt.Sprintf("创建 venv 失败: %v\n%s", err, string(out))
			return st
		}

		venvPy := venvPythonPath(venvDir)
		if _, serr := os.Stat(venvPy); serr == nil {
			st.VenvReady = true
			st.VenvPath = venvDir
			emit("venv", 100, fmt.Sprintf("虚拟环境就绪: %s", venvDir))
		} else {
			st.Error = "venv 创建后验证失败"
			return st
		}
	}

	emit("done", 100, fmt.Sprintf("Python 环境就绪: v%s, venv: %s", st.Version, st.VenvPath))
	return st
}

// replacePrivatePythonInstall replaces installDir with a verified extractedDir.
// On success it deliberately retains install.previous until the caller has
// completed its final post-move verification via commitPrivatePythonInstall.
func replacePrivatePythonInstall(installDir, extractedDir string) error {
	backupDir := installDir + ".previous"

	hadInstall := false
	hadBackup := false
	if _, statErr := os.Stat(installDir); statErr == nil {
		hadInstall = true
		// install 存在时，.previous 只能是旧操作遗留的过期备份；新的
		// 替换会以当前 install 作为可回滚副本。
		if err := os.RemoveAll(backupDir); err != nil {
			return fmt.Errorf("清理旧备份目录: %w", err)
		}
		if err := os.Rename(installDir, backupDir); err != nil {
			return fmt.Errorf("备份旧安装: %w", err)
		}
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("检查旧安装: %w", statErr)
	} else if _, backupErr := os.Stat(backupDir); backupErr == nil {
		// 上一次可能在“旧安装已备份、新安装尚未就位”之间被中断。
		// 保留这个唯一的已知可用副本，直到新安装成功落位。
		hadBackup = true
	} else if !os.IsNotExist(backupErr) {
		return fmt.Errorf("检查旧安装备份: %w", backupErr)
	}

	moveErr := os.Rename(extractedDir, installDir)
	if moveErr == nil {
		return nil
	}
	err := fmt.Errorf("移动新安装: %w", moveErr)

	if hadInstall || hadBackup {
		if restoreErr := os.Rename(backupDir, installDir); restoreErr != nil {
			return fmt.Errorf("%w；恢复旧安装失败: %v", err, restoreErr)
		}
	}
	return err
}

func commitPrivatePythonInstall(installDir string) error {
	if err := os.RemoveAll(installDir + ".previous"); err != nil {
		return fmt.Errorf("清理旧安装备份: %w", err)
	}
	return nil
}

func rollbackPrivatePythonInstall(installDir string) error {
	backupDir := installDir + ".previous"
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		// Fresh installation: the current directory is the unverified replacement,
		// so remove it instead of leaving a broken private runtime behind.
		if err := os.RemoveAll(installDir); err != nil {
			return fmt.Errorf("移除失败的新安装: %w", err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("检查旧安装备份: %w", err)
	}
	if err := os.RemoveAll(installDir); err != nil {
		return fmt.Errorf("移除失败的新安装: %w", err)
	}
	if err := os.Rename(backupDir, installDir); err != nil {
		return fmt.Errorf("恢复旧安装: %w", err)
	}
	return nil
}
