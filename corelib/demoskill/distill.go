package demoskill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// LLM is the minimal chat call the distiller needs.
type LLM func(ctx context.Context, system, user string) (string, error)

// MaxPromptEvents bounds how many non-narration events reach the model.
const MaxPromptEvents = 400

// noiseGap: input events this close to the previous identical one are repeats.
const noiseGap = 150 * time.Millisecond

// Compact orders the timeline, drops noise (empty events, repeated identical
// inputs, focus flapping back to the same window, screenshots without a
// caption) and keeps every narration. Narration carries the intent, so it is
// never dropped.
func Compact(tl Timeline) []Event {
	evs := append([]Event(nil), tl.Events...)
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].At.Before(evs[j].At) })
	out := make([]Event, 0, len(evs))
	var lastInput *Event
	lastFocus := ""
	for i := range evs {
		e := evs[i]
		e.Text = strings.TrimSpace(e.Text)
		switch e.Kind {
		case KindNarration:
			if e.Text == "" {
				continue
			}
		case KindFocus:
			key := e.App + "\x00" + e.Window
			if key == "\x00" || key == lastFocus {
				continue
			}
			lastFocus = key
		case KindInput:
			if e.Action == "" {
				continue
			}
			if lastInput != nil && lastInput.Action == e.Action && lastInput.Text == e.Text &&
				lastInput.X == e.X && lastInput.Y == e.Y && e.At.Sub(lastInput.At) < noiseGap {
				continue
			}
			cp := e
			lastInput = &cp
		case KindScreenshot:
			if e.Text == "" {
				continue
			}
		default:
			continue
		}
		out = append(out, e)
	}
	if len(out) > MaxPromptEvents {
		// keep all narration, thin the rest evenly
		var nar, rest []Event
		for _, e := range out {
			if e.Kind == KindNarration {
				nar = append(nar, e)
			} else {
				rest = append(rest, e)
			}
		}
		budget := MaxPromptEvents - len(nar)
		if budget < 0 {
			budget = 0
		}
		thin := make([]Event, 0, budget)
		for i := 0; i < budget && len(rest) > 0; i++ {
			thin = append(thin, rest[i*len(rest)/budget])
		}
		out = append(nar, thin...)
		sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	}
	return out
}

const distillSystem = `你把用户在桌面上的一次演示（带讲解）提炼成一个可复用的技能。
第一原则：bot 的核心是能持续推进任务，交付结果。技能是意图和指导，不是坐标回放。
规则：
- 讲解（narration）权重高于原始操作；操作只用来补全讲解没说的细节。
- 丢弃噪声、误操作、中途打断和与目标无关的动作。
- 每一步写成意图（例如“只保留当前窗口，最小化其他窗口”），不要写坐标；并给出可观察的成功检查 check。
- 演示里具体的值（文件名、关键词、收件人、日期等）如果每次会变，抽成 params。
- 需要长时间等待的步骤标 wait=true。涉及发布或付款的步骤 risky 写 "publish" 或 "payment"。
- deliverable 写清最终交付物，evidence 写如何证明它已完成。
只输出一个 JSON 对象：{"name","goal","triggers":[],"preconditions":[],"params":[{"name","description","example","required"}],"steps":[{"intent","check","hint","wait","risky"}],"deliverable","evidence"}`

// BuildPrompt renders the compacted timeline for the model.
func BuildPrompt(tl Timeline) string {
	evs := Compact(tl)
	var b strings.Builder
	if tl.Title != "" {
		fmt.Fprintf(&b, "演示标题：%s\n", tl.Title)
	}
	b.WriteString("时间线（相对秒数）：\n")
	start := tl.StartedAt
	if start.IsZero() && len(evs) > 0 {
		start = evs[0].At
	}
	for _, e := range evs {
		sec := e.At.Sub(start).Seconds()
		switch e.Kind {
		case KindNarration:
			fmt.Fprintf(&b, "[%6.1f] 讲解: %s\n", sec, e.Text)
		case KindFocus:
			fmt.Fprintf(&b, "[%6.1f] 焦点: %s | %s\n", sec, e.App, e.Window)
		case KindInput:
			if e.Text != "" {
				fmt.Fprintf(&b, "[%6.1f] 操作: %s %q\n", sec, e.Action, e.Text)
			} else {
				fmt.Fprintf(&b, "[%6.1f] 操作: %s\n", sec, e.Action)
			}
		case KindScreenshot:
			fmt.Fprintf(&b, "[%6.1f] 画面: %s\n", sec, e.Text)
		}
	}
	return b.String()
}

// Distill asks the model for a skill and validates it. One repair round is
// attempted when the answer is not usable, so a sloppy answer does not stop
// the flow.
func Distill(ctx context.Context, llm LLM, tl Timeline) (*Skill, error) {
	if llm == nil {
		return nil, errors.New("demoskill: no llm")
	}
	if len(Compact(tl)) == 0 {
		return nil, errors.New("demoskill: empty timeline")
	}
	user := BuildPrompt(tl)
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		prompt := user
		if lastErr != nil {
			prompt = user + "\n上次输出不可用：" + lastErr.Error() + "。请只输出符合要求的 JSON。"
		}
		raw, err := llm(ctx, distillSystem, prompt)
		if err != nil {
			return nil, err
		}
		sk, err := ParseSkill(raw)
		if err == nil {
			sk.SourceDemo = tl.ID
			return sk, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// ParseSkill extracts and validates the JSON skill from a model answer.
func ParseSkill(raw string) (*Skill, error) {
	s := strings.TrimSpace(raw)
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			s = s[i : j+1]
		}
	}
	var sk Skill
	if err := json.Unmarshal([]byte(s), &sk); err != nil {
		return nil, fmt.Errorf("invalid json: %v", err)
	}
	return &sk, sk.Normalize()
}

// Normalize trims fields and checks the skill can drive progress: it needs a
// goal, at least one step with an intent, and a deliverable. A step without a
// check gets one derived from its intent so it is still verifiable.
func (sk *Skill) Normalize() error {
	sk.Name = strings.TrimSpace(sk.Name)
	sk.Goal = strings.TrimSpace(sk.Goal)
	sk.Deliverable = strings.TrimSpace(sk.Deliverable)
	if sk.Goal == "" {
		return errors.New("missing goal")
	}
	if sk.Deliverable == "" {
		return errors.New("missing deliverable")
	}
	if sk.Name == "" {
		sk.Name = sk.Goal
	}
	steps := sk.Steps[:0]
	for _, st := range sk.Steps {
		st.Intent = strings.TrimSpace(st.Intent)
		if st.Intent == "" {
			continue
		}
		st.Check = strings.TrimSpace(st.Check)
		if st.Check == "" {
			st.Check = "屏幕或结果能看出「" + st.Intent + "」已完成"
		}
		switch st.Risky {
		case "publish", "payment":
		default:
			st.Risky = ""
		}
		steps = append(steps, st)
	}
	sk.Steps = steps
	if len(sk.Steps) == 0 {
		return errors.New("no steps")
	}
	params := sk.Params[:0]
	seen := map[string]bool{}
	for _, p := range sk.Params {
		p.Name = strings.TrimSpace(p.Name)
		if p.Name == "" || seen[p.Name] {
			continue
		}
		seen[p.Name] = true
		params = append(params, p)
	}
	sk.Params = params
	return nil
}
