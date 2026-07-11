package httpapi

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	coreskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// skillMarketPackageError is a classified preflight failure so handlers can map
// HTTP status/code without parsing Chinese error text.
type skillMarketPackageError struct {
	Code string
	Msg  string
}

func (e *skillMarketPackageError) Error() string {
	if e == nil {
		return ""
	}
	return e.Msg
}

func skillPackageErr(code, msg string) error {
	return &skillMarketPackageError{Code: code, Msg: msg}
}

func skillMarketPackageErrorCode(err error) string {
	var pe *skillMarketPackageError
	if errors.As(err, &pe) && pe != nil && pe.Code != "" {
		return pe.Code
	}
	return "PACKAGE_INVALID"
}

// keptEntry is a file selected for market upload after path normalization.
type keptEntry struct {
	file *zip.File
	name string // normalized package-relative path
}

// prepareSkillZipForHubCenterMarket rewrites a skill package zip to drop
// runtime/cache artifacts (node_modules, .git, venv, __MACOSX, …) and bare
// directory entries, then validates HubCenter limits with size as the primary gate:
//
//   - total uncompressed size ≤ MaxSkillMarketZipTotalBytes (500 MiB)
//   - single file ≤ MaxSkillMarketZipSingleFileBytes (50 MiB)
//   - file entry count ≤ MaxSkillMarketZipEntries (DoS backstop only)
//
// Many necessary resource files (templates, fonts, SVGs) are allowed when
// overall volume is reasonable.
//
// Returns the path to upload. When filtering is unnecessary and the original
// package already satisfies limits, the original path is returned with a no-op
// cleanup. Otherwise a temp zip is written and cleanup removes it.
func prepareSkillZipForHubCenterMarket(srcZip string) (outPath string, cleanup func(), err error) {
	noop := func() {}
	srcZip = strings.TrimSpace(srcZip)
	if srcZip == "" {
		return "", noop, skillPackageErr("PACKAGE_INVALID", "skill package path is empty")
	}

	r, err := zip.OpenReader(srcZip)
	if err != nil {
		return "", noop, skillPackageErr("PACKAGE_INVALID", fmt.Sprintf("open skill package: %v", err))
	}
	defer r.Close()

	// Only file entries are kept. Directory rows are redundant (MkdirAll on
	// extract) and inflate HubCenter entry counts without carrying content.
	kept := make([]keptEntry, 0, len(r.File))
	seen := make(map[string]struct{}, len(r.File))
	var skipped int
	var totalUncompressed int64

	for _, f := range r.File {
		rawName := strings.TrimSpace(f.Name)
		if rawName == "" {
			skipped++
			continue
		}
		if enterpriseSkillPathHasRuntimeArtifact(rawName) {
			skipped++
			continue
		}
		info := f.FileInfo()
		if info.Mode()&os.ModeSymlink != 0 {
			return "", noop, skillPackageErr("PACKAGE_INVALID", fmt.Sprintf("skill package contains symlink %s", rawName))
		}
		slashName := filepath.ToSlash(rawName)
		if info.IsDir() || strings.HasSuffix(slashName, "/") {
			skipped++
			continue
		}

		name := normalizeEnterpriseSkillZipEntryName(rawName)
		if name == "" {
			return "", noop, skillPackageErr("PACKAGE_INVALID", fmt.Sprintf("skill package contains unsafe path %s", rawName))
		}
		if _, dup := seen[name]; dup {
			return "", noop, skillPackageErr("PACKAGE_INVALID", fmt.Sprintf("skill package contains duplicate path %s", name))
		}
		seen[name] = struct{}{}

		usize := f.UncompressedSize64
		if usize > uint64(coreskill.MaxSkillMarketZipSingleFileBytes) {
			return "", noop, skillPackageErr("PACKAGE_TOO_LARGE", fmt.Sprintf(
				"技能包单文件过大：%s 解压后约 %s（上限 %s）。请拆分或压缩资源后重试",
				name, coreskill.FormatSkillByteCount(int64(usize)), coreskill.FormatSkillByteCount(coreskill.MaxSkillMarketZipSingleFileBytes),
			))
		}
		if usize > uint64(coreskill.MaxSkillMarketZipTotalBytes) ||
			uint64(totalUncompressed)+usize > uint64(coreskill.MaxSkillMarketZipTotalBytes) {
			return "", noop, skillPackageErr("PACKAGE_TOO_LARGE", fmt.Sprintf(
				"技能包总体积过大：过滤运行时目录后解压体积将超过上限 %s。必要资源可多文件，但总体积需在限制内",
				coreskill.FormatSkillByteCount(coreskill.MaxSkillMarketZipTotalBytes),
			))
		}
		totalUncompressed += int64(usize)
		kept = append(kept, keptEntry{file: f, name: name})
	}

	if len(kept) == 0 {
		return "", noop, skillPackageErr("PACKAGE_INVALID",
			"技能包在排除运行时目录后没有可上传文件（可能只含 node_modules/.git/venv/__MACOSX 等）。请放入 skill.yaml 与业务资源后重试")
	}

	maxEntries := coreskill.MaxSkillMarketZipEntries
	if len(kept) > maxEntries {
		return "", noop, skillPackageErr("PACKAGE_TOO_MANY_ENTRIES", fmt.Sprintf(
			"技能包文件过多：过滤运行时/空目录后仍有 %d 个文件（防滥用上限 %d）。总体积虽可能未超限，但文件数过高会拖垮解压；请合并资源或移除 node_modules/.git/venv 后重试（原始 zip %d 条目，已排除 %d 条）",
			len(kept), maxEntries, len(r.File), skipped,
		))
	}
	if skipped == 0 {
		// Already clean (files only, no runtime junk, names already safe) — stream original.
		return srcZip, noop, nil
	}

	tmp, err := os.CreateTemp(filepath.Dir(srcZip), "hubcenter-upload-*.zip")
	if err != nil {
		tmp, err = os.CreateTemp("", "hubcenter-upload-*.zip")
		if err != nil {
			return "", noop, fmt.Errorf("create filtered skill package: %w", err)
		}
	}
	tmpPath := tmp.Name()
	cleanup = func() { _ = os.Remove(tmpPath) }

	if err := writeFilteredSkillZip(tmp, kept); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", noop, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", noop, fmt.Errorf("sync filtered skill package: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", noop, fmt.Errorf("close filtered skill package: %w", err)
	}
	return tmpPath, cleanup, nil
}

func writeFilteredSkillZip(out *os.File, files []keptEntry) error {
	zw := zip.NewWriter(out)
	fail := func(err error) error {
		_ = zw.Close()
		return err
	}

	for _, entry := range files {
		f := entry.file
		name := entry.name
		if name == "" || strings.HasSuffix(name, "/") {
			continue
		}
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			return fail(skillPackageErr("PACKAGE_INVALID", fmt.Sprintf("skill package contains symlink %s", name)))
		}

		// Fresh header: never reuse source CRC/CompressedSize/Flags.
		hdr := &zip.FileHeader{
			Name:   name,
			Method: zipMethodForName(name),
		}
		mode := f.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		hdr.SetMode(mode)
		if mt := f.Modified; !mt.IsZero() {
			hdr.Modified = mt
		}

		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return fail(fmt.Errorf("write zip entry %s: %w", name, err))
		}
		rc, err := f.Open()
		if err != nil {
			return fail(fmt.Errorf("open zip entry %s: %w", name, err))
		}
		written, copyErr := io.Copy(w, io.LimitReader(rc, coreskill.MaxSkillMarketZipSingleFileBytes+1))
		_ = rc.Close()
		if copyErr != nil {
			return fail(fmt.Errorf("copy zip entry %s: %w", name, copyErr))
		}
		if written > coreskill.MaxSkillMarketZipSingleFileBytes {
			return fail(skillPackageErr("PACKAGE_TOO_LARGE", fmt.Sprintf(
				"技能包单文件过大：%s 实际解压超过上限 %s",
				name, coreskill.FormatSkillByteCount(coreskill.MaxSkillMarketZipSingleFileBytes),
			)))
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("finalize filtered skill package: %w", err)
	}
	return nil
}

// zipMethodForName picks Store for already-compressed formats to avoid
// re-deflating large binary assets during market re-pack.
func zipMethodForName(name string) uint16 {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".zip", ".gz", ".bz2", ".xz",
		".7z", ".rar", ".mp3", ".mp4", ".woff", ".woff2", ".otf", ".ttf",
		".pdf", ".wasm", ".br":
		return zip.Store
	default:
		return zip.Deflate
	}
}

// countSkillZipEntries returns total zip entries and how many file entries
// would be kept after runtime/dir filtering (tests and diagnostics).
func countSkillZipEntries(zipPath string) (total, kept, skipped int, err error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return 0, 0, 0, err
	}
	defer r.Close()
	total = len(r.File)
	for _, f := range r.File {
		name := strings.TrimSpace(f.Name)
		if name == "" || enterpriseSkillPathHasRuntimeArtifact(name) ||
			f.FileInfo().IsDir() || strings.HasSuffix(filepath.ToSlash(name), "/") {
			skipped++
			continue
		}
		if normalizeEnterpriseSkillZipEntryName(name) == "" {
			return total, 0, 0, fmt.Errorf("skill package contains unsafe path %s", name)
		}
		kept++
	}
	return total, kept, skipped, nil
}
