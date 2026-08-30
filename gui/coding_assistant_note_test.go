package main

import "testing"

func TestCompactCodingAssistantNoteFiltersInternal(t *testing.T) {
	if got := compactCodingAssistantNote("Compiling the new hello world."); got == "" {
		t.Fatal("expected engineer note to survive")
	}
	if got := compactCodingAssistantNote("I will add a compile check next."); got == "" {
		t.Fatal("expected engineer note to survive")
	}
	cases := []string{
		"## 执行报告 总计：1",
		"## 验证结果",
		"## 涉及文件",
		"### 计划执行结果",
		"执行步骤： ☐ T1 write",
		"质量审计 PASSED",
		`{"name":"write_file","arguments":{}}`,
		"<think>planning</think>",
		"tool_call write_file",
		"\x01The\x01 user\x01 wants me to continue completing a programming task.",
	}
	for _, input := range cases {
		if got := compactCodingAssistantNote(input); got != "" {
			t.Fatalf("internal note %q should be dropped, got %q", input, got)
		}
	}
}

func TestCompactCodingAssistantNoteDropsReasoningLane(t *testing.T) {
	soh := "\x01"
	leaked := soh + "The" + soh + " user" + soh + " wants me to continue completing a programming task."
	if got := compactCodingAssistantNote(leaked); got != "" {
		t.Fatalf("leaked reasoning should be dropped, got %q", got)
	}
	pua := string(rune(0xEB90))
	if got := compactCodingAssistantNote("Compiling the " + pua + "new hello world."); got != "Compiling the new hello world." {
		t.Fatalf("visible note should keep text after tofu strip, got %q", got)
	}
}

func TestOnTokenDoesNotEmitAssistantNotes(t *testing.T) {
	var notes []string
	local := &codingSubAgentCallbacks{
		subagent: &CodingSubAgent{
			onToken:    func(string) {},
			onProgress: func(text string) { notes = append(notes, text) },
		},
	}
	local.OnToken("I found the narrowest safe edit.")
	local.OnToken("\x01The user wants me to continue completing a programming task.")
	if len(notes) != 0 {
		t.Fatalf("OnToken must not emit assistant notes, got %v", notes)
	}

	notes = nil
	remote := &remoteCodingCallbacks{
		agent: &RemoteCodingSubAgent{
			onToken:    func(string) {},
			onProgress: func(text string) { notes = append(notes, text) },
		},
	}
	remote.OnToken("I found the narrowest safe edit.")
	remote.OnToken("\x01The user wants me to continue completing a programming task.")
	if len(notes) != 0 {
		t.Fatalf("remote OnToken must not emit assistant notes, got %v", notes)
	}
}
