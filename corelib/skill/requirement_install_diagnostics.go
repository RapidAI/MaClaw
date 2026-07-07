package skill

// requirement_install_diagnostics.go classifies pip/npm install failures into
// actionable categories. The classification result is appended to Violation.Message,
// giving both users and LLM self-repair clear remediation paths.
//
// Categories:
//   - package_not_found: typo in package name, or package doesn't exist on PyPI/npm
//   - version_conflict: dependency resolution failure, incompatible constraints
//   - build_failed: C extension compilation failure (missing compiler/headers)
//   - permission_denied: insufficient permissions for install location
//   - network_error: download/connectivity failure (handled separately by isTransientFixError)
//   - disk_full: no space left on device
//   - unknown: unrecognized failure pattern

import "strings"

// InstallErrorCategory is the classified type of an install failure.
type InstallErrorCategory string

const (
	InstallErrPackageNotFound InstallErrorCategory = "package_not_found"
	InstallErrVersionConflict InstallErrorCategory = "version_conflict"
	InstallErrBuildFailed     InstallErrorCategory = "build_failed"
	InstallErrPermission      InstallErrorCategory = "permission_denied"
	InstallErrDiskFull        InstallErrorCategory = "disk_full"
	InstallErrUnknown         InstallErrorCategory = "unknown"
)

// InstallErrorDiagnosis is the result of classifying an install error.
type InstallErrorDiagnosis struct {
	Category   InstallErrorCategory
	Suggestion string // actionable fix suggestion for LLM/user
}

// ClassifyInstallError analyzes pip/npm install output to determine the failure
// category and provide an actionable suggestion.
func ClassifyInstallError(output string) InstallErrorDiagnosis {
	lower := strings.ToLower(output)

	// Package not found
	if containsAnyInstallDiag(lower,
		"no matching distribution found",
		"could not find a version that satisfies",
		"error: not found",
		"npm err! 404",
		"404 not found",
		"package does not exist",
		"no versions available",
	) {
		return InstallErrorDiagnosis{
			Category:   InstallErrPackageNotFound,
			Suggestion: "检查包名拼写是否正确。常见原因：import名与pip包名不同（如 cv2→opencv-python, PIL→Pillow）。",
		}
	}

	// Version conflict / dependency resolution
	if containsAnyInstallDiag(lower,
		"incompatible",
		"version conflict",
		"resolutionimpossible",
		"cannot install",
		"conflicting dependencies",
		"no matching version",
		"peer dep",
		"could not resolve",
		"eresolve",
		"unable to resolve dependency tree",
	) {
		return InstallErrorDiagnosis{
			Category:   InstallErrVersionConflict,
			Suggestion: "依赖版本冲突。建议：移除版本约束（如 >=X.Y → 无约束），或在隔离环境中安装（venv/node_modules）。",
		}
	}

	// Build failed (C extension compilation)
	if containsAnyInstallDiag(lower,
		"error: subprocess-exited-with-error",
		"failed building wheel",
		"command errored out with exit status",
		"microsoft visual c++",
		"cl.exe",
		"gcc",
		"compilation failed",
		"fatal error c",
		"no such file or directory: 'gcc'",
		"unable to find vcvarsall",
		"build-essential",
		"distutils",
		"setup.py install",
	) {
		return InstallErrorDiagnosis{
			Category:   InstallErrBuildFailed,
			Suggestion: "编译 C 扩展失败。Windows: 安装 Visual Studio Build Tools (https://visualstudio.microsoft.com/visual-cpp-build-tools/)。Linux: sudo apt install build-essential python3-dev。macOS: xcode-select --install。",
		}
	}

	// Permission denied
	if containsAnyInstallDiag(lower,
		"permission denied",
		"permissionerror",
		"eacces",
		"access denied",
		"eperm",
	) {
		return InstallErrorDiagnosis{
			Category:   InstallErrPermission,
			Suggestion: "权限不足。建议使用 --user 安装（pip install --user）或在 virtualenv 中安装。",
		}
	}

	// Disk full
	if containsAnyInstallDiag(lower,
		"no space left on device",
		"enospc",
		"disk quota exceeded",
	) {
		return InstallErrorDiagnosis{
			Category:   InstallErrDiskFull,
			Suggestion: "磁盘空间不足。请清理磁盘后重试。",
		}
	}

	return InstallErrorDiagnosis{
		Category:   InstallErrUnknown,
		Suggestion: "安装失败，请查看上方错误信息确定原因。",
	}
}

// FormatInstallDiagnosis formats a diagnosis into a string suitable for
// appending to a Violation message.
func FormatInstallDiagnosis(diag InstallErrorDiagnosis) string {
	if diag.Category == InstallErrUnknown {
		return ""
	}
	return " [diagnosis: " + string(diag.Category) + "] " + diag.Suggestion
}

func containsAnyInstallDiag(s string, patterns ...string) bool {
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}
