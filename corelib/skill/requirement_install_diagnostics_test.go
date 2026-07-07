package skill

import "testing"

func TestClassifyInstallError_PackageNotFound(t *testing.T) {
	tests := []struct {
		output string
	}{
		{"ERROR: No matching distribution found for nonexistent-pkg"},
		{"ERROR: Could not find a version that satisfies the requirement fake-pkg"},
		{"npm ERR! 404 Not Found - GET https://registry.npmjs.org/fake-pkg"},
	}
	for _, tt := range tests {
		diag := ClassifyInstallError(tt.output)
		if diag.Category != InstallErrPackageNotFound {
			t.Errorf("ClassifyInstallError(%q) = %q, want package_not_found", tt.output[:40], diag.Category)
		}
	}
}

func TestClassifyInstallError_VersionConflict(t *testing.T) {
	tests := []struct {
		output string
	}{
		{"ERROR: Cannot install flask==2.0 and werkzeug>=3.0 because these package versions have conflicting dependencies."},
		{"npm ERR! ERESOLVE unable to resolve dependency tree"},
		{"ResolutionImpossible: for help visit https://pip.pypa.io/en/latest/topics/dependency-resolution/"},
	}
	for _, tt := range tests {
		diag := ClassifyInstallError(tt.output)
		if diag.Category != InstallErrVersionConflict {
			t.Errorf("ClassifyInstallError(%q...) = %q, want version_conflict", tt.output[:40], diag.Category)
		}
	}
}

func TestClassifyInstallError_BuildFailed(t *testing.T) {
	tests := []struct {
		output string
	}{
		{"error: subprocess-exited-with-error\n  Building wheel for numpy (pyproject.toml) did not run successfully"},
		{"Failed building wheel for cryptography"},
		{"error: Microsoft Visual C++ 14.0 or greater is required"},
	}
	for _, tt := range tests {
		diag := ClassifyInstallError(tt.output)
		if diag.Category != InstallErrBuildFailed {
			t.Errorf("ClassifyInstallError(%q...) = %q, want build_failed", tt.output[:40], diag.Category)
		}
	}
}

func TestClassifyInstallError_Permission(t *testing.T) {
	diag := ClassifyInstallError("PermissionError: [Errno 13] Permission denied: '/usr/lib/python3/dist-packages'")
	if diag.Category != InstallErrPermission {
		t.Errorf("got %q, want permission_denied", diag.Category)
	}
}

func TestClassifyInstallError_DiskFull(t *testing.T) {
	diag := ClassifyInstallError("OSError: [Errno 28] No space left on device")
	if diag.Category != InstallErrDiskFull {
		t.Errorf("got %q, want disk_full", diag.Category)
	}
}

func TestClassifyInstallError_Unknown(t *testing.T) {
	diag := ClassifyInstallError("some random error that doesn't match anything")
	if diag.Category != InstallErrUnknown {
		t.Errorf("got %q, want unknown", diag.Category)
	}
}

func TestFormatInstallDiagnosis_NonEmpty(t *testing.T) {
	diag := InstallErrorDiagnosis{Category: InstallErrBuildFailed, Suggestion: "install build tools"}
	result := FormatInstallDiagnosis(diag)
	if result == "" {
		t.Error("expected non-empty diagnosis string for build_failed")
	}
}

func TestFormatInstallDiagnosis_EmptyForUnknown(t *testing.T) {
	diag := InstallErrorDiagnosis{Category: InstallErrUnknown, Suggestion: "check logs"}
	result := FormatInstallDiagnosis(diag)
	if result != "" {
		t.Errorf("expected empty for unknown, got %q", result)
	}
}
