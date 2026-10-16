package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestMobileTaskStatusProjection(t *testing.T) {
	digital := &mobileDigitalEmployeeTask{Status: "completed"}
	normalizeMobileDigitalEmployeeTaskStatus(digital)
	if digital.RuntimeStatus != agentruntime.JobStatusSucceeded {
		t.Fatalf("digital runtime status=%q", digital.RuntimeStatus)
	}
	document := &mobileDocumentUploadTask{Status: "needs_ocr"}
	normalizeMobileDocumentUploadTaskStatus(document)
	if document.RuntimeStatus != agentruntime.JobStatusPending {
		t.Fatalf("document runtime status=%q, want pending", document.RuntimeStatus)
	}
}
