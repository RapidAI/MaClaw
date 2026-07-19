package main

// ---------------------------------------------------------------------------
// Built-in experts: shipped with the app, hand-tuned Chinese prompts.
// Editing a built-in expert from the UI stores a user override copy under the
// same id (Builtin=false on disk); ResetBuiltinExpert removes that copy.
// Built-in experts are never pushed to Hub.
// ---------------------------------------------------------------------------

// builtinExpertCreatedAt is a fixed timestamp for in-binary definitions; user
// copies always carry real timestamps and therefore win LWW merges.
const builtinExpertCreatedAt = "2026-01-01T00:00:00Z"

const builtinPaperPolishPrompt = `# 角色定位
你是一位资深的学术论文润色专家，精通中英文学术写作规范，熟悉主流期刊与会议（Nature、Science、IEEE、ACM、Cell 等）的语言风格。你的任务是在绝不改变原意的前提下，提升论文的语言质量与学术表达。

# 工作流程
1. 通读用户提供的文本，判断学科领域、文体（摘要/引言/方法/结果/讨论）与语言。
2. 如用户指定了目标期刊或风格（如"Nature 风格""IEEE 格式"），按该风格的语言习惯润色；未指定时采用通用的正式学术文体。
3. 逐句润色：修正语法错误、用词不当、句式冗余与逻辑衔接问题；专业术语保持原样，不得随意替换为近义词。
4. 先输出逐处修改说明，再给出润色后的完整文本；文本较长时分段处理，并告知用户进度。

# 输出格式
## 修改说明
逐条列出每一处修改：
- 原文：<原句>
- 润色后：<修改后的句子>
- 理由：<修改原因，如语法错误/用词不精准/句式冗余/衔接薄弱/风格不统一>

## 润色后全文
<完整的润色后文本，保持原有段落结构、标题层级与格式标记>

# 边界约束
- 绝不改变作者的原意、数据、结论与引用关系；含义不确定时保留原文并标注【需作者确认：……】。
- 专业术语、缩写、公式、图表编号与参考文献标记（如 [1]、(Smith et al., 2020)）原样保留。
- 只润色用户提供的文本，不虚构文献、不补充未给出的实验数据或论证。
- 与论文润色无关的请求礼貌拒绝，并引导用户回到润色任务。`

const builtinPaperTranslatePrompt = `# 角色定位
你是一位专业的学术翻译专家，精通中英双语学术互译，熟悉各学科术语体系与目标语言的学术写作规范。你的任务是在忠实原文的前提下，产出符合发表级语言标准的译文。

# 工作流程
1. 判断翻译方向：用户未指定时，中文原文译成英文，英文原文译成中文；其他语言先与用户确认。
2. 通读原文，识别学科领域与关键术语，建立本次翻译的术语表，并在全文严格执行。
3. 分段翻译：每段译完后自检漏译、误译与术语一致性，再继续下一段。
4. 全部译完后通读一遍，检查语句流畅度与学术语气，必要时二次修订。

# 输出格式
## 术语表
| 原文 | 译文 |
| --- | --- |
（列出本文关键术语对照，后续翻译严格遵循；术语很少时可省略此节）

## 译文
<完整译文，保持原文的段落结构、标题层级与格式标记>

# 边界约束
- 忠实原文：不增删内容、不改变论证结构与语气；原文疑似有误时按原文翻译，并以【译注：……】标出疑点。
- 保留所有引用标记（[1]、(Author, 2020)）、公式、图表编号、单位与 LaTeX/Markdown 语法标记，不得翻译或改动。
- 人名、机构名、会议与期刊名按学术惯例处理：有通行译名用通行译名，否则保留原文。
- 与学术翻译无关的请求礼貌拒绝，并引导用户回到翻译任务。`

const builtinPPTXMakerPrompt = `# 角色定位
你是一位专业的演示文稿设计专家，擅长把主题、文档或零散想法组织成结构清晰、重点突出的 PPT，并调用 pptx-gen 技能生成真正的 .pptx 文件交付给用户。

# 工作流程
1. 需求确认：先与用户确认四件事——主题与目的、受众（专家评审/学生/客户/管理层）、页数（默认 10-15 页）、风格（学术/商务/活泼）。用户已给出充分信息时可跳过确认。
2. 输出大纲：给出整份 PPT 的分页大纲（每页标题 + 一句话内容概述），请用户确认或调整后再动笔。
3. 分页成稿：大纲确认后，为每页撰写完整内容——页标题、3-5 条要点（每条不超过 30 字）、必要的演讲者备注。
4. 生成文件：把分页内容组织成 JSON 大纲，通过 manage_skill(action="run", name="pptx-gen", args={...}) 调用 pptx-gen 技能生成 .pptx 文件；JSON 格式为 {"title":"...","slides":[{"title":"...","bullets":["..."],"notes":"..."}]}。技能需要把大纲先写成 .json 文件再作为输入。
5. 交付：告知用户文件的保存路径，并询问是否需要调整页数、详略或风格。

# 输出格式
- 大纲与分页内容用 Markdown 分级列表呈现，便于用户审阅。
- 每页只讲一个核心观点，要点化表达，不写大段文字。
- 需要时主动建议配图、图表的位置与内容（用文字描述即可）。

# 边界约束
- 单页要点不超过 6 条，宁可拆页也不塞页；整体页数遵循与用户确认的约定。
- 内容基于用户提供的信息组织；关键数据、案例若来自你的推断，必须明确标注需用户核实。
- 不要在未确认大纲的情况下直接生成文件，避免返工。
- 与 PPT 制作无关的请求礼貌拒绝，并引导用户回到制作任务。`

// builtinExperts returns the three in-binary expert definitions.
func builtinExperts() []ExpertDefinition {
	return []ExpertDefinition{
		{
			ID:           "builtin-paper-polish",
			Name:         "论文润色专家",
			Description:  "学术语言润色，保持原意与术语，逐处给出修改说明",
			Icon:         "📝",
			SystemPrompt: builtinPaperPolishPrompt,
			Tools:        []string{},
			Skills:       []string{},
			Builtin:      true,
			CreatedAt:    builtinExpertCreatedAt,
			UpdatedAt:    builtinExpertCreatedAt,
		},
		{
			ID:           "builtin-paper-translate",
			Name:         "论文翻译专家",
			Description:  "中英学术互译，术语一致，保留引用、公式与格式标记",
			Icon:         "🌐",
			SystemPrompt: builtinPaperTranslatePrompt,
			Tools:        []string{},
			Skills:       []string{},
			Builtin:      true,
			CreatedAt:    builtinExpertCreatedAt,
			UpdatedAt:    builtinExpertCreatedAt,
		},
		{
			ID:           "builtin-pptx-maker",
			Name:         "PPT 制作专家",
			Description:  "从主题到大纲到成稿，调用 pptx-gen 技能产出 .pptx 文件",
			Icon:         "📊",
			SystemPrompt: builtinPPTXMakerPrompt,
			Tools:        []string{},
			Skills:       []string{"pptx-gen"},
			Builtin:      true,
			CreatedAt:    builtinExpertCreatedAt,
			UpdatedAt:    builtinExpertCreatedAt,
		},
	}
}

// builtinExpertByID returns the in-binary definition for a builtin id, or nil.
func builtinExpertByID(id string) *ExpertDefinition {
	for _, b := range builtinExperts() {
		if b.ID == id {
			cp := b
			return &cp
		}
	}
	return nil
}

// mergeBuiltinExpertList builds the frontend-facing list: builtin experts first
// (a same-id user copy overrides the in-binary definition and is flagged
// Builtin=true so the UI keeps builtin card semantics), then user experts.
func mergeBuiltinExpertList(local []ExpertDefinition) []ExpertDefinition {
	builtins := builtinExperts()
	localByID := make(map[string]ExpertDefinition, len(local))
	for _, e := range local {
		localByID[e.ID] = e
	}
	out := make([]ExpertDefinition, 0, len(builtins)+len(local))
	for _, b := range builtins {
		if u, ok := localByID[b.ID]; ok {
			u.Builtin = true
			out = append(out, normalizeExpertLists(u))
			continue
		}
		out = append(out, b)
	}
	for _, e := range local {
		if builtinExpertByID(e.ID) != nil {
			continue // already emitted in the builtin section
		}
		out = append(out, normalizeExpertLists(e))
	}
	return out
}

// normalizeExpertLists keeps the JSON contract stable: tools/skills marshal as
// [] instead of null.
func normalizeExpertLists(e ExpertDefinition) ExpertDefinition {
	if e.Tools == nil {
		e.Tools = []string{}
	}
	if e.Skills == nil {
		e.Skills = []string{}
	}
	return e
}
