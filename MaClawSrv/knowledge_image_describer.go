package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/ocr"
)

// srvKnowledgeImageDescriber resolves OCR and Vision at description time,
// rather than retaining one user's configuration in the process-wide knowledge
// store. ImageHints carry only the owning source scope and are never included
// in prompts, indexes, logs, or API responses.
type srvKnowledgeImageDescriber struct {
	loadConfig func(context.Context, knowledge.ImageHints) (corelib.AppConfig, error)
	persist    func(context.Context, knowledge.ImageHints, knowledge.VisionLLMConfig)
	ocrForTier func(string) knowledge.OCRProvider
	closeOCR   func()
	newVision  func(*knowledge.VisionLLMConfig, knowledge.ConfigPersister) *knowledge.VisionDescriber
}

func (d *srvKnowledgeImageDescriber) Describe(ctx context.Context, imagePath string, hints knowledge.ImageHints) (knowledge.ImageDescription, error) {
	if d == nil || d.loadConfig == nil {
		return knowledge.NewCompositeImageDescriber(nil, nil).Describe(ctx, imagePath, hints)
	}
	cfg, err := d.loadConfig(ctx, hints)
	if err != nil {
		// Missing/deleted users must not make an otherwise valid knowledge import
		// fail. Filename and surrounding-document context remain useful evidence.
		return knowledge.NewCompositeImageDescriber(nil, nil).Describe(ctx, imagePath, hints)
	}

	var vision *knowledge.VisionDescriber
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
	if visionCfg.Enabled && visionCfg.BaseURL != "" && visionCfg.APIKey != "" && visionCfg.Model != "" && visionCfg.Verified && d.newVision != nil {
		vision = d.newVision(&visionCfg, func(updated *knowledge.VisionLLMConfig) {
			if updated != nil && d.persist != nil {
				d.persist(context.Background(), hints, *updated)
			}
		})
	}

	var nativeOCR knowledge.OCRProvider
	if cfg.OCREnabled && d.ocrForTier != nil {
		nativeOCR = d.ocrForTier(corelib.NormalizeOCRModelTier(cfg.OCRModelTier))
	}
	composite := knowledge.NewCompositeImageDescriber(vision, nativeOCR)
	defer composite.Close()
	return composite.Describe(ctx, imagePath, hints)
}

// Close is intentionally delegated to the OCR pool. Vision clients are scoped
// to individual calls and have no persistent transport resources to release.
func (d *srvKnowledgeImageDescriber) Close() {
	if d == nil {
		return
	}
	if d.closeOCR != nil {
		d.closeOCR()
	}
}

type srvKnowledgeOCRPool struct {
	dataRoot string
	mu       sync.Mutex
	byTier   map[string]*browser.NativeOCRProvider
}

type srvKnowledgeOCRAdapter struct {
	runtime *browser.NativeOCRProvider
}

func (a srvKnowledgeOCRAdapter) Recognize(imageBase64 string) ([]knowledge.OCRResult, error) {
	if a.runtime == nil {
		return nil, fmt.Errorf("OCR provider unavailable")
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

func (a srvKnowledgeOCRAdapter) IsAvailable() bool {
	return a.runtime != nil && a.runtime.IsAvailable()
}
func (srvKnowledgeOCRAdapter) Close() {}

func newSrvKnowledgeOCRPool(dataRoot string) *srvKnowledgeOCRPool {
	return &srvKnowledgeOCRPool{dataRoot: dataRoot, byTier: make(map[string]*browser.NativeOCRProvider)}
}

func (p *srvKnowledgeOCRPool) provider(tier string) knowledge.OCRProvider {
	if p == nil {
		return nil
	}
	tier = corelib.NormalizeOCRModelTier(tier)
	p.mu.Lock()
	defer p.mu.Unlock()
	if provider := p.byTier[tier]; provider != nil {
		return srvKnowledgeOCRAdapter{runtime: provider}
	}
	detPath := filepath.Join(p.dataRoot, "models", ocr.DetModelFilename(tier))
	recPath := filepath.Join(p.dataRoot, "models", ocr.RecModelFilename(tier))
	provider := browser.NewNativeOCRProvider(detPath, recPath, func(message string) {
		log.Printf("[knowledge-ocr] %s", message)
	})
	p.byTier[tier] = provider
	return srvKnowledgeOCRAdapter{runtime: provider}
}

func (p *srvKnowledgeOCRPool) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	providers := p.byTier
	p.byTier = make(map[string]*browser.NativeOCRProvider)
	p.mu.Unlock()
	for _, provider := range providers {
		provider.Close()
	}
}

func newSrvKnowledgeImageDescriber(svc *agentservice.Service, dataRoot string) *srvKnowledgeImageDescriber {
	pool := newSrvKnowledgeOCRPool(dataRoot)
	describer := &srvKnowledgeImageDescriber{
		ocrForTier: pool.provider,
		closeOCR:   pool.Close,
		newVision:  knowledge.NewVisionDescriber,
	}
	describer.loadConfig = func(ctx context.Context, hints knowledge.ImageHints) (corelib.AppConfig, error) {
		if svc == nil || hints.TenantID == "" || hints.OwnerID == "" {
			return corelib.AppConfig{}, fmt.Errorf("knowledge image owner configuration is unavailable")
		}
		cfg, err := svc.GetRawUserConfig(ctx, agentservice.Principal{TenantID: hints.TenantID, UserID: hints.OwnerID})
		if err != nil || cfg == nil {
			return corelib.AppConfig{}, err
		}
		return cfg.AppConfig, nil
	}
	describer.persist = func(ctx context.Context, hints knowledge.ImageHints, failed knowledge.VisionLLMConfig) {
		if svc == nil || hints.TenantID == "" || hints.OwnerID == "" {
			return
		}
		principal := agentservice.Principal{TenantID: hints.TenantID, UserID: hints.OwnerID}
		current, err := svc.GetRawUserConfig(ctx, principal)
		if err != nil || current == nil {
			return
		}
		configured := current.AppConfig.KnowledgeVisionLLM
		// A failure must only invalidate the exact endpoint used for this image.
		// If the user replaced the endpoint while the request was in flight, their
		// new verified configuration must remain untouched.
		if !configured.Verified || configured.BaseURL != failed.BaseURL || configured.APIKey != failed.APIKey || configured.Model != failed.Model {
			return
		}
		current.AppConfig.KnowledgeVisionLLM.Verified = failed.Verified
		if _, err := svc.UpdateUserConfig(ctx, principal, current.AppConfig); err != nil {
			log.Printf("[knowledge-vision] persist verification state failed: %v", err)
		}
	}
	return describer
}

var _ knowledge.ImageDescriber = (*srvKnowledgeImageDescriber)(nil)
