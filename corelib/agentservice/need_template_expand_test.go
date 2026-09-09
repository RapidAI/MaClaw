package agentservice

import (
	"reflect"
	"testing"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestExpandNeedTemplateSiblingsPreservesFamilyIdentity(t *testing.T) {
	template := IntentCapabilityNeedTemplate{
		Capability:     "information.search.web",
		Qualifiers:     map[string]string{"freshness": "current"},
		Required:       true,
		MaxInvocations: 3,
	}
	got := ExpandNeedTemplateSiblings(template, ExpandNeedTemplateOptions{
		Confidence:  0.9,
		EvidenceIDs: []string{"intent:search"},
	})
	key := NeedTemplateIdentityKey(template)
	baseID := "need:information.search.web:" + coretool.SchemaDigest([]byte(key))[:12]
	if len(got) != 3 {
		t.Fatalf("siblings=%d, want 3: %#v", len(got), got)
	}
	if got[0].ID != baseID || got[1].ID != coretool.RepeatSiblingNeedID(baseID, 1) || got[2].ID != coretool.RepeatSiblingNeedID(baseID, 2) {
		t.Fatalf("family identity drifted: %#v want base %q", got, baseID)
	}
	if !got[0].Required || got[1].Required || got[2].Required {
		t.Fatalf("only the first sibling should be required: %#v", got)
	}
	if got[0].Polarity != coretool.NeedRequire || got[0].Confidence != 0.9 {
		t.Fatalf("default polarity/confidence drifted: %#v", got[0])
	}
	template.Qualifiers["freshness"] = "reference"
	got[0].Qualifiers["freshness"] = "changed"
	if got[1].Qualifiers["freshness"] != "current" {
		t.Fatalf("siblings must not share qualifier maps: %#v", got)
	}

	optional := ExpandNeedTemplateSiblings(template, ExpandNeedTemplateOptions{ForceOptional: true, IDPrefix: "need:coding:"})
	if len(optional) != 3 || optional[0].Required || optional[1].Required {
		t.Fatalf("ForceOptional must drop Required: %#v", optional)
	}
	if optional[0].ID == "" || optional[0].ID[:12] != "need:coding:" {
		t.Fatalf("coding prefix drifted: %#v", optional[0])
	}
}

func TestExpandNeedTemplatesDropsDuplicateIdentities(t *testing.T) {
	templates := []IntentCapabilityNeedTemplate{
		{Capability: "fs.read.local", Required: true, MaxInvocations: 2},
		{Capability: "fs.read.local", Required: true, MaxInvocations: 8},
		{Capability: "fs.write.local", Required: true},
	}
	got := ExpandNeedTemplates(templates, ExpandNeedTemplateOptions{IDPrefix: "need:coding:", Confidence: 1, EvidenceIDs: []string{"host:coding"}})
	if len(got) != 3 { // 2 read siblings from the first mapping + 1 write.
		t.Fatalf("dedup siblings=%d, want 3: %#v", len(got), got)
	}
	if got[0].Capability != "fs.read.local" || got[2].Capability != "fs.write.local" {
		t.Fatalf("dedup order drifted: %#v", got)
	}
	if !reflect.DeepEqual(got[0].EvidenceIDs, []string{"host:coding"}) {
		t.Fatalf("evidence drifted: %#v", got[0])
	}
}
