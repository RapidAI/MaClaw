package im

import "testing"

func TestParseUnderstandingResult_Normal(t *testing.T) {
	input := `{
		"intent": {
			"category": "coding",
			"summary": "开发一个CRM系统",
			"goals": ["客户管理", "销售跟踪"],
			"constraints": ["使用Go语言"],
			"open_questions": ["目标用户是谁？"],
			"confidence": 0.6,
			"ready": false
		},
		"reply": "我理解您想开发一个CRM系统。请问目标用户是谁？",
		"ready": false
	}`

	intent, reply, ready, err := parseUnderstandingResult(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if intent.Category != WorkflowCoding {
		t.Errorf("category = %q, want %q", intent.Category, WorkflowCoding)
	}
	if intent.Summary != "开发一个CRM系统" {
		t.Errorf("summary = %q, want %q", intent.Summary, "开发一个CRM系统")
	}
	if reply == "" {
		t.Error("reply is empty")
	}
	if ready {
		t.Error("ready should be false")
	}
}

func TestParseUnderstandingResult_MarkdownFenced(t *testing.T) {
	input := "```json\n" + `{
		"intent": {
			"category": "product_design",
			"summary": "设计一个项目管理工具",
			"goals": [],
			"constraints": [],
			"open_questions": [],
			"confidence": 0.8,
			"ready": true
		},
		"reply": "好的，开始工作。",
		"ready": true
	}` + "\n```"

	intent, _, ready, err := parseUnderstandingResult(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if intent.Category != WorkflowProductDesign {
		t.Errorf("category = %q, want %q", intent.Category, WorkflowProductDesign)
	}
	if !ready {
		t.Error("ready should be true")
	}
}

func TestParseUnderstandingResult_Invalid(t *testing.T) {
	_, _, _, err := parseUnderstandingResult("not json at all")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestBuildUnderstandingPrompt(t *testing.T) {
	session := &UnderstandingSession{
		Rounds: []UnderstandingRound{
			{UserText: "帮我做个CRM", AssistantText: "好的，请问..."},
		},
	}
	messages := buildUnderstandingPrompt(session, "用Go语言")

	// Should have: system + round user + round assistant + new user = 4 messages
	if len(messages) != 4 {
		t.Errorf("messages count = %d, want 4", len(messages))
	}
}

func TestDetectOffTopic(t *testing.T) {
	tests := []struct {
		workflowType string
		text         string
		want         OffTopicResult
	}{
		// On-topic: short messages
		{"coding", "好", OnTopic},
		{"coding", "ok", OnTopic},
		// On-topic: workflow interaction patterns
		{"coding", "下一步", OnTopic},
		{"coding", "确认", OnTopic},
		{"coding", "跳过", OnTopic},
		{"coding", "取消", OnTopic},
		{"coding", "改一下接口设计", OnTopic},
		// On-topic: workflow-related keywords
		{"coding", "这个API接口需要修改", OnTopic},
		{"product_design", "用户体验需要改进", OnTopic},
		// Off-topic simple
		{"coding", "今天天气怎么样", OffTopicSimple},
		{"coding", "现在几点了", OffTopicSimple},
		// Off-topic complex
		{"coding", "帮我做另一个完全不同的项目吧", OffTopicComplex},
	}

	for _, tt := range tests {
		got := detectOffTopic(tt.workflowType, tt.text)
		if got != tt.want {
			t.Errorf("detectOffTopic(%q, %q) = %d, want %d", tt.workflowType, tt.text, got, tt.want)
		}
	}
}
