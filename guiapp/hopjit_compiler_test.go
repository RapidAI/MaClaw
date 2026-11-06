package guiapp

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/hopjit"
)

func TestRunHopTaskCompilesAndExecutesSpec(t *testing.T) {
	calls := 0
	caller := &guiHopJITCaller{invoke: func(_ context.Context, _ []interface{}, jsonResponse bool) (string, error) {
		calls++
		if calls == 1 {
			if jsonResponse {
				t.Fatal("compiler requested JSON instead of Markdown")
			}
			return `# Spec: 任务摘要
Goal: 生成并核验摘要
Outputs:
- report % text
## Steps
1. [reason] 生成摘要
  + → report % text
2. [check] 核验摘要
  - ← report
  + → accepted % bool
  + → feedback % text
3. [exit]
`, nil
		}
		if jsonResponse && calls == 2 {
			return `{"report":"完成"}`, nil
		}
		if jsonResponse {
			return `{"accepted":true,"feedback":""}`, nil
		}
		return "", nil
	}}
	result, err := runHopTaskWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), "生成任务摘要", caller)
	if err != nil || result.Status != hopjit.NextCompleted || result.Outputs["report"] != "完成" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if calls != 3 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestCompiledHopSpecRejectsCommit(t *testing.T) {
	calls := 0
	caller := &guiHopJITCaller{invoke: func(context.Context, []interface{}, bool) (string, error) {
		calls++
		return `# Spec: 危险规约
Goal: 不应运行
Outputs:
- report % text
## Steps
1. [commit] 提交
  + → report % text
  > ` + "```hop_python" + `
  > report = "danger"
  > ` + "```" + `
`, nil
	}}
	_, err := runHopTaskWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), "执行危险任务", caller)
	if err == nil || !strings.Contains(err.Error(), "commit is not allowed") {
		t.Fatalf("err=%v", err)
	}
	if calls != 1 {
		t.Fatalf("commit rejection retried, calls=%d", calls)
	}
}

func TestCompiledHopSpecRejectsMalformedDocument(t *testing.T) {
	calls := 0
	caller := &guiHopJITCaller{invoke: func(context.Context, []interface{}, bool) (string, error) {
		calls++
		return "不是 HopSpec", nil
	}}
	_, err := runHopTaskWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), "任务", caller)
	if err == nil {
		t.Fatal("malformed compiler output was accepted")
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want one rewrite", calls)
	}
}

func TestCompiledHopSpecAcceptsFullwidthTitle(t *testing.T) {
	calls := 0
	caller := &guiHopJITCaller{invoke: func(_ context.Context, _ []interface{}, jsonResponse bool) (string, error) {
		calls++
		if calls == 1 {
			return "# Spec：任务摘要\nGoal：生成并核验摘要\nOutputs：\n- report % text\n## Steps\n1. [reason] 生成摘要\n  + → report % text\n2. [check] 核验摘要\n  - ← report\n  + → accepted % bool\n  + → feedback % text\n3. [exit]\n", nil
		}
		if jsonResponse && calls == 2 {
			return `{"report":"完成"}`, nil
		}
		if jsonResponse {
			return `{"accepted":true,"feedback":""}`, nil
		}
		return "", nil
	}}
	result, err := runHopTaskWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), "生成任务摘要", caller)
	if err != nil || result.Status != hopjit.NextCompleted || result.Outputs["report"] != "完成" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if calls != 3 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestCompiledHopSpecRepairsMissingTitle(t *testing.T) {
	const task = "查询北京今天的天气并给出一句结论"
	spec := normalizeCompiledHopMarkdown("Goal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [exit]\n", task)
	if strings.Contains(spec, "MISSING_SPEC_TITLE") || !strings.Contains(spec, "Goal: 查询天气") {
		t.Fatalf("spec=%q", spec)
	}
	if !strings.HasPrefix(spec, "# Spec: 查询北京今天的天气并给出一句结论\n") {
		t.Fatalf("spec=%q", spec)
	}
	headed := normalizeCompiledHopMarkdown("## 目标：查询天气\n## 步骤\n1. [exit]\n", "查询天气")
	if !strings.HasPrefix(headed, "# Spec: 查询天气\n") || !strings.Contains(headed, "## 目标：查询天气") {
		t.Fatalf("headed=%q", headed)
	}
	bold := normalizeCompiledHopMarkdown("**目标：** 查询天气\n## 步骤\n1. [exit]\n", "查询天气")
	if !strings.HasPrefix(bold, "# Spec: 查询天气\n") || !strings.Contains(bold, "查询天气") {
		t.Fatalf("bold=%q", bold)
	}
	long := strings.Repeat("天气", 30)
	title := hopSpecTitleFromTask(long)
	if len([]rune(title)) != 40 {
		t.Fatalf("title runes=%d", len([]rune(title)))
	}
}

func TestNormalizeCompiledHopMarkdownExtractsFencedSpec(t *testing.T) {
	raw := "下面是规约：\n```markdown\n# Spec：北京天气\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [exit]\n```\n以上。"
	spec := normalizeCompiledHopMarkdown(raw, "北京天气")
	if !strings.HasPrefix(spec, "# Spec：北京天气\n") || strings.Contains(spec, "```") || strings.Contains(spec, "以上") || !strings.Contains(spec, "Goal: 查询天气") {
		t.Fatalf("spec=%q", spec)
	}
	withPython := "说明\n```markdown\n# Spec: 任务\nGoal: 做\nOutputs:\n- report % text\n## Steps\n1. [act] 运行\n```hop_python\nreport = \"a:b\"\n```\n2. [exit]\n```\n以上。\n"
	spec = normalizeCompiledHopMarkdown(withPython, "任务")
	if !strings.Contains(spec, "```hop_python") || !strings.Contains(spec, "report = \"a:b\"") || !strings.Contains(spec, "2. [exit]") || strings.Contains(spec, "以上") {
		t.Fatalf("python spec=%q", spec)
	}
	thought := "<think>\n# Spec: 藏在思考里\nGoal: 查询天气\nOutputs:\n- report % text\n## Steps\n1. [exit]\n</think>\n我来解释。"
	spec = normalizeCompiledHopMarkdown(thought, "任务")
	if !strings.HasPrefix(spec, "# Spec: 藏在思考里\n") || strings.Contains(spec, "我来解释") || strings.Contains(spec, "<think>") {
		t.Fatalf("thought spec=%q", spec)
	}
}

func TestHopSpecFromModelFieldsPrefersHeading(t *testing.T) {
	got := hopSpecFromModelFields("可见回答", "可见回答", "<think>\n# Spec: 藏在思考里\n</think>")
	if !strings.Contains(got, "# Spec: 藏在思考里") {
		t.Fatalf("reasoning heading dropped: %q", got)
	}
	got = hopSpecFromModelFields("# Spec: 正文", "raw", "# Spec: 旧思考")
	if !strings.HasPrefix(strings.TrimSpace(got), "# Spec: 正文") {
		t.Fatalf("content heading lost: %q", got)
	}
	got = hopSpecFromModelFields("<think>only</think>答案", "<think>only</think>答案", "thought")
	if got != "答案" {
		t.Fatalf("visible=%q", got)
	}
	got = hopSpecFromModelFields("好的", "好的", "Goal: 查询天气\n## Steps\n1. [exit]\n")
	if !strings.Contains(got, "Goal: 查询天气") || !strings.Contains(got, "## Steps") {
		t.Fatalf("goal document lost behind prose: %q", got)
	}
	got = hopSpecFromModelFields("好的", "好的", "<think>\nGoal: 查询天气\n## Steps\n1. [exit]\n</think>")
	if !strings.Contains(got, "Goal: 查询天气") || !strings.Contains(got, "## Steps") {
		t.Fatalf("goal document inside think was dropped: %q", got)
	}
	spec := normalizeCompiledHopMarkdown(got, "查询天气")
	if !strings.HasPrefix(spec, "# Spec: 查询天气\n") || !strings.Contains(spec, "Goal: 查询天气") {
		t.Fatalf("spec=%q", spec)
	}
}

func TestCompiledHopSpecRewriteRetriesOnce(t *testing.T) {
	calls := 0
	caller := &guiHopJITCaller{invoke: func(_ context.Context, _ []interface{}, jsonResponse bool) (string, error) {
		calls++
		if calls == 1 {
			return "不是 HopSpec", nil
		}
		if !jsonResponse && calls == 2 {
			return `# Spec: 任务摘要
Goal: 生成并核验摘要
Outputs:
- report % text
## Steps
1. [reason] 生成摘要
  + → report % text
2. [check] 核验摘要
  - ← report
  + → accepted % bool
  + → feedback % text
3. [exit]
`, nil
		}
		if jsonResponse && calls == 3 {
			return `{"report":"完成"}`, nil
		}
		if jsonResponse {
			return `{"accepted":true,"feedback":""}`, nil
		}
		return "", nil
	}}
	result, err := runHopTaskWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), "生成任务摘要", caller)
	if err != nil || result.Status != hopjit.NextCompleted || result.Outputs["report"] != "完成" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if calls != 4 {
		t.Fatalf("calls=%d", calls)
	}
}
