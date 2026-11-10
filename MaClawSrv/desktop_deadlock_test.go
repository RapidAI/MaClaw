package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/recovery"
)

// Replay of run_854d0758 (Bot 1, 2026-11-10 21:25-21:27, 知乎+CSDN+小红书):
// two CDP Page.navigate timeouts on 知乎 blocked "publish" for the whole run,
// the block's advice was "take a screenshot or call request_takeover", the
// compose phase refused the screenshot and takeover_for_publish refused the
// takeover. Every guard was right alone; together no action was permitted.

const deadlockTask = "目标：从 Hugging Face Daily Papers 中选出热度最高的一篇论文，撰写一篇图文综述，并分别发布到知乎、CSDN、小红书三个平台。成功标准：三个平台各有一篇已发布的图文综述，回报时给出三篇的发布链接。"

const deadlockInfraErr = "publish step observe failed: could not open the editor https://zhuanlan.zhihu.com/write: cdp timeout: Page.navigate (id=3171) observe: FAILED (15002ms)"

func deadlockEnv(t *testing.T) (agentruntime.TurnRequest, map[string]int) {
	t.Helper()
	recoveryTestEnv(t)
	oldCtl, oldLedger := desktopRemoteCtl, desktopLedgerDir
	dir := t.TempDir()
	desktopLedgerDir = func() string { return dir }
	desktopRemoteCtl = func(ctx context.Context, tenantID, userID, display string, request map[string]any) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	}
	t.Cleanup(func() { desktopRemoteCtl, desktopLedgerDir = oldCtl, oldLedger })
	run := fmt.Sprintf("run-deadlock-%s-%d", strings.ReplaceAll(t.Name(), "/", "-"), time.Now().UnixNano())
	req := recoveryTurn("tenant-dl", desktop.BotDesktopUser("daniel", "bot_964f"), run, deadlockTask)
	calls := map[string]int{}
	editURL := "https://zhuanlan.zhihu.com/p/2103595346576384731/edit"
	operateDesktopFunc = func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
		a := desktopArg(args, "action")
		calls[a]++
		switch a {
		case "navigate":
			return "navigated to " + editURL, nil
		case "publish":
			calls["publish:"+siteExpKey(desktopArg(args, "editor_url"))]++
			return "", errors.New(deadlockInfraErr)
		case "screenshot":
			return "screenshot of " + editURL, nil
		case "probe":
			return "page " + editURL + " editor=true", nil
		case "request_takeover":
			return requestDesktopTakeover(ctx, scope, desktopArg(args, "reason"))
		}
		return "ok", nil
	}
	return req, calls
}

func deadlockPublish(editor string) map[string]any {
	return map[string]any{"action": "publish", "editor_url": editor, "title": "触觉模型登顶 HF 日榜第一", "body": strings.Repeat("触觉基础模型的综述正文。", 40)}
}

func TestRun854GuardDeadlockReplayLeavesObservationAndTakeover(t *testing.T) {
	req, calls := deadlockEnv(t)
	if _, err := invoke(t, req, map[string]any{"action": "navigate", "url": "https://zhuanlan.zhihu.com/p/2103595346576384731/edit"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := invoke(t, req, deadlockPublish("https://zhuanlan.zhihu.com/write")); err == nil {
			t.Fatal("stub publish should fail")
		}
	}
	// The third 知乎 publish is refused, with the way out and the facts.
	_, err := invoke(t, req, deadlockPublish("https://zhuanlan.zhihu.com/write"))
	if err == nil || !strings.Contains(err.Error(), "already failed") || !strings.Contains(err.Error(), "Still permitted") || !strings.Contains(err.Error(), "request_takeover") {
		t.Fatalf("blocked publish: %v", err)
	}
	if calls["publish:zhihu"] != 2 {
		t.Fatalf("zhihu publish ran %d times", calls["publish:zhihu"])
	}
	// Observing is permitted in the compose phase now (was: publish_plan(compose) refused).
	if _, err := invoke(t, req, map[string]any{"action": "screenshot"}); err != nil || calls["screenshot"] != 1 {
		t.Fatalf("screenshot refused on a blocked run: %v", err)
	}
	if _, err := invoke(t, req, map[string]any{"action": "probe", "query": "发布"}); err != nil {
		t.Fatalf("probe refused: %v", err)
	}
	// Handing the step to the person is permitted (was: takeover_for_publish).
	if _, err := invoke(t, req, map[string]any{"action": "request_takeover", "reason": "标题正文和三张图都已填好，知乎编辑器打不开，请你亲自点「发布」"}); err != nil {
		t.Fatalf("takeover refused on a blocked run: %v", err)
	}
	// Another requested site is not blocked by 知乎's timeouts.
	if _, err := invoke(t, req, deadlockPublish("https://editor.csdn.net/md/")); err == nil || strings.Contains(err.Error(), "already failed") {
		t.Fatalf("csdn publish blocked by zhihu: %v", err)
	}
	if calls["publish:csdn"] != 1 {
		t.Fatalf("csdn publish did not run: %v", calls)
	}
	// No infrastructure timeout became an "avoid" lesson for the site.
	if txt := desktopExperienceText(req.Scope, "zhuanlan.zhihu.com"); strings.Contains(txt, "infra") || strings.Contains(txt, "failed with timeout") {
		t.Fatalf("infra timeout learned as a site lesson: %s", txt)
	}
}

func TestRepeatedGuardRefusalsTurnIntoStopAndReport(t *testing.T) {
	req, _ := deadlockEnv(t)
	_, _ = invoke(t, req, map[string]any{"action": "navigate", "url": "https://zhuanlan.zhihu.com/p/1/edit"})
	for i := 0; i < 2; i++ {
		_, _ = invoke(t, req, deadlockPublish("https://zhuanlan.zhihu.com/write"))
	}
	var last error
	for i := 0; i < desktopGuardRefusalLimit; i++ {
		_, last = invoke(t, req, deadlockPublish("https://zhuanlan.zhihu.com/write"))
	}
	if last == nil || !strings.Contains(last.Error(), "STOP_AND_REPORT") || !strings.Contains(last.Error(), "zhihu") {
		t.Fatalf("no stop-and-report after %d refusals: %v", desktopGuardRefusalLimit, last)
	}
}

// Without an impairment the phase plan still steers a healthy run: a
// screenshot in the compose phase stays refused.
func TestHealthyComposePhaseStillRefusesScreenshot(t *testing.T) {
	req, calls := deadlockEnv(t)
	_, _ = invoke(t, req, map[string]any{"action": "navigate", "url": "https://zhuanlan.zhihu.com/p/1/edit"})
	if _, err := invoke(t, req, map[string]any{"action": "screenshot"}); err == nil || !strings.Contains(err.Error(), "publish_plan(compose)") || calls["screenshot"] != 0 {
		t.Fatalf("healthy compose phase: %v", err)
	}
}

// No-deadlock invariant: from every guard state an impaired run reaches, at
// least one non-mutating action (observation or takeover) is permitted.
func TestNoDeadlockInvariantAcrossPhases(t *testing.T) {
	for _, phase := range []desktopPublishPhase{phaseResearch, phaseCompose, phaseSubmitted, phaseLive, phaseIncomplete} {
		t.Run(string(phase), func(t *testing.T) {
			req, calls := deadlockEnv(t)
			_, _ = invoke(t, req, map[string]any{"action": "navigate", "url": "https://zhuanlan.zhihu.com/p/1/edit"})
			for i := 0; i < 2; i++ {
				_, _ = invoke(t, req, deadlockPublish("https://zhuanlan.zhihu.com/write"))
			}
			switch phase {
			case phaseResearch:
				_, _ = invoke(t, req, map[string]any{"action": "navigate", "url": "https://huggingface.co/papers"})
				// navigate succeeded: make the run impaired again by a failing step
				_, _ = invoke(t, req, deadlockPublish("https://zhuanlan.zhihu.com/write"))
			case phaseSubmitted, phaseLive, phaseIncomplete:
				st := map[desktopPublishPhase]browser.PublishState{phaseSubmitted: browser.PublishSubmitted, phaseLive: browser.PublishLive, phaseIncomplete: browser.PublishLiveIncomplete}[phase]
				desktopPublishRecordUpdate(desktopOwner(req.Scope), func(r *desktopPublishRecord) { r.State = st; r.Site = "zhihu" })
			}
			permitted := 0
			for _, a := range []map[string]any{{"action": "screenshot"}, {"action": "probe"}, {"action": "file_list", "path": "/tmp"}, {"action": "request_takeover", "reason": "知乎编辑器打不开，请你看一下"}} {
				before := calls[a["action"].(string)]
				if _, err := invoke(t, req, a); err == nil && calls[a["action"].(string)] > before {
					permitted++
				}
			}
			if permitted == 0 {
				t.Fatalf("phase %s: no observation or takeover permitted", phase)
			}
		})
	}
}

// A multi-site run with one site verified and another blocked reports
// partial, names both, and a reply that omits the verified link gets it.
func TestMultiSiteOutcomeNamesThePublishedSiteWhenAnotherIsBlocked(t *testing.T) {
	req, _ := deadlockEnv(t)
	scope := desktopOwner(req.Scope)
	eng := desktopRecovery()
	sk := desktopRecoveryScope(req.Scope)
	lin := "lin1#1"
	for _, site := range []string{"xiaohongshu", "zhihu"} {
		goal := "publish|" + lin + "|" + site
		desktopRecNoteRunGoal(req.Scope.RunID, sk+"\x01"+goal)
		l, _, err := eng.Acquire(sk, recovery.AttemptRequest{Goal: goal, Surface: "site:" + site + "|publish", Method: "publish", Run: req.Scope.RunID})
		if err != nil {
			t.Fatal(err)
		}
		o := recovery.Observation{Goal: goal, Method: "publish", Run: req.Scope.RunID}
		if site == "xiaohongshu" {
			o.OK, o.Complete, o.Facts = true, true, map[string]float64{"published": 1}
		} else {
			o.Error, o.Infra = deadlockInfraErr, true
		}
		eng.Finish(sk, l.ID, o)
	}
	desktopLedgerAdd(desktopLedgerKey(scope), desktopLedgerEntry{Kind: "posted", Host: "creator.xiaohongshu.com", Title: "触觉模型登顶HF日榜第一", URL: "https://www.xiaohongshu.com/explore/6af31b53000000001803b7af", Run: req.Scope.RunID, Detail: "published"})
	msg := agentservice.Message{Content: "三个平台都没有发布成功：桌面工具多次拒绝并报错。"}
	md := desktopTaskOutcomeMetadata(req.Scope.RunID, msg)
	if md[agentservice.TaskOutcomeKey] != "partial" {
		t.Fatalf("outcome %v", md)
	}
	if ev := md["task_outcome_evidence"]; !strings.Contains(ev, "xiaohongshu") || !strings.Contains(ev, "zhihu") {
		t.Fatalf("evidence must name both parts: %q", ev)
	}
	if n := md[agentservice.TaskOutcomeNoticeKey]; !strings.Contains(n, "https://www.xiaohongshu.com/explore/6af31b53000000001803b7af") {
		t.Fatalf("notice omits the verified post: %q", n)
	}
	if facts := desktopHonestFacts(scope); !strings.Contains(facts, "published https://www.xiaohongshu.com/explore/6af31b53000000001803b7af") || !strings.Contains(facts, "zhihu: NOT done") {
		t.Fatalf("honest facts: %s", facts)
	}
}

// The publish goal is the pipeline's target site, whatever tab was open.
func TestPublishGoalKeyedByTargetNotOpenTab(t *testing.T) {
	req, _ := deadlockEnv(t)
	_, _ = invoke(t, req, map[string]any{"action": "navigate", "url": "http://127.0.0.1:8765/view.html"})
	desktopRunNoteSite(desktopOwner(req.Scope), "127.0.0.1")
	if s := desktopPublishSiteFor(desktopOwner(req.Scope), map[string]any{"editor_url": "https://editor.csdn.net/md/"}); s != "csdn" {
		t.Fatalf("editor_url site %q", s)
	}
	desktopNoteTaskSpec(req.Scope.RunID, deadlockTask)
	if s := desktopPublishSiteFor(desktopOwner(req.Scope), map[string]any{}); s == "127.0.0.1" || s == "unknown" {
		t.Fatalf("publish without editor_url keyed by the open tab: %q", s)
	}
}

// 「分别发布到」 is a publish request; 「别发布」 is not (1eaf1c574 read the 别
// of 分别 as a negation and dropped the whole 3-site request).
func TestPublishAskKeepsWordsContaining别(t *testing.T) {
	if !desktopPublishAskRe.MatchString(deadlockTask) {
		t.Fatal("分别发布到知乎、CSDN、小红书 not read as a publish request")
	}
	for _, neg := range []string{"生成一份简历发我，不要发布到任何网站", "写好后别发布到知乎"} {
		if desktopPublishAskRe.MatchString(neg) {
			t.Fatalf("%q read as a publish request", neg)
		}
	}
}
