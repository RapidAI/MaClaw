package lansengerwatch

import (
	"strings"
	"testing"
)

func TestMatchKeywordCaseInsensitive(t *testing.T) {
	rule := KeywordRule{Keywords: []string{"紧急", "P0"}}
	if got := MatchKeyword(rule, "这是一个紧急问题"); got != "紧急" {
		t.Fatalf("got %q", got)
	}
	if got := MatchKeyword(rule, "please check p0 now"); got != "P0" {
		t.Fatalf("got %q", got)
	}
	if got := MatchKeyword(rule, "普通聊天"); got != "" {
		t.Fatalf("expected no match, got %q", got)
	}
}

func TestJobWatchesStaff(t *testing.T) {
	job := Job{Enabled: true, TargetStaffIDs: []string{" a ", "b"}}
	if !JobWatchesStaff(job, "a") {
		t.Fatal("expected match")
	}
	if JobWatchesStaff(job, "c") {
		t.Fatal("unexpected match")
	}
	job.Enabled = false
	if JobWatchesStaff(job, "a") {
		t.Fatal("disabled job must not match")
	}
}

func TestFilterMembers(t *testing.T) {
	members := []Member{
		{StaffID: "s1", Name: "张三"},
		{StaffID: "s2", Name: "李四"},
	}
	got := FilterMembers(members, "张")
	if len(got) != 1 || got[0].StaffID != "s1" {
		t.Fatalf("%+v", got)
	}
	got = FilterMembers(members, "s2")
	if len(got) != 1 || got[0].Name != "李四" {
		t.Fatalf("%+v", got)
	}
}

func TestExpandAndPreferCLI(t *testing.T) {
	p := CLIParams{
		Date: "2026-07-16T10:00:00Z", Content: "hello",
		SpeakerID: "u1", GroupID: "g1", Keyword: "紧急",
	}
	cmd := ExpandCLICommand("echo {{speaker_id}} {{keyword}}", p)
	if cmd != "echo u1 紧急" {
		t.Fatalf("expand: %q", cmd)
	}
	appended := AppendStandardCLIFlags("mycmd", p)
	if !strings.Contains(appended, "--speaker-id") || !strings.Contains(appended, "u1") {
		t.Fatalf("flags: %q", appended)
	}
	trueVal := true
	rule := KeywordRule{ReplyText: "fallback", CLICommand: "x", ReplyWithCLIStdout: &trueVal}
	reply, used := PreferCLIStdout(rule, CLIResult{Stdout: "from-cli", Err: nil})
	if !used || reply != "from-cli" {
		t.Fatalf("prefer cli: %q %v", reply, used)
	}
	reply, used = PreferCLIStdout(rule, CLIResult{Err: errString("fail")})
	if used || reply != "fallback" {
		t.Fatalf("fallback: %q %v", reply, used)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
