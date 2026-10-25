//go:build !windows

package guiapp

import "testing"

// macOS and Linux have no shell icon mechanism. The cloud drive must read that
// as "no icon" rather than as a transient fault it should keep retrying, while
// the shared attachment path keeps reporting the platform limitation.
func TestFileTypeIconUnsupportedPlatformIsDefinitive(t *testing.T) {
	app := &App{}

	url, err := app.AIAssistantFileTypeIconDataURLIfAssociated("file.pdf")
	if err != nil {
		t.Fatalf("cloud drive variant: %v", err)
	}
	if url != "" {
		t.Fatalf("cloud drive variant returned an icon on an unsupported platform")
	}

	if _, err := app.AIAssistantFileTypeIconDataURL("file.pdf"); err == nil {
		t.Fatal("the shared attachment path should still report the platform limitation")
	}
}
