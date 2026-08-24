package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

func TestSkillVisibleInExperienceDomain(t *testing.T) {
	tests := []struct {
		name        string
		skillDomain string
		agentDomain string
		want        bool
	}{
		{"installed skill reaches coding", corelib.SkillDomainUniversal, corelib.SkillDomainCoding, true},
		{"installed skill reaches general", corelib.SkillDomainUniversal, corelib.SkillDomainGeneral, true},
		{"coding skill in coding turn", corelib.SkillDomainCoding, corelib.SkillDomainCoding, true},
		{"chat skill stays out of coding", corelib.SkillDomainGeneral, corelib.SkillDomainCoding, false},
		{"coding skill stays out of chat", corelib.SkillDomainCoding, corelib.SkillDomainGeneral, false},
		{"unresolved agent domain sees everything", corelib.SkillDomainGeneral, corelib.SkillDomainUniversal, true},
		{"unknown skill domain degrades to universal", "weather", corelib.SkillDomainCoding, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := corelib.SkillVisibleInExperienceDomain(tc.skillDomain, tc.agentDomain); got != tc.want {
				t.Fatalf("SkillVisibleInExperienceDomain(%q, %q) = %v, want %v",
					tc.skillDomain, tc.agentDomain, got, tc.want)
			}
		})
	}
}

func TestFilterSkillsForExperienceDomain(t *testing.T) {
	skills := []NLSkillDefinition{
		{Name: "installed-pdf", ExperienceDomain: corelib.SkillDomainUniversal},
		{Name: "craft_beijing_weather", ExperienceDomain: corelib.SkillDomainGeneral},
		{Name: "craft_rebuild_and_test", ExperienceDomain: corelib.SkillDomainCoding},
	}

	coding := filterSkillsForExperienceDomain(corelib.SkillDomainCoding, skills)
	if len(coding) != 2 {
		t.Fatalf("coding pool = %d skills, want 2", len(coding))
	}
	for _, s := range coding {
		if s.Name == "craft_beijing_weather" {
			t.Fatal("a chat-learned skill reached the coding pool")
		}
	}

	general := filterSkillsForExperienceDomain(corelib.SkillDomainGeneral, skills)
	if len(general) != 2 {
		t.Fatalf("general pool = %d skills, want 2", len(general))
	}
	for _, s := range general {
		if s.Name == "craft_rebuild_and_test" {
			t.Fatal("a coding-learned skill reached the general pool")
		}
	}

	// An unresolved domain must not hide installed capabilities.
	if got := filterSkillsForExperienceDomain(corelib.SkillDomainUniversal, skills); len(got) != len(skills) {
		t.Fatalf("unresolved domain filtered %d of %d skills", len(skills)-len(got), len(skills))
	}
}

// The loop's preferred-skill steering is an unprompted recommendation, so it
// must respect the pool boundary the same way prompt injection does.
func TestMatchPreferredLocalSkillRespectsExperienceDomain(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	app := &App{testHomeDir: tempHome}
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{
		Name: "craft_chat_pdf", Status: "active", Source: "learned",
		ExperienceDomain: corelib.SkillDomainGeneral,
		Description:      "把 pdf 报告转成 markdown 综述",
		Triggers:         []string{"pdf"},
		Steps:            []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo pdf"}}},
	}}}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	exec := NewSkillExecutor(app, nil, nil)

	if name, _ := matchPreferredLocalSkill(exec, "把 pdf 转成 markdown", corelib.SkillDomainGeneral); name != "craft_chat_pdf" {
		t.Fatalf("general turn should reach its own pool, got %q", name)
	}
	if name, _ := matchPreferredLocalSkill(exec, "把 pdf 转成 markdown", corelib.SkillDomainCoding); name != "" {
		t.Fatalf("coding turn steered into a chat-learned skill: %q", name)
	}
}

func TestExperienceDomainForTrajectoryKind(t *testing.T) {
	for _, kind := range []string{"coding_subagent", "remote_coding_subagent", "CODING_SUBAGENT"} {
		if got := experienceDomainForTrajectoryKind(kind); got != corelib.SkillDomainCoding {
			t.Errorf("kind %q → %q, want %q", kind, got, corelib.SkillDomainCoding)
		}
	}
	// "main" carries no signal on its own; the session owner decides.
	for _, kind := range []string{"main", "shared", "btw_subagent", ""} {
		if got := experienceDomainForTrajectoryKind(kind); got != corelib.SkillDomainUniversal {
			t.Errorf("kind %q → %q, want the owner to decide", kind, got)
		}
	}
}

// The production shape for a self-learned skill is a full definition in
// skill.yaml plus a thin config overlay that carries only identity and stats —
// never the experience domain. loadSkills must therefore take the domain from
// disk, or every learned skill would load as universal and be advertised in
// both pools, silently defeating the split.
func TestLoadSkillsTakesExperienceDomainFromDisk(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	externalRoot := filepath.Join(tempHome, "external-skills")
	skillDir := filepath.Join(externalRoot, "craft_rebuild_and_test")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := strings.Join([]string{
		"name: craft_rebuild_and_test",
		"description: 重新编译并跑单测",
		"triggers: [rebuild]",
		"status: active",
		"source: learned",
		"experience_domain: coding",
		"steps:",
		"  - action: bash",
		"    params:",
		"      command: go build ./...",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.ExternalSkillDirs = []string{externalRoot}
	// The overlay the auto-summary pipeline persists: identity only.
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:     "craft_rebuild_and_test",
		Source:   "learned",
		SkillDir: skillDir,
		Status:   "active",
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	exec := NewSkillExecutor(app, nil, nil)
	var found *NLSkillDefinition
	listed := exec.List()
	for i := range listed {
		if listed[i].Name == "craft_rebuild_and_test" {
			found = &listed[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("learned skill missing from List(): %+v", listed)
	}
	if found.ExperienceDomain != corelib.SkillDomainCoding {
		t.Fatalf("domain = %q, want %q — the thin config overlay erased the on-disk value",
			found.ExperienceDomain, corelib.SkillDomainCoding)
	}

	// The whole point: a general turn must not see it.
	if got := filterSkillsForExperienceDomain(corelib.SkillDomainGeneral, []NLSkillDefinition{*found}); len(got) != 0 {
		t.Fatal("a coding-learned skill reached a general turn")
	}
}

// Self-repair rewrites the authoritative skill.yaml from an in-memory entry, so
// a domain missing from that entry would be erased from disk permanently rather
// than just hidden for one turn. This walks the production chain — load, repair,
// rewrite — to keep that path honest.
func TestSelfRepairRewriteKeepsExperienceDomainOnDisk(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	skillDir := filepath.Join(tempHome, "skills", "craft_rebuild_and_test")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := strings.Join([]string{
		"name: craft_rebuild_and_test",
		"description: A learned skill that rebuilds and runs the test suite.",
		"triggers:",
		"  - rebuild",
		"platforms:",
		"  - universal",
		"source: learned",
		"experience_domain: coding",
		"steps:",
		"  - action: bash",
		"    params:",
		"      command: echo old",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.ExternalSkillDirs = []string{filepath.Join(tempHome, "skills")}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name: "craft_rebuild_and_test", Source: "learned", SkillDir: skillDir, Status: "active",
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)

	// Production shape: the repair flow operates on an entry from loadSkills.
	var entry *corelib.NLSkillEntry
	for _, s := range app.skillExecutor.loadSkills() {
		if s.Name == "craft_rebuild_and_test" {
			cp := s
			entry = &cp
			break
		}
	}
	if entry == nil {
		t.Fatal("learned skill missing from loadSkills()")
	}
	entry.Steps = []corelib.NLSkillStep{{
		Action: "bash", Params: map[string]interface{}{"command": "echo repaired"},
	}}

	runner := NewSkillRunner(app.skillExecutor)
	if err := runner.persistRepairResult(entry); err != nil {
		t.Fatalf("persistRepairResult() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(skillDir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	rewritten, err := skill.ParseSkillYAMLFile(data)
	if err != nil {
		t.Fatalf("ParseSkillYAMLFile() error = %v", err)
	}
	if rewritten.ExperienceDomain != corelib.SkillDomainCoding {
		t.Fatalf("repair erased the experience domain from disk: got %q\n%s",
			rewritten.ExperienceDomain, data)
	}
	// Sanity: the repair itself did land, so this is not a no-op write.
	if len(rewritten.Steps) != 1 || rewritten.Steps[0].Params["command"] != "echo repaired" {
		t.Fatalf("repaired steps missing from rewritten definition:\n%s", data)
	}
}

// A shared skill must arrive universal: the recipient installs it deliberately,
// so hiding it from half their agents because of which pool the *author*
// distilled it from would be wrong. The local definition writer, which shares
// the same builder, must still keep the domain.
func TestOutboundSkillPackageDropsExperienceDomain(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name:        "craft_rebuild_and_test",
		Description: "重新编译并跑单测",
		Triggers:    []string{"rebuild"},
		Status:      "active",
		Steps: []corelib.NLSkillStep{{
			Action: "bash", Params: map[string]interface{}{"command": "go build ./..."},
		}},
		ExperienceDomain: corelib.SkillDomainCoding,
	}

	// The shared builder keeps the domain, because it also writes the local
	// definition where the domain must survive a rewrite.
	if got := buildSkillYAMLFileFromPackageEntry(entry).ExperienceDomain; got != corelib.SkillDomainCoding {
		t.Fatalf("local definition builder dropped the domain: got %q", got)
	}

	dir := t.TempDir()
	if err := writePackageViewSkillYAML(dir, entry); err != nil {
		t.Fatalf("writePackageViewSkillYAML() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "skill.yaml"))
	if err != nil {
		t.Fatalf("read generated skill.yaml: %v", err)
	}
	if strings.Contains(string(data), "experience_domain") {
		t.Fatalf("outbound package leaked the experience domain:\n%s", data)
	}
	parsed, err := skill.ParseSkillYAMLFile(data)
	if err != nil {
		t.Fatalf("ParseSkillYAMLFile() error = %v", err)
	}
	if parsed.ExperienceDomain != "" {
		t.Fatalf("packaged skill domain = %q, want universal", parsed.ExperienceDomain)
	}
	// Packaging must not mutate the caller's entry.
	if entry.ExperienceDomain != corelib.SkillDomainCoding {
		t.Fatalf("packaging mutated the source entry domain to %q", entry.ExperienceDomain)
	}
}

// The experience extractor only ever analyses remote coding sessions, so what it
// learns belongs to the coding pool rather than being universal.
func TestStampCodingExperienceDomain(t *testing.T) {
	blank := &corelib.NLSkillEntry{Name: "from-remote-coding"}
	stampCodingExperienceDomain(blank)
	if blank.ExperienceDomain != corelib.SkillDomainCoding {
		t.Fatalf("blank domain = %q, want %q", blank.ExperienceDomain, corelib.SkillDomainCoding)
	}

	// Refining an existing recipe must not reclassify what it is for.
	existing := &corelib.NLSkillEntry{Name: "chat-learned", ExperienceDomain: corelib.SkillDomainGeneral}
	stampCodingExperienceDomain(existing)
	if existing.ExperienceDomain != corelib.SkillDomainGeneral {
		t.Fatalf("existing domain reclassified to %q", existing.ExperienceDomain)
	}

	stampCodingExperienceDomain(nil) // must not panic
}

// The main loop resolves the domain on a handler its own surrounding code
// treats as possibly nil, so nil-receiver safety here is load-bearing rather
// than incidental: a panic would take down trajectory recording for the turn.
func TestResolveTrajectoryExperienceDomainNilHandler(t *testing.T) {
	var h *IMMessageHandler

	if got := h.resolveTrajectoryExperienceDomain("main", "desktop-user"); got != corelib.SkillDomainGeneral {
		t.Fatalf("nil handler main loop = %q, want %q", got, corelib.SkillDomainGeneral)
	}
	// A coding loop kind is decided without consulting the handler at all.
	if got := h.resolveTrajectoryExperienceDomain("coding_subagent", "desktop-user"); got != corelib.SkillDomainCoding {
		t.Fatalf("nil handler coding subagent = %q, want %q", got, corelib.SkillDomainCoding)
	}
	if got := h.skillExperienceDomainForOwner("desktop-user"); got != corelib.SkillDomainGeneral {
		t.Fatalf("nil handler owner domain = %q, want %q", got, corelib.SkillDomainGeneral)
	}
}

// resolveTrajectoryExperienceDomain is the write side: it decides which pool a
// session's learned skills land in, and a wrong answer here mislabels a skill
// on disk permanently. Both signals it combines are covered.
func TestResolveTrajectoryExperienceDomain(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	const codingTab = "desktop-coding"
	const chatTab = "desktop-chat"
	h := &IMMessageHandler{}
	h.stickyCodingWorkbenchMemory.Store(codingTab, stickyCodingWorkbenchMemory{Kind: "local"})

	tests := []struct {
		name   string
		kind   string
		userID string
		want   string
	}{
		{"coding subagent under a chat tab is still coding", "coding_subagent", chatTab, corelib.SkillDomainCoding},
		{"remote coding subagent is coding", "remote_coding_subagent", chatTab, corelib.SkillDomainCoding},
		{"main loop in a coding workbench tab is coding", "main", codingTab, corelib.SkillDomainCoding},
		{"main loop in a plain chat tab is general", "main", chatTab, corelib.SkillDomainGeneral},
		{"unknown owner falls back to general", "main", "", corelib.SkillDomainGeneral},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := h.resolveTrajectoryExperienceDomain(tc.kind, tc.userID); got != tc.want {
				t.Fatalf("resolveTrajectoryExperienceDomain(%q, %q) = %q, want %q",
					tc.kind, tc.userID, got, tc.want)
			}
		})
	}
}
