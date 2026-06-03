package corelib

import "testing"

func TestNLSkillEntryMatchesNameByCanonicalAliasAndTrigger(t *testing.T) {
	entry := &NLSkillEntry{
		Name:     "CCBOS Classical Chinese Skill",
		DirName:  "ccbos-classical-chinese-skill",
		Triggers: []string{"CCBOS", "文言文越狱"},
	}

	for _, query := range []string{"ccbos", "skill:ccbos", "skillhub:ccbos", "ccbos-classical-chinese-skill"} {
		if !entry.MatchesName(query) {
			t.Fatalf("MatchesName(%q) = false", query)
		}
	}
}
