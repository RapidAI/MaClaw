package guiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const tinyPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func TestDecodeDiagramPNG(t *testing.T) {
	data, err := decodeDiagramPNG("data:image/png;base64," + tinyPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 8 || data[0] != 0x89 || string(data[1:4]) != "PNG" {
		t.Fatalf("decoded bytes are not a png: %d", len(data))
	}

	raw, err := decodeDiagramPNG(tinyPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(data) {
		t.Fatal("raw base64 and data url decoded differently")
	}

	for _, bad := range []string{"", "hello", "data:image/jpeg;base64," + tinyPNGBase64, "data:image/png;base64,aaaa"} {
		if _, err := decodeDiagramPNG(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if _, err := decodeDiagramPNG(strings.Repeat("A", diagramPNGMaxEncodedBytes+1)); err == nil {
		t.Fatal("accepted an oversized payload")
	}
}

func TestWriteDiagramPNG(t *testing.T) {
	data, err := decodeDiagramPNG(tinyPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	saved, err := writeDiagramPNG(filepath.Join(dir, "diagram"), data)
	if err != nil {
		t.Fatal(err)
	}
	if saved != filepath.Join(dir, "diagram.png") {
		t.Fatalf("path %s", saved)
	}
	written, err := os.ReadFile(saved)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != string(data) {
		t.Fatal("written bytes differ")
	}
	if _, err := writeDiagramPNG(dir, data); err == nil {
		t.Fatal("wrote a png onto a directory")
	}
	if path, err := writeDiagramPNG("  ", data); err != nil || path != "" {
		t.Fatalf("cancel path=%q err=%v", path, err)
	}
}
