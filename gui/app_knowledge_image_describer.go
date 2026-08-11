package main

import (
	"fmt"

	corelib "github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// knowledgeImageOCRRuntime is the small portion of the shared native OCR
// runtime that knowledge-image ingestion needs. Keeping this adapter local
// prevents the knowledge package from depending on GUI/browser implementations.
type knowledgeImageOCRRuntime interface {
	Recognize(string) ([]browser.OCRResult, error)
	IsAvailable() bool
}

type knowledgeImageOCRAdapter struct {
	runtime knowledgeImageOCRRuntime
	enabled func() bool
}

func (a knowledgeImageOCRAdapter) Recognize(imageBase64 string) ([]knowledge.OCRResult, error) {
	if a.runtime == nil {
		return nil, fmt.Errorf("OCR runtime unavailable")
	}
	if a.enabled != nil && !a.enabled() {
		return nil, fmt.Errorf("OCR disabled in settings")
	}
	results, err := a.runtime.Recognize(imageBase64)
	if err != nil {
		return nil, err
	}
	out := make([]knowledge.OCRResult, 0, len(results))
	for _, result := range results {
		out = append(out, knowledge.OCRResult{
			Text:  result.Text,
			Score: result.Confidence,
			Box:   [][]int{{result.BBox[0], result.BBox[1], result.BBox[2], result.BBox[3]}},
		})
	}
	return out, nil
}

func (a knowledgeImageOCRAdapter) IsAvailable() bool {
	return a.runtime != nil && (a.enabled == nil || a.enabled()) && a.runtime.IsAvailable()
}

// Close is intentionally a no-op: the native OCR provider is process-shared
// with Computer Use and browser tooling, so an individual knowledge store must
// never tear it down.
func (knowledgeImageOCRAdapter) Close() {}

// configureKnowledgeImageDescriber wires imported knowledge images into the
// same local OCR runtime used elsewhere in the desktop app. Without this,
// image nodes only carry fallback filename/context text and OCR terms cannot be
// recalled by knowledge_image_search.
func (a *App) configureKnowledgeImageDescriber(store *knowledge.SQLiteStore) {
	a.configureKnowledgeImageDescriberWithOCR(store, sharedNativeOCRProvider())
}

func (a *App) configureKnowledgeImageDescriberWithOCR(store *knowledge.SQLiteStore, ocrRuntime knowledgeImageOCRRuntime) {
	if a == nil || store == nil {
		return
	}

	ocr := knowledgeImageOCRAdapter{runtime: ocrRuntime, enabled: ocrConfiguredEnabled}
	// The same process-wide PP-OCRv6 runtime also handles whole scanned PDF
	// pages selected by the pure-Go PDF inspector.
	store.SetPDFOCRProvider(ocr)
	store.SetImageDescriber(knowledge.NewCompositeImageDescriber(
		a.knowledgeVisionDescriber(),
		ocr,
	))
}

// knowledgeVisionDescriber returns the explicitly configured, verified vision
// endpoint. The active chat model is deliberately not implicitly reused here:
// it may use a non-OpenAI protocol, while the knowledge vision contract is an
// independently configured OpenAI-compatible endpoint with an explicit health
// check. OCR/context inference remains the safe fallback.
func (a *App) knowledgeVisionDescriber() *knowledge.VisionDescriber {
	cfg, err := a.LoadConfig()
	if err != nil || !cfg.KnowledgeVisionLLM.IsConfigured() {
		return nil
	}
	visionCfg := knowledge.VisionLLMConfig{
		Enabled:     cfg.KnowledgeVisionLLM.Enabled,
		BaseURL:     cfg.KnowledgeVisionLLM.BaseURL,
		APIKey:      cfg.KnowledgeVisionLLM.APIKey,
		Model:       cfg.KnowledgeVisionLLM.Model,
		MaxTokens:   cfg.KnowledgeVisionLLM.MaxTokens,
		TimeoutSec:  cfg.KnowledgeVisionLLM.TimeoutSec,
		Verified:    cfg.KnowledgeVisionLLM.Verified,
		FromMainLLM: cfg.KnowledgeVisionLLM.FromMainLLM,
	}
	return knowledge.NewVisionDescriber(&visionCfg, func(updated *knowledge.VisionLLMConfig) {
		if updated == nil {
			return
		}
		// A runtime failure only changes verification state. Preserve endpoint
		// settings and credentials, which may have changed concurrently.
		_ = a.PatchConfig(func(current *corelib.AppConfig) {
			current.KnowledgeVisionLLM.Verified = updated.Verified
		})
	})
}

// Compile-time guard: the adapter must continue satisfying the package-level
// OCR contract if either side of the shared runtime changes.
var _ knowledge.OCRProvider = knowledgeImageOCRAdapter{}
