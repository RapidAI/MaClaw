package agentruntime

import "testing"

func TestUserFacingTextStripsWorkingState(t *testing.T) {
	if got := UserFacingText("hello\n[任务状态]\ninternal"); got != "hello" {
		t.Fatalf("UserFacingText()=%q", got)
	}
}
