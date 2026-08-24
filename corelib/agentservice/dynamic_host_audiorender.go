package agentservice

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostAudioRenderProviderID     = "core-audiorender"
	reviewedHostAudioRenderImplementation = "local"
	reviewedHostAudioRenderAdapterName    = "host_audio_render_speech"
	reviewedHostAudioRenderMaxBytes       = 32 * 1024
	reviewedHostAudioRenderMaxRunes       = 8000
)

type reviewedHostGeneratedSpeech struct {
	Payload  coretool.ArtifactPayload
	FileName string
	MIMEType string
	Data     []byte
}

type reviewedHostAudioRenderer interface {
	RenderReviewedHostSpeech(ctx context.Context, principal Principal, text string) (string, error)
}

type reviewedHostSpeechSynthesizer interface {
	RenderSpeech(ctx context.Context, text string) ([]byte, error)
}

func reviewedHostAudioRenderInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"text": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"text"},
		"additionalProperties": false,
	}
}

func reviewedHostAudioRenderContractDigest() string {
	return coretool.SchemaDigest([]byte("audio.render.speech:v1:host-audiorender-wav"))
}

func reviewedHostSpeechSynthesizerReady(synth reviewedHostSpeechSynthesizer) bool {
	if synth == nil {
		return false
	}
	if ready, ok := synth.(reviewedHostSpeechReadiness); ok {
		return ready.Ready()
	}
	return true
}

// ProjectReviewedHostAudioRenderProvider projects the host-owned speech
// renderer. It is not a Skill/MCP discovery entry and must not import GUI
// tts / tts_render / tts_local. The closed schema accepts text only. Path,
// channel, destination, and file_name stay out. This is not a send.
func ProjectReviewedHostAudioRenderProvider(renderer reviewedHostAudioRenderer) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if renderer == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host speech renderer is unavailable")
	}
	parameters := reviewedHostAudioRenderInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host audio render schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostAudioRenderContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-audiorender-text-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostAudioRenderAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostAudioRenderProviderID,
			ImplementationID: reviewedHostAudioRenderImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityAudioRender,
			Quality:    1,
		}},
		Produces: []coretool.ArtifactContract{{Kind: "audio", MIMEType: "audio/wav", Required: true}},
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostAudioRender(renderer)}, nil
}

func AttachReviewedHostAudioRenderProvider(catalog DynamicSemanticCatalog, renderer reviewedHostAudioRenderer) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostAudioRenderProvider(renderer)
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

func executeReviewedHostAudioRender(renderer reviewedHostAudioRenderer) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if renderer == nil {
			return "", fmt.Errorf("host_audio_render_unavailable")
		}
		text, err := reviewedHostAudioRenderArgsAllowed(args)
		if err != nil {
			return "", err
		}
		return renderer.RenderReviewedHostSpeech(ctx, principal, text)
	}
}

func reviewedHostAudioRenderArgsAllowed(args map[string]interface{}) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("host_audio_render_arguments_rejected")
	}
	raw, ok := args["text"]
	if !ok {
		return "", fmt.Errorf("host_audio_render_arguments_rejected")
	}
	text, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("host_audio_render_arguments_rejected")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("host_audio_render_text_required")
	}
	if len(text) > reviewedHostAudioRenderMaxBytes || utf8.RuneCountInString(text) > reviewedHostAudioRenderMaxRunes {
		return "", fmt.Errorf("host_audio_render_text_too_large")
	}
	if strings.Contains(text, "[file_base64") || strings.Contains(text, "[voice_base64") {
		return "", fmt.Errorf("host_audio_render_delivery_bypass")
	}
	return text, nil
}

func reviewedHostAudioRenderNeedPresent(needs []coretool.CapabilityNeed) bool {
	for _, need := range needs {
		if need.Capability == CapabilityAudioRender {
			return true
		}
	}
	return false
}

func (c *coreAgentCallbacks) RenderReviewedHostSpeech(ctx context.Context, principal Principal, text string) (string, error) {
	if c == nil || !reviewedHostSpeechSynthesizerReady(c.speechSynthesizer) {
		return "", fmt.Errorf("host_audio_render_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_audio_render_principal_mismatch")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("host_audio_render_text_required")
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
	}
	wav, err := c.speechSynthesizer.RenderSpeech(ctx, text)
	if err != nil || len(wav) == 0 {
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("host_audio_render_unavailable")
	}
	if _, _, ok := reviewedHostDeliverableVoice("speech.wav", "audio/wav"); !ok {
		return "", fmt.Errorf("host_audio_render_voice_required")
	}
	generated, err := reviewedHostGeneratedSpeechFromWAV(text, wav)
	if err != nil {
		return "", err
	}
	c.reviewedHostGeneratedSpeech = generated
	return "Speech artifact published; deliver it through the current-channel voice adapter. This is not a send.", nil
}

func reviewedHostGeneratedSpeechFromWAV(text string, wav []byte) (*reviewedHostGeneratedSpeech, error) {
	scope := coretool.InvocationScope{
		RootTaskID:  "audio-render",
		PlanID:      "render",
		SessionID:   "host",
		TurnID:      "render",
		PrincipalID: "host",
	}
	producer := "selection:audio-render"
	payload, err := coretool.NewArtifactPayload(scope, producer, "audio", "audio/wav", base64.StdEncoding.EncodeToString(wav), time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("host_audio_render_artifact_invalid: %w", err)
	}
	_ = text
	return &reviewedHostGeneratedSpeech{Payload: payload, FileName: "speech.wav", MIMEType: "audio/wav", Data: wav}, nil
}

func (e *CoreAgentExecutor) SetReviewedHostSpeechSynthesizer(synth reviewedHostSpeechSynthesizer) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.speechSynthesizer = synth
	e.mu.Unlock()
}

func (e *CoreAgentExecutor) getReviewedHostSpeechSynthesizer() reviewedHostSpeechSynthesizer {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.speechSynthesizer
}
