package guiapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// uiAppearanceFileName sits beside config.json. The companion process has its
// own WebView profile, so it cannot see the main window's localStorage. Both
// processes read this file. Only the main window writes it.
const uiAppearanceFileName = "ui-appearance.json"

// uiAppearanceEvent is emitted in the companion process when that file changes.
const uiAppearanceEvent = "ui-appearance:changed"

// UIAppearance is the shell theme the main window and the companion window share.
type UIAppearance struct {
	ThemeMode   string `json:"themeMode"`
	DarkScheme  string `json:"darkScheme"`
	LightScheme string `json:"lightScheme"`
}

// uiSchemeToken keeps a palette id that this binary does not know yet.
// Rewriting an unknown id to the default would pin the companion to an old
// palette after the frontend learned a new one. Unsafe text still falls back.
var uiSchemeToken = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,31})?$`)

func uiSchemeOrDefault(id, fallback string) string {
	id = strings.TrimSpace(id)
	if uiSchemeToken.MatchString(id) {
		return id
	}
	return fallback
}

func normalizeUIAppearance(in UIAppearance) (UIAppearance, bool) {
	mode := strings.TrimSpace(in.ThemeMode)
	if mode != "light" && mode != "dark" {
		return UIAppearance{}, false
	}
	return UIAppearance{
		ThemeMode:   mode,
		DarkScheme:  uiSchemeOrDefault(in.DarkScheme, "onyx"),
		LightScheme: uiSchemeOrDefault(in.LightScheme, "fluent"),
	}, true
}

func appearanceWatchRelevant(name string) bool {
	base := filepath.Base(name)
	return base == uiAppearanceFileName || base == uiAppearanceFileName+".tmp"
}

func (a *App) uiAppearancePath() (string, error) {
	if a == nil {
		return "", errors.New("app unavailable")
	}
	configPath, err := a.getConfigPath()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(configPath)
	if dir == "" || dir == "." {
		return "", errors.New("config directory unavailable")
	}
	return filepath.Join(dir, uiAppearanceFileName), nil
}

func (a *App) readUIAppearance() (UIAppearance, bool) {
	path, err := a.uiAppearancePath()
	if err != nil {
		return UIAppearance{}, false
	}
	body, err := os.ReadFile(path)
	if err != nil || len(body) == 0 || len(body) > 4096 {
		return UIAppearance{}, false
	}
	var parsed UIAppearance
	if json.Unmarshal(body, &parsed) != nil {
		return UIAppearance{}, false
	}
	return normalizeUIAppearance(parsed)
}

// GetUIAppearance returns the shared shell theme. An empty ThemeMode means
// the file is missing; callers keep their own stored choice.
func (a *App) GetUIAppearance() UIAppearance {
	appearance, ok := a.readUIAppearance()
	if !ok {
		return UIAppearance{}
	}
	return appearance
}

// uiAppearanceMu keeps a compare-and-write atomic. Two overlapping saves must
// not both observe the old file and then let the older save land last.
var uiAppearanceMu sync.Mutex

// SetUIAppearance stores the shell theme where the companion process can read it.
// An unrecognized mode is refused so a bad call cannot replace a saved dark theme.
func (a *App) SetUIAppearance(next UIAppearance) error {
	if a == nil {
		return errors.New("app unavailable")
	}
	normalized, ok := normalizeUIAppearance(next)
	if !ok {
		return errors.New("invalid appearance")
	}
	uiAppearanceMu.Lock()
	defer uiAppearanceMu.Unlock()
	if current, have := a.readUIAppearance(); have && current == normalized {
		return nil
	}
	path, err := a.uiAppearancePath()
	if err != nil {
		return err
	}
	body, err := json.Marshal(normalized)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return fileCompanionWriteAtomicFile(path, body, 0o600)
}

// startFileCompanionAppearanceWatcher tells this companion window when the
// main window writes a new theme. Events stay inside this process.
func (a *App) startFileCompanionAppearanceWatcher() {
	if a == nil {
		return
	}
	st := a.companionRuntime()
	st.mu.Lock()
	if st.appearanceWatch {
		st.mu.Unlock()
		return
	}
	st.appearanceWatch = true
	st.mu.Unlock()

	dirPath, err := a.uiAppearancePath()
	if err != nil {
		a.stopAppearanceWatchClaim()
		logFileCompanion("appearance watch: %v", err)
		return
	}
	dir := filepath.Dir(dirPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		a.stopAppearanceWatchClaim()
		logFileCompanion("appearance watch: %v", err)
		return
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		a.stopAppearanceWatchClaim()
		logFileCompanion("appearance watch: %v", err)
		return
	}
	if err := watcher.Add(dir); err != nil {
		_ = watcher.Close()
		a.stopAppearanceWatchClaim()
		logFileCompanion("appearance watch %s: %v", dir, err)
		return
	}
	go a.runAppearanceWatch(watcher)
}

const appearanceReadAttempts = 4

// appearanceFanout receives a theme the companion is about to publish.
// Tests use it because a headless App has no Wails event context.
var (
	appearanceFanoutMu sync.Mutex
	appearanceFanout   func(*App, UIAppearance)
)

func (a *App) deliverAppearance(appearance UIAppearance) {
	appearanceFanoutMu.Lock()
	hook := appearanceFanout
	appearanceFanoutMu.Unlock()
	if hook != nil {
		hook(a, appearance)
	}
	a.emitEvent(uiAppearanceEvent, appearance)
}

func (a *App) stopAppearanceWatchClaim() {
	if a == nil {
		return
	}
	st := a.companionRuntime()
	st.mu.Lock()
	st.appearanceWatch = false
	st.mu.Unlock()
}

// runAppearanceWatch collapses the temp-file rename into one read of the
// finished JSON. A directory watch also sees config.json; those names are ignored.
func (a *App) runAppearanceWatch(watcher *fsnotify.Watcher) {
	defer a.stopAppearanceWatchClaim()
	defer watcher.Close()
	var (
		mu         sync.Mutex
		timer      *time.Timer
		last       UIAppearance
		haveLast   bool
		generation uint64
		attempt    int
		waiting    bool
	)
	var emit func(gen uint64)
	emit = func(gen uint64) {
		mu.Lock()
		if gen != generation {
			mu.Unlock()
			return
		}
		// This wait is being served. A later watcher error may schedule
		// another read; it must not keep postponing this one.
		waiting = false
		mu.Unlock()
		appearance, have := a.readUIAppearance()
		mu.Lock()
		// A newer rename already scheduled its own read. Drop this one so a
		// slow read of the previous file cannot paint over the new theme.
		if gen != generation {
			mu.Unlock()
			return
		}
		if !have {
			// Windows removes the destination before renaming the temp file
			// into place. A read in that gap must try again instead of
			// dropping the theme change.
			attempt++
			if attempt >= appearanceReadAttempts {
				mu.Unlock()
				return
			}
			waiting = true
			timer = time.AfterFunc(40*time.Millisecond, func() { emit(gen) })
			mu.Unlock()
			return
		}
		attempt = 0
		unchanged := haveLast && last == appearance
		if !unchanged {
			last = appearance
			haveLast = true
		}
		mu.Unlock()
		if unchanged {
			return
		}
		a.deliverAppearance(appearance)
	}
	schedule := func() {
		mu.Lock()
		generation++
		attempt = 0
		waiting = true
		gen := generation
		previous := timer
		timer = time.AfterFunc(60*time.Millisecond, func() { emit(gen) })
		mu.Unlock()
		// Stop outside the lock. emit also takes it, and Timer.Stop waits
		// for a callback that has already started.
		if previous != nil {
			previous.Stop()
		}
	}
	scheduleIfIdle := func() {
		mu.Lock()
		pending := waiting
		mu.Unlock()
		if !pending {
			schedule()
		}
	}
	defer func() {
		mu.Lock()
		generation++
		previous := timer
		timer = nil
		mu.Unlock()
		if previous != nil {
			previous.Stop()
		}
	}()
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if appearanceWatchRelevant(event.Name) {
				schedule()
			}
		case _, ok := <-watcher.Errors:
			if !ok {
				return
			}
			// A dropped rename notice still needs a read. An error must not
			// postpone a read that is already waiting, or a noisy watcher
			// never lets that read run.
			scheduleIfIdle()
		}
	}
}

func appearanceAttributeScript(appearance UIAppearance) string {
	var b strings.Builder
	b.WriteString("var r=document.documentElement;if(r){r.setAttribute(\"data-ai-theme\",")
	b.WriteString(strconv.Quote(appearance.ThemeMode))
	b.WriteString(");r.setAttribute(\"data-boot-theme\",")
	b.WriteString(strconv.Quote(appearance.ThemeMode))
	b.WriteString(");")
	if appearance.ThemeMode == "dark" {
		b.WriteString("r.setAttribute(\"data-ai-dark-scheme\",")
		b.WriteString(strconv.Quote(appearance.DarkScheme))
		b.WriteString(");r.removeAttribute(\"data-ai-light-scheme\");r.style.colorScheme=\"dark\";")
	} else {
		b.WriteString("r.setAttribute(\"data-ai-light-scheme\",")
		b.WriteString(strconv.Quote(appearance.LightScheme))
		b.WriteString(");r.removeAttribute(\"data-ai-dark-scheme\");r.style.colorScheme=\"light\";")
	}
	b.WriteString("}")
	return b.String()
}

// injectFileCompanionAppearance writes the shared theme into localStorage
// before the boot palette script reads it, so the companion's first frame
// uses the same scheme as the main window.
func injectFileCompanionAppearance(html []byte, appearance UIAppearance) []byte {
	normalized, ok := normalizeUIAppearance(appearance)
	if !ok || bytes.Contains(html, []byte("__MACLAW_UI_APPEARANCE__")) {
		return html
	}
	snippet := "<script>window.__MACLAW_UI_APPEARANCE__=1;try{localStorage.setItem(\"ai_assistant_theme_mode\"," +
		strconv.Quote(normalized.ThemeMode) +
		");localStorage.setItem(\"maclaw.ai.themeMode\"," +
		strconv.Quote(normalized.ThemeMode) +
		");localStorage.setItem(\"maclaw.ai.darkScheme\"," +
		strconv.Quote(normalized.DarkScheme) +
		");localStorage.setItem(\"maclaw.ai.lightScheme\"," +
		strconv.Quote(normalized.LightScheme) +
		");" + appearanceAttributeScript(normalized) + "}catch(e){}</script>"
	const flag = "window.__MACLAW_FILE_COMPANION__=true</script>"
	if bytes.Contains(html, []byte(flag)) {
		return bytes.Replace(html, []byte(flag), []byte(flag+snippet), 1)
	}
	const head = "<head>"
	if bytes.Contains(html, []byte(head)) {
		return bytes.Replace(html, []byte(head), []byte(head+snippet), 1)
	}
	return html
}
