package main

import (
	"testing"
)

func TestParseGitDiffStat_TypicalOutput(t *testing.T) {
	input := ` src/auth/login.go      | 103 +++++++++--
 src/auth/login_test.go |  45 +++++
 src/routes/router.go   |  20 ++-
 3 files changed, 140 insertions(+), 28 deletions(-)`

	stat := parseGitDiffStat(input)
	if stat == nil {
		t.Fatal("parseGitDiffStat returned nil")
	}
	if stat.FilesChanged != 3 {
		t.Errorf("FilesChanged: got %d, want 3", stat.FilesChanged)
	}
	if stat.Insertions != 140 {
		t.Errorf("Insertions: got %d, want 140", stat.Insertions)
	}
	if stat.Deletions != 28 {
		t.Errorf("Deletions: got %d, want 28", stat.Deletions)
	}
	if len(stat.FileStats) != 3 {
		t.Errorf("FileStats count: got %d, want 3", len(stat.FileStats))
	}
	if stat.FileStats[0].Path != "src/auth/login.go" {
		t.Errorf("FileStats[0].Path: got %q", stat.FileStats[0].Path)
	}
}

func TestParseGitDiffStat_InsertionsOnly(t *testing.T) {
	input := ` new_file.go | 50 ++++++++++++++++++++++++++++++++++++++++++++++++++
 1 file changed, 50 insertions(+)`

	stat := parseGitDiffStat(input)
	if stat == nil {
		t.Fatal("parseGitDiffStat returned nil")
	}
	if stat.FilesChanged != 1 {
		t.Errorf("FilesChanged: got %d, want 1", stat.FilesChanged)
	}
	if stat.Insertions != 50 {
		t.Errorf("Insertions: got %d, want 50", stat.Insertions)
	}
	if stat.Deletions != 0 {
		t.Errorf("Deletions: got %d, want 0", stat.Deletions)
	}
}

func TestParseGitDiffStat_DeletionsOnly(t *testing.T) {
	input := ` old_file.go | 30 ------------------------------
 1 file changed, 30 deletions(-)`

	stat := parseGitDiffStat(input)
	if stat == nil {
		t.Fatal("parseGitDiffStat returned nil")
	}
	if stat.Deletions != 30 {
		t.Errorf("Deletions: got %d, want 30", stat.Deletions)
	}
}

func TestParseGitDiffStat_EmptyInput(t *testing.T) {
	if stat := parseGitDiffStat(""); stat != nil {
		t.Error("empty input should return nil")
	}
	if stat := parseGitDiffStat("   \n  "); stat != nil {
		t.Error("whitespace-only input should return nil")
	}
}

func TestParseGitDiffStat_NoSummaryLine(t *testing.T) {
	// Some git versions don't print the summary when piped
	input := ` src/main.go | 10 +++++++---`

	stat := parseGitDiffStat(input)
	if stat == nil {
		t.Fatal("should derive stats from file lines when no summary")
	}
	if stat.FilesChanged != 1 {
		t.Errorf("FilesChanged: got %d, want 1", stat.FilesChanged)
	}
	if stat.FileStats[0].Insertions+stat.FileStats[0].Deletions != 10 {
		t.Errorf("total changes should be 10, got ins=%d del=%d",
			stat.FileStats[0].Insertions, stat.FileStats[0].Deletions)
	}
}

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
