package tablestruct

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

func TestVerifyFileRejectsShortWeights(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, modelFile)
	if err := os.WriteFile(path, []byte("short"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := verifyFile(path)
	if !errors.Is(err, embedding.ErrModelCorrupt) {
		t.Fatalf("err = %v", err)
	}
}
