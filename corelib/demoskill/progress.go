package demoskill

import (
	"fmt"
	"strings"
)

// Progress tracks a demo skill run inside the agent loop. It never ends a run
// on a failed check: a mismatch yields a replan directive and the loop keeps
// going. A run is complete only when every step and the deliverable have
// passed their checks with evidence.
type Progress struct {
	Skill     *Skill
	Done      []bool
	Attempts  []int
	Notes     []string
	Evidence  string
	Delivered bool
}

// NewProgress starts tracking a skill run.
func NewProgress(sk *Skill) *Progress {
	n := len(sk.Steps)
	return &Progress{Skill: sk, Done: make([]bool, n), Attempts: make([]int, n), Notes: make([]string, n)}
}

// Current returns the index of the first step whose check has not passed, or
// -1 when all steps passed.
func (p *Progress) Current() int {
	for i, d := range p.Done {
		if !d {
			return i
		}
	}
	return -1
}

// Check records the observed outcome of step i and returns what the agent
// should do next. Steps that pass out of order count too: the bot may already
// be past them.
func (p *Progress) Check(i int, ok bool, observed string) string {
	if i < 0 || i >= len(p.Done) {
		return p.Next()
	}
	p.Attempts[i]++
	p.Notes[i] = strings.TrimSpace(observed)
	if ok {
		p.Done[i] = true
		return p.Next()
	}
	st := p.Skill.Steps[i]
	var b strings.Builder
	fmt.Fprintf(&b, "第 %d 步检查未通过（第 %d 次）：期望「%s」，实际「%s」。不要中止。", i+1, p.Attempts[i], st.Check, p.Notes[i])
	switch {
	case st.Wait:
		b.WriteString("这是需要等待的步骤：选择一个比上次更长的巡检间隔后再查看。")
	case p.Attempts[i] >= 3:
		b.WriteString("同一做法已多次失败：换一种完全不同的方法（换入口、换工具、先补缺失的前置条件或回退一步）；只有缺少用户才知道的信息或需要用户做选择时才询问用户。")
	default:
		b.WriteString("先观察当前画面和状态，找出与演示不同之处，重新规划这一步再执行。")
	}
	return b.String()
}

// Deliver records the final deliverable check. Without evidence the run is
// not complete.
func (p *Progress) Deliver(ok bool, evidence string) string {
	evidence = strings.TrimSpace(evidence)
	if ok && evidence != "" {
		p.Delivered = true
		p.Evidence = evidence
		return "交付物已验证：" + evidence
	}
	if ok {
		return "交付物看起来已完成，但还没有证据：取得截图、文件、链接或页面状态作为证据后再结束。"
	}
	return "交付物尚未达成：「" + p.Skill.Deliverable + "」。回到未完成或结果不对的步骤，重新规划后继续。"
}

// Complete reports whether every step passed and the deliverable is verified.
func (p *Progress) Complete() bool { return p.Current() < 0 && p.Delivered }

// Next is the directive for the next open step, or the deliverable check.
func (p *Progress) Next() string {
	i := p.Current()
	if i < 0 {
		if p.Delivered {
			return "全部完成，交付物已验证。"
		}
		return "所有步骤已通过：现在验证交付物「" + p.Skill.Deliverable + "」并取得证据。"
	}
	st := p.Skill.Steps[i]
	return fmt.Sprintf("继续第 %d 步：%s（检查：%s）", i+1, st.Intent, st.Check)
}
