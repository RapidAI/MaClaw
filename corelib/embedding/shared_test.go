package embedding

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSharedEmbedderFallsBackToNoopWithoutModel(t *testing.T) {
	ResetSharedEmbedderForTest()
	t.Cleanup(ResetSharedEmbedderForTest)
	emb := SharedEmbedder()
	if emb == nil {
		t.Fatal("expected embedder")
	}
	if !IsNoop(emb) && emb.Dim() != 256 {
		t.Fatalf("unexpected embedder dim=%d noop=%v", emb.Dim(), IsNoop(emb))
	}
	again := SharedEmbedder()
	if again != emb {
		t.Fatal("SharedEmbedder should reuse the process-wide instance")
	}
}

func TestSharedEmbedderReadyDoesNotLoad(t *testing.T) {
	ResetSharedEmbedderForTest()
	t.Cleanup(ResetSharedEmbedderForTest)
	prev, _ := BaseDirFunc.Load().(func() string)
	dir := t.TempDir()
	BaseDirFunc.Store(func() string { return dir })
	t.Cleanup(func() {
		if prev != nil {
			BaseDirFunc.Store(prev)
		}
	})
	path := filepath.Join(dir, "models", DefaultModelFilename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not-a-real-gguf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if SharedEmbedderReady() {
		t.Fatal("peek must not start a load")
	}
	if !IsNoop(SharedEmbedder()) {
		t.Fatal("invalid GGUF must stay noop")
	}
	if SharedEmbedderReady() {
		t.Fatal("invalid GGUF must stay not ready")
	}
}

func TestReloadSharedEmbedderIfReadyRetriesAfterFileAppears(t *testing.T) {
	ResetSharedEmbedderForTest()
	t.Cleanup(ResetSharedEmbedderForTest)
	prev, _ := BaseDirFunc.Load().(func() string)
	dir := t.TempDir()
	BaseDirFunc.Store(func() string { return dir })
	t.Cleanup(func() {
		if prev != nil {
			BaseDirFunc.Store(prev)
		}
	})

	if !IsNoop(SharedEmbedder()) {
		t.Fatal("expected noop before the model file exists")
	}
	path := filepath.Join(dir, "models", DefaultModelFilename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not-a-real-gguf"), 0o644); err != nil {
		t.Fatal(err)
	}
	emb := ReloadSharedEmbedderIfReady()
	if emb == nil {
		t.Fatal("expected embedder after reload")
	}
	if !IsNoop(emb) {
		t.Fatal("invalid GGUF must stay noop; reload must not panic")
	}
}
