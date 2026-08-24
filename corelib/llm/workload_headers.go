package llm

import (
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

// WorkloadHintHeaderValues returns the L1 hint headers that may leave this
// client. Direct third-party URLs yield an empty map even if leftover fields
// remain on the config snapshot.
func WorkloadHintHeaderValues(cfg corelib.MaclawLLMConfig) map[string]string {
	if !cfg.ShouldSendWorkloadHints() {
		return nil
	}
	out := make(map[string]string, 4)
	setHint := func(key, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		out[key] = value
	}
	setHint(llmpool.WorkloadClassHeader, cfg.WorkloadClassHint)
	setHint(llmpool.WorkflowTypeHeader, cfg.WorkflowTypeHint)
	setHint(llmpool.PhaseKindHeader, cfg.PhaseKindHint)
	setHint(llmpool.TaskTypeHeader, cfg.TaskTypeHint)
	if len(out) == 0 {
		return nil
	}
	return out
}

// ApplyWorkloadHintHeaderMap writes V1.1 desktop/workflow L1 hints onto headers.
func ApplyWorkloadHintHeaderMap(header http.Header, cfg corelib.MaclawLLMConfig) {
	if header == nil {
		return
	}
	for key, value := range WorkloadHintHeaderValues(cfg) {
		header.Set(key, value)
	}
}

// ApplyWorkloadHintHeaders attaches V1.1 desktop/workflow L1 hints when the
// request already targets a Hub or HubCenter endpoint.
func ApplyWorkloadHintHeaders(req *http.Request, cfg corelib.MaclawLLMConfig) {
	if req == nil {
		return
	}
	ApplyWorkloadHintHeaderMap(req.Header, cfg)
}
