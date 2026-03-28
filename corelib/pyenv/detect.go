// Package pyenv 提供 Python 环境检测、自动安装和 uv 虚拟环境管理。
// 安装目录为 ~/.maclaw/python/，使用 python-build-standalone 提供私有 Python，
// 通过 uv 创建和管理虚拟环境。
package pyenv

import (
	"context"
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
	"time"
)

// MinPythonMajor 最低 Python 主版本。
const MinPythonMajor = 3

// MinPythonMinor 最低 Python 次版本。
const MinPythonMinor = 10

// Status 表示 Python 环境的状态。
type Status struct {
	Available   bool   `json:"available"`    // Python 是否可用
	PythonPath  string `json:"python_path"`  // python 可执行文件路径
	Version     string `json:"version"`      // 版本字符串，如 "3.12.3"
	UVAvailable bool   `json:"uv_available"` // uv 是否可用
	UVPath      string `json:"uv_path"`      // uv 可执行文件路径
	VenvPath    string `json:"venv_path"`    // 虚拟环境路径
	VenvReady   bool   `json:"venv_ready"`   // 虚拟环境是否就绪
	IsPrivate   bool   `json:"is_private"`   // 是否为 maclaw 私有安装
	Error       string `json:"error"`        // 错误信息
}

// ProgressFunc 安装进度回调。
type ProgressFunc func(stage string, pct int, msg string)

// baseDir 返回 ~/.maclaw/python/。
func baseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法获取用户主目录: %w", err)
	}
	return filepath.Join(home, ".maclaw", "python"), nil
}

// VenvDir 返回虚拟环境目录 ~/.maclaw/python/venv。
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
	if runtime.GOOS == "windows" {
		return filepath.Join(base, "install", "python.exe"), nil
	}
	return filepath.Join(base, "install", "bin", "python3"), nil
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
	cmd := exec.CommandContext(ctx, pythonPath, "--version")
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

// standalonePythonURL 返回 python-build-standalone 的下载 URL。
func standalonePythonURL() (string, error) {
	const baseURL = "https://github.com/astral-sh/python-build-standalone/releases/latest/download"
	arch, err := resolveArch()
	if err != nil {
		return "", err
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
		return "", fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
	}
	return baseURL + "/" + filename, nil
}

// uvInstallURL 返回 uv 的下载 URL。
func uvInstallURL() (string, error) {
	const baseURL = "https://github.com/astral-sh/uv/releases/latest/download"
	arch, err := resolveArch()
	if err != nil {
		return "", err
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
		return "", fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
	}
	return baseURL + "/" + filename, nil
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
	client := &http.Client{Timeout: 15 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	tmpPath := destPath + ".download"
	outFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}

	totalSize := resp.ContentLength
	var written int64
	buf := make([]byte, 64*1024)
	lastPct := -1

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := outFile.Write(buf[:n]); wErr != nil {
				outFile.Close()
				os.Remove(tmpPath)
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
			os.Remove(tmpPath)
			return fmt.Errorf("下载中断: %w", readErr)
		}
	}
	if err := outFile.Sync(); err != nil {
		outFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("sync 失败: %w", err)
	}
	if err := outFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close 失败: %w", err)
	}

	// 原子替换
	os.Remove(destPath)
	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename 失败: %w", err)
	}
	return nil
}

// extractTarGz 解压 tar.gz 到目标目录。
func extractTarGz(archivePath, destDir string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}
	cmd := exec.Command("tar", "xzf", archivePath, "-C", destDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tar 解压失败: %w\n%s", err, string(out))
	}
	return nil
}

// extractZip 解压 zip 到目标目录（Windows uv 用）。
func extractZip(archivePath, destDir string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		cmd := exec.Command("powershell", "-NoProfile", "-Command",
			fmt.Sprintf("Expand-Archive -Force -Path '%s' -DestinationPath '%s'", archivePath, destDir))
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("Expand-Archive 失败: %w\n%s", err, string(out))
		}
		return nil
	}
	cmd := exec.Command("unzip", "-o", archivePath, "-d", destDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("unzip 失败: %w\n%s", err, string(out))
	}
	return nil
}

// errFound 是 findAndMoveBinary 内部用于提前终止 Walk 的哨兵错误。
var errFound = fmt.Errorf("found")

// findAndMoveBinary 在目录树中查找指定二进制文件并复制到目标目录。
func findAndMoveBinary(searchDir, binName, destDir string) error {
	var found string
	filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Name() == binName {
			found = path
			return errFound // 提前终止遍历
		}
		return nil
	})
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

	st := Detect()

	// 已经全部就绪
	if st.Available && st.UVAvailable && st.VenvReady {
		emit("done", 100, fmt.Sprintf("Python 环境就绪: %s (v%s)", st.PythonPath, st.Version))
		return st
	}

	base, err := baseDir()
	if err != nil {
		st.Error = err.Error()
		return st
	}

	// --- 步骤 1: 安装 Python ---
	if !st.Available {
		emit("python", 0, "正在安装 Python 3.12 ...")
		pyURL, err := standalonePythonURL()
		if err != nil {
			st.Error = fmt.Sprintf("获取 Python 下载地址失败: %v", err)
			return st
		}

		archivePath := filepath.Join(base, "python-standalone.tar.gz")
		if err := downloadToFile(pyURL, archivePath, func(stage string, pct int, msg string) {
			emit("python-download", pct, msg)
		}); err != nil {
			st.Error = fmt.Sprintf("下载 Python 失败: %v", err)
			return st
		}

		emit("python-extract", 50, "正在解压 Python ...")
		installDir := filepath.Join(base, "install")
		os.RemoveAll(installDir)
		if err := extractTarGz(archivePath, base); err != nil {
			os.Remove(archivePath)
			st.Error = fmt.Sprintf("解压 Python 失败: %v", err)
			return st
		}
		// python-build-standalone 解压后目录名为 "python"，重命名为 "install"
		extractedDir := filepath.Join(base, "python")
		if _, serr := os.Stat(extractedDir); serr == nil {
			if err := os.Rename(extractedDir, installDir); err != nil {
				os.Remove(archivePath)
				st.Error = fmt.Sprintf("重命名 Python 目录失败: %v", err)
				return st
			}
		}
		os.Remove(archivePath)

		// 验证安装
		pp, _ := privatePythonPath()
		if ver, ok := checkPython(pp); ok {
			st.Available = true
			st.PythonPath = pp
			st.Version = ver
			st.IsPrivate = true
			emit("python", 100, fmt.Sprintf("Python %s 安装完成", ver))
		} else {
			st.Error = "Python 安装后验证失败，请检查网络或手动安装"
			return st
		}
	}

	// --- 步骤 2: 安装 uv ---
	if !st.UVAvailable {
		emit("uv", 0, "正在安装 uv ...")
		uvURL, err := uvInstallURL()
		if err != nil {
			st.Error = fmt.Sprintf("获取 uv 下载地址失败: %v", err)
			return st
		}

		isZip := strings.HasSuffix(uvURL, ".zip")
		ext := ".tar.gz"
		if isZip {
			ext = ".zip"
		}
		archivePath := filepath.Join(base, "uv-archive"+ext)
		if err := downloadToFile(uvURL, archivePath, func(stage string, pct int, msg string) {
			emit("uv-download", pct, msg)
		}); err != nil {
			st.Error = fmt.Sprintf("下载 uv 失败: %v", err)
			return st
		}

		emit("uv-extract", 50, "正在解压 uv ...")
		uvExtractDir := filepath.Join(base, "uv-extract")
		os.RemoveAll(uvExtractDir)
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
		os.MkdirAll(binDir, 0755)
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
	if !st.VenvReady {
		emit("venv", 0, "正在创建虚拟环境 ...")
		venvDir, _ := VenvDir()
		os.RemoveAll(venvDir)

		cmd := exec.Command(st.UVPath, "venv", "--python", st.PythonPath, venvDir)
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
