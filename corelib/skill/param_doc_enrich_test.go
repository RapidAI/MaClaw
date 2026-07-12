package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestExtractParamHintsFromDoc_SectionBulletsAndTypes(t *testing.T) {
	doc := `# Demo Skill

Some intro text.

## Parameters

- ` + "`input`" + `: Path to the source file (required)
- **format** (optional): Output format, default png
- limit (integer, required): Max items to process

## Notes

- do not treat this as a param: something
`
	hints := ExtractParamHintsFromDoc(doc)
	if hints["input"].Description == "" || !strings.Contains(strings.ToLower(hints["input"].Description), "source") {
		t.Fatalf("input hint = %#v", hints["input"])
	}
	if !hints["input"].HasRequired || !hints["input"].Required {
		t.Fatalf("input should be required from docs: %#v", hints["input"])
	}
	if hints["format"].Description == "" || hints["format"].Required {
		t.Fatalf("format hint = %#v", hints["format"])
	}
	if hints["limit"].Type != "integer" || !hints["limit"].Required {
		t.Fatalf("limit hint = %#v", hints["limit"])
	}
}

func TestExtractParamHintsFromDoc_TableAndCLI(t *testing.T) {
	doc := `## 参数

| Name | Description | Required |
| ---- | ----------- | -------- |
| city | 目标城市名称 | yes |
| units | 温度单位 | no |

## Usage

- --verbose  Enable verbose logging
- --output PATH  Destination file path
`
	hints := ExtractParamHintsFromDoc(doc)
	if hints["city"].Description != "目标城市名称" || !hints["city"].Required {
		t.Fatalf("city = %#v", hints["city"])
	}
	if hints["units"].Description != "温度单位" {
		t.Fatalf("units = %#v", hints["units"])
	}
	if hints["output"].Description == "" {
		t.Fatalf("output CLI hint missing: %#v", hints)
	}
}

func TestEnrichParamsFromDoc_DoesNotOverwriteExplicit(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "input", Description: "explicit desc", Required: true},
		{Name: "format", Synthetic: true},
	}
	doc := `## Parameters
- input: from doc should not win
- format: Output image format
`
	got := EnrichParamsFromDoc(params, doc)
	if got[0].Description != "explicit desc" {
		t.Fatalf("explicit description overwritten: %#v", got[0])
	}
	if got[1].Description != "Output image format" {
		t.Fatalf("synthetic description not filled: %#v", got[1])
	}
}

func TestCompleteParamsForSkill_ReadsSKILLMD(t *testing.T) {
	dir := t.TempDir()
	md := `# Weather

## Parameters

- city: City name to query
- units: Temperature units (metric/imperial)
`
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name:     "weather",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "weather --city {{city}} --units {{units}}"},
		}},
		RequiredArgs: []string{"city"},
	}
	params := CompleteParamsForSkill(entry)
	byName := map[string]corelib.NLSkillParam{}
	for _, p := range params {
		byName[p.Name] = p
	}
	if byName["city"].Description != "City name to query" || !byName["city"].Required {
		t.Fatalf("city = %#v", byName["city"])
	}
	if byName["units"].Description != "Temperature units (metric/imperial)" {
		t.Fatalf("units = %#v", byName["units"])
	}

	report := FormatSkillInspectReport(entry)
	if !strings.Contains(report, "City name to query") {
		t.Fatalf("inspect report missing doc description:\n%s", report)
	}
}

func TestEnrichParamsFromDoc_AliasMatch(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "input", Aliases: []string{"file"}, Synthetic: true},
	}
	doc := `## Arguments
- file: Path of the file to process
`
	got := EnrichParamsFromDoc(params, doc)
	if got[0].Description != "Path of the file to process" {
		t.Fatalf("alias enrichment failed: %#v", got[0])
	}
}

func TestApplyCommonParamDescriptionFallbacks(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "input", Synthetic: true},
		{Name: "limit", Synthetic: true},
		{Name: "report_file", Synthetic: true},
		{Name: "is_public", Synthetic: true},
		{Name: "input", Description: "keep me", Synthetic: true}, // duplicate name path via copy
	}
	// Fix: use distinct params — third with explicit description
	params = []corelib.NLSkillParam{
		{Name: "input", Synthetic: true},
		{Name: "limit", Synthetic: true},
		{Name: "report_file", Synthetic: true},
		{Name: "is_public", Synthetic: true},
		{Name: "custom_thing", Description: "already set", Synthetic: true},
	}
	got := ApplyCommonParamDescriptionFallbacks(params)
	if got[0].Description == "" || !strings.Contains(strings.ToLower(got[0].Description), "input") {
		t.Fatalf("input fallback = %#v", got[0])
	}
	if got[1].Description == "" || got[1].Type != "integer" {
		t.Fatalf("limit fallback = %#v", got[1])
	}
	if got[2].Description == "" || !strings.Contains(got[2].Description, "report") {
		t.Fatalf("compound *_file fallback = %#v", got[2])
	}
	if got[3].Type != "boolean" || got[3].Description == "" {
		t.Fatalf("is_* fallback = %#v", got[3])
	}
	if got[4].Description != "already set" {
		t.Fatalf("explicit description overwritten: %#v", got[4])
	}
}

func TestCompleteParamsForSkill_FallbackWithoutDoc(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name: "no-doc",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "tool {{input}} --format {{format}} --limit {{limit}}"},
		}},
		RequiredArgs: []string{"input"},
	}
	params := CompleteParamsForSkill(entry)
	byName := map[string]corelib.NLSkillParam{}
	for _, p := range params {
		byName[p.Name] = p
	}
	if byName["input"].Description == "" {
		t.Fatalf("input should get common fallback: %#v", byName["input"])
	}
	if byName["format"].Description == "" {
		t.Fatalf("format should get common fallback: %#v", byName["format"])
	}
	if byName["limit"].Type != "integer" {
		t.Fatalf("limit type fallback = %#v", byName["limit"])
	}
}

func TestDocEnrichmentBeatsCommonFallback(t *testing.T) {
	dir := t.TempDir()
	md := `## Parameters
- input: Specific doc description for input
`
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name:     "doc-wins",
		SkillDir: dir,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo {{input}}"},
		}},
	}
	params := CompleteParamsForSkill(entry)
	if len(params) != 1 || params[0].Description != "Specific doc description for input" {
		t.Fatalf("doc should win over common fallback: %#v", params)
	}
}
