package botmgmt

import "testing"

func TestDelegateGuardrailWordsDoNotHoldTheTask(t *testing.T) {
	for _, s := range []string{
		"Write /tmp/a.txt, then reply. Do not post, publish, browse or delete anything.",
		"整理一下文件，不要删除任何东西。",
	} {
		if dispatchDestructive(s) {
			t.Fatalf("held a harmless task: %q", s)
		}
	}
	for _, s := range []string{"Delete the old drafts.", "Do not wait. Uninstall the app.", "删除旧草稿", "安装 vscode"} {
		if !dispatchDestructive(s) {
			t.Fatalf("missed a destructive task: %q", s)
		}
	}
}
