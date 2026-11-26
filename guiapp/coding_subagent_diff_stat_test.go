package guiapp

import (
	"testing"
)

func TestSubAgentDiffStat_Summary(t *testing.T) {
	stat := &SubAgentDiffStat{FilesChanged: 3, Insertions: 100, Deletions: 25}
	want := "3 files changed (+100 -25)"
	if got := stat.Summary(); got != want {
		t.Errorf("Summary: got %q, want %q", got, want)
	}
}

func TestSubAgentDiffStat_Summary_Nil(t *testing.T) {
	var stat *SubAgentDiffStat
	if got := stat.Summary(); got != "no changes" {
		t.Errorf("nil stat Summary: got %q, want %q", got, "no changes")
	}
}

func TestSubAgentDiffStatSummaryDisclosesUnknownCounts(t *testing.T) {
	stat := &SubAgentDiffStat{
		FilesChanged: 2,
		Insertions:   4,
		Deletions:    1,
		FileStats: []SubAgentFileDiffStat{
			{Path: "a.go", Insertions: 4, Deletions: 1, HasCounts: true},
			{Path: "blob.bin"},
		},
	}
	if got := stat.Summary(); got != "2 files changed (+4 -1, 1 unknown)" {
		t.Fatalf("Summary()=%q", got)
	}
}

func TestParseGitDiffNumStatExactCountsAndBinary(t *testing.T) {
	input := "7\t3\tsrc/foo.go\n" +
		"2\t1\tspace name.go\n" +
		"-\t-\tassets/blob.bin\n"
	stat := parseGitDiffNumStat(input)
	if stat == nil || len(stat.FileStats) != 3 {
		t.Fatalf("numstat=%v", stat)
	}
	if stat.FileStats[0].Insertions != 7 || stat.FileStats[0].Deletions != 3 || !stat.FileStats[0].HasCounts {
		t.Fatalf("text numstat was not exact: %+v", stat.FileStats[0])
	}
	if stat.FileStats[1].Path != "space name.go" {
		t.Fatalf("path with spaces was not preserved: %+v", stat.FileStats[1])
	}
	if stat.FileStats[2].HasCounts || stat.FileStats[2].Insertions != 0 || stat.FileStats[2].Deletions != 0 {
		t.Fatalf("binary numstat must keep counts unknown, not fake zeroes: %+v", stat.FileStats[2])
	}
	if stat.Insertions != 9 || stat.Deletions != 4 || stat.FilesChanged != 3 {
		t.Fatalf("aggregate numstat mismatch: %+v", stat)
	}
}

func TestParseGitDiffNumStatDecodesQuotedUnicodePath(t *testing.T) {
	input := "1\t0\t\"\\346\\265\\213\\350\\257\\225 file.go\"\n"
	stat := parseGitDiffNumStat(input)
	if stat == nil || len(stat.FileStats) != 1 {
		t.Fatalf("numstat=%v", stat)
	}
	if stat.FileStats[0].Path != "测试 file.go" {
		t.Fatalf("quoted unicode path decoded to %q", stat.FileStats[0].Path)
	}
}

func TestGitNumstatDisplayPathUsesPostRenamePath(t *testing.T) {
	for input, want := range map[string]string{
		"old.go => new.go":               "new.go",
		"dir/{old.go => new.go}/main.go": "dir/new.go/main.go",
		"dir/{old.txt => new.txt}":       "dir/new.txt",
		"plain.go":                       "plain.go",
	} {
		if got := gitNumstatDisplayPath(input); got != want {
			t.Fatalf("gitNumstatDisplayPath(%q)=%q want %q", input, got, want)
		}
	}
}

func TestMergeSubAgentDiffStatPrefersTurnNetEvidenceOverGit(t *testing.T) {
	gitEvidence := &SubAgentDiffStat{FileStats: []SubAgentFileDiffStat{{Path: "dirty.go", Insertions: 100, Deletions: 90, HasCounts: true}}}
	merged := mergeSubAgentDiffStat(gitEvidence, []SubAgentFileDiffStat{{Path: "dirty.go", Insertions: 2, Deletions: 1, HasCounts: true}})
	if merged == nil || len(merged.FileStats) != 1 {
		t.Fatalf("merged=%v", merged)
	}
	if merged.FileStats[0].Insertions != 2 || merged.FileStats[0].Deletions != 1 {
		t.Fatalf("turn-net evidence must override HEAD-relative git counts: %+v", merged.FileStats[0])
	}
}

func TestMergeSubAgentDiffStatUnknownTurnEvidencePoisonsGitPrecision(t *testing.T) {
	gitEvidence := &SubAgentDiffStat{FileStats: []SubAgentFileDiffStat{{Path: "large.go", Insertions: 100, Deletions: 90, HasCounts: true}}}
	merged := mergeSubAgentDiffStat(gitEvidence, []SubAgentFileDiffStat{{Path: "large.go", Insertions: 20, Deletions: 0, HasCounts: false}})
	if merged == nil || len(merged.FileStats) != 1 {
		t.Fatalf("merged=%v", merged)
	}
	if merged.FileStats[0].HasCounts {
		t.Fatalf("unknown turn-net evidence must not borrow HEAD-relative precision: %+v", merged.FileStats[0])
	}
}

func TestMergeSubAgentDiffStatUnknownFallbackDoesNotPoisonExactNetEvidence(t *testing.T) {
	gitEvidence := &SubAgentDiffStat{FileStats: []SubAgentFileDiffStat{{Path: "demo.go", Insertions: 100, Deletions: 90, HasCounts: true}}}
	merged := mergeSubAgentDiffStat(gitEvidence, []SubAgentFileDiffStat{
		{Path: "demo.go", Insertions: 2, Deletions: 1, HasCounts: true},
		{Path: "demo.go", Insertions: 20, Deletions: 0, HasCounts: false},
	})
	if merged == nil || len(merged.FileStats) != 1 {
		t.Fatalf("merged=%v", merged)
	}
	if !merged.FileStats[0].HasCounts || merged.FileStats[0].Insertions != 2 || merged.FileStats[0].Deletions != 1 {
		t.Fatalf("unknown fallback poisoned exact net evidence: %+v", merged.FileStats[0])
	}
}

func TestMergeSubAgentDiffStatLaterExactDoesNotOverrideUnknownTurnEvidence(t *testing.T) {
	gitEvidence := &SubAgentDiffStat{FileStats: []SubAgentFileDiffStat{{Path: "demo.go", Insertions: 100, Deletions: 90, HasCounts: true}}}
	merged := mergeSubAgentDiffStat(gitEvidence, []SubAgentFileDiffStat{
		{Path: "demo.go", HasCounts: false},
		{Path: "demo.go", Insertions: 2, Deletions: 1, HasCounts: true},
	})
	if merged == nil || len(merged.FileStats) != 1 {
		t.Fatalf("merged=%v", merged)
	}
	if merged.FileStats[0].HasCounts || merged.FileStats[0].Insertions != 0 || merged.FileStats[0].Deletions != 0 {
		t.Fatalf("later per-operation precision must not override unknown turn evidence: %+v", merged.FileStats[0])
	}
}

func TestSubAgentDiffEvidencePathsMatchDoesNotMergeNestedRelativeSuffixes(t *testing.T) {
	if subAgentDiffEvidencePathsMatch("a/main.go", "main.go") {
		t.Fatal("relative nested suffixes must not share measured evidence")
	}
	if !subAgentDiffEvidencePathsMatch("/repo/a/main.go", "a/main.go") {
		t.Fatal("absolute and project-relative forms of the same path must match")
	}
	if subAgentDiffEvidencePathsMatch("/repo/Foo.go", "/repo/foo.go") {
		t.Fatal("case-distinct POSIX paths must not share measured evidence")
	}
	if !subAgentDiffEvidencePathsMatch(`C:/Repo/Foo.go`, `c:/repo/foo.go`) {
		t.Fatal("Windows absolute paths remain case-insensitive")
	}
}

func TestCodingSubAgentTaskFileChangesDoesNotMarkNestedSuffixCreated(t *testing.T) {
	changes := codingSubAgentTaskFileChanges(&SubAgentDiffStat{FileStats: []SubAgentFileDiffStat{{Path: "a/main.go", Insertions: 1, HasCounts: true}}}, []string{"b/main.go"})
	if len(changes) != 1 {
		t.Fatalf("changes=%v", changes)
	}
	if changes[0].Created {
		t.Fatalf("different relative nested paths must not borrow created status: %+v", changes[0])
	}
}

func TestFilterSubAgentDiffStatToFilesDropsUnrelatedDirtyWorkspaceRows(t *testing.T) {
	stat := &SubAgentDiffStat{FileStats: []SubAgentFileDiffStat{
		{Path: "task.go", Insertions: 2, Deletions: 1, HasCounts: true},
		{Path: "unrelated.go", Insertions: 100, Deletions: 90, HasCounts: true},
	}}
	filtered := filterSubAgentDiffStatToFiles(stat, []string{"task.go"}, nil)
	if filtered == nil || len(filtered.FileStats) != 1 || filtered.FileStats[0].Path != "task.go" {
		t.Fatalf("filtered=%v", filtered)
	}
	if filtered.Insertions != 2 || filtered.Deletions != 1 || filtered.FilesChanged != 1 {
		t.Fatalf("filtered aggregate mismatch: %+v", filtered)
	}
	if filtered := filterSubAgentDiffStatToFiles(stat, []string{"other.go"}, nil); filtered != nil {
		t.Fatalf("unrelated-only diff evidence must be dropped: %+v", filtered)
	}
}

func TestRememberFileLineBaselineReportsBudgetExhaustion(t *testing.T) {
	oldBudget := codingMaxTrackedBaselineBytes
	codingMaxTrackedBaselineBytes = 3
	defer func() { codingMaxTrackedBaselineBytes = oldBudget }()

	cb := &codingSubAgentCallbacks{}
	cb.trackFile("a.go")
	if !cb.rememberFileLineBaseline("a.go", "abc") {
		t.Fatal("first baseline within budget should be retained")
	}
	cb.trackFile("b.go")
	if cb.rememberFileLineBaseline("b.go", "x") {
		t.Fatal("baseline beyond budget must report failure so caller can keep unknown-touch evidence")
	}
	if len(cb.fileLineBaselines) != 1 {
		t.Fatalf("baselines=%v", cb.fileLineBaselines)
	}
}

func TestMergeSubAgentDiffStatKeepsZeroUnknownEvidence(t *testing.T) {
	gitEvidence := &SubAgentDiffStat{FileStats: []SubAgentFileDiffStat{{Path: "large.go", Insertions: 100, Deletions: 90, HasCounts: true}}}
	merged := mergeSubAgentDiffStat(gitEvidence, []SubAgentFileDiffStat{{Path: "large.go", HasCounts: false}})
	if merged == nil || len(merged.FileStats) != 1 {
		t.Fatalf("zero unknown evidence must not be dropped: %+v", merged)
	}
	if merged.FileStats[0].HasCounts || merged.FileStats[0].Insertions != 0 || merged.FileStats[0].Deletions != 0 {
		t.Fatalf("zero unknown evidence must poison HEAD-relative git precision: %+v", merged.FileStats[0])
	}
}

func TestMergeSubAgentDiffStatKeepsExactZeroOverFallbackChurn(t *testing.T) {
	merged := mergeSubAgentDiffStat(nil, []SubAgentFileDiffStat{
		{Path: "demo.go", HasCounts: true},
		{Path: "demo.go", Insertions: 8, Deletions: 8},
	})
	if merged == nil || len(merged.FileStats) != 1 {
		t.Fatalf("merged=%v", merged)
	}
	if !merged.FileStats[0].HasCounts || merged.FileStats[0].Insertions != 0 || merged.FileStats[0].Deletions != 0 {
		t.Fatalf("exact zero was overwritten by fallback churn: %+v", merged.FileStats[0])
	}
}
