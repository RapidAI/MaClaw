package guiapp

import (
	"sync"
	"testing"
	"time"
)

func TestUILanguageRoundTrip(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if _, ok := app.readUILanguage(); ok {
		t.Fatal("missing file was treated as a language")
	}
	if err := app.writeUILanguageFile("zh-CN"); err != nil {
		t.Fatal(err)
	}
	got, ok := app.readUILanguage()
	if !ok || got != "zh-Hans" {
		t.Fatalf("stored = %q ok=%v", got, ok)
	}
	if err := app.writeUILanguageFile("zh-Hans"); err != nil {
		t.Fatal(err)
	}
	if err := app.writeUILanguageFile("mn"); err != nil {
		t.Fatal(err)
	}
	got, ok = app.readUILanguage()
	if !ok || got != "mn" {
		t.Fatalf("stored = %q ok=%v", got, ok)
	}
}

func TestUILanguageSkipsRealProfileUnderTest(t *testing.T) {
	if !uiLanguageSkipRealProfile() {
		t.Fatal("the test binary should not write the real profile")
	}
	app := &App{}
	if err := app.writeUILanguageFile("mn"); err != nil {
		t.Fatal(err)
	}
}

func TestUILanguageWatchFollowsMainWindow(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	prev, _ := agentViewCurrentLang.Load().(string)
	t.Cleanup(func() { agentViewCurrentLang.Store(prev) })

	app.SetLanguage("mn")
	if app.GetCurrentLanguage() != "mn" {
		t.Fatalf("startup language = %q", app.GetCurrentLanguage())
	}
	if published, ok := app.readUILanguage(); !ok || published != "mn" {
		t.Fatalf("published = %q ok=%v", published, ok)
	}
	got := make(chan string, 4)
	languageFanoutMu.Lock()
	previous := languageFanout
	languageFanout = func(target *App, lang string) {
		if target != app {
			return
		}
		select {
		case got <- lang:
		default:
		}
	}
	languageFanoutMu.Unlock()
	t.Cleanup(func() {
		languageFanoutMu.Lock()
		languageFanout = previous
		languageFanoutMu.Unlock()
	})
	app.startFileCompanionLanguageWatcher()
	// Stop before the agent-view language is restored. Cleanup runs in reverse
	// registration order, and this registration is after that restore.
	t.Cleanup(app.stopFileCompanionLanguageWatcher)

	// The other process writes the file. This process's CurrentLanguage stays
	// on the language it read at startup until the watch applies the new one.
	if err := app.writeUILanguageFile("zh-Hans"); err != nil {
		t.Fatal(err)
	}
	select {
	case lang := <-got:
		if lang != "zh-Hans" {
			t.Fatalf("delivered = %q", lang)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("language change was not delivered")
	}
	if app.GetCurrentLanguage() != "zh-Hans" {
		t.Fatalf("current language = %q", app.GetCurrentLanguage())
	}
	// A second switch must still be delivered. The watch remembers the last
	// tag only to skip a duplicate, not to stick on the first change.
	if err := app.writeUILanguageFile("mn"); err != nil {
		t.Fatal(err)
	}
	select {
	case lang := <-got:
		if lang != "mn" {
			t.Fatalf("delivered = %q", lang)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("switch back was not delivered")
	}
	if app.GetCurrentLanguage() != "mn" {
		t.Fatalf("current language = %q", app.GetCurrentLanguage())
	}
}

func TestUILanguageWatchAdoptsFileAtStart(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	prev, _ := agentViewCurrentLang.Load().(string)
	t.Cleanup(func() { agentViewCurrentLang.Store(prev) })
	if err := app.writeUILanguageFile("zh-Hans"); err != nil {
		t.Fatal(err)
	}
	// Memory matches an older config read. The file is the newer switch.
	app.setLanguage("mn", false)
	if got, ok := app.readUILanguage(); !ok || got != "zh-Hans" {
		t.Fatalf("file before watch = %q ok=%v", got, ok)
	}
	got := make(chan string, 4)
	languageFanoutMu.Lock()
	previous := languageFanout
	languageFanout = func(target *App, lang string) {
		if target != app {
			return
		}
		select {
		case got <- lang:
		default:
		}
	}
	languageFanoutMu.Unlock()
	t.Cleanup(func() {
		languageFanoutMu.Lock()
		languageFanout = previous
		languageFanoutMu.Unlock()
	})
	app.startFileCompanionLanguageWatcher()
	t.Cleanup(app.stopFileCompanionLanguageWatcher)
	select {
	case lang := <-got:
		if lang != "zh-Hans" {
			t.Fatalf("delivered = %q", lang)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("existing language file was not applied")
	}
	if app.GetCurrentLanguage() != "zh-Hans" {
		t.Fatalf("current language = %q", app.GetCurrentLanguage())
	}
	if stored, ok := app.readUILanguage(); !ok || stored != "zh-Hans" {
		t.Fatalf("file after adopt = %q ok=%v", stored, ok)
	}
}

func TestAdoptedLanguageDoesNotRewriteTheFile(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	prev, _ := agentViewCurrentLang.Load().(string)
	t.Cleanup(func() { agentViewCurrentLang.Store(prev) })
	if err := app.writeUILanguageFile("zh-Hans"); err != nil {
		t.Fatal(err)
	}
	app.setLanguage("mn", false)
	if app.GetCurrentLanguage() != "mn" {
		t.Fatalf("current = %q", app.GetCurrentLanguage())
	}
	got, ok := app.readUILanguage()
	if !ok || got != "zh-Hans" {
		t.Fatalf("file = %q ok=%v", got, ok)
	}
}

func TestApplyWatchedLanguagePrefersTheFile(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	prev, _ := agentViewCurrentLang.Load().(string)
	t.Cleanup(func() { agentViewCurrentLang.Store(prev) })
	if err := app.writeUILanguageFile("mn"); err != nil {
		t.Fatal(err)
	}
	app.setLanguage("zh-Hans", false)
	// The callback still carries the tag it read earlier. The file has since
	// moved on, and that older tag must not become the companion language.
	if got := app.applyWatchedUILanguage("zh-Hans"); got != "mn" {
		t.Fatalf("adopted = %q", got)
	}
	if app.GetCurrentLanguage() != "mn" {
		t.Fatalf("current = %q", app.GetCurrentLanguage())
	}
	if stored, ok := app.readUILanguage(); !ok || stored != "mn" {
		t.Fatalf("file = %q ok=%v", stored, ok)
	}
}

func TestUILanguagePublishIsMainWindowOnly(t *testing.T) {
	if uiLanguagePublishedBy(desktopLaunchFileCompanion) {
		t.Fatal("companion process must not publish the language file")
	}
	if !uiLanguagePublishedBy(desktopLaunchMain) {
		t.Fatal("main window must publish the language file")
	}
}

func TestOlderLanguagePublishDoesNotRestoreTheFile(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	prevLang, _ := agentViewCurrentLang.Load().(string)
	prevTray := UpdateTrayMenu
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	done := make(chan struct{})
	newerDone := make(chan struct{})
	newerStart := make(chan struct{})
	var startOnce sync.Once
	kickNewer := func() { startOnce.Do(func() { close(newerStart) }) }
	t.Cleanup(func() {
		unlock()
		kickNewer()
		<-done
		<-newerDone
		UpdateTrayMenu = prevTray
		agentViewCurrentLang.Store(prevLang)
	})
	var trayMu sync.Mutex
	trayLang := ""
	UpdateTrayMenu = func(lang string) {
		if lang == "mn" {
			close(started)
			<-release
		}
		// Record the label when the update finishes. The older call blocks
		// before this write, which is when a newer switch can pass it.
		trayMu.Lock()
		trayLang = lang
		trayMu.Unlock()
	}
	go func() {
		defer close(done)
		app.SetLanguage("mn")
	}()
	go func() {
		defer close(newerDone)
		<-newerStart
		app.SetLanguage("zh-Hans")
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("the older publish did not reach the tray update")
	}
	kickNewer()
	deadline := time.Now().Add(3 * time.Second)
	for app.GetCurrentLanguage() != "zh-Hans" {
		if time.Now().After(deadline) {
			t.Fatal("the newer language was not stored while the older publish was in the tray")
		}
		time.Sleep(5 * time.Millisecond)
	}
	unlock()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the older publish did not finish")
	}
	select {
	case <-newerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("the newer publish did not finish")
	}
	trayMu.Lock()
	gotTray := trayLang
	trayMu.Unlock()
	if gotTray != "zh-Hans" {
		t.Fatalf("tray = %q", gotTray)
	}
	if got, _ := agentViewCurrentLang.Load().(string); got != "zh-Hans" {
		t.Fatalf("agent view = %q", got)
	}
	if app.GetCurrentLanguage() != "zh-Hans" {
		t.Fatalf("current = %q", app.GetCurrentLanguage())
	}
	if got, ok := app.readUILanguage(); !ok || got != "zh-Hans" {
		t.Fatalf("file = %q ok=%v", got, ok)
	}
}

func TestConfigLanguageDoesNotReplaceAChoice(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	prevLang, _ := agentViewCurrentLang.Load().(string)
	t.Cleanup(func() { agentViewCurrentLang.Store(prevLang) })
	seen := app.currentUILanguageSeq()
	app.SetLanguage("zh-Hans")
	app.applyConfigLanguageIfUnchanged("mn", seen)
	if app.GetCurrentLanguage() != "zh-Hans" {
		t.Fatalf("current = %q", app.GetCurrentLanguage())
	}
	if got, ok := app.readUILanguage(); !ok || got != "zh-Hans" {
		t.Fatalf("file = %q ok=%v", got, ok)
	}
}

func TestConfigLanguageAppliesWhenUnchanged(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	prevLang, _ := agentViewCurrentLang.Load().(string)
	t.Cleanup(func() { agentViewCurrentLang.Store(prevLang) })
	app.SetLanguage("en")
	seen := app.currentUILanguageSeq()
	app.applyConfigLanguageIfUnchanged("mn", seen)
	if app.GetCurrentLanguage() != "mn" {
		t.Fatalf("current = %q", app.GetCurrentLanguage())
	}
	if got, ok := app.readUILanguage(); !ok || got != "mn" {
		t.Fatalf("file = %q ok=%v", got, ok)
	}
}

func TestUILanguageWatchRelevant(t *testing.T) {
	if !uiLanguageWatchRelevant("ui-language.json") || !uiLanguageWatchRelevant("dir/ui-language.json.tmp") {
		t.Fatal("language file was ignored")
	}
	if uiLanguageWatchRelevant("ui-appearance.json") || uiLanguageWatchRelevant("config.db") {
		t.Fatal("unrelated file was watched")
	}
}
