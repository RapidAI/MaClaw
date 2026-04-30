package skill

// description_quality.go evaluates the quality of a skill's description
// field based on the three-element model from the "5 Skill Architecture
// Patterns" article:
//   1. Trigger phrases — words the user might say
//   2. Temporal position — when to use (before/after what)
//   3. Product keywords — specific platforms, formats, tools
//
// This is NOT a lint gate — it never blocks execution. It provides
// suggestions at install time to help skill authors write better
// descriptions that improve LLM matching accuracy.

import (
	"strings"
	"unicode/utf8"
)

// DescriptionQuality holds the evaluation result for a skill description.
type DescriptionQuality struct {
	Score       float64  // 0.0–1.0
	Missing     []string // missing elements: "trigger_phrases", "temporal_position", "product_keywords", "length", "trigger_coverage"
	Suggestions []string // human-readable improvement suggestions
}

// actionVerbs are verbs that indicate what the skill does.
// Bilingual: Chinese verbs are common in MacLaw's Chinese-first ecosystem.
var actionVerbs = []string{
	// Chinese
	"生成", "转换", "查询", "部署", "安装", "创建", "下载", "上传",
	"分析", "提取", "合并", "拆分", "压缩", "解压", "翻译", "搜索",
	"导出", "导入", "渲染", "编译", "运行", "测试", "构建", "发布",
	// English
	"generate", "convert", "query", "deploy", "install", "create",
	"download", "upload", "analyze", "extract", "merge", "split",
	"compress", "translate", "search", "export", "import", "render",
	"compile", "run", "test", "build", "publish", "transform",
}

// temporalWords indicate when the skill should be used.
var temporalWords = []string{
	"之前", "之后", "当", "在…前", "在…后", "完成后", "开始前",
	"before", "after", "when", "prior to", "following", "once",
}

// EvaluateDescription scores a skill description on 5 dimensions.
// Each dimension contributes 0.2 to the total score (max 1.0).
//
// Scoring is purely data-driven (string matching), no LLM calls.
// The triggers parameter enables cross-validation: if triggers mention
// "pdf" but description doesn't, that's a coverage gap.
func EvaluateDescription(description string, triggers []string) DescriptionQuality {
	q := DescriptionQuality{}
	lower := strings.ToLower(description)

	// Dimension 1: Length (>= 20 chars)
	if utf8.RuneCountInString(description) >= 20 {
		q.Score += 0.2
	} else {
		q.Missing = append(q.Missing, "length")
		q.Suggestions = append(q.Suggestions, "描述过短，建议至少 20 个字符，说明 skill 的具体功能")
	}

	// Dimension 2: Action verbs
	hasVerb := false
	for _, v := range actionVerbs {
		if strings.Contains(lower, strings.ToLower(v)) {
			hasVerb = true
			break
		}
	}
	if hasVerb {
		q.Score += 0.2
	} else {
		q.Missing = append(q.Missing, "trigger_phrases")
		q.Suggestions = append(q.Suggestions, "建议包含动作动词（如「生成」、「转换」、「deploy」），帮助 LLM 判断何时调用")
	}

	// Dimension 3: Temporal position
	hasTemporal := false
	for _, t := range temporalWords {
		if strings.Contains(lower, strings.ToLower(t)) {
			hasTemporal = true
			break
		}
	}
	if hasTemporal {
		q.Score += 0.2
	} else {
		q.Missing = append(q.Missing, "temporal_position")
		q.Suggestions = append(q.Suggestions, "建议说明使用时机（如「在部署之前」、「当需要转换格式时」）")
	}

	// Dimension 4: Product/format keywords (at least one specific noun)
	hasProduct := containsSpecificNoun(lower)
	if hasProduct {
		q.Score += 0.2
	} else {
		q.Missing = append(q.Missing, "product_keywords")
		q.Suggestions = append(q.Suggestions, "建议包含具体的产品名、格式名或平台名（如 PDF、Cloudflare、Docker）")
	}

	// Dimension 5: Trigger-description coverage
	if len(triggers) == 0 {
		q.Score += 0.2 // no triggers to validate against
	} else {
		covered := 0
		for _, t := range triggers {
			if strings.Contains(lower, strings.ToLower(t)) {
				covered++
			}
		}
		coverage := float64(covered) / float64(len(triggers))
		if coverage >= 0.5 {
			q.Score += 0.2
		} else {
			q.Missing = append(q.Missing, "trigger_coverage")
			q.Suggestions = append(q.Suggestions,
				"triggers 中的关键词在 description 中覆盖率不足 50%，建议在描述中提及 triggers 的关键词")
		}
	}

	return q
}

// containsSpecificNoun checks if text contains product names, file formats,
// or platform names that indicate specificity (not vague).
func containsSpecificNoun(lower string) bool {
	specifics := []string{
		// File formats
		"pdf", "pptx", "ppt", "docx", "xlsx", "csv", "json", "xml",
		"yaml", "yml", "markdown", "html", "svg", "png", "jpg", "mp3",
		"mp4", "wav", "drawio", "mermaid",
		// Platforms
		"docker", "kubernetes", "k8s", "cloudflare", "vercel", "aws",
		"azure", "gcp", "github", "gitlab", "npm", "pip", "cargo",
		// Tools
		"ffmpeg", "imagemagick", "pandoc", "latex", "node", "python",
		"golang", "rust", "cmake", "webpack", "vite",
	}
	for _, s := range specifics {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// SuggestDescriptionImprovement generates a one-line improvement suggestion
// based on the skill's structure. No LLM call — pure template.
func SuggestDescriptionImprovement(skill DescriptionSkillInfo) string {
	parts := []string{}

	if len(skill.Steps) > 0 {
		actions := map[string]bool{}
		for _, s := range skill.Steps {
			actions[s] = true
		}
		if actions["bash"] {
			parts = append(parts, "执行脚本")
		}
		if actions["craft_tool"] {
			parts = append(parts, "动态生成代码")
		}
	}

	if len(skill.Triggers) > 0 {
		parts = append(parts, "关键词: "+strings.Join(skill.Triggers, "/"))
	}

	if len(parts) == 0 {
		return ""
	}
	return "建议描述: " + strings.Join(parts, "，")
}

// DescriptionSkillInfo is a minimal interface to avoid importing full NLSkillEntry.
type DescriptionSkillInfo struct {
	Steps    []string // action names
	Triggers []string
}
