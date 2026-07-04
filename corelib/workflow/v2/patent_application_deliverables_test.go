package v2

import (
	"strings"
	"testing"
)

func TestPatentApplicationAssemblyPromptListsRequiredCNIPADeliverables(t *testing.T) {
	prompt := phaseInstruction(WorkflowPatentApplication, "pa_document_assembly")
	for _, want := range []string{
		"patent_type=invention",
		"\u8bf7\u6c42\u4e66.docx",
		"\u8bf7\u6c42\u4e66",
		"\u8bf4\u660e\u4e66",
		"\u6743\u5229\u8981\u6c42\u4e66",
		"\u6458\u8981",
		"patent_type=utility_model",
		"\u9644\u56fe\uff08\u5fc5\u987b\u63d0\u4f9b\uff09",
		"patent_type=design",
		"\u5916\u89c2\u8bbe\u8ba1\u56fe\u7247\u6216\u7167\u7247",
		"\u7b80\u8981\u8bf4\u660e",
		"\u4e0d\u8981\u751f\u6210\u6216\u8981\u6c42\u6743\u5229\u8981\u6c42\u4e66\u3001\u8bf4\u660e\u4e66\u3001\u6458\u8981",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("patent application assembly prompt missing %q", want)
		}
	}
	if strings.Contains(prompt, "\u8bf7\u6c42\u4e66\u4fe1\u606f.docx") {
		t.Fatal("patent application assembly prompt should generate 请求书.docx, not 请求书信息.docx")
	}
}

func TestPatentApplicationPromptsIncludeDesignBranches(t *testing.T) {
	cases := map[string][]string{
		"pa_disclosure_parsing": {
			"design_mode",
			"file_mode",
			"patent_type \u4e3a design",
			"\u5916\u89c2\u8bbe\u8ba1\u6750\u6599\u89e3\u6790.md",
		},
		"pa_prior_art_search": {
			"\u5916\u89c2\u8bbe\u8ba1\u8fd1\u4f3c\u8bbe\u8ba1\u68c0\u7d22",
			"\u4e0d\u8981\u8fdb\u884c\u6280\u672f\u65b9\u6848\u7684\u65b0\u9896\u6027/\u521b\u9020\u6027\u5206\u6790",
		},
		"pa_claims_drafting": {
			"\u5916\u89c2\u8bbe\u8ba1\u7533\u8bf7\u4e0d\u63d0\u4ea4\u6743\u5229\u8981\u6c42\u4e66",
		},
		"pa_description_writing": {
			"\u5916\u89c2\u8bbe\u8ba1\u7533\u8bf7\u4e0d\u63d0\u4ea4\u8bf4\u660e\u4e66",
			"\u7b80\u8981\u8bf4\u660e.docx",
		},
		"pa_figures_organization": {
			"\u672c\u9636\u6bb5\u5904\u7406\"\u5916\u89c2\u8bbe\u8ba1\u56fe\u7247\u6216\u7167\u7247\"",
			"\u4e0d\u8981\u751f\u6210\u6280\u672f\u9644\u56fe\u3001\u8bf4\u660e\u4e66\u9644\u56fe\u6216\u6458\u8981\u9644\u56fe",
		},
	}
	for phaseID, wants := range cases {
		prompt := phaseInstruction(WorkflowPatentApplication, phaseID)
		for _, want := range wants {
			if !strings.Contains(prompt, want) {
				t.Fatalf("%s prompt missing %q", phaseID, want)
			}
		}
	}
}
