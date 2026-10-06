package agentservice

import (
	"strings"
	"testing"
)

func TestDesktopLoginAnswerContinuesTheSameBrowser(t *testing.T) {
	sess := Session{Metadata: map[string]string{
		sessionMetaPendingAskUser:         "true",
		sessionMetaPendingAskUserQuestion: "这一步需要你在当前桌面的浏览器里完成登录或验证。登录状态会留在这个浏览器里，完成后这个 bot 会接着操作。",
	}}
	got, req := buildEffectiveUserContent(sess, "登录或验证已在当前桌面浏览器完成，请沿用这个登录状态继续。")
	if req == nil {
		t.Fatal("login answer started a new request")
	}
	if !strings.Contains(got, "current task") || !strings.Contains(got, "same browser") || !strings.Contains(got, "If this site opens a window") {
		t.Fatalf("continuation left the logged-in browser: %s", got)
	}
}

func TestAskUserAnswerStaysOnTheCurrentTask(t *testing.T) {
	sess := Session{Metadata: map[string]string{
		sessionMetaPendingAskUser:         "true",
		sessionMetaPendingAskUserQuestion: "Which folder?",
	}}
	got, req := buildEffectiveUserContent(sess, "docs")
	if req == nil || !strings.Contains(got, "current task") {
		t.Fatalf("answer was not kept on the current task: %s", got)
	}
	if strings.Contains(got, "same browser") {
		t.Fatal("an ordinary answer was sent to the desktop browser")
	}
}
