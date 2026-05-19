package memory

import (
	"context"
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib"
)

// Maintenance centralizes long-term memory maintenance operations that host
// adapters expose through GUI/TUI/server surfaces. Hosts should use this facade
// instead of constructing compressors, pipelines, synthesizers, consolidators,
// recall gates, or online extractors directly so maintenance topology stays
// owned by corelib/memory.
type Maintenance struct {
	store              *Store
	llm                LLMChatCaller
	compressor         *Compressor
	pipeline           *Pipeline
	onlineExtractor    *OnlineExtractor
	knowledgeExtractor *KnowledgeExtractor
}

// NewMaintenance creates a shared long-term memory maintenance facade.
func NewMaintenance(store *Store, llm LLMChatCaller, emitter corelib.EventEmitter) *Maintenance {
	compressor := NewCompressor(store, llm, emitter)
	maintenance := &Maintenance{
		store:           store,
		llm:             llm,
		compressor:      compressor,
		onlineExtractor: NewOnlineExtractor(store, llm),
	}
	if store != nil {
		consolidator := NewConsolidator(store, store.TMT(), llm)
		pipeline := NewPipeline(store, compressor, nil, nil, emitter)
		pipeline.SetSynthesizer(NewSynthesizer(store, llm))
		pipeline.SetConsolidator(consolidator, NewProfileConsolidator(store, store.TMT(), llm))
		knowledgeExtractor := NewKnowledgeExtractor(store, llm)
		knowledgeExtractor.SetConsolidator(consolidator)
		maintenance.pipeline = pipeline
		maintenance.knowledgeExtractor = knowledgeExtractor
	}
	return maintenance
}

// InstallRuntime wires store-level maintenance components that should be shared
// by all host integrations: online extraction and recall gating.
func (m *Maintenance) InstallRuntime() {
	if m == nil || m.store == nil {
		return
	}
	m.store.SetOnlineExtractor(m.onlineExtractor)
	m.store.SetRecallGating(NewRecallGating(m.llm))
}

// SetLLM rewires all LLM-backed maintenance components together.
func (m *Maintenance) SetLLM(llm LLMChatCaller) {
	if m == nil {
		return
	}
	m.llm = llm
	if m.store != nil {
		m.store.SetLLMDedup(llm)
		m.store.SetRecallGating(NewRecallGating(llm))
	}
	if m.compressor != nil {
		m.compressor.SetLLM(llm)
	}
	if m.pipeline != nil {
		m.pipeline.SetLLM(llm)
	}
	if m.onlineExtractor != nil {
		m.onlineExtractor.SetLLM(llm)
	}
	if m.knowledgeExtractor != nil {
		m.knowledgeExtractor.SetLLM(llm)
	}
}

// Start begins background maintenance.
func (m *Maintenance) Start() {
	if m == nil || m.pipeline == nil {
		return
	}
	m.pipeline.Start()
}

// Stop halts background maintenance loops owned by this facade.
func (m *Maintenance) Stop() {
	if m == nil {
		return
	}
	if m.pipeline != nil {
		m.pipeline.Stop()
	}
	if m.compressor != nil {
		m.compressor.Stop()
	}
}

// Pipeline returns the shared maintenance pipeline.
func (m *Maintenance) Pipeline() *Pipeline {
	if m == nil {
		return nil
	}
	return m.pipeline
}

// OnlineExtractor returns the shared online extraction component.
func (m *Maintenance) OnlineExtractor() *OnlineExtractor {
	if m == nil {
		return nil
	}
	return m.onlineExtractor
}

// KnowledgeExtractor returns the fallback extractor wired to the same corelib
// consolidation topology as the maintenance pipeline.
func (m *Maintenance) KnowledgeExtractor() *KnowledgeExtractor {
	if m == nil {
		return nil
	}
	return m.knowledgeExtractor
}

// Compressor returns the underlying compressor for host adapters that need to
// expose backup/status controls while keeping policy in corelib/memory.
func (m *Maintenance) Compressor() *Compressor {
	if m == nil {
		return nil
	}
	return m.compressor
}

func (m *Maintenance) compressorOrError() (*Compressor, error) {
	if m == nil || m.compressor == nil || m.store == nil {
		return nil, fmt.Errorf("memory maintenance compressor is unavailable")
	}
	return m.compressor, nil
}

// Compress runs long-term memory dedup/compression maintenance.
func (m *Maintenance) Compress(ctx context.Context) (*CompressResult, error) {
	compressor, err := m.compressorOrError()
	if err != nil {
		return nil, err
	}
	return compressor.Compress(ctx)
}

// StartCompressor begins the compressor's legacy auto-compression loop.
func (m *Maintenance) StartCompressor() error {
	compressor, err := m.compressorOrError()
	if err != nil {
		return err
	}
	compressor.Start()
	return nil
}

// StopCompressor halts the compressor's legacy auto-compression loop.
func (m *Maintenance) StopCompressor() error {
	compressor, err := m.compressorOrError()
	if err != nil {
		return err
	}
	compressor.Stop()
	return nil
}

// SetMaxBackups updates the backup retention limit for the shared compressor.
func (m *Maintenance) SetMaxBackups(n int) error {
	compressor, err := m.compressorOrError()
	if err != nil {
		return err
	}
	compressor.SetMaxBackups(n)
	return nil
}

// CompressorStatus returns the current compressor status.
func (m *Maintenance) CompressorStatus() (CompressorStatus, error) {
	compressor, err := m.compressorOrError()
	if err != nil {
		return CompressorStatus{}, err
	}
	return compressor.Status(), nil
}

// IsCompressing reports whether a compression pass is currently in flight.
func (m *Maintenance) IsCompressing() (bool, error) {
	compressor, err := m.compressorOrError()
	if err != nil {
		return false, err
	}
	return compressor.IsCompressing(), nil
}

// ListBackups returns available long-term memory backup snapshots.
func (m *Maintenance) ListBackups() ([]BackupInfo, error) {
	compressor, err := m.compressorOrError()
	if err != nil {
		return nil, err
	}
	return compressor.ListBackups()
}

// RestoreBackup restores a long-term memory backup snapshot by name.
func (m *Maintenance) RestoreBackup(name string) error {
	compressor, err := m.compressorOrError()
	if err != nil {
		return err
	}
	return compressor.RestoreBackup(name)
}

// DeleteBackup removes a long-term memory backup snapshot by name.
func (m *Maintenance) DeleteBackup(name string) error {
	compressor, err := m.compressorOrError()
	if err != nil {
		return err
	}
	return compressor.DeleteBackup(name)
}
