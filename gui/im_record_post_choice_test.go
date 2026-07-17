package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
)

func TestParseRecordPostChoiceCommand(t *testing.T) {
	action, ok := parseRecordPostChoiceCommand("__record_post__ minutes")
	if !ok || action != recordPostActionMinutes {
		t.Fatalf("minutes: got (%q,%v)", action, ok)
	}
	action, ok = parseRecordPostChoiceCommand("  __record_post__ transcribe  ")
	if !ok || action != recordPostActionTranscribe {
		t.Fatalf("transcribe: got (%q,%v)", action, ok)
	}
	action, ok = parseRecordPostChoiceCommand("__record_post__ keep_only")
	if !ok || action != recordPostActionKeepOnly {
		t.Fatalf("keep_only: got (%q,%v)", action, ok)
	}
	if _, ok := parseRecordPostChoiceCommand("__record_post__ other"); ok {
		t.Fatal("unknown action should fail")
	}
	if _, ok := parseRecordPostChoiceCommand("转写并生成会议纪要"); ok {
		t.Fatal("plain label is not a command")
	}
}

func TestMatchRecordPostChoiceFreeText(t *testing.T) {
	cases := []struct {
		in   string
		want recordPostChoiceAction
	}{
		{"1", recordPostActionMinutes},
		{"转写并生成会议纪要", recordPostActionMinutes},
		{"Transcribe + meeting minutes", recordPostActionMinutes},
		{"2", recordPostActionTranscribe},
		{"仅转写文字", recordPostActionTranscribe},
		{"Transcribe only", recordPostActionTranscribe},
		{"3", recordPostActionKeepOnly},
		{"不做处理", recordPostActionKeepOnly},
		{"Keep audio only", recordPostActionKeepOnly},
		{"__record_post__ minutes", recordPostActionMinutes},
	}
	for _, tc := range cases {
		got, ok := matchRecordPostChoiceFreeText(tc.in)
		if !ok || got != tc.want {
			t.Fatalf("%q: got (%q,%v), want %q", tc.in, got, ok, tc.want)
		}
	}
	if _, ok := matchRecordPostChoiceFreeText("今天天气怎么样"); ok {
		t.Fatal("casual chat must not match")
	}
	if _, ok := matchRecordPostChoiceFreeText("what's the weather"); ok {
		t.Fatal("casual english must not match")
	}
	// Overly broad tokens must not steal unrelated replies.
	for _, bad := range []string{"save", "skip", "keep", "保存", "text only"} {
		if action, ok := matchRecordPostChoiceFreeText(bad); ok {
			t.Fatalf("%q must not match, got %q", bad, action)
		}
	}
}

func TestSuggestMP3ArchivePath(t *testing.T) {
	cases := map[string]string{
		`C:\tmp\a.wav`:  `C:\tmp\a.mp3`,
		`C:\tmp\a.WAV`:  `C:\tmp\a.mp3`,
		`/tmp/a.wav`:    `/tmp/a.mp3`,
		`/tmp/a.mp3`:    `/tmp/a.mp3`,
		`/tmp/meeting`:  `/tmp/meeting.mp3`,
		``:              ``,
	}
	for in, want := range cases {
		if got := suggestMP3ArchivePath(in); got != want {
			t.Fatalf("suggestMP3ArchivePath(%q)=%q want %q", in, got, want)
		}
	}
}

func TestPendingPostRecordingSurvivesInterveningChat(t *testing.T) {
	pending := &pendingPostRecordingState{
		Title:     "会",
		Path:      `C:\tmp\a.wav`,
		CreatedAt: time.Now(),
	}
	// Extended history with a later user turn would break strict prefix binding.
	extended := []agent.ConversationEntry{
		{Role: "user", Content: "report"},
		{Role: "assistant", Content: "choice"},
		{Role: "user", Content: "顺便问下时间"},
		{Role: "assistant", Content: "现在三点"},
	}
	got, ok := pendingPostRecordingForCurrentHistory(pending, extended)
	if !ok || got == nil || got.Path != `C:\tmp\a.wav` {
		t.Fatalf("pending should survive intervening chat, ok=%v got=%v", ok, got)
	}
}

func TestSoftReplyDoesNotRefreshPostRecordingTTL(t *testing.T) {
	// Soft chat must not pin choices forever — CreatedAt stays fixed at offer time.
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	h := &IMMessageHandler{memory: mem}
	anchor := time.Now().Add(-30 * time.Second)
	h.pendingPostRecording.Store("u1", &pendingPostRecordingState{
		Title:     "会",
		Path:      `C:\tmp\a.wav`,
		CreatedAt: anchor,
	})
	_, hostResp, deferred, ok := h.consumePendingPostRecordingChoice("u1", "现在几点", nil)
	if !ok {
		t.Fatal("expected soft consume")
	}
	if hostResp != nil || deferred != nil {
		t.Fatal("soft chat must not host-handle")
	}
	raw, _ := h.pendingPostRecording.Load("u1")
	pending := raw.(*pendingPostRecordingState)
	if !pending.CreatedAt.Equal(anchor) {
		t.Fatalf("soft reply must not refresh CreatedAt: got %v want %v", pending.CreatedAt, anchor)
	}
}

func TestIsSuccessfulRecordingForChoice(t *testing.T) {
	if !isSuccessfulRecordingForChoice("[Recording completed]\nstatus: stopped\npath: C:\\a.wav\n") {
		t.Fatal("stopped+path should succeed")
	}
	if isSuccessfulRecordingForChoice("[Recording completed]\nstatus: cancelled\n") {
		t.Fatal("cancelled without path must not offer choice")
	}
	if isSuccessfulRecordingForChoice("[Recording completed]\nstatus: stopped\n") {
		t.Fatal("stopped without path must not offer choice")
	}
	if isSuccessfulRecordingForChoice("[Recording completed]\nstatus: error\npath: C:\\a.wav\nerror: boom\n") {
		t.Fatal("error status must not offer choice even with path")
	}
}

func TestFormatPostRecordingChoiceTextLocalized(t *testing.T) {
	report := "[Recording completed]\nstatus: stopped\ntitle: 周会\npath: C:\\tmp\\a.wav\nmp3_path: C:\\tmp\\a.mp3\nduration_sec: 10\nsize_bytes: 337000\nformat: wav\n"

	zh := formatPostRecordingChoiceText("周会", report, "zh-Hans")
	if !strings.Contains(zh, "这次录音成功") {
		t.Fatalf("zh missing success banner: %s", zh)
	}
	if !strings.Contains(zh, "录音摘要") {
		t.Fatalf("zh missing summary: %s", zh)
	}
	if !strings.Contains(zh, "请选择后续处理") {
		t.Fatalf("zh missing prompt: %s", zh)
	}
	if !strings.Contains(zh, "MP3") || !strings.Contains(zh, `C:\tmp\a.mp3`) {
		t.Fatalf("zh missing mp3 path: %s", zh)
	}

	hant := formatPostRecordingChoiceText("週會", report, "zh-Hant")
	if !strings.Contains(hant, "這次錄音成功") {
		t.Fatalf("zh-Hant missing success banner: %s", hant)
	}
	if !strings.Contains(hant, "錄音摘要") {
		t.Fatalf("zh-Hant missing summary: %s", hant)
	}
	if !strings.Contains(hant, "請選擇後續處理") {
		t.Fatalf("zh-Hant missing prompt: %s", hant)
	}
	if !strings.Contains(hant, "MP3") || !strings.Contains(hant, `C:\tmp\a.mp3`) {
		t.Fatalf("zh-Hant missing mp3 path: %s", hant)
	}

	en := formatPostRecordingChoiceText("Weekly", report, "en")
	if !strings.Contains(en, "Recording saved successfully") {
		t.Fatalf("en missing success banner: %s", en)
	}
	if !strings.Contains(en, "Recording summary") {
		t.Fatalf("en missing summary: %s", en)
	}
	if !strings.Contains(en, "Choose what to do next") {
		t.Fatalf("en missing prompt: %s", en)
	}
	if !strings.Contains(en, "0:10") {
		t.Fatalf("en missing duration: %s", en)
	}
	if !strings.Contains(en, `C:\tmp\a.wav`) {
		t.Fatalf("en missing path: %s", en)
	}
	if !strings.Contains(en, "MP3 archive") || !strings.Contains(en, `C:\tmp\a.mp3`) {
		t.Fatalf("en missing mp3 path: %s", en)
	}
}

func TestPostRecordingChoiceActionsLocalized(t *testing.T) {
	zh := postRecordingChoiceActions("zh-Hans")
	if len(zh) != 3 {
		t.Fatalf("zh actions = %d", len(zh))
	}
	if zh[0].Label != i18n.T(i18n.MsgRecordPostBtnMinutes, "zh") {
		t.Fatalf("zh minutes label = %q", zh[0].Label)
	}
	hant := postRecordingChoiceActions("zh-Hant")
	if hant[0].Label != "轉寫並生成會議紀要" {
		t.Fatalf("zh-Hant minutes label = %q", hant[0].Label)
	}
	if hant[1].Label != "僅轉寫文字" {
		t.Fatalf("zh-Hant transcribe label = %q", hant[1].Label)
	}
	en := postRecordingChoiceActions("en")
	if en[0].Label != i18n.T(i18n.MsgRecordPostBtnMinutes, "en") {
		t.Fatalf("en minutes label = %q", en[0].Label)
	}
	if en[1].Label != "Transcribe only" {
		t.Fatalf("en transcribe label = %q", en[1].Label)
	}
	if en[2].Label != "Keep audio only" {
		t.Fatalf("en keep label = %q", en[2].Label)
	}
}

func TestMatchRecordPostChoiceMinutesVariants(t *testing.T) {
	for _, in := range []string{"帮我出纪要", "生成會議紀要", "please meeting minutes"} {
		got, ok := matchRecordPostChoiceFreeText(in)
		if !ok || got != recordPostActionMinutes {
			t.Fatalf("%q: got (%q,%v), want minutes", in, got, ok)
		}
	}
	// "不要纪要" must not force minutes; "只转写" should win as transcribe.
	if action, ok := matchRecordPostChoiceFreeText("不要纪要，只转写"); !ok || action != recordPostActionTranscribe {
		t.Fatalf("got (%q,%v), want transcribe", action, ok)
	}
}

func TestBuildPostRecordingChoiceContextRequiresMdPdfMp3(t *testing.T) {
	pending := &pendingPostRecordingState{
		Title:   "周会",
		Path:    `C:\tmp\a.wav`,
		MP3Path: `C:\tmp\a.mp3`,
		Lang:    "zh",
	}
	ctx := buildPostRecordingChoiceContext(pending, recordPostActionMinutes)
	for _, needle := range []string{
		"Markdown",
		"write_file",
		"generate_pdf",
		"PDF",
		"MP3",
		"transcript",
		"完整转写",
		"map-reduce",
		"transcript_file",
		`C:\tmp\a.wav`,
		`C:\tmp\a.mp3`,
		`asr(path="C:\\tmp\\a.wav", for_minutes=true)`,
		"for_minutes=true",
		"do NOT re-encode",
	} {
		if !strings.Contains(ctx, needle) {
			t.Fatalf("minutes context missing %q:\n%s", needle, ctx)
		}
	}

	tx := buildPostRecordingChoiceContext(pending, recordPostActionTranscribe)
	for _, needle := range []string{
		"MP3",
		`C:\tmp\a.mp3`,
		"transcript_file",
		"write_file",
		"generate_pdf",
		"Markdown",
		"PDF",
		"_transcript.md",
		"do not pass for_minutes",
		"DESKTOP EXCEPTION",
		"a_transcript.md",
		"transcript_pdf",
		"a_transcript.pdf",
	} {
		if !strings.Contains(tx, needle) {
			t.Fatalf("transcribe context missing %q:\n%s", needle, tx)
		}
	}
	// Transcribe must save md+pdf of the transcript, but not full meeting minutes.
	if strings.Contains(tx, "meeting minutes in BOTH formats") ||
		strings.Contains(tx, "decisions/action items") ||
		strings.Contains(tx, "for_minutes=true") {
		t.Fatalf("transcribe must not require full minutes structure:\n%s", tx)
	}
	// Minutes path also needs the desktop exception so workflow-doc override does not win.
	if !strings.Contains(ctx, "DESKTOP EXCEPTION") {
		t.Fatalf("minutes context missing desktop exception:\n%s", ctx)
	}

	keep := buildPostRecordingChoiceContext(pending, recordPostActionKeepOnly)
	if !strings.Contains(keep, "MP3") || strings.Contains(keep, "Call asr") {
		t.Fatalf("keep_only should deliver mp3 and not asr:\n%s", keep)
	}

	// Without a known mp3_path, must NOT claim a pre-built product exists.
	noMP3 := &pendingPostRecordingState{
		Title: "周会",
		Path:  `C:\tmp\b.wav`,
		Lang:  "zh",
	}
	ctxNo := buildPostRecordingChoiceContext(noMP3, recordPostActionMinutes)
	if strings.Contains(ctxNo, "do NOT re-encode") || strings.Contains(ctxNo, "Pre-built MP3 already exists") {
		t.Fatalf("must not claim pre-built mp3 when missing:\n%s", ctxNo)
	}
	if !strings.Contains(ctxNo, "Suggested MP3") && !strings.Contains(ctxNo, "ffmpeg") {
		t.Fatalf("without pre-built mp3 should suggest convert path:\n%s", ctxNo)
	}
	// When save-time archive failed, surface mp3_error so the agent converts.
	failedMP3 := &pendingPostRecordingState{
		Title:  "周会",
		Path:   `C:\tmp\d.wav`,
		Report: "mp3_error: wav pcm too large\n",
		Lang:   "zh",
	}
	ctxFail := buildPostRecordingChoiceContext(failedMP3, recordPostActionKeepOnly)
	if !strings.Contains(ctxFail, "mp3_error") && !strings.Contains(ctxFail, "auto-archive failed") {
		t.Fatalf("expected mp3_error guidance:\n%s", ctxFail)
	}
	if strings.Contains(ctxFail, "Pre-built MP3 already exists") {
		t.Fatalf("must not claim pre-built after archive failure:\n%s", ctxFail)
	}
	// Report-only mp3_path (pending.MP3Path empty) still counts as known.
	fromReport := &pendingPostRecordingState{
		Title:  "周会",
		Path:   `C:\tmp\c.wav`,
		Report: "mp3_path: C:\\tmp\\c.mp3\n",
		Lang:   "zh",
	}
	if got := resolveKnownMP3ArchivePath(fromReport); got != `C:\tmp\c.mp3` {
		t.Fatalf("resolveKnown from report = %q", got)
	}
	if got := resolveKnownMP3ArchivePath(noMP3); got != "" {
		t.Fatalf("resolveKnown empty = %q", got)
	}
}

func TestOfferAndConsumePostRecordingChoice(t *testing.T) {
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	h := &IMMessageHandler{memory: mem}
	prior := []agent.ConversationEntry{
		{Role: "user", Content: "帮我录音"},
		{Role: "assistant", Content: "正在打开录音"},
	}
	report := "[Recording completed]\nstatus: stopped\ntitle: 讨论\npath: C:\\tmp\\a.wav\nduration_sec: 12\nsize_bytes: 1000\nformat: wav\n"
	resp := h.offerPostRecordingChoice("u1", "讨论", "现场录制", report, "zh", prior)
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.ResponseSource != imResponseSourceAskUser.String() {
		t.Fatalf("ResponseSource = %q, want ask_user", resp.ResponseSource)
	}
	if len(resp.Actions) != 3 {
		t.Fatalf("actions = %d, want 3", len(resp.Actions))
	}
	if resp.Actions[0].Command != "__record_post__ minutes" {
		t.Fatalf("first command = %q", resp.Actions[0].Command)
	}
	if resp.Actions[0].Label != "转写并生成会议纪要" {
		t.Fatalf("zh label = %q", resp.Actions[0].Label)
	}
	if !h.hasActivePendingPostRecording("u1", h.memory.Load("u1")) {
		t.Fatal("pending should be active after offer")
	}

	// Casual reply keeps pending and injects soft hint.
	soft, hostSoft, deferredSoft, ok := h.consumePendingPostRecordingChoice("u1", "顺便问下时间", h.memory.Load("u1"))
	if !ok || soft == "" {
		t.Fatalf("expected soft context, ok=%v ctx=%q", ok, soft)
	}
	if hostSoft != nil || deferredSoft != nil {
		t.Fatal("soft chat must not host-handle")
	}
	if !h.hasActivePendingPostRecording("u1", h.memory.Load("u1")) {
		t.Fatal("pending should remain after casual reply")
	}

	// Button click resolves.
	ctx, hostResp, deferred, ok := h.consumePendingPostRecordingChoice("u1", "__record_post__ minutes", h.memory.Load("u1"))
	if !ok {
		t.Fatal("expected choice consume success")
	}
	if hostResp != nil || deferred != nil {
		t.Fatal("minutes path should not host-handle (needs LLM)")
	}
	if !strings.Contains(ctx, "minutes") || !strings.Contains(ctx, `C:\tmp\a.wav`) {
		t.Fatalf("context = %q", ctx)
	}
	if !strings.Contains(ctx, "generate_pdf") || !strings.Contains(ctx, "MP3") {
		t.Fatalf("minutes context should require pdf+mp3: %q", ctx)
	}
	if !strings.Contains(ctx, "transcript") && !strings.Contains(ctx, "完整转写") {
		t.Fatalf("minutes context should require full transcript: %q", ctx)
	}
	if h.hasActivePendingPostRecording("u1", h.memory.Load("u1")) {
		t.Fatal("pending should clear after choice")
	}
}

func TestBuildPostRecordingTranscribeHostResponse(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "a_transcript.md")
	pdf := filepath.Join(dir, "a_transcript.pdf")
	if err := os.WriteFile(md, []byte("# t\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pdf, []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	pending := &pendingPostRecordingState{Title: "会", Lang: "zh-Hans"}
	resp := buildPostRecordingTranscribeHostResponse(pending, "你好世界", []string{md, pdf}, false)
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.ResponseSource != imResponseSourceFileDelivery.String() {
		t.Fatalf("source=%q", resp.ResponseSource)
	}
	if !strings.Contains(resp.Text, "你好世界") || !strings.Contains(resp.Text, "转写完成") {
		t.Fatalf("text=%q", resp.Text)
	}
	if len(resp.LocalFilePaths) != 2 {
		t.Fatalf("paths=%v", resp.LocalFilePaths)
	}
	// Long transcript uses preview + omission note.
	long := strings.Repeat("会议内容段落。", 800)
	respLong := buildPostRecordingTranscribeHostResponse(pending, long, []string{md}, true)
	if respLong == nil || !strings.Contains(respLong.Text, "省略") {
		t.Fatalf("long text=%v", respLong)
	}
}

func TestCollectExistingPaths(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.md")
	if err := os.WriteFile(a, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := collectExistingPaths(a, a, filepath.Join(dir, "missing.pdf"), "  ")
	if len(got) != 1 || got[0] != a {
		t.Fatalf("got=%v", got)
	}
}

func TestShouldHostHandlePostRecordingTranscribe(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small.wav")
	if err := os.WriteFile(small, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !shouldHostHandlePostRecordingTranscribe(&pendingPostRecordingState{
		Path: small, DurationSec: "12",
	}) {
		t.Fatal("12s should host-handle")
	}
	if !shouldHostHandlePostRecordingTranscribe(&pendingPostRecordingState{
		Path: small, DurationSec: "300",
	}) {
		t.Fatal("exactly 300s should still host-handle")
	}
	if shouldHostHandlePostRecordingTranscribe(&pendingPostRecordingState{
		Path: small, DurationSec: "301",
	}) {
		t.Fatal(">5min should not host-handle (no progress UI)")
	}
	if shouldHostHandlePostRecordingTranscribe(&pendingPostRecordingState{
		Path: small, DurationSec: "900",
	}) {
		t.Fatal("15min should not host-handle")
	}
	// Unknown duration: only small on-disk files may host-handle.
	if !shouldHostHandlePostRecordingTranscribe(&pendingPostRecordingState{
		Path: small, DurationSec: "",
	}) {
		t.Fatal("unknown duration + small file should host-handle")
	}
	if shouldHostHandlePostRecordingTranscribe(&pendingPostRecordingState{
		Path: filepath.Join(dir, "missing.wav"), DurationSec: "",
	}) {
		t.Fatal("unknown duration + missing file must not host-handle")
	}
	if shouldHostHandlePostRecordingTranscribe(&pendingPostRecordingState{
		Path: "", DurationSec: "10",
	}) {
		t.Fatal("empty path must not host-handle")
	}
}

func TestBuildPostRecordingKeepOnlyHostResponse(t *testing.T) {
	dir := t.TempDir()
	mp3 := filepath.Join(dir, "a.mp3")
	if err := os.WriteFile(mp3, []byte("ID3"), 0o644); err != nil {
		t.Fatal(err)
	}
	pending := &pendingPostRecordingState{Title: "周会", DurationSec: "65", Lang: "zh"}
	resp := buildPostRecordingKeepOnlyHostResponse(pending, []string{mp3})
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.ResponseSource != imResponseSourceFileDelivery.String() {
		t.Fatalf("source=%q", resp.ResponseSource)
	}
	if !strings.Contains(resp.Text, "录音已保留") || !strings.Contains(resp.Text, "1:05") {
		t.Fatalf("text=%q", resp.Text)
	}
	if len(resp.LocalFilePaths) != 1 {
		t.Fatalf("paths=%v", resp.LocalFilePaths)
	}
	if buildPostRecordingKeepOnlyHostResponse(pending, nil) != nil {
		t.Fatal("empty paths must return nil")
	}
}

func TestHostHandlePostRecordingKeepOnlyWithMP3(t *testing.T) {
	dir := t.TempDir()
	mp3 := filepath.Join(dir, "meet.mp3")
	if err := os.WriteFile(mp3, []byte("ID3"), 0o644); err != nil {
		t.Fatal(err)
	}
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	h := &IMMessageHandler{memory: mem}
	pending := &pendingPostRecordingState{
		Title:   "会",
		Path:    filepath.Join(dir, "meet.wav"),
		MP3Path: mp3,
		Lang:    "zh",
	}
	resp := h.hostHandlePostRecordingKeepOnly("u1", pending, nil)
	if resp == nil {
		t.Fatal("expected host keep_only response")
	}
	if resp.SessionKey != "u1" {
		t.Fatalf("session=%q", resp.SessionKey)
	}
	if len(resp.LocalFilePaths) != 1 || resp.LocalFilePaths[0] != mp3 {
		t.Fatalf("paths=%v", resp.LocalFilePaths)
	}
	// Without on-disk mp3/wav, host must fall back (nil).
	if h.hostHandlePostRecordingKeepOnly("u1", &pendingPostRecordingState{
		Path: filepath.Join(dir, "missing.wav"), Lang: "zh",
	}, nil) != nil {
		t.Fatal("missing audio should not host-handle")
	}
	// WAV-only (no mp3) should still host-deliver the source file.
	wavOnly := filepath.Join(dir, "only.wav")
	if err := os.WriteFile(wavOnly, []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}
	wavResp := h.hostHandlePostRecordingKeepOnly("u1", &pendingPostRecordingState{
		Path: wavOnly, Lang: "zh",
	}, nil)
	if wavResp == nil || len(wavResp.LocalFilePaths) != 1 || wavResp.LocalFilePaths[0] != wavOnly {
		t.Fatalf("wav-only keep_only = %+v", wavResp)
	}
}

func TestDeferHostPostRecordingTranscribeEligibility(t *testing.T) {
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	h := &IMMessageHandler{memory: mem} // app nil → not eligible
	pending := &pendingPostRecordingState{
		Path:        `C:\tmp\a.wav`,
		DurationSec: "10",
		Lang:        "zh",
		CreatedAt:   time.Now(),
	}
	if h.deferHostPostRecordingTranscribe("u1", pending, nil) != nil {
		t.Fatal("nil app must not defer host transcribe")
	}
	// Long audio must not defer (agent path).
	if h.deferHostPostRecordingTranscribe("u1", &pendingPostRecordingState{
		Path: `C:\tmp\a.wav`, DurationSec: "300", CreatedAt: time.Now(),
	}, nil) != nil {
		t.Fatal("long audio must not defer host transcribe")
	}
}

func TestHostPostRecordingTranscribeFailureResponse(t *testing.T) {
	resp := hostPostRecordingTranscribeFailureResponse("u1", &pendingPostRecordingState{
		Path: `C:\tmp\a.wav`, Lang: "zh",
	})
	if resp == nil || resp.SessionKey != "u1" {
		t.Fatalf("resp=%+v", resp)
	}
	if !strings.Contains(resp.Text, "转写失败") || !strings.Contains(resp.Text, `C:\tmp\a.wav`) {
		t.Fatalf("text=%q", resp.Text)
	}
}

func TestLastUserContentEquals(t *testing.T) {
	if lastUserContentEquals(nil, "x") {
		t.Fatal("empty history")
	}
	hist := []agent.ConversationEntry{{Role: "user", Content: "仅转写文字"}}
	if !lastUserContentEquals(hist, "仅转写文字") {
		t.Fatal("should match last user")
	}
	if lastUserContentEquals(hist, "其他") {
		t.Fatal("should not match different label")
	}
	hist = append(hist, agent.ConversationEntry{Role: "assistant", Content: "ok"})
	if lastUserContentEquals(hist, "仅转写文字") {
		t.Fatal("last is assistant")
	}
}

func TestFinalizeIMEntryHostResponse(t *testing.T) {
	resp := &IMAgentResponse{Text: "hi"}
	got := finalizeIMEntryHostResponse(resp, "req-1", "user-1")
	if got.RequestID != "req-1" || got.SessionKey != "user-1" {
		t.Fatalf("got=%+v", got)
	}
	// Do not overwrite existing fields.
	got2 := finalizeIMEntryHostResponse(&IMAgentResponse{
		Text: "x", RequestID: "keep", SessionKey: "keep-u",
	}, "req-2", "user-2")
	if got2.RequestID != "keep" || got2.SessionKey != "keep-u" {
		t.Fatalf("overwrite=%+v", got2)
	}
	if finalizeIMEntryHostResponse(nil, "r", "u") != nil {
		t.Fatal("nil in → nil out")
	}
}

func TestOfferPostRecordingChoiceEnglish(t *testing.T) {
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	h := &IMMessageHandler{memory: mem}
	report := "[Recording completed]\nstatus: stopped\ntitle: Standup\npath: /tmp/a.wav\nduration_sec: 5\nsize_bytes: 1000\nformat: wav\n"
	resp := h.offerPostRecordingChoice("u2", "Standup", "", report, "en", nil)
	if resp == nil {
		t.Fatal("expected response")
	}
	if !strings.Contains(resp.Text, "Recording saved successfully") {
		t.Fatalf("text = %s", resp.Text)
	}
	if resp.Actions[0].Label != "Transcribe + meeting minutes" {
		t.Fatalf("label = %q", resp.Actions[0].Label)
	}
}

func TestEntryContextShortCircuitsSuccessfulRecordingCompletion(t *testing.T) {
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	h := &IMMessageHandler{memory: mem}
	history := []agent.ConversationEntry{
		{Role: "user", Content: "会议录音"},
		{Role: "tool", Content: "Opened recording session: 会议", ToolCallID: "tc-1"},
	}
	h.memory.Save("u1", history)
	h.pendingRecordAudio.Store("u1", &pendingRecordAudioState{
		Title:     "会议",
		Purpose:   "现场录制会议/讨论",
		History:   cloneConversationEntries(history),
		Timestamp: time.Now(),
	})

	report := "[Recording completed]\nstatus: stopped\ntitle: 会议\npath: C:\\tmp\\meet.wav\nduration_sec: 10\nsize_bytes: 337000\nformat: wav\n"
	trimmed := report
	msg := IMUserMessage{UserID: "u1", Text: report, Platform: "desktop", Lang: "zh"}
	result := h.resolveIMEntryContext(imEntryContextOptions{
		Message:            &msg,
		Trimmed:            &trimmed,
		EntriesBeforeClear: history,
	})
	if !result.Handled || result.Response == nil {
		t.Fatalf("expected short-circuit response, handled=%v resp=%v", result.Handled, result.Response)
	}
	if len(result.Response.Actions) != 3 {
		t.Fatalf("actions = %d", len(result.Response.Actions))
	}
	if result.Response.Actions[0].Label != "转写并生成会议纪要" {
		t.Fatalf("label = %q", result.Response.Actions[0].Label)
	}
	if _, still := h.pendingRecordAudio.Load("u1"); still {
		t.Fatal("pendingRecordAudio should be cleared after completion")
	}
	if !h.hasActivePendingPostRecording("u1", h.memory.Load("u1")) {
		t.Fatal("pendingPostRecording should be set")
	}
}
