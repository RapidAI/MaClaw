package demoskill

import (
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// SourceDemo marks NLSkillEntry values produced from a demonstration.
const SourceDemo = "demo"

// Guidance renders the skill as instructions for the normal agent loop. The
// agent keeps reasoning: steps are intents with checks, a failed check means
// replan and continue, and the run ends only with verified evidence of the
// deliverable or a real need for the user's information or choice.
func (sk *Skill) Guidance(params map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# 演示技能：%s\n\n", sk.Name)
	b.WriteString("第一原则：持续推进任务，交付结果。下面是用户演示得到的意图和指导，不是坐标回放。\n")
	b.WriteString("- 每一步先看当前画面/状态再行动；界面或状态与演示不同就按意图自行调整做法。\n")
	b.WriteString("- 每步做完用该步的「检查」确认；检查不通过就重新规划（换方法、补前置步骤、回退一步），不要中止，也不要停在半成品。\n")
	b.WriteString("- 已满足检查的步骤可以跳过；顺序可按实际情况调整，只要最终交付物达成。\n")
	b.WriteString("- 需要等待的步骤：自己选择合适的巡检间隔（由短到长）反复查看，直到观察到完成。\n")
	b.WriteString("- 只有在确实缺少用户才知道的信息或必须由用户做选择时才询问用户。\n")
	b.WriteString("- 标注为发布/付款的步骤照常遵守现有的审核与询问规则。\n")
	b.WriteString("- 不做任何反检测或模拟人类的随机化操作。\n")
	b.WriteString("- 用户正在查看或操作桌面时，暂停桌面操作，等用户离开后继续。\n\n")
	fmt.Fprintf(&b, "目标：%s\n", sk.Goal)
	if len(sk.Preconditions) > 0 {
		b.WriteString("前置条件（不满足就先设法满足）：\n")
		for _, p := range sk.Preconditions {
			fmt.Fprintf(&b, "- %s\n", p)
		}
	}
	if len(sk.Params) > 0 {
		b.WriteString("参数：\n")
		for _, p := range sk.Params {
			v := strings.TrimSpace(params[p.Name])
			switch {
			case v != "":
				fmt.Fprintf(&b, "- %s = %s\n", p.Name, v)
			case p.Required:
				fmt.Fprintf(&b, "- %s：%s（未给出：先从任务上下文推断，确实无法推断再问用户）\n", p.Name, p.Description)
			default:
				fmt.Fprintf(&b, "- %s：%s（可选，演示值：%s）\n", p.Name, p.Description, p.Example)
			}
		}
	}
	b.WriteString("步骤：\n")
	for i, st := range sk.Steps {
		fmt.Fprintf(&b, "%d. %s\n   检查：%s\n", i+1, st.Intent, st.Check)
		if st.Hint != "" {
			fmt.Fprintf(&b, "   演示时的做法（仅供参考）：%s\n", st.Hint)
		}
		if st.Wait {
			b.WriteString("   需等待：巡检直到检查通过。\n")
		}
		switch st.Risky {
		case "publish":
			b.WriteString("   涉及发布：按现有审核规则处理。\n")
		case "payment":
			b.WriteString("   涉及付款：按现有询问规则处理。\n")
		}
	}
	fmt.Fprintf(&b, "交付物：%s\n", sk.Deliverable)
	if sk.Evidence != "" {
		fmt.Fprintf(&b, "完成证据：%s\n", sk.Evidence)
	}
	b.WriteString("结束前必须拿到交付物已完成的证据（截图、文件、链接或页面状态），并在回复里给出。\n")
	if line := sk.SpecLine(); line != "" {
		b.WriteString("\n")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// Entry converts the skill into a knowledge-type NLSkillEntry so it is stored
// and injected through the existing skill system unchanged.
func (sk *Skill) Entry(now time.Time) corelib.NLSkillEntry {
	triggers := sk.Triggers
	if len(triggers) == 0 {
		triggers = []string{sk.Goal}
	}
	e := corelib.NLSkillEntry{
		Name:        sk.Name,
		Description: sk.Goal,
		Triggers:    triggers,
		Status:      "active",
		Source:      SourceDemo,
		Type:        "knowledge",
		Content:     sk.Guidance(nil),
		CreatedAt:   now.Format(time.RFC3339),
		RequiresGUI: true,
	}
	for _, p := range sk.Params {
		e.Params = append(e.Params, corelib.NLSkillParam{Name: p.Name, Description: describeParam(p), Required: p.Required})
	}
	return e
}

func describeParam(p Param) string {
	if p.Example == "" {
		return p.Description
	}
	return p.Description + "（演示值：" + p.Example + "）"
}
