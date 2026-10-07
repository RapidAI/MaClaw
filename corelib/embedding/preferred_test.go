package embedding

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fakePrefEmb struct {
	dim    int
	id     string
	closed bool
	fail   bool
}

func (f *fakePrefEmb) Embed(string) ([]float32, error) {
	if f.fail {
		return nil, errors.New("boom")
	}
	return make([]float32, f.dim), nil
}
func (f *fakePrefEmb) EmbedBatch(t []string) ([][]float32, error) {
	return make([][]float32, len(t)), nil
}
func (f *fakePrefEmb) Dim() int        { return f.dim }
func (f *fakePrefEmb) Close()          { f.closed = true }
func (f *fakePrefEmb) ModelID() string { return f.id }

type loadSpy struct {
	qwenCalls, gemmaCalls int
	qwenErr               error
	qwenSmokeFail         bool
	qwen                  *fakePrefEmb
}

func (s *loadSpy) loaders() modelLoaders {
	return modelLoaders{
		qwen3: func(string) (Embedder, error) {
			s.qwenCalls++
			if s.qwenErr != nil {
				return nil, s.qwenErr
			}
			s.qwen = &fakePrefEmb{dim: 1024, id: "qwen3-embedding:1024", fail: s.qwenSmokeFail}
			return s.qwen, nil
		},
		gemma: func(string, int) (Embedder, error) {
			s.gemmaCalls++
			return &fakePrefEmb{dim: 768, id: "gemma-embedding:768"}, nil
		},
	}
}

func prefModels(t *testing.T, qwen, gemma bool) (dir, gemmaPath string) {
	t.Helper()
	dir = t.TempDir()
	gemmaPath = filepath.Join(dir, DefaultModelFilename)
	if gemma {
		os.WriteFile(gemmaPath, []byte("gguf"), 0o644)
	}
	if qwen {
		os.WriteFile(filepath.Join(dir, Qwen3ModelFilename), []byte("gguf"), 0o644)
	}
	return dir, gemmaPath
}

func TestPreferredQwen3ChosenGemmaNeverLoaded(t *testing.T) {
	t.Setenv(EmbedModelEnv, "")
	t.Setenv(EmbedModelPathEnv, "")
	_, gp := prefModels(t, true, true)
	spy := &loadSpy{}
	res, err := loadPreferredWith(spy.loaders(), gp, PreferredOptions{SmokeTest: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Family != FamilyQwen3 || res.Fallback || res.Embedder.Dim() != 1024 {
		t.Fatalf("got %+v", res)
	}
	if spy.gemmaCalls != 0 {
		t.Fatalf("gemma loader called %d times while qwen3 loaded", spy.gemmaCalls)
	}
	if EmbedderFamily(res.Embedder) != FamilyQwen3 {
		t.Fatalf("family %q", EmbedderFamily(res.Embedder))
	}
}

func TestPreferredFallbackWhenQwen3Missing(t *testing.T) {
	t.Setenv(EmbedModelEnv, "")
	t.Setenv(EmbedModelPathEnv, "")
	_, gp := prefModels(t, false, true)
	spy := &loadSpy{}
	res, err := loadPreferredWith(spy.loaders(), gp, PreferredOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Family != FamilyGemma || !res.Fallback || res.QwenErr == nil {
		t.Fatalf("got %+v", res)
	}
	if spy.qwenCalls != 0 {
		t.Fatalf("qwen3 loader must not run for a missing file")
	}
	if spy.gemmaCalls != 1 {
		t.Fatalf("gemma calls %d", spy.gemmaCalls)
	}
}

func TestPreferredFallbackWhenQwen3PathEnvMissing(t *testing.T) {
	t.Setenv(EmbedModelEnv, "qwen3")
	_, gp := prefModels(t, true, true)
	t.Setenv(EmbedModelPathEnv, filepath.Join(t.TempDir(), "nope.gguf"))
	spy := &loadSpy{}
	res, err := loadPreferredWith(spy.loaders(), gp, PreferredOptions{})
	if err != nil || res.Family != FamilyGemma || !res.Fallback {
		t.Fatalf("got %+v err=%v", res, err)
	}
}

func TestPreferredFallbackWhenQwen3Unsupported(t *testing.T) {
	t.Setenv(EmbedModelEnv, "")
	t.Setenv(EmbedModelPathEnv, "")
	_, gp := prefModels(t, true, true)
	spy := &loadSpy{qwenErr: errors.New(`unsupported architecture "llama"`)}
	res, err := loadPreferredWith(spy.loaders(), gp, PreferredOptions{})
	if err != nil || res.Family != FamilyGemma || spy.qwenCalls != 1 || spy.gemmaCalls != 1 {
		t.Fatalf("got %+v err=%v spy=%+v", res, err, spy)
	}
}

func TestPreferredSmokeFailureClosesQwen3ThenFallsBack(t *testing.T) {
	t.Setenv(EmbedModelEnv, "")
	t.Setenv(EmbedModelPathEnv, "")
	_, gp := prefModels(t, true, true)
	spy := &loadSpy{qwenSmokeFail: true}
	res, err := loadPreferredWith(spy.loaders(), gp, PreferredOptions{SmokeTest: true})
	if err != nil || res.Family != FamilyGemma {
		t.Fatalf("got %+v err=%v", res, err)
	}
	if spy.qwen == nil || !spy.qwen.closed {
		t.Fatal("failed qwen3 instance must be closed before gemma loads (never two models)")
	}
}

func TestPreferredGemmaPinnedSkipsQwen3(t *testing.T) {
	t.Setenv(EmbedModelEnv, "gemma")
	_, gp := prefModels(t, true, true)
	spy := &loadSpy{}
	res, err := loadPreferredWith(spy.loaders(), gp, PreferredOptions{})
	if err != nil || res.Family != FamilyGemma || res.Fallback || spy.qwenCalls != 0 {
		t.Fatalf("got %+v err=%v spy=%+v", res, err, spy)
	}
}

func TestPreferredNoFilesIsNoop(t *testing.T) {
	t.Setenv(EmbedModelEnv, "")
	t.Setenv(EmbedModelPathEnv, "")
	_, gp := prefModels(t, false, false)
	spy := &loadSpy{}
	res, err := loadPreferredWith(spy.loaders(), gp, PreferredOptions{})
	if err != nil || !IsNoop(res.Embedder) || spy.qwenCalls+spy.gemmaCalls != 0 {
		t.Fatalf("got %+v err=%v", res, err)
	}
	if PreferredModelFileExists(gp) {
		t.Fatal("no files exist")
	}
}

// Real GGUFs (MACLAW_TEST_MODELS_DIR containing both files): Qwen3 is loaded
// and Gemma is not; with the Qwen3 path pointed at a missing file, Gemma.
func TestPreferredRealModels(t *testing.T) {
	dir := os.Getenv("MACLAW_TEST_MODELS_DIR")
	if dir == "" {
		t.Skip("MACLAW_TEST_MODELS_DIR not set")
	}
	t.Setenv(EmbedModelEnv, "")
	t.Setenv(EmbedModelPathEnv, "")
	gp := filepath.Join(dir, DefaultModelFilename)
	res, err := LoadPreferredEmbedder(gp, PreferredOptions{SmokeTest: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.Embedder.(*Qwen3Embedder); !ok || res.Family != FamilyQwen3 || res.Embedder.Dim() != 1024 {
		t.Fatalf("want qwen3, got %s", res)
	}
	res.Embedder.Close()
	t.Setenv(EmbedModelPathEnv, filepath.Join(dir, "missing-qwen3.gguf"))
	res, err = LoadPreferredEmbedder(gp, PreferredOptions{SmokeTest: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.Embedder.(*GemmaEmbedder); !ok || !res.Fallback || res.Embedder.Dim() != 768 {
		t.Fatalf("want gemma fallback, got %s", res)
	}
	res.Embedder.Close()
}
