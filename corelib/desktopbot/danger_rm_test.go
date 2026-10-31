package desktopbot

import (
	"strings"
	"testing"
	"time"
)

// rm -rf of scratch paths in the bot's container runs; anything else asks,
// and the question names the paths in plain words.
func TestDangerRmScratchRunsAndAskNamesPaths(t *testing.T) {
	ResetDangerState()
	run := func(cmd string) DangerResult {
		return GateDanger(DangerInput{Call: DangerCall{Action: "bash", Command: cmd}, Key: cmd})
	}
	for _, cmd := range []string{"rm -rf /tmp/maclaw && git clone https://github.com/rapidai/maclaw /tmp/maclaw", "rm -rf ./build dist", "rm -rf ~/work/maclaw", "rm -rf /root/work/x /var/tmp/y/*", "cd /tmp && rm -rf maclaw"} {
		if r := run(cmd); !r.Run {
			t.Errorf("%q should run: %+v", cmd, r)
		}
	}
	for _, cmd := range []string{"rm -rf /", "rm -rf ~", "rm -rf /tmp/*", "rm -rf *", "rm -rf .", "rm -rf /etc/nginx", "rm -rf ~/.ssh", "rm -rf /tmp/../etc", "rm -rf $DIR", "rm -rf /root", "rm -rf /tmp/x /usr/lib"} {
		if r := run(cmd); r.Run {
			t.Errorf("%q must ask", cmd)
		}
	}
	block := GateDanger(DangerInput{Call: DangerCall{Action: "bash", Command: "rm -rf /tmp/a"}, Verdict: "block", VerdictOK: true, Key: "b"})
	if block.Run {
		t.Fatal("block verdict must hold")
	}
	ResetDangerState()
	r := run("rm -rf /etc/maclaw /srv/abcdefabcdefabcdefabcdefabcdef")
	if r.Ask.Text != "我想把 /etc/maclaw、/srv/… 整个删掉再继续，可以吗？" {
		t.Fatalf("ask %q", r.Ask.Text)
	}
	if strings.Contains(dangerAskText("这一条命令"), "「") || !strings.Contains(dangerAskText("git push"), "git push") {
		t.Fatal("generic asks")
	}
}

// 确认 runs the held command once; 拒绝 tells the model to go on without it;
// the ask carries the two buttons.
func TestDangerAnswerButtons(t *testing.T) {
	ResetDangerState()
	t0 := DangerNow()
	in := DangerInput{Call: DangerCall{Action: "bash", Command: "rm -rf /etc/x"}, Key: "t\x00u\x00i\x00bash\x00rm -rf /etc/x", Now: t0}
	if r := GateDanger(in); r.Run || r.Ask.Text == "" {
		t.Fatalf("first: %+v", r)
	}
	NoteDangerAsk([]string{"s"}, ConnectorAsk{Kind: "ask", Text: "我想把 /etc/x 整个删掉再继续，可以吗？"})
	meta := map[string]string{}
	ApplyDangerAsk(meta, "s")
	if meta["ask_user_options_json"] != `["确认","拒绝"]` || meta["ask_user_question"] == "" {
		t.Fatalf("meta %v", meta)
	}
	in.Now = t0.Add(3 * time.Minute) // a human click, well past 15 s
	if yes, ok := DangerAnswer("确认"); !ok || !yes || AnswerDanger("t\x00u\x00i\x00", true, in.Now) != 1 {
		t.Fatal("answer")
	}
	if r := GateDanger(in); !r.Run {
		t.Fatalf("approved must run: %+v", r)
	}
	if r := GateDanger(in); r.Run {
		t.Fatal("approval is one-shot")
	}
	AnswerDanger("t\x00u\x00i\x00", false, in.Now)
	if r := GateDanger(in); r.Run || r.Error != DangerDeclinedError {
		t.Fatalf("declined: %+v", r)
	}
	if _, ok := DangerAnswer("帮我删掉吧，顺便看看日志"); ok {
		t.Fatal("a sentence is not a bare answer")
	}
}
