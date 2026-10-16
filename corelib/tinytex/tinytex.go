// Package tinytex installs TeX Live scheme-small without shipping a full TeX Live tree.
//
// The official TinyTeX project (rstudio/tinytex-releases) does not publish a
// scheme-small archive. TinyTeX-0 is the infraonly bootstrap (tlmgr + engines).
// scheme-small is installed afterwards with tlmgr from a CTAN mirror.
package tinytex

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	// Scheme is the TeX Live scheme installed after the TinyTeX-0 bootstrap.
	Scheme = "scheme-small"
	// Bundle is the official TinyTeX bootstrap. It is infraonly, not scheme-small.
	Bundle = "TinyTeX-0"
	// ReleaseTag is the moving daily tag documented by TinyTeX install scripts.
	ReleaseTag = "daily"

	stampName       = "maclaw-scheme-small.ok"
	minArchiveBytes = 256 << 10
)

// OfficialBase is the canonical GitHub Release for TinyTeX binaries.
// Mirrors below only proxy this URL; they are not a separate TeX build.
const OfficialBase = "https://github.com/rstudio/tinytex-releases/releases/download/" + ReleaseTag

// InstallRoot is the per-user directory that holds the cache and the extracted tree.
func InstallRoot(dataDir string) string {
	return filepath.Join(dataDir, "tinytex")
}

// DistDir is where the self-extracting archive or tarball is unpacked.
func DistDir(dataDir string) string {
	return filepath.Join(InstallRoot(dataDir), "dist")
}

// AssetName returns the official daily asset for this OS/arch.
func AssetName(goos, goarch string) (string, error) {
	switch goos {
	case "windows":
		switch goarch {
		case "amd64", "arm64":
			return "TinyTeX-0-windows.exe", nil
		default:
			return "", fmt.Errorf("unsupported windows arch %s", goarch)
		}
	case "darwin":
		return "TinyTeX-0-darwin.tar.xz", nil
	case "linux":
		switch goarch {
		case "amd64":
			return "TinyTeX-0-linux-x86_64.tar.xz", nil
		case "arm64":
			return "TinyTeX-0-linux-arm64.tar.xz", nil
		default:
			return "", fmt.Errorf("unsupported linux arch %s", goarch)
		}
	default:
		return "", fmt.Errorf("unsupported os %s", goos)
	}
}

// OfficialAssetURL is the GitHub Release URL for the current platform.
func OfficialAssetURL(goos, goarch string) (string, error) {
	name, err := AssetName(goos, goarch)
	if err != nil {
		return "", err
	}
	return OfficialBase + "/" + name, nil
}

// DownloadURLs lists the official asset first, then GitHub proxies.
// When china is set, proxies are tried first because github.com is often slow,
// but every URL still points at the same rstudio/tinytex-releases asset.
func DownloadURLs(goos, goarch string, china bool) ([]string, error) {
	official, err := OfficialAssetURL(goos, goarch)
	if err != nil {
		return nil, err
	}
	proxies := []string{
		"https://ghproxy.net/" + official,
		"https://ghfast.top/" + official,
	}
	if china {
		return append(proxies, official), nil
	}
	return append([]string{official}, proxies...), nil
}

// CTANRepositories are tried in order for `tlmgr install scheme-small`.
// mirror.ctan.org is the official redirector. The others are public CTAN mirrors.
func CTANRepositories(china bool) []string {
	official := "https://mirror.ctan.org/systems/texlive/tlnet"
	tuna := "https://mirrors.tuna.tsinghua.edu.cn/CTAN/systems/texlive/tlnet"
	ustc := "https://mirrors.ustc.edu.cn/CTAN/systems/texlive/tlnet"
	if china {
		return []string{tuna, ustc, official}
	}
	return []string{official, tuna}
}

// ArchiveOK rejects HTML error pages and truncated downloads before we execute
// or unpack them. Windows assets are 7-Zip self-extractors (MZ). Unix assets are xz.
func ArchiveOK(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if fi.Size() < minArchiveBytes {
		return fmt.Errorf("archive too small (%d bytes)", fi.Size())
	}
	buf := make([]byte, 6)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".exe":
		if n < 2 || buf[0] != 'M' || buf[1] != 'Z' {
			return fmt.Errorf("not a windows executable")
		}
	case ".xz":
		magic := []byte{0xfd, '7', 'z', 'X', 'Z', 0x00}
		if n < len(magic) || !bytes.Equal(buf[:len(magic)], magic) {
			return fmt.Errorf("not an xz archive")
		}
	default:
		return fmt.Errorf("unsupported archive type %q", filepath.Ext(path))
	}
	return nil
}

// Extract unpacks a verified TinyTeX-0 archive into dest.
// The Windows asset is a 7-Zip self-extractor; -y -o<dir> is its silent switch.
func Extract(ctx context.Context, archive, dest string) error {
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	absArchive, err := filepath.Abs(archive)
	if err != nil {
		return err
	}
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	var cmdName string
	var args []string
	switch strings.ToLower(filepath.Ext(absArchive)) {
	case ".exe":
		cmdName = absArchive
		args = []string{"-y", "-o" + absDest}
	case ".xz":
		cmdName = "tar"
		args = []string{"-xJf", absArchive, "-C", absDest}
	default:
		return fmt.Errorf("unsupported archive type %q", filepath.Ext(absArchive))
	}
	cmd := tool.CommandContext(ctx, cmdName, args...)
	cmd.Dir = filepath.Dir(absArchive)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("extract tinytex: %w: %s", err, clip(string(out), 800))
	}
	return nil
}

// FindDistRoot locates the directory that contains bin/ and tlpkg/.
// The self-extractor usually creates a TinyTeX subdirectory.
func FindDistRoot(dest string) (string, error) {
	if isDist(dest) {
		return dest, nil
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		return "", err
	}
	var found []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(dest, e.Name())
		if isDist(candidate) {
			found = append(found, candidate)
		}
	}
	if len(found) == 1 {
		return found[0], nil
	}
	if len(found) == 0 {
		return "", fmt.Errorf("tinytex tree not found under %s", dest)
	}
	return "", fmt.Errorf("multiple tinytex trees under %s", dest)
}

func isDist(dir string) bool {
	bin, errBin := os.Stat(filepath.Join(dir, "bin"))
	tlpkg, errPkg := os.Stat(filepath.Join(dir, "tlpkg"))
	return errBin == nil && bin.IsDir() && errPkg == nil && tlpkg.IsDir()
}

// FindBin searches bin/<platform>/ for names, in the order given.
func FindBin(dist string, names ...string) (string, error) {
	binRoot := filepath.Join(dist, "bin")
	want := map[string]struct{}{}
	for _, name := range names {
		want[strings.ToLower(name)] = struct{}{}
	}
	found := map[string]string{}
	err := filepath.WalkDir(binRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		key := strings.ToLower(d.Name())
		if _, ok := want[key]; ok {
			if _, exists := found[key]; !exists {
				found[key] = path
			}
		}
		return nil
	})
	for _, name := range names {
		if path, ok := found[strings.ToLower(name)]; ok {
			return path, nil
		}
	}
	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("missing %s under %s", strings.Join(names, "|"), binRoot)
}

// FindEngine returns the xelatex binary used for the install check.
func FindEngine(dist string) (string, error) {
	if runtime.GOOS == "windows" {
		return FindBin(dist, "xelatex.exe", "xelatex")
	}
	return FindBin(dist, "xelatex")
}

// FindTlmgr returns the TeX Live package manager shipped in TinyTeX-0.
func FindTlmgr(dist string) (string, error) {
	if runtime.GOOS == "windows" {
		return FindBin(dist, "tlmgr.bat", "tlmgr.exe", "tlmgr")
	}
	return FindBin(dist, "tlmgr")
}

// ArticleClassPath is the file scheme-small must provide (collection-latex).
func ArticleClassPath(dist string) string {
	return filepath.Join(dist, "texmf-dist", "tex", "latex", "base", "article.cls")
}

// ArticlePresent reports whether scheme-small's base class is on disk.
func ArticlePresent(dist string) bool {
	st, err := os.Stat(ArticleClassPath(dist))
	return err == nil && st.Size() > 0 && !st.IsDir()
}

// EnginePresent reports whether the xelatex binary is on disk.
func EnginePresent(dist string) bool {
	_, err := FindEngine(dist)
	return err == nil
}

// StampPath records a successful xelatex --version check after scheme-small.
func StampPath(dist string) string {
	return filepath.Join(dist, stampName)
}

// WriteStamp stores the first line of `xelatex --version`.
func WriteStamp(dist, version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return fmt.Errorf("empty xelatex version")
	}
	if i := strings.IndexByte(version, '\n'); i >= 0 {
		version = strings.TrimSpace(version[:i])
	}
	path := StampPath(dist)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(version+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadStamp returns the stored xelatex version line.
func ReadStamp(dist string) (string, bool) {
	b, err := os.ReadFile(StampPath(dist))
	if err != nil {
		return "", false
	}
	line := strings.TrimSpace(string(b))
	if line == "" {
		return "", false
	}
	return line, true
}

// Verified is true only after scheme-small files exist and xelatex has been checked.
func Verified(dist string) bool {
	if dist == "" || !EnginePresent(dist) || !ArticlePresent(dist) {
		return false
	}
	_, ok := ReadStamp(dist)
	return ok
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
