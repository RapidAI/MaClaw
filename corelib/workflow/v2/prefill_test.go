package v2

import "testing"

func TestShouldPrefill_BlocksSensitiveFields(t *testing.T) {
	blocked := []string{"ssh_password", "material_path", "material_text", "contract_path",
		"core_question", "key_contribution", "hypothesis", "paper_title", "work_dir"}
	for _, name := range blocked {
		if ShouldPrefill(name) {
			t.Errorf("ShouldPrefill(%q) = true, want false", name)
		}
	}
}

func TestShouldPrefill_AllowsNormalFields(t *testing.T) {
	allowed := []string{"name", "institution", "title", "research_field",
		"gender", "birth_date", "h_index", "discipline_code"}
	for _, name := range allowed {
		if !ShouldPrefill(name) {
			t.Errorf("ShouldPrefill(%q) = false, want true", name)
		}
	}
}

func TestPrefillFromContext_NilSchema(t *testing.T) {
	result := PrefillFromContext(nil, "我是张三", nil)
	if result != nil {
		t.Errorf("expected nil for nil schema, got %v", result)
	}
}

func TestPrefillFromContext_EmptyMessage(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{{Name: "name", Label: "姓名", Type: "text"}},
	}
	result := PrefillFromContext(schema, "", nil)
	if result != nil {
		t.Errorf("expected nil for empty message, got %v", result)
	}
}

func TestPrefillFromContext_ExtractsPersonName(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "name", Label: "姓名", Type: "text", Required: true},
			{Name: "institution", Label: "现工作单位", Type: "text"},
		},
	}

	tests := []struct {
		msg      string
		wantName string
		wantInst string
	}{
		{"我是张三，来自北京大学", "张三", "北京大学"},
		{"我叫李明华，清华大学教授", "李明华", "清华大学"},
		{"申请人：王五，中国科学院计算所", "王五", "中国科学院"},  // matches 学院 suffix in 科学院
		{"帮我写杰青申请书", "", ""},                     // no name info
		{"姓名：赵六", "赵六", ""},
		{"我是北京大学计算机学院的张伟教授", "张伟", "北京大学计算机学院"}, // possessive pattern
	}

	for _, tt := range tests {
		result := PrefillFromContext(schema, tt.msg, nil)
		gotName := ""
		gotInst := ""
		if result != nil {
			if v, ok := result["name"]; ok {
				gotName = v.Value.(string)
			}
			if v, ok := result["institution"]; ok {
				gotInst = v.Value.(string)
			}
		}
		if gotName != tt.wantName {
			t.Errorf("msg=%q: name=%q, want %q", tt.msg, gotName, tt.wantName)
		}
		if gotInst != tt.wantInst {
			t.Errorf("msg=%q: institution=%q, want %q", tt.msg, gotInst, tt.wantInst)
		}
	}
}

func TestPrefillFromContext_ExtractsSelectField(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "gender", Label: "性别", Type: "select", Options: []PhaseInputOption{
				{Label: "男", Value: "男"}, {Label: "女", Value: "女"},
			}},
			{Name: "category", Label: "申报类别", Type: "select", Options: []PhaseInputOption{
				{Label: "特聘教授", Value: "特聘教授"},
				{Label: "青年学者", Value: "青年学者"},
			}},
		},
	}

	result := PrefillFromContext(schema, "我是男性，想申报青年学者", nil)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if v, ok := result["gender"]; !ok || v.Value != "男" {
		t.Errorf("gender: got %v, want 男", result["gender"])
	}
	if v, ok := result["category"]; !ok || v.Value != "青年学者" {
		t.Errorf("category: got %v, want 青年学者", result["category"])
	}
}

func TestPrefillFromContext_ExtractsAcademicTitle(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "title", Label: "职称", Type: "text"},
		},
	}

	tests := []struct {
		msg  string
		want string
	}{
		{"我是XX大学教授", "教授"},
		{"目前是副教授", "副教授"},
		{"刚评上研究员", "研究员"},
		{"我是学生", ""},
	}
	for _, tt := range tests {
		result := PrefillFromContext(schema, tt.msg, nil)
		got := ""
		if result != nil {
			if v, ok := result["title"]; ok {
				got = v.Value.(string)
			}
		}
		if got != tt.want {
			t.Errorf("msg=%q: title=%q, want %q", tt.msg, got, tt.want)
		}
	}
}

func TestPrefillFromContext_ExtractsByLabelAnchor(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "research_field", Label: "研究领域", Type: "text"},
			{Name: "h_index", Label: "H指数", Type: "text"},
		},
	}

	msg := "研究领域：自然语言处理\nH指数：42"
	result := PrefillFromContext(schema, msg, nil)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if v, ok := result["research_field"]; !ok || v.Value != "自然语言处理" {
		t.Errorf("research_field: got %v", result["research_field"])
	}
	if v, ok := result["h_index"]; !ok || v.Value != "42" {
		t.Errorf("h_index: got %v", result["h_index"])
	}
}

func TestPrefillFromContext_SkipsBlockedFields(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "name", Label: "姓名", Type: "text"},
			{Name: "ssh_password", Label: "密码", Type: "text"},
			{Name: "core_question", Label: "核心问题", Type: "textarea"},
		},
	}

	msg := "我是张三，密码：abc123，核心问题：如何解决X"
	result := PrefillFromContext(schema, msg, nil)
	if result == nil {
		t.Fatal("expected non-nil result (at least name)")
	}
	if _, ok := result["ssh_password"]; ok {
		t.Error("ssh_password should not be prefilled")
	}
	if _, ok := result["core_question"]; ok {
		t.Error("core_question should not be prefilled")
	}
}

func TestPrefillFromContext_UsesContextTexts(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "name", Label: "姓名", Type: "text"},
			{Name: "institution", Label: "现工作单位", Type: "text"},
		},
	}

	// Name in current message, institution in context history
	msg := "我叫陈明"
	history := []string{"之前聊过，我在浙江大学工作"}
	result := PrefillFromContext(schema, msg, history)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if v, ok := result["name"]; !ok || v.Value != "陈明" {
		t.Errorf("name: got %v, want 陈明", result["name"])
	}
	if v, ok := result["institution"]; !ok || v.Value != "浙江大学" {
		t.Errorf("institution: got %v, want 浙江大学", result["institution"])
	}
}

func TestPrefillFromContext_AllSourcesMarkedAsContext(t *testing.T) {
	schema := &PhaseInputSchema{
		Fields: []PhaseInputField{
			{Name: "name", Label: "姓名", Type: "text"},
		},
	}

	result := PrefillFromContext(schema, "我是王芳", nil)
	if result == nil || result["name"] == nil {
		t.Fatal("expected name to be extracted")
	}
	pv := result["name"]
	if pv.Source != "context" {
		t.Errorf("source=%q, want 'context'", pv.Source)
	}
	if pv.NeedsConfirm {
		t.Error("context source should not need confirm")
	}
	if pv.Confidence < 0.5 {
		t.Errorf("confidence=%f, too low", pv.Confidence)
	}
}
