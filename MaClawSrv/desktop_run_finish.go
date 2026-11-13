package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// A finished run's user-visible message is this structure, not the model's
// prose. Raw text stays in the outcome file. status is success or failure.
// next_action is continue (the bot can remove the blocker), ask_user, or stop.
type desktopRunFinish struct {
	Status     string
	Results    []string
	Blocker    string
	Unblock    []string
	Next       string
	Remediated bool
	Record     string
}

type desktopRunFact struct {
	classes []string
	heals   int
}

var desktopRunFactMu sync.Mutex
var desktopRunFacts = map[string]*desktopRunFact{}

// desktopContinueTried marks a follow-up run that was started to remove a
// blocker. The same blocker on that run is not tried again.
var desktopContinueTried sync.Map // run id -> blocker kind

func desktopNoteRunBlocker(runID, class string) {
	runID, class = strings.TrimSpace(runID), strings.TrimSpace(class)
	if runID == "" || class == "" {
		return
	}
	desktopRunFactMu.Lock()
	defer desktopRunFactMu.Unlock()
	f := desktopRunFacts[runID]
	if f == nil {
		f = &desktopRunFact{}
		desktopRunFacts[runID] = f
	}
	for _, have := range f.classes {
		if have == class {
			return
		}
	}
	f.classes = append(f.classes, class)
}

func desktopNoteRunHeal(runID string) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}
	desktopRunFactMu.Lock()
	defer desktopRunFactMu.Unlock()
	f := desktopRunFacts[runID]
	if f == nil {
		f = &desktopRunFact{}
		desktopRunFacts[runID] = f
	}
	f.heals++
}

func desktopRunFactOf(runID string) desktopRunFact {
	desktopRunFactMu.Lock()
	defer desktopRunFactMu.Unlock()
	f := desktopRunFacts[strings.TrimSpace(runID)]
	if f == nil {
		return desktopRunFact{}
	}
	out := desktopRunFact{heals: f.heals, classes: append([]string(nil), f.classes...)}
	return out
}

// desktopBlockerKind maps a recorded failure class to a blocker the person
// can understand, and whether the bot can remove it without them.
func desktopBlockerKind(class string) (kind string, self bool) {
	switch class {
	case "login_wall", "mfa":
		return "login", false
	case "payment_confirm", "publish_needs_confirmation":
		return "permission", false
	case "infra_timeout", "timeout", "element_not_found", "script_error", "not_applied", "cross_origin_block", "rich_text_dom_write":
		return "resource", true
	default:
		return "", false
	}
}

func desktopBlockerWords(kind string) string {
	switch kind {
	case "login":
		return "登录"
	case "missing_info":
		return "缺少信息"
	case "account":
		return "账号不对"
	case "permission":
		return "需要你确认"
	case "resource":
		return "这一步的条件还不具备"
	default:
		return kind
	}
}

func desktopUnblockFor(kind string, self bool, ask string) string {
	switch kind {
	case "login":
		return "你在这个桌面完成登录或验证"
	case "permission":
		return "你确认这一步"
	case "missing_info":
		if ask != "" {
			return ask
		}
		return "你补充这一步缺少的信息"
	case "account":
		return "你指定要用的账号"
	case "resource":
		if self {
			return "我换一种做法再试"
		}
		return "这台桌面的浏览器重新有响应，或你接手这一步"
	default:
		return ""
	}
}

func desktopPlainResult(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, "{}[]") {
		return ""
	}
	if len([]rune(s)) > 160 {
		return ""
	}
	return s
}

func desktopComposeRunFinish(runID string, msg agentservice.Message, outcome, why string) (desktopRunFinish, bool) {
	if strings.TrimSpace(runID) == "" || outcome == "" || outcome == agentservice.TaskOutcomeRunning {
		return desktopRunFinish{}, false
	}
	fact := desktopRunFactOf(runID)
	var kind string
	self := false
	for _, class := range fact.classes {
		k, can := desktopBlockerKind(class)
		if k == "" {
			continue
		}
		kind, self = k, can
		if !can {
			break
		}
	}
	ask := ""
	if msg.Metadata != nil {
		ask = desktopPlainResult(msg.Metadata["ask_user_question"])
		if msg.Metadata["ask_user_secret_name"] != "" && kind != "login" {
			kind, self = "missing_info", false
		} else if ask != "" && kind == "" {
			kind, self = "missing_info", false
		}
	}
	fin := desktopRunFinish{Status: "failure", Next: "ask_user"}
	if outcome == agentservice.TaskOutcomeDone {
		fin.Status = "success"
		fin.Next = "stop"
		kind, self = "", false
	}
	fin.Results = desktopRunMeasuredResults(runID)
	if sentence, ok := desktopFinishSentence(runID); ok {
		if plain := desktopPlainResult(sentence); plain != "" && fin.Status == "success" {
			fin.Results = append(fin.Results, plain)
		}
	}
	if fin.Status == "failure" {
		fin.Blocker = kind
		if line := desktopUnblockFor(kind, self, ask); line != "" {
			fin.Unblock = []string{line}
		}
		continued := false
		if _, ok := desktopContinueTried.Load(runID); ok {
			fin.Remediated = true
			continued = true
			self = false
			if kind == "resource" {
				fin.Unblock = []string{desktopUnblockFor("resource", false, "")}
			}
		} else if fact.heals > 0 {
			fin.Remediated = true
		}
		if desktopImprovementReady(kind, self, continued) {
			fin.Next = "continue"
		}
	}
	_ = why
	return fin, true
}

func desktopRunMeasuredResults(runID string) []string {
	var out []string
	for _, r := range desktopPublishRunRecords(runID) {
		if !r.posted() {
			continue
		}
		line := "已发布"
		if r.Title != "" {
			line += "《" + r.Title + "》"
		}
		if r.URL != "" {
			line += " " + r.URL
		} else if r.PostID != "" {
			line += " " + r.PostID
		}
		out = append(out, line)
	}
	for _, j := range desktopLongJobAll() {
		if j.SourceRun != runID && j.OutcomeRun != runID {
			continue
		}
		if j.RoundMetric == nil {
			continue
		}
		name := j.RoundMetricName
		if name == "" {
			name = "结果"
		}
		out = append(out, fmt.Sprintf("%s %.4g", name, *j.RoundMetric))
	}
	return out
}

func (f desktopRunFinish) Sentence() string {
	return desktopRunFinishSentence(f)
}

func desktopRunFinishSentence(f desktopRunFinish) string {
	if f.Status == "success" {
		s := "做成了。"
		if len(f.Results) > 0 {
			s += "\n" + strings.Join(f.Results, "\n")
		}
		return s
	}
	var b strings.Builder
	b.WriteString("没有做成。")
	if f.Blocker != "" {
		b.WriteString("\n卡在：" + desktopBlockerWords(f.Blocker))
	}
	if len(f.Unblock) > 0 {
		b.WriteString("\n要继续需要：" + strings.Join(f.Unblock, "；"))
	}
	if f.Remediated {
		b.WriteString("\n我已经自己试过处理。")
	} else {
		b.WriteString("\n我还没能自己处理。")
	}
	if suggestion := desktopFinishSuggestion(f); suggestion != "" {
		b.WriteString("\n建议：" + suggestion)
	}
	return b.String()
}

func desktopFinishSuggestion(f desktopRunFinish) string {
	switch f.Blocker {
	case "login":
		return "你在这个桌面登录或通过验证后，我从这一页继续"
	case "permission":
		return "你确认这一步之后，我继续"
	case "missing_info":
		return "你补充缺的信息之后，我继续"
	case "account":
		return "你指定账号之后，我继续"
	case "resource":
		if desktopImprovementReady(f.Blocker, true, f.Remediated) {
			return "这一步我可以自己换做法，我接着做"
		}
		return "换一种做法，或者你接手这一步"
	default:
		if f.Status == "failure" {
			return "指出卡在哪一步，我再继续"
		}
		return ""
	}
}

// desktopImprovementReady is the gate for doing the next improvement instead
// of ending. The blocker has to be one the bot can remove, and this run must
// not already have tried that same step.
func desktopImprovementReady(blocker string, self, alreadyTried bool) bool {
	if alreadyTried || !self || blocker != "resource" {
		return false
	}
	return true
}

func desktopStoreRunFinish(runID, modelText, why string, fin desktopRunFinish) {
	dir := filepath.Join(corelib.MaclawDataDir(), "run-outcomes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	rec := map[string]any{
		"run_id": runID, "status": fin.Status, "results": fin.Results,
		"blocker": fin.Blocker, "unblock_conditions": fin.Unblock,
		"next_action": fin.Next, "self_remediated": fin.Remediated,
		"model_text": modelText, "evidence": why,
	}
	raw, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, runID+".json"), raw, 0o644); err != nil {
		return
	}
}

func desktopRunFinishRecordPath(runID string) string {
	return filepath.Join(corelib.MaclawDataDir(), "run-outcomes", runID+".json")
}

// desktopRunContinueSend starts the follow-up that removes a blocker the bot
// can fix. Tests replace it. An error leaves the run's own outcome message
// in place.
var desktopRunContinueSend = desktopRunContinueSendDefault

func desktopRunContinueSendDefault(ctx context.Context, svc *agentservice.Service, p agentservice.Principal, instanceID, sessionID, runID, blocker, unblock string) error {
	if svc == nil || strings.TrimSpace(instanceID) == "" || strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("cannot continue this run")
	}
	prompt := "这不是新任务。上一步没有做成，卡在" + desktopBlockerWords(blocker) + "。你可以自己处理：" + unblock + "。不要把日志或 JSON 贴给这个人。处理完用一条话说结果；仍然需要这个人时再问。"
	_, run, err := svc.SendMessageAsync(ctx, p, instanceID, agentservice.SendMessageInput{
		Content:   prompt,
		SessionID: sessionID,
		Metadata:  map[string]string{"outcome_parent": runID, "outcome_tried": blocker},
	})
	if err != nil {
		return err
	}
	if run != nil {
		desktopContinueTried.Store(run.ID, blocker)
	}
	return nil
}
