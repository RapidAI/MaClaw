package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// Tests in this package download fake model bytes; the manifest check is
// exercised explicitly below.
func init() { srvVerifyEmbeddingModel = func(string) error { return nil } }

func TestSrvQwen3DownloadRejectsCorruptFileAndRemovesIt(t *testing.T) {
	t.Setenv(embedding.EmbedModelEnv, "")
	t.Setenv("MACLAW_EMBEDDING_MODEL_PATH", "")
	t.Setenv("MACLAW_DISABLE_MODEL_DOWNLOADS", "")
	orig := srvVerifyEmbeddingModel
	srvVerifyEmbeddingModel = func(p string) error { return embedding.VerifyModelFile(p, embedding.Qwen3ModelSpec) }
	defer func() { srvVerifyEmbeddingModel = orig }()
	origDL := srvDownloadModelFile
	var gotURL string
	srvDownloadModelFile = func(url, dest string, done <-chan struct{}) error {
		gotURL = url
		return os.WriteFile(dest, []byte("truncated"), 0o644)
	}
	defer func() { srvDownloadModelFile = origDL }()

	m := newSrvAIModelManager(t.TempDir())
	err := m.download(srvAIModelEmbedding, corelib.AppConfig{}, nil)
	if !errors.Is(err, embedding.ErrModelCorrupt) {
		t.Fatalf("want ErrModelCorrupt, got %v", err)
	}
	if gotURL != embedding.Qwen3ModelSpec.URLs[0] {
		t.Fatalf("primary URL = %q, want the release asset %q", gotURL, embedding.Qwen3ModelSpec.URLs[0])
	}
	if _, err := os.Stat(filepath.Join(m.dataRoot, "models", embedding.Qwen3ModelFilename)); !os.IsNotExist(err) {
		t.Fatalf("corrupt download must be removed (stat err=%v)", err)
	}
}

func TestSrvEmbedderLoadsQwen3NotGemma(t *testing.T) {
	t.Setenv(embedding.EmbedModelEnv, "")
	var loaded string
	orig := srvLoadEmbedder
	srvLoadEmbedder = func(p string) (embedding.Embedder, error) { loaded = p; return &countingSrvEmbedder{}, nil }
	defer func() { srvLoadEmbedder = orig }()
	dataRoot := t.TempDir()
	os.MkdirAll(filepath.Join(dataRoot, "models"), 0o755)
	os.WriteFile(filepath.Join(dataRoot, "models", embedding.DefaultModelFilename), []byte("gemma"), 0o644)
	m := newSrvAIModelManager(dataRoot)
	if _, err := m.embedBatch(nil, corelib.AppConfig{}, []string{"x"}); err == nil {
		t.Fatal("only the Gemma file exists: the server must report not-ready, not load Gemma")
	}
	os.WriteFile(filepath.Join(dataRoot, "models", embedding.Qwen3ModelFilename), []byte("qwen3"), 0o644)
	if _, err := m.embedBatch(nil, corelib.AppConfig{}, []string{"x"}); err != nil {
		t.Fatal(err)
	}
	if filepath.Base(loaded) != embedding.Qwen3ModelFilename {
		t.Fatalf("loaded %q", loaded)
	}
	id := srvAIModelEmbedderAdapter{manager: m}.ModelID()
	if want := embedding.Qwen3ModelFilename + ":1024:"; len(id) < len(want) || id[:len(want)] != want {
		t.Fatalf("ModelID %q", id)
	}
}
