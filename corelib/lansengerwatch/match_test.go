package lansengerwatch

import (
	"context"
	"runtime"
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

func TestCLICommandForExecutionQuotesGroupText(t *testing.T) {
	p := CLIParams{Content: `$(whoami); echo "pwned"`, SpeakerID: "u'1"}
	command := cliCommandForExecution("hook --content={{content}} --speaker={{speaker_id}}", p)
	if runtime.GOOS == "windows" {
		if strings.Contains(command, p.Content) || !strings.Contains(command, "!LANXIN_WATCH_CONTENT!") {
			t.Fatalf("windows command must use delayed environment expansion: %q", command)
		}
		return
	}
	if strings.Contains(command, p.Content) || !strings.Contains(command, `'$(whoami); echo "pwned"'`) {
		t.Fatalf("posix command must quote content: %q", command)
	}
	if !strings.Contains(command, `'u'"'"'1'`) {
		t.Fatalf("single quote must remain one shell word: %q", command)
	}
}

func TestRunCLIGroupTextIsNotParsedAsShellSyntax(t *testing.T) {
	p := CLIParams{Content: `group text; echo INJECTED`}
	command := "printf '%s' {{content}}"
	if runtime.GOOS == "windows" {
		command = "echo {{content}}"
	}
	result := RunCLI(context.Background(), command, p, 5)
	if result.Err != nil {
		t.Fatalf("RunCLI failed: %v (%s)", result.Err, result.Stderr)
	}
	if strings.Contains(result.Stdout, "INJECTED") && result.Stdout != p.Content {
		t.Fatalf("group text was parsed as shell syntax: %q", result.Stdout)
	}
	if result.Stdout != p.Content {
		t.Fatalf("stdout = %q, want exact group text %q", result.Stdout, p.Content)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
