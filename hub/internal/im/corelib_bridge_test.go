package im

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	cim "github.com/RapidAI/CodeClaw/corelib/im"
)

type stubCorelibPlugin struct {
	name    string
	handler cim.MessageHandler
}

type stubAudioCorelibPlugin struct {
	stubCorelibPlugin
	audioData []byte
	audioUID  string
}

func (s *stubAudioCorelibPlugin) SendAudio(ctx context.Context, target cim.UserTarget, audioData []byte, durationMs int) error {
	s.audioUID = target.PlatformUID
	s.audioData = append([]byte(nil), audioData...)
	return nil
}

func (s *stubCorelibPlugin) Name() string                         { return s.name }
func (s *stubCorelibPlugin) Start(ctx context.Context) error      { return nil }
func (s *stubCorelibPlugin) Stop(ctx context.Context) error       { return nil }
func (s *stubCorelibPlugin) OnMessage(handler cim.MessageHandler) { s.handler = handler }
func (s *stubCorelibPlugin) SendText(ctx context.Context, target cim.UserTarget, text string) error {
	return nil
}
func (s *stubCorelibPlugin) SendMarkdown(ctx context.Context, target cim.UserTarget, md string) error {
	return nil
}
func (s *stubCorelibPlugin) Capabilities() cim.Capabilities {
	return cim.Capabilities{SupportsMarkdown: true, MaxTextLength: 4096}
}

func TestCorelibPluginAdapter_Name(t *testing.T) {
	p := &stubCorelibPlugin{name: "test-gw"}
	adapter := NewCorelibPluginAdapter(p, nil)
	if adapter.Name() != "test-gw" {
		t.Errorf("expected test-gw, got %s", adapter.Name())
	}
}

func TestCorelibPluginAdapter_ReceiveMessage(t *testing.T) {
	p := &stubCorelibPlugin{name: "feishu"}
	adapter := NewCorelibPluginAdapter(p, nil)

	var received IncomingMessage
	adapter.ReceiveMessage(func(msg IncomingMessage) {
		received = msg
	})

	// Simulate corelib plugin receiving a message
	p.handler(cim.IncomingMessage{
		Platform:    "feishu",
		PlatformUID: "ou_123",
		Text:        "hello from feishu",
		MessageType: "text",
		Timestamp:   time.Now(),
	})

	if received.PlatformName != "feishu" {
		t.Errorf("expected feishu, got %s", received.PlatformName)
	}
	if received.Text != "hello from feishu" {
		t.Errorf("expected 'hello from feishu', got %q", received.Text)
	}
	if received.PlatformUID != "ou_123" {
		t.Errorf("expected ou_123, got %s", received.PlatformUID)
	}
}

func TestCorelibPluginAdapter_Capabilities(t *testing.T) {
	p := &stubCorelibPlugin{name: "dingtalk"}
	adapter := NewCorelibPluginAdapter(p, nil)
	caps := adapter.Capabilities()
	if !caps.SupportsMarkdown {
		t.Error("expected markdown support")
	}
	if caps.MaxTextLength != 4096 {
		t.Errorf("expected 4096, got %d", caps.MaxTextLength)
	}
}

func TestCorelibPluginAdapter_VoiceCapabilityRequiresSendAudio(t *testing.T) {
	plain := NewCorelibPluginAdapter(&stubCorelibPlugin{name: "feishu"}, nil)
	if plain.Capabilities().SupportsVoice {
		t.Fatal("plain corelib plugin SupportsVoice = true, want false")
	}

	audio := NewCorelibPluginAdapter(&stubAudioCorelibPlugin{stubCorelibPlugin: stubCorelibPlugin{name: "dingtalk"}}, nil)
	if !audio.Capabilities().SupportsVoice {
		t.Fatal("audio corelib plugin SupportsVoice = false, want true")
	}
}

func TestCorelibPluginAdapter_SendVoiceUsesSendAudio(t *testing.T) {
	plugin := &stubAudioCorelibPlugin{stubCorelibPlugin: stubCorelibPlugin{name: "dingtalk"}}
	adapter := NewCorelibPluginAdapter(plugin, nil)
	data := []byte("ogg voice")
	if err := adapter.SendVoice(context.Background(), UserTarget{PlatformUID: "staff-1"}, base64.StdEncoding.EncodeToString(data), "voice.ogg", "audio/ogg"); err != nil {
		t.Fatalf("SendVoice() error = %v", err)
	}
	if plugin.audioUID != "staff-1" || string(plugin.audioData) != string(data) {
		t.Fatalf("SendAudio target/data = %q/%q", plugin.audioUID, plugin.audioData)
	}
}

func TestCorelibPluginAdapter_SendText(t *testing.T) {
	p := &stubCorelibPlugin{name: "wecom"}
	adapter := NewCorelibPluginAdapter(p, nil)
	err := adapter.SendText(context.Background(), UserTarget{PlatformUID: "user1"}, "hi")
	if err != nil {
		t.Errorf("send text: %v", err)
	}
}

func TestCorelibPluginAdapter_ResolveUser(t *testing.T) {
	p := &stubCorelibPlugin{name: "feishu"}

	// Without resolver
	adapter := NewCorelibPluginAdapter(p, nil)
	uid, _ := adapter.ResolveUser(context.Background(), "ou_123")
	if uid != "" {
		t.Errorf("expected empty without resolver, got %s", uid)
	}

	// With resolver
	adapter2 := NewCorelibPluginAdapter(p, func(platformUID string) (string, error) {
		return "internal-" + platformUID, nil
	})
	uid, _ = adapter2.ResolveUser(context.Background(), "ou_123")
	if uid != "internal-ou_123" {
		t.Errorf("expected internal-ou_123, got %s", uid)
	}
}

func TestCorelibToHubIncoming_WithAttachments(t *testing.T) {
	msg := cim.IncomingMessage{
		Platform:    "dingtalk",
		PlatformUID: "staff_123",
		Text:        "see attached",
		MessageType: "file",
		Attachments: []cim.Attachment{
			{Type: "file", FileName: "report.pdf", MimeType: "application/pdf", Size: 1024},
		},
	}
	hub := corelibToHubIncoming(msg)
	if hub.PlatformName != "dingtalk" {
		t.Errorf("expected dingtalk, got %s", hub.PlatformName)
	}
	if len(hub.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(hub.Attachments))
	}
	if hub.Attachments[0].FileName != "report.pdf" {
		t.Errorf("expected report.pdf, got %s", hub.Attachments[0].FileName)
	}
}
