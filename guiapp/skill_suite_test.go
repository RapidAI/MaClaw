package guiapp

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestDecodeSkillSuiteJSON(t *testing.T) {
	data := []byte(`{"name":"Office","members":[{"skill_id":"pdf","required":true}],"skills":[{"id":"pdf","name":"PDF","version":"1.2.0","files":{"skill.yaml":"bmFtZTogUERGCg=="}}]}`)
	suite, err := decodeSkillSuiteJSON(data, "fallback")
	if err != nil {
		t.Fatalf("decodeSkillSuiteJSON() error = %v", err)
	}
	if suite.ID != "fallback" || len(suite.Skills) != 1 || suite.Skills[0].Files["skill.yaml"] == "" {
		t.Fatalf("unexpected suite: %#v", suite)
	}
}

func TestValidateSkillSuiteZipArchiveRejectsUnsafeAndDuplicateEntries(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"suite.json", "suite.json"} {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte(`{"id":"x","skills":[{"id":"a"}]}`))
	}
	_ = zw.Close()
	if err := validateSkillSuiteZipArchive(buf.Bytes()); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate entry rejection, got %v", err)
	}
}

func TestValidateSkillSuiteZipArchiveRequiresManifestCoverage(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range map[string][]byte{"suite.json": []byte(`{"id":"x","skills":[{"id":"a"}]}`), "suite.yaml": []byte("id: x\n"), "suite_integrity_manifest.json": []byte(`{"files":{"suite.json":"bad"}}`), "skills/a/skill.yaml": []byte("name: a\n")} {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write(data)
	}
	_ = zw.Close()
	if err := validateSkillSuiteZipArchive(buf.Bytes()); err == nil {
		t.Fatal("expected checksum rejection")
	}
}

func TestSkillSuiteArchivePathRejectsWindowsDrive(t *testing.T) {
	for _, name := range []string{"C:/evil", `C:\\evil`, "../evil", "/absolute"} {
		if _, ok := safeSkillSuiteArchivePath(name); ok {
			t.Fatalf("path %q should be rejected", name)
		}
	}
}

func TestDecodeSkillSuiteJSONRejectsEmptyMembers(t *testing.T) {
	_, err := decodeSkillSuiteJSON([]byte(`{"id":"empty","skills":[]}`), "empty")
	if err == nil || !strings.Contains(err.Error(), "contains no skills") {
		t.Fatalf("error = %v, want empty-suite validation", err)
	}
}

func TestValidateSkillSuitePayloadRejectsUnsafeFiles(t *testing.T) {
	suite := &SkillSuiteFull{Skills: []SkillSuiteSkill{{ID: "a", Name: "A", Files: map[string]string{"../evil": "eA=="}}, {ID: "b", Name: "B"}}}
	if err := validateSkillSuitePayload(suite); err == nil {
		t.Fatal("expected unsafe path rejection")
	}
}

func TestValidateSkillSuitePayloadAcceptsMembers(t *testing.T) {
	suite := &SkillSuiteFull{Skills: []SkillSuiteSkill{{ID: "a", Name: "A", Files: map[string]string{"run.sh": "ZWNobyBoaQo="}}, {ID: "b", Name: "B"}}}
	if err := validateSkillSuitePayload(suite); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
