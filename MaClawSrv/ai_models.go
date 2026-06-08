package main

import (
	"archive/zip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/asr"
	"github.com/RapidAI/CodeClaw/corelib/audioconv"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/tts"
)

const (
	srvAIModelEmbedding = "embedding"
	srvAIModelASR       = "asr"
	srvAIModelTTS       = "tts"

	srvASRModelFilename           = "moonshine-base-zh.gguf"
	srvASRModelDefaultURL         = "https://github.com/RapidAI/MaClaw/releases/download/Model_Release/moonshine-base-zh.gguf"
	srvTTSMaxRunes                = 300
	srvASRAudioBodyMaxBytes int64 = 32 << 20
)

var errSrvAIModelUnknown = errors.New("unknown ai model")
var errSrvAIModelNotReady = errors.New("ai model is not ready")
var errSrvAIModelInvalidInput = errors.New("invalid ai model input")

var srvPreparePlayableVoiceMP3 = tts.PreparePlayableVoiceMP3
var srvDownloadModelFile = downloadModelFile

const srvAIModelDownloadStatusMaxAge = 35 * time.Minute

type srvAIModelManager struct {
	dataRoot               string
	downloadConfigProvider func() corelib.AppConfig
	mu                     sync.Mutex
	done                   chan struct{}
	closed                 bool
	downloadWG             sync.WaitGroup
	embeddingRunMu         sync.Mutex
	asrRunMu               sync.Mutex
	ttsRunMu               sync.Mutex
	tasks                  map[string]*srvAIModelTask
	embeddingMgr           embedding.Embedder
	asrMgr                 srvASRTranscriber
	ttsMgr                 srvTTSSynthesizer
	ttsVoice               string
}

type srvASRTranscriber interface {
	TranscribeWAV([]byte) (string, error)
}

type srvTTSSynthesizer interface {
	SynthesizeText(string) ([]byte, error)
	Unload()
}

type srvAIModelTask struct {
	Downloading bool      `json:"downloading"`
	LastError   string    `json:"last_error,omitempty"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
}

type srvAIModelStatus struct {
	Name            string    `json:"name"`
	Enabled         bool      `json:"enabled"`
	Exists          bool      `json:"exists"`
	Ready           bool      `json:"ready"`
	Downloading     bool      `json:"downloading"`
	SizeBytes       int64     `json:"size_bytes,omitempty"`
	Filename        string    `json:"filename,omitempty"`
	Path            string    `json:"path,omitempty"`
	VoiceID         string    `json:"voice_id,omitempty"`
	VoicePath       string    `json:"voice_path,omitempty"`
	Decoder         string    `json:"decoder,omitempty"`
	DecoderReady    *bool     `json:"decoder_ready,omitempty"`
	Encoder         string    `json:"encoder,omitempty"`
	MP3EncoderReady *bool     `json:"mp3_encoder_ready,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
}

func newSrvAIModelManager(dataRoot string) *srvAIModelManager {
	return &srvAIModelManager{dataRoot: dataRoot, done: make(chan struct{}), tasks: map[string]*srvAIModelTask{}}
}

func (m *srvAIModelManager) setDownloadConfigProvider(provider func() corelib.AppConfig) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.downloadConfigProvider = provider
	m.mu.Unlock()
}

func (s *HTTPServer) handleAIModelsStatus(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	cfg := s.effectiveConfigForAIModels(r.Context(), p)
	writeJSON(w, http.StatusOK, map[string]any{"models": s.aiModels.status(cfg)})
}

func (s *HTTPServer) handleAIModelDownload(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	model := strings.ToLower(strings.TrimSpace(r.PathValue("model")))
	cfg := s.effectiveConfigForAIModels(r.Context(), p)
	if err := s.aiModels.startDownload(model, cfg, true); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errSrvAIModelUnknown) {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"model": model, "status": s.aiModels.statusOne(model, cfg)})
}

func (s *HTTPServer) handleAdminAIModelsStatus(w http.ResponseWriter, r *http.Request) {
	cfg := s.defaultConfigForAIModels(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"models": s.aiModels.statusWithPaths(cfg)})
}

func (s *HTTPServer) handleAdminAIModelDownload(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	model := strings.ToLower(strings.TrimSpace(r.PathValue("model")))
	cfg := s.defaultConfigForAIModels(r.Context())
	if err := s.aiModels.startDownload(model, cfg, true); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errSrvAIModelUnknown) {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"model": model, "status": s.aiModels.statusOneWithPath(model, cfg)})
}

func (s *HTTPServer) handleAdminAIModelEmbeddingEmbed(w http.ResponseWriter, r *http.Request) {
	cfg := s.defaultConfigForAIModels(r.Context())
	var in struct {
		Text string `json:"text"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	vector, err := s.aiModels.embedText(r.Context(), cfg, in.Text)
	if err != nil {
		s.writeAIModelRuntimeError(w, srvAIModelEmbedding, cfg, err)
		return
	}
	norm := 0.0
	for _, v := range vector {
		norm += float64(v) * float64(v)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"text":      in.Text,
		"dimension": len(vector),
		"norm":      math.Sqrt(norm),
		"values":    vector,
	})
}

func (s *HTTPServer) handleAdminAIModelASRTranscribe(w http.ResponseWriter, r *http.Request) {
	cfg := s.defaultConfigForAIModels(r.Context())
	if exists, _ := modelFileReady(s.aiModels.modelPath(srvASRModelFilename)); !exists {
		s.writeAIModelRuntimeError(w, srvAIModelASR, cfg, fmt.Errorf("%w: asr model is missing", errSrvAIModelNotReady))
		return
	}
	wav, err := readASRWAVPayload(w, r)
	if err != nil {
		writeASRPayloadError(w, err)
		return
	}
	text, err := s.aiModels.transcribeWAV(r.Context(), cfg, wav)
	if err != nil {
		s.writeAIModelRuntimeError(w, srvAIModelASR, cfg, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
}

func (s *HTTPServer) handleAdminAIModelTTSSynthesize(w http.ResponseWriter, r *http.Request) {
	cfg := s.defaultConfigForAIModels(r.Context())
	var in struct {
		Text    string `json:"text"`
		VoiceID string `json:"voice_id,omitempty"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.VoiceID) != "" {
		cfg.TTSVoiceID = in.VoiceID
	}
	wav, voiceID, err := s.aiModels.synthesizeText(r.Context(), cfg, in.Text)
	if err != nil {
		s.writeAIModelRuntimeError(w, srvAIModelTTS, cfg, err)
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Content-Disposition", `attachment; filename="maclaw-tts-test.wav"`)
	w.Header().Set("X-MaClaw-TTS-Voice-ID", voiceID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(wav)
}

func (s *HTTPServer) handleAIModelASRTranscribe(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	cfg := s.effectiveConfigForAIModels(r.Context(), p)
	if exists, _ := modelFileReady(s.aiModels.modelPath(srvASRModelFilename)); !exists {
		s.writeAIModelRuntimeError(w, srvAIModelASR, cfg, fmt.Errorf("%w: asr model is missing", errSrvAIModelNotReady))
		return
	}
	wav, err := readASRWAVPayload(w, r)
	if err != nil {
		writeASRPayloadError(w, err)
		return
	}
	text, err := s.aiModels.transcribeWAV(r.Context(), cfg, wav)
	if err != nil {
		s.writeAIModelRuntimeError(w, srvAIModelASR, cfg, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
}

func (s *HTTPServer) handleAIModelTTSSynthesize(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	cfg := s.effectiveConfigForAIModels(r.Context(), p)
	var in struct {
		Text    string `json:"text"`
		VoiceID string `json:"voice_id,omitempty"`
		Format  string `json:"format,omitempty"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.VoiceID) != "" {
		cfg.TTSVoiceID = in.VoiceID
	}
	format := normalizeSrvTTSAudioFormat(in.Format)
	if format == "mp3" {
		mp3, voiceID, err := s.aiModels.synthesizeTextMP3(r.Context(), cfg, in.Text)
		if err != nil {
			s.writeAIModelRuntimeError(w, srvAIModelTTS, cfg, err)
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("X-MaClaw-TTS-Voice-ID", voiceID)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(mp3)
		return
	}
	wav, voiceID, err := s.aiModels.synthesizeText(r.Context(), cfg, in.Text)
	if err != nil {
		s.writeAIModelRuntimeError(w, srvAIModelTTS, cfg, err)
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("X-MaClaw-TTS-Voice-ID", voiceID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(wav)
}

func (s *HTTPServer) writeAIModelRuntimeError(w http.ResponseWriter, model string, cfg corelib.AppConfig, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, errSrvAIModelNotReady):
		status = http.StatusConflict
		_ = s.aiModels.startDownload(model, cfg, false)
	case errors.Is(err, errSrvAIModelInvalidInput):
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeASRPayloadError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		status = http.StatusRequestEntityTooLarge
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func (s *HTTPServer) effectiveConfigForAIModels(ctx context.Context, p agentservice.Principal) corelib.AppConfig {
	out, err := s.svc.GetUserConfig(ctx, p)
	if err != nil || out == nil {
		return corelib.AppConfigDefaults()
	}
	return out.AppConfig
}

func (s *HTTPServer) defaultConfigForAIModels(ctx context.Context) corelib.AppConfig {
	out, err := s.svc.GetDefaultClientConfig(ctx)
	if err != nil || out == nil {
		return corelib.AppConfigDefaults()
	}
	if out.UpdatedAt.IsZero() {
		return corelib.AppConfigDefaults()
	}
	return out.AppConfig
}

func (s *HTTPServer) decorateAssistantMessageMetadata(ctx context.Context, p agentservice.Principal, inst agentservice.Instance, sess agentservice.Session, run agentservice.Run, msg agentservice.Message, cfg corelib.AppConfig) map[string]string {
	_ = ctx
	_ = p
	_ = inst
	_ = sess
	_ = run
	if s == nil || s.aiModels == nil || strings.TrimSpace(msg.Content) == "" {
		return nil
	}
	status := s.aiModels.statusOne(srvAIModelTTS, cfg)
	if !status.Ready {
		_ = s.aiModels.startDownload(srvAIModelTTS, cfg, false)
		return map[string]string{
			"tts_available": "false",
			"tts_status":    "model_not_ready",
		}
	}
	return map[string]string{
		"tts_available": "true",
		"tts_endpoint":  "/api/v1/ai-models/tts/synthesize",
		"tts_voice_id":  status.VoiceID,
	}
}

func (s *HTTPServer) ensureConfiguredAIModelsAsync(cfg corelib.AppConfig) {
	if s == nil || s.aiModels == nil {
		return
	}
	_ = s.aiModels.startDownload(srvAIModelEmbedding, cfg, false)
	_ = s.aiModels.startDownload(srvAIModelASR, cfg, false)
	_ = s.aiModels.startDownload(srvAIModelTTS, cfg, false)
}

func (m *srvAIModelManager) status(cfg corelib.AppConfig) map[string]srvAIModelStatus {
	return map[string]srvAIModelStatus{
		srvAIModelEmbedding: m.statusOne(srvAIModelEmbedding, cfg),
		srvAIModelASR:       m.statusOne(srvAIModelASR, cfg),
		srvAIModelTTS:       m.statusOne(srvAIModelTTS, cfg),
	}
}

func (m *srvAIModelManager) statusWithPaths(cfg corelib.AppConfig) map[string]srvAIModelStatus {
	return map[string]srvAIModelStatus{
		srvAIModelEmbedding: m.statusOneWithPath(srvAIModelEmbedding, cfg),
		srvAIModelASR:       m.statusOneWithPath(srvAIModelASR, cfg),
		srvAIModelTTS:       m.statusOneWithPath(srvAIModelTTS, cfg),
	}
}

func (m *srvAIModelManager) statusOneWithPath(model string, cfg corelib.AppConfig) srvAIModelStatus {
	status := m.statusOne(model, cfg)
	if status.Filename != "" {
		status.Path = m.modelPath(status.Filename)
	}
	if model == srvAIModelTTS && status.VoiceID != "" {
		status.VoicePath = filepath.Join(m.ttsVoiceDir(), status.VoiceID+".koro")
	}
	return status
}

func (m *srvAIModelManager) statusOne(model string, cfg corelib.AppConfig) srvAIModelStatus {
	status := srvAIModelStatus{Name: model}
	switch model {
	case srvAIModelEmbedding:
		status.Enabled = true
		status.Filename = embedding.DefaultModelFilename
		status.Exists, status.SizeBytes = modelFileReady(m.modelPath(embedding.DefaultModelFilename))
	case srvAIModelASR:
		status.Enabled = true
		status.Filename = srvASRModelFilename
		status.Decoder = audioconv.CompressedAudioDecoderName
		decoderReady := audioconv.HasCompressedAudioDecoder()
		status.DecoderReady = &decoderReady
		status.Exists, status.SizeBytes = modelFileReady(m.modelPath(srvASRModelFilename))
	case srvAIModelTTS:
		status.Enabled = true
		status.Filename = tts.TTSModelFilename
		status.VoiceID = normalizeSrvTTSVoiceID(cfg.TTSVoiceID)
		status.Encoder = tts.MP3EncoderName
		mp3Ready := tts.HasMP3Encoder()
		status.MP3EncoderReady = &mp3Ready
		modelExists, modelSize := modelFileReady(m.modelPath(tts.TTSModelFilename))
		status.SizeBytes = modelSize
		status.Exists = modelExists && m.ttsVoicesReady()
	default:
		status.Name = model
	}
	status.Ready = status.Enabled && status.Exists
	m.mu.Lock()
	if task := m.tasks[model]; task != nil {
		status.Downloading = task.Downloading
		status.LastError = task.LastError
		status.UpdatedAt = task.UpdatedAt
		if task.Downloading && !task.UpdatedAt.IsZero() && time.Since(task.UpdatedAt) > srvAIModelDownloadStatusMaxAge {
			status.Downloading = false
			if status.LastError == "" {
				status.LastError = "download status expired; retry download"
			}
		}
	}
	m.mu.Unlock()
	if status.Ready {
		status.Downloading = false
		status.LastError = ""
	}
	return status
}

func (m *srvAIModelManager) startDownload(model string, cfg corelib.AppConfig, force bool) error {
	switch model {
	case srvAIModelEmbedding, srvAIModelASR, srvAIModelTTS:
	default:
		return errSrvAIModelUnknown
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return fmt.Errorf("ai model manager is closed")
	}
	m.mu.Unlock()
	if !force && m.statusOne(model, cfg).Exists {
		return nil
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return fmt.Errorf("ai model manager is closed")
	}
	task := m.tasks[model]
	if task == nil {
		task = &srvAIModelTask{}
		m.tasks[model] = task
	}
	if task.Downloading {
		if task.UpdatedAt.IsZero() || time.Since(task.UpdatedAt) <= srvAIModelDownloadStatusMaxAge {
			m.mu.Unlock()
			return nil
		}
		task.Downloading = false
		task.LastError = "download status expired; retrying"
	}
	task.Downloading = true
	task.LastError = ""
	task.UpdatedAt = time.Now().UTC()
	done := m.done
	m.downloadWG.Add(1)
	m.mu.Unlock()

	downloadCfg := m.downloadConfig(cfg)
	go func() {
		defer m.downloadWG.Done()
		err := m.download(model, downloadCfg, done)
		m.mu.Lock()
		task.Downloading = false
		task.UpdatedAt = time.Now().UTC()
		if err != nil {
			task.LastError = err.Error()
			log.Printf("[ai-models] %s download failed: %v", model, err)
		} else {
			task.LastError = ""
			log.Printf("[ai-models] %s model ready", model)
		}
		m.mu.Unlock()
		if err == nil {
			m.resetRuntime(model)
		}
	}()
	return nil
}

func (m *srvAIModelManager) downloadConfig(fallback corelib.AppConfig) corelib.AppConfig {
	m.mu.Lock()
	provider := m.downloadConfigProvider
	m.mu.Unlock()
	if provider == nil {
		return fallback
	}
	return provider()
}

func (m *srvAIModelManager) doneChannel() <-chan struct{} {
	m.mu.Lock()
	done := m.done
	m.mu.Unlock()
	return done
}

func (m *srvAIModelManager) download(model string, cfg corelib.AppConfig, done <-chan struct{}) error {
	switch model {
	case srvAIModelEmbedding:
		return m.downloadWithFallback(embedding.DefaultModelFilename, embedding.DefaultModelDownloadURL, cfg, done)
	case srvAIModelASR:
		return m.downloadWithFallback(srvASRModelFilename, srvASRModelDefaultURL, cfg, done)
	case srvAIModelTTS:
		if err := m.downloadWithFallback(tts.TTSModelFilename, srvTTSAssetURL(tts.TTSModelFilename), cfg, done); err != nil {
			return err
		}
		if m.ttsVoicesReady() {
			return nil
		}
		if err := m.downloadWithFallback(tts.TTSVoiceZipFilename, srvTTSAssetURL(tts.TTSVoiceZipFilename), cfg, done); err != nil {
			return err
		}
		return m.unzipTTSVoices()
	default:
		return errSrvAIModelUnknown
	}
}

func (m *srvAIModelManager) downloadWithFallback(filename, primaryURL string, cfg corelib.AppConfig, done <-chan struct{}) error {
	destPath := m.modelPath(filename)
	if exists, _ := modelFileReady(destPath); exists {
		return nil
	}
	if srvTruthyEnv(os.Getenv("MACLAW_DISABLE_MODEL_DOWNLOADS")) {
		return fmt.Errorf("model downloads are disabled")
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	if err := srvDownloadModelFile(primaryURL, destPath, done); err == nil {
		return nil
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	if hubURL == "" {
		hubURL = strings.TrimRight(strings.TrimSpace(os.Getenv("MACLAW_HUB_URL")), "/")
	}
	if hubURL == "" {
		return fmt.Errorf("download %s failed and Hub URL is not configured", filename)
	}
	return srvDownloadModelFile(hubURL+"/api/v1/models/"+filename, destPath, done)
}

func (m *srvAIModelManager) modelPath(filename string) string {
	return filepath.Join(m.dataRoot, "models", filename)
}

func (m *srvAIModelManager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		close(m.done)
	}
	now := time.Now().UTC()
	for _, task := range m.tasks {
		if task != nil && task.Downloading {
			task.Downloading = false
			task.LastError = "ai model manager is closed"
			task.UpdatedAt = now
		}
	}
	m.mu.Unlock()
	m.downloadWG.Wait()
	m.resetRuntime(srvAIModelEmbedding)
	m.resetRuntime(srvAIModelASR)
	m.resetRuntime(srvAIModelTTS)
}

func (m *srvAIModelManager) resetRuntime(model string) {
	switch model {
	case srvAIModelEmbedding:
		m.embeddingRunMu.Lock()
		defer m.embeddingRunMu.Unlock()
		m.mu.Lock()
		mgr := m.embeddingMgr
		m.embeddingMgr = nil
		m.mu.Unlock()
		if mgr != nil {
			mgr.Close()
		}
	case srvAIModelASR:
		m.asrRunMu.Lock()
		defer m.asrRunMu.Unlock()
		m.mu.Lock()
		m.asrMgr = nil
		m.mu.Unlock()
	case srvAIModelTTS:
		m.ttsRunMu.Lock()
		defer m.ttsRunMu.Unlock()
		m.mu.Lock()
		mgr := m.ttsMgr
		m.ttsMgr = nil
		m.ttsVoice = ""
		m.mu.Unlock()
		if mgr != nil {
			mgr.Unload()
		}
	}
}

func (m *srvAIModelManager) embedText(ctx context.Context, cfg corelib.AppConfig, text string) ([]float32, error) {
	_ = ctx
	_ = cfg
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("%w: text is required", errSrvAIModelInvalidInput)
	}
	modelPath := m.modelPath(embedding.DefaultModelFilename)
	if exists, _ := modelFileReady(modelPath); !exists {
		return nil, fmt.Errorf("%w: embedding model is missing", errSrvAIModelNotReady)
	}
	m.embeddingRunMu.Lock()
	defer m.embeddingRunMu.Unlock()
	m.mu.Lock()
	if m.embeddingMgr == nil {
		mgr, err := embedding.NewGemmaEmbedder(modelPath, 256)
		if err != nil {
			m.mu.Unlock()
			return nil, err
		}
		m.embeddingMgr = mgr
	}
	mgr := m.embeddingMgr
	m.mu.Unlock()
	vector, err := mgr.Embed(text)
	if err != nil {
		return nil, err
	}
	if len(vector) == 0 {
		return nil, fmt.Errorf("%w: embedding model returned empty vector", errSrvAIModelNotReady)
	}
	return vector, nil
}

func (m *srvAIModelManager) transcribeWAV(ctx context.Context, cfg corelib.AppConfig, wav []byte) (string, error) {
	_ = ctx
	if exists, _ := modelFileReady(m.modelPath(srvASRModelFilename)); !exists {
		return "", fmt.Errorf("%w: asr model is missing", errSrvAIModelNotReady)
	}
	m.mu.Lock()
	if m.asrMgr == nil {
		m.asrMgr = asr.NewManager(m.modelPath(srvASRModelFilename))
	}
	mgr := m.asrMgr
	m.mu.Unlock()
	m.asrRunMu.Lock()
	defer m.asrRunMu.Unlock()
	return mgr.TranscribeWAV(wav)
}

func (m *srvAIModelManager) synthesizeText(ctx context.Context, cfg corelib.AppConfig, text string) ([]byte, string, error) {
	_ = ctx
	text = cleanSrvTTSReplyText(text)
	if text == "" {
		return nil, "", fmt.Errorf("%w: text is required", errSrvAIModelInvalidInput)
	}
	if exists, _ := modelFileReady(m.modelPath(tts.TTSModelFilename)); !exists || !m.ttsVoicesReady() {
		return nil, "", fmt.Errorf("%w: tts model is missing", errSrvAIModelNotReady)
	}
	voiceID := normalizeSrvTTSVoiceID(cfg.TTSVoiceID)
	m.ttsRunMu.Lock()
	defer m.ttsRunMu.Unlock()
	m.mu.Lock()
	if m.ttsMgr == nil || m.ttsVoice != voiceID {
		if m.ttsMgr != nil {
			m.ttsMgr.Unload()
		}
		m.ttsMgr = tts.NewKokoroManager(m.modelPath(tts.TTSModelFilename), m.ttsVoiceDir(), voiceID)
		m.ttsVoice = voiceID
	}
	mgr := m.ttsMgr
	m.mu.Unlock()
	wav, err := mgr.SynthesizeText(text)
	if err != nil {
		return nil, voiceID, err
	}
	return wav, voiceID, nil
}

func cleanSrvTTSReplyText(text string) string {
	text = tts.CleanForSpeech(text)
	if text == "" {
		return ""
	}
	if len([]rune(text)) > srvTTSMaxRunes {
		text = tts.TruncateRunesSmart(text, srvTTSMaxRunes)
	}
	return text
}

func (m *srvAIModelManager) synthesizeTextMP3(ctx context.Context, cfg corelib.AppConfig, text string) ([]byte, string, error) {
	wav, voiceID, err := m.synthesizeText(ctx, cfg, text)
	if err != nil {
		return nil, voiceID, err
	}
	playable, err := srvPreparePlayableVoiceMP3(ctx, "voice.wav", wav)
	if err != nil {
		return nil, voiceID, err
	}
	return playable.Data, voiceID, nil
}

func (m *srvAIModelManager) ttsVoiceDir() string {
	return filepath.Join(m.dataRoot, "models", "kokoro_voices")
}

func (m *srvAIModelManager) ttsVoicesReady() bool {
	for _, voiceID := range tts.SupportedTTSVoiceIDs {
		exists, size := fileStatus(filepath.Join(m.ttsVoiceDir(), voiceID+".koro"))
		if !exists || size <= 0 {
			return false
		}
	}
	return true
}

func (m *srvAIModelManager) unzipTTSVoices() error {
	if m.ttsVoicesReady() {
		return nil
	}
	zipPath := m.modelPath(tts.TTSVoiceZipFilename)
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer reader.Close()
	if err := os.MkdirAll(m.ttsVoiceDir(), 0o755); err != nil {
		return err
	}
	for _, f := range reader.File {
		name := filepath.Base(f.Name)
		if name == "." || name == string(filepath.Separator) || !strings.HasSuffix(strings.ToLower(name), ".koro") {
			continue
		}
		src, err := f.Open()
		if err != nil {
			return err
		}
		dstPath := filepath.Join(m.ttsVoiceDir(), name)
		tmpPath := dstPath + ".tmp"
		dst, err := os.Create(tmpPath)
		if err != nil {
			src.Close()
			return err
		}
		_, copyErr := io.Copy(dst, src)
		closeErr := dst.Close()
		src.Close()
		if copyErr != nil {
			_ = os.Remove(tmpPath)
			return copyErr
		}
		if closeErr != nil {
			_ = os.Remove(tmpPath)
			return closeErr
		}
		if err := os.Rename(tmpPath, dstPath); err != nil {
			_ = os.Remove(tmpPath)
			return err
		}
	}
	return nil
}

func fileStatus(path string) (bool, int64) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false, 0
	}
	return true, info.Size()
}

func modelFileReady(path string) (bool, int64) {
	exists, size := fileStatus(path)
	return exists && size > 0, size
}

func srvTTSAssetURL(filename string) string {
	return "https://github.com/RapidAI/MaClaw/releases/download/Model_Release/" + filename
}

func normalizeSrvTTSVoiceID(voiceID string) string {
	voiceID = strings.TrimSpace(voiceID)
	for _, supported := range tts.SupportedTTSVoiceIDs {
		if voiceID == supported {
			return voiceID
		}
	}
	return tts.DefaultTTSVoiceID
}

func normalizeSrvTTSAudioFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "mp3", "audio/mpeg", "audio/mp3":
		return "mp3"
	default:
		return "wav"
	}
}

func srvTruthyEnv(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func readASRWAVPayload(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if r.ContentLength > srvASRAudioBodyMaxBytes {
		return nil, &http.MaxBytesError{Limit: srvASRAudioBodyMaxBytes}
	}
	r.Body = http.MaxBytesReader(w, r.Body, srvASRAudioBodyMaxBytes)
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if contentType == "application/json" || contentType == "" {
		var in struct {
			AudioBase64 string `json:"audio_base64"`
			Format      string `json:"format,omitempty"`
		}
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&in); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				return nil, err
			}
			return nil, fmt.Errorf("invalid json: %w", err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			if err == nil {
				return nil, fmt.Errorf("invalid json: multiple json values")
			}
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				return nil, err
			}
			return nil, fmt.Errorf("invalid json: %w", err)
		}
		if strings.TrimSpace(in.AudioBase64) == "" {
			return nil, fmt.Errorf("audio_base64 is required")
		}
		audio, err := base64.StdEncoding.DecodeString(in.AudioBase64)
		if err != nil {
			return nil, fmt.Errorf("audio_base64 is invalid")
		}
		format := srvASRAudioFormatHint(in.Format)
		if strings.TrimSpace(in.Format) != "" && format == "" {
			return nil, fmt.Errorf("format must be wav, ogg, opus, silk, mp3, m4a, or aac")
		}
		return audioconv.ToWAV(audio, format)
	}
	formatHeader := r.Header.Get("X-MaClaw-Audio-Format")
	format := srvASRAudioFormatHint(formatHeader)
	if strings.TrimSpace(formatHeader) != "" && format == "" {
		return nil, fmt.Errorf("X-MaClaw-Audio-Format must be wav, ogg, opus, silk, mp3, m4a, or aac")
	}
	if format == "" {
		format = srvASRAudioFormatHint(contentType)
	}
	if format == "" && contentType != "application/octet-stream" {
		return nil, fmt.Errorf("content-type must be audio/wav, audio/ogg, audio/opus, audio/mpeg, audio/mp4, audio/aac, application/octet-stream, or application/json")
	}
	audio, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if len(audio) == 0 {
		return nil, fmt.Errorf("audio body is required")
	}
	return audioconv.ToWAV(audio, format)
}

func srvASRAudioFormatHint(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "wav", "wave", "audio/wav", "audio/x-wav":
		return audioconv.FormatWAV
	case "ogg", "oga", "opus", "audio/ogg", "audio/opus":
		return audioconv.FormatOGG
	case "silk", "silk_v3", "audio/silk":
		return audioconv.FormatSilk
	case "mp3", "mpeg", "audio/mpeg", "audio/mp3":
		return audioconv.FormatMP3
	case "m4a", "mp4", "audio/mp4", "audio/m4a", "audio/x-m4a":
		return audioconv.FormatM4A
	case "aac", "audio/aac":
		return audioconv.FormatAAC
	default:
		return ""
	}
}
