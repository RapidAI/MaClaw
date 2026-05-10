package agent

import (
	"strings"
	"testing"
)

func TestInferFileDeliveryMessageUsesStructuredDocType(t *testing.T) {
	generic := InferFileDeliveryMessage("requirements_design_tasks.pdf")
	if strings.Contains(generic, "需求文档") || strings.Contains(generic, "技术设计") || strings.Contains(generic, "任务列表") {
		t.Fatalf("InferFileDeliveryMessage inferred workflow type from file name: %q", generic)
	}

	requirements := InferFileDeliveryMessageForDocType("requirements", "anything.pdf")
	if !strings.Contains(requirements, "需求文档") {
		t.Fatalf("requirements message = %q, want requirements prompt", requirements)
	}

	tasks := InferFileDeliveryMessageForDocType("task_plan", "anything.pdf")
	if !strings.Contains(tasks, "任务列表") {
		t.Fatalf("task_plan message = %q, want task-list prompt", tasks)
	}
}
