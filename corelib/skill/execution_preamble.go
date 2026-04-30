package skill

// execution_preamble.go generates system-level execution instructions that
// are injected before skill execution. These instructions enforce safety
// defaults, negative constraints, and quality thresholds that skill authors
// shouldn't need to write manually.
//
// Inspired by the "4 weapons against LLM laziness" from the "5 Skill
// Architecture Patterns" article:
//   1. Imperative tone — command-style instructions
//   2. Excuse rebuttal — preempt common shortcuts
//   3. Quantitative thresholds — hard minimums
//   4. Negative instructions — explicit "do NOT" rules

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// BuildExecutionPreamble generates execution rules for a skill based on
// its structural characteristics. Returns empty string if no rules apply.
//
// Rules are data-driven (derived from step actions, not keywords).
// This is a system-level supplement — it does NOT replace instructions
// that skill authors write in SKILL.md.
func BuildExecutionPreamble(skill *corelib.NLSkillEntry) string {
	var rules []string

	// Universal rules (all skills)
	rules = append(rules,
		"严格按照 skill 定义的步骤顺序执行，不要跳过任何步骤",
		"不要自行修改 skill 脚本的参数或逻辑",
		"遇到错误时报告具体错误信息，不要猜测原因或自行重试",
	)

	features := analyzeSkillFeatures(skill)

	// Deploy safety
	if features.hasDeploy {
		rules = append(rules,
			"不要部署到 production 环境，除非用户明确要求",
			"不要删除或覆盖已有的部署配置",
		)
	}

	// Database safety
	if features.hasDatabase {
		rules = append(rules,
			"不要执行 DROP/DELETE/TRUNCATE 操作，除非用户明确要求",
			"数据库操作前先备份或确认",
		)
	}

	// craft_tool quality thresholds
	if features.hasCraftTool {
		rules = append(rules,
			"生成的脚本必须包含错误处理（try/catch 或 set -e）",
			"生成的脚本必须在开头验证所有输入参数非空",
			"不要生成超过 200 行的单个脚本——拆分为多个函数",
		)
	}

	// Verification enforcement
	if features.hasVerification {
		rules = append(rules,
			"验证步骤的输出必须完整引用，不要截断或总结",
			"验证失败时必须执行修复步骤，不要跳过",
		)
	}

	// File operation safety
	if features.hasFileOps {
		rules = append(rules,
			"写入文件前确认目标路径正确，不要覆盖用户未指定的文件",
		)
	}

	if len(rules) <= 3 {
		// Only universal rules, no skill-specific rules — skip preamble
		// to avoid noise in simple skills.
		return ""
	}

	var b strings.Builder
	b.WriteString("[执行规则]\n")
	for _, r := range rules {
		b.WriteString("- ")
		b.WriteString(r)
		b.WriteString("\n")
	}
	return b.String()
}

type skillFeatures struct {
	hasDeploy       bool
	hasDatabase     bool
	hasCraftTool    bool
	hasVerification bool
	hasFileOps      bool
}

// analyzeSkillFeatures inspects step actions and params to determine
// what safety rules apply. This is structural analysis, not keyword
// matching on description/name.
func analyzeSkillFeatures(skill *corelib.NLSkillEntry) skillFeatures {
	f := skillFeatures{}
	for _, step := range skill.Steps {
		switch step.Action {
		case "craft_tool":
			f.hasCraftTool = true
		}

		cmd, _ := step.Params["command"].(string)
		lower := strings.ToLower(cmd)

		// Deploy detection: deployment commands
		if containsAny(lower, "deploy", "kubectl apply", "docker push",
			"helm install", "terraform apply", "serverless deploy") {
			f.hasDeploy = true
		}

		// Database detection
		if containsAny(lower, "psql", "mysql", "sqlite", "mongosh",
			"redis-cli", "drop ", "delete from", "truncate") {
			f.hasDatabase = true
		}

		// File write detection
		if containsAny(lower, "write_file", "> ", ">> ", "tee ",
			"mv ", "cp ") {
			f.hasFileOps = true
		}

		// Verification: poll, loop, or conditional steps
		if step.Poll != nil || step.Loop != nil {
			f.hasVerification = true
		}
		if step.Condition == "on_failure" || step.Condition == "on_success" {
			f.hasVerification = true
		}
	}

	return f
}

// containsAny returns true if s contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
