package guiapp

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// uiLanguageFileName sits beside config.json. The companion process has its
// own WebView and its own in-memory language, taken once at startup. The main
// window writes this file from SetLanguage so an already-open companion can
// follow a later switch, including Mongolian back to Chinese.
const uiLanguageFileName = "ui-language.json"

// uiLanguageEvent is emitted in the companion process when that file changes.
const uiLanguageEvent = "ui-language:changed"

const uiLanguageReadAttempts = 4

// uiLanguageNote is the shared language file. Language is the normalized tag.
type uiLanguageNote struct {
	Language string `json:"language"`
}

// uiLanguageMu keeps one process's compare-and-write atomic. The companion
// is another process, so it can still see the remove-then-rename gap; the
// watcher retries that read.
var uiLanguageMu sync.Mutex

func uiLanguageWatchRelevant(name string) bool {
	base := filepath.Base(name)
	return base == uiLanguageFileName || base == uiLanguageFileName+".tmp"
}

// uiLanguagePublishedBy reports whether this launch mode may write the shared
// language file. The companion process reads it. A write from that process
// would put its startup language back over a switch the main window just saved.
func uiLanguagePublishedBy(mode string) bool {
	return mode != desktopLaunchFileCompanion
}

// uiLanguageSkipRealProfile is true while go test is executing this package.
// SetLanguage is called from tests that have no temp home; those must not
// write the developer's real ui-language.json.
func uiLanguageSkipRealProfile() bool {
	base := strings.ToLower(filepath.Base(os.Args[0]))
	return strings.Contains(base, ".test")
}

func (a *App) uiLanguagePath() (string, error) {
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
	return filepath.Join(dir, uiLanguageFileName), nil
}

func (a *App) readUILanguageUnlocked() (string, bool) {
	path, err := a.uiLanguagePath()
	if err != nil {
		return "", false
	}
	body, err := os.ReadFile(path)
	if err != nil || len(body) == 0 || len(body) > 256 {
		return "", false
	}
	var parsed uiLanguageNote
	if json.Unmarshal(body, &parsed) != nil {
		return "", false
	}
	lang := string(normalizeAppLanguageKind(parsed.Language))
	if strings.TrimSpace(parsed.Language) == "" {
		return "", false
	}
	return lang, true
}

func (a *App) readUILanguage() (string, bool) {
	if a == nil {
		return "", false
	}
	uiLanguageMu.Lock()
	defer uiLanguageMu.Unlock()
	return a.readUILanguageUnlocked()
}

// publishUILanguageIfCurrent writes the sidecar when lang is still the
// process language. The caller may have blocked in the tray update after
// storing lang, and a newer switch may already have replaced it. Writing
// the older tag would move the companion back while the main window stayed
// on the newer one.
func (a *App) publishUILanguageIfCurrent(lang string) error {
	if a == nil {
		return errors.New("app unavailable")
	}
	if a.testHomeDir == "" && uiLanguageSkipRealProfile() {
		return nil
	}
	normalized := string(normalizeAppLanguageKind(lang))
	uiLanguageMu.Lock()
	defer uiLanguageMu.Unlock()
	a.uiLanguageStateMu.Lock()
	current := a.CurrentLanguage
	a.uiLanguageStateMu.Unlock()
	if string(normalizeAppLanguageKind(current)) != normalized {
		return nil
	}
	if have, ok := a.readUILanguageUnlocked(); ok && have == normalized {
		return nil
	}
	return a.writeUILanguageBody(normalized)
}

// writeUILanguageFile stores the interface language where the companion
// process can read it. The same tag is left untouched. Callers that publish
// a live switch use publishUILanguageIfCurrent so an older call cannot
// restore the previous tag.
func (a *App) writeUILanguageFile(lang string) error {
	if a == nil {
		return errors.New("app unavailable")
	}
	if a.testHomeDir == "" && uiLanguageSkipRealProfile() {
		return nil
	}
	normalized := string(normalizeAppLanguageKind(lang))
	uiLanguageMu.Lock()
	defer uiLanguageMu.Unlock()
	if current, have := a.readUILanguageUnlocked(); have && current == normalized {
		return nil
	}
	return a.writeUILanguageBody(normalized)
}

// writeUILanguageBody writes the sidecar. The caller holds uiLanguageMu.
func (a *App) writeUILanguageBody(lang string) error {
	path, err := a.uiLanguagePath()
	if err != nil {
		return err
	}
	body, err := json.Marshal(uiLanguageNote{Language: lang})
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return fileCompanionWriteAtomicFile(path, body, 0o600)
}

// languageFanout receives a language the companion is about to publish.
// Tests use it because a headless App has no Wails event context.
var (
	languageFanoutMu sync.Mutex
	languageFanout   func(*App, string)
)

func (a *App) deliverCompanionLanguage(lang string) {
	languageFanoutMu.Lock()
	hook := languageFanout
	languageFanoutMu.Unlock()
	if hook != nil {
		hook(a, lang)
	}
	a.emitEvent(uiLanguageEvent, lang)
}

// applyWatchedUILanguage adopts a language written by the main window.
// It does not write the file back, so the watch cannot loop.
// The file is read again here: the callback's tag can be older than a write
// that landed while this callback was waiting, and storing that tag would
// put the companion back on the previous language.
func (a *App) applyWatchedUILanguage(lang string) string {
	if a == nil {
		return ""
	}
	normalized := string(normalizeAppLanguageKind(lang))
	if latest, ok := a.readUILanguage(); ok {
		normalized = latest
	}
	a.uiLanguageStateMu.Lock()
	current := a.CurrentLanguage
	a.uiLanguageStateMu.Unlock()
	if strings.TrimSpace(current) != "" && string(normalizeAppLanguageKind(current)) == normalized {
		return normalized
	}
	a.setLanguage(normalized, false)
	a.deliverCompanionLanguage(normalized)
	return normalized
}

func (a *App) stopLanguageWatchClaim() {
	if a == nil {
		return
	}
	st := a.companionRuntime()
	st.mu.Lock()
	st.languageWatch = false
	st.mu.Unlock()
}

// startFileCompanionLanguageWatcher tells this companion window when the
// main window writes a new interface language. Events stay inside this process.
func (a *App) startFileCompanionLanguageWatcher() {
	if a == nil {
		return
	}
	st := a.companionRuntime()
	st.mu.Lock()
	if st.languageWatch {
		st.mu.Unlock()
		return
	}
	st.languageWatch = true
	st.mu.Unlock()

	filePath, err := a.uiLanguagePath()
	if err != nil {
		a.stopLanguageWatchClaim()
		logFileCompanion("language watch: %v", err)
		return
	}
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		a.stopLanguageWatchClaim()
		logFileCompanion("language watch: %v", err)
		return
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		a.stopLanguageWatchClaim()
		logFileCompanion("language watch: %v", err)
		return
	}
	if err := watcher.Add(dir); err != nil {
		_ = watcher.Close()
		a.stopLanguageWatchClaim()
		logFileCompanion("language watch %s: %v", dir, err)
		return
	}
	st.mu.Lock()
	st.languageWatcher = watcher
	st.mu.Unlock()
	// The file may already hold a switch that happened before this process
	// existed. Config read at startup can be older than that file.
	if lang, ok := a.readUILanguage(); ok {
		a.applyWatchedUILanguage(lang)
	}
	go a.runLanguageWatch(watcher)
}

// stopFileCompanionLanguageWatcher closes the watch. Close is not called
// again from the read loop: a second Close on Windows can race.
func (a *App) stopFileCompanionLanguageWatcher() {
	if a == nil {
		return
	}
	st := a.companionRuntime()
	st.mu.Lock()
	watcher := st.languageWatcher
	st.languageWatcher = nil
	st.mu.Unlock()
	if watcher != nil {
		_ = watcher.Close()
	}
}

// runLanguageWatch collapses the temp-file rename into one read of the
// finished JSON. A directory watch also sees config and appearance files;
// those names are ignored.
func (a *App) runLanguageWatch(watcher *fsnotify.Watcher) {
	defer a.stopLanguageWatchClaim()
	var (
		mu         sync.Mutex
		timer      *time.Timer
		last       string
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
		waiting = false
		mu.Unlock()
		lang, have := a.readUILanguage()
		mu.Lock()
		if gen != generation {
			mu.Unlock()
			return
		}
		if !have {
			attempt++
			if attempt >= uiLanguageReadAttempts {
				mu.Unlock()
				return
			}
			waiting = true
			timer = time.AfterFunc(40*time.Millisecond, func() { emit(gen) })
			mu.Unlock()
			return
		}
		attempt = 0
		if haveLast && last == lang {
			mu.Unlock()
			return
		}
		last = lang
		haveLast = true
		mu.Unlock()
		adopted := a.applyWatchedUILanguage(lang)
		if adopted == lang {
			return
		}
		// The file moved past the tag this callback observed. Remember the
		// tag that was actually adopted, unless a newer callback already did.
		mu.Lock()
		if gen == generation {
			last = adopted
			haveLast = adopted != ""
		}
		mu.Unlock()
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
			if uiLanguageWatchRelevant(event.Name) {
				schedule()
			}
		case _, ok := <-watcher.Errors:
			if !ok {
				return
			}
			scheduleIfIdle()
		}
	}
}

func logUILanguageWrite(err error) {
	if err != nil {
		log.Printf("[i18n] language file: %v", err)
	}
}
