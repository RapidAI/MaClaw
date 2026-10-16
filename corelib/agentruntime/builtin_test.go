package agentruntime

import (
	"context"
	"strings"
	"testing"
)

func TestRegisterBuiltinModulesIncludesCompiledHostTools(t *testing.T) {
	registry, err := RegisterBuiltinModules()
	if err != nil {
		t.Fatal(err)
	}
	headless := HeadlessHostCapabilities{Capabilities: map[string]bool{"http": true}}
	snapshot := registry.SnapshotForHost(headless)
	ids := map[string]bool{}
	for _, descriptor := range snapshot {
		ids[descriptor.ModuleID] = true
	}
	if !ids["prompt.default_role"] {
		t.Fatalf("missing default role module: %#v", snapshot)
	}
	desktop := staticHostCapabilities{headless: false, desktop: true}
	tools, executable, err := registry.ToolsWithExecutability(context.Background(), TurnRequest{Host: desktop})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range tools {
		if tool.Name == "screenshot" {
			found = true
			if !executable["screenshot"] {
				t.Fatal("desktop host must execute screenshot")
			}
		}
	}
	if !found {
		t.Fatal("screenshot missing from desktop builtin surface")
	}
	_, handled, invErr := registry.InvokeTool(context.Background(), TurnRequest{Host: headless}, "screenshot", nil)
	if !handled || !IsCapabilityUnavailable(invErr, "") {
		t.Fatalf("headless screenshot: handled=%v err=%v", handled, invErr)
	}
}

func TestResolveRoleUsesSharedDefaults(t *testing.T) {
	name, description := ResolveRole("", "")
	if name != DefaultRoleName || description != DefaultRoleDescription {
		t.Fatalf("role = %q %q", name, description)
	}
	name, description = ResolveRole("Ops", "custom")
	if name != "Ops" || description != "custom" {
		t.Fatalf("custom role lost: %q %q", name, description)
	}
}

func TestRegisterBuiltinModulesCachesTheDefaultRegistry(t *testing.T) {
	first, err := RegisterBuiltinModules()
	if err != nil {
		t.Fatal(err)
	}
	second, err := RegisterBuiltinModules()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("default builtin registry should be reused")
	}
}

func TestOpenModuleRoutesHTTPToURLLauncher(t *testing.T) {
	registry, err := RegisterBuiltinModules()
	if err != nil {
		t.Fatal(err)
	}
	host := &recordingOpenHost{}
	result, handled, invErr := registry.InvokeTool(context.Background(), TurnRequest{Host: host}, "open", map[string]any{"target": "https://example.com/doc"})
	if !handled || invErr != nil || !strings.Contains(result, "https://example.com/doc") {
		t.Fatalf("http open: handled=%v err=%v result=%q", handled, invErr, result)
	}
	if host.url != "https://example.com/doc" || host.doc != "" {
		t.Fatalf("http open used wrong port: %#v", host)
	}
}

func TestOpenModuleRoutesFileURLToDocumentLauncher(t *testing.T) {
	registry, err := RegisterBuiltinModules()
	if err != nil {
		t.Fatal(err)
	}
	host := &recordingOpenHost{}
	_, handled, invErr := registry.InvokeTool(context.Background(), TurnRequest{Host: host}, "open", map[string]any{"target": "file:///tmp/report.pdf"})
	if !handled || invErr != nil {
		t.Fatalf("file url open: handled=%v err=%v", handled, invErr)
	}
	if host.doc != "/tmp/report.pdf" || host.url != "" {
		t.Fatalf("file url open used wrong port: %#v", host)
	}
}

func TestOpenModuleUnescapesAndNormalizesFileURLs(t *testing.T) {
	registry, err := RegisterBuiltinModules()
	if err != nil {
		t.Fatal(err)
	}
	host := &recordingOpenHost{}
	_, handled, invErr := registry.InvokeTool(context.Background(), TurnRequest{Host: host}, "open", map[string]any{"target": "file:///C:/Users/me/My%20Doc.pdf"})
	if !handled || invErr != nil {
		t.Fatalf("windows file url: handled=%v err=%v", handled, invErr)
	}
	if host.doc != `C:/Users/me/My Doc.pdf` {
		t.Fatalf("windows file url path = %q", host.doc)
	}
}

func TestOpenModuleRejectsEmptyFileURL(t *testing.T) {
	registry, err := RegisterBuiltinModules()
	if err != nil {
		t.Fatal(err)
	}
	host := &recordingOpenHost{}
	_, handled, invErr := registry.InvokeTool(context.Background(), TurnRequest{Host: host}, "open", map[string]any{"target": "file://"})
	if !handled || invErr == nil || !strings.Contains(invErr.Error(), "missing file path") {
		t.Fatalf("empty file url: handled=%v err=%v", handled, invErr)
	}
	if host.doc != "" || host.url != "" {
		t.Fatalf("empty file url opened something: %#v", host)
	}
}

func TestScreenshotModulePassesDisplayToHostPort(t *testing.T) {
	registry, err := RegisterBuiltinModules()
	if err != nil {
		t.Fatal(err)
	}
	host := &recordingCaptureHost{}
	_, handled, invErr := registry.InvokeTool(context.Background(), TurnRequest{Host: host}, "screenshot", map[string]any{"display": 2})
	if !handled || invErr != nil {
		t.Fatalf("screenshot display: handled=%v err=%v", handled, invErr)
	}
	if !host.called || host.display != 2 {
		t.Fatalf("display not forwarded: %#v", host)
	}
	host = &recordingCaptureHost{}
	_, handled, invErr = registry.InvokeTool(context.Background(), TurnRequest{Host: host}, "screenshot", nil)
	if !handled || invErr != nil {
		t.Fatalf("screenshot default: handled=%v err=%v", handled, invErr)
	}
	if !host.called || host.display != DesktopCaptureAllDisplays {
		t.Fatalf("omitted display = %d, want %d", host.display, DesktopCaptureAllDisplays)
	}
}

func TestScreenshotModuleRejectsEmptyCapture(t *testing.T) {
	registry, err := RegisterBuiltinModules()
	if err != nil {
		t.Fatal(err)
	}
	_, handled, invErr := registry.InvokeTool(context.Background(), TurnRequest{Host: emptyCaptureHost{}}, "screenshot", nil)
	if !handled || invErr == nil || !strings.Contains(invErr.Error(), "no image") {
		t.Fatalf("empty capture: handled=%v err=%v", handled, invErr)
	}
}

func TestAdvertisedHostFlagsDoNotBypassMissingPorts(t *testing.T) {
	host := HeadlessHostCapabilities{Capabilities: map[string]bool{"desktop_capture": true}}
	registry, err := RegisterBuiltinModules()
	if err != nil {
		t.Fatal(err)
	}
	_, handled, invErr := registry.InvokeTool(context.Background(), TurnRequest{Host: host}, "screenshot", nil)
	if !handled || !IsCapabilityUnavailable(invErr, "") {
		t.Fatalf("advertised flag without port: handled=%v err=%v", handled, invErr)
	}
}

func TestObserveToolFailureIgnoresCapabilityUnavailable(t *testing.T) {
	retry := NewAdaptiveRetry(nil)
	retry.SetMaxFailuresForTesting(1)
	retry.ObserveToolFailure("screenshot", CapabilityUnavailableResult("desktop_capture"), 0)
	retry.ObserveToolFailure("screenshot", CapabilityUnavailableResult("desktop_capture"), 1)
	if _, blocked := retry.GuardDisabledTool("screenshot"); blocked {
		t.Fatal("capability_unavailable must not disable a host tool")
	}
}

func TestAdaptiveRetryDisablesToolAfterRepeatedFailures(t *testing.T) {
	retry := NewAdaptiveRetry(nil)
	retry.SetMaxFailuresForTesting(2)
	if _, blocked := retry.GuardDisabledTool("bash"); blocked {
		t.Fatal("fresh retry must not disable tools")
	}
	retry.ObserveToolFailure("bash", "connection reset by peer", 0)
	retry.ObserveToolFailure("bash", "connection reset by peer", 1)
	retry.ObserveToolFailure("bash", "connection reset by peer", 2)
	msg, blocked := retry.GuardDisabledTool("bash")
	if !blocked || !strings.Contains(msg, "disabled") {
		t.Fatalf("expected disable gate, got blocked=%v msg=%q", blocked, msg)
	}
}

type staticHostCapabilities struct {
	headless bool
	desktop  bool
}

func (h staticHostCapabilities) Profile() CapabilityProfile {
	caps := map[string]bool{}
	if h.desktop {
		caps["desktop_capture"] = true
		caps["document_launcher"] = true
		caps["url_launcher"] = true
	}
	return CapabilityProfile{Name: "test", Headless: h.headless, Capabilities: caps}
}

func (h staticHostCapabilities) DesktopCapture() (DesktopCapturePort, bool) {
	if !h.desktop {
		return nil, false
	}
	return staticDesktopCapture{}, true
}

func (staticHostCapabilities) DocumentLauncher() (DocumentLauncherPort, bool) { return nil, false }
func (staticHostCapabilities) URLLauncher() (URLLauncherPort, bool)           { return nil, false }
func (staticHostCapabilities) SpeechRenderer() (SpeechRendererPort, bool)     { return nil, false }

type staticDesktopCapture struct{}

func (staticDesktopCapture) Capture(context.Context, DesktopCaptureRequest) ([]byte, string, error) {
	return []byte("png"), "image/png", nil
}

type emptyCaptureHost struct{}

func (emptyCaptureHost) Profile() CapabilityProfile {
	return CapabilityProfile{Name: "gui", Headless: false, Capabilities: map[string]bool{"desktop_capture": true}}
}
func (emptyCaptureHost) DesktopCapture() (DesktopCapturePort, bool) {
	return emptyDesktopCapture{}, true
}
func (emptyCaptureHost) DocumentLauncher() (DocumentLauncherPort, bool) { return nil, false }
func (emptyCaptureHost) URLLauncher() (URLLauncherPort, bool)           { return nil, false }
func (emptyCaptureHost) SpeechRenderer() (SpeechRendererPort, bool)     { return nil, false }

type emptyDesktopCapture struct{}

func (emptyDesktopCapture) Capture(context.Context, DesktopCaptureRequest) ([]byte, string, error) {
	return nil, "image/png", nil
}

type recordingCaptureHost struct {
	called  bool
	display int
}

func (recordingCaptureHost) Profile() CapabilityProfile {
	return CapabilityProfile{Name: "gui", Headless: false, Capabilities: map[string]bool{"desktop_capture": true}}
}
func (h *recordingCaptureHost) DesktopCapture() (DesktopCapturePort, bool) {
	return recordingDesktopCapture{host: h}, true
}
func (*recordingCaptureHost) DocumentLauncher() (DocumentLauncherPort, bool) { return nil, false }
func (*recordingCaptureHost) URLLauncher() (URLLauncherPort, bool)           { return nil, false }
func (*recordingCaptureHost) SpeechRenderer() (SpeechRendererPort, bool)     { return nil, false }

type recordingDesktopCapture struct{ host *recordingCaptureHost }

func (c recordingDesktopCapture) Capture(_ context.Context, req DesktopCaptureRequest) ([]byte, string, error) {
	c.host.called = true
	c.host.display = req.Display
	return []byte("png"), "image/png", nil
}

type recordingOpenHost struct {
	url string
	doc string
}

func (recordingOpenHost) Profile() CapabilityProfile {
	return CapabilityProfile{Name: "gui", Headless: false}
}
func (h *recordingOpenHost) DesktopCapture() (DesktopCapturePort, bool) { return nil, false }
func (h *recordingOpenHost) DocumentLauncher() (DocumentLauncherPort, bool) {
	return recordingDocumentLauncher{host: h}, true
}
func (h *recordingOpenHost) URLLauncher() (URLLauncherPort, bool) {
	return recordingURLLauncher{host: h}, true
}
func (recordingOpenHost) SpeechRenderer() (SpeechRendererPort, bool) { return nil, false }

type recordingDocumentLauncher struct{ host *recordingOpenHost }

func (l recordingDocumentLauncher) OpenDocument(_ context.Context, absPath string) error {
	l.host.doc = absPath
	return nil
}

type recordingURLLauncher struct{ host *recordingOpenHost }

func (l recordingURLLauncher) OpenURL(_ context.Context, rawURL string) error {
	l.host.url = rawURL
	return nil
}
