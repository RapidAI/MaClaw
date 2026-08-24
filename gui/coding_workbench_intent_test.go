package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatCodingRequestUnderstandingTextIsPlainProse(t *testing.T) {
	got := formatCodingRequestUnderstandingText("把现有的控制台贪吃蛇改成图形界面版本，保留原有玩法。")
	if got == "" || strings.Contains(got, "##") || strings.Contains(got, "需求理解") {
		t.Fatalf("streamed understanding must be plain prose, got %q", got)
	}
	var tokens []string
	emitCodingRequestUnderstanding(func(s string) { tokens = append(tokens, s) }, got)
	if len(tokens) != 1 || tokens[0] != got {
		t.Fatalf("emit tokens=%q", tokens)
	}
}

func TestFallbackCodingRequestRestatementDoesNotCopyUser(t *testing.T) {
	user := "\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248"
	got := fallbackCodingRequestRestatement(user, stickyCodingWorkbenchMemory{
		SessionPlan: "\u63a7\u5236\u53f0\u8d2a\u5403\u86c7, ANSI",
		LastSummary: "done snake.cpp console",
	})
	if got == "" {
		t.Fatal("expected restatement")
	}
	if codingRestatementCopiesUser(got, user) {
		t.Fatalf("restatement copied the user command: %q", got)
	}
	if !strings.Contains(got, "\u56fe\u5f62\u754c\u9762") {
		t.Fatalf("restatement should name the GUI change: %q", got)
	}
	if !strings.Contains(got, "\u8d2a\u5403\u86c7") {
		t.Fatalf("restatement should use session context: %q", got)
	}
}

func TestParseCodingRequestRestatementRejectsCopy(t *testing.T) {
	got := parseCodingRequestRestatement("{\"restatement\":\"\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248\"}")
	if got != "\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248" {
		t.Fatalf("parse=%q", got)
	}
	if !codingRestatementCopiesUser(got, "\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248") {
		t.Fatal("copy detector missed verbatim restatement")
	}
}

func TestResolveCodingRequestUnderstandingUsesFallbackWithoutLLM(t *testing.T) {
	h := &IMMessageHandler{}
	got := h.resolveCodingRequestUnderstanding("\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248", stickyCodingWorkbenchMemory{
		LastSummary: "snake.cpp console snake built",
	}, codingRequestDecision{Kind: codingRequestImplementation, NeedsPlan: true})
	if codingRestatementCopiesUser(got, "\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248") {
		t.Fatalf("copied user text: %q", got)
	}
	if got == "" {
		t.Fatal("empty restatement")
	}
}

func TestCodingRequestShouldNotRestateWorkspaceClear(t *testing.T) {
	if codingRequestShouldRestate(codingRequestDecision{Kind: codingRequestImplementation}, "\u6e05\u7a7a\u5f53\u524d\u76ee\u5f55") {
		t.Fatal("workspace clear must stay on the host wipe path")
	}
}

func TestResolveCodingWorkbenchTasksPlansGUIRewriteFollowUp(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	understood := "\u628a\u73b0\u6709\u63a7\u5236\u53f0\u8d2a\u5403\u86c7\u6539\u6210\u56fe\u5f62\u754c\u9762\u7248\u672c"
	h.setStickyCodingRequirementRestatement(userID, understood)
	mem := stickyCodingWorkbenchMemory{
		TurnCount:              1,
		SessionPlan:            "\u63a7\u5236\u53f0\u8d2a\u5403\u86c7",
		LastSummary:            "snake.cpp console done",
		RequirementRestatement: understood,
	}
	tasks, md, planned := h.resolveCodingWorkbenchTasksWithDecision(
		userID,
		"\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248",
		"D:/repo",
		mem,
		codingRequestDecision{Kind: codingRequestImplementation, NeedsPlan: true},
		nil,
		nil,
	)
	if !planned || len(tasks) < 2 {
		t.Fatalf("GUI rewrite follow-up must plan, planned=%v steps=%d", planned, len(tasks))
	}
	if !strings.Contains(md, "\u63a7\u5236\u53f0") && !strings.Contains(md, "\u56fe\u5f62\u754c\u9762") {
		t.Fatalf("plan goal should be a restatement, markdown=%q", md)
	}
}

func TestFallbackIgnoresGenericTaskTitle(t *testing.T) {
	user := "\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248"
	got := fallbackCodingRequestRestatement(user, stickyCodingWorkbenchMemory{
		SessionPlan: "\u65b0\u5efa\u672c\u5730\u7f16\u7a0b\u4efb\u52a1-1787267059475988200",
	})
	if strings.Contains(got, "\u65b0\u5efa\u672c\u5730\u7f16\u7a0b\u4efb\u52a1") {
		t.Fatalf("generic task title leaked into restatement: %q", got)
	}
	if !strings.Contains(got, "\u56fe\u5f62\u754c\u9762") {
		t.Fatalf("rewrite restatement lost the GUI target: %q", got)
	}
}

func TestPublishCodingRequestUnderstandingEmitsBeforeRefine(t *testing.T) {
	h := &IMMessageHandler{}
	var tokens []string
	got := h.publishCodingRequestUnderstanding("", "\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248", stickyCodingWorkbenchMemory{
		SessionPlan: "\u63a7\u5236\u53f0\u8d2a\u5403\u86c7",
	}, func(s string) { tokens = append(tokens, s) })
	if got == "" || len(tokens) != 1 || tokens[0] != got {
		t.Fatalf("publish tokens=%q got=%q", tokens, got)
	}
	if strings.Contains(got, "##") || strings.Contains(got, "\u9700\u6c42\u7406\u89e3") {
		t.Fatalf("first prose must stay plain: %q", got)
	}
}

func TestJoinCodingUnderstandingAndBody(t *testing.T) {
	got := joinCodingUnderstandingAndBody("keep the snake playable", "## \u9700\u8981\u786e\u8ba4\u6267\u884c\u8ba1\u5212\n\n2 steps")
	if !strings.HasPrefix(got, "keep the snake playable\n\n") {
		t.Fatalf("joined=%q", got)
	}
	if joinCodingUnderstandingAndBody("same", "same\n\nbody") != "same\n\nbody" {
		t.Fatal("prefix body must not be duplicated")
	}
}

func TestCodingSessionContextLooksGeneric(t *testing.T) {
	if !codingSessionContextLooksGeneric("\u65b0\u5efa\u672c\u5730\u7f16\u7a0b\u4efb\u52a1-1") {
		t.Fatal("default local task title must be generic")
	}
	if codingSessionContextLooksGeneric("\u63a7\u5236\u53f0\u8d2a\u5403\u86c7") {
		t.Fatal("real session goal must stay usable")
	}
}

func TestCodingTextMentionsGUIWordBoundary(t *testing.T) {
	if codingTextMentionsGUI("follow the guidelines") {
		t.Fatal("guidelines must not count as a GUI rewrite")
	}
	if !codingTextMentionsGUI("port to GUI") || !codingTextMentionsGUI("\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248") {
		t.Fatal("explicit GUI asks must match")
	}
}

func TestFallbackDoesNotTreatGuidelinesAsGUI(t *testing.T) {
	got := fallbackCodingRequestRestatement("follow the guidelines", stickyCodingWorkbenchMemory{})
	if strings.Contains(got, "\u56fe\u5f62\u754c\u9762") || strings.Contains(strings.ToLower(got), "gui") {
		t.Fatalf("guidelines leaked into a GUI restatement: %q", got)
	}
}

func TestFallbackUsesWorkspaceSourceIdentity(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "snake.cpp"), []byte("int main(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := fallbackCodingRequestRestatement("\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248", stickyCodingWorkbenchMemory{
		ProjectPath: dir,
	})
	if !strings.Contains(strings.ToLower(got), "snake") {
		t.Fatalf("workspace source identity missing: %q", got)
	}
}

func TestFallbackUsesReadmeIdentity(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Console Snake\n\nA demo.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := fallbackCodingRequestRestatement("\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248", stickyCodingWorkbenchMemory{
		ProjectPath: dir,
	})
	if !strings.Contains(got, "Console Snake") {
		t.Fatalf("readme identity missing: %q", got)
	}
}

func TestResolveCodingWorkbenchTasksSeedsRestatementNotShortCommand(t *testing.T) {
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	understood := "\u628a\u5f53\u524d\u9879\u76ee\u6539\u6210\u56fe\u5f62\u754c\u9762\u7248\u672c\uff0c\u4fdd\u7559\u539f\u6709\u6838\u5fc3\u884c\u4e3a"
	_, _, planned := h.resolveCodingWorkbenchTasksWithDecision(
		userID,
		"\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248",
		"D:/repo",
		stickyCodingWorkbenchMemory{RequirementRestatement: understood},
		codingRequestDecision{Kind: codingRequestImplementation, NeedsPlan: true},
		nil,
		nil,
	)
	if !planned {
		t.Fatal("rewrite must still plan")
	}
	got := h.getStickyCodingWorkbenchMemory(userID).SessionPlan
	if !strings.Contains(got, "\u56fe\u5f62\u754c\u9762") {
		t.Fatalf("session plan should seed from restatement, got %q", got)
	}
	if got == "\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248" {
		t.Fatal("short follow-up must not become the session goal")
	}
}

func TestCodingRequestLooksRunOnly(t *testing.T) {
	if !codingRequestLooksRunOnly("\u8fd0\u884c\u4e00\u4e0b") {
		t.Fatal("short run request must stay operational")
	}
	if codingRequestLooksRunOnly("\u8fd0\u884c\u5e76\u52a0\u4e0a\u65e5\u5fd7") {
		t.Fatal("run-and-edit must not be treated as run-only")
	}
	got := fallbackCodingRequestRestatement("\u8fd0\u884c\u5e76\u52a0\u4e0a\u65e5\u5fd7", stickyCodingWorkbenchMemory{})
	if strings.Contains(got, "\u4e0d\u6539\u6e90\u4ee3\u7801") {
		t.Fatalf("edit request restated as run-only: %q", got)
	}
}

func TestFallbackUsesCMakeProjectIdentity(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CMakeLists.txt"), []byte("cmake_minimum_required(VERSION 3.16)\nproject(snake CXX)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "util.cpp"), []byte("int util(){return 0;}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "board.cpp"), []byte("int board(){return 0;}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := fallbackCodingRequestRestatement("\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248", stickyCodingWorkbenchMemory{ProjectPath: dir})
	if !strings.Contains(strings.ToLower(got), "snake") {
		t.Fatalf("cmake project identity missing: %q", got)
	}
}

func TestFallbackUsesNestedSourceAndBinaryIdentity(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(srcDir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "src", "snake.cpp"), []byte("int main(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := fallbackCodingRequestRestatement("\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248", stickyCodingWorkbenchMemory{ProjectPath: srcDir})
	if !strings.Contains(strings.ToLower(got), "snake") {
		t.Fatalf("src/ identity missing: %q", got)
	}

	binDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(binDir, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "build", "snake.exe"), []byte("mz"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = fallbackCodingRequestRestatement("\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248", stickyCodingWorkbenchMemory{ProjectPath: binDir})
	if !strings.Contains(strings.ToLower(got), "snake") {
		t.Fatalf("build exe identity missing: %q", got)
	}
}

func TestFallbackFindsSourceAfterManyDirs(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 12; i++ {
		if err := os.Mkdir(filepath.Join(dir, "dir"+string(rune('a'+i))), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "snake.cpp"), []byte("int main(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := fallbackCodingRequestRestatement("\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248", stickyCodingWorkbenchMemory{ProjectPath: dir})
	if !strings.Contains(strings.ToLower(got), "snake") {
		t.Fatalf("source after dirs missing: %q", got)
	}
}

func TestFallbackFindsExeAfterManyBuildFiles(t *testing.T) {
	dir := t.TempDir()
	build := filepath.Join(dir, "build")
	if err := os.Mkdir(build, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		if err := os.WriteFile(filepath.Join(build, "obj"+string(rune('A'+i%26))+string(rune('0'+i/26))+".obj"), []byte("o"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(build, "snake.exe"), []byte("mz"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := fallbackCodingRequestRestatement("\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248", stickyCodingWorkbenchMemory{ProjectPath: dir})
	if !strings.Contains(strings.ToLower(got), "snake") {
		t.Fatalf("exe after junk build files missing: %q", got)
	}
}

func TestCodingRequestLooksRunOnlyRejectsQuestions(t *testing.T) {
	if codingRequestLooksRunOnly("\u600e\u4e48\u8fd0\u884c") {
		t.Fatal("how-to-run questions must not look run-only")
	}
	got := fallbackCodingRequestRestatement("\u600e\u4e48\u8fd0\u884c", stickyCodingWorkbenchMemory{})
	if strings.Contains(got, "\u4e0d\u6539\u6e90\u4ee3\u7801") {
		t.Fatalf("question restated as a run: %q", got)
	}
	if !strings.Contains(got, "\u4e0d\u6539\u6587\u4ef6") {
		t.Fatalf("question should stay inquiry: %q", got)
	}
}

func TestAttachCodingWorkRootOverridesStalePath(t *testing.T) {
	mem := stickyCodingWorkbenchMemory{ProjectPath: "D:/old-workspace"}
	attachCodingWorkRoot(&mem, "D:/live-workspace")
	if mem.ProjectPath != "D:/live-workspace" {
		t.Fatalf("work root=%q", mem.ProjectPath)
	}
}

func TestFallbackKeepsEnglishHowToAsInquiry(t *testing.T) {
	if codingRequestLooksRunOnly("how to run this") {
		t.Fatal("how-to questions must not look run-only")
	}
	got := fallbackCodingRequestRestatement("how to run this", stickyCodingWorkbenchMemory{})
	if strings.Contains(got, "\u4e0d\u6539\u6e90\u4ee3\u7801") {
		t.Fatalf("english how-to restated as a run: %q", got)
	}
	if !strings.Contains(got, "\u4e0d\u6539\u6587\u4ef6") {
		t.Fatalf("english how-to should stay inquiry: %q", got)
	}
}

func TestFallbackDoesNotTreatRunFailureAsRunOnly(t *testing.T) {
	if codingRequestLooksRunOnly("\u8fd0\u884c\u5931\u8d25\u4e86") {
		t.Fatal("a failed run must not look run-only")
	}
	got := fallbackCodingRequestRestatement("\u8fd0\u884c\u5931\u8d25\u4e86", stickyCodingWorkbenchMemory{})
	if strings.Contains(got, "\u4e0d\u6539\u6e90\u4ee3\u7801") {
		t.Fatalf("failed run restated as a no-edit launch: %q", got)
	}
}

func TestCodingRequestLooksLikeFailureWordBoundary(t *testing.T) {
	if codingRequestLooksLikeFailure("run the debug build") {
		t.Fatal("debug must not count as a failure restatement")
	}
	if !codingRequestLooksLikeFailure("crash on start") {
		t.Fatal("crash should count as a failure")
	}
}

func TestFallbackRewriteUsesRewriteWordBoundary(t *testing.T) {
	got, ok := fallbackRewriteRestatement("please review the rewriter comments", "")
	if ok {
		t.Fatalf("rewriter must not count as rewrite: %q", got)
	}
	if _, ok := fallbackRewriteRestatement("rewrite the parser", ""); !ok {
		t.Fatal("rewrite as a word must still match")
	}
}

func TestFallbackKeepsHowToGUIAsInquiry(t *testing.T) {
	got := fallbackCodingRequestRestatement("\u600e\u4e48\u6539\u6210\u56fe\u5f62\u754c\u9762", stickyCodingWorkbenchMemory{
		SessionPlan: "\u63a7\u5236\u53f0\u8d2a\u5403\u86c7",
	})
	if strings.Contains(got, "\u6539\u6210\u56fe\u5f62\u754c\u9762\u7248\u672c") && !strings.Contains(got, "\u4e0d\u6539\u6587\u4ef6") {
		t.Fatalf("how-to GUI ask restated as an implementation rewrite: %q", got)
	}
	if !strings.Contains(got, "\u4e0d\u6539\u6587\u4ef6") {
		t.Fatalf("how-to GUI ask should stay inquiry: %q", got)
	}
	if codingRestatementFallbackIsSpecific("\u600e\u4e48\u6539\u6210\u56fe\u5f62\u754c\u9762", stickyCodingWorkbenchMemory{}) {
		t.Fatal("how-to GUI ask must not take the host rewrite plan")
	}
	got = fallbackCodingRequestRestatement("\u6539\u4e3a\u56fe\u5f62\u754c\u9762\u7248\uff1f", stickyCodingWorkbenchMemory{})
	if !strings.Contains(got, "\u56fe\u5f62\u754c\u9762") || strings.Contains(got, "\u4e0d\u6539\u6587\u4ef6") {
		t.Fatalf("imperative GUI ask with a question mark should still rewrite: %q", got)
	}
}
