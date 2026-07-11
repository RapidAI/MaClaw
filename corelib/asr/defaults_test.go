package asr

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding/gguf"
)

func TestModelFileStatus(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.gguf")
	if _, ok := ModelFileStatus(missing); ok {
		t.Fatal("missing model reported as ready")
	}

	empty := filepath.Join(dir, "empty.gguf")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := ModelFileStatus(empty); ok {
		t.Fatal("empty model reported as ready")
	}

	dirPath := filepath.Join(dir, "model.gguf")
	if err := os.Mkdir(dirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := ModelFileStatus(dirPath); ok {
		t.Fatal("model directory reported as ready")
	}

	notGGUF := filepath.Join(dir, "not-gguf.gguf")
	if err := os.WriteFile(notGGUF, []byte("model"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := ModelFileStatus(notGGUF); ok {
		t.Fatal("non-GGUF model reported as ready")
	}

	model := filepath.Join(dir, DefaultModelFilename)
	header := make([]byte, 4)
	binary.LittleEndian.PutUint32(header, gguf.Magic)
	if err := os.WriteFile(model, header, 0o644); err != nil {
		t.Fatal(err)
	}
	if size, ok := ModelFileStatus(model); !ok || size != 4 {
		t.Fatalf("valid GGUF status = (%d, %t), want (4, true)", size, ok)
	}
}
