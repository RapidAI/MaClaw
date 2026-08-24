package embedding

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSharedGemma256FallsBackToNoopWithoutModel(t *testing.T) {
	ResetSharedGemmaForTest()
	t.Cleanup(ResetSharedGemmaForTest)
	emb := SharedGemma256()
	if emb == nil {
		t.Fatal("expected embedder")
	}
	if !IsNoop(emb) && emb.Dim() != 256 {
		t.Fatalf("unexpected embedder dim=%d noop=%v", emb.Dim(), IsNoop(emb))
	}
	again := SharedGemma256()
	if again != emb {
		t.Fatal("SharedGemma256 should reuse the process-wide instance")
	}
}

func TestSharedGemmaReadyDoesNotLoad(t *testing.T) {
	ResetSharedGemmaForTest()
	t.Cleanup(ResetSharedGemmaForTest)
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
	if SharedGemmaReady() {
		t.Fatal("peek must not start a load")
	}
	if !IsNoop(SharedGemma256()) {
		t.Fatal("invalid GGUF must stay noop")
	}
	if SharedGemmaReady() {
		t.Fatal("invalid GGUF must stay not ready")
	}
}

func TestReloadSharedGemmaIfReadyRetriesAfterFileAppears(t *testing.T) {
	ResetSharedGemmaForTest()
	t.Cleanup(ResetSharedGemmaForTest)
	prev, _ := BaseDirFunc.Load().(func() string)
	dir := t.TempDir()
	BaseDirFunc.Store(func() string { return dir })
	t.Cleanup(func() {
		if prev != nil {
			BaseDirFunc.Store(prev)
		}
	})

	if !IsNoop(SharedGemma256()) {
		t.Fatal("expected noop before the model file exists")
	}
	path := filepath.Join(dir, "models", DefaultModelFilename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not-a-real-gguf"), 0o644); err != nil {
		t.Fatal(err)
	}
	emb := ReloadSharedGemmaIfReady()
	if emb == nil {
		t.Fatal("expected embedder after reload")
	}
	if !IsNoop(emb) {
		t.Fatal("invalid GGUF must stay noop; reload must not panic")
	}
}
