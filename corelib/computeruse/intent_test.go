package computeruse

import "testing"

func TestHasExplicitTrigger(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"@computer open notepad", true},
		{"用 computer use 测一下计算器", true},
		{"try computer_use mode", true},
		{"computer-use please", true},
		// Semantic desktop phrasing is NOT an explicit trigger — activation is
		// decided by the unified intent classifier (corelib/intent).
		{"打开word程序写一份简历", false},
		{"点击屏幕上的保存按钮", false},
		{"open notepad and type hello", false},
		{"", false},
	}
	for _, c := range cases {
		if got := HasExplicitTrigger(c.in); got != c.want {
			t.Errorf("HasExplicitTrigger(%q)=%v want %v", c.in, got, c.want)
		}
	}
}

func TestIsComputerUseTool(t *testing.T) {
	if !IsComputerUseTool("computer_observe") {
		t.Fatal("expected computer_observe")
	}
	if IsComputerUseTool("bash") {
		t.Fatal("bash is not CU")
	}
}
