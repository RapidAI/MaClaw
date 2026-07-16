package computeruse

import "testing"

func TestShouldActivate(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"@computer open notepad", true},
		{"帮我在桌面操作记事本输入 hello", true},
		{"点击屏幕上的保存按钮", true},
		{"用 computer use 测一下计算器", true},
		{"open notepad and type hello", true},
		{"写一个排序算法", false},
		{"在浏览器里点击购买", false},
		{"打开网页 https://example.com", false},
		{"", false},
	}
	for _, c := range cases {
		if got := ShouldActivate(c.in); got != c.want {
			t.Errorf("ShouldActivate(%q)=%v want %v", c.in, got, c.want)
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
