package agentservice

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/remote"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostAudioSynthesizeProviderID     = "core-audiosynthesize"
	reviewedHostAudioSynthesizeImplementation = "local"
	reviewedHostAudioSynthesizeAdapterName    = "host_audio_synthesize_local"
	reviewedHostAudioSynthesizeMaxBytes       = reviewedHostAudioRenderMaxBytes
	reviewedHostAudioSynthesizeMaxRunes       = reviewedHostAudioRenderMaxRunes
)

type reviewedHostAudioSynthesizer interface {
	PlayReviewedHostSpeech(ctx context.Context, principal Principal, text string) (string, error)
}

type reviewedHostSpeechPlayer interface {
	PlaySpeech(ctx context.Context, wav []byte) error
}

func reviewedHostAudioSynthesizeInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"text": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"text"},
		"additionalProperties": false,
	}
}

func reviewedHostAudioSynthesizeContractDigest() string {
	return coretool.SchemaDigest([]byte("audio.synthesize.local:v1:host-audiosynthesize-text-play"))
}

func reviewedHostSpeechPlayerReady(player reviewedHostSpeechPlayer) bool {
	if player == nil {
		return false
	}
	if ready, ok := player.(reviewedHostSpeechReadiness); ok {
		return ready.Ready()
	}
	return true
}

// ProjectReviewedHostAudioSynthesizeProvider projects host-owned local
// speech playback. It is not a Skill/MCP discovery entry and must not
// import GUI tts / tts_render / tts_local. The closed schema accepts text
// only. Path, channel, and destination stay out. This is not a send and
// not audio.render.speech.
func ProjectReviewedHostAudioSynthesizeProvider(player reviewedHostAudioSynthesizer) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if player == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host speech player is unavailable")
	}
	parameters := reviewedHostAudioSynthesizeInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host audio synthesize schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostAudioSynthesizeContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-audiosynthesize-text-play-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostAudioSynthesizeAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostAudioSynthesizeProviderID,
			ImplementationID: reviewedHostAudioSynthesizeImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityAudioSynthesize,
			Quality:    1,
		}},
		Effects: []coretool.EffectClass{coretool.EffectLocalMutation},
		Ready:   true,
	}
	definition := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "dynamic_provider",
			"description": "",
			"parameters":  parameters,
		},
	}
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostAudioSynthesize(player)}, nil
}

func AttachReviewedHostAudioSynthesizeProvider(catalog DynamicSemanticCatalog, player reviewedHostAudioSynthesizer) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostAudioSynthesizeProvider(player)
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

func executeReviewedHostAudioSynthesize(player reviewedHostAudioSynthesizer) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if player == nil {
			return "", fmt.Errorf("host_audio_synthesize_unavailable")
		}
		text, err := reviewedHostAudioSynthesizeArgsAllowed(args)
		if err != nil {
			return "", err
		}
		return player.PlayReviewedHostSpeech(ctx, principal, text)
	}
}

func reviewedHostAudioSynthesizeArgsAllowed(args map[string]interface{}) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("host_audio_synthesize_arguments_rejected")
	}
	raw, ok := args["text"]
	if !ok {
		return "", fmt.Errorf("host_audio_synthesize_arguments_rejected")
	}
	text, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("host_audio_synthesize_arguments_rejected")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("host_audio_synthesize_text_required")
	}
	if len(text) > reviewedHostAudioSynthesizeMaxBytes || utf8.RuneCountInString(text) > reviewedHostAudioSynthesizeMaxRunes {
		return "", fmt.Errorf("host_audio_synthesize_text_too_large")
	}
	if strings.Contains(text, "[file_base64") || strings.Contains(text, "[voice_base64") {
		return "", fmt.Errorf("host_audio_synthesize_delivery_bypass")
	}
	return text, nil
}

func (c *coreAgentCallbacks) PlayReviewedHostSpeech(ctx context.Context, principal Principal, text string) (string, error) {
	if c == nil || !reviewedHostSpeechSynthesizerReady(c.speechSynthesizer) || !reviewedHostSpeechPlayerReady(c.speechPlayer) {
		return "", fmt.Errorf("host_audio_synthesize_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_audio_synthesize_principal_mismatch")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("host_audio_synthesize_text_required")
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
		return "", fmt.Errorf("host_audio_synthesize_unavailable")
	}
	if _, _, ok := reviewedHostDeliverableVoice("speech.wav", "audio/wav"); !ok {
		return "", fmt.Errorf("host_audio_synthesize_voice_required")
	}
	if err := c.speechPlayer.PlaySpeech(ctx, wav); err != nil {
		return "", err
	}
	return "Speech played on the host. This is not a send.", nil
}

type reviewedHostNativeSpeechPlayer struct{}

func (reviewedHostNativeSpeechPlayer) Ready() bool {
	ok, _ := remote.DetectDisplayServer()
	return ok
}

func (reviewedHostNativeSpeechPlayer) PlaySpeech(ctx context.Context, wav []byte) error {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	if len(wav) == 0 {
		return fmt.Errorf("host_audio_synthesize_unavailable")
	}
	tmp, err := os.CreateTemp("", "maclaw-host-speech-*.wav")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(wav); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", name)
	case "darwin":
		cmd = exec.Command("afplay", name)
	default:
		cmd = exec.Command("xdg-open", name)
	}
	if err := cmd.Start(); err != nil {
		_ = os.Remove(name)
		return err
	}
	go func() {
		_ = cmd.Wait()
		_ = os.Remove(name)
	}()
	return nil
}

// WireReviewedHostNativeSpeechPlayer attaches the host-owned local speech
// player. Ready() is the plan-time gate: headless hosts stay unpublished.
func WireReviewedHostNativeSpeechPlayer(e *CoreAgentExecutor) {
	if e == nil {
		return
	}
	e.SetReviewedHostSpeechPlayer(reviewedHostNativeSpeechPlayer{})
}

func (e *CoreAgentExecutor) SetReviewedHostSpeechPlayer(player reviewedHostSpeechPlayer) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.speechPlayer = player
	e.mu.Unlock()
}

func (e *CoreAgentExecutor) getReviewedHostSpeechPlayer() reviewedHostSpeechPlayer {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.speechPlayer
}
