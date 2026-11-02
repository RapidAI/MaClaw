package guiapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tinytex"
)

const (
	latexProgressEvent        = "latex-tinytex-progress"
	latexUpdaterProgressEvent = "latex-tinytex-updater"
)

var errLatexDisabled = errors.New("latex tinytex disabled")

// latexRuntime tracks the in-process install so the settings page can show
// progress without a second download starting in parallel.
var latexRuntime struct {
	mu         sync.Mutex
	running    bool
	phase      string
	percent    int
	downloaded int64
	total      int64
	err        string
}

var latexVerifiedCache struct {
	mu         sync.Mutex
	generation uint64
	valid      bool
	dataDir    string
	dist       string
	version    string
}

func invalidateLatexVerifiedCache() {
	latexVerifiedCache.mu.Lock()
	latexVerifiedCache.generation++
	latexVerifiedCache.valid = false
	latexVerifiedCache.dataDir = ""
	latexVerifiedCache.dist = ""
	latexVerifiedCache.version = ""
	latexVerifiedCache.mu.Unlock()
}

func latexVerifiedTree(dataDir string) (dist, version string, ready bool) {
	latexVerifiedCache.mu.Lock()
	if latexVerifiedCache.valid && latexVerifiedCache.dataDir == dataDir {
		dist, version = latexVerifiedCache.dist, latexVerifiedCache.version
		latexVerifiedCache.mu.Unlock()
		return dist, version, true
	}
	generation := latexVerifiedCache.generation
	latexVerifiedCache.mu.Unlock()

	dist, _ = tinytex.FindDistRoot(tinytex.DistDir(dataDir))
	ready = tinytex.Verified(dist)
	if ready {
		version, _ = tinytex.ReadStamp(dist)
		latexVerifiedCache.mu.Lock()
		if latexVerifiedCache.generation == generation {
			latexVerifiedCache.valid = true
			latexVerifiedCache.dataDir = dataDir
			latexVerifiedCache.dist = dist
			latexVerifiedCache.version = version
		}
		latexVerifiedCache.mu.Unlock()
	}
	return dist, version, ready
}

// GetLatexTinyTeXStatus reports whether scheme-small is verified and, if an
// install is running, which step it is on.
func (a *App) GetLatexTinyTeXStatus() map[string]interface{} {
	dataDir := ""
	if a != nil {
		dataDir = a.GetDataDir()
	}
	dist, version, ready := latexVerifiedTree(dataDir)
	source, _ := tinytex.OfficialAssetURL(runtime.GOOS, runtime.GOARCH)

	latexRuntime.mu.Lock()
	phase := latexRuntime.phase
	running := latexRuntime.running
	percent := latexRuntime.percent
	downloaded := latexRuntime.downloaded
	total := latexRuntime.total
	errMsg := latexRuntime.err
	latexRuntime.mu.Unlock()

	if ready && !running {
		phase = "ready"
		percent = 100
		errMsg = ""
	} else if phase == "" {
		phase = "idle"
	}
	installDir := dist
	if installDir == "" {
		installDir = tinytex.InstallRoot(dataDir)
	}
	return map[string]interface{}{
		"enabled":     a.latexTinyTeXEnabled(),
		"ready":       ready,
		"phase":       phase,
		"percent":     percent,
		"downloaded":  downloaded,
		"total":       total,
		"error":       errMsg,
		"version":     version,
		"install_dir": installDir,
		"scheme":      tinytex.Scheme,
		"bundle":      tinytex.Bundle,
		"source":      source,
	}
}

// SetLatexTinyTeXEnabled persists the switch. Turning it on starts the
// background download when scheme-small is not verified yet.
func (a *App) SetLatexTinyTeXEnabled(enabled bool) error {
	if _, err := a.PatchConfigFields(map[string]interface{}{"latex_tinytex_enabled": enabled}); err != nil {
		return err
	}
	if enabled {
		go a.ensureLatexTinyTeX()
	}
	return nil
}

// DownloadLatexTinyTeX retries the official TinyTeX-0 download and the
// scheme-small install. It returns immediately; progress is reported on
// latex-tinytex-progress and GetLatexTinyTeXStatus.
func (a *App) DownloadLatexTinyTeX() error {
	if !a.latexTinyTeXEnabled() {
		return a.SetLatexTinyTeXEnabled(true)
	}
	go func() {
		if err := a.installLatexTinyTeX(); err != nil && !errors.Is(err, errLatexDisabled) {
			log.Printf("[latex] install failed: %v", err)
		}
	}()
	return nil
}

// ensureLatexTinyTeX runs once at startup. testHomeDir installs never download.
func (a *App) ensureLatexTinyTeX() {
	if a == nil || a.testHomeDir != "" {
		return
	}
	if !a.latexTinyTeXEnabled() {
		log.Printf("[latex] auto-install skipped: disabled")
		return
	}
	if a.latexTreeVerified() {
		log.Printf("[latex] scheme-small already verified")
		return
	}
	log.Printf("[latex] scheme-small missing; downloading official TinyTeX-0 and installing scheme-small")
	if err := a.installLatexTinyTeX(); err != nil && !errors.Is(err, errLatexDisabled) {
		log.Printf("[latex] install failed: %v", err)
	}
}

func (a *App) latexTinyTeXEnabled() bool {
	if a == nil {
		return true
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return true
	}
	return cfg.LatexTinyTeXEnabled
}

func (a *App) latexUseChinaMirror() bool {
	lang := ""
	if a != nil {
		lang = a.CurrentLanguage
	}
	if strings.TrimSpace(lang) == "" && a != nil {
		if cfg, err := a.LoadConfig(); err == nil {
			lang = cfg.Language
		}
	}
	return normalizeAppLanguageKind(lang).IsChinese()
}

func (a *App) latexTreeVerified() bool {
	if a == nil {
		return false
	}
	_, _, ready := latexVerifiedTree(a.GetDataDir())
	return ready
}

func (a *App) installLatexTinyTeX() error {
	latexRuntime.mu.Lock()
	if latexRuntime.running {
		latexRuntime.mu.Unlock()
		return nil
	}
	latexRuntime.running = true
	latexRuntime.err = ""
	latexRuntime.mu.Unlock()
	invalidateLatexVerifiedCache()
	defer func() {
		invalidateLatexVerifiedCache()
		latexRuntime.mu.Lock()
		latexRuntime.running = false
		latexRuntime.mu.Unlock()
	}()

	if !a.latexStillEnabled() {
		return errLatexDisabled
	}
	if a.latexTreeVerified() {
		a.setLatexPhase("ready", 100, 0, 0, "")
		return nil
	}

	dataDir := a.GetDataDir()
	distDir := tinytex.DistDir(dataDir)
	dist, _ := tinytex.FindDistRoot(distDir)
	if !tinytex.EnginePresent(dist) {
		if err := a.downloadAndExtractTinyTeX(dataDir); err != nil {
			return err
		}
		var err error
		dist, err = tinytex.FindDistRoot(distDir)
		if err != nil {
			a.setLatexPhase("error", 0, 0, 0, err.Error())
			return err
		}
	}
	if !a.latexStillEnabled() {
		a.setLatexPhase("idle", 0, 0, 0, "")
		return errLatexDisabled
	}
	if !tinytex.ArticlePresent(dist) {
		if err := a.installTinyTeXScheme(dist); err != nil {
			return err
		}
	}
	if !a.latexStillEnabled() {
		a.setLatexPhase("idle", 0, 0, 0, "")
		return errLatexDisabled
	}
	return a.verifyTinyTeXScheme(dist)
}

func (a *App) latexStillEnabled() bool {
	return a.latexTinyTeXEnabled()
}

func (a *App) downloadAndExtractTinyTeX(dataDir string) error {
	name, err := tinytex.AssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		a.setLatexPhase("error", 0, 0, 0, err.Error())
		return err
	}
	urls, err := tinytex.DownloadURLs(runtime.GOOS, runtime.GOARCH, a.latexUseChinaMirror())
	if err != nil {
		a.setLatexPhase("error", 0, 0, 0, err.Error())
		return err
	}
	cacheDir := filepath.Join(tinytex.InstallRoot(dataDir), "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		a.setLatexPhase("error", 0, 0, 0, err.Error())
		return err
	}
	archive := filepath.Join(cacheDir, name)
	a.setLatexPhase("downloading", 0, 0, 0, "")

	var last error
	for i, rawURL := range urls {
		if !a.latexStillEnabled() {
			a.setLatexPhase("idle", 0, 0, 0, "")
			return errLatexDisabled
		}
		if i > 0 {
			_ = os.Remove(archive + ".tmp")
			_ = os.Remove(archive)
		}
		log.Printf("[latex] downloading %s", rawURL)
		err := a.downloadModelFromWithEvent(rawURL, archive, i == len(urls)-1, latexProgressEvent)
		if err != nil {
			last = err
			log.Printf("[latex] download failed: %v", err)
			continue
		}
		if err := tinytex.ArchiveOK(archive); err != nil {
			last = err
			log.Printf("[latex] reject archive from %s: %v", rawURL, err)
			_ = os.Remove(archive)
			continue
		}
		last = nil
		break
	}
	if last != nil {
		msg := last.Error()
		a.setLatexPhase("error", 0, 0, 0, msg)
		return last
	}

	if !a.latexStillEnabled() {
		a.setLatexPhase("idle", 0, 0, 0, "")
		return errLatexDisabled
	}
	a.setLatexPhase("extracting", 0, 0, 0, "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := tinytex.Extract(ctx, archive, tinytex.DistDir(dataDir)); err != nil {
		a.setLatexPhase("error", 0, 0, 0, err.Error())
		return err
	}
	return nil
}

func (a *App) installTinyTeXScheme(dist string) error {
	tlmgr, err := tinytex.FindTlmgr(dist)
	if err != nil {
		a.setLatexPhase("error", 0, 0, 0, err.Error())
		return err
	}
	a.setLatexPhase("installing", 0, 0, 0, "")
	var last error
	for _, repo := range tinytex.CTANRepositories(a.latexUseChinaMirror()) {
		if !a.latexStillEnabled() {
			a.setLatexPhase("idle", 0, 0, 0, "")
			return errLatexDisabled
		}
		repo = a.pinTLNet(repo)
		log.Printf("[latex] tlmgr repository %s", repo)
		if err := a.tlmgrSetRepository(tlmgr, repo); err != nil {
			last = err
			log.Printf("[latex] %v", last)
			if latexSchemeBlocked(err) {
				break
			}
			continue
		}
		// TinyTeX-0 freezes tlmgr at the daily build. A newer tlnet refuses
		// `tlmgr install` (Windows perl dies with status 255) until
		// `tlmgr update --self` has run. On Windows that self-update only
		// lands when tlmgr.bat runs temp\updater-w32 after perl exits, so
		// this call has to go through the batch launcher FindTlmgr returns.
		if err := a.ensureTlmgrCurrent(dist, &tlmgr, repo); err != nil {
			last = err
			log.Printf("[latex] %v", last)
			if latexSchemeBlocked(err) {
				break
			}
			continue
		}
		if err := a.tlmgrInstallScheme(dist, tlmgr, repo); err != nil {
			last = err
			log.Printf("[latex] %v", last)
			if latexSchemeBlocked(err) {
				break
			}
			// Classify the tlmgr transcript. The error string keeps only a
			// tail for the settings panel, and that tail can lose the banner.
			recovery := tinytex.ClassifyTlmgrInstallFailure(latexTlmgrOutput(err))
			if recovery == tinytex.TlmgrNextMirror {
				continue
			}
			// Self-update reported success but install is still blocked.
			// The usual cause is a Windows infra update that did not apply,
			// or a redirector that moved between the two calls.
			log.Printf("[latex] scheme install still blocked; running official tlmgr updater")
			if rec := a.recoverTlmgr(context.Background(), dist, tlmgr, repo, recovery == tinytex.TlmgrOfficialUpgrade, true); rec != nil {
				last = rec
				log.Printf("[latex] %v", last)
				if latexSchemeBlocked(rec) {
					break
				}
				continue
			}
			if again, findErr := tinytex.FindTlmgr(dist); findErr == nil {
				tlmgr = again
			}
			if err := a.tlmgrInstallScheme(dist, tlmgr, repo); err != nil {
				last = err
				log.Printf("[latex] %v", last)
				if latexSchemeBlocked(err) {
					break
				}
				continue
			}
		}
		return nil
	}
	if last == nil {
		last = fmt.Errorf("no CTAN repository available")
	}
	a.setLatexPhase("error", 0, 0, 0, last.Error())
	return last
}

func (a *App) pinTLNet(repo string) string {
	ctx, cancel := context.WithTimeout(context.Background(), tinytex.TLNetResolveTimeout)
	defer cancel()
	pinned := strings.TrimRight(strings.TrimSpace(resolveLatexRepo(ctx, repo)), "/")
	if pinned == "" {
		return repo
	}
	if pinned != repo {
		log.Printf("[latex] tlmgr repository %s resolved to %s", repo, pinned)
	}
	return pinned
}

func (a *App) tlmgrSetRepository(tlmgr, repo string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	out, err := invokeLatexTool(ctx, tlmgr, "", nil, "option", "repository", repo)
	if err != nil {
		return fmt.Errorf("set repository %s: %w: %s", repo, err, clipLatexOutput(out))
	}
	return nil
}

func (a *App) ensureTlmgrCurrent(dist string, tlmgr *string, repo string) error {
	a.setLatexPhase("updating", 0, 0, 0, "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	out, err := invokeLatexTool(ctx, *tlmgr, "", nil, "update", "--self")
	cancel()
	switch tinytex.ClassifyTlmgrSelfUpdate(err, string(out)) {
	case tinytex.TlmgrReady:
		if again, findErr := tinytex.FindTlmgr(dist); findErr == nil {
			*tlmgr = again
		}
		return nil
	case tinytex.TlmgrOfficialUpgrade:
		log.Printf("[latex] TeX Live year is behind %s; official updater --upgrade", repo)
		if rec := a.recoverTlmgr(context.Background(), dist, *tlmgr, repo, true, true); rec != nil {
			return rec
		}
	case tinytex.TlmgrOfficialUpdater:
		log.Printf("[latex] tlmgr update --self cannot continue against %s; official updater", repo)
		if rec := a.recoverTlmgr(context.Background(), dist, *tlmgr, repo, false, true); rec != nil {
			return rec
		}
	default:
		return fmt.Errorf("tlmgr update --self from %s: %w: %s", repo, err, clipLatexOutput(out))
	}
	if again, findErr := tinytex.FindTlmgr(dist); findErr == nil {
		*tlmgr = again
	}
	return nil
}

func (a *App) tlmgrInstallScheme(dist, tlmgr, repo string) error {
	a.setLatexPhase("installing", 0, 0, 0, "")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	out, err := invokeLatexTool(ctx, tlmgr, "", nil, "install", tinytex.Scheme)
	if err != nil {
		return &latexCommandError{
			msg:    fmt.Sprintf("install %s from %s: %v: %s", tinytex.Scheme, repo, err, clipLatexOutput(out)),
			output: string(out),
			err:    err,
		}
	}
	if tinytex.ArticlePresent(dist) {
		return nil
	}
	return fmt.Errorf("%s install from %s did not provide article.cls", tinytex.Scheme, repo)
}

// useOfficialTlmgrUpdater makes InstallPackage use the same archive as the
// scheme installer. 编译预览 and build_verify both reach InstallPackage.
func (a *App) useOfficialTlmgrUpdater() {
	if a == nil {
		return
	}
	tinytex.SetApplyOfficialUpdater(func(ctx context.Context, dist, tlmgr, repo string, upgrade bool) error {
		// Package installs must not move the settings-page phase. That page
		// follows latexRuntime, which this call does not own.
		return a.recoverTlmgr(ctx, dist, tlmgr, repo, upgrade, false)
	})
}

// recoverTlmgr runs the official update-tlmgr-latest archive from repo.
// Windows locks the running perl and batch files, so a failed in-tree
// update cannot replace them. The archive locates this installation with
// kpsewhich, which has to resolve to TinyTeX's own bin directory.
// ctx bounds the download and the run. A nil ctx gets 30 minutes; a shorter
// parent, such as 编译预览, still ends at that parent. announce is set for
// the settings installer. A package install leaves the settings phase alone.
func (a *App) recoverTlmgr(ctx context.Context, dist, tlmgrPath, repo string, upgrade, announce bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	url, err := tinytex.TlmgrUpdaterURL(repo, runtime.GOOS)
	if err != nil {
		return err
	}
	if announce {
		// The download is not the scheme install. Keep the settings panel on
		// the tlmgr update phase while the archive is fetched and applied.
		a.setLatexPhase("updating", 0, 0, 0, "")
	}
	// dist/temp is where tlmgr.bat keeps updater-w32. The official archive
	// must not live there: a leftover next to that script is what makes the
	// batch file refuse to start. The parent of dist stays outside the tree
	// whether the root is dist itself or dist/TinyTeX.
	cacheDir := filepath.Join(filepath.Dir(dist), "maclaw-tlmgr-updater")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(cacheDir, tinytex.TlmgrUpdaterName(runtime.GOOS))
	defer func() {
		_ = os.Remove(dest)
		_ = os.Remove(dest + ".tmp")
		_ = os.Remove(cacheDir)
	}()
	_ = os.Remove(dest)
	_ = os.Remove(dest + ".tmp")
	log.Printf("[latex] downloading %s", url)
	if err := fetchLatexFile(a, ctx, url, dest); err != nil {
		return fmt.Errorf("download tlmgr updater from %s: %w", repo, err)
	}
	if err := tinytex.UpdaterOK(dest); err != nil {
		_ = os.Remove(dest)
		return fmt.Errorf("tlmgr updater from %s: %w", repo, err)
	}
	binDir := filepath.Dir(tlmgrPath)
	perlDir := filepath.Join(dist, "tlpkg", "tlperl", "bin")
	args := tinytex.TlmgrUpdaterArgs(upgrade)
	var out []byte
	if runtime.GOOS == "windows" {
		out, err = invokeLatexTool(ctx, dest, dist, []string{binDir, perlDir}, args...)
	} else {
		out, err = invokeLatexTool(ctx, "sh", dist, []string{binDir}, append([]string{dest}, args...)...)
	}
	if err != nil {
		return fmt.Errorf("tlmgr updater from %s: %w: %s", repo, err, clipLatexOutput(out))
	}
	return nil
}

func (a *App) verifyTinyTeXScheme(dist string) error {
	a.setLatexPhase("verifying", 0, 0, 0, "")
	if !tinytex.ArticlePresent(dist) {
		err := fmt.Errorf("%s is missing %s", tinytex.Scheme, tinytex.ArticleClassPath(dist))
		a.setLatexPhase("error", 0, 0, 0, err.Error())
		return err
	}
	engine, err := tinytex.FindEngine(dist)
	if err != nil {
		a.setLatexPhase("error", 0, 0, 0, err.Error())
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := runLatexTool(ctx, engine, "--version")
	if err != nil {
		msg := fmt.Errorf("xelatex --version: %w: %s", err, clipLatexOutput(out))
		a.setLatexPhase("error", 0, 0, 0, msg.Error())
		return msg
	}
	version := firstLatexLine(string(out))
	if version == "" {
		err := fmt.Errorf("xelatex --version returned no output")
		a.setLatexPhase("error", 0, 0, 0, err.Error())
		return err
	}
	if err := tinytex.WriteStamp(dist, version); err != nil {
		a.setLatexPhase("error", 0, 0, 0, err.Error())
		return err
	}
	log.Printf("[latex] verified %s at %s (%s)", tinytex.Scheme, dist, version)
	a.setLatexPhase("ready", 100, 0, 0, "")
	return nil
}

func (a *App) setLatexPhase(phase string, pct int, downloaded, total int64, errMsg string) {
	latexRuntime.mu.Lock()
	latexRuntime.phase = phase
	latexRuntime.percent = pct
	// A phase change passes 0,0. Keeping the previous archive size would make
	// the next poll paint the TinyTeX-0 bar onto "Updating tlmgr…".
	latexRuntime.downloaded = downloaded
	latexRuntime.total = total
	latexRuntime.err = errMsg
	latexRuntime.mu.Unlock()
	if a == nil {
		return
	}
	a.emitEvent(latexProgressEvent, map[string]interface{}{
		"phase":      phase,
		"percent":    pct,
		"downloaded": downloaded,
		"total":      total,
		"error":      errMsg,
	})
}

// noteLatexDownloadProgress folds a TinyTeX-0 or official-updater byte count
// into the settings install. The panel polls latexRuntime and listens only to
// latex-tinytex-progress. 编译预览 and build_verify leave the phase on ready,
// so their updater download does not move that panel.
func (a *App) noteLatexDownloadProgress(pct int, downloaded, total int64, errMsg string) {
	if a == nil {
		return
	}
	latexRuntime.mu.Lock()
	phase := latexRuntime.phase
	track := phase == "downloading" || phase == "updating"
	if track && errMsg == "" {
		latexRuntime.percent = pct
		latexRuntime.downloaded = downloaded
		latexRuntime.total = total
	}
	latexRuntime.mu.Unlock()
	if !track {
		return
	}
	if errMsg != "" {
		a.setLatexPhase("error", pct, downloaded, total, errMsg)
		return
	}
	a.emitEvent(latexProgressEvent, map[string]interface{}{
		"phase":      phase,
		"percent":    pct,
		"downloaded": downloaded,
		"total":      total,
		"error":      "",
	})
}

func runLatexTool(ctx context.Context, bin string, args ...string) ([]byte, error) {
	return runLatexToolPath(ctx, bin, "", nil, args...)
}

func runLatexToolPath(ctx context.Context, bin, dir string, prepend []string, args ...string) ([]byte, error) {
	out, code, err := tinytex.RunCommandWithPath(ctx, bin, dir, prepend, args...)
	if err != nil {
		return []byte(out), err
	}
	if code != 0 {
		return []byte(out), fmt.Errorf("exit status %d", code)
	}
	return []byte(out), nil
}

// invokeLatexTool, fetchLatexFile, and resolveLatexRepo are the process,
// download, and mirror-pin seams. Tests replace them; the defaults run the
// real tlmgr, the shared downloader, and one redirect lookup.
var invokeLatexTool = runLatexToolPath

var fetchLatexFile = func(a *App, ctx context.Context, url, dest string) error {
	return a.downloadModelFromWithEventContext(ctx, url, dest, false, latexUpdaterProgressEvent)
}

var resolveLatexRepo = tinytex.ResolveTLNet

// latexCommandError keeps the full tlmgr transcript. Error is the clipped
// line shown in settings; TlmgrOutput is what the recovery classifier reads.
type latexCommandError struct {
	msg    string
	output string
	err    error
}

func (e *latexCommandError) Error() string {
	if e == nil {
		return ""
	}
	return e.msg
}

func (e *latexCommandError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func (e *latexCommandError) TlmgrOutput() string {
	if e == nil {
		return ""
	}
	return e.output
}

// latexSchemeBlocked reports a local updater script that the next mirror
// cannot move. tlmgr.bat exits before perl when temp\updater-w32 remains,
// and update --self fails at the rename when updater-w32.bat remains.
func latexSchemeBlocked(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, tinytex.ErrStaleUpdater) {
		return true
	}
	return tinytex.TlmgrOutputStaleUpdater(latexTlmgrOutput(err))
}

func latexTlmgrOutput(err error) string {
	var carrier interface{ TlmgrOutput() string }
	if errors.As(err, &carrier) && carrier != nil && carrier.TlmgrOutput() != "" {
		return carrier.TlmgrOutput()
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

func firstLatexLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func clipLatexOutput(b []byte) string {
	s := strings.TrimSpace(string(b))
	const limit = 800
	if len(s) <= limit {
		return s
	}
	return s[len(s)-limit:]
}
