package main

import (
	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// guiOfficeReadConfig snapshots the GUI's document-reading policy for a
// single operation. A value copy prevents a later settings update from
// changing an extraction already in progress.
func guiOfficeReadConfig(cfg corelib.AppConfig) agent.OfficeReadConfig {
	return agent.CloneOfficeReadConfig(agent.OfficeReadConfig{
		Engine:       cfg.OfficeReadEngine,
		Formats:      cfg.OfficeReadFormats,
		Fallback:     cfg.OfficeReadFallback,
		EmitMarkdown: cfg.OfficeReadEmitMarkdown,
	})
}
