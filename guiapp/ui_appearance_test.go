package guiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUIAppearanceRoundTrip(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if got := app.GetUIAppearance(); got.ThemeMode != "" {
		t.Fatalf("missing file = %#v", got)
	}
	next := UIAppearance{ThemeMode: "dark", DarkScheme: "graphite", LightScheme: "github"}
	if err := app.SetUIAppearance(next); err != nil {
		t.Fatal(err)
	}
	got := app.GetUIAppearance()
	if got != next {
		t.Fatalf("stored = %#v", got)
	}
	path, err := app.uiAppearancePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := app.SetUIAppearance(next); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	// Identical content must not open the file again. A read-only file would fail a rewrite.
	if err := app.SetUIAppearance(next); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.SetUIAppearance(UIAppearance{ThemeMode: "dark", DarkScheme: "../x", LightScheme: "github"}); err != nil {
		t.Fatal(err)
	}
	got = app.GetUIAppearance()
	if got != (UIAppearance{ThemeMode: "dark", DarkScheme: "onyx", LightScheme: "github"}) {
		t.Fatalf("unsafe scheme = %#v", got)
	}
	if err := app.SetUIAppearance(UIAppearance{ThemeMode: "dark", DarkScheme: "mist", LightScheme: "github"}); err != nil {
		t.Fatal(err)
	}
	got = app.GetUIAppearance()
	if got.DarkScheme != "mist" {
		t.Fatalf("unknown scheme was rewritten: %#v", got)
	}
	if err := app.SetUIAppearance(UIAppearance{ThemeMode: "nope", DarkScheme: "../x", LightScheme: "nope"}); err == nil {
		t.Fatal("invalid mode was stored")
	}
	got = app.GetUIAppearance()
	if got.ThemeMode != "dark" || got.DarkScheme != "mist" {
		t.Fatalf("invalid mode changed stored appearance: %#v", got)
	}
	if filepath.Base(path) != "ui-appearance.json" {
		t.Fatalf("path = %s", path)
	}
}

func TestAppearanceWatchRelevant(t *testing.T) {
	if !appearanceWatchRelevant(filepath.Join("dir", "ui-appearance.json")) || !appearanceWatchRelevant("ui-appearance.json.tmp") {
		t.Fatal("appearance file was ignored")
	}
	if appearanceWatchRelevant(filepath.Join("dir", "config.json")) || appearanceWatchRelevant("ui-appearance.json.bak") {
		t.Fatal("unrelated file was watched")
	}
}

func TestAppearanceWatchDeliversAtomicWrite(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	got := make(chan UIAppearance, 4)
	appearanceFanoutMu.Lock()
	previous := appearanceFanout
	appearanceFanout = func(target *App, appearance UIAppearance) {
		if target != app {
			return
		}
		select {
		case got <- appearance:
		default:
		}
	}
	appearanceFanoutMu.Unlock()
	t.Cleanup(func() {
		appearanceFanoutMu.Lock()
		appearanceFanout = previous
		appearanceFanoutMu.Unlock()
	})
	app.startFileCompanionAppearanceWatcher()
	next := UIAppearance{ThemeMode: "dark", DarkScheme: "violet", LightScheme: "fluent"}
	if err := app.SetUIAppearance(next); err != nil {
		t.Fatal(err)
	}
	select {
	case appearance := <-got:
		if appearance != next {
			t.Fatalf("delivered = %#v", appearance)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("appearance change was not delivered")
	}
}

func TestInjectFileCompanionAppearanceBeforeBootPalette(t *testing.T) {
	html := injectFileCompanionBootFlag([]byte("<head><script>var d = localStorage.getItem(\"ai_assistant_theme_mode\") === 'dark';</script></head>"))
	got := string(injectFileCompanionAppearance(html, UIAppearance{ThemeMode: "dark", DarkScheme: "violet", LightScheme: "notion"}))
	flag := strings.Index(got, "__MACLAW_FILE_COMPANION__")
	theme := strings.Index(got, `__MACLAW_UI_APPEARANCE__`)
	boot := strings.Index(got, `getItem("ai_assistant_theme_mode")`)
	if flag < 0 || theme < flag || boot < theme {
		t.Fatalf("order flag=%d theme=%d boot=%d\n%s", flag, theme, boot, got)
	}
	if !strings.Contains(got, `localStorage.setItem("ai_assistant_theme_mode","dark")`) {
		t.Fatalf("theme missing: %s", got)
	}
	if !strings.Contains(got, `localStorage.setItem("maclaw.ai.darkScheme","violet")`) || !strings.Contains(got, `localStorage.setItem("maclaw.ai.lightScheme","notion")`) {
		t.Fatalf("schemes missing: %s", got)
	}
	if !strings.Contains(got, `setAttribute("data-ai-theme","dark")`) || !strings.Contains(got, `setAttribute("data-ai-dark-scheme","violet")`) || !strings.Contains(got, `removeAttribute("data-ai-light-scheme")`) {
		t.Fatalf("document theme missing: %s", got)
	}
	again := injectFileCompanionAppearance([]byte(got), UIAppearance{ThemeMode: "light", DarkScheme: "onyx", LightScheme: "fluent"})
	if strings.Count(string(again), "__MACLAW_UI_APPEARANCE__") != 1 {
		t.Fatalf("appearance injected twice: %s", again)
	}
	if string(injectFileCompanionAppearance(html, UIAppearance{})) != string(html) {
		t.Fatal("invalid appearance changed the shell")
	}
}
