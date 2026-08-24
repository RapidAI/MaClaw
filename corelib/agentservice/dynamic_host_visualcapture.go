package agentservice

import (
	"context"
	"encoding/base64"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostVisualCaptureProviderID     = "core-visualcapture"
	reviewedHostVisualCaptureImplementation = "local"
	reviewedHostVisualCaptureAdapterName    = "host_visual_capture_desktop"
)

type reviewedHostGeneratedImage struct {
	Payload  coretool.ArtifactPayload
	FileName string
	MIMEType string
	Data     []byte
}

type reviewedHostVisualCapturer interface {
	CaptureReviewedHostDesktop(ctx context.Context, principal Principal) (string, error)
}

type reviewedHostDesktopCapturer interface {
	CapturePrimary(ctx context.Context) ([]byte, error)
}

func reviewedHostVisualCaptureInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func reviewedHostVisualCaptureContractDigest() string {
	return coretool.SchemaDigest([]byte("visual.capture.desktop:v1:host-visualcapture-primary-png"))
}

func reviewedHostDesktopCapturerReady(capturer reviewedHostDesktopCapturer) bool {
	if capturer == nil {
		return false
	}
	if ready, ok := capturer.(reviewedHostSpeechReadiness); ok {
		return ready.Ready()
	}
	return true
}

// ProjectReviewedHostVisualCaptureProvider projects the host-owned primary
// display capture. It is not a Skill/MCP discovery entry and must not import
// GUI screenshot / computer_use. The closed schema is empty. Path, session_id,
// display, channel, and destination stay out. This is not a send.
func ProjectReviewedHostVisualCaptureProvider(capturer reviewedHostVisualCapturer) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if capturer == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host desktop capturer is unavailable")
	}
	parameters := reviewedHostVisualCaptureInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host visual capture schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostVisualCaptureContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-visualcapture-primary-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostVisualCaptureAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostVisualCaptureProviderID,
			ImplementationID: reviewedHostVisualCaptureImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityVisualCapture,
			Qualifiers: map[string]string{QualifierCaptureDisplay: CaptureDisplayPrimary},
			Quality:    1,
		}},
		Produces: []coretool.ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}},
		Effects:  []coretool.EffectClass{coretool.EffectLocalMutation},
		Ready:    true,
	}
	definition := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "dynamic_provider",
			"description": "",
			"parameters":  parameters,
		},
	}
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostVisualCapture(capturer)}, nil
}

func AttachReviewedHostVisualCaptureProvider(catalog DynamicSemanticCatalog, capturer reviewedHostVisualCapturer) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostVisualCaptureProvider(capturer)
	if err != nil {
		return DynamicSemanticCatalog{}, err
	}
	if err := catalog.add(provider, definition, dynamicSemanticRuntimeBinding{
		provider: provider.Binding,
		host:     &host,
	}); err != nil {
		return DynamicSemanticCatalog{}, err
	}
	return catalog, nil
}

func executeReviewedHostVisualCapture(capturer reviewedHostVisualCapturer) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if capturer == nil {
			return "", fmt.Errorf("host_visual_capture_unavailable")
		}
		if err := reviewedHostVisualCaptureArgsAllowed(args); err != nil {
			return "", err
		}
		return capturer.CaptureReviewedHostDesktop(ctx, principal)
	}
}

func reviewedHostVisualCaptureArgsAllowed(args map[string]interface{}) error {
	if len(args) > 0 {
		return fmt.Errorf("host_visual_capture_arguments_rejected")
	}
	return nil
}

func reviewedHostVisualCaptureNeedPresent(needs []coretool.CapabilityNeed) bool {
	for _, need := range needs {
		if need.Capability == CapabilityVisualCapture {
			return true
		}
	}
	return false
}

func (c *coreAgentCallbacks) CaptureReviewedHostDesktop(ctx context.Context, principal Principal) (string, error) {
	if c == nil || !reviewedHostDesktopCapturerReady(c.desktopCapturer) {
		return "", fmt.Errorf("host_visual_capture_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_visual_capture_principal_mismatch")
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
	}
	png, err := c.desktopCapturer.CapturePrimary(ctx)
	if err != nil || len(png) == 0 {
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("host_visual_capture_unavailable")
	}
	if len(png) > reviewedHostImageDeliverMaxBytes {
		return "", fmt.Errorf("host_visual_capture_too_large")
	}
	if _, _, ok := reviewedHostDeliverableImage("screenshot.png", "image/png"); !ok {
		return "", fmt.Errorf("host_visual_capture_image_required")
	}
	generated, err := reviewedHostGeneratedImageFromPNG(png)
	if err != nil {
		return "", err
	}
	c.reviewedHostGeneratedImage = generated
	return "Screenshot artifact published; deliver it through the current-channel image adapter. This is not a send.", nil
}

func reviewedHostGeneratedImageFromPNG(png []byte) (*reviewedHostGeneratedImage, error) {
	scope := coretool.InvocationScope{
		RootTaskID:  "visual-capture",
		PlanID:      "capture",
		SessionID:   "host",
		TurnID:      "capture",
		PrincipalID: "host",
	}
	producer := "selection:visual-capture"
	payload, err := coretool.NewArtifactPayload(scope, producer, "image", "image/png", base64.StdEncoding.EncodeToString(png), time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("host_visual_capture_artifact_invalid: %w", err)
	}
	return &reviewedHostGeneratedImage{Payload: payload, FileName: "screenshot.png", MIMEType: "image/png", Data: png}, nil
}

type reviewedHostNativeDesktopCapturer struct{}

func (reviewedHostNativeDesktopCapturer) Ready() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	ok, _ := remote.DetectDisplayServer()
	return ok
}

func (reviewedHostNativeDesktopCapturer) CapturePrimary(ctx context.Context) ([]byte, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	if runtime.GOOS != "windows" {
		return nil, fmt.Errorf("host_visual_capture_unavailable")
	}
	encoded, err := remote.NativeScreenshot()
	if err != nil || strings.TrimSpace(encoded) == "" {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("host_visual_capture_unavailable")
	}
	png, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(png) == 0 {
		return nil, fmt.Errorf("host_visual_capture_unavailable")
	}
	return png, nil
}

// WireReviewedHostNativeDesktopCapturer attaches the host-owned primary
// display capturer. Ready() is the plan-time gate: non-Windows and
// headless hosts stay unpublished.
func WireReviewedHostNativeDesktopCapturer(e *CoreAgentExecutor) {
	if e == nil {
		return
	}
	e.SetReviewedHostDesktopCapturer(reviewedHostNativeDesktopCapturer{})
}

func (e *CoreAgentExecutor) SetReviewedHostDesktopCapturer(capturer reviewedHostDesktopCapturer) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.desktopCapturer = capturer
	e.mu.Unlock()
}

func (e *CoreAgentExecutor) getReviewedHostDesktopCapturer() reviewedHostDesktopCapturer {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.desktopCapturer
}
