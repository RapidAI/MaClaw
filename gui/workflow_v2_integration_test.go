package main

import (
	"strings"
	"testing"
)

func TestInferExplicitWorkflowHint_Presentation(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{text: "generate a ppt about Beijing", want: "presentation_design"},
		{text: "build a slide deck for the meeting", want: "presentation_design"},
		{text: "make a PowerPoint for the launch review", want: "presentation_design"},
		{text: "生成一份ppt，介绍北京", want: "presentation_design"},
		{text: "制作演示文稿用于项目汇报", want: "presentation_design"},
		{text: "open the ppt file and take a screenshot", want: ""},
		{text: "read this powerpoint and summarize it", want: ""},
		{text: "打开桌面上的ppt文件并截图", want: ""},
		{text: "读取这个演示文稿然后总结", want: ""},
		{text: "design a product strategy", want: ""},
	}
	for _, tc := range tests {
		if got := inferExplicitWorkflowHint(tc.text); got != tc.want {
			t.Errorf("inferExplicitWorkflowHint(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}

func TestRouteWithWorkflowV2_PresentationCreationUsesPresentationWorkflow(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "test-presentation-explicit-hint"

	result := handler.routeWithWorkflowV2(IMUserMessage{
		UserID:   userID,
		Text:     "生成一份ppt，介绍北京。",
		Platform: "desktop",
	}, "生成一份ppt，介绍北京。")

	if result.Response == nil {
		t.Fatal("expected workflow confirmation response for explicit presentation request")
	}
	raw, ok := handler.pendingWorkflowChoice.Load(userID)
	if !ok {
		t.Fatal("expected pending workflow choice to be stored")
	}
	pending := raw.(*pendingWorkflowChoice)
	if pending.RouteResult == nil || pending.RouteResult.WorkflowType != "presentation_design" {
		t.Fatalf("workflow type = %#v, want presentation_design", pending.RouteResult)
	}
	if !strings.Contains(result.Response.Text, "PPT") {
		t.Fatalf("response text should mention presentation workflow, got %q", result.Response.Text)
	}
}

func TestRouteWithWorkflowV2_PresentationReadDoesNotStartWorkflow(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	cases := []struct {
		userID string
		text   string
	}{
		{userID: "test-presentation-read-no-workflow-en", text: "open the ppt file and take a screenshot"},
		{userID: "test-presentation-read-no-workflow-zh", text: "打开桌面上的ppt文件并截图"},
	}

	for _, tc := range cases {
		result := handler.routeWithWorkflowV2(IMUserMessage{
			UserID:   tc.userID,
			Text:     tc.text,
			Platform: "desktop",
		}, tc.text)

		if result.Response != nil || result.WorkflowAgentLoop || result.WorkflowDocPhase || result.SkipNeedsConfirmGate {
			t.Fatalf("expected plain pass-through for non-creation ppt request %q, got %#v", tc.text, result)
		}
		if _, ok := handler.pendingWorkflowChoice.Load(tc.userID); ok {
			t.Fatalf("did not expect pending workflow choice for non-creation ppt request %q", tc.text)
		}
	}
}
