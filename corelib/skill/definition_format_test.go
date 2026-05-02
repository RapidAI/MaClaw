package skill

import "testing"

func TestSkillDefinitionFileRejectsJSONFormat(t *testing.T) {
	if _, err := ParseSkillDefinitionFile([]byte(`{"name":"json-tool"}`), "json"); err == nil {
		t.Fatal("ParseSkillDefinitionFile(json) should reject retired JSON skill definitions")
	}
	if _, err := FormatSkillDefinitionFile(&SkillYAMLFile{Name: "json-tool"}, "json"); err == nil {
		t.Fatal("FormatSkillDefinitionFile(json) should reject retired JSON skill definitions")
	}
}
