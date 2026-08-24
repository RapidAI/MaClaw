package agentservice

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/audioconv"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostAudioTranscribeProviderID     = "core-audio-transcribe"
	reviewedHostAudioTranscribeImplementation = "local"
	reviewedHostAudioTranscribeAdapterName    = "host_audio_transcribe_speech"
	// ReviewedHostAudioTranscribeMaxBytes is the shared host cap for one
	// trusted current-turn audio attachment.
	ReviewedHostAudioTranscribeMaxBytes = 20 << 20
)

type reviewedHostAudioInput struct {
	MIME string
	Data []byte
}

type reviewedHostSpeechReadiness interface {
	Ready() bool
}

type reviewedHostSpeechTranscriber interface {
	TranscribeSpeech(ctx context.Context, mime string, data []byte) (string, error)
}

// ReviewedHostWAVSpeechEngine is a host-owned WAV-only recognizer. Hosts must
// not expose path, minutes, or GUI asr names through this boundary.
type ReviewedHostWAVSpeechEngine interface {
	TranscribeWAV(ctx context.Context, wav []byte) (string, error)
}

type reviewedHostAudioconvSpeechTranscriber struct {
	engine ReviewedHostWAVSpeechEngine
}

// NewReviewedHostSpeechTranscriber converts trusted attachment bytes with
// audioconv.ToWAV and hands 16 kHz WAV to the host engine. It does not run
// minutes/map-reduce and does not accept a model-supplied path. A nil engine
// stays absent so the catalog can fail closed.
func NewReviewedHostSpeechTranscriber(engine ReviewedHostWAVSpeechEngine) reviewedHostSpeechTranscriber {
	if engine == nil {
		return nil
	}
	return reviewedHostAudioconvSpeechTranscriber{engine: engine}
}

func (t reviewedHostAudioconvSpeechTranscriber) Ready() bool {
	if t.engine == nil {
		return false
	}
	if ready, ok := t.engine.(reviewedHostSpeechReadiness); ok {
		return ready.Ready()
	}
	return true
}

func (t reviewedHostAudioconvSpeechTranscriber) TranscribeSpeech(ctx context.Context, mime string, data []byte) (string, error) {
	if t.engine == nil || len(data) == 0 || !t.Ready() {
		return "", fmt.Errorf("host_audio_transcribe_unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	wav, err := audioconv.ToWAV(data, mime)
	if err != nil || len(wav) == 0 {
		if err != nil {
			return "", fmt.Errorf("host_audio_transcribe_decode_failed: %w", err)
		}
		return "", fmt.Errorf("host_audio_transcribe_decode_failed")
	}
	text, err := t.engine.TranscribeWAV(ctx, wav)
	if err != nil {
		return "", err
	}
	return reviewedHostTranscriptText(text)
}

func reviewedHostSpeechReady(transcriber reviewedHostSpeechTranscriber) bool {
	if transcriber == nil {
		return false
	}
	if ready, ok := transcriber.(reviewedHostSpeechReadiness); ok {
		return ready.Ready()
	}
	return true
}

func reviewedHostTranscriptText(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("host_audio_transcribe_empty")
	}
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("host_audio_transcribe_delivery_bypass")
	}
	return text, nil
}

func bindReviewedHostAudioTurn(needs []coretool.CapabilityNeed, inputs []reviewedHostAudioInput, inputErr error) ([]coretool.CapabilityNeed, error) {
	if !reviewedHostAudioNeedPresent(needs) {
		return needs, nil
	}
	if inputErr != nil {
		return nil, inputErr
	}
	return applyReviewedHostAudioInputs(needs, inputs)
}

type reviewedHostAudioTranscriber interface {
	TranscribeReviewedHostAudio(ctx context.Context, principal Principal, args map[string]interface{}) (string, error)
}

func reviewedHostAudioTranscribeInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func reviewedHostAudioTranscribeContractDigest() string {
	return coretool.SchemaDigest([]byte("audio.transcribe.speech:v1:host-audio-transcribe"))
}

// ProjectReviewedHostAudioTranscribeProvider projects the host-owned
// trusted-attachment speech transcriber. It is not a Skill/MCP discovery
// entry and must not import the GUI asr catalog. The closed schema has no
// path, format, minutes, or channel fields; bytes come from one host-published
// current-turn audio attachment. This is not audio.capture.microphone,
// audio.synthesize.local, or audio.render.speech. The host process observes
// the transcript, so the handler result is the local completion receipt.
func ProjectReviewedHostAudioTranscribeProvider(transcriber reviewedHostAudioTranscriber) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if transcriber == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host audio transcriber is unavailable")
	}
	parameters := reviewedHostAudioTranscribeInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host audio transcribe schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostAudioTranscribeContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-audio-transcribe-empty-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostAudioTranscribeAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostAudioTranscribeProviderID,
			ImplementationID: reviewedHostAudioTranscribeImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityAudioTranscribe,
			Quality:    1,
		}},
		Effects: []coretool.EffectClass{coretool.EffectReadOnly},
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostAudioTranscribe(transcriber)}, nil
}

func AttachReviewedHostAudioTranscribeProvider(catalog DynamicSemanticCatalog, transcriber reviewedHostAudioTranscriber) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostAudioTranscribeProvider(transcriber)
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

func executeReviewedHostAudioTranscribe(transcriber reviewedHostAudioTranscriber) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if transcriber == nil {
			return "", fmt.Errorf("host_audio_transcribe_unavailable")
		}
		if len(args) != 0 {
			return "", fmt.Errorf("host_audio_transcribe_arguments_rejected")
		}
		return transcriber.TranscribeReviewedHostAudio(ctx, principal, args)
	}
}

func reviewedHostAudioInputsForTurn(rootTaskID, turnID, principalID string, attachments []agent.MessageAttachment) ([]reviewedHostAudioInput, error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	attachments = CanonicalizeReviewedHostMessageAttachments(attachments)
	inputs := make([]reviewedHostAudioInput, 0, len(attachments))
	for _, attachment := range attachments {
		mimeType, ok := reviewedHostAudioMIME(attachment)
		if !ok {
			continue
		}
		raw, err := decodeReviewedHostAttachmentBytes(attachment.Data)
		if err != nil {
			return nil, err
		}
		if len(raw) > ReviewedHostAudioTranscribeMaxBytes {
			return nil, fmt.Errorf("trusted_audio_attachment_too_large")
		}
		inputs = append(inputs, reviewedHostAudioInput{MIME: mimeType, Data: raw})
	}
	return inputs, nil
}

func applyReviewedHostAudioInputs(needs []coretool.CapabilityNeed, inputs []reviewedHostAudioInput) ([]coretool.CapabilityNeed, error) {
	if !reviewedHostAudioNeedPresent(needs) {
		return needs, nil
	}
	if len(inputs) == 0 {
		return nil, fmt.Errorf("trusted_audio_input_missing")
	}
	if len(inputs) != 1 {
		return nil, fmt.Errorf("trusted_audio_input_ambiguous")
	}
	return needs, nil
}

func reviewedHostAudioNeedPresent(needs []coretool.CapabilityNeed) bool {
	for _, need := range needs {
		if need.Capability == CapabilityAudioTranscribe {
			return true
		}
	}
	return false
}

// ReviewedHostTrustedAudioMIME reports whether a host attachment is in the
// closed speech-transcribe allowlist and returns the canonical MIME.
func ReviewedHostTrustedAudioMIME(attachment agent.MessageAttachment) (string, bool) {
	return reviewedHostAudioMIME(attachment)
}

// ReviewedHostTrustedAudioMIMEType reports the same closed audio allowlist
// from a filename and MIME without requiring a full attachment.
func ReviewedHostTrustedAudioMIMEType(fileName, mimeType string) (string, bool) {
	return reviewedHostAudioMIME(agent.MessageAttachment{FileName: fileName, MimeType: mimeType})
}

func reviewedHostAudioMIME(attachment agent.MessageAttachment) (string, bool) {
	mimeType := strings.ToLower(strings.TrimSpace(strings.SplitN(attachment.MimeType, ";", 2)[0]))
	switch mimeType {
	case "audio/wav", "audio/x-wav", "audio/wave":
		return "audio/wav", true
	case "audio/mpeg", "audio/mp3":
		return "audio/mpeg", true
	case "audio/ogg":
		return "audio/ogg", true
	case "audio/opus":
		return "audio/opus", true
	case "audio/silk", "audio/x-silk":
		return "audio/silk", true
	case "audio/mp4", "audio/aac", "audio/x-m4a":
		return "audio/mp4", true
	case "audio/webm":
		return "audio/webm", true
	}
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(attachment.FileName))) {
	case ".wav":
		return "audio/wav", true
	case ".mp3":
		return "audio/mpeg", true
	case ".ogg":
		return "audio/ogg", true
	case ".opus":
		return "audio/opus", true
	case ".silk":
		return "audio/silk", true
	case ".m4a", ".aac":
		return "audio/mp4", true
	case ".webm":
		return "audio/webm", true
	}
	return "", false
}

func decodeReviewedHostAttachmentBytes(data string) ([]byte, error) {
	encoded := strings.TrimSpace(data)
	if encoded == "" {
		return nil, fmt.Errorf("trusted_audio_attachment_content_missing")
	}
	encodings := []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding}
	if strings.ContainsAny(encoded, "-_") {
		encodings = append(encodings, base64.URLEncoding, base64.RawURLEncoding)
	}
	for _, enc := range encodings {
		raw, err := enc.DecodeString(encoded)
		if err == nil && len(raw) > 0 {
			return raw, nil
		}
	}
	return nil, fmt.Errorf("trusted_audio_attachment_content_missing")
}

func (c *coreAgentCallbacks) TranscribeReviewedHostAudio(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
	if c == nil || c.reviewedHostAudio == nil || !reviewedHostSpeechReady(c.speechTranscriber) {
		return "", fmt.Errorf("host_audio_transcribe_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_audio_transcribe_principal_mismatch")
	}
	if len(args) != 0 {
		return "", fmt.Errorf("host_audio_transcribe_arguments_rejected")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
	}
	raw := c.reviewedHostAudio.Data
	if len(raw) == 0 {
		return "", fmt.Errorf("trusted_audio_payload_invalid")
	}
	text, err := c.speechTranscriber.TranscribeSpeech(ctx, c.reviewedHostAudio.MIME, raw)
	if err != nil {
		return "", err
	}
	return reviewedHostTranscriptText(text)
}

func (e *CoreAgentExecutor) SetReviewedHostSpeechTranscriber(transcriber reviewedHostSpeechTranscriber) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.speechTranscriber = transcriber
	e.mu.Unlock()
}

func (e *CoreAgentExecutor) getReviewedHostSpeechTranscriber() reviewedHostSpeechTranscriber {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.speechTranscriber
}
