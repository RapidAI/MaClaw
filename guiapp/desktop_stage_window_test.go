package guiapp

import (
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestDesktopStagePhysicalWidthUsesWindowScreenScale(t *testing.T) {
	if got := desktopStagePhysicalWidth(1280, &application.Screen{ScaleFactor: 1.5}); got != 1920 {
		t.Fatalf("1280 DIP at 150%% = %d physical px, want 1920", got)
	}
	if got := desktopStagePhysicalWidth(1280, nil); got != 1280 {
		t.Fatalf("nil screen fallback = %d physical px, want 1280", got)
	}
}

func TestDesktopStageTitleMarksOnlyReviewBuilds(t *testing.T) {
	saved := version
	t.Cleanup(func() { version = saved })
	version = "review-desktop-rdp12"
	if got := desktopStageTitle(); !strings.Contains(got, "review-desktop-rdp12") {
		t.Fatalf("review stage title = %q, want version marker", got)
	}
	version = "8.0.0"
	if got := desktopStageTitle(); got != "MaClaw 桌面画面" {
		t.Fatalf("formal stage title = %q, want stable product title", got)
	}
}

func TestDesktopStageLogURLRedactsHandoffCapability(t *testing.T) {
	raw := "https://hub.example/api/v1/desktop-handoff/secret-token/h264.html?view_only=1&w=640&fps=10"
	got := desktopStageLogURL(raw)
	if strings.Contains(got, "secret-token") || strings.Contains(got, "view_only") {
		t.Fatalf("log url leaked capability or query: %q", got)
	}
	if !strings.Contains(got, "/desktop-handoff/<handoff>/h264.html") {
		t.Fatalf("log url lost player identity: %q", got)
	}
}

func TestOpenDesktopStageWindowFailsBeforeWailsAppInit(t *testing.T) {
	saved := runningWails3App
	runningWails3App = nil
	t.Cleanup(func() { runningWails3App = saved })
	app := &App{}
	if app.DesktopStageWindowSupported() {
		t.Fatalf("reported supported before wails3 application initialization")
	}
	if err := app.OpenDesktopStageWindow("https://hub.example/api/v1/desktop-handoff/token/h264.html?view_only=1"); err == nil {
		t.Fatalf("opened a native window before wails3 application initialization")
	}
}

func TestDesktopStageValidURLRejectsCredentialsAndNonHTTP(t *testing.T) {
	for _, raw := range []string{"javascript:alert(1)", "https://user:pass@hub.example/x", "wss://hub.example/x", "https://hub.example/x", "https://hub.example/api/v1/desktop-handoff/token/x.html"} {
		if err := desktopStageValidURL(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	if err := desktopStageValidURL("https://hub.example/api/v1/desktop-handoff/token/h264.html?view_only=1"); err != nil {
		t.Fatalf("rejected player url: %v", err)
	}
}
