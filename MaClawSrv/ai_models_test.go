package main

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/tts"
)

func init() {
	_ = os.Setenv("MACLAW_DISABLE_MODEL_DOWNLOADS", "true")
}

type blockingSrvASRTranscriber struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingSrvASRTranscriber) TranscribeWAV([]byte) (string, error) {
	b.started <- struct{}{}
	<-b.release
	return "ok", nil
}

type blockingSrvTTSSynthesizer struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingSrvTTSSynthesizer) SynthesizeText(string) ([]byte, error) {
	b.started <- struct{}{}
	<-b.release
	return []byte("RIFF-tts"), nil
}

func (b *blockingSrvTTSSynthesizer) Unload() {}

func TestSrvAIModelManagerSerializesSharedASRRuntime(t *testing.T) {
	dataRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataRoot, "models"), 0o755); err != nil {
		t.Fatalf("create models dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataRoot, "models", srvASRModelFilename), []byte("fake-asr-model"), 0o644); err != nil {
		t.Fatalf("write asr model marker: %v", err)
	}
	blocking := &blockingSrvASRTranscriber{started: make(chan struct{}, 2), release: make(chan struct{})}
	manager := newSrvAIModelManager(dataRoot)
	manager.asrMgr = blocking
	errs := make(chan error, 2)
	cfg := corelib.AppConfig{ASREnabled: true}
	go func() {
		_, err := manager.transcribeWAV(context.Background(), cfg, testWAVBytes())
		errs <- err
	}()
	<-blocking.started
	go func() {
		_, err := manager.transcribeWAV(context.Background(), cfg, testWAVBytes())
		errs <- err
	}()
	select {
	case <-blocking.started:
		t.Fatal("second ASR call started before shared runtime was released")
	case <-time.After(100 * time.Millisecond):
	}
	close(blocking.release)
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("ASR call %d failed: %v", i, err)
		}
	}
}

func TestSrvAIModelManagerSerializesSharedTTSRuntime(t *testing.T) {
	dataRoot := t.TempDir()
	seedReadyTTSModel(t, dataRoot)
	blocking := &blockingSrvTTSSynthesizer{started: make(chan struct{}, 2), release: make(chan struct{})}
	manager := newSrvAIModelManager(dataRoot)
	manager.ttsMgr = blocking
	manager.ttsVoice = "zf_xiaoyi"
	errs := make(chan error, 2)
	cfg := corelib.AppConfig{TTSEnabled: true, TTSVoiceID: "zf_xiaoyi"}
	go func() {
		_, _, err := manager.synthesizeText(context.Background(), cfg, "first")
		errs <- err
	}()
	<-blocking.started
	go func() {
		_, _, err := manager.synthesizeText(context.Background(), cfg, "second")
		errs <- err
	}()
	select {
	case <-blocking.started:
		t.Fatal("second TTS call started before shared runtime was released")
	case <-time.After(100 * time.Millisecond):
	}
	close(blocking.release)
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("TTS call %d failed: %v", i, err)
		}
	}
}

func TestSrvAIModelManagerIgnoresManualDisableFlags(t *testing.T) {
	dataRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataRoot, "models"), 0o755); err != nil {
		t.Fatalf("create models dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataRoot, "models", srvASRModelFilename), []byte("fake-asr-model"), 0o644); err != nil {
		t.Fatalf("write asr model marker: %v", err)
	}
	seedReadyTTSModel(t, dataRoot)
	manager := newSrvAIModelManager(dataRoot)
	manager.asrMgr = &fakeSrvASRTranscriber{text: "asr ok"}
	manager.ttsMgr = &fakeSrvTTSSynthesizer{wav: []byte("RIFF-tts")}
	manager.ttsVoice = "zf_xiaoyi"

	for name, got := range manager.status(corelib.AppConfig{ASREnabled: false, TTSEnabled: false, VectorSearchEnabled: false, TTSVoiceID: "zf_xiaoyi"}) {
		if !got.Enabled {
			t.Fatalf("%s status should be auto-enabled: %#v", name, got)
		}
	}
	if got := manager.statusOne(srvAIModelASR, corelib.AppConfig{ASREnabled: false}); !got.Enabled {
		t.Fatalf("ASR status should be auto-enabled: %#v", got)
	}
	text, err := manager.transcribeWAV(context.Background(), corelib.AppConfig{ASREnabled: false}, testWAVBytes())
	if err != nil || text != "asr ok" {
		t.Fatalf("transcribe with disabled flag = %q err=%v", text, err)
	}
	if got := manager.statusOne(srvAIModelTTS, corelib.AppConfig{TTSEnabled: false}); !got.Enabled {
		t.Fatalf("TTS status should be auto-enabled: %#v", got)
	}
	wav, _, err := manager.synthesizeText(context.Background(), corelib.AppConfig{TTSEnabled: false, TTSVoiceID: "zf_xiaoyi"}, "hello")
	if err != nil || string(wav) != "RIFF-tts" {
		t.Fatalf("synthesize with disabled flag = %q err=%v", wav, err)
	}
}

func TestSrvAIModelManagerUnzipsTTSVoicesAtomically(t *testing.T) {
	dataRoot := t.TempDir()
	modelsDir := filepath.Join(dataRoot, "models")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatalf("create models dir: %v", err)
	}
	zipPath := filepath.Join(modelsDir, tts.TTSVoiceZipFilename)
	file, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(file)
	for _, voiceID := range []string{"zm_yunxi", "zm_yunyang", "zf_xiaoxiao", "zf_xiaoyi"} {
		w, err := zw.Create("nested/" + voiceID + ".koro")
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := w.Write([]byte("voice-" + voiceID)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	manager := newSrvAIModelManager(dataRoot)
	if err := manager.unzipTTSVoices(); err != nil {
		t.Fatalf("unzipTTSVoices: %v", err)
	}
	if !manager.ttsVoicesReady() {
		t.Fatal("tts voices not ready after unzip")
	}
	for _, voiceID := range []string{"zm_yunxi", "zm_yunyang", "zf_xiaoxiao", "zf_xiaoyi"} {
		if _, err := os.Stat(filepath.Join(manager.ttsVoiceDir(), voiceID+".koro.tmp")); !os.IsNotExist(err) {
			t.Fatalf("temporary voice file remained for %s: %v", voiceID, err)
		}
	}
}

func TestSrvAIModelManagerRejectsEmptyTTSVoiceFile(t *testing.T) {
	dataRoot := t.TempDir()
	voicesDir := filepath.Join(dataRoot, "models", "kokoro_voices")
	if err := os.MkdirAll(voicesDir, 0o755); err != nil {
		t.Fatalf("create voices dir: %v", err)
	}
	for i, voiceID := range tts.SupportedTTSVoiceIDs {
		data := []byte("voice")
		if i == 0 {
			data = nil
		}
		if err := os.WriteFile(filepath.Join(voicesDir, voiceID+".koro"), data, 0o644); err != nil {
			t.Fatalf("write voice %s: %v", voiceID, err)
		}
	}
	manager := newSrvAIModelManager(dataRoot)
	if manager.ttsVoicesReady() {
		t.Fatal("empty TTS voice file must not be treated as ready")
	}
}

func TestSrvAIModelManagerRejectsEmptyModelFiles(t *testing.T) {
	dataRoot := t.TempDir()
	modelsDir := filepath.Join(dataRoot, "models")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatalf("create models dir: %v", err)
	}
	for _, filename := range []string{embedding.DefaultModelFilename, srvASRModelFilename, tts.TTSModelFilename} {
		if err := os.WriteFile(filepath.Join(modelsDir, filename), nil, 0o644); err != nil {
			t.Fatalf("write empty model %s: %v", filename, err)
		}
	}
	manager := newSrvAIModelManager(dataRoot)
	if got := manager.statusOne(srvAIModelEmbedding, corelib.AppConfig{}); got.Exists || got.Ready {
		t.Fatalf("empty embedding model must not be ready: %#v", got)
	}
	if got := manager.statusOne(srvAIModelASR, corelib.AppConfig{}); got.Exists || got.Ready {
		t.Fatalf("empty asr model must not be ready: %#v", got)
	}
	if _, err := manager.transcribeWAV(context.Background(), corelib.AppConfig{}, testWAVBytes()); !errors.Is(err, errSrvAIModelNotReady) {
		t.Fatalf("empty asr model transcribe err = %v", err)
	}
	if got := manager.statusOne(srvAIModelTTS, corelib.AppConfig{}); got.Exists || got.Ready {
		t.Fatalf("empty tts model must not be ready: %#v", got)
	}
	if _, _, err := manager.synthesizeText(context.Background(), corelib.AppConfig{}, "hello"); !errors.Is(err, errSrvAIModelNotReady) {
		t.Fatalf("empty tts model synthesize err = %v", err)
	}
}

func TestSrvAIModelStatusReadySuppressesStaleDownloading(t *testing.T) {
	dataRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataRoot, "models"), 0o755); err != nil {
		t.Fatalf("create models dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataRoot, "models", embedding.DefaultModelFilename), []byte("model"), 0o644); err != nil {
		t.Fatalf("write embedding model: %v", err)
	}
	manager := newSrvAIModelManager(dataRoot)
	manager.tasks[srvAIModelEmbedding] = &srvAIModelTask{
		Downloading: true,
		LastError:   "old transient download state",
		UpdatedAt:   time.Now().UTC(),
	}

	got := manager.statusOne(srvAIModelEmbedding, corelib.AppConfig{})
	if !got.Ready || got.Downloading || got.LastError != "" {
		t.Fatalf("ready model should suppress stale download task: %#v", got)
	}
}

func TestSrvAIModelManagerUsesGlobalDownloadHubForSharedModels(t *testing.T) {
	t.Setenv("MACLAW_DISABLE_MODEL_DOWNLOADS", "false")
	dataRoot := t.TempDir()
	manager := newSrvAIModelManager(dataRoot)
	manager.setDownloadConfigProvider(func() corelib.AppConfig {
		return corelib.AppConfig{RemoteHubURL: "https://global.example"}
	})

	var mu sync.Mutex
	var urls []string
	oldDownload := srvDownloadModelFile
	srvDownloadModelFile = func(url, destPath string, done <-chan struct{}) error {
		_ = done
		mu.Lock()
		urls = append(urls, url)
		call := len(urls)
		mu.Unlock()
		if call == 1 {
			return errors.New("primary unavailable")
		}
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(destPath, []byte("model"), 0o644)
	}
	t.Cleanup(func() { srvDownloadModelFile = oldDownload })

	userCfg := corelib.AppConfig{RemoteHubURL: "https://user.example"}
	if err := manager.startDownload(srvAIModelEmbedding, userCfg, true); err != nil {
		t.Fatalf("startDownload: %v", err)
	}
	waitSrvAIModelDownloadDone(t, manager, srvAIModelEmbedding)

	mu.Lock()
	got := append([]string(nil), urls...)
	mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("download calls = %d, want 2: %#v", len(got), got)
	}
	if got[1] != "https://global.example/api/v1/models/"+embedding.DefaultModelFilename {
		t.Fatalf("fallback URL = %q, want global hub", got[1])
	}
}

func waitSrvAIModelDownloadDone(t *testing.T, manager *srvAIModelManager, model string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		manager.mu.Lock()
		task := manager.tasks[model]
		downloading := task != nil && task.Downloading
		lastErr := ""
		if task != nil {
			lastErr = task.LastError
		}
		manager.mu.Unlock()
		if !downloading {
			if lastErr != "" {
				t.Fatalf("%s download failed: %s", model, lastErr)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s download did not finish", model)
}
