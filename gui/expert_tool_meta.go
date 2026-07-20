package main

import "strings"

// expertToolMeta is human-facing metadata for the expert editor allow-list.
// ListAvailableToolNames embeds these fields so the frontend does not need a
// full hard-coded catalog that drifts from the live registry.
type expertToolMeta struct {
	Category string `json:"category"` // interaction|files|web|office|media|automation|system|knowledge|other
	Risk     string `json:"risk"`     // safe|elevated|dangerous
	LabelZh  string `json:"label_zh"`
	LabelEn  string `json:"label_en"`
}

// expertToolMetaByName is the curated catalog for well-known tools (exact keys).
// expertToolMetaByLower is a case-insensitive index built once at init.
// Unknown tools fall through to inferExpertToolMeta.
var expertToolMetaByName = map[string]expertToolMeta{
	// Interaction / session
	"memory":               {Category: "interaction", Risk: "safe", LabelZh: "记忆", LabelEn: "Memory"},
	"ask_user":             {Category: "interaction", Risk: "safe", LabelZh: "向用户提问", LabelEn: "Ask user"},
	"discover_tool":        {Category: "interaction", Risk: "safe", LabelZh: "按需发现工具", LabelEn: "Discover tools"},
	"recommend_tool":       {Category: "interaction", Risk: "safe", LabelZh: "推荐工具", LabelEn: "Recommend tool"},
	"session_search":       {Category: "interaction", Risk: "safe", LabelZh: "会话检索", LabelEn: "Session search"},
	"set_nickname":         {Category: "interaction", Risk: "safe", LabelZh: "设置昵称", LabelEn: "Set nickname"},
	"set_max_iterations":   {Category: "interaction", Risk: "elevated", LabelZh: "设置最大轮次", LabelEn: "Set max iterations"},
	"switch_llm_provider":  {Category: "interaction", Risk: "elevated", LabelZh: "切换模型服务商", LabelEn: "Switch LLM provider"},
	"manage_user_model":    {Category: "interaction", Risk: "elevated", LabelZh: "管理用户模型", LabelEn: "Manage user model"},
	"read_tool_result":     {Category: "interaction", Risk: "safe", LabelZh: "读取工具结果", LabelEn: "Read tool result"},
	"manage_skill":         {Category: "interaction", Risk: "elevated", LabelZh: "管理/运行技能", LabelEn: "Manage skill"},
	"search_and_install_skill": {Category: "interaction", Risk: "elevated", LabelZh: "搜索并安装技能", LabelEn: "Search & install skill"},
	"manage_template":      {Category: "interaction", Risk: "safe", LabelZh: "管理模板", LabelEn: "Manage template"},
	"manage_config":        {Category: "interaction", Risk: "elevated", LabelZh: "管理配置", LabelEn: "Manage config"},
	"manage_schedule":      {Category: "interaction", Risk: "elevated", LabelZh: "管理定时任务", LabelEn: "Manage schedule"},
	"im_message":           {Category: "interaction", Risk: "elevated", LabelZh: "IM 消息", LabelEn: "IM message"},
	"list_skills":          {Category: "interaction", Risk: "safe", LabelZh: "列出技能", LabelEn: "List skills"},
	"search_skill_hub":     {Category: "interaction", Risk: "elevated", LabelZh: "搜索技能市场", LabelEn: "Search skill hub"},
	"install_skill_hub":    {Category: "interaction", Risk: "elevated", LabelZh: "安装技能", LabelEn: "Install skill"},
	"run_skill":            {Category: "interaction", Risk: "elevated", LabelZh: "运行技能", LabelEn: "Run skill"},
	"get_skill_run":        {Category: "interaction", Risk: "safe", LabelZh: "查询技能运行", LabelEn: "Get skill run"},

	// Files / code
	"read_file":       {Category: "files", Risk: "safe", LabelZh: "读取文件", LabelEn: "Read file"},
	"fs_read":         {Category: "files", Risk: "safe", LabelZh: "读取文件", LabelEn: "Read file"},
	"FileRead":        {Category: "files", Risk: "safe", LabelZh: "按行读文件", LabelEn: "File read"},
	"write_file":      {Category: "files", Risk: "elevated", LabelZh: "写入文件", LabelEn: "Write file"},
	"fs_write":        {Category: "files", Risk: "elevated", LabelZh: "写入文件", LabelEn: "Write file"},
	"edit_file":       {Category: "files", Risk: "elevated", LabelZh: "编辑文件", LabelEn: "Edit file"},
	"list_directory":  {Category: "files", Risk: "safe", LabelZh: "列出目录", LabelEn: "List directory"},
	"search_files":    {Category: "files", Risk: "safe", LabelZh: "搜索文件", LabelEn: "Search files"},
	"ripgrep":         {Category: "files", Risk: "safe", LabelZh: "代码搜索", LabelEn: "Ripgrep"},
	"Glob":            {Category: "files", Risk: "safe", LabelZh: "文件匹配", LabelEn: "Glob"},
	"send_file":       {Category: "files", Risk: "elevated", LabelZh: "发送文件", LabelEn: "Send file"},
	"open":            {Category: "files", Risk: "elevated", LabelZh: "打开文件/路径", LabelEn: "Open path"},
	"office":          {Category: "files", Risk: "elevated", LabelZh: "办公文件处理", LabelEn: "Office files"},
	"download_file":   {Category: "files", Risk: "elevated", LabelZh: "下载文件", LabelEn: "Download file"},

	// Office
	"read_document": {Category: "office", Risk: "safe", LabelZh: "读取文档", LabelEn: "Read document"},
	"read_doc":      {Category: "office", Risk: "safe", LabelZh: "读取 DOC", LabelEn: "Read DOC"},
	"read_docx":     {Category: "office", Risk: "safe", LabelZh: "读取 DOCX", LabelEn: "Read DOCX"},
	"read_pdf":      {Category: "office", Risk: "safe", LabelZh: "读取 PDF", LabelEn: "Read PDF"},
	"read_excel":    {Category: "office", Risk: "safe", LabelZh: "读取表格", LabelEn: "Read Excel"},
	"write_excel":   {Category: "office", Risk: "elevated", LabelZh: "写入表格", LabelEn: "Write Excel"},
	"read_pptx":     {Category: "office", Risk: "safe", LabelZh: "读取 PPT", LabelEn: "Read PPTX"},

	// Web
	"web_search": {Category: "web", Risk: "safe", LabelZh: "网页搜索", LabelEn: "Web search"},
	"web_fetch":  {Category: "web", Risk: "elevated", LabelZh: "抓取网页", LabelEn: "Fetch web page"},

	// Media
	"screenshot":   {Category: "media", Risk: "elevated", LabelZh: "截屏", LabelEn: "Screenshot"},
	"record_audio": {Category: "media", Risk: "elevated", LabelZh: "录音", LabelEn: "Record audio"},
	"tts":          {Category: "media", Risk: "safe", LabelZh: "语音播报", LabelEn: "Text-to-speech"},
	"asr":          {Category: "media", Risk: "safe", LabelZh: "语音识别", LabelEn: "Speech recognition"},

	// Automation / tasks
	"task":              {Category: "automation", Risk: "elevated", LabelZh: "任务", LabelEn: "Task"},
	"goal":              {Category: "automation", Risk: "elevated", LabelZh: "长期目标", LabelEn: "Goal"},
	"parallel_execute":  {Category: "automation", Risk: "elevated", LabelZh: "并行执行", LabelEn: "Parallel execute"},
	"passthrough_task":  {Category: "automation", Risk: "elevated", LabelZh: "透传任务", LabelEn: "Passthrough task"},
	"project_manage":    {Category: "automation", Risk: "elevated", LabelZh: "项目管理", LabelEn: "Project manage"},
	"send_to_im":        {Category: "automation", Risk: "elevated", LabelZh: "发送到 IM", LabelEn: "Send to IM"},
	"send_input":        {Category: "system", Risk: "dangerous", LabelZh: "发送键鼠输入", LabelEn: "Send input"},

	// System / high risk
	"ssh":              {Category: "system", Risk: "dangerous", LabelZh: "SSH 远程", LabelEn: "SSH"},
	"bash":             {Category: "system", Risk: "dangerous", LabelZh: "Shell 命令", LabelEn: "Bash / shell"},
	"query_audit_log":  {Category: "system", Risk: "elevated", LabelZh: "查询审计日志", LabelEn: "Query audit log"},
	"mis_data":         {Category: "system", Risk: "elevated", LabelZh: "业务数据", LabelEn: "Business data"},

	// Knowledge (representative set; prefix rule covers the rest)
	"knowledge_search":       {Category: "knowledge", Risk: "safe", LabelZh: "知识库搜索", LabelEn: "Knowledge search"},
	"knowledge_save_text":    {Category: "knowledge", Risk: "elevated", LabelZh: "保存文本到知识库", LabelEn: "Save text to knowledge"},
	"knowledge_save_url":     {Category: "knowledge", Risk: "elevated", LabelZh: "保存 URL 到知识库", LabelEn: "Save URL to knowledge"},
	"knowledge_import_files": {Category: "knowledge", Risk: "elevated", LabelZh: "导入文件到知识库", LabelEn: "Import files to knowledge"},
	"knowledge_export":       {Category: "knowledge", Risk: "elevated", LabelZh: "导出知识库", LabelEn: "Export knowledge"},
}

var expertToolMetaByLower map[string]expertToolMeta

func init() {
	expertToolMetaByLower = make(map[string]expertToolMeta, len(expertToolMetaByName))
	for k, m := range expertToolMetaByName {
		expertToolMetaByLower[strings.ToLower(k)] = m
	}
}

// lookupExpertToolMeta returns curated or inferred metadata for a tool name.
func lookupExpertToolMeta(name string) expertToolMeta {
	name = strings.TrimSpace(name)
	if name == "" {
		return expertToolMeta{Category: "other", Risk: "elevated", LabelZh: name, LabelEn: name}
	}
	if m, ok := expertToolMetaByName[name]; ok {
		return m
	}
	if m, ok := expertToolMetaByLower[strings.ToLower(name)]; ok {
		return m
	}
	return inferExpertToolMeta(name)
}

// inferExpertToolMeta classifies unknown tools by prefix / keyword so new
// registry entries still get a reasonable group and risk without a catalog edit.
func inferExpertToolMeta(name string) expertToolMeta {
	lower := strings.ToLower(strings.TrimSpace(name))
	meta := expertToolMeta{
		Category: "other",
		Risk:     "elevated",
		LabelZh:  name,
		LabelEn:  name,
	}
	switch {
	case strings.HasPrefix(lower, "knowledge_"):
		meta.Category = "knowledge"
		meta.Risk = "elevated"
		if strings.Contains(lower, "search") || strings.Contains(lower, "list") || strings.Contains(lower, "explain") || strings.Contains(lower, "stats") || strings.Contains(lower, "health") {
			meta.Risk = "safe"
		}
		meta.LabelZh = "知识库 · " + name
		meta.LabelEn = "Knowledge · " + name
	case strings.HasPrefix(lower, "browser_"):
		meta.Category = "automation"
		meta.Risk = "elevated"
		meta.LabelZh = "浏览器 · " + name
		meta.LabelEn = "Browser · " + name
	case strings.HasPrefix(lower, "gui_") || strings.HasPrefix(lower, "computer_"):
		meta.Category = "system"
		meta.Risk = "dangerous"
		meta.LabelZh = "桌面控制 · " + name
		meta.LabelEn = "Desktop control · " + name
	case strings.HasPrefix(lower, "create_") || strings.HasPrefix(lower, "list_") || strings.HasPrefix(lower, "update_") || strings.HasPrefix(lower, "delete_") || strings.HasPrefix(lower, "get_") || strings.HasPrefix(lower, "export_") || strings.HasPrefix(lower, "import_") || strings.HasPrefix(lower, "batch_"):
		meta.Category = "interaction"
		meta.Risk = "elevated"
	case strings.Contains(lower, "ssh") || lower == "bash" || strings.Contains(lower, "shell"):
		meta.Category = "system"
		meta.Risk = "dangerous"
	case strings.Contains(lower, "web_") || strings.Contains(lower, "search"):
		meta.Category = "web"
		meta.Risk = "safe"
	case strings.Contains(lower, "file") || strings.Contains(lower, "read") || strings.Contains(lower, "write") || strings.Contains(lower, "edit") || strings.Contains(lower, "glob"):
		meta.Category = "files"
		if strings.Contains(lower, "write") || strings.Contains(lower, "edit") || strings.Contains(lower, "delete") {
			meta.Risk = "elevated"
		} else {
			meta.Risk = "safe"
		}
	}
	return meta
}
