package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// desktopVisionCacheKey is the provider URL and model this turn's screenshot
// decision was made for. A later image refusal is recorded against that key.
type desktopVisionCacheKey struct{}

type visionMemo struct {
	Sees       bool   `json:"sees"`
	At         string `json:"at"`
	Downgraded bool   `json:"downgraded,omitempty"`
}

var (
	visionMemoMu sync.Mutex
	visionMemos  = map[string]visionMemo{}
	// desktopProbeVision is the live image probe. Tests replace it.
	desktopProbeVision = func(cfg corelib.MaclawLLMConfig) (supported, known bool) {
		return llm.ProbeVision(context.Background(), cfg)
	}
)

func init() {
	agent.VisionSeesImages = desktopCachedVision
	agent.NoteVisionRejected = desktopNoteVisionRejected
	agent.VisionReading = desktopVisionReading
	loadVisionMemos()
}

func desktopVisionKey(request agentruntime.TurnRequest) string {
	cfg, ok := turnAppConfig(request)
	if !ok {
		return ""
	}
	llmCfg, err := agentservice.ResolveTurnLLMRole(cfg, desktopMessageMeta(request), nil)
	if err != nil {
		return ""
	}
	return visionMemoKey(llmCfg.URL, llmCfg.Model)
}

func visionMemoKey(rawURL, model string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(rawURL), "/")) + "\n" + strings.ToLower(strings.TrimSpace(model))
}

// desktopModelSeesImages is the screenshot switch for the model this turn
// actually calls. An admin on/off wins. A recorded probe wins. Unknown is
// probed once and cached; it is not treated as "no images".
func desktopModelSeesImages(app corelib.AppConfig, llmCfg corelib.MaclawLLMConfig) bool {
	provider, ok := findProviderForLLM(app, llmCfg)
	if !ok {
		return llmCfg.SupportsVision
	}
	key := visionMemoKey(llmCfg.URL, llmCfg.Model)
	sees, known, shouldProbe := corelib.ProviderVisionDecision(provider, llmCfg.Model)
	if memo, hit := visionMemoOf(key); hit && memo.Downgraded && corelib.ModelVisionOverride(provider, llmCfg.Model) != "on" {
		if !known || memoAfter(memo, modelVisionDetectedAt(provider, llmCfg.Model)) {
			return false
		}
	}
	if known {
		return sees
	}
	if !shouldProbe {
		return false
	}
	if memo, hit := visionMemoOf(key); hit && !memo.Downgraded {
		return memo.Sees
	}
	supported, probed := desktopProbeVision(llmCfg)
	if !probed {
		log.Printf("[desktop-vision] probe inconclusive provider=%s model=%s; this shot uses OCR and the result is not cached", llmCfg.ProviderName, llmCfg.Model)
		return false
	}
	saveVisionMemo(key, visionMemo{Sees: supported, At: time.Now().UTC().Format(time.RFC3339)})
	log.Printf("[desktop-vision] probed provider=%s model=%s supports_vision=%v", llmCfg.ProviderName, llmCfg.Model, supported)
	return supported
}

func modelVisionDetectedAt(provider corelib.MaclawLLMProvider, model string) string {
	for _, item := range provider.VisionByModel {
		if strings.EqualFold(strings.TrimSpace(item.Model), strings.TrimSpace(model)) {
			return item.DetectedAt
		}
	}
	return ""
}

func memoAfter(memo visionMemo, detectedAt string) bool {
	detected := strings.TrimSpace(detectedAt)
	if detected == "" {
		return true
	}
	return memo.At > detected
}

func findProviderForLLM(app corelib.AppConfig, llmCfg corelib.MaclawLLMConfig) (corelib.MaclawLLMProvider, bool) {
	for _, provider := range app.MaclawLLMProviders {
		if llmCfg.ProviderID != "" && (provider.ID == llmCfg.ProviderID || corelib.MaclawLLMLegacyProviderID(provider.Name) == llmCfg.ProviderID) {
			return provider, true
		}
		if corelib.MaclawLLMProviderNameEqual(provider.Name, llmCfg.ProviderName) {
			return provider, true
		}
	}
	return corelib.MaclawLLMProvider{}, false
}

func desktopCachedVision(cfg corelib.MaclawLLMConfig) (bool, bool) {
	memo, ok := visionMemoOf(visionMemoKey(cfg.URL, cfg.Model))
	if !ok {
		return false, false
	}
	return memo.Sees, true
}

func visionDowngraded(key string) bool {
	memo, ok := visionMemoOf(key)
	return ok && memo.Downgraded && !memo.Sees
}

func desktopNoteVisionRejected(cfg corelib.MaclawLLMConfig) {
	key := visionMemoKey(cfg.URL, cfg.Model)
	saveVisionMemo(key, visionMemo{Sees: false, Downgraded: true, At: time.Now().UTC().Format(time.RFC3339)})
	log.Printf("[desktop-vision] downgraded provider=%s model=%s; this turn continues with OCR", cfg.ProviderName, cfg.Model)
}

func desktopVisionReading(png []byte) string {
	if len(png) == 0 || desktopScreenVision == nil {
		return ""
	}
	log.Printf("[desktop-screen] local OCR/OmniParser after image rejection bytes=%d", len(png))
	return desktopScreenVision(context.Background(), png, true)
}

func visionMemoOf(key string) (visionMemo, bool) {
	if key == "" || key == "\n" {
		return visionMemo{}, false
	}
	visionMemoMu.Lock()
	defer visionMemoMu.Unlock()
	memo, ok := visionMemos[key]
	return memo, ok
}

func saveVisionMemo(key string, memo visionMemo) {
	if key == "" || key == "\n" {
		return
	}
	visionMemoMu.Lock()
	visionMemos[key] = memo
	snapshot := make(map[string]visionMemo, len(visionMemos))
	for k, v := range visionMemos {
		snapshot[k] = v
	}
	visionMemoMu.Unlock()
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return
	}
	path := filepath.Join(corelib.MaclawDataDir(), "vision-capability.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, raw, 0o644)
}

func loadVisionMemos() {
	raw, err := os.ReadFile(filepath.Join(corelib.MaclawDataDir(), "vision-capability.json"))
	if err != nil || len(raw) == 0 {
		return
	}
	var saved map[string]visionMemo
	if json.Unmarshal(raw, &saved) != nil {
		return
	}
	visionMemoMu.Lock()
	for key, memo := range saved {
		visionMemos[key] = memo
	}
	visionMemoMu.Unlock()
}
