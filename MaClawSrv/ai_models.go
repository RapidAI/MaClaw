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
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/guiautomation"
	"github.com/RapidAI/CodeClaw/corelib/ocr"
	"github.com/RapidAI/CodeClaw/corelib/tts"
)

const (
	srvAIModelEmbedding  = "embedding"
	srvAIModelASR        = "asr"
	srvAIModelTTS        = "tts"
	srvAIModelOCR        = "ocr"
	srvAIModelOmniParser = "omniparser"

	// srvOmniParserFilename is the OmniParser V2 weight the desktop app stores
	// as omniparser-v2.yolow. The server downloads the same release asset.
	srvOmniParserFilename   = "omniparser-v2.yolow"
	srvOmniParserDefaultURL = "https://github.com/RapidAI/MaClaw/releases/download/Model_Release/omniparser-v2.yolow"

	srvASRModelFilename   = asr.DefaultModelFilename
	srvASRModelDefaultURL = asr.DefaultModelDownloadURL
	// Per-chunk budget for long-form TTS (clamped further inside tts.SplitSpeechChunks).
	// Total reply length is no longer hard-truncated — semantic chunks are concatenated.
	srvTTSMaxRunes                = tts.DefaultSpeechChunkRunes
	srvASRAudioBodyMaxBytes int64 = 32 << 20

	// srvAIEmbeddingDim is the width of the vectors the server stores. The
	// server uses the same width as the desktop app: the embedding model is
	// MRL-trained, and the 256-dim truncation was measured to collapse CJK
	// discrimination (see embedding.DefaultEmbeddingDim for that measurement —
	// a weather query ranked git_status above web_search). Keeping both
	// surfaces on one width also means one ModelID per model file instead of
	// two spaces that only differ by how much of the vector was kept.
	//
	// Tied to embedding.DefaultEmbeddingDim instead of written as a literal so
	// the server cannot drift from the production width. It must also stay in
	// sync with the width the loaded model reports: it is part of the
	// vector space identity reported by ModelID, so a mismatch would let the
	// knowledge store believe it is reading one space while the manager writes
	// another. TestSrvAIEmbeddingDimMatchesProductionWidth pins both.
	srvAIEmbeddingDim = embedding.DefaultEmbeddingDim
)

// Single shipped embedding model: the server follows the desktop app onto
// Qwen3-Embedding-0.6B (full 1024 width, Qwen3 instruct prompts); there is no
// other local model. The filename, width and prompt recipe all feed the
// adapter's ModelID, so a store written by the retired EmbeddingGemma model is
// re-embedded once instead of mixing spaces.
func srvEmbedFamily() string {
	return embedding.FamilyQwen3
}

func srvEmbeddingModelFilename() string {
	return embedding.Qwen3ModelFilename
}

func srvEmbeddingDim() int {
	return embedding.Qwen3EmbeddingDim
}

// srvVerifyEmbeddingModel checks a downloaded Qwen3 file against the release
// manifest (size + sha256). Swappable in tests that download fake bytes.
var srvVerifyEmbeddingModel = func(path string) error {
	return embedding.VerifyModelFile(path, embedding.Qwen3ModelSpec)
}

// srvLoadEmbedder loads the model at path (swappable in tests). Qwen3 goes
// through the hardened factory (low-memory guard, failure backoff, panic-safe).
var srvLoadEmbedder = func(path string) (embedding.Embedder, error) {
	res, err := embedding.LoadPreferredEmbedder(path, embedding.PreferredOptions{})
	if err != nil {
		return nil, err
	}
	if embedding.IsNoop(res.Embedder) {
		return nil, fmt.Errorf("%w: %v", errSrvAIModelNotReady, res.QwenErr)
	}
	return res.Embedder, nil
}

var errSrvAIModelUnknown = errors.New("unknown ai model")
var errSrvAIModelNotReady = errors.New("ai model is not ready")
var errSrvAIModelInvalidInput = errors.New("invalid ai model input")

var srvPreparePlayableVoiceMP3 = tts.PreparePlayableVoiceMP3
var srvDownloadModelFile = downloadModelFileSlowAbort

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
	ocrRunMu               sync.Mutex
	yoloRunMu              sync.Mutex
	tasks                  map[string]*srvAIModelTask
	embeddingMgr           embedding.Embedder
	embeddingPath          string
	asrMgr                 srvASRTranscriber
	ttsMgr                 srvTTSSynthesizer
	ttsVoice               string
	ocrMgr                 *browser.NativeOCRProvider
	yoloParser             *guiautomation.YOLOScreenParser
	embeddingReadyHook     func()
	remote                 *modelRemote
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
	return &srvAIModelManager{dataRoot: dataRoot, done: make(chan struct{}), tasks: map[string]*srvAIModelTask{}, remote: newModelRemoteFromEnv()}
}

func (m *srvAIModelManager) setDownloadConfigProvider(provider func() corelib.AppConfig) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.downloadConfigProvider = provider
	m.mu.Unlock()
}

func (m *srvAIModelManager) setEmbeddingReadyHook(hook func()) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.embeddingReadyHook = hook
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
	if _, exists := asr.ModelFileStatus(s.aiModels.modelPath(srvASRModelFilename)); !exists {
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
	if _, exists := asr.ModelFileStatus(s.aiModels.modelPath(srvASRModelFilename)); !exists {
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
	_ = s.aiModels.startDownload(srvAIModelOCR, cfg, false)
	_ = s.aiModels.startDownload(srvAIModelOmniParser, cfg, false)
}

func (m *srvAIModelManager) status(cfg corelib.AppConfig) map[string]srvAIModelStatus {
	return map[string]srvAIModelStatus{
		srvAIModelEmbedding:  m.statusOne(srvAIModelEmbedding, cfg),
		srvAIModelASR:        m.statusOne(srvAIModelASR, cfg),
		srvAIModelTTS:        m.statusOne(srvAIModelTTS, cfg),
		srvAIModelOCR:        m.statusOne(srvAIModelOCR, cfg),
		srvAIModelOmniParser: m.statusOne(srvAIModelOmniParser, cfg),
	}
}

func (m *srvAIModelManager) statusWithPaths(cfg corelib.AppConfig) map[string]srvAIModelStatus {
	return map[string]srvAIModelStatus{
		srvAIModelEmbedding:  m.statusOneWithPath(srvAIModelEmbedding, cfg),
		srvAIModelASR:        m.statusOneWithPath(srvAIModelASR, cfg),
		srvAIModelTTS:        m.statusOneWithPath(srvAIModelTTS, cfg),
		srvAIModelOCR:        m.statusOneWithPath(srvAIModelOCR, cfg),
		srvAIModelOmniParser: m.statusOneWithPath(srvAIModelOmniParser, cfg),
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
		status.Filename = srvEmbeddingModelFilename()
		status.Exists, status.SizeBytes = modelFileReady(m.modelPath(status.Filename))
	case srvAIModelASR:
		status.Enabled = true
		status.Filename = srvASRModelFilename
		status.Decoder = audioconv.CompressedAudioDecoderName
		decoderReady := audioconv.HasCompressedAudioDecoder()
		status.DecoderReady = &decoderReady
		status.SizeBytes, status.Exists = asr.ModelFileStatus(m.modelPath(srvASRModelFilename))
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
	case srvAIModelOCR:
		status.Enabled = true
		status.Filename = ocr.DetModelFilename(corelib.DefaultOCRModelTier)
		status.Exists, status.SizeBytes = m.ocrModelsReady()
	case srvAIModelOmniParser:
		status.Enabled = true
		status.Filename = srvOmniParserFilename
		status.Exists, status.SizeBytes = yolowFileReady(m.modelPath(srvOmniParserFilename))
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
	case srvAIModelEmbedding, srvAIModelASR, srvAIModelTTS, srvAIModelOCR, srvAIModelOmniParser:
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
			if model == srvAIModelEmbedding {
				m.mu.Lock()
				hook := m.embeddingReadyHook
				m.mu.Unlock()
				if hook != nil {
					hook()
				}
			}
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
		if err := m.downloadWithFallback(embedding.Qwen3ModelFilename, embedding.Qwen3ModelSpec.URLs[0], cfg, done); err != nil {
			return err
		}
		// sha256 + size against the release manifest; a bad file is removed
		// so the next attempt re-downloads instead of loading garbage.
		path := m.modelPath(embedding.Qwen3ModelFilename)
		if err := srvVerifyEmbeddingModel(path); err != nil {
			if os.Getenv("MACLAW_EMBEDDING_MODEL_PATH") == "" {
				_ = os.Remove(path)
			}
			return fmt.Errorf("verify %s: %w", embedding.Qwen3ModelFilename, err)
		}
		return nil
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
	case srvAIModelOCR:
		return m.downloadOCRModels(cfg, done)
	case srvAIModelOmniParser:
		return m.downloadOmniParser(cfg, done)
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
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	if hubURL == "" {
		hubURL = strings.TrimRight(strings.TrimSpace(os.Getenv("MACLAW_HUB_URL")), "/")
	}
	urls := srvModelDownloadURLs(primaryURL, hubURL, filename)
	var lastErr error
	for i, u := range urls {
		// Every source but the last gives up when it crawls, so a slow
		// overseas link falls through to the next one instead of holding
		// the model for half an hour.
		err := srvDownloadModelFile(u, destPath, done, i < len(urls)-1)
		if err == nil {
			return nil
		}
		log.Printf("[ai-models] download %s from %s failed: %v", filename, u, err)
		lastErr = err
	}
	if hubURL == "" && lastErr != nil {
		return fmt.Errorf("download %s failed and Hub URL is not configured: %w", filename, lastErr)
	}
	return lastErr
}

// srvModelDownloadURLs orders model sources: MACLAW_MODEL_MIRROR_URLS bases
// first (comma separated, e.g. a domestic object store), then the Hub mirror
// (same region as this server), then the GitHub release.
func srvModelDownloadURLs(primaryURL, hubURL, filename string) []string {
	var urls []string
	for _, base := range strings.Split(os.Getenv("MACLAW_MODEL_MIRROR_URLS"), ",") {
		if base = strings.TrimRight(strings.TrimSpace(base), "/"); base != "" {
			urls = append(urls, base+"/"+filename)
		}
	}
	if hubURL != "" {
		urls = append(urls, hubURL+"/api/v1/models/"+filename)
	}
	return append(urls, primaryURL)
}

func (m *srvAIModelManager) modelPath(filename string) string {
	if filename == srvEmbeddingModelFilename() {
		if custom := strings.TrimSpace(os.Getenv("MACLAW_EMBEDDING_MODEL_PATH")); custom != "" {
			return custom
		}
	}
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
	m.resetRuntime(srvAIModelOCR)
	m.resetRuntime(srvAIModelOmniParser)
}

func (m *srvAIModelManager) resetRuntime(model string) {
	switch model {
	case srvAIModelEmbedding:
		m.embeddingRunMu.Lock()
		defer m.embeddingRunMu.Unlock()
		m.mu.Lock()
		mgr := m.embeddingMgr
		m.embeddingMgr = nil
		m.embeddingPath = ""
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
	case srvAIModelOCR:
		m.ocrRunMu.Lock()
		defer m.ocrRunMu.Unlock()
		m.mu.Lock()
		mgr := m.ocrMgr
		m.ocrMgr = nil
		m.mu.Unlock()
		if mgr != nil {
			mgr.Close()
		}
	case srvAIModelOmniParser:
		m.yoloRunMu.Lock()
		defer m.yoloRunMu.Unlock()
		m.mu.Lock()
		parser := m.yoloParser
		m.yoloParser = nil
		m.mu.Unlock()
		if parser != nil {
			parser.Unload()
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
	vectors, err := m.embedBatch(ctx, cfg, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("%w: embedding model returned unexpected batch size", errSrvAIModelNotReady)
	}
	return vectors[0], nil
}

func (m *srvAIModelManager) embedBatch(ctx context.Context, cfg corelib.AppConfig, texts []string) ([][]float32, error) {
	_ = ctx
	_ = cfg
	cleaned := make([]string, len(texts))
	for i, text := range texts {
		text = strings.TrimSpace(text)
		if text == "" {
			return nil, fmt.Errorf("%w: text %d is required", errSrvAIModelInvalidInput, i)
		}
		cleaned[i] = text
	}
	if len(cleaned) == 0 {
		return [][]float32{}, nil
	}
	if m.remote != nil {
		v, err := m.remote.embed(ctx, cleaned)
		if !m.remote.useLocal(err) {
			return v, err
		}
	}
	modelPath := m.modelPath(srvEmbeddingModelFilename())
	if exists, _ := modelFileReady(modelPath); !exists {
		return nil, fmt.Errorf("%w: embedding model is missing", errSrvAIModelNotReady)
	}
	m.embeddingRunMu.Lock()
	defer m.embeddingRunMu.Unlock()
	m.mu.Lock()
	if m.embeddingMgr != nil && m.embeddingPath == "" {
		m.embeddingPath = modelPath
	}
	if m.embeddingMgr != nil && m.embeddingPath != "" && m.embeddingPath != modelPath {
		old := m.embeddingMgr
		m.embeddingMgr = nil
		m.embeddingPath = ""
		m.mu.Unlock()
		old.Close()
		m.mu.Lock()
	}
	if m.embeddingMgr == nil {
		mgr, err := srvLoadEmbedder(modelPath)
		if err != nil {
			m.mu.Unlock()
			return nil, err
		}
		m.embeddingMgr = mgr
		m.embeddingPath = modelPath
	}
	mgr := m.embeddingMgr
	m.mu.Unlock()
	vectors, err := mgr.EmbedBatch(cleaned)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(cleaned) {
		return nil, fmt.Errorf("%w: embedding model returned %d vectors for %d texts", errSrvAIModelNotReady, len(vectors), len(cleaned))
	}
	for i, vector := range vectors {
		if len(vector) == 0 {
			return nil, fmt.Errorf("%w: embedding model returned empty vector at index %d", errSrvAIModelNotReady, i)
		}
	}
	return vectors, nil
}

func (m *srvAIModelManager) transcribeWAV(ctx context.Context, cfg corelib.AppConfig, wav []byte) (string, error) {
	if m.remote != nil {
		t, err := m.remote.asr(ctx, wav)
		if !m.remote.useLocal(err) {
			return t, err
		}
	}
	if _, exists := asr.ModelFileStatus(m.modelPath(srvASRModelFilename)); !exists {
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
	return m.synthesizeTextWithVoice(ctx, cfg, "", text)
}

// synthesizeTextWithVoice keeps the user-level configuration as the fallback
// while allowing a hardware binding to select one bundled Kokoro voice.
func (m *srvAIModelManager) synthesizeTextWithVoice(ctx context.Context, cfg corelib.AppConfig, requestedVoiceID, text string) ([]byte, string, error) {
	_ = ctx
	text = cleanSrvTTSReplyText(text)
	if text == "" {
		return nil, "", fmt.Errorf("%w: text is required", errSrvAIModelInvalidInput)
	}
	voiceID := normalizeSrvTTSVoiceID(requestedVoiceID)
	if strings.TrimSpace(requestedVoiceID) == "" {
		voiceID = normalizeSrvTTSVoiceID(cfg.TTSVoiceID)
	}
	if m.remote != nil {
		wav, v, err := m.remote.tts(ctx, voiceID, text)
		if !m.remote.useLocal(err) {
			if v == "" {
				v = voiceID
			}
			return wav, v, err
		}
	}
	if exists, _ := modelFileReady(m.modelPath(tts.TTSModelFilename)); !exists || !m.ttsVoiceReady(voiceID) {
		return nil, "", fmt.Errorf("%w: tts model is missing", errSrvAIModelNotReady)
	}
	m.ttsRunMu.Lock()
	defer m.ttsRunMu.Unlock()
	m.mu.Lock()
	// Tests and embedders can inject a synthesizer without a concrete Kokoro
	// runtime. It has no per-voice state, so keep it instead of replacing it.
	// The normal production path always creates/replaces a Kokoro manager.
	if m.ttsMgr != nil && m.ttsVoice != voiceID {
		if _, ok := m.ttsMgr.(*tts.Manager); !ok {
			m.ttsVoice = voiceID
		}
	}
	if m.ttsMgr == nil || m.ttsVoice != voiceID {
		if m.ttsMgr != nil {
			m.ttsMgr.Unload()
		}
		m.ttsMgr = tts.NewKokoroManager(m.modelPath(tts.TTSModelFilename), m.ttsVoiceDir(), voiceID)
		m.ttsVoice = voiceID
	}
	mgr := m.ttsMgr
	m.mu.Unlock()
	// Long-form: semantic chunk + concatenate (no silent mid-text truncate).
	// Soft-cap pathological inputs; srvTTSMaxRunes is per-chunk budget.
	text = tts.CapSpeechText(text, tts.MaxLongFormSpeechRunes)
	wav, _, err := tts.SynthesizeSpeechLongWAV(mgr, text, srvTTSMaxRunes)
	if err != nil {
		return nil, voiceID, err
	}
	return wav, voiceID, nil
}

func cleanSrvTTSReplyText(text string) string {
	// Clean only — length is handled by semantic chunking / CapSpeechText.
	return tts.CleanForSpeech(text)
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

// srvAIModelEmbedderAdapter is stored in embedding.Embedder and compared
// with == (query-vector reuse, local L3 attach). A func field makes that
// comparison panic: "comparing uncomparable type". Live config stays on the
// manager, which is already a comparable pointer.
type srvAIModelEmbedderAdapter struct {
	manager *srvAIModelManager
}

func (a srvAIModelEmbedderAdapter) appConfig() corelib.AppConfig {
	fallback := corelib.AppConfigDefaults()
	if a.manager == nil {
		return fallback
	}
	return a.manager.downloadConfig(fallback)
}

func (a srvAIModelEmbedderAdapter) Embed(text string) ([]float32, error) {
	if a.manager == nil {
		return nil, errSrvAIModelNotReady
	}
	// The loaded model does not read download config. startDownload loads the
	// live provider itself and uses this fallback only when none is installed.
	vector, err := a.manager.embedText(context.Background(), corelib.AppConfig{}, text)
	if errors.Is(err, errSrvAIModelNotReady) {
		_ = a.manager.startDownload(srvAIModelEmbedding, corelib.AppConfigDefaults(), false)
	}
	return vector, err
}

func (a srvAIModelEmbedderAdapter) EmbedBatch(texts []string) ([][]float32, error) {
	if a.manager == nil {
		return nil, errSrvAIModelNotReady
	}
	vectors, err := a.manager.embedBatch(context.Background(), corelib.AppConfig{}, texts)
	if errors.Is(err, errSrvAIModelNotReady) {
		_ = a.manager.startDownload(srvAIModelEmbedding, corelib.AppConfigDefaults(), false)
	}
	return vectors, err
}

func (srvAIModelEmbedderAdapter) Dim() int { return srvEmbeddingDim() }
func (srvAIModelEmbedderAdapter) Close()   {}

// EmbedWithRole and EmbedBatchWithRole make the adapter satisfy
// embedding.RoleEmbedder. The adapter is the only embedder the server-side
// knowledge store ever sees, so without them EmbedAs would fall back to the
// un-prompted space and the server would index and query a different vector
// space than the desktop app — the prompt feature would be silently
// half-applied.
func (a srvAIModelEmbedderAdapter) EmbedWithRole(text string, role embedding.Role) ([]float32, error) {
	return a.Embed(embedding.ApplyFamilyPrompt(srvEmbedFamily(), text, role))
}

func (a srvAIModelEmbedderAdapter) EmbedBatchWithRole(texts []string, role embedding.Role) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	fam := srvEmbedFamily()
	if !embedding.FamilyNeedsPrompt(fam, role) {
		return a.EmbedBatch(texts)
	}
	prompted := make([]string, len(texts))
	for i, text := range texts {
		prompted[i] = embedding.ApplyFamilyPrompt(fam, text, role)
	}
	return a.EmbedBatch(prompted)
}

// ModelID identifies the vector space this adapter serves. Without it the
// knowledge store falls back to "%T:%d", which does not encode the prompt
// regime — enabling prompts would then leave every stored vector in place and
// the index would hold two incompatible spaces.
//
// The model name comes from the path the manager actually resolves, not from a
// hardcoded constant, so MACLAW_EMBEDDING_MODEL_PATH pointing at a different
// file yields a different identity instead of silently reusing the old index.
//
// It deliberately does not depend on whether the weights have finished
// loading: a value that changed at load time would trigger a second full
// re-embed. modelPath is a pure lookup, and SpaceRecipe covers the prompt
// regime.
func (a srvAIModelEmbedderAdapter) ModelID() string {
	name := srvEmbeddingModelFilename()
	if a.manager != nil {
		if base := filepath.Base(a.manager.modelPath(name)); base != "" && base != "." && base != string(filepath.Separator) {
			name = base
		}
	}
	return fmt.Sprintf("%s:%d:%s", name, a.Dim(), embedding.FamilySpaceRecipe(srvEmbedFamily()))
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

func (m *srvAIModelManager) ttsVoiceReady(voiceID string) bool {
	exists, size := fileStatus(filepath.Join(m.ttsVoiceDir(), voiceID+".koro"))
	return exists && size > 0
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

func (m *srvAIModelManager) downloadOmniParser(cfg corelib.AppConfig, done <-chan struct{}) error {
	path := m.modelPath(srvOmniParserFilename)
	if ready, _ := yolowFileReady(path); ready {
		return nil
	}
	// A partial or non-yolow file would otherwise sit forever: the shared
	// downloader treats any non-empty file as already installed.
	if exists, _ := modelFileReady(path); exists {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return m.downloadWithFallback(srvOmniParserFilename, srvOmniParserDefaultURL, cfg, done)
}

func (m *srvAIModelManager) ocrTierReady() bool {
	if m == nil {
		return false
	}
	tier := corelib.DefaultOCRModelTier
	_, detOK := ocr.ModelFileStatus(m.modelPath(ocr.DetModelFilename(tier)))
	_, recOK := ocr.ModelFileStatus(m.modelPath(ocr.RecModelFilename(tier)))
	return detOK && recOK
}

func (m *srvAIModelManager) ocrModelsReady() (bool, int64) {
	var total int64
	for _, name := range srvOCRModelFilenames() {
		size, ok := ocr.ModelFileStatus(m.modelPath(name))
		if !ok {
			return false, 0
		}
		total += size
	}
	return true, total
}

func (m *srvAIModelManager) downloadOCRModels(cfg corelib.AppConfig, done <-chan struct{}) error {
	if ready, _ := m.ocrModelsReady(); ready {
		return nil
	}
	zipPath := m.modelPath(ocr.ModelsZipFilename)
	if err := m.downloadWithFallback(ocr.ModelsZipFilename, ocr.DefaultModelsZipURL, cfg, done); err != nil {
		return err
	}
	extractErr := extractSrvOCRModelsZip(zipPath, filepath.Dir(zipPath))
	_ = os.Remove(zipPath)
	return extractErr
}

func srvOCRModelFilenames() []string {
	out := make([]string, 0, 6)
	for _, tier := range []string{"tiny", "small", "medium"} {
		out = append(out, ocr.DetModelFilename(tier), ocr.RecModelFilename(tier))
	}
	return out
}

// extractSrvOCRModelsZip installs the PP-OCRv6 det/rec ONNX files from the
// release zip. Non-ONNX entries are skipped. A path that escapes the models
// directory is rejected, and a failed extract removes the files it wrote.
func extractSrvOCRModelsZip(zipPath, dir string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open OCR models zip: %w", err)
	}
	defer reader.Close()

	extracted := make([]string, 0, len(reader.File))
	fail := func(err error) error {
		for _, path := range extracted {
			_ = os.Remove(path)
		}
		return err
	}
	for _, f := range reader.File {
		cleaned := filepath.Clean(f.Name)
		if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			return fail(fmt.Errorf("OCR models zip contains unsafe path %q", f.Name))
		}
		name := filepath.Base(cleaned)
		if !strings.HasSuffix(strings.ToLower(name), ".onnx") {
			continue
		}
		dst := filepath.Join(dir, name)
		if err := extractSrvZipEntry(f, dst); err != nil {
			return fail(err)
		}
		extracted = append(extracted, dst)
	}
	for _, name := range srvOCRModelFilenames() {
		if _, ok := ocr.ModelFileStatus(filepath.Join(dir, name)); !ok {
			return fail(fmt.Errorf("extracted OCR model %s failed validation", name))
		}
	}
	return nil
}

func extractSrvZipEntry(f *zip.File, dst string) error {
	src, err := f.Open()
	if err != nil {
		return fmt.Errorf("open zip entry %s: %w", f.Name, err)
	}
	defer src.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", f.Name, err)
	}
	_, copyErr := io.Copy(out, src)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("extract %s: %w", f.Name, copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("extract %s: %w", f.Name, closeErr)
	}
	if err := replaceInstalledFile(tmp, dst); err != nil {
		return fmt.Errorf("install %s: %w", f.Name, err)
	}
	return nil
}

// replaceInstalledFile renames tmp onto dst. Windows will not rename over a
// file that is already there, so a partial ONNX from a failed extract would
// otherwise stay in place and the next install would fail the same way.
func replaceInstalledFile(tmp, dst string) error {
	if err := os.Rename(tmp, dst); err == nil {
		return nil
	}
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func yolowFileReady(path string) (bool, int64) {
	f, err := os.Open(path)
	if err != nil {
		return false, 0
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() || info.Size() < 32 {
		return false, 0
	}
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return false, 0
	}
	if string(magic[:]) != "YOLW" {
		return false, 0
	}
	return true, info.Size()
}

func (m *srvAIModelManager) ocrProvider() *browser.NativeOCRProvider {
	if m == nil {
		return nil
	}
	tier := corelib.DefaultOCRModelTier
	detPath := m.modelPath(ocr.DetModelFilename(tier))
	recPath := m.modelPath(ocr.RecModelFilename(tier))
	m.ocrRunMu.Lock()
	defer m.ocrRunMu.Unlock()
	if m.ocrMgr == nil {
		m.ocrMgr = browser.NewNativeOCRProvider(detPath, recPath, func(message string) {
			log.Printf("[desktop-vision] %s", message)
		})
	}
	return m.ocrMgr
}

func (m *srvAIModelManager) omniParser() *guiautomation.YOLOScreenParser {
	if m == nil {
		return nil
	}
	m.yoloRunMu.Lock()
	defer m.yoloRunMu.Unlock()
	if m.yoloParser == nil {
		m.yoloParser = guiautomation.NewYOLOScreenParser(m.modelPath(srvOmniParserFilename), 0.3, 0.5)
	}
	return m.yoloParser
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
