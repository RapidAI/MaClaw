package agentservice

import (
	"context"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// bindRequestHostCapabilities lets the request-level Host overlay executor-level
// desktop ports. An explicit headless host clears GUI-only ports so srv cannot
// inherit a leftover desktop adapter from the process.
func (c *coreAgentCallbacks) bindRequestHostCapabilities() {
	if c == nil {
		return
	}
	host := c.host
	if host == nil {
		host = agentruntime.HeadlessHostCapabilities{}
		c.host = host
	}
	if port, ok := host.DesktopCapture(); ok && port != nil {
		c.desktopCapturer = runtimeDesktopCapturer{port: port}
	} else {
		c.desktopCapturer = nil
	}
	if port, ok := host.DocumentLauncher(); ok && port != nil {
		c.documentLauncher = runtimeDocumentLauncher{port: port}
	} else {
		c.documentLauncher = nil
	}
	if port, ok := host.URLLauncher(); ok && port != nil {
		c.urlLauncher = runtimeURLLauncher{port: port}
	} else {
		c.urlLauncher = nil
	}
	if port, ok := host.SpeechRenderer(); ok && port != nil {
		c.speechPlayer = runtimeSpeechPlayer{port: port}
	} else {
		c.speechPlayer = nil
	}
}

type runtimeDesktopCapturer struct {
	port agentruntime.DesktopCapturePort
}

func (c runtimeDesktopCapturer) CapturePrimary(ctx context.Context) ([]byte, error) {
	return c.CaptureDisplay(ctx, 0)
}

func (c runtimeDesktopCapturer) CaptureDisplay(ctx context.Context, display int) ([]byte, error) {
	data, _, err := c.port.Capture(ctx, agentruntime.DesktopCaptureRequest{Display: display})
	return data, err
}

type runtimeDocumentLauncher struct {
	port agentruntime.DocumentLauncherPort
}

func (l runtimeDocumentLauncher) OpenDocument(ctx context.Context, absPath string) error {
	return l.port.OpenDocument(ctx, absPath)
}

type runtimeURLLauncher struct {
	port agentruntime.URLLauncherPort
}

func (l runtimeURLLauncher) OpenURL(ctx context.Context, rawURL string) error {
	return l.port.OpenURL(ctx, rawURL)
}

type runtimeSpeechPlayer struct {
	port agentruntime.SpeechRendererPort
}

func (p runtimeSpeechPlayer) PlaySpeech(ctx context.Context, wav []byte) error {
	return p.port.RenderSpeech(ctx, wav, "audio/wav")
}

func capabilityUnavailableToolResult(capability string) agent.ToolExecutionResult {
	return agent.ToolExecutionResult{
		Result:  agentruntime.CapabilityUnavailableResult(capability),
		Outcome: agent.ToolExecutionOutcomeError,
	}
}
