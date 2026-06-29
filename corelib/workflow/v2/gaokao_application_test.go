package v2

import (
	"strings"
	"testing"
)

func TestGaokaoApplicationTemplateStructure(t *testing.T) {
	tmpl := GaokaoApplicationTemplate()
	if tmpl.Type != string(WorkflowGaokaoApplication) {
		t.Fatalf("type = %q, want %q", tmpl.Type, WorkflowGaokaoApplication)
	}
	if !strings.Contains(tmpl.Description, "境外校区") {
		t.Fatal("template description should mention 境外校区")
	}
	if !strings.Contains(tmpl.Description, "宁波诺丁汉大学") {
		t.Fatal("template description should mention foreign-university China campuses such as 宁波诺丁汉大学")
	}
	keywords := map[string]bool{}
	for _, keyword := range tmpl.Keywords {
		keywords[keyword] = true
	}
	for _, keyword := range []string{"高考志愿", "中外合办", "中外合作大学", "国外高校中国校区", "境外校区"} {
		if !keywords[keyword] {
			t.Fatalf("template keywords missing %q: %#v", keyword, tmpl.Keywords)
		}
	}
	if len(tmpl.Phases) != 4 {
		t.Fatalf("expected 4 phases, got %d", len(tmpl.Phases))
	}

	want := []struct {
		id     string
		policy ToolPolicy
	}{
		{GaokaoPhaseProfile, ToolPolicyDocOnly},
		{GaokaoPhaseDataSearch, ToolPolicyFull},
		{GaokaoPhaseCandidateRanking, ToolPolicyFull},
		{GaokaoPhaseFinalPlan, ToolPolicyFull},
	}
	for i, w := range want {
		got := tmpl.Phases[i]
		if got.ID != w.id {
			t.Errorf("phase %d ID = %q, want %q", i, got.ID, w.id)
		}
		if got.ToolPolicy != w.policy {
			t.Errorf("phase %s ToolPolicy = %q, want %q", got.ID, got.ToolPolicy, w.policy)
		}
		if !got.NeedsConfirm {
			t.Errorf("phase %s should require confirmation", got.ID)
		}
	}
}

func TestGaokaoProfileInputSchema(t *testing.T) {
	tmpl := GaokaoApplicationTemplate()
	schema := tmpl.Phases[0].InputSchema
	if schema == nil {
		t.Fatal("profile phase should have InputSchema")
	}

	fields := map[string]PhaseInputField{}
	for _, f := range schema.Fields {
		fields[f.Name] = f
	}
	for _, name := range []string{"province", "exam_year", "subject_type", "gender", "rank", "education_level", "accept_joint_program"} {
		f, ok := fields[name]
		if !ok {
			t.Fatalf("required field %q missing", name)
		}
		if !f.Required {
			t.Errorf("field %q should be Required", name)
		}
	}
	if fields["score"].Required {
		t.Error("score should be optional; rank is the core required field")
	}
	province := fields["province"]
	if province.Type != "select" {
		t.Fatalf("province Type = %q, want select", province.Type)
	}
	if len(province.Options) != 31 {
		t.Fatalf("province options count = %d, want 31", len(province.Options))
	}
	provinceValues := map[string]bool{}
	for _, opt := range province.Options {
		if opt.Label != opt.Value {
			t.Fatalf("province option label/value mismatch: %#v", opt)
		}
		provinceValues[opt.Value] = true
	}
	for _, value := range []string{"北京", "山东", "河南", "江苏", "广东", "新疆"} {
		if !provinceValues[value] {
			t.Fatalf("province options = %#v, want %q", province.Options, value)
		}
	}
	mode := fields["province_admission_mode"]
	if mode.Type != "select" {
		t.Fatalf("province_admission_mode Type = %q, want select", mode.Type)
	}
	modeValues := map[string]bool{}
	for _, opt := range mode.Options {
		modeValues[opt.Value] = true
	}
	for _, value := range []string{"自动判断", "新高考专业组模式", "院校专业组模式", "专业+院校模式", "传统文理模式"} {
		if !modeValues[value] {
			t.Fatalf("province_admission_mode options = %#v, want %q", mode.Options, value)
		}
	}
	for _, name := range []string{"career_intent", "future_plan"} {
		f, ok := fields[name]
		if !ok {
			t.Fatalf("optional field %q missing", name)
		}
		if f.Required {
			t.Errorf("field %q should be optional", name)
		}
	}

	gender := fields["gender"]
	if gender.Type != "select" {
		t.Fatalf("gender Type = %q, want select", gender.Type)
	}
	values := map[string]bool{}
	for _, opt := range gender.Options {
		values[opt.Value] = true
	}
	if !values["男"] || !values["女"] {
		t.Fatalf("gender options = %#v, want 男 and 女", gender.Options)
	}

	futurePlan := fields["future_plan"]
	if futurePlan.Type != "select" {
		t.Fatalf("future_plan Type = %q, want select", futurePlan.Type)
	}
	futurePlanValues := map[string]bool{}
	for _, opt := range futurePlan.Options {
		futurePlanValues[opt.Value] = true
	}
	for _, value := range []string{"就业优先", "考研/保研优先", "出国深造", "考公/事业编", "暂不明确"} {
		if !futurePlanValues[value] {
			t.Fatalf("future_plan options = %#v, want %q", futurePlan.Options, value)
		}
	}
	for _, name := range []string{"province", "province_admission_mode", "gender", "preferred_majors", "career_intent", "future_plan", "excluded_majors", "preferred_locations", "school_tier", "accept_joint_program", "tuition_limit", "strategy"} {
		if !fields[name].Reusable {
			t.Errorf("field %q should be Reusable for preference memory", name)
		}
	}
}

func TestGaokaoPhaseInstructionContainsHardConstraints(t *testing.T) {
	for _, phaseID := range []string{GaokaoPhaseProfile, GaokaoPhaseDataSearch, GaokaoPhaseCandidateRanking, GaokaoPhaseFinalPlan} {
		t.Run(phaseID, func(t *testing.T) {
			instruction := GaokaoPhaseInstruction(phaseID)
			if strings.TrimSpace(instruction) == "" {
				t.Fatal("instruction should not be empty")
			}
			for _, marker := range []string{
				"位次",
				"普通",
				"中外合办",
				"中外合作大学",
				"国外高校中国校区",
				"宁波诺丁汉大学",
				"西交利物浦大学",
				"上海纽约大学",
				"境外校区",
				"厦门大学马来西亚分校",
				"河北工业大学芬兰校区",
				"往年最低位次",
				"性别",
				"数据来源",
				"URL",
				"严禁幻觉",
				"就业",
				"未来规划",
				"省份录取规则",
				"结构化缓存",
				"近三年",
				"数据可信度",
				"学费风险",
				"所有信息必须有来源",
				"不能找到来源",
				"判断也要溯源",
				"不得用经验判断替代来源",
			} {
				if !strings.Contains(instruction, marker) {
					t.Errorf("instruction missing %q", marker)
				}
			}
			if strings.Contains(instruction, "经验判断，待核验") {
				t.Errorf("instruction should not allow experience judgment as a source substitute")
			}
		})
	}
	final := GaokaoPhaseInstruction(GaokaoPhaseFinalPlan)
	for _, marker := range []string{"总排清单", "冲", "稳", "保", "推荐理由", "限制/风险提示", "就业/发展方向适配", "境外校区", "录取概率标识", "数据可信度标识", "学费风险标识", "依据来源"} {
		if !strings.Contains(final, marker) {
			t.Errorf("final instruction missing %q", marker)
		}
	}
}

func TestIsGaokaoApplicationPhase(t *testing.T) {
	for _, phaseID := range []string{GaokaoPhaseProfile, GaokaoPhaseDataSearch, GaokaoPhaseCandidateRanking, GaokaoPhaseFinalPlan} {
		if !IsGaokaoApplicationPhase(phaseID) {
			t.Errorf("%s should be detected as gaokao phase", phaseID)
		}
	}
	if IsGaokaoApplicationPhase("requirements") {
		t.Error("requirements should not be detected as gaokao phase")
	}
}
