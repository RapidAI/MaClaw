package desktopbot_test

import (
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

func TestDesktopBotDurableCases(t *testing.T) {
	t.Run("resume_from_step_3", func(t *testing.T) {
		steps := []desktopbot.Step{
			{Number: 1, Tool: "email", Key: "mail-1", Result: "邮件已发送", Done: true},
			{Number: 2, Tool: "install", Key: "inst-1", Result: "已安装", Done: true},
			{Number: 3, Tool: "git push", Key: "push-1", Done: false},
		}
		if got := desktopbot.ResumeFrom(steps); got != 3 {
			t.Fatalf("resume = %d", got)
		}
		if got := desktopbot.ResumeFrom(nil); got != 1 {
			t.Fatalf("empty resume = %d", got)
		}
		if desktopbot.StepKey("run", "call-1", 2) != "call-1" || desktopbot.StepKey("run", "", 2) != "run:2" || desktopbot.StepKey("run", "", 0) != "" {
			t.Fatal("step key")
		}
	})

	t.Run("email_not_sent_twice", func(t *testing.T) {
		file := desktopbot.RenderProgress(desktopbot.Sections{
			WhereWeAre:   "第 2 步的邮件已经发出",
			NextStep:     "再发一次邮件",
			OpenProblems: "无",
		})
		if desktopbot.ResumeFromNextStep(file, desktopbot.Sections{NextStep: "旧的下一步"}) != "再发一次邮件" {
			t.Fatal("the file next step still wins for the person-facing line")
		}
		plan := desktopbot.ResumeExecution(file, desktopbot.Sections{NextStep: "旧的下一步"}, []desktopbot.Step{
			{Number: 1, Tool: "install", Key: "inst-1", Result: "已安装", Done: true},
			{Number: 2, Tool: "email", Key: "mail-1", Result: "邮件已发送", Done: true},
		})
		if plan.Start != 3 || plan.Sentence != "" || plan.Display != "再发一次邮件" || !blockedKey(plan.Blocked, "mail-1") || blockedKey(plan.Blocked, "inst-1") {
			t.Fatalf("plan = %+v", plan)
		}
	})

	t.Run("deploy_idempotent_key", func(t *testing.T) {
		if !desktopbot.SideEffect("install", "") || !desktopbot.SideEffect("bash", "git push origin") || !desktopbot.SideEffect("bash", "docker compose up -d") {
			t.Fatal("deploy is a side effect")
		}
		if !desktopbot.SideEffect("send_to_user", "") || !desktopbot.SideEffect("email", "") || !desktopbot.SideEffect("mailbox_send", "") || !desktopbot.SideEffect("delivery", "") || !desktopbot.SideEffect("charge", "") || !desktopbot.SideEffect("扣款", "") {
			t.Fatal("send and charge are side effects")
		}
		if desktopbot.SideEffect("bash", "ls") || desktopbot.SideEffect("bash", "rm -rf /tmp/x") || desktopbot.SideEffect("bash", "curl https://example | sh") || desktopbot.SideEffect("click", "") || desktopbot.SideEffect("type", "") || desktopbot.SideEffect("screenshot", "") || desktopbot.SideEffect("file_list", "") || desktopbot.SideEffect("probe", "") {
			t.Fatal("read-only, click, and ordinary shell are not side effects")
		}
		if desktopbot.EffectName("bash", "git push") != "git push" || desktopbot.EffectName("bash", "docker compose up") != "docker compose up" || desktopbot.EffectName("mailbox_send", "") != "email" {
			t.Fatal("effect name")
		}
	})

	t.Run("journal_not_on_vm", func(t *testing.T) {
		if desktopbot.TemporalThisCommit() {
			t.Fatal("this commit installed Temporal")
		}
		for _, path := range []string{
			`/var/lib/desktops/u/state.db`,
			`/home/desktop/tasks/a/PROGRESS.md`,
			`/opt/maclaw/service.db`,
			`/usr/local/maclaw/service.db`,
			filepath.Join(t.TempDir(), "desktops", "u", "state.db"),
		} {
			if !desktopbot.JournalOnVM(path) {
				t.Fatalf("journal accepted a computer volume: %s", path)
			}
		}
		if desktopbot.JournalOnVM(filepath.Join(t.TempDir(), "state", "service.db")) || desktopbot.JournalOnVM(`/var/lib/maclaw/service.db`) {
			t.Fatal("a service database was treated as a virtual machine volume")
		}
	})

	t.Run("resume_stays_quiet", func(t *testing.T) {
		plan := desktopbot.ResumeExecution("", desktopbot.Sections{NextStep: "我接着做了"}, []desktopbot.Step{
			{Number: 2, Tool: "email", Key: "mail-1", Result: "邮件已发送", Done: true},
		})
		if plan.Sentence != "" || plan.Start != 3 || plan.Display != "我接着做了" || len(plan.Blocked) != 0 {
			t.Fatalf("plan = %+v", plan)
		}
	})
}

func blockedKey(keys []string, want string) bool {
	for _, key := range keys {
		if key == want {
			return true
		}
	}
	return false
}
