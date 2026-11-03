//go:build windows

package windows

import (
	"slices"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options"
	winoptions "github.com/wailsapp/wails/v2/pkg/options/windows"
)

// TestBrowserDisableFeaturesAlwaysKeepsTheCursorWorkaround guards the primary
// fix for the stuck WebView2 cursor: every option combination must still pass
// HideCursorWhileTyping to the runtime.
func TestBrowserDisableFeaturesAlwaysKeepsTheCursorWorkaround(t *testing.T) {
	configs := []struct {
		name string
		opts *options.App
	}{
		{name: "defaults", opts: &options.App{}},
		{name: "fraudulent detection on", opts: &options.App{EnableFraudulentWebsiteDetection: true}},
		{name: "renderer integrity off", opts: &options.App{Windows: &winoptions.Options{}}},
		{name: "renderer integrity disabled", opts: &options.App{Windows: &winoptions.Options{WebviewDisableRendererCodeIntegrity: true}}},
	}
	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			features := browserDisableFeatures(tc.opts)
			if !slices.Contains(features, "HideCursorWhileTyping") {
				t.Fatalf("HideCursorWhileTyping missing from %v", features)
			}
		})
	}
}

// TestBrowserDisableFeaturesKeepsTheExistingSwitches makes sure folding the
// list into a helper did not drop the switches the app already relied on.
func TestBrowserDisableFeaturesKeepsTheExistingSwitches(t *testing.T) {
	opts := &options.App{Windows: &winoptions.Options{WebviewDisableRendererCodeIntegrity: true}}
	features := browserDisableFeatures(opts)
	if !slices.Contains(features, "msSmartScreenProtection") {
		t.Fatalf("msSmartScreenProtection missing from %v", features)
	}
	if !slices.Contains(features, "RendererCodeIntegrity") {
		t.Fatalf("RendererCodeIntegrity missing from %v", features)
	}

	opts = &options.App{EnableFraudulentWebsiteDetection: true}
	features = browserDisableFeatures(opts)
	if slices.Contains(features, "msSmartScreenProtection") {
		t.Fatalf("msSmartScreenProtection should be absent when detection is enabled: %v", features)
	}
}
