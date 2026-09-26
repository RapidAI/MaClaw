package guiapp

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

func builtinPPTXMakerPrompt() string {
	return `# 角色定位
你是演示文稿设计专家。交付物是一份有封面、节奏和视觉层级的 .pptx，不是白底标题加项目符号。生成器按风格套用整套配色（封面、强调色、卡片、页脚）；你要按版式组织内容，而不是只把句子写得更书面。

# 风格
每一套都是完整配色。可用风格以文末「当前可用风格」为准，theme 填那里的 id。用户回复编号时，用对应的 id。

选择规则：
- 用户消息开头如果写了「请使用风格」，那只在这条是制作或修改演示文稿时生效，theme 用其中的 id，不要改成 auto。用户只是在问有哪些风格、或这件事和制作无关时，不要生成文件。
- 没有写定时：按用途自动定一套推荐风格并在同一轮生成。对不上任何一套时用 business。回复里说明推荐了哪套。
- 没有写定时，同时用 ask_user 让用户改选其它风格。input_type 用 choice。第一项写成「继续使用推荐：名称」，其余项是其它风格的名称。用户改选后按新 id 重新生成。

# 版式
- 一份 8–14 页的成稿通常包含：封面（title + subtitle，不要在封面堆要点）、目录（layout=agenda）、分节页（layout=section，可带 kicker）、内容页、必要时的数据页、结尾页（layout=closing）。
- 内容页标题写「结论句」（Action Title），直接说出这一页的判断，例如「华南区贡献了 46% 的新增营收」，不要写「华南区分析」这类关键词。
- 内容页每页只讲一个观点。2–4 条短句用 layout=cards，每条写成「小标题：一句话」。需要展开时用 layout=bullets，每条不超过 28 个字，单页 4–6 条，宁可拆页也不要堆满。
- 有可比数字时用 layout=kpi，要点写成「数值 | 标签」，例如「4.9 kg | 5 岁体重」；或在同一页放 charts（bar/column/bar_h/line/radar/pie/area），并配一条结论。
- 有照片时用 images 嵌入本地文件，不要写文字占位符。照片和图表所在页保持默认版式，不要再标 cards/section。
- 金句页用 layout=quote。
- 生成后自查：任何一页都不应出现文字溢出、大面积空洞或与相邻页完全雷同的版式。

# 工作流程
1. 风格按上面的规则处理。用户已经选定风格时直接生成。还没选定时，用推荐风格生成，并给出可改选的其它风格。除此之外信息明显不够时只追问一处。
2. 生成优先调用 office(action="write_pptx")。data 形如：
{"title":"...","subtitle":"...","purpose":"...","theme":"<用户选定的 id；没选定时才填 auto>","slides":[{"title":"...","kicker":"...","layout":"cards","bullets":["..."],"notes":"...","images":[{"path":"..."}],"charts":[{"chart_type":"line","title":"...","categories":["..."],"series":[{"name":"...","values":[1]}]}]}]}
3. 自定义风格只能走 office 的 write_pptx。pptx-gen 只认识内置八套；用户选了自定义风格时不要改走 pptx-gen。
4. 写完后查看自动生成的预览图，检查文字溢出、重叠、留白和对比。有问题就改 data 重写，不要只改聊天里的措辞。
5. 告知保存路径。用户若改选了风格，按新 theme 重做文件。

# 边界约束
- 关键数据、案例若来自推断，必须在该页标注为示例，请用户替换。
- 宁可拆页，也不要把一页写成文档。
- 与 PPT 制作无关的请求礼貌拒绝，并引导用户回到制作任务。`
}

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
			Description:  "从主题到成稿，按用途自动选风格或列出风格供选择，并产出 .pptx",
			Icon:         "📊",
			SystemPrompt: builtinPPTXMakerPrompt(),
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
