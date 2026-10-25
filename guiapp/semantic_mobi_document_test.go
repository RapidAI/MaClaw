package guiapp

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestIMSemanticDesktopPickerMOBIReadsNatively(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	dir := filepath.Join(t.TempDir(), "Downloads")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "book.mobi")
	if err := os.WriteFile(path, desktopTestMOBI("泵手册", "<p>叶轮间隙为 0.2 mm。</p>"), 0o600); err != nil {
		t.Fatal(err)
	}
	turn := "书中讲什么？\n\n" + filePathPromptPrefix + "\n" + path + "\n"
	if err := h.captureSelectedLocalDocuments("desktop-user", "desktop", "", turn); err != nil {
		t.Fatalf("capture mobi: %v", err)
	}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(
		"desktop-user", turn, "desktop", "root-mobi", "turn-mobi",
		&intent.ClassificationResult{Primary: intent.LabelDocumentRead, Confidence: .98}, nil,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v surface=%v err=%v", handled, surface != nil, err)
	}
	found := false
	var selection tool.PlannedSelection
	for _, sel := range surface.plan.Selections {
		if sel.AdapterName == "semantic_read_trusted_document" && sel.FitProof.QualifierBindings["format"] == "mobi" {
			selection = sel
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("mobi read missing: %+v", surface.plan.Selections)
	}
	if got := tool.DocumentTempSuffixForSelection(selection); got != ".mobi" {
		t.Fatalf("temp suffix=%q", got)
	}
	name := tool.LiveGrantNameForCapability(surface.plan, surface.grants, "document.read.local")
	if name == "" {
		t.Fatal("missing live document.read grant")
	}
	got := (&sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}).ExecuteTool(name, `{}`)
	if strings.Contains(got, "trusted_document_read_failed") || strings.Contains(got, "重新选择") {
		t.Fatalf("native mobi read rejected: %q", got)
	}
	if !strings.Contains(got, "叶轮间隙为 0.2 mm。") || !strings.Contains(got, "泵手册") {
		t.Fatalf("mobi body=%q", got)
	}
}

func TestIMSemanticDesktopPickerMOBISampleWhenPresent(t *testing.T) {
	path := strings.TrimSpace(os.Getenv("MACLAW_MOBI_SAMPLE"))
	if path == "" {
		t.Skip("MACLAW_MOBI_SAMPLE not set")
	}
	h := &IMMessageHandler{registry: NewToolRegistry()}
	turn := "书中讲什么？\n\n" + filePathPromptPrefix + "\n" + path + "\n"
	if err := h.captureSelectedLocalDocuments("desktop-user", "desktop", "", turn); err != nil {
		t.Fatalf("capture mobi: %v", err)
	}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(
		"desktop-user", turn, "desktop", "root-mobi-sample", "turn-mobi-sample",
		&intent.ClassificationResult{Primary: intent.LabelDocumentRead, Confidence: .98}, nil,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v surface=%v err=%v", handled, surface != nil, err)
	}
	name := tool.LiveGrantNameForCapability(surface.plan, surface.grants, "document.read.local")
	if name == "" {
		t.Fatalf("missing live document.read grant: %+v", surface.plan.Selections)
	}
	var format string
	for _, sel := range surface.plan.Selections {
		if sel.AdapterName == "semantic_read_trusted_document" {
			format = sel.FitProof.QualifierBindings["format"]
			if got := tool.DocumentTempSuffixForSelection(sel); got != ".mobi" {
				t.Fatalf("temp suffix=%q", got)
			}
		}
	}
	if format != "mobi" {
		t.Fatalf("format=%q selections=%+v", format, surface.plan.Selections)
	}
	got := (&sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}).ExecuteTool(name, `{}`)
	if strings.Contains(got, "trusted_document_read_failed") || strings.Contains(got, "重新选择") {
		head := got
		if len(head) > 180 {
			head = head[:180]
		}
		t.Fatalf("native mobi read rejected: %q", head)
	}
	if !strings.Contains(got, "Robot") && !strings.Contains(got, "robot") && !strings.Contains(got, "Love") {
		t.Fatalf("sample read len=%d", len(got))
	}
}

func desktopTestMOBI(title, html string) []byte {
	titleBytes := []byte(title)
	body := []byte(html)
	headerLen := 0x80
	rec0 := make([]byte, 16+headerLen+len(titleBytes))
	binary.BigEndian.PutUint16(rec0[0:2], 1)
	binary.BigEndian.PutUint32(rec0[4:8], uint32(len(body)))
	binary.BigEndian.PutUint16(rec0[8:10], 1)
	binary.BigEndian.PutUint16(rec0[10:12], 4096)
	copy(rec0[16:20], "MOBI")
	binary.BigEndian.PutUint32(rec0[20:24], uint32(headerLen))
	binary.BigEndian.PutUint32(rec0[24:28], 2)
	binary.BigEndian.PutUint32(rec0[28:32], 65001)
	titleAt := 16 + headerLen
	binary.BigEndian.PutUint32(rec0[0x54:0x58], uint32(titleAt))
	binary.BigEndian.PutUint32(rec0[0x58:0x5C], uint32(len(titleBytes)))
	copy(rec0[titleAt:], titleBytes)

	pdbHead := 78 + 2*8
	rec0At := pdbHead
	rec1At := rec0At + len(rec0)
	out := make([]byte, rec1At+len(body))
	copy(out[0:32], []byte("Manual"))
	copy(out[60:68], "BOOKMOBI")
	binary.BigEndian.PutUint16(out[76:78], 2)
	binary.BigEndian.PutUint32(out[78:82], uint32(rec0At))
	binary.BigEndian.PutUint32(out[86:90], uint32(rec1At))
	copy(out[rec0At:], rec0)
	copy(out[rec1At:], body)
	return out
}
