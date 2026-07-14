package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// windowsMSVCToolchain is a host-side discovery result for MSVC / Visual Studio.
// cl.exe is intentionally not expected on the default PATH; callers should use
// VCVars64 via cmd /c "call ... && cl ...".
type windowsMSVCToolchain struct {
	DisplayName string
	InstallPath string
	Version     string
	VCVars64    string
}

var (
	windowsMSVCDetectOnce sync.Once
	windowsMSVCDetected   *windowsMSVCToolchain
)

// detectWindowsMSVCToolchain finds a local Visual Studio install that includes
// C++ tools. Cached for the process lifetime. Returns nil on non-Windows or when
// no suitable install is found.
func detectWindowsMSVCToolchain() *windowsMSVCToolchain {
	if runtime.GOOS != "windows" {
		return nil
	}
	windowsMSVCDetectOnce.Do(func() {
		windowsMSVCDetected = probeWindowsMSVCToolchain()
		if windowsMSVCDetected != nil {
			log.Printf("[msvc-detect] found %s path=%q vcvars=%q",
				windowsMSVCDetected.DisplayName,
				windowsMSVCDetected.InstallPath,
				windowsMSVCDetected.VCVars64,
			)
		}
	})
	return windowsMSVCDetected
}

func probeWindowsMSVCToolchain() *windowsMSVCToolchain {
	vswhere := windowsVswherePath()
	if vswhere != "" {
		if tc := probeWindowsMSVCViaVswhere(vswhere); tc != nil {
			return tc
		}
	}
	return probeWindowsMSVCFromCommonPaths()
}

// vswhereJSONEntry is the subset of vswhere -format json we need.
type vswhereJSONEntry struct {
	InstallationPath    string `json:"installationPath"`
	DisplayName         string `json:"displayName"`
	InstallationVersion string `json:"installationVersion"`
}

func probeWindowsMSVCViaVswhere(vswhere string) *windowsMSVCToolchain {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	// One process: prefer C++ toolset installs; fall back to any latest product.
	attempts := [][]string{
		{
			"-latest", "-products", "*",
			"-requires", "Microsoft.VisualStudio.Component.VC.Tools.x86.x64",
			"-format", "json",
		},
		{
			"-latest", "-products", "*",
			"-format", "json",
		},
	}
	for _, args := range attempts {
		entries := runVswhereJSON(ctx, vswhere, args...)
		for _, e := range entries {
			tc := toolchainFromInstallPath(e.InstallationPath, e.DisplayName, e.InstallationVersion)
			if tc != nil {
				return tc
			}
		}
	}
	return nil
}

func runVswhereJSON(ctx context.Context, vswhere string, args ...string) []vswhereJSONEntry {
	cmd := exec.CommandContext(ctx, vswhere, args...)
	hideCommandWindow(cmd)
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil
	}
	// vswhere may return a single object or an array depending on flags/version.
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 {
		return nil
	}
	if trimmed[0] == '{' {
		var one vswhereJSONEntry
		if json.Unmarshal(trimmed, &one) == nil && strings.TrimSpace(one.InstallationPath) != "" {
			return []vswhereJSONEntry{one}
		}
		return nil
	}
	var many []vswhereJSONEntry
	if json.Unmarshal(trimmed, &many) != nil {
		return nil
	}
	return many
}

func windowsVswherePath() string {
	seen := map[string]bool{}
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft Visual Studio", "Installer", "vswhere.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Microsoft Visual Studio", "Installer", "vswhere.exe"),
		`C:\Program Files (x86)\Microsoft Visual Studio\Installer\vswhere.exe`,
		`C:\Program Files\Microsoft Visual Studio\Installer\vswhere.exe`,
	}
	for _, p := range candidates {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

func probeWindowsMSVCFromCommonPaths() *windowsMSVCToolchain {
	// Year / edition matrix: VS 2026 uses "18", 2022 uses "2022", etc.
	roots := uniqueNonEmptyStrings([]string{
		filepath.Join(os.Getenv("ProgramFiles"), "Microsoft Visual Studio"),
		`C:\Program Files\Microsoft Visual Studio`,
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft Visual Studio"),
		`C:\Program Files (x86)\Microsoft Visual Studio`,
	})
	// Prefer newer product lines first.
	years := []string{"18", "2022", "17", "2026", "2019", "16"}
	editions := []string{"Community", "Professional", "Enterprise", "BuildTools", "Preview"}
	for _, root := range roots {
		for _, year := range years {
			for _, ed := range editions {
				install := filepath.Join(root, year, ed)
				if tc := toolchainFromInstallPath(install, "Visual Studio "+ed+" "+year, year); tc != nil {
					return tc
				}
			}
		}
	}
	return nil
}

func uniqueNonEmptyStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func toolchainFromInstallPath(installPath, displayName, version string) *windowsMSVCToolchain {
	installPath = strings.TrimSpace(installPath)
	if installPath == "" {
		return nil
	}
	// Normalize quotes vswhere sometimes leaves.
	installPath = strings.Trim(installPath, "\"")
	vcvars := filepath.Join(installPath, "VC", "Auxiliary", "Build", "vcvars64.bat")
	if fi, err := os.Stat(vcvars); err != nil || fi.IsDir() {
		return nil
	}
	if strings.TrimSpace(displayName) == "" {
		displayName = filepath.Base(installPath)
	}
	return &windowsMSVCToolchain{
		DisplayName: strings.TrimSpace(displayName),
		InstallPath: installPath,
		Version:     strings.TrimSpace(version),
		VCVars64:    vcvars,
	}
}

// formatWindowsMSVCToolchainHint returns system-prompt guidance so the coding
// agent stops blind-searching Program Files for cl.exe / old VS years.
func formatWindowsMSVCToolchainHint(tc *windowsMSVCToolchain) string {
	if tc == nil || strings.TrimSpace(tc.VCVars64) == "" {
		return strings.TrimSpace(`
## Host C/C++ toolchain
No Visual Studio C++ tools were auto-detected on this host.
cl.exe is usually NOT on PATH even when Visual Studio is installed.
If you need MSVC: locate vcvars64.bat via
  & "${env:ProgramFiles(x86)}\Microsoft Visual Studio\Installer\vswhere.exe" -latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath
then compile with:
  cmd /c "call \"<install>\VC\Auxiliary\Build\vcvars64.bat\" && cl /utf-8 /EHsc ..."
Do NOT recursively scan Program Files. Do NOT assume VS 2019/2022 paths. Prefer project build scripts when present.
`)
	}

	var b strings.Builder
	b.WriteString("\n## Host C/C++ toolchain (auto-detected)\n")
	b.WriteString("Visual Studio C++ tools ARE installed on this machine.\n")
	if tc.DisplayName != "" {
		b.WriteString("- Product: ")
		b.WriteString(tc.DisplayName)
		b.WriteByte('\n')
	}
	if tc.Version != "" {
		b.WriteString("- Version: ")
		b.WriteString(tc.Version)
		b.WriteByte('\n')
	}
	b.WriteString("- Install path: ")
	b.WriteString(tc.InstallPath)
	b.WriteByte('\n')
	b.WriteString("- vcvars64.bat: ")
	b.WriteString(tc.VCVars64)
	b.WriteByte('\n')
	b.WriteString("cl.exe is NOT on the default PATH (this is normal for MSVC).\n")
	b.WriteString("Compile/run with ONE bash command using cmd.exe (so vcvars + cl share the env):\n")
	b.WriteString("  cmd /c \"call \\\"")
	b.WriteString(tc.VCVars64)
	b.WriteString("\\\" && cl /utf-8 /EHsc /Fe:app.exe file1.cpp file2.cpp && app.exe\"\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Do NOT run bare `where cl.exe` / `cl` / `Get-Command cl` without vcvars first — they will fail and waste turns.\n")
	b.WriteString("- Do NOT recursively search Program Files for cl.exe or vcvars.\n")
	b.WriteString("- Do NOT hardcode Visual Studio 2019/2022 paths; use the install path above (VS 2026 is product line 18).\n")
	b.WriteString("- Avoid Launch-VsDevShell.ps1 (PowerShell execution policy often blocks it).\n")
	return b.String()
}
