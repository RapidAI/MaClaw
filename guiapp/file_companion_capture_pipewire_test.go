package guiapp

import "testing"

func TestPipeWireOutputNodeFiltersPID(t *testing.T) {
	raw := []byte(`[
		{"id": 7, "type": "PipeWire:Interface:Node", "info": {"props": {"application.process.id": 99, "media.class": "Stream/Output/Audio"}}},
		{"id": 8, "type": "PipeWire:Interface:Node", "info": {"props": {"application.process.id": "42", "media.class": "Audio/Source"}}},
		{"id": 9, "type": "PipeWire:Interface:Node", "info": {"props": {"application.process.id": 42, "media.class": "Stream/Output/Audio"}}},
		{"id": 10, "type": "PipeWire:Interface:Device", "info": {"props": {"application.process.id": 42}}}
	]`)
	if got := pipeWireOutputNode(raw, 42); got != "9" {
		t.Fatalf("node = %q, want the output node of this process", got)
	}
	if got := pipeWireOutputNode(raw, 100); got != "" {
		t.Fatalf("other pid node = %q, want none", got)
	}
	if got := pipeWireOutputNode([]byte(`not-json`), 42); got != "" {
		t.Fatalf("bad dump node = %q", got)
	}
}
