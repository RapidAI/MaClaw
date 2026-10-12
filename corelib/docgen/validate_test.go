package docgen

import (
	"strings"
	"testing"
	"time"
)

// These tests pin the content bounds where they actually take effect.
// Validation used to live only in swarm, leaving direct callers of
// GenerateToFile (the desktop companion export) unbounded; it now runs inside
// Generate itself so every caller is covered.

func TestValidateContentRejectsEmpty(t *testing.T) {
	for _, content := range []string{"", "   \n\t "} {
		if err := ValidateContent(content); err == nil {
			t.Fatalf("ValidateContent(%q) should reject empty content", content)
		}
	}
}

func TestValidateContentRejectsOversizedContent(t *testing.T) {
	maxContent, _, _ := Limits()
	// Many well-formed paragraphs: long enough to trip the total size limit
	// without tripping the per-paragraph or base64 checks first.
	unit := "这是一段正常的正文内容。\n\n"
	oversized := strings.Repeat(unit, maxContent/len(unit)+2)
	if err := ValidateContent(oversized); err == nil {
		t.Fatal("oversized content must be rejected")
	} else if !strings.Contains(err.Error(), "内容过长") {
		t.Fatalf("expected the total-size error, got %v", err)
	}
	atLimit := strings.Repeat(unit, (maxContent-256)/len(unit))
	if len(atLimit) > maxContent {
		t.Fatalf("fixture is not under the limit: %d", len(atLimit))
	}
	if err := ValidateContent(atLimit); err != nil {
		t.Fatalf("content just under the limit must be accepted: %v", err)
	}
}

func TestValidateContentRejectsOversizedParagraph(t *testing.T) {
	_, maxParagraph, _ := Limits()
	// One never-wrapping block: no blank line anywhere.
	if err := ValidateContent(strings.Repeat("段", maxParagraph+1)); err == nil {
		t.Fatal("a single oversized paragraph must be rejected")
	}
}

func TestValidateContentRejectsFilePayload(t *testing.T) {
	content := "# 报告\n\n[file_base64|report.pdf|application/pdf]AAAA"
	if err := ValidateContent(content); err == nil {
		t.Fatal("file payload marker must be rejected")
	}
}

func TestValidateContentRejectsSuspiciousBase64Run(t *testing.T) {
	_, _, maxB64Run := Limits()
	if err := ValidateContent("# 报告\n\n" + strings.Repeat("A", maxB64Run)); err == nil {
		t.Fatal("long base64 run must be rejected")
	}
}

// TestGenerateRejectsUnboundedContent is the regression test for the gap
// itself: Generate must refuse content the old swarm-only check would have
// let through when called directly.
func TestGenerateRejectsUnboundedContent(t *testing.T) {
	g := New()
	if !g.HasFont() {
		t.Skip("no usable system font on this machine")
	}
	maxContent, _, _ := Limits()
	_, err := g.Generate(Spec{
		Title:     "过大",
		Content:   strings.Repeat("中文正文。\n\n", maxContent/12+2),
		Timestamp: time.Now(),
	})
	if err == nil {
		t.Fatal("Generate must reject content past the limit even though no caller pre-validated it")
	}
}

// TestValidateContentAcceptsRealisticDocument guards against the limits being
// tightened to the point of rejecting ordinary companion replies.
func TestValidateContentAcceptsRealisticDocument(t *testing.T) {
	var b strings.Builder
	b.WriteString("# 论文解读\n\n")
	for i := 0; i < 60; i++ {
		b.WriteString("## 小节标题\n\n")
		b.WriteString("这是一段正常的正文，包含标点与换行。\n\n")
	}
	if err := ValidateContent(b.String()); err != nil {
		t.Fatalf("a normal document must pass: %v", err)
	}
}
