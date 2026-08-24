package main

import (
	"testing"
)

func TestExtractUserIntentCategory(t *testing.T) {
	tests := []struct {
		text string
		want intentCategory
	}{
		// Chinese query verbs (2-char)
		{"统计d盘上的pdf文件", intentCatQuery},
		{"搜索论文", intentCatQuery},
		{"查找所有markdown文件", intentCatQuery},
		{"列出D盘的文档", intentCatQuery},
		{"查看这个pdf", intentCatQuery},
		{"打开桌面上的ppt文件", intentCatQuery},
		{"读取config.json", intentCatQuery},
		{"扫描目录中的图片", intentCatQuery},

		// Chinese generate verbs (2-char)
		{"生成一份PDF报告", intentCatGenerate},
		{"创建一个markdown文档", intentCatGenerate},
		{"导出为PDF格式", intentCatGenerate},
		{"把这个md转换成pdf", intentCatGenerate},
		{"制作一份简历", intentCatGenerate},
		{"编写一份技术文档", intentCatGenerate},

		// Chinese modify verbs (2-char)
		{"修改这个文件", intentCatModify},
		{"编辑readme.md", intentCatModify},

		// Chinese send verbs (2-char)
		{"发送这个pdf给我", intentCatSend},
		{"把文件发给我", intentCatSend},

		// English verbs (whole-word)
		{"count all pdf files on D drive", intentCatQuery},
		{"search for papers", intentCatQuery},
		{"find markdown files", intentCatQuery},
		{"list all documents", intentCatQuery},
		{"generate a PDF report", intentCatGenerate},
		{"convert markdown to pdf", intentCatGenerate},
		{"export as pdf", intentCatGenerate},
		{"send me the file", intentCatSend},

		// Single-char false positive prevention:
		// These contain single CJK chars that WERE verbs in the old table
		// but should NOT match because they're parts of compound words.
		{"数据分析报告", intentCatUnknown},   // "数据" = data, not "数" = count
		{"好看的PDF模板", intentCatUnknown}, // "好看" = pretty, not "看" = look
		{"写真集下载", intentCatUnknown},    // "写真" = photo, not "写" = write
		{"做法大全", intentCatUnknown},     // "做法" = method, not "做" = make

		// Unknown
		{"hello", intentCatUnknown},
		{"pdf", intentCatUnknown},
		{"", intentCatUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got := extractUserIntentCategory(tt.text)
			if got != tt.want {
				t.Errorf("extractUserIntentCategory(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestExtractSkillCapabilities(t *testing.T) {
	tests := []struct {
		desc string
		want map[capabilityCategory]bool
	}{
		{
			"Converts Markdown files to polished, print-ready PDF",
			map[capabilityCategory]bool{capCatGenerate: true},
		},
		{
			"Search local documents using semantic keywords",
			map[capabilityCategory]bool{capCatQuery: true},
		},
		{
			"OCR images from local paths or URLs",
			map[capabilityCategory]bool{capCatAnalyze: true},
		},
		{
			"Generates reports and analyzes data",
			map[capabilityCategory]bool{capCatGenerate: true, capCatAnalyze: true},
		},
		{
			"",
			map[capabilityCategory]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := extractSkillCapabilities(tt.desc)
			if len(got) != len(tt.want) {
				t.Errorf("extractSkillCapabilities(%q) = %v, want %v", tt.desc, got, tt.want)
				return
			}
			for k := range tt.want {
				if !got[k] {
					t.Errorf("extractSkillCapabilities(%q) missing %q", tt.desc, k)
				}
			}
		})
	}
}

func TestExtractTaskAndSkillDomainRecognizeAllOfficeFormats(t *testing.T) {
	for _, format := range []string{"doc", "docx", "xls", "xlsx", "ppt", "pptx"} {
		t.Run(format, func(t *testing.T) {
			if got := extractTaskDomain("open the report." + format); got != skillDomainDocument {
				t.Fatalf("extractTaskDomain(%q) = %q, want document", format, got)
			}
			if got := extractSkillDomain("parse and summarize " + format + " files"); got != skillDomainDocument {
				t.Fatalf("extractSkillDomain(%q) = %q, want document", format, got)
			}
		})
	}
}

func TestIsIntentCompatibleWithSkill(t *testing.T) {
	tests := []struct {
		name      string
		userText  string
		skillDesc string
		want      bool
	}{
		// Core case: "统计 PDF" (query) vs "Converts Markdown to PDF" (generate) → incompatible
		{
			"count_pdf_vs_md_to_pdf",
			"统计d盘上的pdf文件",
			"Converts Markdown files to polished, print-ready PDF with headings, code blocks, tables, and CJK-friendly typography.",
			false,
		},
		// "生成 PDF 报告" (generate) vs "Converts to PDF" (generate) → compatible
		{
			"generate_pdf_vs_md_to_pdf",
			"生成一份PDF报告",
			"Converts Markdown files to polished, print-ready PDF",
			true,
		},
		// "搜索论文" (query) vs "Search documents" (query) → compatible
		{
			"search_vs_search_skill",
			"搜索论文",
			"Search local documents using semantic keywords",
			true,
		},
		// "统计图片中的文字" (query) vs "OCR images" (analyze) → compatible
		{
			"count_vs_ocr",
			"统计图片中的文字",
			"OCR images from local paths or URLs",
			true,
		},
		// Unknown intent → always compatible
		{
			"unknown_intent",
			"pdf",
			"Converts Markdown to PDF",
			true,
		},
		// No skill capabilities → always compatible
		{
			"no_capabilities",
			"统计pdf文件",
			"A useful tool",
			true,
		},
		// "导出为PDF" (generate) vs "Converts to PDF" (generate) → compatible
		{
			"export_vs_convert",
			"导出为PDF格式",
			"Converts Markdown files to PDF",
			true,
		},
		// "发送PDF" (send) vs "Converts to PDF" (generate) → compatible
		{
			"send_vs_convert",
			"把这个文档发送为PDF",
			"Converts Markdown files to PDF",
			true,
		},
		// "查找所有markdown文件" (query) vs "Converts Markdown to PDF" (generate) → incompatible
		{
			"find_md_vs_md_to_pdf",
			"查找所有markdown文件",
			"Converts Markdown files to polished PDF",
			false,
		},
		// "list all pdf files" (query) vs "generate PDF" (generate) → incompatible
		{
			"list_pdf_vs_generate_pdf",
			"list all pdf files on disk",
			"Generates PDF documents from templates",
			false,
		},
		// "convert this to pdf" (generate) vs "Converts to PDF" (generate) → compatible
		{
			"convert_vs_convert",
			"convert this markdown to pdf",
			"Converts Markdown files to PDF",
			true,
		},
		// "数据分析报告" (unknown — "数据" is not a verb) vs generate → compatible (unknown = don't block)
		{
			"data_analysis_report_vs_generate",
			"数据分析报告",
			"Generates PDF reports",
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isIntentCompatibleWithSkill(tt.userText, tt.skillDesc)
			if got != tt.want {
				t.Errorf("isIntentCompatibleWithSkill(%q, %q) = %v, want %v",
					tt.userText, tt.skillDesc, got, tt.want)
			}
		})
	}
}

func TestShouldPreferSkillForTask_QueryIntentBlocked(t *testing.T) {
	if shouldPreferSkillForTask("统计d盘上的pdf文件") {
		t.Error("should return false for query intent with pdf keyword")
	}
	if shouldPreferSkillForTask("搜索D盘的pdf文件") {
		t.Error("should return false for search intent with pdf keyword")
	}
	if shouldPreferSkillForTask("查找所有markdown文档") {
		t.Error("should return false for find intent with markdown keyword")
	}
	if shouldPreferSkillForTask("列出报告文件") {
		t.Error("should return false for list intent with 报告 keyword")
	}
	if shouldPreferSkillForTask("打开桌面上的pdf") {
		t.Error("should return false for open intent with pdf keyword")
	}
}

func TestInitialAgentLoopPhaseIgnoresPickerFilenameSkillHints(t *testing.T) {
	h := &IMMessageHandler{}
	text := "北京天气\n\n" + filePathPromptPrefix + "\nC:\\tmp\\weather-report.jpg"
	phase := h.initialAgentLoopPhase(text, nil)
	if phase.ForceSkillPreference {
		t.Fatal("picker filename containing report must not force skill preference")
	}
	if !shouldPreferSkillForTask(text) {
		t.Fatal("control: raw picker text with report.jpg must trip the skill hint so the strip is doing the work")
	}
	phase = h.initialAgentLoopPhase("杭州天气，生成pdf报告", nil)
	if !phase.ForceSkillPreference {
		t.Fatal("an explicit PDF request must still prefer skills")
	}
}

func TestShouldPreferSkillForTask_GenerateIntentAllowed(t *testing.T) {
	if !shouldPreferSkillForTask("生成一份PDF报告") {
		t.Error("should return true for generate intent with pdf keyword")
	}
	if !shouldPreferSkillForTask("把这个md转换成pdf") {
		t.Error("should return true for convert intent with pdf keyword")
	}
	if !shouldPreferSkillForTask("导出为markdown格式") {
		t.Error("should return true for export intent with markdown keyword")
	}
}

func TestShouldPreferSkillForTask_SingleCharVerbNoFalsePositive(t *testing.T) {
	// "数据分析报告" contains "报告" (a substringHint) and "数据" which
	// previously matched single-char "数" → query → blocked.
	// After fix: "数据" is not a 2-char verb, intent is unknown,
	// "报告" hint matches → should return true.
	if !shouldPreferSkillForTask("数据分析报告") {
		t.Error("should return true: '数据' is not a verb, '报告' is a valid hint")
	}
}

func TestIsIntentSkillPreferenceCompatible(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"统计pdf文件", false},    // query → not compatible
		{"搜索论文", false},       // query → not compatible
		{"生成PDF报告", true},     // generate → compatible
		{"发送文件给我", true},      // send → compatible
		{"修改这个文件", false},     // modify → not compatible
		{"hello world", true}, // unknown → compatible (don't block)
		{"pdf", true},         // unknown → compatible
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got := isIntentSkillPreferenceCompatible(tt.text)
			if got != tt.want {
				t.Errorf("isIntentSkillPreferenceCompatible(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}
