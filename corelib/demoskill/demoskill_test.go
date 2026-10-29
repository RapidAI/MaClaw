package demoskill

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func loadFixture(t *testing.T) Timeline {
	t.Helper()
	data, err := os.ReadFile("testdata/minimize_and_mail.json")
	if err != nil {
		t.Fatal(err)
	}
	var tl Timeline
	if err := json.Unmarshal(data, &tl); err != nil {
		t.Fatal(err)
	}
	return tl
}

const goodAnswer = "好的：\n```json\n" + `{"name":"日报草稿","goal":"把今天的日报存为发给老板的邮件草稿",
"preconditions":["浏览器已登录邮箱"],
"params":[{"name":"recipient","description":"收件人","example":"boss@example.com","required":true},{"name":"recipient","description":"dup"}],
"steps":[{"intent":"只保留邮箱浏览器窗口，最小化其他窗口","check":"屏幕上只有邮箱窗口","hint":"点任务栏"},
{"intent":"新建邮件，收件人填 recipient，标题为今天日期加日报","check":""},
{"intent":"  "},
{"intent":"保存草稿，不发送","check":"草稿箱里有这封邮件","risky":"bogus"}],
"deliverable":"草稿箱里有一封标题为当天日期+日报的邮件","evidence":"草稿箱截图"}` + "\n```"

func TestEnabledDefaultOff(t *testing.T) {
	t.Setenv(EnvFlag, "")
	if Enabled() {
		t.Fatal("must be off by default")
	}
	t.Setenv(EnvFlag, "on")
	if !Enabled() {
		t.Fatal("on should enable")
	}
}

func TestCompactDropsNoiseKeepsNarration(t *testing.T) {
	evs := Compact(loadFixture(t))
	var nar, focus, inputs, shots int
	for _, e := range evs {
		switch e.Kind {
		case KindNarration:
			nar++
		case KindFocus:
			focus++
		case KindInput:
			inputs++
		case KindScreenshot:
			shots++
		}
	}
	if nar != 3 || focus != 1 || inputs != 3 || shots != 1 {
		t.Fatalf("narr=%d focus=%d input=%d shot=%d", nar, focus, inputs, shots)
	}
}

func TestBuildPromptHasNarrationNoCoordinates(t *testing.T) {
	p := BuildPrompt(loadFixture(t))
	if !strings.Contains(p, "讲解: 保存草稿就行，不要发送") || strings.Contains(p, "700") {
		t.Fatalf("prompt:\n%s", p)
	}
}

func TestDistillRepairsOnceThenNormalizes(t *testing.T) {
	calls := 0
	llm := func(ctx context.Context, sys, user string) (string, error) {
		calls++
		if !strings.Contains(sys, "持续推进任务") {
			t.Fatal("system prompt lost the first principle")
		}
		if calls == 1 {
			return "not json", nil
		}
		if !strings.Contains(user, "上次输出不可用") {
			t.Fatal("repair prompt missing")
		}
		return goodAnswer, nil
	}
	sk, err := Distill(context.Background(), llm, loadFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || sk.SourceDemo != "demo_fixture_1" {
		t.Fatalf("calls=%d src=%q", calls, sk.SourceDemo)
	}
	if len(sk.Steps) != 3 || len(sk.Params) != 1 {
		t.Fatalf("steps=%d params=%d", len(sk.Steps), len(sk.Params))
	}
	if sk.Steps[1].Check == "" || sk.Steps[2].Risky != "" {
		t.Fatalf("normalize: %+v", sk.Steps)
	}
}

func TestDistillErrors(t *testing.T) {
	if _, err := Distill(context.Background(), nil, loadFixture(t)); err == nil {
		t.Fatal("nil llm")
	}
	if _, err := Distill(context.Background(), func(context.Context, string, string) (string, error) { return "", nil }, Timeline{}); err == nil {
		t.Fatal("empty timeline")
	}
	boom := errors.New("boom")
	if _, err := Distill(context.Background(), func(context.Context, string, string) (string, error) { return "", boom }, loadFixture(t)); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if _, err := ParseSkill(`{"goal":"g","steps":[{"intent":"a"}]}`); err == nil {
		t.Fatal("missing deliverable must fail")
	}
}

func TestGuidanceAndEntry(t *testing.T) {
	sk, err := ParseSkill(goodAnswer)
	if err != nil {
		t.Fatal(err)
	}
	g := sk.Guidance(map[string]string{"recipient": "a@b.c"})
	for _, want := range []string{"持续推进任务", "不是坐标回放", "重新规划", "recipient = a@b.c", "检查：屏幕上只有邮箱窗口", "完成证据：草稿箱截图", "暂停桌面操作"} {
		if !strings.Contains(g, want) {
			t.Fatalf("guidance missing %q:\n%s", want, g)
		}
	}
	e := sk.Entry(time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC))
	if e.Type != "knowledge" || e.Source != SourceDemo || !e.RequiresGUI || len(e.Params) != 1 || !strings.Contains(e.Content, "演示技能") {
		t.Fatalf("entry %+v", e)
	}
}

func TestProgressReplansNeverAborts(t *testing.T) {
	sk, _ := ParseSkill(goodAnswer)
	sk.Steps[1].Wait = true
	p := NewProgress(sk)
	if !strings.Contains(p.Next(), "继续第 1 步") {
		t.Fatal(p.Next())
	}
	// step 3 already true out of order
	p.Check(2, true, "草稿已在")
	for i := 0; i < 3; i++ {
		d := p.Check(0, false, "还有别的窗口")
		if !strings.Contains(d, "不要中止") {
			t.Fatal(d)
		}
		if i == 2 && !strings.Contains(d, "完全不同的方法") {
			t.Fatal(d)
		}
	}
	if d := p.Check(0, true, "ok"); !strings.Contains(d, "继续第 2 步") {
		t.Fatal(d)
	}
	if d := p.Check(1, false, "还在加载"); !strings.Contains(d, "巡检间隔") {
		t.Fatal(d)
	}
	if d := p.Check(1, true, "ok"); !strings.Contains(d, "验证交付物") {
		t.Fatal(d)
	}
	if p.Complete() {
		t.Fatal("not delivered yet")
	}
	if d := p.Deliver(true, ""); !strings.Contains(d, "没有证据") || p.Complete() {
		t.Fatal(d)
	}
	if d := p.Deliver(false, ""); !strings.Contains(d, "重新规划后继续") {
		t.Fatal(d)
	}
	p.Deliver(true, "草稿箱截图 s9.png")
	if !p.Complete() {
		t.Fatal("should be complete")
	}
}

func TestSpecRoundTripAndReview(t *testing.T) {
	sk, _ := ParseSkill(goodAnswer)
	found := FindSpec("tool result:\n" + sk.Guidance(nil) + "\nmore")
	if found == nil || found.Deliverable != sk.Deliverable || len(found.Steps) != 3 {
		t.Fatalf("found %+v", found)
	}
	p := NewProgress(found)
	results := map[string]bool{}
	judge := func(req string) (bool, string, error) {
		if req == found.Steps[1].Check {
			if !results["s2"] {
				return false, "标题还是空的", nil
			}
		}
		if strings.HasPrefix(req, "交付物") {
			return results["deliver"], "草稿箱为空", nil
		}
		if req == found.Steps[2].Check {
			return false, "", errors.New("judge down")
		}
		return true, "", nil
	}
	cont, d := p.Review(judge)
	if !cont || !strings.Contains(d, "第 2 步") || !strings.Contains(d, "标题还是空的") || !p.Done[0] {
		t.Fatalf("cont=%v d=%s", cont, d)
	}
	results["s2"] = true
	cont, d = p.Review(judge)
	if !cont || !strings.Contains(d, "交付物尚未达成") {
		t.Fatalf("cont=%v d=%s", cont, d)
	}
	results["deliver"] = true
	if cont, _ = p.Review(judge); cont || !p.Delivered {
		t.Fatal("should finish")
	}
	if FindSpec("no marker") != nil || FindSpec(SpecMarker+"{bad") != nil {
		t.Fatal("bad spec")
	}
}
