package guiapp

import (
	"archive/tar"
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	ffmpegPrivateRelease  = "7.1.1"
	maxFFmpegZipBytes     = 150 << 20
	maxFFmpegExtractBytes = 400 << 20
	minFFmpegBinaryBytes  = 1 << 20
	ffmpegArchivePartial  = "ffmpeg.archive.partial"
)

// fileCompanionWorkLimit is the work-directory cap. Tests may lower it.
var fileCompanionWorkLimit int64 = 512 << 20

// ffmpegRelease is one pinned archive. Sizes are measured bytes.
type ffmpegRelease struct {
	GOOS      string
	GOARCH    string
	URL       string
	Size      int64
	Extracted int64
	URL2      string
	Size2     int64
	Extract2  int64
}

func fileCompanionFFmpegArchives() []ffmpegRelease {
	const btbN = "https://github.com/BtbN/FFmpeg-Builds/releases/download/autobuild-2025-03-31-12-55/"
	return []ffmpegRelease{
		{GOOS: "windows", GOARCH: "amd64", URL: "https://github.com/GyanD/codexffmpeg/releases/download/7.1.1/ffmpeg-7.1.1-essentials_build.zip", Size: 92234348, Extracted: 273024520},
		{GOOS: "windows", GOARCH: "arm64", URL: btbN + "ffmpeg-n7.1.1-5-g276bd388f3-winarm64-gpl-7.1.zip", Size: 104085939},
		{GOOS: "linux", GOARCH: "amd64", URL: btbN + "ffmpeg-n7.1.1-5-g276bd388f3-linux64-gpl-7.1.tar.xz", Size: 112892776, Extracted: 417289010},
		{GOOS: "linux", GOARCH: "arm64", URL: btbN + "ffmpeg-n7.1.1-5-g276bd388f3-linuxarm64-gpl-7.1.tar.xz", Size: 97620368},
		{GOOS: "darwin", GOARCH: "amd64", URL: "https://evermeet.cx/ffmpeg/ffmpeg-7.1.1.zip", Size: 25458015, Extracted: 79612272, URL2: "https://evermeet.cx/ffmpeg/ffprobe-7.1.1.zip", Size2: 25397160, Extract2: 79521376},
		{GOOS: "darwin", GOARCH: "arm64", URL: "https://www.osxexperts.net/ffmpeg711arm.zip", Size: 21408583, Extracted: 50268776, URL2: "https://www.osxexperts.net/ffprobe711arm.zip", Size2: 21349340, Extract2: 50177256},
	}
}

func fileCompanionFFmpegArchive(goos, goarch string) (ffmpegRelease, bool) {
	for _, item := range fileCompanionFFmpegArchives() {
		if item.GOOS == goos && item.GOARCH == goarch {
			return item, true
		}
	}
	return ffmpegRelease{}, false
}

// fileCompanionFFmpegLookHook replaces LookPath in tests.
var fileCompanionFFmpegLookHook func(name string) (string, error)

// fileCompanionFFmpegFetchHook replaces the archive download. Tests fail it
// without touching the network.
var fileCompanionFFmpegFetchHook func(ctx context.Context, url string) ([]byte, error)

// fileCompanionWingetHook replaces the Windows winget fallback.
var fileCompanionWingetHook func(ctx context.Context) error

var ffmpegPositiveLocate struct {
	mu            sync.Mutex
	ffmpeg, probe string
}

func fileCompanionLookBinary(name string) (string, error) {
	if fileCompanionFFmpegLookHook != nil {
		return fileCompanionFFmpegLookHook(name)
	}
	return exec.LookPath(name)
}

func fileCompanionBinaryNames() (string, string) {
	if runtime.GOOS == "windows" {
		return "ffmpeg.exe", "ffprobe.exe"
	}
	return "ffmpeg", "ffprobe"
}

type fileCompanionFFmpegMedia struct {
	app *App
}

func (m *fileCompanionFFmpegMedia) Locate() (string, string, bool) {
	ffmpegPositiveLocate.mu.Lock()
	cachedF, cachedP := ffmpegPositiveLocate.ffmpeg, ffmpegPositiveLocate.probe
	ffmpegPositiveLocate.mu.Unlock()
	if cachedF != "" && cachedP != "" && fileNonEmpty(cachedF) && fileNonEmpty(cachedP) {
		return cachedF, cachedP, true
	}
	ff, ffErr := fileCompanionLookBinary("ffmpeg")
	fp, fpErr := fileCompanionLookBinary("ffprobe")
	if ffErr == nil && fpErr == nil && fileNonEmpty(ff) && fileNonEmpty(fp) {
		fileCompanionRememberFFmpeg(ff, fp)
		return ff, fp, true
	}
	if m == nil || m.app == nil {
		return "", "", false
	}
	dir := filepath.Join(m.app.GetDataDir(), "tools", "ffmpeg")
	ffName, fpName := fileCompanionBinaryNames()
	ff = filepath.Join(dir, ffName)
	fp = filepath.Join(dir, fpName)
	// The archive layout keeps the binaries under bin/.
	if !fileAtLeast(ff, minFFmpegBinaryBytes) {
		ff = filepath.Join(dir, "bin", ffName)
	}
	if !fileAtLeast(fp, minFFmpegBinaryBytes) {
		fp = filepath.Join(dir, "bin", fpName)
	}
	if fileAtLeast(ff, minFFmpegBinaryBytes) && fileAtLeast(fp, minFFmpegBinaryBytes) {
		absF, _ := filepath.Abs(ff)
		absP, _ := filepath.Abs(fp)
		if absF == "" {
			absF = ff
		}
		if absP == "" {
			absP = fp
		}
		fileCompanionRememberFFmpeg(absF, absP)
		return absF, absP, true
	}
	return "", "", false
}

func fileCompanionRememberFFmpeg(ffmpeg, probe string) {
	ffmpegPositiveLocate.mu.Lock()
	ffmpegPositiveLocate.ffmpeg = ffmpeg
	ffmpegPositiveLocate.probe = probe
	ffmpegPositiveLocate.mu.Unlock()
}

func fileCompanionWorkBytes(dir string) int64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}

func fileCompanionWorkOverLimit(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	return fileCompanionWorkBytes(dir) > fileCompanionWorkLimit
}

func fileNonEmpty(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

func fileAtLeast(path string, n int64) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() >= n
}

func (m *fileCompanionFFmpegMedia) Install(ctx context.Context) (string, string, error) {
	if m == nil || m.app == nil {
		return "", "", errors.New("app unavailable")
	}
	if err := m.app.fileCompanionInstallPrivateFFmpeg(ctx); err != nil {
		return "", "", err
	}
	ff, fp, ok := m.Locate()
	if !ok {
		return "", "", errors.New("ffmpeg missing")
	}
	return ff, fp, nil
}

func (a *App) fileCompanionInstallPrivateFFmpeg(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	rel, ok := fileCompanionFFmpegArchive(runtime.GOOS, runtime.GOARCH)
	if !ok {
		return errors.New("no ffmpeg archive for this architecture")
	}
	tools := filepath.Join(a.GetDataDir(), "tools")
	if err := os.MkdirAll(tools, 0o755); err != nil {
		return err
	}
	partial := filepath.Join(tools, ffmpegArchivePartial)
	tmp := filepath.Join(tools, "ffmpeg.tmp")
	final := filepath.Join(tools, "ffmpeg")
	cleanup := func() {
		_ = os.RemoveAll(partial)
		_ = os.RemoveAll(tmp)
	}
	fail := func(err error) error {
		cleanup()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if runtime.GOOS == "windows" {
			if wingErr := fileCompanionInstallFFmpegWinget(ctx); wingErr == nil {
				if _, _, ok := (&fileCompanionFFmpegMedia{app: a}).Locate(); ok {
					return nil
				}
			}
		}
		return err
	}
	body, err := fileCompanionFetchFFmpeg(ctx, rel.URL)
	if err != nil {
		return fail(err)
	}
	if int64(len(body)) > maxFFmpegZipBytes || (rel.Size > 0 && int64(len(body)) > maxFFmpegZipBytes) {
		logFileCompanion("install 校验未通过")
		return fail(errors.New("ffmpeg archive too large"))
	}
	if err := os.WriteFile(partial, body, 0o644); err != nil {
		return fail(err)
	}
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return fail(err)
	}
	if err := fileCompanionExtractFFmpeg(ctx, partial, tmp, rel); err != nil {
		logFileCompanion("install 校验未通过")
		return fail(err)
	}
	if rel.URL2 != "" {
		second, err2 := fileCompanionFetchFFmpeg(ctx, rel.URL2)
		if err2 != nil {
			return fail(err2)
		}
		secondPartial := partial + ".2"
		if err := os.WriteFile(secondPartial, second, 0o644); err != nil {
			_ = os.Remove(secondPartial)
			return fail(err)
		}
		if err := fileCompanionExtractFFmpeg(ctx, secondPartial, tmp, ffmpegRelease{URL: rel.URL2}); err != nil {
			_ = os.Remove(secondPartial)
			logFileCompanion("install 校验未通过")
			return fail(err)
		}
		_ = os.Remove(secondPartial)
	}
	ffName, fpName := fileCompanionBinaryNames()
	ff, fp := findNamedBinaries(tmp, ffName, fpName)
	if !fileAtLeast(ff, minFFmpegBinaryBytes) || !fileAtLeast(fp, minFFmpegBinaryBytes) {
		logFileCompanion("install 校验未通过")
		return fail(errors.New("ffmpeg binaries missing"))
	}
	_ = os.RemoveAll(final)
	if err := os.Rename(tmp, final); err != nil {
		return fail(err)
	}
	_ = os.Remove(partial)
	return nil
}

func fileCompanionFetchFFmpeg(ctx context.Context, url string) ([]byte, error) {
	if fileCompanionFFmpegFetchHook != nil {
		return fileCompanionFFmpegFetchHook(ctx, url)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		logFileCompanion("install http=%d 校验未通过", resp.StatusCode)
		return nil, errors.New("ffmpeg download failed")
	}
	limited := io.LimitReader(resp.Body, maxFFmpegZipBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxFFmpegZipBytes {
		return nil, errors.New("ffmpeg archive too large")
	}
	return body, nil
}

func fileCompanionInstallFFmpegWinget(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if fileCompanionWingetHook != nil {
		return fileCompanionWingetHook(ctx)
	}
	winget, err := exec.LookPath("winget")
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(wctx, winget, "install", "--id", "Gyan.FFmpeg", "--scope", "user", "--silent", "--disable-interactivity")
	return cmd.Run()
}

func fileCompanionExtractFFmpeg(ctx context.Context, archive, dest string, rel ffmpegRelease) error {
	lower := strings.ToLower(archive + rel.URL)
	if strings.Contains(lower, ".tar.xz") || strings.HasSuffix(strings.ToLower(archive), ".xz") {
		return fileCompanionExtractTarXZ(ctx, archive, dest)
	}
	return fileCompanionExtractZip(archive, dest)
}

func fileCompanionExtractZip(archive, dest string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer reader.Close()
	var total int64
	for _, file := range reader.File {
		name := file.Name
		if !ffmpegArchiveNameSafe(name) {
			return errors.New("ffmpeg archive path refused")
		}
		if strings.HasPrefix(name, "__MACOSX/") {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		if !pathInside(dest, target) {
			return errors.New("ffmpeg archive path refused")
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		total += int64(file.UncompressedSize64)
		if total > maxFFmpegExtractBytes {
			_ = os.RemoveAll(dest)
			return errors.New("ffmpeg extract too large")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			_ = src.Close()
			return err
		}
		_, copyErr := io.Copy(out, io.LimitReader(src, maxFFmpegExtractBytes))
		_ = out.Close()
		_ = src.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

func fileCompanionExtractTarXZ(ctx context.Context, archive, dest string) error {
	xz, err := exec.LookPath("xz")
	if err != nil {
		return errors.New("xz missing")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, xz, "-dc", archive)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	extractErr := fileCompanionExtractTar(stdout, dest)
	waitErr := cmd.Wait()
	if extractErr != nil {
		return extractErr
	}
	return waitErr
}

func fileCompanionExtractTar(r io.Reader, dest string) error {
	tr := tar.NewReader(r)
	var total int64
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := hdr.Name
		if !ffmpegArchiveNameSafe(name) {
			return errors.New("ffmpeg archive path refused")
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		if !pathInside(dest, target) {
			return errors.New("ffmpeg archive path refused")
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			total += hdr.Size
			if total > maxFFmpegExtractBytes {
				_ = os.RemoveAll(dest)
				return errors.New("ffmpeg extract too large")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, io.LimitReader(tr, maxFFmpegExtractBytes))
			_ = out.Close()
			if copyErr != nil {
				return copyErr
			}
		}
	}
}

func ffmpegArchiveNameSafe(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) {
		return false
	}
	if len(name) >= 2 && name[1] == ':' {
		return false
	}
	slash := strings.ReplaceAll(name, `\`, "/")
	for _, part := range strings.Split(slash, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

func pathInside(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func findNamedBinaries(root, ffName, fpName string) (string, string) {
	var ff, fp string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		base := info.Name()
		if strings.EqualFold(base, ffName) {
			ff = path
		}
		if strings.EqualFold(base, fpName) {
			fp = path
		}
		return nil
	})
	return ff, fp
}

func (m *fileCompanionFFmpegMedia) Probe(ctx context.Context, media string) (videoNoteProbeInfo, error) {
	_, probe, ok := m.Locate()
	if !ok {
		return videoNoteProbeInfo{Corrupt: true}, errors.New("ffprobe missing")
	}
	pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(pctx, probe, "-v", "quiet", "-print_format", "json", "-show_format", "-show_streams", "-show_chapters", media)
	out, err := cmd.Output()
	if err != nil {
		return videoNoteProbeInfo{Corrupt: true}, err
	}
	return parseFFprobeJSON(out)
}

func parseFFprobeJSON(raw []byte) (videoNoteProbeInfo, error) {
	var doc struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
		} `json:"streams"`
		Chapters []struct {
			StartTime string `json:"start_time"`
			Tags      struct {
				Title string `json:"title"`
			} `json:"tags"`
		} `json:"chapters"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return videoNoteProbeInfo{Corrupt: true}, err
	}
	info := videoNoteProbeInfo{}
	if doc.Format.Duration != "" {
		if sec, err := strconv.ParseFloat(doc.Format.Duration, 64); err == nil && sec > 0 {
			info.Duration = time.Duration(sec * float64(time.Second))
		}
	}
	for _, stream := range doc.Streams {
		switch stream.CodecType {
		case "audio":
			info.HasAudio = true
		case "subtitle":
			info.HasText = true
		}
	}
	for _, chapter := range doc.Chapters {
		sec, _ := strconv.ParseFloat(chapter.StartTime, 64)
		info.Chapters = append(info.Chapters, videoNoteChapter{
			Start: time.Duration(sec * float64(time.Second)),
			Title: chapter.Tags.Title,
		})
	}
	return info, nil
}

func (m *fileCompanionFFmpegMedia) ExtractWAV(ctx context.Context, media string, start time.Duration, seconds int, dest string) error {
	ff, _, ok := m.Locate()
	if !ok {
		return errors.New("ffmpeg missing")
	}
	if !outputUnderWork(m.app, dest) {
		return errors.New("output outside work dir")
	}
	if seconds < 1 {
		seconds = 1
	}
	ss := formatFFmpegSeconds(start)
	cmd := exec.CommandContext(ctx, ff, "-ss", ss, "-t", itoaNote(seconds), "-i", media, "-vn", "-ac", "1", "-ar", "16000", "-f", "wav", dest)
	return cmd.Run()
}

func (m *fileCompanionFFmpegMedia) ExtractSubs(ctx context.Context, media, dest string) error {
	ff, _, ok := m.Locate()
	if !ok {
		return errors.New("ffmpeg missing")
	}
	if !outputUnderWork(m.app, dest) {
		return errors.New("output outside work dir")
	}
	cmd := exec.CommandContext(ctx, ff, "-i", media, "-map", "0:s:0", dest)
	return cmd.Run()
}

func (m *fileCompanionFFmpegMedia) ExtractGray(ctx context.Context, media string, at time.Duration) ([]byte, error) {
	ff, _, ok := m.Locate()
	if !ok {
		return nil, errors.New("ffmpeg missing")
	}
	cmd := exec.CommandContext(ctx, ff, "-ss", formatFFmpegSeconds(at), "-i", media, "-frames:v", "1", "-vf", "scale=16:16,format=gray", "-f", "rawvideo", "pipe:1")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	if len(out) < 256 {
		return nil, errors.New("short gray frame")
	}
	return out[:256], nil
}

func (m *fileCompanionFFmpegMedia) ExtractJPEG(ctx context.Context, media string, at time.Duration) ([]byte, error) {
	ff, _, ok := m.Locate()
	if !ok {
		return nil, errors.New("ffmpeg missing")
	}
	cmd := exec.CommandContext(ctx, ff, "-ss", formatFFmpegSeconds(at), "-i", media, "-frames:v", "1", "-vf", "scale=768:768:force_original_aspect_ratio=decrease", "-q:v", "5", "-f", "image2pipe", "pipe:1")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return out, nil
}

func outputUnderWork(a *App, dest string) bool {
	if a == nil {
		return false
	}
	root := filepath.Join(a.GetDataDir(), "file-companion", "work")
	return pathInside(root, dest)
}

func formatFFmpegSeconds(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return itoaNote(int(d / time.Second))
}
