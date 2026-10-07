package embedding

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	gemmaPath = filepath.Join(dir, LegacyGemmaModelFilename)
	if gemma {
		os.WriteFile(gemmaPath, []byte("gemma"), 0o644)
	}
	if qwen {
		os.WriteFile(filepath.Join(dir, Qwen3ModelFilename), []byte("gguf"), 0o644)
	}
	return dir, gemmaPath
}

// hardenStubs makes the fake 4-byte "GGUF" files resolvable: no release-size
// check, arch read from content ("qwen3"/other), exe dir isolated, plenty of
// memory, fresh backoff state and a controllable clock.
func hardenStubs(t *testing.T) *time.Time {
	t.Helper()
	oSize, oPeek, oExe, oMem, oNow := qwen3ExpectSize, peekArchFn, executableFn, availableMemoryFn, gateNow
	exeDir := t.TempDir()
	qwen3ExpectSize = func() int64 { return 0 }
	peekArchFn = func(p string) (string, error) {
		b, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		if string(b) == "gemma" {
			return "gemma-embedding", nil
		}
		return "qwen3", nil
	}
	executableFn = func() (string, error) { return filepath.Join(exeDir, "maclaw"), nil }
	availableMemoryFn = func() (uint64, bool) { return 64 << 30, true }
	now := time.Unix(1_800_000_000, 0)
	gateNow = func() time.Time { return now }
	ResetQwen3LoadBackoff()
	t.Cleanup(func() {
		qwen3ExpectSize, peekArchFn, executableFn, availableMemoryFn, gateNow = oSize, oPeek, oExe, oMem, oNow
		ResetQwen3LoadBackoff()
	})
	return &now
}

func TestPreferredQwen3ChosenGemmaNeverLoaded(t *testing.T) {
	hardenStubs(t)
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

// Single-model rule: a missing Qwen3 file never brings Gemma in, even when the
// Gemma file is on disk. The host degrades (noop) while the fetch runs.
func TestPreferredQwen3MissingDoesNotFallBackToGemma(t *testing.T) {
	hardenStubs(t)
	t.Setenv(EmbedModelEnv, "")
	t.Setenv(EmbedModelPathEnv, "")
	_, gp := prefModels(t, false, true)
	spy := &loadSpy{}
	res, err := loadPreferredWith(spy.loaders(), gp, PreferredOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !IsNoop(res.Embedder) || res.Family != "" || res.Fallback || res.QwenErr == nil {
		t.Fatalf("got %+v", res)
	}
	if spy.qwenCalls != 0 || spy.gemmaCalls != 0 {
		t.Fatalf("loader calls qwen=%d gemma=%d", spy.qwenCalls, spy.gemmaCalls)
	}
	if PreferredModelFileExists(gp) {
		t.Fatal("only the Gemma file exists: a download is still needed")
	}
}

func TestPreferredQwen3LoadErrorDegradesWithoutGemma(t *testing.T) {
	hardenStubs(t)
	t.Setenv(EmbedModelEnv, "")
	t.Setenv(EmbedModelPathEnv, "")
	_, gp := prefModels(t, true, true)
	spy := &loadSpy{qwenErr: errors.New(`unsupported architecture "llama"`)}
	res, err := loadPreferredWith(spy.loaders(), gp, PreferredOptions{})
	if err == nil || !IsNoop(res.Embedder) || spy.qwenCalls != 1 || spy.gemmaCalls != 0 {
		t.Fatalf("got %+v err=%v spy=%+v", res, err, spy)
	}
}

func TestPreferredSmokeFailureClosesQwen3AndDegrades(t *testing.T) {
	hardenStubs(t)
	t.Setenv(EmbedModelEnv, "")
	t.Setenv(EmbedModelPathEnv, "")
	_, gp := prefModels(t, true, true)
	spy := &loadSpy{qwenSmokeFail: true}
	res, err := loadPreferredWith(spy.loaders(), gp, PreferredOptions{SmokeTest: true})
	if err == nil || !IsNoop(res.Embedder) || spy.gemmaCalls != 0 {
		t.Fatalf("got %+v err=%v", res, err)
	}
	if spy.qwen == nil || !spy.qwen.closed {
		t.Fatal("failed qwen3 instance must be closed")
	}
}

func TestPreferredLoadPanicDegradesNotCrash(t *testing.T) {
	hardenStubs(t)
	t.Setenv(EmbedModelEnv, "")
	t.Setenv(EmbedModelPathEnv, "")
	_, gp := prefModels(t, true, false)
	ld := modelLoaders{qwen3: func(string) (Embedder, error) { panic("corrupt tensor table") }}
	res, err := loadPreferredWith(ld, gp, PreferredOptions{})
	if err == nil || !IsNoop(res.Embedder) || !strings.Contains(err.Error(), "panic") {
		t.Fatalf("got %+v err=%v", res, err)
	}
}

// The retired MACLAW_EMBED_MODEL=gemma pin is ignored: Qwen3 loads, Gemma never.
func TestPreferredLegacyGemmaPinIgnored(t *testing.T) {
	hardenStubs(t)
	t.Setenv(EmbedModelEnv, "gemma")
	_, gp := prefModels(t, true, true)
	spy := &loadSpy{}
	res, err := loadPreferredWith(spy.loaders(), gp, PreferredOptions{})
	if err != nil || res.Family != FamilyQwen3 || spy.gemmaCalls != 0 || spy.qwenCalls != 1 {
		t.Fatalf("got %+v err=%v spy=%+v", res, err, spy)
	}
	if GemmaPinned() {
		t.Fatal("GemmaPinned must be retired (always false)")
	}
}

func TestPreferredNoFilesIsNoop(t *testing.T) {
	hardenStubs(t)
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
// and Gemma is not; with Qwen3 unresolvable the result is noop (no Gemma).
func TestPreferredRealModels(t *testing.T) {
	dir := os.Getenv("MACLAW_TEST_MODELS_DIR")
	if dir == "" {
		t.Skip("MACLAW_TEST_MODELS_DIR not set")
	}
	t.Setenv(EmbedModelEnv, "")
	t.Setenv(EmbedModelPathEnv, "")
	gp := filepath.Join(dir, LegacyGemmaModelFilename)
	res, err := LoadPreferredEmbedder(gp, PreferredOptions{SmokeTest: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.Embedder.(*Qwen3Embedder); !ok || res.Family != FamilyQwen3 || res.Embedder.Dim() != 1024 {
		t.Fatalf("want qwen3, got %s", res)
	}
	res.Embedder.Close()
	// Gemma slot in a directory without the Qwen3 file: noop, never Gemma.
	other := t.TempDir()
	if b, err := os.ReadFile(gp); err == nil {
		os.WriteFile(filepath.Join(other, LegacyGemmaModelFilename), b, 0o644)
	}
	oExe := executableFn
	executableFn = func() (string, error) { return filepath.Join(other, "maclaw"), nil }
	defer func() { executableFn = oExe }()
	res, err = LoadPreferredEmbedder(filepath.Join(other, LegacyGemmaModelFilename), PreferredOptions{SmokeTest: true})
	if err != nil {
		t.Fatal(err)
	}
	if !IsNoop(res.Embedder) || res.Fallback {
		t.Fatalf("want noop (no gemma fallback), got %s", res)
	}
}
