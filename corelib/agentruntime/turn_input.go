package agentruntime

// TurnInput is the transport-neutral conversational input accepted by the
// shared Runtime. History and attachments intentionally use interface values:
// their canonical representations live in corelib/agent, while keeping this
// contract independent prevents an import cycle and lets hosts project their
// native DTOs without changing the Runtime API.
//
// HostPayload is never serialized or interpreted by the shared contract. A
// transport adapter may use it for local callback wiring during migration, but
// a real core Runtime must rely only on the fields above it.
type TurnInput struct {
	SystemPrompt  string `json:"system_prompt,omitempty"`
	UserText      string `json:"user_text"`
	History       any    `json:"history,omitempty"`
	Attachments   any    `json:"attachments,omitempty"`
	Platform      string `json:"platform,omitempty"`
	MinIterations int    `json:"min_iterations,omitempty"`
	// Callbacks are process-local presentation hooks and are intentionally
	// excluded from JSON. A headless Runtime may leave them nil and rely on
	// EventSink instead; GUI adapters can use them for legacy Wails/IM updates.
	Callbacks   *TurnCallbacks `json:"-"`
	HostPayload any            `json:"-"`
}

// DecodeTurnInput accepts the value and pointer forms used by in-process
// transports while keeping the Runtime boundary's decoding semantics in one
// place. Callers should treat a false result as an invalid request rather than
// silently falling back to a transport-specific DTO.
func DecodeTurnInput(value any) (TurnInput, bool) {
	switch input := value.(type) {
	case TurnInput:
		return input, true
	case *TurnInput:
		if input != nil {
			return *input, true
		}
	}
	return TurnInput{}, false
}

// TurnCallbacks is the minimal callback surface retained for compatibility
// while transports migrate to EventSink. It contains no GUI types, so a core
// Runtime can accept a TurnInput without importing guiapp.
type TurnCallbacks struct {
	OnProgress   func(string)
	OnToken      func(string)
	OnNewRound   func()
	OnStreamDone func()
}
