package im

// This file bridges hub's IM types with corelib/im types.
// It allows hub to use corelib's IM gateways (feishu, dingtalk, wecom)
// without duplicating gateway code, while keeping hub's richer IMPlugin interface.

import (
	"context"
	"encoding/base64"
	"fmt"

	cim "github.com/RapidAI/CodeClaw/corelib/im"
)

type corelibAudioSender interface {
	SendAudio(ctx context.Context, target cim.UserTarget, audioData []byte, durationMs int) error
}

// CorelibPluginAdapter wraps a corelib/im.Plugin to satisfy hub's IMPlugin interface.
// This allows corelib gateways to be registered with hub's IM Adapter.
type CorelibPluginAdapter struct {
	plugin       cim.Plugin
	userResolver func(platformUID string) (string, error) // optional
}

// NewCorelibPluginAdapter creates an adapter that wraps a corelib IM plugin
// for use with hub's IM Adapter.
func NewCorelibPluginAdapter(plugin cim.Plugin, userResolver func(string) (string, error)) *CorelibPluginAdapter {
	return &CorelibPluginAdapter{plugin: plugin, userResolver: userResolver}
}

func (a *CorelibPluginAdapter) Name() string { return a.plugin.Name() }

func (a *CorelibPluginAdapter) ReceiveMessage(handler func(msg IncomingMessage)) {
	a.plugin.OnMessage(func(msg cim.IncomingMessage) {
		handler(corelibToHubIncoming(msg))
	})
}

func (a *CorelibPluginAdapter) SendText(ctx context.Context, target UserTarget, text string) error {
	return a.plugin.SendText(ctx, cim.UserTarget{
		PlatformUID: target.PlatformUID,
	}, text)
}

func (a *CorelibPluginAdapter) SendCard(ctx context.Context, target UserTarget, card OutgoingMessage) error {
	// Corelib plugins don't support rich cards, fall back to text
	text := card.FallbackText
	if text == "" {
		text = card.Body
	}
	if text == "" {
		text = card.Title
	}
	return a.plugin.SendText(ctx, cim.UserTarget{PlatformUID: target.PlatformUID}, text)
}

func (a *CorelibPluginAdapter) SendImage(ctx context.Context, target UserTarget, imageKey string, caption string) error {
	// Corelib plugins don't have image upload, send caption as text
	if caption != "" {
		return a.plugin.SendText(ctx, cim.UserTarget{PlatformUID: target.PlatformUID}, caption)
	}
	return nil
}

func (a *CorelibPluginAdapter) SendFile(ctx context.Context, target UserTarget, fileData, fileName, mimeType string) error {
	// Corelib plugins don't have file upload, send notification
	return a.plugin.SendText(ctx, cim.UserTarget{PlatformUID: target.PlatformUID},
		"[文件] "+fileName)
}

func (a *CorelibPluginAdapter) SendVoice(ctx context.Context, target UserTarget, voiceData, fileName, mimeType string) error {
	sender, ok := a.plugin.(corelibAudioSender)
	if !ok {
		return fmt.Errorf("%s: native voice is not supported", a.plugin.Name())
	}
	data, err := base64.StdEncoding.DecodeString(voiceData)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(voiceData)
		if err != nil {
			return fmt.Errorf("%s: voice base64 decode failed: %w", a.plugin.Name(), err)
		}
	}
	if len(data) == 0 {
		return fmt.Errorf("%s: empty voice data", a.plugin.Name())
	}
	return sender.SendAudio(ctx, cim.UserTarget{PlatformUID: target.PlatformUID}, data, 0)
}

func (a *CorelibPluginAdapter) ResolveUser(ctx context.Context, platformUID string) (string, error) {
	if a.userResolver != nil {
		return a.userResolver(platformUID)
	}
	return "", nil
}

func (a *CorelibPluginAdapter) Capabilities() CapabilityDeclaration {
	caps := a.plugin.Capabilities()
	return CapabilityDeclaration{
		SupportsRichCard:    caps.SupportsRichCard,
		SupportsMarkdown:    caps.SupportsMarkdown,
		SupportsImage:       caps.SupportsImage,
		SupportsFile:        caps.SupportsFile,
		SupportsVoice:       corelibSupportsNativeVoice(a.plugin),
		SupportsButton:      false,
		SupportsMessageEdit: false,
		MaxTextLength:       caps.MaxTextLength,
	}
}

func corelibSupportsNativeVoice(plugin cim.Plugin) bool {
	_, ok := plugin.(corelibAudioSender)
	return ok
}

func (a *CorelibPluginAdapter) Start(ctx context.Context) error {
	return a.plugin.Start(ctx)
}

func (a *CorelibPluginAdapter) Stop(ctx context.Context) error {
	return a.plugin.Stop(ctx)
}

// corelibToHubIncoming converts a corelib IncomingMessage to hub's IncomingMessage.
func corelibToHubIncoming(msg cim.IncomingMessage) IncomingMessage {
	hubMsg := IncomingMessage{
		PlatformName: msg.Platform,
		PlatformUID:  msg.PlatformUID,
		MessageID:    msg.MessageID,
		MessageType:  msg.MessageType,
		Text:         msg.Text,
		Lang:         msg.Lang,
		RawPayload:   msg.RawPayload,
		Timestamp:    msg.Timestamp,
	}
	for _, att := range msg.Attachments {
		hubMsg.Attachments = append(hubMsg.Attachments, MessageAttachment{
			Type:          att.Type,
			FileName:      att.FileName,
			MimeType:      att.MimeType,
			Data:          att.Data,
			Size:          att.Size,
			SourceMediaID: att.SourceMediaID,
		})
	}
	return hubMsg
}

// hubToCorelibIncoming converts hub's IncomingMessage to corelib format.
func hubToCorelibIncoming(msg IncomingMessage) cim.IncomingMessage {
	cMsg := cim.IncomingMessage{
		Platform:    msg.PlatformName,
		PlatformUID: msg.PlatformUID,
		MessageID:   msg.MessageID,
		MessageType: msg.MessageType,
		Text:        msg.Text,
		Lang:        msg.Lang,
		RawPayload:  msg.RawPayload,
		Timestamp:   msg.Timestamp,
	}
	for _, att := range msg.Attachments {
		cMsg.Attachments = append(cMsg.Attachments, cim.Attachment{
			Type:          att.Type,
			FileName:      att.FileName,
			MimeType:      att.MimeType,
			Data:          att.Data,
			Size:          att.Size,
			SourceMediaID: att.SourceMediaID,
		})
	}
	return cMsg
}
