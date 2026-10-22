package main

// Shared model service: one process per host loads every local AI model
// (embedding, ASR, TTS, OCR, screen YOLO) once and runs inference one call
// at a time. maclawsrv instances reach it over a unix socket when
// MACLAW_MODEL_SERVICE_SOCKET is set, so no maclawsrv loads its own copy.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	corelib "github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

const defaultModelServiceSocket = "/run/maclaw-models/models.sock"

type modelServiceEmbedReq struct {
	Texts []string `json:"texts"`
}
type modelServiceEmbedResp struct {
	Vectors [][]float32 `json:"vectors"`
}
type modelServiceASRReq struct {
	WAV []byte `json:"wav"`
}
type modelServiceTextResp struct {
	Text string `json:"text"`
}
type modelServiceTTSReq struct {
	Voice string `json:"voice"`
	Text  string `json:"text"`
}
type modelServiceTTSResp struct {
	WAV   []byte `json:"wav"`
	Voice string `json:"voice"`
}
type modelServiceImageReq struct {
	PNGBase64 string `json:"png_base64"`
}
type modelServiceOCRResp struct {
	Results []browser.OCRResult `json:"results"`
}
type modelServiceDetectResp struct {
	Elements []taskengine.UIElement `json:"elements"`
}
type modelServiceErr struct {
	Error string `json:"error"`
	Kind  string `json:"kind,omitempty"`
}

// modelRemote is the maclawsrv side of the shared model service.
type modelRemote struct {
	socket   string
	client   *http.Client
	fallback bool
}

func newModelRemoteFromEnv() *modelRemote {
	socket := strings.TrimSpace(os.Getenv("MACLAW_MODEL_SERVICE_SOCKET"))
	if socket == "" || socket == "off" {
		return nil
	}
	return newModelRemote(socket, envTruthy("MACLAW_MODEL_SERVICE_INPROC_FALLBACK"))
}

func newModelRemote(socket string, fallback bool) *modelRemote {
	timeout := 180 * time.Second
	if v, err := strconv.Atoi(os.Getenv("MACLAW_MODEL_SERVICE_TIMEOUT_SEC")); err == nil && v > 0 {
		timeout = time.Duration(v) * time.Second
	}
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}, MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second}
	return &modelRemote{socket: socket, client: &http.Client{Transport: tr, Timeout: timeout}, fallback: fallback}
}

func envTruthy(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

var errModelServiceUnavailable = errors.New("model service unavailable")

// call posts one request. A refused or missing socket is retried for up to
// about 10s (the service restarting); other failures return at once.
func (r *modelRemote) call(ctx context.Context, path string, in, out any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	var last error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://models"+path, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := r.client.Do(req)
		if err != nil {
			last = fmt.Errorf("%w: %v", errModelServiceUnavailable, err)
			if ctx.Err() != nil || !modelServiceRetryable(err) {
				return last
			}
			continue
		}
		data, rerr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if rerr != nil {
			return fmt.Errorf("%w: %v", errModelServiceUnavailable, rerr)
		}
		if resp.StatusCode != http.StatusOK {
			var e modelServiceErr
			_ = json.Unmarshal(data, &e)
			msg := strings.TrimSpace(e.Error)
			if msg == "" {
				msg = resp.Status
			}
			switch e.Kind {
			case "not_ready":
				return fmt.Errorf("%w: %s", errSrvAIModelNotReady, msg)
			case "invalid":
				return fmt.Errorf("%w: %s", errSrvAIModelInvalidInput, msg)
			}
			return fmt.Errorf("model service %s: %s", path, msg)
		}
		return json.Unmarshal(data, out)
	}
	return last
}

func modelServiceRetryable(err error) bool {
	s := err.Error()
	return strings.Contains(s, "connection refused") || strings.Contains(s, "no such file") || strings.Contains(s, "connection reset") || strings.Contains(s, "EOF")
}

// useLocal reports whether a failed remote call may run in this process.
func (r *modelRemote) useLocal(err error) bool {
	if r == nil || err == nil || !r.fallback {
		return false
	}
	if errors.Is(err, errSrvAIModelInvalidInput) || errors.Is(err, errSrvAIModelNotReady) {
		return false
	}
	log.Printf("[model-service] in-process fallback: %v", err)
	return true
}

func (r *modelRemote) embed(ctx context.Context, texts []string) ([][]float32, error) {
	var out modelServiceEmbedResp
	err := r.call(ctx, "/v1/embed", modelServiceEmbedReq{Texts: texts}, &out)
	return out.Vectors, err
}

func (r *modelRemote) asr(ctx context.Context, wav []byte) (string, error) {
	var out modelServiceTextResp
	err := r.call(ctx, "/v1/asr", modelServiceASRReq{WAV: wav}, &out)
	return out.Text, err
}

func (r *modelRemote) tts(ctx context.Context, voice, text string) ([]byte, string, error) {
	var out modelServiceTTSResp
	err := r.call(ctx, "/v1/tts", modelServiceTTSReq{Voice: voice, Text: text}, &out)
	return out.WAV, out.Voice, err
}

func (r *modelRemote) ocr(ctx context.Context, b64 string) ([]browser.OCRResult, error) {
	var out modelServiceOCRResp
	err := r.call(ctx, "/v1/ocr", modelServiceImageReq{PNGBase64: b64}, &out)
	return out.Results, err
}

func (r *modelRemote) detect(ctx context.Context, b64 string) ([]taskengine.UIElement, error) {
	var out modelServiceDetectResp
	err := r.call(ctx, "/v1/detect", modelServiceImageReq{PNGBase64: b64}, &out)
	return out.Elements, err
}

// recognizeOCR runs OCR through the shared service or in this process.
func (m *srvAIModelManager) recognizeOCR(ctx context.Context, b64 string) ([]browser.OCRResult, error) {
	if m.remote != nil {
		res, err := m.remote.ocr(ctx, b64)
		if !m.remote.useLocal(err) {
			return res, err
		}
	}
	provider := m.ocrProvider()
	if provider == nil || !provider.IsAvailable() {
		return nil, errSrvAIModelNotReady
	}
	return provider.Recognize(b64)
}

// detectUI runs the screen YOLO through the shared service or in this process.
func (m *srvAIModelManager) detectUI(ctx context.Context, b64 string) ([]taskengine.UIElement, error) {
	if m.remote != nil {
		res, err := m.remote.detect(ctx, b64)
		if !m.remote.useLocal(err) {
			return res, err
		}
	}
	parser := m.omniParser()
	if parser == nil || !parser.IsAvailable() {
		return nil, errSrvAIModelNotReady
	}
	release, ok := desktopAcquireYOLO(ctx)
	if !ok {
		return nil, context.Canceled
	}
	defer release()
	return parser.Parse(b64)
}

// ── service side ──

// modelServeMain runs "maclawsrv model-serve".
func modelServeMain() error {
	socket := strings.TrimSpace(os.Getenv("MACLAW_MODEL_SERVICE_SOCKET"))
	if socket == "" || socket == "off" {
		socket = defaultModelServiceSocket
	}
	m := newSrvAIModelManager(defaultDataRoot())
	m.remote = nil
	if err := os.MkdirAll(filepath.Dir(socket), 0o750); err != nil {
		return err
	}
	_ = os.Remove(socket)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	_ = os.Chmod(socket, 0o660)
	log.Printf("[model-service] listening on %s data_root=%s", socket, m.dataRoot)
	res := newModelResidencyFromEnv(m)
	log.Printf("[model-service] idle_unload=%s one_model=%v", res.idle, res.oneModel)
	var lock sync.Mutex
	go res.run(&lock)
	return http.Serve(ln, newModelServiceHandlerWith(m, res, &lock))
}

func newModelServiceHandler(m *srvAIModelManager) http.Handler {
	var mu sync.Mutex
	return newModelServiceHandlerWith(m, nil, &mu)
}

// newModelServiceHandlerWith serves the model APIs; mu serializes every
// inference across all models, res (optional) manages residency.
func newModelServiceHandlerWith(m *srvAIModelManager, res *modelResidency, mu *sync.Mutex) http.Handler {
	mux := http.NewServeMux()
	models := map[string]string{"/v1/embed": srvAIModelEmbedding, "/v1/asr": srvAIModelASR, "/v1/tts": srvAIModelTTS, "/v1/ocr": srvAIModelOCR, "/v1/detect": srvAIModelOmniParser}
	handle := func(path string, fn func(r *http.Request) (any, error)) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "POST only", http.StatusMethodNotAllowed)
				return
			}
			mu.Lock()
			if res != nil {
				res.before(models[path])
			}
			out, err := fn(r)
			if res != nil {
				res.after(models[path])
			}
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			if err != nil {
				kind := ""
				switch {
				case errors.Is(err, errSrvAIModelNotReady):
					kind = "not_ready"
				case errors.Is(err, errSrvAIModelInvalidInput):
					kind = "invalid"
				}
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = json.NewEncoder(w).Encode(modelServiceErr{Error: err.Error(), Kind: kind})
				return
			}
			_ = json.NewEncoder(w).Encode(out)
		})
	}
	decode := func(r *http.Request, v any) error {
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<20)).Decode(v); err != nil {
			return fmt.Errorf("%w: %v", errSrvAIModelInvalidInput, err)
		}
		return nil
	}
	handle("/v1/embed", func(r *http.Request) (any, error) {
		var in modelServiceEmbedReq
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		v, err := m.embedBatch(r.Context(), corelib.AppConfig{}, in.Texts)
		return modelServiceEmbedResp{Vectors: v}, err
	})
	handle("/v1/asr", func(r *http.Request) (any, error) {
		var in modelServiceASRReq
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		t, err := m.transcribeWAV(r.Context(), corelib.AppConfig{}, in.WAV)
		return modelServiceTextResp{Text: t}, err
	})
	handle("/v1/tts", func(r *http.Request) (any, error) {
		var in modelServiceTTSReq
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		cfg := corelib.AppConfig{TTSVoiceID: in.Voice}
		wav, voice, err := m.synthesizeTextWithVoice(r.Context(), cfg, in.Voice, in.Text)
		return modelServiceTTSResp{WAV: wav, Voice: voice}, err
	})
	handle("/v1/ocr", func(r *http.Request) (any, error) {
		var in modelServiceImageReq
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		res, err := m.recognizeOCR(r.Context(), in.PNGBase64)
		releaseModelScratch()
		return modelServiceOCRResp{Results: res}, err
	})
	handle("/v1/detect", func(r *http.Request) (any, error) {
		var in modelServiceImageReq
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		res, err := m.detectUI(r.Context(), in.PNGBase64)
		return modelServiceDetectResp{Elements: res}, err
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}
