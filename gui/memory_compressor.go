package main

import (
	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// GUI memory compression is a thin adapter over corelib/memory.Compressor.
// Durable memory maintenance, dedup, backups, status, and background cycles are
// owned by corelib/memory so GUI/TUI/server do not grow separate implementations.
type MemoryCompressor = memory.Compressor

type MemoryCompressorStatus = memory.CompressorStatus

type MemoryBackupInfo = memory.BackupInfo

func NewMemoryCompressor(store *memory.Store, _ corelib.MaclawLLMConfig, app *App) *MemoryCompressor {
	if app != nil {
		return app.newMemoryCompressor(store)
	}
	return memory.NewMaintenance(store, nil, corelib.NoopEmitter{}).Compressor()
}

func (a *App) newMemoryCompressor(store *memory.Store) *MemoryCompressor {
	if a == nil {
		return memory.NewMaintenance(store, nil, corelib.NoopEmitter{}).Compressor()
	}
	if a.memoryMaintenance == nil {
		maintenance := memory.NewMaintenance(store, &archiverLLMCaller{app: a}, guiMemoryEventEmitter{app: a})
		maintenance.InstallRuntime()
		a.memoryMaintenance = maintenance
		if a.currentMemoryPipeline() == nil {
			a.setMemoryPipeline(maintenance.Pipeline())
		}
		maintenance.Start()
	}
	compressor := a.memoryMaintenance.Compressor()
	a.configureMemoryCompressor(compressor)
	return compressor
}

func (a *App) configureMemoryCompressor(compressor *MemoryCompressor) {
	if a == nil || compressor == nil {
		return
	}
	if appCfg, err := a.LoadConfig(); err == nil && appCfg.MemoryMaxBackups > 0 {
		compressor.SetMaxBackups(appCfg.MemoryMaxBackups)
	}
}

type guiMemoryEventEmitter struct {
	app *App
}

func (e guiMemoryEventEmitter) Emit(eventType string, payload interface{}) {
	if e.app == nil {
		return
	}
	// Background maintenance mutates the same store outside the Wails binding
	// calls. Invalidate before publishing the completion event so an Agent's
	// next turn cannot observe a stale frozen memory snapshot.
	switch eventType {
	case "memory:compressed", "memory:gc", "memory:dream", "memory:pipeline_completed":
		e.app.refreshActiveAgentMemorySnapshots()
	}
	e.app.emitEvent(eventType, payload)
}

func (e guiMemoryEventEmitter) Subscribe(string, corelib.EventHandler) {}
