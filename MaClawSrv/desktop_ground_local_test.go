package main

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

func TestDesktopLocalGroundPicksLabel(t *testing.T) {
	lines := []taskengine.OCRResult{{Text: "发布设置", BBox: [4]int{180, 610, 60, 20}}, {Text: "发布", BBox: [4]int{780, 610, 40, 20}}}
	x, y, label, ok := desktopPickOCR(lines, desktopTargetLabels(`the blue "发布" button`))
	if !ok || label != "发布" || x != 800 || y != 620 {
		t.Fatalf("got %d,%d %q %v", x, y, label, ok)
	}
	if got := desktopTargetLabels("右下角的添加话题按钮"); len(got) == 0 || got[0] != "右下角的添加话题按钮" {
		t.Fatalf("cjk labels %v", got)
	}
}

func TestDesktopGroundRefusesPublishTarget(t *testing.T) {
	steps := []any{map[string]any{"action": "left_click", "target": "「发布」"}}
	if _, _, err := groundDesktopSteps(context.Background(), agentruntime.Scope{TenantID: "t", UserID: "u"}, ":1", steps); err == nil || desktopErrorClass("", err) != "publish_needs_confirmation" {
		t.Fatalf("want publish refusal, got %v", err)
	}
}
