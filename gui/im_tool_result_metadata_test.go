package main

import "testing"

func TestExtractSkillRunIDFromToolTextJSON(t *testing.T) {
	got := extractSkillRunIDFromToolText(`{"run_id":"run-1775734674900-1","status":"running","skill_name":"long_writer"}`)
	if got != "run-1775734674900-1" {
		t.Fatalf("run id = %q, want run-1775734674900-1", got)
	}
}

func TestIsSkillRunTerminalFromTextJSON(t *testing.T) {
	if isSkillRunTerminalFromText(`{"run_id":"run-1","status":"running"}`) {
		t.Fatal("running JSON status should not be terminal")
	}
	if !isSkillRunTerminalFromText(`{"run_id":"run-1","status":"success"}`) {
		t.Fatal("success JSON status should be terminal")
	}
}
