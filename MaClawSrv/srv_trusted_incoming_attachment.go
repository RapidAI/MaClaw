package main

import (
	"encoding/base64"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/audioconv"
	"github.com/RapidAI/CodeClaw/corelib/weixin"
)

type srvTrustedIncomingMedia struct {
	FileName         string
	MediaType        string
	MimeType         string
	SourceMediaID    string
	Data             []byte
	AudioFormatHint  string
	DefaultAudioMIME string
}

func srvWeixinTrustedIncomingAttachment(msg weixin.IncomingMessage) (agent.MessageAttachment, bool) {
	sourceID := ""
	if token := strings.TrimSpace(msg.ContextToken); token != "" {
		sourceID = "weixin-media:" + token
	}
	defaultMIME := ""
	if srvWeixinIncomingIsVoice(msg) {
		defaultMIME = "audio/silk"
	}
	return srvTrustedIncomingHostAttachment(srvTrustedIncomingMedia{
		FileName:         msg.MediaName,
		MediaType:        msg.MediaType,
		MimeType:         msg.MediaType,
		SourceMediaID:    sourceID,
		Data:             msg.MediaData,
		AudioFormatHint:  srvWeixinAudioFormatHint(msg),
		DefaultAudioMIME: defaultMIME,
	})
}

func srvIMTrustedIncomingAttachment(platform string, msg srvIMIncomingMessage) (agent.MessageAttachment, bool) {
	sourceID := ""
	if id := strings.TrimSpace(msg.ClientEventID); id != "" {
		sourceID = normalizeSrvIMPlatform(platform) + "-media:" + id
	}
	return srvTrustedIncomingHostAttachment(srvTrustedIncomingMedia{
		FileName:        msg.MediaName,
		MediaType:       msg.MediaType,
		MimeType:        firstNonEmptyString(msg.MimeType, msg.MediaType),
		SourceMediaID:   sourceID,
		Data:            msg.MediaData,
		AudioFormatHint: srvIMAudioFormatHint(msg),
	})
}

func srvTrustedIncomingHostAttachment(in srvTrustedIncomingMedia) (agent.MessageAttachment, bool) {
	if len(in.Data) == 0 {
		return agent.MessageAttachment{}, false
	}
	if att, ok := agentservice.ReviewedHostCanonicalizeTrustedAttachment(agent.MessageAttachment{
		Type:          firstNonEmptyString(in.MediaType, "file"),
		FileName:      in.FileName,
		MimeType:      firstNonEmptyString(in.MimeType, in.MediaType),
		Data:          base64.StdEncoding.EncodeToString(in.Data),
		SourceMediaID: strings.TrimSpace(in.SourceMediaID),
	}); ok {
		return att, true
	}
	if att, ok := srvTrustedIncomingAudioAttachment(in); ok {
		return att, true
	}
	if att, ok := srvTrustedIncomingDocumentAttachment(in); ok {
		return att, true
	}
	return agent.MessageAttachment{}, false
}

func srvTrustedIncomingAudioAttachment(in srvTrustedIncomingMedia) (agent.MessageAttachment, bool) {
	if len(in.Data) == 0 || len(in.Data) > agentservice.ReviewedHostAudioTranscribeMaxBytes {
		return agent.MessageAttachment{}, false
	}
	mime, name, ok := srvTrustedIncomingAudioMIME(in)
	if !ok {
		return agent.MessageAttachment{}, false
	}
	return agent.MessageAttachment{
		Type:          "audio",
		FileName:      name,
		MimeType:      mime,
		Data:          base64.StdEncoding.EncodeToString(in.Data),
		Size:          int64(len(in.Data)),
		SourceMediaID: strings.TrimSpace(in.SourceMediaID),
	}, true
}

func srvTrustedIncomingDocumentAttachment(in srvTrustedIncomingMedia) (agent.MessageAttachment, bool) {
	if len(in.Data) == 0 || int64(len(in.Data)) > agent.MaxOfficeReadFileBytes {
		return agent.MessageAttachment{}, false
	}
	mime, ok := agentservice.ReviewedHostTrustedDocumentMIME(in.FileName, firstNonEmptyString(in.MimeType, in.MediaType))
	if !ok {
		return agent.MessageAttachment{}, false
	}
	return agent.MessageAttachment{
		Type:          "file",
		FileName:      srvTrustedIncomingFileName(in.FileName, "document"),
		MimeType:      mime,
		Data:          base64.StdEncoding.EncodeToString(in.Data),
		Size:          int64(len(in.Data)),
		SourceMediaID: strings.TrimSpace(in.SourceMediaID),
	}, true
}

func srvTrustedIncomingAudioMIME(in srvTrustedIncomingMedia) (string, string, bool) {
	probe := agent.MessageAttachment{
		Type:     "audio",
		FileName: in.FileName,
		MimeType: firstNonEmptyString(in.MimeType, in.MediaType),
	}
	if mime, ok := agentservice.ReviewedHostTrustedAudioMIME(probe); ok {
		return mime, srvTrustedIncomingFileName(in.FileName, srvTrustedIncomingAudioFallbackName(mime)), true
	}
	if mime, ok := srvAudioMIMEFromFormatHint(in.AudioFormatHint); ok {
		return mime, srvTrustedIncomingFileName(in.FileName, srvTrustedIncomingAudioFallbackName(mime)), true
	}
	if mime, ok := agentservice.ReviewedHostTrustedAudioMIME(agent.MessageAttachment{
		Type:     "audio",
		MimeType: in.DefaultAudioMIME,
	}); ok {
		return mime, srvTrustedIncomingFileName(in.FileName, srvTrustedIncomingAudioFallbackName(mime)), true
	}
	return "", "", false
}

func srvAudioMIMEFromFormatHint(hint string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(hint)) {
	case audioconv.FormatSilk:
		return "audio/silk", true
	case audioconv.FormatOGG:
		return "audio/ogg", true
	case audioconv.FormatOpus:
		return "audio/opus", true
	case audioconv.FormatWAV:
		return "audio/wav", true
	case audioconv.FormatMP3:
		return "audio/mpeg", true
	case audioconv.FormatM4A, audioconv.FormatAAC:
		return "audio/mp4", true
	default:
		return "", false
	}
}

func srvTrustedIncomingAudioFallbackName(mime string) string {
	switch mime {
	case "audio/wav":
		return "recording.wav"
	case "audio/mpeg":
		return "recording.mp3"
	case "audio/ogg":
		return "recording.ogg"
	case "audio/opus":
		return "recording.opus"
	case "audio/silk":
		return "recording.silk"
	case "audio/mp4":
		return "recording.m4a"
	case "audio/webm":
		return "recording.webm"
	default:
		return "recording"
	}
}

func srvTrustedIncomingFileName(name, fallback string) string {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return fallback
	}
	return base
}
