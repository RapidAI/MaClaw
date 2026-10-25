package corelib

import "time"

// Generation connection probes may sit without response headers until the
// model emits its first token. These budgets are shared by every provider
// test, including both Qoder editions. Login, refresh, and model-catalog
// calls keep their own short clients.
const (
	LLMProbeHeaderTimeout = 2 * time.Minute
	LLMProbeTotalTimeout  = 3 * time.Minute
	LLMProbeDialTimeout   = 10 * time.Second
	// A connection probe only needs a short reply. Leaving the provider's
	// normal output cap (often 65536) makes a non-streaming gateway buffer
	// the whole completion before it sends headers. 256 still covers the
	// minimal reasoning trace xAI emits when thinking is off; 32 can be
	// spent entirely on that trace and come back with an empty answer.
	LLMProbeMaxOutputTokens = 256
	// Models that cannot skip the thinking trace spend a 256-token cap inside
	// that trace. ProbeNeedsReasoningRoom is that set: always-on GLM, xAI,
	// Kimi K3 / k2.7-code, and MiniMax checkpoints other than M3. The
	// connectivity check needs room for a short low-effort thought and the answer.
	LLMProbeAlwaysOnMaxOutputTokens = 2048
)
