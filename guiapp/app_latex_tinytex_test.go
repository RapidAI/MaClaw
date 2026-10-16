package guiapp

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tinytex"
)

func TestGetLatexTinyTeXStatusReportsSchemeAndVerifiedTree(t *testing.T) {
	latexRuntime.mu.Lock()
	latexRuntime.phase = ""
	latexRuntime.running = false
	latexRuntime.err = ""
	latexRuntime.percent = 0
	latexRuntime.mu.Unlock()

	app := &App{testHomeDir: t.TempDir()}
	app.configCacheValid = true
	app.configCache = corelib.AppConfigDefaults()

	status := app.GetLatexTinyTeXStatus()
	if status["enabled"] != true {
		t.Fatalf("enabled = %#v", status["enabled"])
	}
	if status["ready"] != false {
		t.Fatalf("ready = %#v", status["ready"])
	}
	if status["scheme"] != tinytex.Scheme || status["bundle"] != tinytex.Bundle {
		t.Fatalf("scheme/bundle = %#v %#v", status["scheme"], status["bundle"])
	}
	source, _ := status["source"].(string)
	if source == "" || status["phase"] != "idle" {
		t.Fatalf("status = %#v", status)
	}

	dist := filepath.Join(tinytex.DistDir(app.GetDataDir()), "TinyTeX")
	if err := os.MkdirAll(filepath.Join(dist, "bin", "windows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dist, "tlpkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	engineName := "xelatex"
	if runtime.GOOS == "windows" {
		engineName = "xelatex.exe"
	}
	if err := os.WriteFile(filepath.Join(dist, "bin", "windows", engineName), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	article := tinytex.ArticleClassPath(dist)
	if err := os.MkdirAll(filepath.Dir(article), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(article, []byte("\\documentclass"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tinytex.WriteStamp(dist, "XeTeX test"); err != nil {
		t.Fatal(err)
	}

	status = app.GetLatexTinyTeXStatus()
	if status["ready"] != true || status["phase"] != "ready" || status["version"] != "XeTeX test" {
		t.Fatalf("verified status = %#v", status)
	}

	app.configCache.LatexTinyTeXEnabled = false
	app.configCacheValid = true
	app.configSnap.Store(&app.configCache)
	status = app.GetLatexTinyTeXStatus()
	if status["enabled"] != false {
		t.Fatalf("enabled after disable = %#v", status["enabled"])
	}
	if app.testHomeDir == "" || app.latexTreeVerified() != true {
		t.Fatal("verified tree should stay on disk when the switch is off")
	}
}
