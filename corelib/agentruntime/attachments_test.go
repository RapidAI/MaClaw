package agentruntime

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestAttachmentsWithinSharedLoopLimit(t *testing.T) {
	if !AttachmentsWithinRuntimeLimit(nil) {
		t.Fatal("empty attachment batch should be accepted")
	}
	if !AttachmentsWithinRuntimeLimit([]agent.MessageAttachment{{Size: SharedLoopMaxAttachmentBytes}}) {
		t.Fatal("batch at the byte limit should be accepted")
	}
	if AttachmentsWithinRuntimeLimit([]agent.MessageAttachment{{Size: SharedLoopMaxAttachmentBytes + 1}}) {
		t.Fatal("batch above the byte limit should be rejected")
	}
	tooMany := make([]agent.MessageAttachment, SharedLoopMaxAttachments+1)
	if AttachmentsWithinRuntimeLimit(tooMany) {
		t.Fatal("batch above the item limit should be rejected")
	}
	encoded := strings.Repeat("A", int(SharedLoopMaxAttachmentBytes*4/3)+4)
	if AttachmentsWithinRuntimeLimit([]agent.MessageAttachment{{Data: encoded}}) {
		t.Fatal("base64-estimated payload above the limit should be rejected")
	}
}
