package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestTargetedFileFromThirdPartyUsesStructuredSender(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	h := &IMMessageHandler{}
	var got agent.IMFileDeliveryRequest
	h.SetStructuredIMFileSender(func(req agent.IMFileDeliveryRequest) error {
		got = req
		return nil
	})
	resp := &IMAgentResponse{}
	count := h.populateFileArtifactResponse(resp, []pendingFile{{
		name: "report.pdf", mimeType: "application/pdf", data: "ZGF0YQ==", forwardIM: true,
		target: agent.IMFileDeliveryTarget{Channel: "lansenger", GroupID: "g-9"},
	}}, "thirdparty:echoear")
	if count != 1 {
		t.Fatalf("forward count=%d, text=%q", count, resp.Text)
	}
	if got.Target.Channel != "lansenger" || got.Target.GroupID != "g-9" || got.FileName != "report.pdf" {
		t.Fatalf("sender request=%#v", got)
	}
	if len(resp.LocalFilePaths) != 0 || resp.LocalFilePath != "" {
		t.Fatalf("exact target must not also attach to hardware origin: %+v", resp)
	}
}

func TestUntargetedFileFromThirdPartyStaysOnOriginGateway(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	h := &IMMessageHandler{}
	calls := 0
	h.SetStructuredIMFileSender(func(agent.IMFileDeliveryRequest) error {
		calls++
		return nil
	})
	resp := &IMAgentResponse{}
	count := h.populateFileArtifactResponse(resp, []pendingFile{{
		name: "report.pdf", mimeType: "application/pdf", data: "ZGF0YQ==", forwardIM: true,
	}}, "thirdparty:echoear")
	if count != 0 || calls != 0 {
		t.Fatalf("untargeted origin delivery count=%d senderCalls=%d", count, calls)
	}
	if len(resp.LocalFilePaths) != 1 {
		t.Fatalf("untargeted file must remain attached to hardware reply: %+v", resp)
	}
}

func TestHandleAgentLoopTargetedFileFromThirdPartyUsesOnlyStructuredSender(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	calls := 0
	h.SetStructuredIMFileSender(func(req agent.IMFileDeliveryRequest) error {
		calls++
		if req.Target.Channel != "lansenger" || req.Target.GroupID != "g-9" {
			t.Fatalf("unexpected target: %#v", req.Target)
		}
		return nil
	})
	result := h.handleAgentLoopFileArtifacts(
		"thirdparty:echoear:default",
		"thirdparty:echoear",
		[]pendingFile{{
			name: "report.pdf", mimeType: "application/pdf", data: "ZGF0YQ==", forwardIM: true,
			target: agent.IMFileDeliveryTarget{Channel: "lansenger", GroupID: "g-9"},
		}},
		"", "", "", nil, true, func(*IMAgentResponse) {},
	)
	if calls != 1 {
		t.Fatalf("structured sender calls=%d, want 1", calls)
	}
	if result.Response == nil {
		t.Fatal("expected response")
	}
	if result.Response.FileData != "" || len(result.Response.LocalFilePaths) != 0 || result.Response.LocalFilePath != "" {
		t.Fatalf("targeted file leaked back to hardware origin: %+v", result.Response)
	}
	if result.Response.ResponseSource != imResponseSourceFileDelivery.String() {
		t.Fatalf("response source=%q", result.Response.ResponseSource)
	}
}

func TestSharedLoopMaterializesThirdPartyExactTargetWithoutOriginAttachment(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	h := &IMMessageHandler{}
	calls := 0
	h.SetStructuredIMFileSender(func(req agent.IMFileDeliveryRequest) error {
		calls++
		if req.FileName != "report.pdf" || req.Target.Channel != "lansenger" || req.Target.GroupID != "g-9" {
			t.Fatalf("request=%#v", req)
		}
		return nil
	})
	flag := agent.EncodeIMFileDeliveryTargetFlag(map[string]interface{}{
		"channel": "lansenger", "group_id": "g-9",
	})
	result := h.materializeToolFilePayloadForPlatform(
		"[file_base64|report.pdf|application/pdf|im|"+flag+"]ZGF0YQ==",
		"thirdparty:echoear",
	)
	if !result.Handled || !result.Forwarded || calls != 1 {
		t.Fatalf("result=%+v senderCalls=%d", result, calls)
	}
	if len(result.LocalPaths) != 0 {
		t.Fatalf("exact target must not be attached to origin: %v", result.LocalPaths)
	}
	if strings.Contains(result.Text, "send_to_im") {
		t.Fatalf("success text must not claim only a local artifact: %q", result.Text)
	}
}
