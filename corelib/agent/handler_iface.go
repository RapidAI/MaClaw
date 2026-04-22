package agent

// Handler is the interface for the unified agent message handler.
// Both GUI and TUI construct an implementation of this interface.
//
// GUI: gui.IMMessageHandler implements this directly.
// TUI: calls gui.NewIMMessageHandlerStandalone which returns an implementation.
//
// The interface lives in corelib/agent so both gui/ and tui/ can reference it
// without cross-importing package main.
type Handler interface {
	// HandleMessage processes a user message and returns the agent's response.
	// This is the primary entry point for all platforms.
	HandleMessage(msg UserMessage) *Response

	// HandleMessageWithProgress adds progress callbacks for long-running tasks.
	HandleMessageWithProgress(msg UserMessage, onProgress ProgressCallback) *Response

	// HandleMessageWithStream adds streaming support (token-by-token output).
	HandleMessageWithStream(
		msg UserMessage,
		onProgress ProgressCallback,
		onToken TokenCallback,
		onNewRound NewRoundCallback,
		onStreamDone StreamDoneCallback,
	) *Response

	// Stop gracefully shuts down the handler (closes conversation memory, etc.).
	Stop()
}

// HandlerFactory creates a Handler from a Config. This is the bridge between
// the corelib interface and the gui implementation.
//
// GUI registers its factory at init time. TUI calls it to get a Handler
// without importing gui/ directly.
//
// Usage:
//
//	// In gui/ init:
//	agent.RegisterHandlerFactory(func(cfg agent.Config) agent.Handler {
//	    return NewIMMessageHandlerStandalone(convertConfig(cfg))
//	})
//
//	// In tui/:
//	handler := agent.NewHandler(agent.Config{...})
type HandlerFactory func(cfg Config) Handler

var globalFactory HandlerFactory

// RegisterHandlerFactory sets the global handler factory.
// Called by gui/ at init time.
func RegisterHandlerFactory(f HandlerFactory) {
	globalFactory = f
}

// NewHandler creates a Handler using the registered factory.
// Panics if no factory has been registered.
func NewHandler(cfg Config) Handler {
	if globalFactory == nil {
		panic("agent.NewHandler: no HandlerFactory registered — ensure gui/ is linked")
	}
	return globalFactory(cfg)
}

// HasHandlerFactory returns true if a factory has been registered.
func HasHandlerFactory() bool {
	return globalFactory != nil
}
