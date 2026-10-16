package guiapp

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

type guiHostCapabilities struct{}

func (guiHostCapabilities) Profile() agentruntime.CapabilityProfile {
	return agentruntime.CapabilityProfile{
		Name:     "gui",
		Headless: false,
		Capabilities: map[string]bool{
			"desktop_capture":   true,
			"document_launcher": true,
			"url_launcher":      true,
			"sessions":          true,
			"ask_user":          true,
			"local_bash":        true,
			"ssh":               true,
			"ssh_file_transfer": true,
		},
	}
}

func (guiHostCapabilities) DesktopCapture() (agentruntime.DesktopCapturePort, bool) {
	return guiDesktopCapturePort{}, true
}

func (guiHostCapabilities) DocumentLauncher() (agentruntime.DocumentLauncherPort, bool) {
	return guiDocumentLauncherPort{}, true
}

func (guiHostCapabilities) URLLauncher() (agentruntime.URLLauncherPort, bool) {
	return guiURLLauncherPort{}, true
}

func (guiHostCapabilities) SpeechRenderer() (agentruntime.SpeechRendererPort, bool) {
	return nil, false
}

type guiDesktopCapturePort struct{}

func (guiDesktopCapturePort) Capture(_ context.Context, req agentruntime.DesktopCaptureRequest) ([]byte, string, error) {
	encoded, err := captureDesktopScreenshot(req.Display)
	if err != nil {
		return nil, "", err
	}
	encoded = strings.TrimSpace(encoded)
	if idx := strings.Index(encoded, ","); idx >= 0 && strings.Contains(encoded[:idx], "base64") {
		encoded = encoded[idx+1:]
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 {
		return nil, "", fmt.Errorf("desktop capture returned no image")
	}
	return data, "image/png", nil
}

type guiDocumentLauncherPort struct{}

func (guiDocumentLauncherPort) OpenDocument(_ context.Context, absPath string) error {
	absPath = strings.TrimSpace(absPath)
	if absPath == "" {
		return fmt.Errorf("missing file path")
	}
	return startSystemOpen(absPath)
}

type guiURLLauncherPort struct{}

func (guiURLLauncherPort) OpenURL(_ context.Context, rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return fmt.Errorf("missing url")
	}
	return startSystemOpen(rawURL)
}
