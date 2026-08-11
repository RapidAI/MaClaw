//go:build windows

package accessibility

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// C# UIA sidecar preferred over PowerShell (lower latency once compiled).
// Binary: <MaclawBaseDir>/bin/maclaw-uia-sidecar.exe

const uiaCSharpExeName = "maclaw-uia-sidecar.exe"

var (
	uiaCSharpMu       sync.Mutex
	uiaCSharpResolved string
	uiaCSharpTried    bool
)

// uiaCSharpSidecarPath returns a path to maclaw-uia-sidecar.exe, compiling it
// with Framework csc.exe on first use when possible.
func uiaCSharpSidecarPath() string {
	uiaCSharpMu.Lock()
	defer uiaCSharpMu.Unlock()
	if uiaCSharpTried {
		return uiaCSharpResolved
	}
	uiaCSharpTried = true
	if p := findExistingUIACsharp(); p != "" {
		uiaCSharpResolved = p
		return p
	}
	if p, err := compileUIACsharp(); err == nil && p != "" {
		uiaCSharpResolved = p
		return p
	}
	return ""
}

func findExistingUIACsharp() string {
	var candidates []string
	// Prefer install-dir / next-to-exe first (NSIS ships sidecar beside MaClaw.exe).
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, uiaCSharpExeName),
			filepath.Join(dir, "bin", uiaCSharpExeName),
		)
	}
	if base := corelib.MaclawBaseDir(); base != "" {
		candidates = append(candidates, filepath.Join(base, "bin", uiaCSharpExeName))
	}
	if data := corelib.MaclawDataDir(); data != "" {
		candidates = append(candidates, filepath.Join(data, "bin", uiaCSharpExeName))
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "dist", uiaCSharpExeName),
			filepath.Join(wd, "corelib", "accessibility", "tools", "MaclawUIASidecar", uiaCSharpExeName),
		)
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Size() > 0 {
			// Seed into base/bin for stable subsequent lookups.
			seedUIACsharpToBaseBin(p)
			return p
		}
	}
	return ""
}

// seedUIACsharpToBaseBin copies a found prebuilt exe into <MaclawBaseDir>/bin when missing.
func seedUIACsharpToBaseBin(src string) {
	base := corelib.MaclawBaseDir()
	if base == "" || src == "" {
		return
	}
	dst := filepath.Join(base, "bin", uiaCSharpExeName)
	if sameFile(src, dst) {
		return
	}
	if st, err := os.Stat(dst); err == nil && st.Size() > 0 {
		return
	}
	_ = os.MkdirAll(filepath.Dir(dst), 0o755)
	in, err := os.ReadFile(src)
	if err != nil || len(in) == 0 {
		return
	}
	_ = os.WriteFile(dst, in, 0o755)
}

func sameFile(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return a == b
	}
	return strings.EqualFold(aa, bb)
}

func findCSC() string {
	windir := os.Getenv("WINDIR")
	if windir == "" {
		windir = `C:\Windows`
	}
	for _, p := range []string{
		filepath.Join(windir, "Microsoft.NET", "Framework64", "v4.0.30319", "csc.exe"),
		filepath.Join(windir, "Microsoft.NET", "Framework", "v4.0.30319", "csc.exe"),
	} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func compileUIACsharp() (string, error) {
	csc := findCSC()
	if csc == "" {
		return "", fmt.Errorf("csc.exe not found")
	}
	src, err := locateOrMaterializeCSharpSource()
	if err != nil {
		return "", err
	}
	outDir := filepath.Join(corelib.MaclawBaseDir(), "bin")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	outExe := filepath.Join(outDir, uiaCSharpExeName)
	args := []string{"/nologo", "/t:exe", "/out:" + outExe, "/platform:anycpu"}
	for _, r := range findUIARefAssemblies() {
		args = append(args, "/r:"+r)
	}
	args = append(args, src)
	// Bound the compile: a wedged csc must not hang UIA startup forever.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, csc, args...)
	coretool.HideCommandWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("csc timed out after 60s")
		}
		return "", fmt.Errorf("csc failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	if st, err := os.Stat(outExe); err != nil || st.Size() == 0 {
		return "", fmt.Errorf("csc produced no exe")
	}
	return outExe, nil
}

// findUIARefAssemblies locates UIAutomation + WindowsBase for csc /r:.
// Prefer Reference Assemblies; fall back to GAC_MSIL v4 copies.
func findUIARefAssemblies() []string {
	names := []string{"UIAutomationClient.dll", "UIAutomationTypes.dll", "WindowsBase.dll"}
	var roots []string
	// .NET Framework reference assemblies (if VS/SDK installed)
	for _, base := range []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Reference Assemblies", "Microsoft", "Framework", ".NETFramework"),
		filepath.Join(os.Getenv("ProgramFiles"), "Reference Assemblies", "Microsoft", "Framework", ".NETFramework"),
	} {
		if base == "" || strings.HasPrefix(base, string(filepath.Separator)) && !dirExists(base) {
			continue
		}
		if entries, err := os.ReadDir(base); err == nil {
			// prefer highest v4.x
			for i := len(entries) - 1; i >= 0; i-- {
				if entries[i].IsDir() && strings.HasPrefix(entries[i].Name(), "v4") {
					roots = append(roots, filepath.Join(base, entries[i].Name()))
				}
			}
		}
	}
	// Framework install dir (usually missing UIA DLLs)
	if csc := findCSC(); csc != "" {
		roots = append(roots, filepath.Dir(csc))
	}
	// GAC_MSIL (present on typical Windows desktops)
	gac := filepath.Join(os.Getenv("WINDIR"), "Microsoft.NET", "assembly", "GAC_MSIL")
	if gac == filepath.Join("", "Microsoft.NET", "assembly", "GAC_MSIL") {
		gac = `C:\Windows\Microsoft.NET\assembly\GAC_MSIL`
	}
	var out []string
	for _, name := range names {
		found := ""
		for _, root := range roots {
			p := filepath.Join(root, name)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				found = p
				break
			}
		}
		if found == "" {
			// GAC layout: GAC_MSIL/<AsmName>/v4.0_.../<AsmName>.dll
			asm := strings.TrimSuffix(name, ".dll")
			gacRoot := filepath.Join(gac, asm)
			_ = filepath.Walk(gacRoot, func(path string, info os.FileInfo, err error) error {
				if err != nil || info == nil || info.IsDir() {
					return nil
				}
				if strings.EqualFold(info.Name(), name) && strings.Contains(path, "v4.0") {
					found = path
					return filepath.SkipDir
				}
				return nil
			})
		}
		if found != "" {
			out = append(out, found)
		}
	}
	return out
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func locateOrMaterializeCSharpSource() (string, error) {
	var candidates []string
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "corelib", "accessibility", "tools", "MaclawUIASidecar", "Program.cs"))
	}
	candidates = append(candidates, filepath.Join("corelib", "accessibility", "tools", "MaclawUIASidecar", "Program.cs"))
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	dir := filepath.Join(corelib.MaclawBaseDir(), "bin", "uia-src")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	src := filepath.Join(dir, "Program.cs")
	if err := os.WriteFile(src, []byte(uiaCSharpSource), 0o644); err != nil {
		return "", err
	}
	return src, nil
}

// ResetUIACsharpCache clears the resolve/compile cache (tests).
func ResetUIACsharpCache() {
	uiaCSharpMu.Lock()
	defer uiaCSharpMu.Unlock()
	uiaCSharpTried = false
	uiaCSharpResolved = ""
}

// UIABackend reports which sidecar is running: "csharp", "powershell", or "".
func UIABackend() string {
	globalUIASidecar.mu.Lock()
	defer globalUIASidecar.mu.Unlock()
	if !globalUIASidecar.ready {
		return ""
	}
	return globalUIASidecar.backend
}
