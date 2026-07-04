package skill

import (
	"reflect"
	"testing"
)

func TestSkillDefinitionFileRejectsJSONFormat(t *testing.T) {
	if _, err := ParseSkillDefinitionFile([]byte(`{"name":"json-tool"}`), "json"); err == nil {
		t.Fatal("ParseSkillDefinitionFile(json) should reject retired JSON skill definitions")
	}
	if _, err := FormatSkillDefinitionFile(&SkillYAMLFile{Name: "json-tool"}, "json"); err == nil {
		t.Fatal("FormatSkillDefinitionFile(json) should reject retired JSON skill definitions")
	}
}

func TestSkillYAMLFileRoundTripsCapabilities(t *testing.T) {
	want := []string{"current_data", "weather"}
	data, err := FormatSkillYAMLFile(&SkillYAMLFile{
		Name:         "weather-query",
		Capabilities: want,
	})
	if err != nil {
		t.Fatalf("FormatSkillYAMLFile() error = %v", err)
	}
	parsed, err := ParseSkillYAMLFile(data)
	if err != nil {
		t.Fatalf("ParseSkillYAMLFile() error = %v", err)
	}
	if !reflect.DeepEqual(parsed.Capabilities, want) {
		t.Fatalf("Capabilities = %#v, want %#v", parsed.Capabilities, want)
	}
}

func TestFormatSkillYAMLFileReturnsErrorForUnsupportedValue(t *testing.T) {
	_, err := FormatSkillYAMLFile(&SkillYAMLFile{
		Name: "bad-value",
		Steps: []SkillYAMLStep{{
			Action: "bash",
			Params: map[string]interface{}{"bad": func() {}},
		}},
	})
	if err == nil {
		t.Fatal("FormatSkillYAMLFile() error = nil, want unsupported value error")
	}
}

func TestParseSkillYAMLFileAcceptsCSVCapabilities(t *testing.T) {
	parsed, err := ParseSkillYAMLFile([]byte("name: weather-query\ncapabilities: current_data, weather\n"))
	if err != nil {
		t.Fatalf("ParseSkillYAMLFile() error = %v", err)
	}
	want := []string{"current_data", "weather"}
	if !reflect.DeepEqual(parsed.Capabilities, want) {
		t.Fatalf("Capabilities = %#v, want %#v", parsed.Capabilities, want)
	}
}
