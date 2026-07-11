package httpapi

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	coreskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// prepareSkillZipForHubCenterMarket rewrites a skill package zip to drop
// runtime/cache artifacts (node_modules, .git, venv, etc.), then validates
// HubCenter limits with size as the primary gate:
//
//   - total uncompressed size ≤ MaxSkillMarketZipTotalBytes (500 MiB)
//   - single file ≤ MaxSkillMarketZipSingleFileBytes (50 MiB)
//   - entry count ≤ MaxSkillMarketZipEntries (DoS backstop only)
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
		return "", noop, fmt.Errorf("skill package path is empty")
	}

	r, err := zip.OpenReader(srcZip)
	if err != nil {
		return "", noop, fmt.Errorf("open skill package: %w", err)
	}
	defer r.Close()

	kept := make([]*zip.File, 0, len(r.File))
	var skipped int
	var totalUncompressed int64
	for _, f := range r.File {
		name := strings.TrimSpace(f.Name)
		if name == "" {
			skipped++
			continue
		}
		if !enterpriseSkillZipPathAllowed(name) {
			return "", noop, fmt.Errorf("skill package contains unsafe path %s", name)
		}
		if enterpriseSkillPathHasRuntimeArtifact(name) {
			skipped++
			continue
		}
		if !f.FileInfo().IsDir() {
			if f.UncompressedSize64 > uint64(coreskill.MaxSkillMarketZipSingleFileBytes) {
				return "", noop, fmt.Errorf(
					"技能包单文件过大：%s 解压后约 %d 字节（上限 %d）。请拆分或压缩资源后重试",
					name, f.UncompressedSize64, coreskill.MaxSkillMarketZipSingleFileBytes,
				)
			}
			totalUncompressed += int64(f.UncompressedSize64)
			if totalUncompressed > coreskill.MaxSkillMarketZipTotalBytes {
				return "", noop, fmt.Errorf(
					"技能包总体积过大：过滤运行时目录后解压体积约 %d 字节（上限 %d）。必要资源可多文件，但总体积需在限制内",
					totalUncompressed, coreskill.MaxSkillMarketZipTotalBytes,
				)
			}
		}
		kept = append(kept, f)
	}

	// Entry count is a DoS backstop only — not a product “max useful files” cap.
	maxEntries := coreskill.MaxSkillMarketZipEntries
	if len(kept) > maxEntries {
		return "", noop, fmt.Errorf(
			"技能包条目过多：过滤运行时目录后仍有 %d 个条目（防滥用上限 %d）。总体积虽可能未超限，但条目数过高会拖垮解压；请合并资源、去掉无用空目录，或移除 node_modules/.git/venv 后重试（原始 zip %d 条目，已排除运行时 %d 条）",
			len(kept), maxEntries, len(r.File), skipped,
		)
	}
	if skipped == 0 {
		// Already clean and under limits — stream the original file.
		return srcZip, noop, nil
	}

	tmp, err := os.CreateTemp(filepath.Dir(srcZip), "hubcenter-upload-*.zip")
	if err != nil {
		// Fall back to system temp if package dir is not writable.
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
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", noop, fmt.Errorf("close filtered skill package: %w", err)
	}
	return tmpPath, cleanup, nil
}

func writeFilteredSkillZip(out *os.File, files []*zip.File) error {
	zw := zip.NewWriter(out)
	for _, f := range files {
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			_ = zw.Close()
			return fmt.Errorf("skill package contains symlink %s", f.Name)
		}
		hdr := f.FileHeader
		// Normalize to forward slashes; drop data descriptor flags that can
		// confuse some unzippers when we re-stream content.
		hdr.Name = filepath.ToSlash(f.Name)
		hdr.Flags &^= 0x8
		w, err := zw.CreateHeader(&hdr)
		if err != nil {
			_ = zw.Close()
			return fmt.Errorf("write zip entry %s: %w", f.Name, err)
		}
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			_ = zw.Close()
			return fmt.Errorf("open zip entry %s: %w", f.Name, err)
		}
		_, copyErr := io.Copy(w, rc)
		_ = rc.Close()
		if copyErr != nil {
			_ = zw.Close()
			return fmt.Errorf("copy zip entry %s: %w", f.Name, copyErr)
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("finalize filtered skill package: %w", err)
	}
	return nil
}

// countSkillZipEntries returns total zip entries and how many would be kept
// after runtime filtering (used by tests and diagnostics).
func countSkillZipEntries(zipPath string) (total, kept, skipped int, err error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return 0, 0, 0, err
	}
	defer r.Close()
	total = len(r.File)
	for _, f := range r.File {
		if enterpriseSkillPathHasRuntimeArtifact(f.Name) || strings.TrimSpace(f.Name) == "" {
			skipped++
			continue
		}
		if !enterpriseSkillZipPathAllowed(f.Name) {
			return total, 0, 0, fmt.Errorf("skill package contains unsafe path %s", f.Name)
		}
		kept++
	}
	return total, kept, skipped, nil
}
