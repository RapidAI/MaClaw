package imgconv

import (
	"os"
	"runtime"
	"testing"

	"golang.org/x/image/font/opentype"
)

// TestParseSystemFontReadsTTC guards against macOS-style font collections:
// the plain parser rejects .ttc, so system faces would silently degrade to
// the embedded Go font. parseSystemFont must open them.
func TestParseSystemFontReadsTTC(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("fixture only available on windows")
	}
	const path = `C:\Windows\Fonts\msyh.ttc`
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skip("msyh.ttc not available")
	}
	if _, err := opentype.Parse(data); err == nil {
		t.Fatal("fixture is not a collection; test no longer proves anything")
	}
	font, err := parseSystemFont(data)
	if err != nil {
		t.Fatalf("parseSystemFont failed on a TrueType Collection: %v", err)
	}
	if font == nil {
		t.Fatal("parseSystemFont returned no font")
	}
}
