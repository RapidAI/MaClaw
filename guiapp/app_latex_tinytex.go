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
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const latexProgressEvent = "latex-tinytex-progress"

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

// GetLatexTinyTeXStatus reports whether scheme-small is verified and, if an
// install is running, which step it is on.
func (a *App) GetLatexTinyTeXStatus() map[string]interface{} {
	dataDir := ""
	if a != nil {
		dataDir = a.GetDataDir()
	}
	dist, _ := tinytex.FindDistRoot(tinytex.DistDir(dataDir))
	ready := tinytex.Verified(dist)
	version := ""
	if ready {
		version, _ = tinytex.ReadStamp(dist)
	}
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
	dist, err := tinytex.FindDistRoot(tinytex.DistDir(a.GetDataDir()))
	if err != nil {
		return false
	}
	return tinytex.Verified(dist)
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
	defer func() {
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
		log.Printf("[latex] tlmgr repository %s", repo)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		out, err := runLatexTool(ctx, tlmgr, "option", "repository", repo)
		cancel()
		if err != nil {
			last = fmt.Errorf("set repository %s: %w: %s", repo, err, clipLatexOutput(out))
			log.Printf("[latex] %v", last)
			continue
		}
		ctx, cancel = context.WithTimeout(context.Background(), 60*time.Minute)
		out, err = runLatexTool(ctx, tlmgr, "install", tinytex.Scheme)
		cancel()
		if err != nil {
			last = fmt.Errorf("install %s from %s: %w: %s", tinytex.Scheme, repo, err, clipLatexOutput(out))
			log.Printf("[latex] %v", last)
			continue
		}
		if tinytex.ArticlePresent(dist) {
			return nil
		}
		last = fmt.Errorf("%s install from %s did not provide article.cls", tinytex.Scheme, repo)
		log.Printf("[latex] %v", last)
	}
	if last == nil {
		last = fmt.Errorf("no CTAN repository available")
	}
	a.setLatexPhase("error", 0, 0, 0, last.Error())
	return last
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
	if downloaded > 0 || total > 0 {
		latexRuntime.downloaded = downloaded
		latexRuntime.total = total
	}
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

func runLatexTool(ctx context.Context, bin string, args ...string) ([]byte, error) {
	var cmdName string
	var cmdArgs []string
	if runtime.GOOS == "windows" && strings.EqualFold(filepath.Ext(bin), ".bat") {
		parts := make([]string, 0, len(args)+1)
		parts = append(parts, quoteCmdArg(bin))
		for _, arg := range args {
			parts = append(parts, quoteCmdArg(arg))
		}
		cmdName = tool.ResolveCmdExe()
		cmdArgs = []string{"/d", "/s", "/c", strings.Join(parts, " ")}
	} else {
		cmdName = bin
		cmdArgs = args
	}
	cmd := tool.CommandContext(ctx, cmdName, cmdArgs...)
	cmd.Dir = filepath.Dir(bin)
	binDir := filepath.Dir(bin)
	cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return cmd.CombinedOutput()
}

func quoteCmdArg(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\"") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
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
