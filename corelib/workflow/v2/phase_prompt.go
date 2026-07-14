package v2

import (
	"fmt"
	"sort"
	"strings"
)

// documentParsingGuidance is the shared instruction block injected into any
// phase where the user provides a document file path. All input-driven phases
// share this single definition — add/fix parsing methods here once.
const documentParsingGuidance = `
## 文档解析方法

用户提供了文件路径，请根据文件扩展名选择解析方式：

### .docx 文件
bash(command="python -c \"from docx import Document; doc=Document(r'路径'); print('\\n'.join(p.text for p in doc.paragraphs))\"")
- 如果 python-docx 未安装：bash(command="pip install python-docx && python -c ...")
- 注意：Windows 上用 python，Linux/Mac 上用 python3；如果一个不行换另一个

### .doc 文件（旧格式 Word 97-2003）
Windows（需要已安装 Word 或 WPS）：
bash(command="python -c \"import win32com.client,os; w=win32com.client.Dispatch('Word.Application'); w.Visible=0; d=w.Documents.Open(os.path.abspath(r'路径')); print(d.Content.Text); d.Close(); w.Quit()\"")
- 如果 win32com 未安装：bash(command="pip install pywin32 && python -c ...")
- 备选（需要 LibreOffice）：bash(command="pip install doc2docx && python -c \"from doc2docx import convert; convert(r'路径')\"") 然后用 python-docx 读取生成的 .docx
- 如果都失败：提示用户用 Word 将 .doc 另存为 .docx 格式后重新提供

### .pdf 文件
bash(command="pip install pymupdf && python -c \"import fitz; doc=fitz.open(r'路径'); print('\\n'.join(page.get_text() for page in doc))\"")

### .txt/.md 文件
直接使用 read_file 工具

### 已安装的文档解析 Skill
如果有已安装的文档解析类 Skill（如 doc-parser、any2pdf 等），优先使用 manage_skill(action="run") 调用。

### 重要提示
- Windows 路径在 Python 中用 r'原始字符串' 避免反斜杠转义问题
- 如果 python 命令不存在尝试 python3，反之亦然
- 如果所有方法都失败，告知用户将文件转换为 .docx 或 .txt 格式后重新提供
- 【严禁】说"无法读取文件"或"无法访问本地文件"——你有 bash 工具可以用 Python 解析文档
`

const (
	fullDependencyRuneBudget      = 30000
	fullDependencyTotalRuneBudget = 60000
)

// BuildPhasePrompt constructs the system prompt injection for the current phase.
func BuildPhasePrompt(state *WorkflowState) string {
	if state == nil {
		return ""
	}
	phase := state.ActivePhase()
	if phase == nil {
		return ""
	}

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## 当前任务\n\n你正在执行「%s」工作流的「%s」阶段。\n\n", state.Type, phase.Name))
	sb.WriteString(fmt.Sprintf("用户需求：%s\n\n", state.Summary))

	// Resolve effective project path: prefer output_dir from first phase's form data
	// over a useless default like "." which resolves to the app's working directory.
	effectivePath := state.ProjectPath
	if effectivePath == "" || effectivePath == "." {
		if od := resolveOutputDirFromState(state); od != "" {
			effectivePath = od
		}
	}
	if effectivePath != "" && effectivePath != "." {
		sb.WriteString(fmt.Sprintf("项目路径：%s\n\n", effectivePath))
	}

	// Previous phase outputs — strategy depends on dependency declaration.
	//
	// DependsOnFull phases: the current phase has a hard data dependency on the
	// prior output. Inject that authoritative state directly. Do not make the
	// model discover an alleged file on disk: phase output is guaranteed by the
	// state machine, whereas UI delivery and project-file persistence are
	// asynchronous integration concerns. This keeps phase handoff deterministic.
	//
	// Non-DependsOnFull phases: inject a short 500-rune summary (enough for
	// context awareness without polluting the prompt).
	fullDepSet := make(map[string]bool, len(phase.DependsOnFull))
	for _, dep := range phase.DependsOnFull {
		fullDepSet[dep] = true
	}
	// A declared full dependency is a correctness precondition. Do not silently
	// fall back to generic project search or a partial summary when a prior phase
	// was skipped, failed, or its output was not recorded.
	missingDependencies := MissingFullDependencies(state)
	if len(missingDependencies) > 0 {
		sort.Strings(missingDependencies)
		sb.WriteString("## 前序产出物不可用\n\n")
		sb.WriteString(fmt.Sprintf("本阶段依赖以下已确认的完整产出物，但当前工作流状态中未找到：%s。\n", strings.Join(missingDependencies, "、")))
		sb.WriteString("不要搜索项目目录、PDF、记忆或其他交付文件来猜测内容；请停止生成并要求恢复或重新执行缺失的前序阶段。\n")
		return sb.String()
	}
	remainingFullBudget := fullDependencyTotalRuneBudget
	hasPrevOutputs := false
	for i := 0; i < state.CurrentPhase && i < len(state.Phases); i++ {
		if state.Phases[i].Output != "" {
			hasPrevOutputs = true
			break
		}
	}
	if hasPrevOutputs {
		sb.WriteString("## 前序阶段产出物\n\n")
		for i := 0; i < state.CurrentPhase && i < len(state.Phases); i++ {
			p := state.Phases[i]
			if p.Output == "" {
				continue
			}
			output := stripBase64DataURLs(p.Output)
			if fullDepSet[p.ID] {
				// Full dependency: retain a deliberately generous, deterministic
				// context budget. The remaining text is explicitly marked as a
				// transport limit rather than directing the model to search for files.
				outputRunes := []rune(output)
				runeCount := len(outputRunes)
				fullBudget := fullDependencyRuneBudget
				if fullBudget > remainingFullBudget {
					fullBudget = remainingFullBudget
				}
				if fullBudget > runeCount {
					fullBudget = runeCount
				}
				remainingFullBudget -= fullBudget
				fullOutput := string(outputRunes[:fullBudget])
				truncated := runeCount > fullBudget

				if !truncated {
					sb.WriteString(fmt.Sprintf("### %s（完整，%d字）\n%s\n\n", p.Name, runeCount, fullOutput))
				} else {
					sb.WriteString(fmt.Sprintf("### %s（%d字；当前上下文载入前%d字）\n%s\n...(受上下文传输上限截断；不得搜索或转换其他交付文件来替代该阶段产物)\n\n", p.Name, runeCount, fullBudget, fullOutput))
				}
			} else {
				// Default: truncate to 500 runes summary
				runes := []rune(output)
				if len(runes) > 500 {
					output = string(runes[:500]) + "\n...(摘要)"
				}
				sb.WriteString(fmt.Sprintf("### %s（摘要）\n%s\n\n", p.Name, output))
			}
		}
	}

	// Inject form data as structured context (when phase has InputSchema + user submitted form)
	if phase.FormData != nil && len(phase.FormData) > 0 {
		sb.WriteString("## 用户提供的结构化信息（必须使用，禁止再询问）\n\n")
		sb.WriteString("以下信息由用户通过表单提交，请**直接基于这些信息**生成本阶段文档。禁止向用户重复索要这些已提供的信息：\n\n")
		// Inject active variant label (tells the LLM which input mode to use).
		if phase.InputSchema != nil {
			if variantID, ok := phase.FormData["_agent_view_variant"]; ok && variantID != nil {
				vid := fmt.Sprintf("%v", variantID)
				for _, v := range phase.InputSchema.Variants {
					if v.ID == vid {
						sb.WriteString(fmt.Sprintf("- **输入方式**：%s\n", v.Label))
						break
					}
				}
			}
		}
		sb.WriteString(RenderFormDataFields(phase, true))
		sb.WriteString("\n")
	}

	// Keep information collected by earlier form phases available to every later
	// phase. FormData belongs to the phase that collected it, so looking only at
	// the active phase loses inputs such as a PPT's topic, audience, page count,
	// and style as soon as the workflow advances. The rendered values preserve
	// the same sensitive-field masking as active-phase form data.
	if state.CurrentPhase > 0 {
		hasPriorFormData := false
		for i := 0; i < state.CurrentPhase && i < len(state.Phases); i++ {
			if len(state.Phases[i].FormData) > 0 {
				hasPriorFormData = true
				break
			}
		}
		if hasPriorFormData {
			sb.WriteString("## 工作流已收集的结构化信息（必须继承，禁止重复询问）\n\n")
			sb.WriteString("以下信息已在前序阶段由用户确认；请直接继承并使用，除非用户明确要求修改。\n\n")
			for i := 0; i < state.CurrentPhase && i < len(state.Phases); i++ {
				prior := &state.Phases[i]
				if len(prior.FormData) == 0 {
					continue
				}
				name := strings.TrimSpace(prior.Name)
				if name == "" {
					name = prior.ID
				}
				sb.WriteString(fmt.Sprintf("### %s\n", name))
				sb.WriteString(RenderFormDataFields(prior, true))
				sb.WriteString("\n")
			}
		}
	}

	// Inject supplementary documents as reference context for LLM generation.
	// These are optional materials (research plans, publication lists, etc.) that
	// the user uploaded alongside the form. Injected in all phases after Phase 1
	// (which collects them) so the LLM has research direction context.
	if len(state.SupplementaryDocs) > 0 {
		sb.WriteString("## 用户提供的补充参考材料\n\n")
		sb.WriteString("以下文档由用户上传作为参考，请在生成本阶段内容时充分利用这些材料中的信息：\n\n")
		totalBudget := 10000 // total rune budget across all supplementary docs
		perDocBudget := totalBudget / len(state.SupplementaryDocs)
		if perDocBudget > 4000 {
			perDocBudget = 4000
		}
		// Sort by file name for deterministic prompt output
		suppNames := make([]string, 0, len(state.SupplementaryDocs))
		for name := range state.SupplementaryDocs {
			suppNames = append(suppNames, name)
		}
		sort.Strings(suppNames)
		for _, name := range suppNames {
			content := state.SupplementaryDocs[name]
			sb.WriteString(fmt.Sprintf("### %s\n\n", name))
			runes := []rune(content)
			if len(runes) > perDocBudget {
				sb.WriteString(string(runes[:perDocBudget]))
				sb.WriteString("\n\n...(内容过长已截断)\n\n")
			} else {
				sb.WriteString(content)
				sb.WriteString("\n\n")
			}
		}
	}

	// Auto-inject document parsing guidance when form data contains file paths.
	// This is the mechanism-level fix: any phase that receives a file path from
	// the user automatically gets parsing instructions — no need to repeat them
	// in each phaseInstruction case.
	//
	// Two detection paths:
	// 1. Form data contains a file path field/value (user submitted via InputSchema form)
	// 2. No form data but state.Summary contains a file path (user typed path in chat)
	needsParsingGuidance := formDataContainsFilePath(phase.FormData)
	if !needsParsingGuidance && (phase.FormData == nil || len(phase.FormData) == 0) {
		// Check if the user's original request message contains a file path.
		needsParsingGuidance = textContainsFilePath(state.Summary)
	}
	if needsParsingGuidance && !phaseInstructionHasOwnParsingGuidance(phase.ID) {
		sb.WriteString(documentParsingGuidance)
		sb.WriteString("\n")
	}

	// Artifact generation guidance is driven by phase semantics, not by a
	// workflow-specific phase ID. New templates can opt in by setting
	// Kind=artifact_generation or MutationScope=artifact.
	if isArtifactGenerationPhase(state.Type, phase) && !phaseInstructionHasOwnArtifactGuidance(WorkflowType(state.Type), phase.ID) {
		sb.WriteString(genericArtifactGenerationGuidance(phase))
		sb.WriteString("\n")
	}

	// Phase-specific instructions
	sb.WriteString(phaseInstruction(WorkflowType(state.Type), phase.ID))

	return sb.String()
}

// MissingFullDependencies returns the hard dependencies of the active phase
// that have no recorded prior output. Callers must block execution when this is
// non-empty; generic project search is not a substitute for workflow state.
func MissingFullDependencies(state *WorkflowState) []string {
	if state == nil || state.ActivePhase() == nil {
		return nil
	}
	phase := state.ActivePhase()
	missing := make([]string, 0, len(phase.DependsOnFull))
	for _, dep := range phase.DependsOnFull {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		found := false
		for i := 0; i < state.CurrentPhase && i < len(state.Phases); i++ {
			if state.Phases[i].ID == dep && strings.TrimSpace(state.Phases[i].Output) != "" {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, dep)
		}
	}
	sort.Strings(missing)
	return missing
}

// RenderFormDataFields renders the FormData key-value pairs as a bullet list.
// This is the single implementation used by both:
// - BuildPhasePrompt (system prompt injection)
// - GUI's buildFormDataInlinedUserText (user message injection)
//
// boldLabels: when true, renders "- **Label**：value" (system prompt style);
// when false, renders "- Label：value" (user message style).
func RenderFormDataFields(phase *Phase, boldLabels bool) string {
	if phase.FormData == nil || len(phase.FormData) == 0 {
		return ""
	}
	var sb strings.Builder

	renderField := func(label string, value interface{}, sensitive bool) {
		s := fmt.Sprintf("%v", value)
		if s == "" || s == "<nil>" {
			return
		}
		if sensitive {
			s = "已填写（敏感信息已隐藏）"
		}
		if boldLabels {
			sb.WriteString(fmt.Sprintf("- **%s**：", label))
		} else {
			sb.WriteString(fmt.Sprintf("- %s：", label))
		}
		// Multi-line values: indent continuation lines to preserve bullet structure.
		if strings.Contains(s, "\n") {
			lines := strings.Split(s, "\n")
			sb.WriteString(lines[0])
			sb.WriteString("\n")
			for _, line := range lines[1:] {
				if strings.TrimSpace(line) == "" {
					continue
				}
				sb.WriteString("  " + line + "\n")
			}
		} else {
			sb.WriteString(s)
			sb.WriteString("\n")
		}
	}

	if phase.InputSchema != nil {
		allFields := make([]PhaseInputField, 0, len(phase.InputSchema.Fields)+8)
		allFields = append(allFields, phase.InputSchema.Fields...)
		if variantID, ok := phase.FormData["_agent_view_variant"]; ok && variantID != nil {
			vid := fmt.Sprintf("%v", variantID)
			for _, v := range phase.InputSchema.Variants {
				if v.ID == vid {
					allFields = append(allFields, v.Fields...)
					break
				}
			}
		}
		for _, f := range allFields {
			if f.Name == "" || strings.HasPrefix(f.Name, "_") {
				continue
			}
			value, ok := phase.FormData[f.Name]
			if !ok || value == nil || fmt.Sprintf("%v", value) == "" {
				continue
			}
			label := f.Label
			if label == "" {
				label = f.Name
			}
			renderField(label, value, f.Sensitive)
		}
	} else {
		for key, value := range phase.FormData {
			if key == "" || strings.HasPrefix(key, "_") {
				continue
			}
			renderField(key, value, false)
		}
	}
	return sb.String()
}

// formDataContainsFilePath returns true if any value in the form data looks like
// a file path (Windows drive letter path, Unix absolute path, or common document
// extensions). This triggers automatic injection of documentParsingGuidance.
func formDataContainsFilePath(formData map[string]interface{}) bool {
	if len(formData) == 0 {
		return false
	}
	for key, val := range formData {
		if key == "" || strings.HasPrefix(key, "_") {
			continue
		}
		s := fmt.Sprintf("%v", val)
		if s == "" || s == "<nil>" {
			continue
		}
		// Signal 1: field name contains "path" or "file" AND value is non-trivial.
		// This is the strongest signal — template authors name file fields explicitly.
		kl := strings.ToLower(key)
		if strings.Contains(kl, "path") || strings.Contains(kl, "file") {
			if len(s) > 2 {
				return true
			}
		}
		// Signal 2: value looks like a Windows drive-letter path (D:\... or C:/...).
		if len(s) >= 4 && ((s[0] >= 'A' && s[0] <= 'Z') || (s[0] >= 'a' && s[0] <= 'z')) &&
			s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
			return true
		}
		// Signal 3: value looks like a Unix absolute path with depth (at least 2 segments).
		// "/home/user/file.pdf" matches; "/yes" does not (too short, no depth).
		if len(s) >= 5 && s[0] == '/' && strings.Count(s, "/") >= 2 {
			// Exclude patterns that are clearly not paths (e.g. URLs handled by web_fetch).
			if !strings.HasPrefix(s, "//") && !strings.Contains(s, "://") {
				return true
			}
		}
		// Signal 4: value ends with a document file extension.
		sl := strings.ToLower(s)
		for _, ext := range []string{".docx", ".doc", ".pdf", ".pptx", ".ppt", ".xlsx", ".xls"} {
			if strings.HasSuffix(sl, ext) {
				return true
			}
		}
	}
	return false
}

// phaseInstructionHasOwnParsingGuidance returns true if the phase-specific
// instruction already includes document parsing methods, in which case the
// shared documentParsingGuidance should be skipped to avoid redundancy.
func phaseInstructionHasOwnParsingGuidance(phaseID string) bool {
	switch phaseID {
	case "pa_disclosure_parsing", "us_disclosure_analysis":
		return true
	}
	return false
}

func isArtifactGenerationPhase(workflowType string, phase *Phase) bool {
	if phase == nil {
		return false
	}
	if phase.Kind == PhaseKindArtifactGeneration || phase.MutationScope == MutationScopeArtifact {
		return true
	}
	kind, mutationScope, _ := phaseMetadataSemantics(WorkflowType(workflowType), CanonicalPhaseID(phase.ID))
	return kind == PhaseKindArtifactGeneration || mutationScope == MutationScopeArtifact
}

func phaseInstructionHasOwnArtifactGuidance(workflowType WorkflowType, phaseID string) bool {
	// Instead of hardcoding phase IDs, detect from the instruction content itself.
	// A phase instruction that already mentions artifact-generation tool chains
	// (manage_skill, craft_tool, send_file) doesn't need the generic guidance.
	instruction := phaseInstruction(workflowType, phaseID)
	if instruction == "" {
		return false
	}
	lower := strings.ToLower(instruction)
	// If the instruction mentions both a tool-invocation method AND a delivery method,
	// it has its own artifact generation guidance.
	hasToolChain := strings.Contains(lower, "manage_skill") || strings.Contains(lower, "craft_tool")
	hasDelivery := strings.Contains(lower, "send_file") || strings.Contains(lower, "send_to_im") ||
		strings.Contains(lower, ".pptx") || strings.Contains(lower, ".docx")
	return hasToolChain && hasDelivery
}

func genericArtifactGenerationGuidance(phase *Phase) string {
	name := "当前"
	if phase != nil && strings.TrimSpace(phase.Name) != "" {
		name = strings.TrimSpace(phase.Name)
	}
	return fmt.Sprintf(`## 通用产物生成阶段指令

「%s」是产物生成阶段，完成标准是实际生成并发送可下载文件，而不是只输出 Markdown 文案或生成计划。

执行要求：
- 基于前序阶段产出和用户输入生成最终文件，文件类型以本阶段名称、交付物描述和用户需求为准。
- 优先复用已有产物生成 Skill：manage_skill(action="run", name="...", args={...})。
- 如果合适的 Skill 不存在或不可用，先用 search_and_install_skill 搜索/安装。
- 如果仍不可用，使用 craft_tool 创建本次任务所需的生成工具，再调用该工具生成文件。
- 工具参数保持结构化和简洁，避免把超长全文塞进单个 JSON 字符串导致工具调用截断。
- 成功后必须调用 send_file（桌面展示）或 send_to_im（发到微信/飞书等 IM）发送最终文件；预览 PDF 或中间文件只能作为附加物，不能替代主交付物。
- 只有在已有 Skill、安装 Skill、craft_tool 自建工具都明确失败时，才说明失败原因，并列出真实尝试结果。

禁止事项：
- 禁止只承诺“将生成文件”但不调用工具。
- 禁止只输出内容草稿后停止；本阶段的完成标准是实际文件已生成并发送。
`, name)
}

// textContainsFilePath checks if a plain text string contains a file path
// or document file extension. Used when no form data is available (user typed
// a file path directly in chat).
func textContainsFilePath(text string) bool {
	if len(text) < 4 {
		return false
	}
	// Check for Windows drive-letter paths anywhere in the text.
	for i := 0; i+2 < len(text); i++ {
		c := text[i]
		if ((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) &&
			text[i+1] == ':' && (text[i+2] == '\\' || text[i+2] == '/') {
			// Ensure this is a word boundary (not mid-word like "abc:\dir").
			if i == 0 || text[i-1] == ' ' || text[i-1] == '\n' || text[i-1] == '\t' || text[i-1] >= 0x80 {
				return true
			}
		}
	}
	// Check for document file extensions.
	sl := strings.ToLower(text)
	for _, ext := range []string{".docx", ".doc", ".pdf", ".pptx", ".ppt", ".xlsx", ".xls"} {
		if strings.Contains(sl, ext) {
			return true
		}
	}
	return false
}

func phaseInstruction(workflowType WorkflowType, phaseID string) string {
	// Academic application phases are generated parametrically from FundingProfiles.
	// Check if this phaseID belongs to an academic template before the hardcoded switch.
	// If the factory generates a non-empty instruction, use it. Otherwise fall through
	// to the hardcoded switch (backward compat with old phase IDs from persisted workflows).
	if profile, isAcademic := IsAcademicApplicationPhase(phaseID); isAcademic {
		if instruction := AcademicPhaseInstruction(phaseID, profile); instruction != "" {
			return instruction
		}
	}
	if IsGaokaoApplicationPhase(phaseID) {
		if instruction := GaokaoPhaseInstruction(phaseID); instruction != "" {
			return instruction
		}
	}

	// Disambiguate shared phase IDs by workflow type.
	// Some phase IDs (outline, report, analysis, conclusion, methodology, budget)
	// are reused across templates with different semantics. The disambiguation
	// switch runs BEFORE the main switch. If no type matches here, the main switch
	// provides a sensible default (typically the first template that used the ID).
	switch {
	case phaseID == "outline" && workflowType == WorkflowPaperWriting:
		return paperWritingOutline
	case phaseID == "report" && workflowType == WorkflowComplianceAudit:
		return complianceAuditReport
	case phaseID == "analysis" && workflowType == WorkflowResearchReport:
		return researchReportAnalysis
	case phaseID == "conclusion" && workflowType == WorkflowDueDiligence:
		return dueDiligenceConclusion
	case phaseID == "methodology" && workflowType == WorkflowGrantProposal:
		return grantProposalMethodology
	case phaseID == "budget" && workflowType == WorkflowGrantProposal:
		return grantProposalBudget
	}

	switch phaseID {
	case "requirements":
		return `## 阶段指令

生成需求文档（Markdown 格式），包含：
- 功能需求
- 非功能需求  
- 边界情况
- 验收标准

信息不足的部分标记为「待确认」。直接生成文档，不要先问澄清问题。

## 重要约束（违反将导致错误）
- 只生成一份需求文档，输出完毕后立即停止。
- 【严禁】输出确认提示语、分隔线或任何后续内容。
- 【严禁】在文档后自己说"好的"或模拟用户确认。
- 你只负责输出文档本身，系统会自动提示用户确认。
`
	case "design":
		return `## 阶段指令

基于已确认的需求，生成技术设计文档（Markdown 格式），包含：
- 架构设计
- 技术选型
- 模块划分
- 接口设计
- 数据结构

## 重要约束（违反将导致错误）
- 只生成一份技术设计文档，输出完毕后立即停止。
- 【严禁】输出确认提示语、分隔线或任何后续内容。
- 【严禁】自己模拟用户确认。
- 你只负责输出文档本身，系统会自动提示用户确认。
`
	case "tasks":
		return `## 阶段指令

基于已确认的设计，生成任务拆分文档。使用以下格式（不要使用表格）：

### T1: 任务标题
- **描述**：具体要做什么
- **涉及文件**：file1.cpp, file2.h
- **依赖**：无 / 依赖 T0
- **优先级**：P0/P1/P2
- **工作量**：预估说明

每个任务必须包含描述、涉及文件、依赖、优先级、工作量五个字段。

## 重要约束（违反将导致错误）
- 只生成一份任务拆分文档，输出完毕后立即停止。
- 【严禁】输出确认提示语、分隔线或任何后续内容。
- 【严禁】在同一次回复中开始写代码或执行任何任务。
- 【严禁】自己模拟用户确认。
- 你只负责任务拆分这一步，后续编码由系统自动调度。
`
	case "implementation":
		return `## Coding Implementation Handoff Contract

You are the workflow coordinator for the coding implementation phase.
The main workflow loop must not mutate the project directly. All code writing,
project bootstrapping, file edits, and build-fix work must be delegated to the
internal CodingSubAgent.

Required execution path:
1. Read the confirmed requirements, design, and task breakdown context.
2. Select the next concrete implementation task to execute.
3. Delegate the implementation through delegate_task(agent="coding_workflow", request="...").
4. Use the main loop only for coordination, inspection, and summarizing progress.

Hard rules:
- Do not write code directly in the main workflow loop.
- Do not use local project-mutation tools from the main workflow loop.
- Every project-changing action must go through delegate_task(agent="coding_workflow").
- The delegation request should mention the concrete task, touched files, and acceptance target when possible.

Reference the task breakdown explicitly so CodingSubAgent knows which task to execute next.`

	case "verification":
		return `## 阶段指令

编码执行已完成。现在进行最终验收：

1. 编译整个项目（确认编译通过无错误）
2. 运行程序（确认启动不崩溃）
3. 检查需求文档中的验收标准是否满足
4. 生成验收报告，列出：
   - 编译结果（通过/失败+错误信息）
   - 运行结果（正常启动/崩溃+错误信息）
   - 各验收标准的通过情况
   - 如有问题，给出修复建议

## 重要约束
- 必须实际执行编译和运行命令，不要只描述。
- 如果编译失败，尝试修复后重新编译（最多3次）。
- 生成完验收报告后停止输出。
`

	case "audience_goal":
		return `## 阶段指令

分析 PPT 的目标受众和演讲目标。

## 产出物结构

### 1. 目标受众分析
- 受众身份（职位、部门、决策层级）
- 知识水平（对主题的了解程度）
- 关注点（他们最关心什么）
- 可能的疑虑/反对意见

### 2. 演讲目标
- 演讲类型（说服/教育/汇报/激励）
- 核心信息（一句话能被记住的信息）
- 期望的行动（听众听完后应该做什么）

### 3. 演讲约束
- 时间限制
- 场合（会议室/大会/线上）
- 技术条件（投影/屏幕比例）

### 4. 内容策略
- 信息密度（详细 vs 概要）
- 说服路径（逻辑/情感/案例/数据）
- 视觉风格方向（商务/学术/创意）

## 重要约束（违反将导致错误）
- 只生成一份受众目标分析文档，输出完毕后立即停止。
- 【严禁】输出确认提示语、分隔线或任何后续内容。
- 【严禁】自己模拟用户确认。
`
	case "outline":
		return `## 阶段指令

基于受众和目标，设计 PPT 内容大纲。

## 产出物结构

### PPT 大纲

| 页码 | 标题 | 核心要点 | 视觉建议 |
|------|------|---------|---------|
| 1 | 封面 | 标题+副标题+演讲人 | |
| 2 | 目录/议程 | 主要章节 | |
| 3-N | 内容页 | 每页1个核心观点 | 图表/图片/数据 |
| N+1 | 总结/行动号召 | 关键结论+下一步 | |
| N+2 | Q&A/联系方式 | | |

### 逻辑流设计
- 开场策略（问题/故事/数据/引用）
- 正文逻辑（时间线/问题→方案/对比/递进）
- 结尾策略（总结/呼吁/展望）

### 页数和时间分配
- 预估总页数
- 每页预估讲解时间
- 总时长是否匹配约束

## 重要约束（违反将导致错误）
- 只生成一份大纲文档，输出完毕后立即停止。
- 【严禁】输出确认提示语、分隔线或任何后续内容。
- 【严禁】自己模拟用户确认。
`
	case "slide_scripting":
		return `## 阶段指令

基于大纲，为每一页编写详细脚本。

## 产出物结构

为每一页 PPT 生成完整脚本：

### 第 1 页：[页面标题]
- **页面类型**：封面/目录/内容/过渡/总结
- **展示文字**：页面上实际显示的文字（精炼，不是演讲词）
- **视觉元素**：图表类型/图片建议/图标/动画
- **布局建议**：左右分栏/全图/标题+列表/数据可视化
- **演讲备注**：演讲者在这页要讲什么（口语化，1-3句）
- **过渡语**：到下一页的过渡衔接

### 第 2 页：...
（逐页展开，直到最后一页）

### 全局设计指引
- 主色调/辅助色
- 字体建议
- 图片风格（摄影/插画/图标）

## 重要约束（违反将导致错误）
- 必须覆盖大纲中的每一页，不能遗漏。
- 展示文字必须精炼（每页不超过 50 字），详细内容放演讲备注。
- 只生成一份逐页脚本文档，输出完毕后立即停止。
- 【严禁】输出确认提示语、分隔线或任何后续内容。
- 【严禁】自己模拟用户确认。
`
	case "ppt_generation":
		return `## 阶段指令

这是最终 PPT 产物生成阶段，必须实际生成可下载的 .pptx 文件，不要只输出 Markdown 文案，也不要把最终交付降级为 PDF。

关键约束——分步写入，禁止单次大输出：
由于模型输出长度限制，一次 tool call 中写入超过 3000 字符的内容会被系统截断导致失败。
你必须按以下模式分步执行：
1. 先用 write_file 写入一个 Python 生成脚本（< 2500 字符），该脚本内嵌少量页面数据或读取外部数据。
2. 用 write_file 分多次写入页面数据文件（纯文本格式，每行一条记录），每次控制在 2500 字符以内：
   - 第一次用 write_file 写入前 5-8 页的数据。
   - 后续用 write_file(mode="append") 追加剩余页面（纯文本追加，无 JSON 语法顾虑）。
3. 最后用 bash 执行该脚本。

推荐的数据文件格式（每页用分隔行隔开，避免 JSON 语法错误）：

===SLIDE===
title: 第1页标题
bullet: 要点1
bullet: 要点2
notes: 演讲备注
===SLIDE===
title: 第2页标题
...

示例流程：
- write_file(path="gen_pptx.py", content='...脚本读取slides.txt生成pptx...')  // 短脚本 < 2500字符
- write_file(path="slides.txt", content='===SLIDE===\ntitle: ...\n...')  // 前8页
- write_file(path="slides.txt", mode="append", content='===SLIDE===\ntitle: ...\n...')  // 后续页
- bash(command="pip install python-pptx -q && python gen_pptx.py")

执行要求：
- 基于前序阶段的「受众与目标」「内容大纲」「逐页脚本」生成完整演示文稿。
- 优先复用已有产物生成能力，例如调用 manage_skill(action="run", name="pptx-generator", args={...})。
- 如果现有 PPTX 生成 Skill 不存在或不可用，先尝试 search_and_install_skill 搜索/安装合适的 PPTX 生成 Skill。
- 如果 Skill 也不可用，按上面「分步写入」模式手动创建脚本生成 .pptx。
- 输出文件名使用清晰的 ASCII 或安全中文文件名，扩展名必须是 .pptx。
- 如果工具返回 run_id，使用对应 status 动作轮询直到 completed/failed。
- 成功后调用 send_file（桌面）或 send_to_im（发到微信/飞书）发送生成的 .pptx 文件。
- 只有在已有 Skill、安装 Skill、手动脚本生成都明确失败时，才说明失败原因。

禁止事项：
- 禁止在单次 write_file/bash/craft_tool 的参数中放入超过 2500 字符的内容——会被截断！
- 禁止把完整 Python 脚本（含所有页面数据）塞进一个 tool call。
- 禁止只承诺“将生成 PPT”但不调用工具。
- 禁止只调用 generate_pdf 生成 PDF 作为最终结果。
- 禁止输出“完整 PPT 内容如下”后停止；本阶段的完成标准是实际 .pptx 文件已生成并发送。
`

	case "problem_discovery":
		return `## 阶段指令

分析产品要解决的核心问题。

## 产出物结构

### 1. 目标用户
- 用户群体描述
- 用户规模估算
- 核心特征

### 2. 痛点分析
| 痛点 | 频率 | 严重度 | 当前解决方式 | 不满意原因 |
|------|------|--------|------------|----------|

### 3. 现有解决方案
| 方案 | 提供者 | 优点 | 不足 |
|------|--------|------|------|

### 4. 机会识别
- 市场空白
- 技术趋势带来的新可能
- 用户需求升级方向

### 5. 问题陈述
用一段话总结：谁（用户）在什么场景下遇到什么问题，现有方案为什么不够好。

## 重要约束（违反将导致错误）
- 只生成一份问题发现文档，输出完毕后立即停止。
- 【严禁】输出确认提示语、分隔线或任何后续内容。
- 【严禁】自己模拟用户确认。
`
	case "user_research":
		return `## 阶段指令

基于问题发现，深入用户研究。

## 产出物结构

### 1. 用户画像
| 维度 | 描述 |
|------|------|
| 人口统计 | 年龄/性别/职业/收入 |
| 行为特征 | 使用习惯/消费模式 |
| 目标动机 | 想达成什么/为什么 |
| 痛点场景 | 具体的不满场景 |

### 2. 使用场景
每个场景：
- 场景描述（谁、在哪、做什么）
- 触发条件
- 期望结果
- 当前体验问题

### 3. 需求优先级
| 需求 | 类型(基本/期望/兴奋) | 频率 | 重要度 | 优先级 |
|------|---------------------|------|--------|--------|

### 4. 竞品用户评价分析
- 用户选择竞品的原因
- 用户离开竞品的原因
- 未被满足的诉求

## 重要约束（违反将导致错误）
- 只生成一份用户研究文档，输出完毕后立即停止。
- 【严禁】输出确认提示语、分隔线或任何后续内容。
- 【严禁】自己模拟用户确认。
`
	case "solution_design":
		return `## 阶段指令

基于用户研究，设计产品方案。

## 产出物结构

### 1. 产品定位
- 一句话定位
- 核心价值主张
- 目标用户（从研究中确认）

### 2. 功能架构
| 模块 | 功能 | 优先级 | MVP必备 | 用户价值 |
|------|------|--------|---------|---------|

### 3. 信息架构
- 一级导航
- 核心页面列表
- 页面间关系

### 4. 用户旅程
| 阶段 | 用户行为 | 触点 | 情绪 | 设计机会 |
|------|---------|------|------|---------|

### 5. MVP 定义
- MVP 功能范围（最小集）
- MVP 成功标准
- MVP 后迭代方向

## 重要约束（违反将导致错误）
- 只生成一份方案设计文档，输出完毕后立即停止。
- 【严禁】输出确认提示语、分隔线或任何后续内容。
- 【严禁】自己模拟用户确认。
`
	case "prototype":
		return `## 阶段指令

整合所有研究和设计，输出完整的产品设计文档（PRD）。这是最终交付物。

## 产出物结构（完整产品设计文档）

### 1. 产品概述
- 产品定位
- 目标用户
- 核心价值

### 2. 功能规格
每个核心功能：
- 功能描述
- 用户故事
- 验收标准
- 交互说明
- 异常处理

### 3. 页面设计描述
每个关键页面：
- 页面目的
- 包含元素
- 布局建议
- 交互行为

### 4. 非功能需求
- 性能要求
- 安全要求
- 兼容性要求

### 5. 上线计划
- MVP 范围和时间
- 后续迭代计划

## 重要约束（违反将导致错误）
- 产品设计文档正文不少于 2500 字。
- 只生成一份完整文档，输出完毕后立即停止。
- 【严禁】输出确认提示语、分隔线或任何后续内容。
- 【严禁】自己模拟用户确认。
`

	// --- Patent Analysis Workflow ---

	case "tech_parsing":
		return `## 阶段指令

解析用户提供的专利文档/技术资料，提炼核心技术方案。

根据用户输入方式处理：
- **用户提供了文件路径**：先用上方"文档解析方法"中的 bash + Python 命令提取文本内容，再基于提取的文本进行分析。如果上方没有"文档解析方法"section，则自行用 bash 调 Python 解析（pymupdf 读 PDF、python-docx 读 docx）。
- **用户粘贴了文本内容**：直接基于文本内容分析。
- **用户只提供了专利号/关键词**：使用 web_search 搜索专利全文，获取后分析。

## 生成文档内容

基于获取的文本内容和用户表单信息，生成技术解析文档：
1. **专利/技术概述**：技术领域、核心发明点、申请日/公开日（如有）
2. **权利要求分析**（如为专利文件）：
   - 独立权利要求的技术特征拆解
   - 从属权利要求的附加技术特征
   - 权利要求的保护范围评估
3. **技术方案详解**：
   - 关键技术特征（结构/步骤/参数/材料）
   - 技术特征之间的逻辑关系
   - 与已知技术的区别
4. **技术效果**：有益效果、性能数据（如有）

## 重要约束（违反将导致错误）
- 如果用户提供了文件路径，必须先解析文件获取内容，不要凭文件名猜测内容。
- 技术分析必须基于实际文档内容，不要臆造。
- 只生成一份技术解析文档，输出完毕后立即停止。
- 【严禁】输出确认提示语或后续内容。
- 【严禁】自己模拟用户确认。
`
	case "prior_art":
		return `## 阶段指令

基于技术解析结果，进行现有技术检索和分析。

使用 web_search 搜索相关现有技术：
- 搜索关键技术特征相关的专利和论文
- 搜索竞争对手（如用户提供了）的已公开专利
- 搜索技术领域的背景文献

生成文档内容：
1. **检索策略**：使用的关键词组合、数据库范围
2. **相关现有技术列表**：
   - 专利号/论文标题、申请人/作者、日期
   - 技术内容摘要
   - 与目标专利的相关度评估
3. **技术发展脉络**：该技术领域的发展历程
4. **最接近的现有技术**：识别 1-3 篇最接近的对比文件

## 重要约束
- 使用 web_search 实际搜索，不要凭记忆编造专利号或论文。
- 只生成一份现有技术分析文档，输出完毕后立即停止。
- 【严禁】自己模拟用户确认。
`
	case "infringement":
		return `## 阶段指令

基于技术解析和现有技术分析，进行侵权风险评估。

生成文档内容：
1. **权利要求对比分析**：
   - 逐项对比目标专利权利要求与分析对象的技术特征
   - 标注"相同特征"、"等同特征"、"缺少特征"
2. **侵权判定分析**：
   - 全面覆盖原则分析
   - 等同原则分析
   - 禁止反悔原则考量
3. **风险等级评估**：高/中/低风险，及判定依据
4. **规避设计建议**（如分析目的为规避）：
   - 可替换的技术特征
   - 替换后的技术效果影响

## 重要约束
- 分析必须基于前两个阶段的实际数据。
- 法律分析需谨慎，标注"本分析仅供参考，不构成法律意见"。
- 只生成一份侵权评估文档，输出完毕后立即停止。
- 【严禁】自己模拟用户确认。
`
	case "strategy":
		return `## 阶段指令

基于前序分析结果，提出知识产权策略建议。

根据用户的分析目的（侵权风险评估/专利布局/无效宣告等），生成针对性策略：
1. **总体策略建议**：基于分析结论的行动建议
2. **具体措施**：
   - 短期措施（立即可执行）
   - 中期措施（3-6 个月）
   - 长期措施（6-12 个月）
3. **专利布局建议**（如适用）：
   - 核心专利方向
   - 外围专利方向
   - 防御性专利建议
4. **风险控制**：潜在风险及应对预案
5. **成本估算**（如适用）：各项措施的预估投入

## 重要约束
- 策略必须基于前序阶段的分析数据。
- 标注"本建议仅供参考，重要决策请咨询专利律师"。
- 只生成一份策略建议文档，输出完毕后立即停止。
- 【严禁】自己模拟用户确认。
`
	case "report":
		return `## 阶段指令

整合前序所有阶段的分析结果，生成完整的专利分析报告。

报告结构：
1. **封面信息**：报告标题、分析目的、分析对象、完成日期
2. **摘要**：核心结论（1 段话）
3. **技术解析**：核心技术方案概述（来自阶段一）
4. **现有技术分析**：检索结果和技术发展概况（来自阶段二）
5. **侵权/新颖性评估**：对比分析结论（来自阶段三）
6. **策略建议**：行动建议（来自阶段四）
7. **结论与免责声明**

## 重要约束
- 报告内容必须来自前序阶段的实际产出物，不要新增未经分析的结论。
- 末尾必须包含免责声明："本报告由 AI 辅助生成，仅供参考。涉及法律判断的内容请咨询专利代理师或知识产权律师。"
- 只生成一份报告，输出完毕后立即停止。
- 【严禁】自己模拟用户确认。
`

	// --- Paper Reproduction Workflow ---

	case "paper_analysis":
		return `## 阶段指令

深度解读目标论文。如果用户未提供论文 PDF 或 URL，立即请求用户提供。

论文解读文档应包含：
1. **论文基本信息**：标题、作者、发表会议/期刊、年份
2. **核心方法**：算法/模型架构、关键创新点、与前人工作的区别
3. **实验设置**：数据集（名称、来源、规模）、评价指标、基线方法、超参数设置
4. **关键基准指标（必须精确提取）**：
   - 主指标名称及论文报告的最优值（如 "Accuracy: 85.3%", "BLEU: 32.1", "F1: 91.2"）
   - 各数据集上的结果（如有多个数据集）
   - 各基线方法的对比数值
   - 以表格形式呈现，每行：方法名 | 指标1 | 指标2 | ...
5. **复现关键点**：可能的难点、论文中不够详细需要推断的细节
6. **代码/数据线索**：论文中提到的官方代码仓库、数据集下载链接

## 关键产出：基准指标 JSON

在解读文档末尾，必须输出一个结构化的基准指标块（后续阶段会引用此数据作为达标判定依据）：

[BASELINE_METRICS]
- 主指标名称：（如 Accuracy / F1 / BLEU / mAP）
- 论文最优值：（精确数值，如 85.3）
- 单位/方向：（% 越高越好 / 越低越好）
- 数据集：（在哪个数据集上的结果）
- 次要指标（如有）：指标名=数值, 指标名=数值
[/BASELINE_METRICS]

## 重要约束（违反将导致错误）
- 如果用户已提供论文 URL：
  - arXiv 链接（arxiv.org/abs/XXXX）：将 /abs/ 替换为 /pdf/ 获取 PDF 全文（如 https://arxiv.org/pdf/2401.12345）
  - 其他学术网站：尝试直接 web_fetch，如果只得到摘要页，搜索该论文的 PDF 链接或预印本
  - 使用 web_fetch 获取论文完整内容进行分析
- 如果用户上传了 PDF，直接分析附件内容。
- 实验数据必须精确提取（具体数值，不要概括为"较好"/"有提升"等模糊表述）。
- 基准指标必须是论文中该方法的最优结果，不是基线方法的结果。
- 只生成一份解读文档，输出完毕后立即停止。
- 【严禁】自己模拟用户确认。
`
	case "reproduction_plan":
		return `## 阶段指令

基于论文解读，制定完整的复现规划。需要实际上网搜索源码和数据集。

复现规划文档应包含：
1. **源码搜索结果**：
   - 在 GitHub/GitLab 搜索官方实现和第三方实现
   - 列出找到的仓库（URL、star 数、最后更新时间、框架）
   - 如果无现成代码，列出需要从头实现的模块
2. **数据集搜索结果**：
   - 论文使用的每个数据集的下载地址
   - 数据集大小、格式、预处理需求
   - 替代数据集（如官方不可用）
3. **硬件需求评估**：GPU 显存、磁盘空间、训练时间预估
4. **依赖环境**：Python 版本、PyTorch/TensorFlow 版本、CUDA 版本、关键库
5. **项目目录结构**：

   paper_reproduction/
     src/          -- 源码
     data/         -- 数据集
     checkpoints/  -- 模型权重
     logs/         -- 训练日志
     results/      -- 实验结果
       tables/     -- 结果表格
       figures/    -- 图表
     reports/      -- 实验报告

6. **复现步骤清单**：按顺序列出从 0 到得到结果的所有步骤
7. **风险与备选方案**：可能失败的环节及应对措施

## 重要约束
- 必须使用 web_search 实际搜索源码和数据集，不要凭记忆编造链接。
- 数据集链接必须经过验证（尝试 web_fetch 确认可访问）。
- 只生成一份规划文档，输出完毕后立即停止。
- 【严禁】自己模拟用户确认。
`
	case "env_and_data":
		return `## 阶段指令

在远程服务器上搭建实验环境并准备数据。

如果用户尚未提供远程服务器信息（IP/域名、用户名、密码），使用 ask_user 工具请求：
- 服务器 IP 或域名（含端口）
- 用户名
- 密码或密钥
- 工作目录（可选，不提供则自动选择空间最大的分区）

执行步骤：
1. SSH 连接远程服务器
2. 检查 GPU（nvidia-smi）、磁盘空间（df -h）、已有环境
3. 如用户未指定工作目录，找到空间最大的分区并创建项目目录
4. 创建项目目录结构（按复现规划中的结构）
5. 克隆源码仓库（或创建代码骨架）
6. 创建 conda/venv 环境，安装依赖
7. 下载数据集并进行必要的预处理
8. 验证环境：import 关键库成功、GPU 可用、数据集文件完整

## 重要约束
- 所有操作通过 SSH 在远程服务器执行。
- 大文件下载使用后台任务（nohup/screen）。
- 每个步骤完成后验证结果，失败则尝试修复。
- 环境搭建完成后输出一份简要的环境报告（GPU 信息、Python 版本、已安装库、数据集状态）。
`
	case "baseline_reproduction":
		return `## 阶段指令

在远程服务器上复现论文的基线实验。

执行步骤：
1. 按论文的超参数设置配置训练/推理脚本
2. 先跑一个小规模测试（少量 epoch/数据子集）确认代码无 bug
3. 启动完整训练/推理（使用后台任务）
4. 监控训练过程（loss 曲线、GPU 利用率）
5. 训练完成后评估模型，提取指标
6. 与论文报告的数值对比

目标：复现结果与论文报告的数值相差不超过 ±2%（或合理范围内）。

## 重要约束
- 长时间训练使用 SSH 后台任务（submit_task），定期 check_task 监控。
- 记录所有超参数和运行命令到 logs/ 目录。
- 保存模型 checkpoint 到 checkpoints/ 目录。
- 实验结果保存到 results/ 目录（JSON/CSV 格式）。
- 完成后输出基线结果对比表（论文数值 vs 复现数值）。
`
	case "iterative_improvement":
		return `## 阶段指令

在基线复现的基础上，全自动迭代改进实验结果。本阶段设计为无人值守的自动科研模式——设定好目标后持续运行，只在关键节点通知用户。

## 启动前参数确认

在开始改进之前，使用 ask_user 一次性确认以下参数（用户未指定的用默认值）：
- **目标超出值**：主指标超越论文多少时通知用户，默认 0.1%（如论文 85.0% → 达到 85.1% 即通知）。用户可根据领域调整（如 NLP 任务可设 0.5%，CV 任务可设 0.1%）
- **最大运行时间**：总实验运行时间上限，默认 48 小时
- **最大改进轮数**：防止无限循环，默认 50 轮
- **平台期容忍轮数**：连续多少轮无改善后暂停请求用户指导，默认 8 轮

确认后开始全自动运行，不再每轮打断用户。

## 自动改进循环

每轮（完全自主决策，无需用户参与）：
1. 分析历史实验数据（所有已完成轮次的配置→结果映射），识别最有潜力的改进方向
2. 通过 SSH 修改远程服务器上的源码（使用 ssh exec 执行 sed/python 脚本/heredoc 写入等方式编辑文件）
3. 启动训练（SSH 后台任务）
4. 等待训练完成，收集结果
5. 更新结果记录（results/exp_NNN/）
6. 判断是否触发通知条件

源码修改规范：
- 每次修改前先 ssh exec "cat 文件" 确认当前内容
- 小改动用 sed -i 或 python -c 脚本
- 大改动用 heredoc 重写整个文件或关键函数
- 修改后 ssh exec "python -c 'import module'" 验证语法正确
- 记录每轮修改的 diff 到 results/exp_NNN/changes.patch

## 通知用户的条件（仅以下情况才打断用户）

通知必须通过 IM 通道（飞书/微信/QQ）推送，确保用户不在电脑前也能收到。使用 ask_user 工具发送通知——系统会自动将消息推送到用户绑定的 IM 通道（飞书/微信/QQ）。

1. **达成目标**：主指标超越论文 ≥ 目标超出值 → 推送通知"目标达成！当前最佳 XX.X%（论文 YY.Y%，超出 +Z.Z%）"，询问是继续冲击更高结果还是生成报告
2. **时间到期**：累计运行时间 ≥ 最大运行时间 → 推送通知"时间到期（已运行 Nh）。当前最佳 XX.X%，共完成 N 轮"，询问是延长还是停止
3. **平台期**：连续 N 轮主指标无改善（波动 < 0.1%）→ 推送通知"遇到平台期（连续 N 轮无进展）。当前 XX.X%，已尝试：[方向列表]"，请求用户给出新方向
4. **轮数用尽**：完成最大改进轮数 → 推送通知"已完成全部 N 轮改进。最佳 XX.X%（基线 YY.Y%）"

通知格式要求：
- 第一行：关键数据（指标值、对比），用户扫一眼就知道状态
- 第二行：简要说明
- 最后一行：可选操作（"回复'继续'/'停止'/'换方向:XXX'"）
- 控制在 5 行以内，方便手机阅读

除以上 4 种情况外，全程自动运行不通知用户。

## 用户介入后的处理

用户在收到通知后可以：
- 指定新的改进方向（如"试试 contrastive learning"、"换 AdamW 为 LAMB"）→ 按指定方向继续自动循环
- 调整参数（如"再跑 24 小时"、"目标改为 5%"）→ 更新参数后继续
- 说"继续"→ AI 自主选择方向继续
- 说"停止"/"生成报告"→ 进入实验报告阶段

## 自主改进策略（AI 无人值守时的决策逻辑）

按优先级和实验阶段选择：
- 前 5 轮：超参数网格搜索（学习率、batch size、weight decay）
- 第 6-15 轮：数据增强 + 训练策略（mixup、label smoothing、scheduler）
- 第 16-30 轮：模型架构调整（attention、残差、宽度/深度）
- 第 31+ 轮：组合最优配置 + 消融验证

每轮选择依据：从 results/ 中分析哪个维度改进幅度最大但尚未充分探索。

## 实验记录规范

每个实验目录 results/exp_NNN/：
- config.json：完整超参数和修改说明（"why": 为什么选这个方向）
- metrics.json：所有评估指标
- train.log：训练日志摘要（首尾各 50 行 + 最终指标）
- delta.txt：与基线和历史最佳的对比

## 重要约束
- 全自动模式下严禁每轮都用 ask_user 打断用户。
- 所有训练使用 SSH 后台任务（submit_task），通过 check_task 监控。
- 记录改进决策的理论依据（写入 config.json 的 "why" 字段）。
- 维护一个 results/summary.json 记录所有轮次的关键指标对比表。
- 达到通知条件时用 ask_user 通知，附简洁的数据摘要（3-5行），不要长篇大论。
`
	case "experiment_report":
		return `## 阶段指令

生成完整的实验报告，包含所有实验的原始数据和可视化材料。

报告内容：
1. **实验概述**：论文信息、复现目标、环境配置
2. **基线复现结果**：与论文数值的对比表
3. **改进实验汇总**：
   - 对比实验表（所有变体 vs 基线 vs 论文）
   - 消融实验表（各组件贡献）
   - 超参数影响分析（关键超参数 vs 指标的关系）
4. **图表**：
   - 训练曲线（loss/metric vs epoch）
   - 性能对比柱状图
   - 超参数敏感性曲线
5. **结论**：最佳配置、关键发现、与论文差异分析
6. **产出物清单**：
   - 代码位置（远程服务器路径）
   - 最佳模型 checkpoint 路径
   - 数据集路径
   - 所有实验日志位置

产出物保存到项目的 reports/ 目录。同时在本地（用户工作目录）生成报告文件。

## 重要约束
- 实验数据必须精确，从 results/ 目录读取实际结果。
- 图表数据保存为 CSV（results/tables/），方便后续使用。
- 报告使用 Markdown 格式，图表用描述性文字+数据表格。
- 只生成一份完整报告，输出完毕后等待用户确认。
`
	// 专利申请 (Patent Application)
	// ---------------------------------------------------------------------------
	case "pa_disclosure_parsing":
		return `## 阶段指令

解析专利申请材料：发明/实用新型提炼核心技术方案，外观设计提炼产品用途、设计要点和图片/照片要求。

用户通过表单提交了输入信息，根据输入方式（_agent_view_variant 字段）进行处理：

**方式零：外观设计材料解析（patent_type=design 时必须执行）**
- 只要 patent_type 为 design，无论用户选择 design_mode 还是 file_mode，都先走本分支。
- 不要提炼技术方案、技术问题、有益效果或权利要求基础；外观设计保护的是产品外观设计。
- 如果是 design_mode：检查 design_images_paths 中列出的图片或照片是否存在、是否覆盖必要视图、是否清楚一致，并使用 design_product_name、design_product_use、design_brief_description 生成"外观设计材料解析.md"。
- 如果是 file_mode：先按下方文件解析方式读取 disclosure_path，将其作为外观设计交底材料；同时检查材料中是否列明图片或照片路径。如未提供图片或照片，必须标记为不合规并要求用户补充。
- 文档内容包括：产品名称、产品用途、图片或照片清单、视图完整性检查、设计要点、最能表明设计要点的图片或照片、需补充材料。
- 保存后立即停止，等待用户确认；不要继续执行下方发明/实用新型交底书解析指令。

**方式一：交底书/申请材料文件（file_mode）**
- 先用 bash 提取文档文本内容：
  - .docx 文件：bash(command="pip install python-docx -q && python -c \"from docx import Document; doc=Document(r'路径'); print('\\n'.join(p.text for p in doc.paragraphs))\"")
  - 如果 python/pip 不可用（报错 "not found"/"无法识别"）：改用 PowerShell COM 提取：bash(command="powershell -Command \"$word = New-Object -ComObject Word.Application; $word.Visible = $false; $doc = $word.Documents.Open([System.IO.Path]::GetFullPath('路径')); $doc.Content.Text; $doc.Close(); $word.Quit()\"")
  - .doc 文件（旧格式）：必须使用 PowerShell COM 方式（python-docx 不支持 .doc）：bash(command="powershell -Command \"$word = New-Object -ComObject Word.Application; $word.Visible = $false; $doc = $word.Documents.Open([System.IO.Path]::GetFullPath('路径')); $doc.Content.Text; $doc.Close(); $word.Quit()\"")
  - .txt/.md 文件：直接使用 read_file
  - .pdf 文件：bash(command="pip install pymupdf -q && python -c \"import fitz; doc=fitz.open(r'路径'); print('\\n'.join(page.get_text() for page in doc))\"")
  - 如果 Python 不可用且是 PDF：告知用户将 PDF 转换为 Word 或文本格式后重新提交
- 如果以上方法都失败：发明/实用新型告知用户改用"手工输入"方式；外观设计告知用户改用"外观设计图片或照片"方式

**方式二：手工输入（manual_mode）**
- 直接使用上方表单中的"要解决的技术问题"、"技术方案"、"有益效果"等字段内容生成文档
- 如果提供了"附图文件路径"和"附图说明"，整合为附图清单

生成文档内容：
1. **技术领域**：明确本发明所属的技术领域（基于表单中的 tech_field）
2. **背景技术分析**：
   - 现有技术方案描述
   - 现有技术的缺陷和不足（要具体，这是撰写权利要求"区别特征"的基础）
3. **技术问题凝练**：本发明要解决的技术问题（1-2 个核心问题）
4. **技术方案提炼**：
   - 核心发明点（独立权利要求的基础）
   - 优选/改进方案（从属权利要求的基础）
   - 关键技术特征列表（结构/步骤/参数/材料）
   - 各技术特征之间的逻辑关系
5. **有益效果**：
   - 每个效果对应哪个技术特征
   - 尽可能量化（提高XX%、降低XX%）
6. **附图清单**：
   - 用户提供的附图列表（文件名和路径）
   - 每张图的内容说明（来自 figures_descriptions 或根据文件名推测）
   - 建议补充的附图（如有缺失）
7. **现有技术对比**（如用户提供了 prior_art）：
   - 对比文件分析
   - 与本发明的区别特征
8. **专利类型适配分析**：
   - 根据技术方案特点，确认选择的专利类型（patent_type）是否合适
   - 发明专利 vs 实用新型的适用性建议

## 重要约束（违反将导致错误）
- 技术方案必须从用户提供的材料中提炼，不要臆造技术内容。
- 区别特征必须明确——这直接影响权利要求的撰写质量。
- 生成文档后，必须保存到磁盘：write_file(path="OUTPUT_DIR/交底书解析与技术提炼文档.md", content="...", mode="write")。如果内容超过约 6000 字符，建议分多次 mode="append" 写入。
- 只生成一份解析文档，输出完毕后立即停止。
- 【严禁】输出确认提示语或后续内容。
- 【严禁】自己模拟用户确认。
`
	case "pa_prior_art_search":
		return `## 阶段指令

基于交底书解析结果，进行现有技术检索（查新），分析本发明的新颖性和创造性。

## 外观设计分支（patent_type=design 时必须执行）

如果表单中的 patent_type 为 design：
- 不要进行技术方案的新颖性/创造性分析，也不要生成权利要求撰写策略。
- 改为进行外观设计近似设计检索和风险分析，重点关注相同或相近产品类别、整体视觉效果、主要设计特征、现有设计公开情况。
- 使用 web_search 检索相同/相近产品外观、已公开外观设计专利、商品图片或公开销售页面；不得凭记忆编造对比设计。
- 生成"外观设计近似检索与风险分析.md"，内容包括：检索关键词、检索来源、近似设计列表、相似点/区别点、整体视觉效果差异、提交风险和图片/照片补正建议。
- 保存后立即停止，等待用户确认；不要继续执行下方发明/实用新型查新指令。

## 第零步：读取技术方案（必须先执行）

用 list_directory 查看项目目录，然后用 read_file 读取上一阶段生成的交底书解析文档，获取核心技术特征和发明点。

## 检索策略

1. **确定检索关键词**：从技术方案中提取 3-5 组关键词组合（技术领域 + 技术特征 + 技术效果），包括中英文关键词
2. **多源检索**：使用 web_search 在以下来源检索：
   - Google Patents（搜索 "site:patents.google.com" + 技术关键词）
   - WIPO PatentScope（搜索 "site:patentscope.wipo.int" + 关键词）
   - Google Scholar（学术论文中的相关技术方案）
   - 直接搜索中文关键词 + "专利" / "发明"（Google 会索引 CNIPA 公开专利）
3. **对比文件筛选**：选出 3-5 篇最接近的对比文件（专利或论文）
4. **详情获取**：对高相关度结果，使用 web_fetch 获取摘要和权利要求详情

## 文档结构

### 一、检索范围与关键词
- 列出检索使用的关键词组合
- 说明检索的数据库/来源

### 二、相关对比文件列表
| 序号 | 文件编号/名称 | 来源 | 公开日期 | 相关度 | 核心内容摘要 |
|------|-------------|------|---------|--------|-------------|
| D1   | ...         | ...  | ...     | 高/中  | ...         |

### 三、新颖性分析
对每篇高相关度对比文件：
- 本发明的技术特征 vs 对比文件的技术特征
- 区别技术特征（本发明有而对比文件没有的）
- 结论：是否具有新颖性

### 四、创造性分析
- 最接近的现有技术（选一篇作为基础）
- 区别技术特征
- 该区别特征是否是"本领域技术人员容易想到的"
- 技术效果是否有预料不到的改进
- 结论：是否具有创造性

### 五、权利要求撰写策略建议
基于查新结果，建议：
- 独立权利要求应包含哪些技术特征（保证新颖性+创造性的最小特征集）
- 建议避开的技术特征（已被现有技术公开的）
- 建议强调的区别特征

## 重要约束
- 必须实际使用 web_search 进行检索，不得凭记忆编造对比文件。
- 如果检索不到强相关结果，如实报告"未检索到高度相关的对比文件"。
- 生成文档后，必须保存到磁盘：write_file(path="OUTPUT_DIR/查新检索与新颖性分析.md", content="...", mode="write")。如果内容超过约 6000 字符，建议分多次 mode="append" 写入。
- 只生成一份查新报告，输出完毕后立即停止。
- 【严禁】输出确认提示语或后续内容。
- 【严禁】自己模拟用户确认。
`
	case "pa_claims_drafting":
		return `## 阶段指令

基于技术方案提炼结果和查新检索报告，撰写权利要求书。

## 外观设计分支（patent_type=design 时必须执行）

如果表单中的 patent_type 为 design：
- 本阶段不要撰写权利要求书，外观设计申请不提交权利要求书。
- 改为生成"外观设计保护要点与图片/照片清单.md"，内容包括：产品名称、用途、设计要点、各视图/照片文件清单、最能表明设计要点的图片或照片。
- 保存后立即停止，等待用户确认；不要继续执行下方发明/实用新型权利要求书指令。

## 第零步：读取完整的技术方案和查新报告（必须先执行）

上方"前序阶段产出物（摘要）"中的内容已被截断。撰写权利要求前，必须先用 list_directory 查看项目目录，然后用 read_file 读取：
1. 完整的"交底书解析与技术提炼文档.md"——获取所有技术特征和发明点细节
2. 完整的"查新检索与新颖性分析.md"——获取对比文件、新颖性/创造性分析、权利要求撰写策略建议

撰写权利要求时必须参考查新报告中的策略建议，确保独立权利要求包含足以区别于现有技术的特征集。

## 文档结构

文档分为两部分：

### 第一部分：权利要求书正文（可直接用于申请提交）

按编号直接撰写权利要求，格式示例：
1. 一种XX装置，包括...，其特征在于：...。
2. 根据权利要求1所述的XX装置，其特征在于：...。
3. ...

要求：
- 产品独立权利要求（结构/组成/连接关系）
- 方法独立权利要求（步骤/条件/参数）——如适用
- 采用"两段式"撰写（前序部分 + 特征部分）
- 保护范围要适当上位概括（不要过窄也不要过宽）
- 每个独立权利要求下 5-10 条从属权利要求
- 从宽到窄的层次布局，覆盖优选方案、具体参数、具体结构

### 第二部分：撰写说明（供用户参考，不提交）

- **保护范围层次说明**：解释独立权利要求到从属权利要求的递进关系
- **新颖性/创造性自评估**：与最接近现有技术的区别分析
- **修改建议**：如有需要调整的地方

## 撰写规范
- 每条权利要求为一个完整句子，以句号结尾
- 独立权利要求使用"其特征在于"连接前序和特征部分
- 从属权利要求使用"根据权利要求X所述的..."开头
- 从属权利要求可以引用一条权利要求（单一从属）或多条权利要求（多重从属，如"根据权利要求1-3中任一项所述的..."）
- 多重从属权利要求不得作为另一项多重从属权利要求的引用基础（专利法实施细则第23条）
- 技术术语前后一致
- 避免功能性限定（除非必要）
- 数值范围使用"为...至..."或"为..."
- 方法权利要求中步骤用"步骤一"、"步骤二"或"S1"、"S2"编号

## 重要约束（违反将导致错误）
- 第一部分必须是可直接提交的格式——纯编号权利要求，不含解释性文字。
- 权利要求必须有层次感——独立权利要求最宽，从属权利要求逐步限缩。
- 独立权利要求的前序部分必须包含与最接近现有技术共有的技术特征。
- 文档生成后，必须保存为 Word 文件（见下方"保存为 Word 文件"section）。
- 【严禁】输出确认提示语或后续内容。
- 【严禁】自己模拟用户确认。

## 保存为 Word 文件（必须执行）

文档内容输出完毕后，必须立即将权利要求书保存为 .docx 文件。

**【强制】必须严格按照下方三步模板执行。禁止自行编写 PowerShell/VBScript/其他转换脚本。禁止尝试用 bash 直接传递中文内容。如果下方步骤 3 执行失败，改用下方的"PowerShell COM 方案"，不要自行发明其他方法。**

**严禁将文档内容嵌入 bash command 参数中**——权利要求文本超过 4000 字符会触发 inline payload limit。

正确做法（三步）：

步骤 1：用 write_file 保存权利要求书纯文本为 .md 文件。
write_file(path="OUTPUT_DIR/权利要求书.md", content="1. 一种...", mode="write")
如果内容超过约 6000 字符，建议分多次 mode="append" 写入以避免模型输出截断。
格式：每条权利要求占一段，段落之间用空行分隔。

步骤 2：用 write_file 保存转换脚本（以下内容直接复制，只需替换 OUTPUT_DIR）：
write_file(path="OUTPUT_DIR/md2docx.py", content="import os\nimport re\nfrom docx import Document\nfrom docx.shared import Pt, Cm\nfrom docx.enum.text import WD_ALIGN_PARAGRAPH\nfrom docx.oxml.ns import qn\n\nd = r'OUTPUT_DIR'\nsrc = os.path.join(d, '权利要求书.md')\ndst = os.path.join(d, '权利要求书.docx')\n\ndoc = Document()\nstyle = doc.styles['Normal']\nstyle.font.name = 'Times New Roman'\nstyle.font.size = Pt(12)\nstyle.element.rPr.rFonts.set(qn('w:eastAsia'), '宋体')\nh = doc.add_heading('权利要求书', level=1)\nh.alignment = WD_ALIGN_PARAGRAPH.CENTER\n\ntext = open(src, encoding='utf-8').read()\nclaims = re.split(r'\\n(?=\\d+\\.\\s)', text.strip())\nfor claim in claims:\n    claim = claim.strip()\n    if not claim:\n        continue\n    p = doc.add_paragraph(claim)\n    p.paragraph_format.first_line_indent = Cm(0.74)\n\ndoc.save(dst)\nprint('saved:', dst)\n", mode="write")

步骤 3：用 bash 安装依赖并执行转换：
bash(command="pip install python-docx -q && python OUTPUT_DIR/md2docx.py")

如果 Python 不可用（pip/python 报错 "not found"）：
- **macOS**：运行 brew install python3 或从 python.org 下载安装，然后重试步骤 3
- **Linux**：运行 sudo apt install python3 python3-pip（或对应包管理器），然后重试步骤 3
- **Windows**（需已安装 Microsoft Word）：改用 PowerShell COM 方案：
步骤 2（替代）：用 write_file 保存 PowerShell 转换脚本：
write_file(path="OUTPUT_DIR/md2docx.ps1", content="$src = 'OUTPUT_DIR/权利要求书.md'\n$dst = 'OUTPUT_DIR/权利要求书.docx'\n$word = New-Object -ComObject Word.Application\n$word.Visible = $false\ntry {\n    $doc = $word.Documents.Add()\n    $text = Get-Content $src -Encoding UTF8 -Raw\n    $claims = $text -split '(?m)(?=^\\d+\\.\\s)'\n    foreach ($claim in $claims) {\n        $claim = $claim.Trim()\n        if ($claim) { $doc.Paragraphs.Add().Range.Text = $claim }\n    }\n    $doc.SaveAs2([System.IO.Path]::GetFullPath($dst))\n    Write-Output \"saved: $dst\"\n} finally {\n    $doc.Close()\n    $word.Quit()\n    [System.Runtime.Interopservices.Marshal]::ReleaseComObject($word) | Out-Null\n}\n", mode="write")
步骤 3（替代）：执行 PowerShell 脚本：
bash(command="powershell -ExecutionPolicy Bypass -File OUTPUT_DIR/md2docx.ps1")

要求：
- 所有 OUTPUT_DIR 替换为项目路径（在本消息顶部的"项目路径"字段中可以看到）
- 完成后告知用户文件路径
`
	case "pa_description_writing":
		return `## 阶段指令

基于交底书解析、权利要求书和已确认的附图编号，撰写完整的专利说明书。

## 外观设计分支（patent_type=design 时必须执行）

如果表单中的 patent_type 为 design：
- 本阶段不要撰写说明书，外观设计申请不提交说明书。
- 改为生成"简要说明.md"和"简要说明.docx"。
- 简要说明必须包含：产品名称、产品用途、设计要点、指定最能表明设计要点的图片或照片。
- 不要生成摘要；外观设计申请不提交摘要。
- 保存后立即停止，等待用户确认；不要继续执行下方发明/实用新型说明书指令。

## 第零步：读取前序阶段产出物（必须先执行）

上方"前序阶段产出物（摘要）"中的内容已被截断，不包含完整的权利要求和技术方案细节。
撰写说明书前，必须先用 read_file 读取项目目录中的以下文件（如存在）：
- 交底书解析与技术提炼文档.md — 完整的技术方案
- 权利要求书.md — 完整的权利要求条文（说明书必须逐条支持）

然后再基于完整内容撰写说明书。

注意：附图编号和标记表已在上一阶段（附图整理）中确定并经用户确认，请直接引用。

文档应为可直接提交的说明书正文格式（按专利说明书标准格式）：

# [发明名称]

## 技术领域

[一段话说明所属领域]

## 背景技术

[现有技术描述 + 缺陷分析。注意：不要贬低现有技术，客观描述不足]

## 发明内容

### 要解决的技术问题

[与独立权利要求对应的技术问题]

### 技术方案

[用说明性语言描述技术方案，与独立权利要求对应但更详细]

### 有益效果

[每个效果对应具体技术特征]

## 附图说明

图1是本发明实施例提供的XXX系统的整体架构示意图。
图2是本发明实施例提供的XXX方法的步骤流程图。

[逐图描述每张附图的内容。注意：此处只写文字描述，不嵌入图片——附图说明是说明书正文的一部分，提交专利局时不含图片内联。图片在上一阶段（附图整理）中已生成并经用户确认。]

## 具体实施方式

[至少 2-3 个实施例：
- 实施例 1：与独立权利要求对应的最基本实施方式
- 实施例 2-3：与从属权利要求对应的优选/变体实施方式
- 每个实施例结合附图标记详细描述
- 工艺参数、材料、尺寸等具体数据]

## 撰写规范
- 说明书必须充分公开技术方案——"所属技术领域的技术人员能够实现"
- 说明书必须支持权利要求——权利要求中的每个特征都能在说明书中找到依据
- 附图标记在全文中统一编号（引用上一阶段确定的标记表）
- 同一技术特征在全文中使用相同术语
- 具体实施方式要有足够细节，包括参数、条件、步骤顺序

## 重要约束（违反将导致错误）
- 输出必须是可直接提交的说明书格式——用户可以直接复制到申请文件中。
- 说明书内容必须与权利要求书对应——不能出现权利要求有而说明书没有的特征。
- 实施例必须具体——不能只是权利要求的重复改写。
- 文档生成后，必须保存为 Word 文件（见下方"保存为 Word 文件"section）。
- 【严禁】输出确认提示语或后续内容。
- 【严禁】自己模拟用户确认。

## 保存为 Word 文件（必须执行）

说明书内容输出完毕后，必须立即保存为 .docx 文件。

**【强制】必须严格按照下方三步模板执行。禁止自行编写 PowerShell/VBScript/其他转换脚本。禁止尝试用 bash 直接传递中文内容。如果下方步骤 3 执行失败，改用下方的"PowerShell COM 方案"，不要自行发明其他方法。**

**严禁将文档内容嵌入 bash command 参数中**——说明书文本超过 4000 字符会触发 inline payload limit。

正确做法（三步）：

步骤 1：用 write_file 保存说明书纯文本为 .md 文件。
write_file(path="OUTPUT_DIR/说明书.md", content="# 发明名称\n\n## 技术领域\n...", mode="write")
如果内容超过约 6000 字符，建议分多次 mode="append" 写入以避免模型输出截断。
格式：使用标准 Markdown（# 标题、正文段落、空行分隔）。

步骤 2：用 write_file 保存转换脚本（以下内容直接复制，只需替换 OUTPUT_DIR）：
write_file(path="OUTPUT_DIR/md2docx_desc.py", content="import os\nfrom docx import Document\nfrom docx.shared import Pt\nfrom docx.oxml.ns import qn\n\nd = r'OUTPUT_DIR'\nsrc = os.path.join(d, '说明书.md')\ndst = os.path.join(d, '说明书.docx')\n\ndoc = Document()\nstyle = doc.styles['Normal']\nstyle.font.name = 'Times New Roman'\nstyle.font.size = Pt(12)\nstyle.element.rPr.rFonts.set(qn('w:eastAsia'), '宋体')\n\ntext = open(src, encoding='utf-8').read()\nfor block in text.split('\\n\\n'):\n    block = block.strip()\n    if not block:\n        continue\n    if block.startswith('#'):\n        level = min(len(block) - len(block.lstrip('#')), 4)\n        doc.add_heading(block.lstrip('#').strip(), level=level)\n    else:\n        doc.add_paragraph(block)\n\ndoc.save(dst)\nprint('saved:', dst)\n", mode="write")

步骤 3：用 bash 安装依赖并执行转换：
bash(command="pip install python-docx -q && python OUTPUT_DIR/md2docx_desc.py")

如果 Python 不可用（pip/python 报错 "not found"）：
- **macOS**：运行 brew install python3 或从 python.org 下载安装，然后重试步骤 3
- **Linux**：运行 sudo apt install python3 python3-pip（或对应包管理器），然后重试步骤 3
- **Windows**（需已安装 Microsoft Word）：改用 PowerShell COM 方案：
步骤 2（替代）：用 write_file 保存 PowerShell 转换脚本：
write_file(path="OUTPUT_DIR/md2docx_desc.ps1", content="$src = 'OUTPUT_DIR/说明书.md'\n$dst = 'OUTPUT_DIR/说明书.docx'\n$word = New-Object -ComObject Word.Application\n$word.Visible = $false\ntry {\n    $doc = $word.Documents.Add()\n    $text = Get-Content $src -Encoding UTF8 -Raw\n    foreach ($block in ($text -split '\\n\\n')) {\n        $block = $block.Trim()\n        if (-not $block) { continue }\n        if ($block.StartsWith('#')) {\n            $level = ($block -replace '[^#]','').Length\n            $title = $block.TrimStart('#').Trim()\n            $doc.Paragraphs.Add().Range.Text = $title\n        } else {\n            $doc.Paragraphs.Add().Range.Text = $block\n        }\n    }\n    $doc.SaveAs2([System.IO.Path]::GetFullPath($dst))\n    Write-Output \"saved: $dst\"\n} finally {\n    $doc.Close()\n    $word.Quit()\n    [System.Runtime.Interopservices.Marshal]::ReleaseComObject($word) | Out-Null\n}\n", mode="write")
步骤 3（替代）：执行 PowerShell 脚本：
bash(command="powershell -ExecutionPolicy Bypass -File OUTPUT_DIR/md2docx_desc.ps1")

要求：
- 所有 OUTPUT_DIR 替换为项目路径（在本消息顶部的"项目路径"字段中可以看到）
- 完成后告知用户文件路径
`
	case "pa_figures_organization":
		return `## 阶段指令

基于技术方案和权利要求，生成专利附图并撰写附图说明。

## 外观设计分支（patent_type=design 时必须执行）

如果表单中的 patent_type 为 design：
- 本阶段处理"外观设计图片或照片"，不是说明书附图。
- 检查 design_images_paths 中的图片或照片是否存在、视图是否清楚一致；如缺少图片或照片，标记为不合规并要求用户补充。
- 生成"外观设计图片或照片清单.md"，列明每张图片/照片的文件名、视图名称、用途和是否作为最能表明设计要点的图片或照片。
- 不要生成技术附图、说明书附图或摘要附图。
- 保存后立即停止，等待用户确认；不要继续执行下方发明/实用新型附图指令。

## 第零步：读取技术方案和权利要求（必须先执行）

用 list_directory 查看项目目录，然后用 read_file 读取：
1. 交底书解析与技术提炼文档.md——获取技术方案描述和组件关系
2. 权利要求书.md——获取需要在附图中体现的技术特征

同时确认用户是否已提供附图文件（检查 .png/.jpg/.svg/.drawio 等）。

## 附图生成策略

### 情况一：用户已提供附图
- 确认文件存在（list_directory），记录文件名
- 直接进入"附图编号与说明"步骤

### 情况二：用户未提供附图（更常见）
必须使用工具自动生成附图。按以下优先级选择生成方式：

**方式 A：SVG 文件（首选，系统自动转 PNG）**
用 write_file 将 SVG 源码写入项目目录下的 .svg 文件（如 图1-系统架构.svg）。
**系统会自动将 SVG 转换为同名 PNG 文件**（write_file 写入 .svg 时自动触发转换），无需手动执行任何转换命令。
SVG 规范要求：
- **必须使用纯黑白**：stroke="#000" fill="none" 或 fill="#fff"，禁止彩色/渐变
- **文字用数字标记**：<text> 中只写阿拉伯数字（1、2、3...），不写中文
- **使用基础 SVG 元素**：rect、line、circle、polygon、polyline、path、marker、text
- **禁止使用**：linearGradient、radialGradient、filter、foreignObject、CSS class

**方式 B：Python + matplotlib/PIL（如系统有 Python）**
使用 bash 执行 Python 脚本生成结构图/流程图：
- 系统架构/模块关系图：用 matplotlib 的 patches + arrows 绘制方框图
- 流程图：用 matplotlib 绘制步骤框和箭头
- 数据流图：用 matplotlib 绘制节点和连线
- 保存为 PNG 格式到项目附图目录

**方式 C：Mermaid → PNG（如系统有 mmdc 命令）**
用 write_file 写 .mmd 文件，bash 调用 mmdc 转 PNG。

**方式 D：drawio-skill（如已安装）**
调用 manage_skill 运行 drawio-skill 生成图表。

### 必须生成的附图类型（根据技术方案选择适用的）
1. **系统整体架构图**（几乎所有发明专利都需要）——展示主要模块和连接关系
2. **方法流程图**（如有方法权利要求）——展示步骤顺序和判断逻辑
3. **关键模块详细结构图**（如有复杂子系统）
4. **数据处理/信号流向图**（如涉及数据处理）

### 生成附图的规范
- 图中**不要包含中文文字**——用数字标记（1、2、3...）代替，标记含义在附图说明中解释
- **必须使用黑白线条图**——专利法规定附图"应当使用黑色墨水绘制，不得着色"（《专利审查指南》第一部分第一章4.3节）
- 禁止使用彩色、灰度填充、渐变色——仅使用黑色线条和白色背景
- 剖面可用斜线填充，不同部件可用不同方向/密度的斜线区分
- 线条清晰均匀，粗细一致
- 每张图保存为独立的 PNG 文件，命名为 图1-xxx.png、图2-xxx.png
- 分辨率建议 300 DPI 或 2000x1500 像素以上

## 附图编号与说明文档

生成所有附图后，输出文档内容：

### 一、附图清单
| 图号 | 文件名 | 描述 |
|------|--------|------|
| 图1  | 图1-系统架构.png | 本发明系统整体架构示意图 |
| 图2  | 图2-方法流程.png | 本发明方法实施步骤流程图 |

### 二、附图标记对照表
| 标记号 | 组件名称 | 所在附图 |
|--------|---------|---------|
| 1 | 控制器 | 图1 |
| 2 | 传感器 | 图1 |

### 三、附图说明（图文对照）

每张附图的说明紧跟该图的 base64 内联显示。格式如下：

**图1** 是本发明实施例提供的XXX系统的整体架构示意图。

![图1](data:image/png;base64,{图1的base64编码})

**图2** 是本发明实施例提供的XXX方法的步骤流程图。

![图2](data:image/png;base64,{图2的base64编码})

> 实现方式：生成每张 PNG 后，用 bash 执行 base64 编码命令获取图片的 base64 字符串：
> - Windows PowerShell: [Convert]::ToBase64String([IO.File]::ReadAllBytes("图片路径"))
> - Linux/Mac: base64 -w0 图片路径
> 然后将 base64 字符串嵌入到上面的格式中（替换花括号部分）。
> 这样用户在右侧预览面板中可以直接看到每张图与其说明的对应关系。

## 重要约束
- 附图中的标记必须与说明书和权利要求中的组件一一对应。
- 所有生成的图形内容（SVG/PNG/Mermaid）必须用 write_file 或 bash 保存到项目目录下的文件中。不要在回复中直接贴 SVG/Mermaid 源码。
- 在"三、附图说明"section 中，必须将已保存的 PNG 文件转为 base64 data URL 嵌入（用于面板预览），同时保留磁盘文件（用于最终提交）。
- 如果所有绘图方式都失败，明确告知用户需要人工补充正式附图。
- 只生成一份附图整理文档，输出完毕后立即停止。
- 【严禁】输出确认提示语或后续内容。
- 【严禁】自己模拟用户确认。
`
	case "pa_document_assembly":
		return `## 阶段指令

整合所有阶段产出物，组装完整的专利申请文件并进行最终检查。

## 第零步：读取前序阶段产出物（必须先执行）

上方的"前序阶段产出物（摘要）"已被截断。做一致性检查前，先用 list_directory 查看项目目录，然后按 patent_type 读取已生成的完整文件：
- 发明专利/实用新型：重点读取 权利要求书.md/docx、说明书.md/docx、摘要.md/docx、附图整理文档和图片文件清单。
- 外观设计：重点读取 外观设计材料解析.md、外观设计图片或照片清单.md、简要说明.md/docx 和图片/照片文件清单。

## 第一步：生成检查报告

生成文档内容：
1. **按专利类型生成完整申请文件清单（必须严格区分）**：
   - 发明专利（patent_type=invention）：
     - [ ] 请求书
     - [ ] 说明书
     - [ ] 权利要求书
     - [ ] 摘要
     - [ ] 附图（必要时提供；如技术方案无图也必须在检查报告中说明“无附图”）
   - 实用新型（patent_type=utility_model）：
     - [ ] 请求书
     - [ ] 说明书
     - [ ] 权利要求书
     - [ ] 摘要
     - [ ] 附图（必须提供）
   - 外观设计（patent_type=design）：
     - [ ] 请求书
     - [ ] 外观设计图片或照片
     - [ ] 简要说明
     - [ ] 不生成权利要求书、说明书、摘要；如前序阶段已有这些草稿，仅作为内部参考，不列为提交产物
2. **摘要/简要说明**：
   - 发明专利、实用新型：生成摘要（300 字以内）
     - 写明发明创造名称、技术领域、技术问题、主要技术特征和用途
     - 不得使用商业性宣传用语
   - 外观设计：生成简要说明
     - 写明产品名称、产品用途、设计要点、指定最能表明设计要点的图片或照片
     - 如请求保护色彩，应在简要说明中明确
3. **请求书（必须保存为请求书.docx）**：
   - 发明创造名称/外观设计产品名称
   - 申请人信息（名称、地址、国籍）
   - 发明人/设计人信息
   - 联系人信息
   - 专利类型
   - 优先权信息（如有）
4. **一致性全面检查**：
   - 发明名称在各文件中是否统一
   - 权利要求中的技术特征是否全部在说明书中有描述
   - 附图标记是否前后一致
   - 技术术语是否全文统一
   - 从属权利要求引用关系是否正确
   - 说明书各部分是否完整
5. **形式审查预检（按专利类型区分）**：
   - 发明专利/实用新型：权利要求是否超过 10 条（超过需额外缴费）、说明书页数、附图数量、是否有明显形式缺陷
   - 实用新型：附图是否存在（必须提供）
   - 外观设计：图片或照片是否存在、视图是否清楚一致、简要说明是否完整、是否误包含权利要求书/说明书/摘要
6. **审查风险评估**：
   - 发明专利：新颖性风险点（与已知现有技术的对比）、创造性风险点（技术效果是否明显）、建议的应对策略
   - 实用新型：初步审查要点（是否属于产品形状/构造的技术方案、是否实用、权利要求是否清楚）
   - 外观设计：图片或照片视图是否完整、是否清楚一致、简要说明是否写明产品名称/用途/设计要点/最能表明设计要点的图片或照片
   - 注意：实用新型不经过实质审查，但授权后可能面临无效宣告，仍需关注新颖性
7. **提交建议**：
   - 提交方式（推荐电子申请，通过 https://cponline.cnipa.gov.cn）
   - 发明专利：列出申请费、实质审查费、权利要求附加费等可能项目；提醒 18 个月公布、可请求提前公布和按期提出实质审查请求
   - 实用新型：列出申请费、权利要求附加费等可能项目；提醒通常仅进行初步审查，不提出实质审查请求
   - 外观设计：列出申请费等可能项目；提醒通常进行初步审查，不涉及权利要求附加费、说明书页数或实质审查费
   - 具体金额请查询国知局最新收费标准

## 第二步：保存为 Word 文件（必须执行）

前序阶段应该已经分别保存了 权利要求书.docx 和 说明书.docx（发明/实用新型）或外观设计图片/照片文件（外观设计）。请先检查输出目录中是否存在这些文件。

如果 patent_type 为 invention：
- 必须确保输出目录包含：请求书.docx、说明书.docx、权利要求书.docx、摘要.docx
- 如技术方案需要附图，必须包含附图 PNG/JPG 文件；如不需要附图，在检查报告中说明理由

如果 patent_type 为 utility_model：
- 必须确保输出目录包含：请求书.docx、说明书.docx、权利要求书.docx、摘要.docx、附图 PNG/JPG 文件
- 如果没有附图，必须标记为不合规并要求用户补充或重新生成

如果 patent_type 为 design：
- 必须确保输出目录包含：请求书.docx、外观设计图片或照片 PNG/JPG 文件、简要说明.docx
- 不要生成或要求权利要求书、说明书、摘要
- 如果没有图片或照片，必须标记为不合规并要求用户补充或重新生成

如果发明/实用新型文件已存在：
- 使用 bash + python 读取各文件验证内容完整性
- 生成一份"摘要.docx"（300字以内）保存到同目录

如果发明/实用新型文件不存在（前序阶段未保存）：
- 从前序阶段的产出物摘要中获取内容
- 分别生成 权利要求书.docx、说明书.docx、摘要.docx 保存到输出目录

如果外观设计简要说明不存在：
- 从 design_brief_description、design_images_paths、前序阶段图片整理结果中生成 简要说明.docx
- 简要说明必须包含：产品名称、产品用途、设计要点、指定最能表明设计要点的图片或照片

### 生成"申请文件一致性检查报告.docx"（必须执行）

检查报告（第一步生成的内容）必须保存为 Word 文件。

**【强制】必须严格按照下方三步模板执行。禁止自行编写 PowerShell/VBScript/其他转换脚本。禁止尝试用 bash 直接传递中文内容。如果下方步骤 3 执行失败，改用下方的"PowerShell COM 方案"，不要自行发明其他方法。**

**严禁将文档内容嵌入 bash command 参数中**——检查报告文本超过 4000 字符会触发 inline payload limit。

正确做法（三步）：

步骤 1：用 write_file 保存检查报告纯文本为 .md 文件。
write_file(path="OUTPUT_DIR/申请文件一致性检查报告.md", content="# 专利申请文件一致性检查报告\n\n## 一、完整申请文件清单\n...", mode="write")
如果内容超过约 6000 字符，建议分多次 mode="append" 写入以避免模型输出截断。

步骤 2：用 write_file 保存转换脚本（以下内容直接复制，只需替换 OUTPUT_DIR）：
write_file(path="OUTPUT_DIR/md2docx_report.py", content="import os\nimport re\nfrom docx import Document\nfrom docx.shared import Pt, Cm\nfrom docx.oxml.ns import qn\n\nd = r'OUTPUT_DIR'\nsrc = os.path.join(d, '申请文件一致性检查报告.md')\ndst = os.path.join(d, '申请文件一致性检查报告.docx')\n\ndoc = Document()\nstyle = doc.styles['Normal']\nstyle.font.name = 'Times New Roman'\nstyle.font.size = Pt(12)\nstyle.element.rPr.rFonts.set(qn('w:eastAsia'), '宋体')\n\ndef is_table_line(line):\n    return line.strip().startswith('|') and line.strip().endswith('|')\n\ndef is_separator_line(line):\n    return bool(re.match(r'^\\|[\\s\\-:|]+\\|$', line.strip()))\n\ndef parse_table_block(lines):\n    rows = []\n    for line in lines:\n        if is_separator_line(line):\n            continue\n        cells = [c.strip() for c in line.strip().strip('|').split('|')]\n        rows.append(cells)\n    return rows\n\ndef add_table(doc, rows):\n    if not rows:\n        return\n    ncols = max(len(r) for r in rows)\n    tbl = doc.add_table(rows=len(rows), cols=ncols)\n    tbl.style = 'Table Grid'\n    for i, row in enumerate(rows):\n        for j, cell in enumerate(row):\n            if j < ncols:\n                tbl.rows[i].cells[j].text = cell\n\ntext = open(src, encoding='utf-8').read()\nlines = text.split('\\n')\ni = 0\nwhile i < len(lines):\n    line = lines[i]\n    if is_table_line(line):\n        table_lines = []\n        while i < len(lines) and is_table_line(lines[i]):\n            table_lines.append(lines[i])\n            i += 1\n        rows = parse_table_block(table_lines)\n        add_table(doc, rows)\n        continue\n    if line.strip() == '':\n        i += 1\n        continue\n    if line.strip().startswith('#'):\n        level = min(len(line) - len(line.lstrip('#')), 4)\n        doc.add_heading(line.lstrip('#').strip(), level=level)\n    elif line.strip().startswith('- '):\n        doc.add_paragraph(line.strip()[2:], style='List Bullet')\n    elif re.match(r'^\\d+\\.\\s', line.strip()):\n        doc.add_paragraph(line.strip(), style='List Number')\n    else:\n        doc.add_paragraph(line.strip())\n    i += 1\n\ndoc.save(dst)\nprint('saved:', dst)\n", mode="write")

步骤 3：用 bash 安装依赖并执行转换：
bash(command="pip install python-docx -q && python OUTPUT_DIR/md2docx_report.py")

如果 Python 不可用（pip/python 报错 "not found"）：
- **macOS**：运行 brew install python3 或从 python.org 下载安装，然后重试步骤 3
- **Linux**：运行 sudo apt install python3 python3-pip（或对应包管理器），然后重试步骤 3
- **Windows**（需已安装 Microsoft Word）：改用 PowerShell COM 方案：
步骤 2（替代）：用 write_file 保存 PowerShell 转换脚本：
write_file(path="OUTPUT_DIR/md2docx_report.ps1", content="[Console]::OutputEncoding = [System.Text.Encoding]::UTF8\n$src = 'OUTPUT_DIR/申请文件一致性检查报告.md'\n$dst = 'OUTPUT_DIR/申请文件一致性检查报告.docx'\n$word = New-Object -ComObject Word.Application\n$word.Visible = $false\ntry {\n    $doc = $word.Documents.Add()\n    $text = Get-Content $src -Encoding UTF8 -Raw\n    foreach ($block in ($text -split '\\n\\n')) {\n        $block = $block.Trim()\n        if (-not $block) { continue }\n        if ($block.StartsWith('#')) {\n            $title = $block.TrimStart('#').Trim()\n            $doc.Paragraphs.Add().Range.Text = $title\n        } else {\n            $doc.Paragraphs.Add().Range.Text = $block\n        }\n    }\n    $doc.SaveAs2([System.IO.Path]::GetFullPath($dst))\n    Write-Output \"saved: $dst\"\n} finally {\n    $doc.Close()\n    $word.Quit()\n    [System.Runtime.Interopservices.Marshal]::ReleaseComObject($word) | Out-Null\n}\n", mode="write")
步骤 3（替代）：执行 PowerShell 脚本：
bash(command="powershell -ExecutionPolicy Bypass -File OUTPUT_DIR/md2docx_report.ps1")

### 生成"请求书.docx"（必须执行）

请求书内容较短，可使用 python-docx 生成，至少包含：申请人、发明人/设计人、发明创造名称、专利类型、联系人、优先权信息（如有）。注意：正式提交时仍需在 CNIPA 电子申请系统中填写请求书表单，本文件作为请求书字段准备稿和提交核对表。

### 生成"摘要.docx"或"简要说明.docx"

发明专利、实用新型生成"摘要.docx"（300字以内）；外观设计生成"简要说明.docx"。内容较短，可使用短脚本或先写 .md 再转换。

如需用短脚本生成，先根据 patent_type 设置文件名和标题：发明/实用新型使用"摘要.docx"/"摘要"，外观设计使用"简要说明.docx"/"简要说明"。脚本中的正文必须替换为本阶段实际生成的摘要或简要说明，不得保留占位文字。

### 检查已有文件

bash(command="pip install python-docx -q && python -c \"import os; d=r'OUTPUT_DIR'; print('权利要求书:', '存在' if os.path.exists(os.path.join(d,'权利要求书.docx')) else '不存在'); print('说明书:', '存在' if os.path.exists(os.path.join(d,'说明书.docx')) else '不存在')\"")

注意：如果需要生成较长的文件（如补写权利要求书或说明书），**严禁将 Python 脚本内联到 bash command 参数中**。正确做法：先用 write_file 保存 Python 脚本为 .py 文件，再用 bash 执行该文件。

最终输出目录应按 patent_type 包含：
- 发明专利：请求书.docx、说明书.docx、权利要求书.docx、摘要.docx、附图（必要时）、申请文件一致性检查报告.docx
- 实用新型：请求书.docx、说明书.docx、权利要求书.docx、摘要.docx、附图、申请文件一致性检查报告.docx
- 外观设计：请求书.docx、外观设计图片或照片、简要说明.docx、申请文件一致性检查报告.docx

## 免责声明
文档末尾必须包含：本文件由 AI 辅助生成，建议提交前由专业专利代理人审核。专利权的最终授予以国家知识产权局审查决定为准。

## 重要约束（违反将导致错误）
- 一致性检查必须逐项核对，不能跳过。
- 摘要必须控制在 300 字以内。
- 必须通过工具调用来生成 .docx 文件——不要把 Python 代码作为文本输出到对话中让用户自己运行。
- 长脚本（>200 字符）先用 write_file 保存为 .py 文件再用 bash 执行；短脚本（如摘要生成，<200 字符）可直接内联 bash。
- OUTPUT_DIR 替换为项目路径（在本消息顶部的"项目路径"字段中可以看到）。
- 完成检查报告 + 保存摘要 docx 后立即停止。
- 【严禁】自己模拟用户确认。
`

	// ---------------------------------------------------------------------------
	// US Patent Application (USPTO)
	// ---------------------------------------------------------------------------
	case "us_disclosure_analysis":
		return `## Phase Instructions

Analyze the invention disclosure and extract key technical content for US patent drafting.
The disclosure may be written in Chinese or English — process either language correctly.

## Step 0: Read Input Materials

Check the form data above for the input mode:

**Mode 1: Disclosure File (file_mode)**
- The file path is shown above as "Disclosure File Path / 交底书文件路径". Replace FILE_PATH below with that value.
- Extract text from the document based on file extension:
  - .docx: bash(command="pip install python-docx -q && python -c \"from docx import Document; doc=Document(r'FILE_PATH'); print('\\n'.join(p.text for p in doc.paragraphs))\"")
  - .doc (legacy Word format): python-docx does NOT support .doc. Use PowerShell COM: bash(command="powershell -Command \"$word = New-Object -ComObject Word.Application; $word.Visible = $false; $doc = $word.Documents.Open([System.IO.Path]::GetFullPath('FILE_PATH')); $doc.Content.Text; $doc.Close(); $word.Quit()\"")
  - .txt/.md: use read_file directly
  - .pdf: bash(command="pip install pymupdf -q && python -c \"import fitz; doc=fitz.open(r'FILE_PATH'); print('\\n'.join(page.get_text() for page in doc))\"")
- If python/pip is not available (reports "not found" / "无法识别"):
  - For .docx/.doc: use the PowerShell COM method above (works without Python, requires Microsoft Word)
  - For .pdf without Python: inform user to convert PDF to Word/text format and resubmit
- If ALL methods fail: inform user to use "Manual Input" mode instead.
- If the disclosure is in Chinese, you MUST translate the technical content into English for the patent document while preserving all technical details. Keep the original Chinese in internal analysis notes for reference.

**Mode 2: Manual Input (manual_mode)**
- Use the content from the form fields above: "Problem to be Solved / 要解决的技术问题", "Technical Solution / 技术方案", "Advantages / 有益效果"
- If input is in Chinese, translate to English for patent drafting

## Document Structure (output in English)

### 1. Field of the Invention
One paragraph identifying the technical field.

### 2. Background of the Invention
- Description of prior art
- Problems and limitations of prior art (be specific — this forms the basis for distinguishing claims)

### 3. Summary of the Invention
- Technical problem to be solved
- Core inventive concept (basis for independent claims)
- Preferred embodiments (basis for dependent claims)
- Key technical features list (structure/steps/parameters/materials)
- Logical relationships between features

### 4. Brief Description of the Drawings
- List of figures (if provided via figures_paths)
- Description of each figure
- Suggested additional figures if needed

### 5. Advantages Over Prior Art
- Each advantage mapped to specific technical features
- Quantify where possible (improves by X%, reduces by Y%)

### 6. Patent Type Considerations
- Utility patent vs. provisional application strategy
- If provisional: note what can be less formal; deadline for non-provisional conversion (12 months)

## Important Constraints
- Extract technical content faithfully from user materials — do not invent technical details.
- ALL patent document content must be in English (per USPTO requirements).
- If disclosure is in Chinese, translate accurately; preserve original Chinese terms in parentheses for key technical terms on first occurrence.
- After generating the document, save it: write_file(path="OUTPUT_DIR/Disclosure_Analysis.md", content="...", mode="write"). If content exceeds ~6000 chars, use mode="append" for subsequent parts.
- Output one document only, then stop immediately.
- Do NOT output confirmation prompts or simulate user confirmation.
`
	case "us_prior_art_search":
		return `## Phase Instructions

Based on the disclosure analysis, conduct prior art search to assess novelty and non-obviousness under 35 U.S.C. §102 and §103.

## Step 0: Read Previous Phase Output

Use list_directory to check the project directory, then read_file to load:
- Disclosure_Analysis.md — core technical features and inventive concepts

## Search Strategy

1. **Identify search terms**: Extract 3-5 keyword combinations from the technical solution (technical field + features + effects), in English
2. **Multi-source search** using web_search:
   - Google Patents (search "site:patents.google.com" + technical keywords)
   - USPTO Full-Text (search "site:patft.uspto.gov" or "site:appft.uspto.gov" + keywords)
   - Google Scholar (academic papers with related solutions)
   - Espacenet (search "site:worldwide.espacenet.com" + keywords)
3. **Select closest references**: Choose 3-5 most relevant prior art documents
4. **Retrieve details**: Use web_fetch for abstracts and claims of highly relevant results

## Document Structure

### I. Search Scope and Keywords
- Keyword combinations used
- Databases and sources searched
- CPC/IPC classification codes consulted (if identifiable)

### II. Prior Art References
| Ref# | Document ID | Source | Publication Date | Relevance | Key Teaching |
|------|-------------|--------|-----------------|-----------|-------------|
| D1   | US...       | USPTO  | ...             | High/Med  | ...         |

### III. Novelty Analysis (35 U.S.C. §102)
For each highly relevant reference:
- Claim elements of the present invention vs. reference disclosure
- Distinguishing features (elements present in invention but absent from reference)
- Conclusion: Does the reference anticipate any claim element?

### IV. Non-Obviousness Analysis (35 U.S.C. §103)
- Closest prior art (select one as primary reference)
- Differences between invention and closest prior art
- Whether a person of ordinary skill (PHOSITA) would find it obvious to combine references
- Unexpected/superior technical effects
- Conclusion: Is the invention non-obvious?

### V. Claims Strategy Recommendations
Based on search results:
- Recommended scope for independent claims (minimum feature set for novelty + non-obviousness)
- Features to avoid in independent claims (already disclosed in prior art)
- Distinguishing features to emphasize
- Potential §101 issues (abstract idea, natural phenomenon) if applicable

## Important Constraints
- Must actually perform web_search — do not fabricate prior art references from memory.
- If no highly relevant results found, honestly report "No highly relevant prior art identified."
- All analysis in English.
- After generating the document, save it: write_file(path="OUTPUT_DIR/Prior_Art_Search.md", content="...", mode="write"). If content exceeds ~6000 chars, use mode="append" for subsequent parts.
- Output one document only, then stop immediately.
- Do NOT simulate user confirmation.
`
	case "us_claims_drafting":
		return `## Phase Instructions

Based on the disclosure analysis and prior art search, draft patent claims conforming to USPTO requirements (35 U.S.C. §112, MPEP Chapter 2100).

## Step 0: Read Previous Phase Outputs

Use list_directory then read_file to load:
- Disclosure_Analysis.md — full technical solution
- Prior_Art_Search.md — novelty/non-obviousness conclusions and claims strategy

## Claims Drafting Rules (USPTO Format)

### Claim Structure
- Claims must be in ONE sentence (single period at the end)
- Independent claims: preamble + transitional phrase + body
- Transitional phrases: "comprising" (open-ended, preferred), "consisting of" (closed), "consisting essentially of" (semi-open)
- Dependent claims: "The [device/method] of claim N, wherein/further comprising..."
- Number claims sequentially starting from 1

### Required Claims
1. **Apparatus/System independent claim** (if applicable) — structural elements and connections
2. **Method independent claim** (if applicable) — steps in logical order
3. **CRM claim** (Computer Readable Medium, if software-related) — optional but recommended
4. **Dependent claims**: 5-15 per independent claim, narrowing progressively

### Claim Drafting Best Practices
- Use "a" for first introduction, "the" or "said" for subsequent references
- Avoid relative terms ("approximately", "substantially") unless necessary and defined
- Each claim element should have antecedent basis
- Method claims use gerund form ("receiving...", "determining...", "generating...")
- Avoid negative limitations where possible
- Keep independent claims broad; narrow in dependent claims

## Document Structure

### Part 1: Claims (Formal — ready for filing)

1. A [device/system/method] for [purpose], comprising:
   a first component configured to [...];
   a second component coupled to the first component and configured to [...]; and
   a controller configured to [...].

2. The [device] of claim 1, wherein the first component further comprises [...].

3. The [device] of claim 1, wherein [...].

[Continue with all claims]

### Part 2: Drafting Notes (for applicant reference, not filed)

- **Claim tree**: Visual hierarchy showing dependency relationships
- **Claim-to-specification mapping**: Which specification section supports each claim
- **Prosecution strategy notes**: Potential narrowing amendments if rejections received
- **Claim count assessment**: Total claims (note: >20 claims incur excess claim fees)

## Save as Word File (MUST execute)

**【MANDATORY】Follow the three-step template below exactly. Do NOT write your own PowerShell/VBScript/other conversion scripts. Do NOT attempt to pass Chinese/English content directly through bash inline.**

Step 1: Save claims as .md file (**Part 1 ONLY** — do NOT include Part 2 Drafting Notes):
write_file(path="OUTPUT_DIR/Claims.md", content="1. A system for...", mode="write")
If content exceeds ~6000 characters, use mode="append" for subsequent parts.

Then save drafting notes separately (for applicant reference, not converted to docx):
write_file(path="OUTPUT_DIR/Claims_Notes.md", content="## Claim Tree\n...", mode="write")

Step 2: Save conversion script:
write_file(path="OUTPUT_DIR/md2docx_claims.py", content="import os\nimport re\nfrom docx import Document\nfrom docx.shared import Pt, Cm\nfrom docx.enum.text import WD_ALIGN_PARAGRAPH\n\nd = r'OUTPUT_DIR'\nsrc = os.path.join(d, 'Claims.md')\ndst = os.path.join(d, 'Claims.docx')\n\ndoc = Document()\nstyle = doc.styles['Normal']\nstyle.font.name = 'Times New Roman'\nstyle.font.size = Pt(12)\nh = doc.add_heading('Claims', level=1)\nh.alignment = WD_ALIGN_PARAGRAPH.CENTER\n\ntext = open(src, encoding='utf-8').read()\nclaims = re.split(r'\\n(?=\\d+\\.\\s)', text.strip())\nfor claim in claims:\n    claim = claim.strip()\n    if not claim:\n        continue\n    p = doc.add_paragraph(claim)\n    p.paragraph_format.first_line_indent = Cm(1.27)\n    p.paragraph_format.line_spacing = 2.0\n\ndoc.save(dst)\nprint('saved:', dst)\n", mode="write")

Step 3: Execute conversion:
bash(command="pip install python-docx -q && python OUTPUT_DIR/md2docx_claims.py")

If Python is not available (pip/python reports "not found"):
- **macOS**: run brew install python3 or download from python.org, then retry Step 3
- **Linux**: run sudo apt install python3 python3-pip (or equivalent), then retry Step 3
- **Windows** (requires Microsoft Word installed): use PowerShell COM fallback — write_file the PS1 script below, then execute with bash(command="powershell -ExecutionPolicy Bypass -File OUTPUT_DIR/md2docx_claims.ps1")

Requirements:
- Replace all OUTPUT_DIR with the actual project path
- Claims document must use Times New Roman 12pt, double-spaced, 1.27cm indent (USPTO format)
- Inform user of file path when complete
`
	case "us_drawings":
		return `## Phase Instructions

Based on the technical solution and claims, generate patent drawings conforming to 37 CFR §1.84 (USPTO drawing requirements).

## Step 0: Read Previous Phase Outputs

Use list_directory then read_file to load:
- Disclosure_Analysis.md — technical solution and component relationships
- Claims.md — features that must be illustrated in drawings

Check if user has already provided drawing files (.png/.jpg/.svg).

## Drawing Generation Strategy

### If user provided drawings:
- Verify files exist (list_directory), record filenames
- Proceed to "Figure Numbering" section

### If no drawings provided (generate automatically):

**Method A: Python + matplotlib (recommended, no external deps)**
Generate black-and-white technical diagrams:
- Architecture/block diagrams: matplotlib patches + arrows
- Flowcharts: matplotlib step boxes and arrows
- Save as PNG, 300 DPI minimum

**Method B: SVG (if matplotlib unavailable)**
Write SVG source to .svg files in project directory.

**Method C: drawio-skill (if installed)**
Call manage_skill to run drawio-skill.

### Required Drawing Types (select applicable)
1. **System block diagram** (almost always needed) — major components and connections
2. **Method flowchart** (if method claims exist) — steps and decision logic
3. **Detailed component diagram** (if complex subsystems)
4. **Data flow diagram** (if data processing involved)

### USPTO Drawing Requirements (37 CFR §1.84)
- **Black ink on white background ONLY** — no color, no grayscale shading, no gradients
- Cross-hatching permitted for cross-sections
- Reference numerals (10, 20, 30...) — use numbers with consistent increments, NOT sequential 1,2,3
- Lines must be clean, uniform thickness, and sufficiently dense for reproduction
- Each figure on separate sheet, labeled "FIG. 1", "FIG. 2" etc.
- Minimum margin: 2.5cm top, 2.5cm left, 1.5cm right, 1.0cm bottom
- Acceptable size: Letter (21.6 x 27.9 cm) or A4

## Figure Numbering and Description Document

After generating all drawings, output document:

### I. Figures List
| Figure | Filename | Description |
|--------|----------|-------------|
| FIG. 1 | Fig1-Architecture.png | Block diagram of the system according to an embodiment |
| FIG. 2 | Fig2-Method.png | Flowchart of the method according to an embodiment |

### II. Reference Numerals
| Numeral | Component | Appears in |
|---------|-----------|-----------|
| 10 | Controller | FIG. 1 |
| 20 | Sensor module | FIG. 1, FIG. 2 |
| 30 | Processing unit | FIG. 1 |

### III. Brief Description of Drawings (for Specification use)

FIG. 1 is a block diagram of a system according to an embodiment of the present disclosure.

![FIG. 1](data:image/png;base64,{base64 of Fig1})

FIG. 2 is a flowchart illustrating a method according to an embodiment of the present disclosure.

![FIG. 2](data:image/png;base64,{base64 of Fig2})

> To embed images: After generating each PNG, use bash to get base64:
> - Windows PowerShell: [Convert]::ToBase64String([IO.File]::ReadAllBytes("path"))
> - Linux/Mac: base64 -w0 path
> Replace the {base64 of FigN} placeholder with the actual base64 string.

## Important Constraints
- Reference numerals must be consistent with specification and claims.
- USPTO requires reference numerals NOT be enclosed in parentheses or brackets in drawings (different from EPO/CNIPA).
- Save all graphics to files in the project directory. Do NOT inline SVG/Mermaid source in the response.
- After generating the document (sections I + II + III above), save it: write_file(path="OUTPUT_DIR/Drawings.md", content="...", mode="write"). If content exceeds ~6000 chars, use mode="append" for subsequent parts.
- If all drawing methods fail, inform user that formal drawings need to be prepared manually.
- Output one document only, then stop immediately.
- Do NOT simulate user confirmation.
`
	case "us_specification_writing":
		return `## Phase Instructions

Based on the disclosure analysis, claims, and confirmed drawings, write the complete patent specification conforming to 35 U.S.C. §112 and MPEP guidelines.

## Step 0: Read Previous Phase Outputs (MUST execute first)

The "prior phase outputs" summary above is truncated. Before writing, use read_file to load:
- Disclosure_Analysis.md — full technical details
- Claims.md — the complete claims (specification must support every claim element)
- Prior_Art_Search.md — prior art conclusions (useful for Background section)
- Drawings.md — reference numerals and figure descriptions

## Specification Format (USPTO Standard)

# [TITLE OF THE INVENTION]
(in ALL CAPS per USPTO convention)

## CROSS-REFERENCE TO RELATED APPLICATIONS
[State "None" if no related applications, or list provisional/continuation/CIP relationships]

## FIELD OF THE INVENTION
[One paragraph identifying the technical field]

## BACKGROUND OF THE INVENTION
[Prior art description + limitations. Use neutral language — do not disparage prior art (MPEP §2001.06)]

## SUMMARY OF THE INVENTION
[Brief description of the invention addressing the identified problems. Should correspond to independent claims but in descriptive language.]

## BRIEF DESCRIPTION OF THE DRAWINGS
FIG. 1 is a block diagram of [...] according to an embodiment of the present disclosure.
FIG. 2 is a flowchart of [...] according to an embodiment of the present disclosure.
[One sentence per figure. Do NOT embed images here — this is filing text.]

## DETAILED DESCRIPTION OF THE PREFERRED EMBODIMENTS

[At least 2-3 embodiments:
- First embodiment: corresponds to independent claim — describe EVERY element with reference numerals
- Second/third embodiments: correspond to dependent claims — variations and preferences
- Use reference numerals consistently (e.g., "the controller 10", "the sensor module 20")
- Include specific parameters, dimensions, materials, operating conditions
- Use phrases like "In one embodiment...", "In another embodiment...", "Optionally..."
- Enable a person of ordinary skill to make and use the invention without undue experimentation]

## Writing Standards
- Specification must satisfy §112(a): written description + enablement + best mode
- Every claim element must be described in the specification
- Reference numerals consistent throughout (from the Drawings phase)
- Same technical term used consistently (define terms on first use if needed)
- Use "approximately", "about" only where technically appropriate and define the range
- Detailed description must be detailed enough for PHOSITA to reproduce

## Important Constraints
- Output must be filing-ready specification format — user can directly copy to filing system.
- Specification content must support ALL claims — no claim element without specification basis.
- Embodiments must be specific — not just restatement of claims.
- Document MUST be saved as Word file (see below).
- Do NOT output confirmation prompts.
- Do NOT simulate user confirmation.

## Save as Word File (MUST execute)

**【MANDATORY】Follow the three-step template below exactly. Do NOT write your own conversion scripts.**

Step 1: Save specification as .md file:
write_file(path="OUTPUT_DIR/Specification.md", content="# TITLE OF THE INVENTION\n\n## CROSS-REFERENCE...", mode="write")
If content exceeds ~6000 characters, use mode="append" for subsequent parts.

Step 2: Save conversion script:
write_file(path="OUTPUT_DIR/md2docx_spec.py", content="import os\nfrom docx import Document\nfrom docx.shared import Pt, Cm\n\nd = r'OUTPUT_DIR'\nsrc = os.path.join(d, 'Specification.md')\ndst = os.path.join(d, 'Specification.docx')\n\ndoc = Document()\nstyle = doc.styles['Normal']\nstyle.font.name = 'Times New Roman'\nstyle.font.size = Pt(12)\n\ntext = open(src, encoding='utf-8').read()\nfor block in text.split('\\n\\n'):\n    block = block.strip()\n    if not block:\n        continue\n    if block.startswith('#'):\n        level = min(len(block) - len(block.lstrip('#')), 4)\n        doc.add_heading(block.lstrip('#').strip(), level=level)\n    else:\n        p = doc.add_paragraph(block)\n        p.paragraph_format.line_spacing = 2.0\n        p.paragraph_format.first_line_indent = Cm(1.27)\n\ndoc.save(dst)\nprint('saved:', dst)\n", mode="write")

Step 3: Execute conversion:
bash(command="pip install python-docx -q && python OUTPUT_DIR/md2docx_spec.py")

If Python is not available (pip/python reports "not found"):
- **macOS**: run brew install python3 or download from python.org, then retry Step 3
- **Linux**: run sudo apt install python3 python3-pip (or equivalent), then retry Step 3
- **Windows** (requires Microsoft Word installed): use PowerShell COM fallback — write_file the PS1 script below, then execute with bash(command="powershell -ExecutionPolicy Bypass -File OUTPUT_DIR/md2docx_spec.ps1")

Requirements:
- Times New Roman 12pt, double-spaced, 1.27cm first-line indent (USPTO format requirements)
- Replace all OUTPUT_DIR with actual project path
- Inform user of file path when complete
`
	case "us_application_assembly":
		return `## Phase Instructions

Assemble all phase outputs into a complete USPTO patent application package and perform final consistency check.

## Step 0: Read Previous Phase Outputs (MUST execute first)

Use list_directory to check the project directory, then read_file to load all generated files (especially Claims.md/docx and Specification.md/docx).

## Step 1: Generate Filing Checklist and Consistency Report

### 1. Complete Application Package Checklist
For **Non-Provisional Utility Application**:
   - [ ] Specification (Description + Claims + Abstract + Drawings)
   - [ ] Claims (separately paginated)
   - [ ] Abstract (≤150 words)
   - [ ] Drawings (formal drawings conforming to 37 CFR §1.84)
   - [ ] Application Data Sheet (ADS) — information summary
   - [ ] Inventor Declaration (37 CFR §1.63)
   - [ ] Filing fee information

For **Provisional Application**:
   - [ ] Specification (can be less formal)
   - [ ] Drawings (can be informal)
   - [ ] Cover sheet (37 CFR §1.51(c)(1))
   - NOTE: Claims and formal drawings not required but recommended

### 2. Abstract (≤150 words)
- Concise summary of the disclosure
- Must mention the technical field, problem, and solution
- Should correspond to the most representative independent claim
- No legal phraseology ("said", "comprising", "wherein")

### 3. Application Data Sheet Information
- Title of Invention
- Applicant(s) information
- Inventor(s) information (all must be named)
- Correspondence address
- Application type (non-provisional/provisional)
- Priority claim (if applicable)

### 4. Consistency Check
- [ ] Title matches across all documents
- [ ] Every claim element has basis in specification (§112 support)
- [ ] Reference numerals consistent between specification and drawings
- [ ] Technical terminology consistent throughout
- [ ] Dependent claim references are correct (no circular references)
- [ ] No new matter introduced in claims not supported by specification
- [ ] Abstract ≤150 words and does not contain legal phrases
- [ ] Drawings show all elements referenced in claims

### 5. Formal Requirements Check
- [ ] Specification: Times New Roman 12pt, double-spaced, margins (top/bottom 2.5cm, left/right 2.5cm)
- [ ] Claims: separately paginated from specification
- [ ] Claims numbered sequentially
- [ ] Total claims count (note: >20 claims = excess claims fee; >3 independent = excess independent claims fee)
- [ ] Specification page count
- [ ] Number of drawing sheets

### 6. Patentability Risk Assessment
- §101 (Subject Matter Eligibility): Any abstract idea / Alice issues?
- §102 (Novelty): Risk points from prior art search
- §103 (Obviousness): Combination attack risks
- §112 (Written Description / Enablement): Any gaps?
- Suggested response strategies for potential rejections

### 7. Filing Recommendations
- Filing method: USPTO Patent Center (https://patentcenter.uspto.gov)
- Fee schedule reference: https://www.uspto.gov/learning-and-resources/fees-and-payment
- Key deadlines:
  - Provisional → Non-provisional: 12 months
  - Response to Office Action: typically 3 months (extendable to 6)
  - PCT international filing: 12 months from priority date

## Step 2: Save as Word File (MUST execute)

**【MANDATORY】Follow the template below. Do NOT write your own conversion scripts.**

### Generate "Application_Checklist.docx"

Step 1: Save report as .md file:
write_file(path="OUTPUT_DIR/Application_Checklist.md", content="# USPTO Patent Application Filing Checklist\n\n## 1. Application Package...", mode="write")

Step 2: Save conversion script:
write_file(path="OUTPUT_DIR/md2docx_checklist.py", content="import os\nimport re\nfrom docx import Document\nfrom docx.shared import Pt\n\nd = r'OUTPUT_DIR'\nsrc = os.path.join(d, 'Application_Checklist.md')\ndst = os.path.join(d, 'Application_Checklist.docx')\n\ndoc = Document()\nstyle = doc.styles['Normal']\nstyle.font.name = 'Times New Roman'\nstyle.font.size = Pt(12)\n\ndef is_table_line(line):\n    return line.strip().startswith('|') and line.strip().endswith('|')\n\ndef is_separator_line(line):\n    return bool(re.match(r'^\\|[\\s\\-:|]+\\|$', line.strip()))\n\ndef parse_table_block(lines):\n    rows = []\n    for line in lines:\n        if is_separator_line(line):\n            continue\n        cells = [c.strip() for c in line.strip().strip('|').split('|')]\n        rows.append(cells)\n    return rows\n\ndef add_table(doc, rows):\n    if not rows:\n        return\n    ncols = max(len(r) for r in rows)\n    tbl = doc.add_table(rows=len(rows), cols=ncols)\n    tbl.style = 'Table Grid'\n    for i, row in enumerate(rows):\n        for j, cell in enumerate(row):\n            if j < ncols:\n                tbl.rows[i].cells[j].text = cell\n\ntext = open(src, encoding='utf-8').read()\nlines = text.split('\\n')\ni = 0\nwhile i < len(lines):\n    line = lines[i]\n    if is_table_line(line):\n        table_lines = []\n        while i < len(lines) and is_table_line(lines[i]):\n            table_lines.append(lines[i])\n            i += 1\n        rows = parse_table_block(table_lines)\n        add_table(doc, rows)\n        continue\n    if line.strip() == '':\n        i += 1\n        continue\n    if line.strip().startswith('#'):\n        level = min(len(line) - len(line.lstrip('#')), 4)\n        doc.add_heading(line.lstrip('#').strip(), level=level)\n    elif line.strip().startswith('- '):\n        doc.add_paragraph(line.strip()[2:], style='List Bullet')\n    elif re.match(r'^\\d+\\.\\s', line.strip()):\n        doc.add_paragraph(line.strip(), style='List Number')\n    else:\n        doc.add_paragraph(line.strip())\n    i += 1\n\ndoc.save(dst)\nprint('saved:', dst)\n", mode="write")

Step 3: Execute:
bash(command="pip install python-docx -q && python OUTPUT_DIR/md2docx_checklist.py")

### Generate "Abstract.docx"

bash(command="pip install python-docx -q && python -c \"from docx import Document; from docx.shared import Pt; import os; doc=Document(); s=doc.styles['Normal']; s.font.name='Times New Roman'; s.font.size=Pt(12); doc.add_heading('Abstract',level=1); doc.add_paragraph('ACTUAL ABSTRACT CONTENT HERE - 150 words max'); d=r'OUTPUT_DIR'; os.makedirs(d,exist_ok=True); doc.save(os.path.join(d,'Abstract.docx')); print('saved')\"")

### Check Existing Files

bash(command="python -c \"import os; d=r'OUTPUT_DIR'; files=['Claims.docx','Specification.docx']; [print(f'{f}: exists' if os.path.exists(os.path.join(d,f)) else f'{f}: MISSING') for f in files]\"")

### Final Output Directory Should Contain:
- Claims.docx
- Specification.docx
- Abstract.docx
- Application_Checklist.docx (this phase's report)
- Drawings/ (PNG files directory)

## Disclaimer
Document must end with: This document was generated with AI assistance. It is recommended to have a registered patent attorney/agent review all documents before filing. Patent grant is ultimately determined by the USPTO examination process.

## Important Constraints
- Consistency check must be thorough — do not skip items.
- Abstract MUST be ≤150 words (USPTO requirement, strict).
- Must use tool calls to generate .docx files.
- Long scripts (>200 chars): save as .py file then execute via bash.
- Replace OUTPUT_DIR with actual project path.
- After completing checklist + saving Abstract.docx, stop immediately.
- Do NOT simulate user confirmation.
`
	// --- Literature Review Workflow ---

	case "topic_definition":
		return literatureReviewTopicDefinition
	case "search_strategy":
		return literatureReviewSearchStrategy
	case "screening":
		return literatureReviewScreening
	case "analysis":
		return literatureReviewAnalysis
	case "synthesis":
		return literatureReviewSynthesis

	// --- Research Report Workflow ---

	case "problem_definition":
		return researchReportProblemDefinition
	case "methodology":
		return researchReportMethodology
	case "data_collection":
		return researchReportDataCollection
	case "conclusion":
		return researchReportConclusion

	// --- Competitive Analysis Workflow ---

	case "competitor_id":
		return competitiveAnalysisCompetitorID
	case "comparison":
		return competitiveAnalysisComparison
	case "swot":
		return competitiveAnalysisSWOT
	case "differentiation":
		return competitiveAnalysisDifferentiation
	case "action_plan":
		return competitiveAnalysisActionPlan

	// --- Innovation Workflow ---

	case "trend_analysis":
		return innovationTrendAnalysis
	case "opportunity":
		return innovationOpportunity
	case "solution":
		return innovationSolution
	case "feasibility":
		return innovationFeasibility
	case "roadmap":
		return innovationRoadmap

	// --- Business Plan Workflow ---

	case "market_analysis":
		return businessPlanMarketAnalysis
	case "business_model":
		return businessPlanBusinessModel
	case "financial_plan":
		return businessPlanFinancialPlan
	case "operations":
		return businessPlanOperations
	case "risk_assessment":
		return businessPlanRiskAssessment

	// --- Testing Workflow ---

	case "test_strategy":
		return testingTestStrategy
	case "test_cases":
		return testingTestCases
	case "test_environment":
		return testingTestEnvironment
	case "test_execution":
		return testingTestExecution
	case "defect_report":
		return testingDefectReport

	// --- Project Proposal Workflow ---

	case "background":
		return projectProposalBackground
	case "objectives":
		return projectProposalObjectives
	case "plan":
		return projectProposalPlan
	case "budget":
		return projectProposalBudget
	case "risk_plan":
		return projectProposalRiskPlan

	// --- Event Planning Workflow ---

	case "positioning":
		return eventPlanningPositioning
	case "creative":
		return eventPlanningCreative
	case "execution_plan":
		return eventPlanningExecutionPlan
	case "budget_schedule":
		return eventPlanningBudgetSchedule
	case "contingency":
		return eventPlanningContingency

	// --- Bid Response Workflow ---

	case "tender_analysis":
		return bidResponseTenderAnalysis
	case "qualification":
		return bidResponseQualification
	case "technical":
		return bidResponseTechnical
	case "commercial":
		return bidResponseCommercial
	case "assembly":
		return bidResponseAssembly

	// --- Contract Review Workflow ---

	case "parsing":
		return contractReviewParsing
	case "risk_analysis":
		return contractReviewRiskAnalysis
	case "compliance":
		return contractReviewCompliance
	case "suggestions":
		return contractReviewSuggestions
	case "opinion":
		return contractReviewOpinion

	// --- Due Diligence Workflow ---

	case "company_profile":
		return dueDiligenceCompanyProfile
	case "business_dd":
		return dueDiligenceBusinessDD
	case "financial_dd":
		return dueDiligenceFinancialDD
	case "legal_dd":
		return dueDiligenceLegalDD

	// --- Compliance Audit Workflow ---

	case "scope":
		return complianceAuditScope
	case "assessment":
		return complianceAuditAssessment
	case "risk_rating":
		return complianceAuditRiskRating
	case "remediation":
		return complianceAuditRemediation

	// --- Experiment Design Workflow ---

	case "hypothesis":
		return experimentDesignHypothesis
	case "variables":
		return experimentDesignVariables
	case "data_plan":
		return experimentDesignDataPlan
	case "analysis_plan":
		return experimentDesignAnalysisPlan

	// --- Grant Proposal Workflow ---

	case "topic":
		return grantProposalTopic
	case "foundation":
		return grantProposalFoundation
	case "proposal":
		return grantProposalProposal

	// --- Paper Writing Workflow ---

	case "literature":
		return paperWritingLiterature
	case "drafting":
		return paperWritingDrafting
	case "figures":
		return paperWritingFigures
	case "polishing":
		return paperWritingPolishing

	default:
		// Generic instruction for doc-only phases without specific prompts.
		return `## 阶段指令

请基于前序阶段的产出物和用户需求，生成本阶段的完整文档内容（Markdown 格式）。
内容要详实、结构清晰、有可操作性。

## 重要约束（违反将导致错误）
- 只生成一份文档，输出完毕后立即停止。
- 【严禁】输出确认提示语、分隔线或任何后续内容。
- 【严禁】自己模拟用户确认。
- 你只负责输出文档本身，系统会自动提示用户确认。
`
	}
}
