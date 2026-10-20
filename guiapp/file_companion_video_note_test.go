package guiapp

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/audioconv"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestFileCompanionVideoNotePhraseDoesNotWriteOnOrdinaryQuestion(t *testing.T) {
	app := newVideoNoteTestApp(t)
	video := writeMedia(t, app, "lecture.mp4", []byte("not-a-real-video"))
	audio := writeMedia(t, app, "talk.wav", tinyPCMWav(1))
	pdf := writeMedia(t, app, "paper.pdf", []byte("%PDF-1.7 fake"))
	media := &fakeVideoNoteMedia{}
	restore := useVideoNoteMedia(t, media)
	defer restore()
	var ordinary int
	prev := fileCompanionTurnModelHook
	fileCompanionTurnModelHook = func(msg IMUserMessage) *IMAgentResponse {
		ordinary++
		return &IMAgentResponse{Text: "好的"}
	}
	t.Cleanup(func() { fileCompanionTurnModelHook = prev })

	before := mediaModTime(t, video)
	if err := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: video, Text: "不要做笔记"}); err != nil {
		t.Fatal(err)
	}
	if media.installs != 0 {
		t.Fatalf("ordinary question installed a decoder: %d", media.installs)
	}
	if noteFiles(t, app, video) != "" {
		t.Fatal("ordinary question wrote a note")
	}
	if got := mediaModTime(t, video); !got.Equal(before) {
		t.Fatal("media mtime changed")
	}

	if err := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: pdf, Text: "音频笔记"}); err != nil {
		t.Fatal(err)
	}
	if noteFiles(t, app, pdf) != "" {
		t.Fatal("audio phrase wrote a note for a pdf")
	}
	if media.installs != 0 {
		t.Fatal("pdf phrase installed a decoder")
	}

	setNoteModel(t, "## 概要\n讲过了\n## 章节\n00:01 开始\n## 要点\n00:01 一点\n## 金句\n00:01 「原话」\n## 不确定的地方\n无\n")
	writeSidecar(t, audio, "1\n00:00:00,000 --> 00:00:01,000\n你好世界\n")
	if err := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: audio, Text: "生成笔记", RequestID: "audio-note"}); err != nil {
		t.Fatal(err)
	}
	if media.installs != 0 {
		t.Fatalf("sidecar audio installed a decoder: %d", media.installs)
	}
	md := noteFiles(t, app, audio)
	if md == "" || !strings.HasSuffix(md, "talk-笔记.md") {
		t.Fatalf("audio note = %q", md)
	}
	if ordinary == 0 {
		t.Fatal("ordinary questions did not reach the model")
	}
}

func TestFileCompanionVideoNoteSidecarWithoutFFmpeg(t *testing.T) {
	app := newVideoNoteTestApp(t)
	video := writeMedia(t, app, "board.mp4", []byte("video-bytes"))
	writeSidecar(t, video, "1\n00:00:00,000 --> 00:00:08,000\n第一句字幕\n\n2\n00:00:08,000 --> 00:00:10,000\n第二句字幕\n")
	media := &fakeVideoNoteMedia{installErr: os.ErrNotExist}
	restore := useVideoNoteMedia(t, media)
	defer restore()
	prevFetch := fileCompanionFFmpegFetchHook
	fileCompanionFFmpegFetchHook = func(context.Context, string) ([]byte, error) {
		t.Fatal("sidecar note downloaded ffmpeg")
		return nil, os.ErrNotExist
	}
	t.Cleanup(func() { fileCompanionFFmpegFetchHook = prevFetch })
	setNoteModel(t, "## 概要\n字幕内容\n## 章节\n00:01 开始\n## 要点\n00:01 一点\n## 金句\n00:01 「字幕」\n## 不确定的地方\n无\n")
	before := mediaModTime(t, video)
	if err := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: video, Text: "生成笔记", Purpose: "video-note", RequestID: "side-1"}); err != nil {
		t.Fatal(err)
	}
	if media.installs == 0 {
		t.Fatal("video sidecar did not try to install a decoder for frames")
	}
	body := readNote(t, app, video)
	if !strings.Contains(body, "画面未识别") {
		t.Fatalf("note = %s", body)
	}
	if !strings.Contains(body, "字幕内容") {
		t.Fatalf("note dropped the model text: %s", body)
	}
	if strings.Contains(body, "已生成论文解读") {
		t.Fatal("note used the paper sentence")
	}
	if got := mediaModTime(t, video); !got.Equal(before) {
		t.Fatal("media mtime changed")
	}
	pdf := notePDF(t, app, video)
	raw, err := os.ReadFile(pdf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, []byte("%PDF")) {
		t.Fatal("result is not a pdf")
	}
	assertNoteEvent(t, app, "side-1", body)
	sessionID := fileCompanionSessionID(mustCanonical(t, video))
	history := app.fileCompanionHistory(sessionID)
	var assistant string
	for _, msg := range history {
		if msg.Role == "assistant" {
			assistant = msg.Text
			if msg.ResultPath != "" {
				t.Fatalf("result path = %q", msg.ResultPath)
			}
			if msg.NoteCardPath == "" {
				t.Fatal("history dropped the pdf card")
			}
		}
	}
	if !strings.Contains(assistant, "字幕内容") {
		t.Fatalf("history = %q", assistant)
	}
	entries := app.imHandler.memory.Load("file-companion:" + sessionID)
	stored := ""
	for _, entry := range entries {
		if strings.EqualFold(entry.Role, "assistant") {
			stored, _ = entry.Content.(string)
		}
	}
	if strings.TrimSpace(stored) != fileCompanionVideoNoteStored {
		t.Fatalf("stored assistant = %q", stored)
	}
}

func TestFileCompanionClosedSurfaceStaysEmptyForMP4(t *testing.T) {
	app := newVideoNoteTestApp(t)
	for _, name := range []string{"clip.mp4", "clip.wav", "clip.mp3", "clip.m4a", "clip.flac"} {
		path := writeMedia(t, app, name, []byte("x"))
		doc, err := app.FileCompanionOpen(path)
		if err != nil {
			t.Fatal(err)
		}
		userID := "file-companion:" + doc.SessionID
		if petCompanionToolsDisabled(userID) {
			t.Fatal("pet tool mode turned on")
		}
		if got := app.imHandler.fileCompanionClosedSurfaceTools(userID); len(got) != 0 {
			t.Fatalf("%s tools = %#v", name, got)
		}
		callbacks := &sharedAgentLoopCallbacks{userID: userID, semanticSurface: &semanticCallSurface{}}
		if tools := callbacks.BuildToolsForModelRequest("生成笔记", 1); tools != nil {
			t.Fatalf("%s model tools = %#v", name, tools)
		}
		if name != "clip.mp4" {
			continue
		}
		target := filepath.Join(t.TempDir(), "forged.txt")
		result := app.imHandler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
			UserID: userID,
			ToolCall: llm.ToolCall{ID: "forged", Type: "function", Function: llm.ToolCallFunction{
				Name:      "write_file",
				Arguments: `{"path":"` + filepath.ToSlash(target) + `","content":"nope"}`,
			}},
		})
		if result.Outcome != toolOutcomeFailed || result.FailureKind != toolFailurePolicyRejected {
			t.Fatalf("forged tool result = %+v", result)
		}
		if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
			t.Fatalf("forged tool wrote %s (%v)", target, statErr)
		}
	}
}

func TestFileCompanionVideoNoteFailureWritesNothing(t *testing.T) {
	app := newVideoNoteTestApp(t)
	empty := writeMedia(t, app, "empty.mp4", nil)
	// writeMedia uses the bytes it is given. An empty file is a separate case.
	empty = filepath.Join(t.TempDir(), "empty.mp4")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(empty); err != nil {
		t.Fatal(err)
	}
	err := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: empty, Text: "生成笔记", Purpose: "video-note"})
	if err == nil || err.Error() != fileCompanionErrEmptyFile {
		t.Fatalf("empty = %v", err)
	}
	if noteFiles(t, app, empty) != "" {
		t.Fatal("empty file wrote a note")
	}

	pdf := writeMedia(t, app, "paper.pdf", []byte("%PDF-1.7"))
	err = app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: pdf, Purpose: "video-note", Text: "生成笔记"})
	if err == nil || err.Error() != fileCompanionErrNoteMediaOnly {
		t.Fatalf("pdf purpose = %v", err)
	}
	video := writeMedia(t, app, "clip.mp4", []byte("video"))
	err = app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: video, Purpose: "paper", Text: "论文解读"})
	if err == nil || err.Error() != fileCompanionErrNotPaper {
		t.Fatalf("paper on video = %v", err)
	}
	if noteFiles(t, app, video) != "" {
		t.Fatal("rejected purpose wrote a note")
	}
}

func TestFileCompanionRememberNoteKeepsEarlierTurns(t *testing.T) {
	app := newVideoNoteTestApp(t)
	opened, err := app.FileCompanionOpenWebPage("https://www.youtube.com/watch?v=keep-chat")
	if err != nil {
		t.Fatal(err)
	}
	userID := "file-companion:" + opened.SessionID
	if err := app.ensureFileCompanionHandler(userID, opened.SessionID); err != nil {
		t.Fatal(err)
	}
	prior := fileCompanionAppendNoteExcerpt("这个页面在讲什么", "OLD_NOTE")
	app.imHandler.memory.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: prior},
		{Role: "assistant", Content: "还没有笔记"},
	})
	app.fileCompanionRememberNote(userID, "记笔记", "https://www.youtube.com/watch?v=keep-chat")
	app.fileCompanionRememberNote(userID, "记笔记", "https://www.youtube.com/watch?v=keep-chat")
	loaded := app.imHandler.memory.Load(userID)
	if len(loaded) != 6 {
		t.Fatalf("turns = %d %#v", len(loaded), loaded)
	}
	first, _ := loaded[0].Content.(string)
	if first != "这个页面在讲什么" || strings.Contains(first, "OLD_NOTE") {
		t.Fatalf("earlier question = %q", first)
	}
	if loaded[1].Content.(string) != "还没有笔记" {
		t.Fatalf("earlier answer = %#v", loaded[1].Content)
	}
	for _, entry := range loaded[2:] {
		if strings.EqualFold(entry.Role, "assistant") && entry.Content.(string) != fileCompanionVideoNoteStored {
			t.Fatalf("stored note = %#v", entry.Content)
		}
	}
	history, err := app.FileCompanionWebHistory(opened.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 6 || history[0].Text != "这个页面在讲什么" || history[2].Text != "记笔记" || history[4].Text != "记笔记" {
		t.Fatalf("restored = %#v", history)
	}
	if app.fileCompanionVideoNoteTurnIndex(opened.SessionID) != 2 {
		t.Fatalf("note turn = %d", app.fileCompanionVideoNoteTurnIndex(opened.SessionID))
	}
	dir := filepath.Join(app.GetDataDir(), "file-companion", "results", opened.SessionID)
	if err := os.WriteFile(filepath.Join(dir, "watch-笔记.md"), []byte("## 概要\n第二版正文\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "watch-笔记.pdf"), []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	shown, err := app.FileCompanionWebHistory(opened.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if shown[3].Text != fileCompanionVideoNoteStored || shown[3].NoteCardPath != "" || shown[3].VideoNote {
		t.Fatalf("earlier note = %#v", shown[3])
	}
	if !strings.Contains(shown[5].Text, "第二版正文") || shown[5].NoteCardPath == "" || !shown[5].VideoNote || !shown[5].WebNote {
		t.Fatalf("latest note = %#v", shown[5])
	}
}

func TestFileCompanionVideoNoteFollowUpUsesExcerptOnce(t *testing.T) {
	app := newVideoNoteTestApp(t)
	audio := writeMedia(t, app, "talk.wav", tinyPCMWav(1))
	writeSidecar(t, audio, "1\n00:00:00,000 --> 00:00:01,000\n你好\n")
	marker := "UNIQUE_NOTE_SENTENCE"
	long := marker + strings.Repeat("甲", 5000) + "TAIL_MARK"
	setNoteModel(t, long)
	if err := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: audio, Text: "生成笔记", RequestID: "long-1"}); err != nil {
		t.Fatal(err)
	}
	var follow string
	prev := fileCompanionTurnModelHook
	fileCompanionTurnModelHook = func(msg IMUserMessage) *IMAgentResponse {
		follow = msg.Text
		return &IMAgentResponse{Text: "摘录已看到"}
	}
	t.Cleanup(func() { fileCompanionTurnModelHook = prev })
	if err := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: audio, Text: "笔记里的第三点是什么"}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(follow, marker) != 1 || strings.Count(follow, "TAIL_MARK") != 1 {
		t.Fatalf("excerpt count marker=%d tail=%d", strings.Count(follow, marker), strings.Count(follow, "TAIL_MARK"))
	}
	idx := strings.Index(follow, "已生成的笔记摘录")
	if idx < 0 {
		t.Fatal("follow-up omitted the note")
	}
	excerpt := follow[idx:]
	if fileCompanionCountRunes(excerpt) > fileCompanionVideoNoteFollowTokens+80 {
		t.Fatalf("excerpt runes = %d", fileCompanionCountRunes(excerpt))
	}
	var second string
	fileCompanionNoteModelHook = func(req fileCompanionNoteModelRequest) *IMAgentResponse {
		second = req.Msg.Text
		return &IMAgentResponse{Text: "第二版笔记"}
	}
	if err := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: audio, Text: "生成笔记", RequestID: "long-2"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(second, marker) {
		t.Fatal("regenerating a note attached the old note")
	}
}

func TestFileCompanionNoteExcerptStaysOutOfTheTranscript(t *testing.T) {
	question := "笔记里的第一点是什么"
	excerpt := "UNIQUE_EXCERPT_BODY"
	stored := fileCompanionAppendNoteExcerpt(question, excerpt)
	if got := fileCompanionVisibleUserText(stored); got != question {
		t.Fatalf("web bubble = %q", got)
	}
	local := fileCompanionAppendNoteExcerpt(fileCompanionUserTurn(`C:\talk.mp4`, question, ""), excerpt)
	if got := fileCompanionVisibleUserText(local); got != question {
		t.Fatalf("local bubble = %q", got)
	}
	shown := fileCompanionDisplayMessages([]agent.ConversationEntry{
		{Role: "user", Content: stored},
		{Role: "assistant", Content: "回答里可以出现" + fileCompanionNoteExcerptLabel},
	}, nil)
	if len(shown) != 2 || shown[0].Text != question {
		t.Fatalf("bubbles = %#v", shown)
	}
	if !strings.Contains(shown[1].Text, fileCompanionNoteExcerptLabel) {
		t.Fatal("assistant text was treated as a user excerpt")
	}

	blocks := fileCompanionStripNoteExcerptHistory("file-companion:blocks", []agent.ConversationEntry{{
		Role: "user",
		Content: []interface{}{
			map[string]interface{}{"type": "text", "text": stored},
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,aa"}},
		},
	}})
	gotBlocks, ok := blocks[0].Content.([]interface{})
	if !ok || len(gotBlocks) != 2 {
		t.Fatalf("blocks = %#v", blocks[0].Content)
	}
	textBlock := gotBlocks[0].(map[string]interface{})
	if textBlock["text"] != question {
		t.Fatalf("text block = %#v", textBlock["text"])
	}
	if _, ok := gotBlocks[1].(map[string]interface{})["image_url"]; !ok {
		t.Fatal("image block was rewritten")
	}
	desktop := fileCompanionStripNoteExcerptHistory("desktop-user", []agent.ConversationEntry{{Role: "user", Content: stored}})
	if desktop[0].Content.(string) != stored {
		t.Fatal("a non-companion transcript was rewritten")
	}

	app := newVideoNoteTestApp(t)
	opened, err := app.FileCompanionOpenWebPage("https://www.youtube.com/watch?v=excerpt-hide")
	if err != nil {
		t.Fatal(err)
	}
	userID := "file-companion:" + opened.SessionID
	if err := app.ensureFileCompanionHandler(userID, opened.SessionID); err != nil {
		t.Fatal(err)
	}
	app.imHandler.memory.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: stored},
		{Role: "assistant", Content: "只回答问题"},
	})
	history, err := app.FileCompanionWebHistory(opened.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Text != question || strings.Contains(history[0].Text, excerpt) {
		t.Fatalf("restored web chat = %#v", history)
	}
	app.imHandler.saveConversationHistoryTimed(userID, []agent.ConversationEntry{
		{Role: "user", Content: stored},
		{Role: "assistant", Content: "只回答问题"},
	}, nil)
	loaded := app.imHandler.memory.Load(userID)
	if len(loaded) != 2 {
		t.Fatalf("saved turns = %#v", loaded)
	}
	userText, _ := loaded[0].Content.(string)
	if userText != question || strings.Contains(userText, excerpt) {
		t.Fatalf("persisted user = %q", userText)
	}
	if loaded[1].Content.(string) != "只回答问题" {
		t.Fatalf("persisted assistant = %#v", loaded[1].Content)
	}
}

func TestFileCompanionVideoNoteDoesNotPostRemoteASR(t *testing.T) {
	app := newVideoNoteTestApp(t)
	audio := writeMedia(t, app, "talk.mp3", []byte("ID3tiny"))
	var posts int
	prev := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "asr/transcribe") {
			posts++
		}
		return nil, os.ErrClosed
	})
	t.Cleanup(func() { http.DefaultTransport = prev })
	prevTo := fileCompanionNativeToWAV
	fileCompanionNativeToWAV = func(data []byte, format string) ([]byte, error) {
		return tinyPCMWav(1), nil
	}
	t.Cleanup(func() { fileCompanionNativeToWAV = prevTo })
	prevASR := fileCompanionNoteASRHook
	fileCompanionNoteASRHook = func([]byte) (string, error) { return "本地转写", nil }
	t.Cleanup(func() { fileCompanionNoteASRHook = prevASR })
	setNoteModel(t, "## 概要\n本地\n## 章节\n00:00 开始\n## 要点\n00:00 一点\n## 金句\n00:00 「话」\n## 不确定的地方\n无\n")
	media := &fakeVideoNoteMedia{}
	restore := useVideoNoteMedia(t, media)
	defer restore()
	if err := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: audio, Text: "生成笔记"}); err != nil {
		t.Fatal(err)
	}
	if posts != 0 {
		t.Fatalf("remote ASR posts = %d", posts)
	}
	if media.installs != 0 {
		t.Fatal("in-limit mp3 installed ffmpeg")
	}
}

func TestFileCompanionVideoNoteRejectsOverDuration(t *testing.T) {
	app := newVideoNoteTestApp(t)
	video := writeMedia(t, app, "long.mp4", []byte("video"))
	media := &fakeVideoNoteMedia{located: true, probe: videoNoteProbeInfo{Duration: 10801 * time.Second, HasAudio: true}}
	restore := useVideoNoteMedia(t, media)
	defer restore()
	err := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: video, Text: "生成笔记", RequestID: "too-long"})
	if err == nil || err.Error() != fileCompanionErrVideoTooLong {
		t.Fatalf("err = %v", err)
	}
	if noteFiles(t, app, video) != "" {
		t.Fatal("over-duration note was written")
	}
}

func TestFileCompanionVideoNoteDecodeLimits(t *testing.T) {
	app := newVideoNoteTestApp(t)
	media := &fakeVideoNoteMedia{installErr: os.ErrNotExist}
	restore := useVideoNoteMedia(t, media)
	defer restore()
	var calls int
	var sizes []int
	var wavs int
	prev := fileCompanionNativeToWAV
	fileCompanionNativeToWAV = func(data []byte, format string) ([]byte, error) {
		calls++
		sizes = append(sizes, len(data))
		work := filepath.Join(app.GetDataDir(), "file-companion", "work")
		_ = filepath.Walk(work, func(path string, info os.FileInfo, err error) error {
			if err == nil && info != nil && strings.HasSuffix(strings.ToLower(path), ".wav") {
				wavs++
				if wavs > 1 {
					t.Fatalf("work dir held more than one wav: %s", path)
				}
			}
			return nil
		})
		if int64(len(data)) > audioconv.MaxNativeAudioInputBytes {
			t.Fatalf("ToWAV input = %d", len(data))
		}
		formatN, rate, _, _, _, dataLen, err := wavPCMLayout(data)
		if err == nil && formatN == 1 && rate > 0 {
			block := 2
			if sec := dataLen / (rate * block); sec > 60 {
				t.Fatalf("slice = %ds", sec)
			}
		}
		if strings.Contains(string(data), "TOO_BIG") {
			return nil, os.ErrInvalid // not a size error
		}
		if bytes.HasPrefix(data, []byte("PCM10")) {
			return nil, errors.New("audioconv: mp3 decoded PCM too large")
		}
		return tinyPCMWav(1), nil
	}
	t.Cleanup(func() { fileCompanionNativeToWAV = prev })
	prevASR := fileCompanionNoteASRHook
	fileCompanionNoteASRHook = func([]byte) (string, error) { return "转写", nil }
	t.Cleanup(func() { fileCompanionNoteASRHook = prevASR })
	setNoteModel(t, "## 概要\n好\n## 章节\n00:00 开始\n## 要点\n00:00 一点\n## 金句\n00:00 「话」\n## 不确定的地方\n无\n")

	big := writePCMWav(t, app, "big.wav", 8000, int(audioconv.MaxNativeAudioInputBytes)+8000)
	if err := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: big, Text: "生成笔记", RequestID: "big-wav"}); err != nil {
		t.Fatal(err)
	}
	if media.installs != 0 {
		t.Fatal("pcm wav installed ffmpeg")
	}
	if calls < 2 {
		t.Fatalf("slices = %d", calls)
	}
	for _, n := range sizes {
		if int64(n) > audioconv.MaxNativeAudioInputBytes {
			t.Fatalf("slice %d", n)
		}
	}

	calls = 0
	media.installs = 0
	short := writeMedia(t, app, "short.mp3", []byte("ID3short"))
	if err := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: short, Text: "生成笔记"}); err != nil {
		t.Fatal(err)
	}
	if media.installs != 0 || calls == 0 {
		t.Fatalf("short mp3 installs=%d toWAV=%d", media.installs, calls)
	}

	calls = 0
	opus := writeMedia(t, app, "voice.ogg", append([]byte("OggS"), []byte("OpusHead")...))
	if err := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: opus, Text: "生成笔记"}); err != nil {
		t.Fatal(err)
	}
	if media.installs != 0 || calls == 0 {
		t.Fatalf("opus installs=%d toWAV=%d", media.installs, calls)
	}

	calls = 0
	vorbis := writeMedia(t, app, "music.ogg", append([]byte("OggS"), []byte("\x01vorbis")...))
	err := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: vorbis, Text: "生成笔记"})
	if calls != 0 {
		t.Fatal("vorbis called ToWAV")
	}
	if media.installs == 0 {
		t.Fatal("vorbis did not install")
	}
	if err == nil || strings.Contains(err.Error(), "可能已损坏") || err.Error() != fileCompanionErrOggNotOpus {
		t.Fatalf("vorbis err = %v", err)
	}

	media.installs = 0
	over := writeMedia(t, app, "over.mp3", bytes.Repeat([]byte{0}, int(audioconv.MaxNativeAudioInputBytes)+1))
	err = app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: over, Text: "生成笔记"})
	if calls != 0 {
		t.Fatal("over-limit mp3 called ToWAV")
	}
	if media.installs == 0 {
		t.Fatal("over-limit mp3 did not install")
	}
	if err == nil || strings.Contains(err.Error(), "可能已损坏") || err.Error() != fileCompanionErrNativeLimit {
		t.Fatalf("over-limit err = %v", err)
	}
	for _, name := range []string{"over.opus", "over.silk"} {
		media.installs = 0
		path := writeMedia(t, app, name, bytes.Repeat([]byte{1}, int(audioconv.MaxNativeAudioInputBytes)+1))
		err = app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: path, Text: "生成笔记"})
		if media.installs == 0 {
			t.Fatalf("%s did not install", name)
		}
		if err == nil || strings.Contains(err.Error(), "可能已损坏") || err.Error() != fileCompanionErrNativeLimit {
			t.Fatalf("%s err = %v", name, err)
		}
	}
	media.installs = 0
	calls = 0
	ten := writeMedia(t, app, "ten.mp3", []byte("PCM10"))
	err = app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: ten, Text: "生成笔记"})
	if calls == 0 || media.installs == 0 {
		t.Fatalf("10-minute mp3 calls=%d installs=%d", calls, media.installs)
	}
	if err == nil || strings.Contains(err.Error(), "可能已损坏") || err.Error() != fileCompanionErrNativeLimit {
		t.Fatalf("10-minute err = %v", err)
	}

	for _, name := range []string{"song.m4a", "song.aac", "song.flac", "film.mkv"} {
		media.installs = 0
		path := writeMedia(t, app, name, []byte("container"))
		err = app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: path, Text: "生成笔记"})
		if media.installs == 0 {
			t.Fatalf("%s did not install", name)
		}
		if err == nil || strings.Contains(err.Error(), "可能已损坏") {
			t.Fatalf("%s err = %v", name, err)
		}
		if noteFiles(t, app, path) != "" {
			t.Fatalf("%s wrote a note after install failed", name)
		}
	}
}

func TestFileCompanionVideoNoteArchiveTableAndPartial(t *testing.T) {
	var sumDarwin int64
	for _, item := range fileCompanionFFmpegArchives() {
		if item.Size <= 0 || item.Size >= maxFFmpegZipBytes {
			t.Fatalf("%s/%s size %d", item.GOOS, item.GOARCH, item.Size)
		}
		if item.Size2 != 0 && item.Size2 >= maxFFmpegZipBytes {
			t.Fatalf("second zip %d", item.Size2)
		}
		if item.GOOS == "darwin" && item.GOARCH == "amd64" {
			sumDarwin = item.Size + item.Size2
		}
		if strings.Contains(item.URL, "johnvansickle") || strings.Contains(item.URL, "/latest/") {
			t.Fatalf("unpinned url %s", item.URL)
		}
	}
	if sumDarwin >= maxFFmpegZipBytes {
		t.Fatalf("darwin zip sum %d", sumDarwin)
	}
	app := newVideoNoteTestApp(t)
	prev := fileCompanionFFmpegFetchHook
	fileCompanionFFmpegFetchHook = func(context.Context, string) ([]byte, error) {
		return nil, os.ErrClosed
	}
	t.Cleanup(func() { fileCompanionFFmpegFetchHook = prev })
	prevLook := fileCompanionFFmpegLookHook
	fileCompanionFFmpegLookHook = func(string) (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() { fileCompanionFFmpegLookHook = prevLook })
	prevWinget := fileCompanionWingetHook
	fileCompanionWingetHook = func(context.Context) error { return os.ErrNotExist }
	t.Cleanup(func() { fileCompanionWingetHook = prevWinget })
	ffmpegPositiveLocate.mu.Lock()
	ffmpegPositiveLocate.ffmpeg, ffmpegPositiveLocate.probe = "", ""
	ffmpegPositiveLocate.mu.Unlock()
	err := app.fileCompanionInstallPrivateFFmpeg(context.Background())
	if err == nil {
		t.Fatal("failed fetch installed ffmpeg")
	}
	tools := filepath.Join(app.GetDataDir(), "tools")
	_ = filepath.Walk(tools, func(path string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			t.Fatalf("partial left %s", path)
		}
		return nil
	})
}

func TestFileCompanionVideoNoteClearDropsLateFile(t *testing.T) {
	app := newVideoNoteTestApp(t)
	video := writeMedia(t, app, "clip.mp4", []byte("video"))
	started := make(chan *exec.Cmd, 1)
	media := &fakeVideoNoteMedia{}
	media.installFn = func(ctx context.Context) error {
		cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", "Start-Sleep -Seconds 30")
		if err := cmd.Start(); err != nil {
			return err
		}
		started <- cmd
		return cmd.Wait()
	}
	restore := useVideoNoteMedia(t, media)
	defer restore()
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: video, Text: "生成笔记", RequestID: "cancel-1"})
	}()
	var cmd *exec.Cmd
	select {
	case cmd = <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("install did not start")
	}
	progress := recordedProgress(app)
	if !strings.Contains(progress, "正在安装视频解码器") || !strings.Contains(progress, "cancel-1") || !strings.Contains(progress, "file-companion:") {
		t.Fatalf("progress = %s", progress)
	}
	var event AIAssistantStreamEvent
	if err := json.Unmarshal([]byte(progressLine(progress)), &event); err != nil {
		t.Fatal(err)
	}
	if event.RequestID != "cancel-1" || !strings.HasPrefix(event.SessionKey, "file-companion:") || !strings.Contains(event.Text, "正在安装视频解码器") {
		t.Fatalf("event = %+v", event)
	}
	userID := "file-companion:" + fileCompanionSessionID(mustCanonical(t, video))
	before := app.fileCompanionChatGen(userID)
	if err := app.ClearFileCompanionChat(video); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errCh:
		if err == nil || err.Error() != fileCompanionErrNoteCancelled {
			t.Fatalf("cancel err = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("clear did not finish the note")
	}
	if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
		_ = cmd.Process.Kill()
		t.Fatal("child still running")
	}
	if noteFiles(t, app, video) != "" {
		t.Fatal("late note file remained")
	}
	if app.fileCompanionChatGen(userID) == before {
		t.Fatal("clear did not bump chat generation")
	}
}

func TestFileCompanionVideoNoteCancelKeepsChatGen(t *testing.T) {
	app := newVideoNoteTestApp(t)
	video := writeMedia(t, app, "clip.mp4", []byte("video"))
	canonical := mustCanonical(t, video)
	userID := "file-companion:" + fileCompanionSessionID(canonical)
	if err := app.ensureFileCompanionHandler(userID, fileCompanionSessionID(canonical)); err != nil {
		t.Fatal(err)
	}
	app.imHandler.memory.Append(userID, agent.ConversationEntry{Role: "user", Content: "刚才那句"})
	gen := app.fileCompanionChatGen(userID)
	if err := app.FileCompanionCancelNote(video); err != nil {
		t.Fatal(err)
	}
	if app.fileCompanionChatGen(userID) != gen {
		t.Fatal("cancelling a note bumped chatGen")
	}
	entries := app.imHandler.memory.Load(userID)
	found := false
	for _, entry := range entries {
		if text, _ := entry.Content.(string); strings.Contains(text, "刚才那句") {
			found = true
		}
	}
	if !found {
		t.Fatal("cancel dropped the short phrase")
	}
}

func TestFileCompanionVideoNoteQuietDropOmitsTranscript(t *testing.T) {
	app := &App{testHomeDir: t.TempDir(), configCacheValid: true, configCache: appConfigASREnabled()}
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	secret := "对你好啊对你好啊对你好啊对你好啊对你好啊"
	prevHook := asrTranscribeModelHook
	asrTranscribeModelHook = func([]float32) (string, error) { return secret, nil }
	t.Cleanup(func() { asrTranscribeModelHook = prevHook })
	t.Setenv("MACLAW_ASR_DUMP", "1")
	_, err := app.transcribeWAVBytes(tinyPCMWav(1), asrTranscribeOpts{Quiet: true, SkipVAD: true})
	if err != nil {
		t.Fatal(err)
	}
	logged := buf.String()
	if strings.Contains(logged, secret) {
		t.Fatalf("quiet log contained the transcript: %s", logged)
	}
	if strings.Contains(logged, "asr_dump") || strings.Contains(logged, "loading model from") {
		t.Fatalf("quiet log contained a path: %s", logged)
	}
}

func TestFileCompanionNoteClockIgnoresSilentTail(t *testing.T) {
	frames := []videoNoteFrame{{At: 3 * time.Second}}
	if got := fileCompanionNoteClock(true, time.Minute, frames); got != 3*time.Second {
		t.Fatalf("silent tail clock = %s", got)
	}
	if got := fileCompanionNoteClock(true, time.Minute, nil); got != 0 {
		t.Fatalf("no frame clock = %s", got)
	}
	if got := fileCompanionNoteClock(false, time.Minute, frames); got != time.Minute {
		t.Fatalf("speech clock = %s", got)
	}
	long := []videoNoteFrame{{At: 0}, {At: 200 * time.Second}}
	if got := fileCompanionNoteClock(true, 0, long); got != 200*time.Second {
		t.Fatalf("frame clock = %s", got)
	}
}

func TestFileCompanionRangeStampStaysOrganized(t *testing.T) {
	in := "## 概要\n讲过了\n## 章节\n- 0:00–2:00 机器翻译的几个时代（推断）\n- 3:00–4:00 Transformer 编码器（推断）\n没有时间的一句\n## 要点\n- 0:30 一点\n## 金句\n- 0:00 「原话」\n## 不确定的地方\n画面里的人名没看清\n"
	out := fileCompanionRewriteNoteTimestamps(in, 30*time.Minute)
	if strings.Contains(out, "时间不明 - 0:00–2:00") || strings.Contains(out, "时间不明 - 3:00–4:00") || strings.Contains(out, "时间不明 - 0:00 「") {
		t.Fatalf("in-range line marked unknown:\n%s", out)
	}
	if !strings.Contains(out, "0:00–2:00 机器翻译的几个时代（推断）") || !strings.Contains(out, "00:30 一点") || !strings.Contains(out, "00:00 「原话」") {
		t.Fatalf("organized line dropped:\n%s", out)
	}
	idx := strings.Index(out, "## 不确定的地方")
	if idx < 0 {
		t.Fatal("missing uncertain section")
	}
	tail := out[idx:]
	if strings.Contains(tail, "机器翻译的几个时代") || strings.Contains(tail, "Transformer 编码器") || strings.Contains(tail, "「原话」") {
		t.Fatalf("organized line copied into uncertain:\n%s", tail)
	}
	if !strings.Contains(tail, "没有时间的一句") || !strings.Contains(tail, "画面里的人名没看清") {
		t.Fatalf("real doubt dropped:\n%s", tail)
	}
	if got := fileCompanionOrganizedCaption(out, 3*time.Minute+30*time.Second); got != "Transformer 编码器（推断）" {
		t.Fatalf("caption = %q", got)
	}
	// 45:00 parses (minutes stay under 60) and is past a 30-minute recording.
	// 90:00 does not parse, so it must not be the late-stamp case.
	late := fileCompanionRewriteNoteTimestamps("## 章节\n45:00 太晚\n## 不确定的地方\n无\n", 30*time.Minute)
	if !strings.Contains(late, "时间不明 太晚") || strings.Contains(late, "45:00") {
		t.Fatalf("late = %s", late)
	}
	empty := fileCompanionRewriteNoteTimestamps("## 章节\n0:00 开头\n0:00–13:32 整段\n5:01 黑板\n## 金句\n05:01 「话」\n## 不确定的地方\n无\n", 0)
	if !strings.Contains(empty, "0:00 开头") || !strings.Contains(empty, "时间不明 整段") || !strings.Contains(empty, "时间不明 黑板") || !strings.Contains(empty, "时间不明 「话」") || strings.Contains(empty, "13:32") || strings.Contains(empty, "5:01") {
		t.Fatalf("empty recording = %s", empty)
	}
	lateRange := fileCompanionRewriteNoteTimestamps("## 章节\n0:00–45:00 超出\n## 不确定的地方\n无\n", 30*time.Minute)
	if !strings.Contains(lateRange, "时间不明 超出") || strings.Contains(lateRange, "45:00") {
		t.Fatalf("range end = %s", lateRange)
	}
	unknown := fileCompanionRewriteNoteTimestamps("## 章节\n5:01 保留\n## 不确定的地方\n无\n", -1)
	if !strings.Contains(unknown, "5:01 保留") || strings.Contains(unknown, "时间不明 保留") {
		t.Fatalf("unmeasured length = %s", unknown)
	}
	spaced := fileCompanionRewriteNoteTimestamps("## 章节\n0:00 – 13:32 整段\n0:30 - 检查接口\n## 不确定的地方\n无\n", time.Minute)
	if !strings.Contains(spaced, "时间不明 整段") || strings.Contains(spaced, "13:32") || strings.Contains(spaced, "时间不明 检查接口") || !strings.Contains(spaced, "00:30 - 检查接口") {
		t.Fatalf("spaced range = %s", spaced)
	}
	spacedKept := fileCompanionRewriteNoteTimestamps("## 章节\n0:00 – 2:00 仍在\n## 不确定的地方\n无\n", 30*time.Minute)
	if strings.Contains(spacedKept, "时间不明 仍在") || !strings.Contains(spacedKept, "2:00") || !strings.Contains(spacedKept, "仍在") {
		t.Fatalf("in-range spaced = %s", spacedKept)
	}
	broken := fileCompanionRewriteNoteTimestamps("## 章节\n0:00–99:99 坏范围\n0:00–99:99粘在一起\n## 不确定的地方\n无\n", 30*time.Minute)
	if !strings.Contains(broken, "时间不明 坏范围") || !strings.Contains(broken, "时间不明 粘在一起") || strings.Contains(broken, "99:99") {
		t.Fatalf("broken range = %s", broken)
	}
	glued := fileCompanionRewriteNoteTimestamps("## 章节\n0:27–2:04，检查显卡\n## 不确定的地方\n无\n", 0)
	if !strings.Contains(glued, "时间不明 检查显卡") || strings.Contains(glued, "2:04") || strings.Contains(glued, "0:27") {
		t.Fatalf("glued range = %s", glued)
	}
	gluedKept := fileCompanionRewriteNoteTimestamps("## 章节\n0:27–2:04，检查显卡\n## 不确定的地方\n无\n", 30*time.Minute)
	if !strings.Contains(gluedKept, "0:27–2:04，检查显卡") || strings.Contains(gluedKept, "时间不明 检查") {
		t.Fatalf("in-range glued = %s", gluedKept)
	}
	word := fileCompanionRewriteNoteTimestamps("## 章节\n0:00-废卡 说明\n## 不确定的地方\n无\n", 30*time.Minute)
	if strings.Contains(word, "时间不明") || !strings.Contains(word, "0:00-废卡 说明") {
		t.Fatalf("hyphenated word = %s", word)
	}
	for _, line := range strings.Split(late, "\n") {
		if strings.TrimSpace(line) == "无" {
			t.Fatalf("placeholder left above the real doubt:\n%s", late)
		}
	}
}

func TestFileCompanionDisplayMathStaysInTheChapter(t *testing.T) {
	in := "## 章节\n0:00–2:00 推导如下\n$$\nE=mc^2\n$$\n$$a+b$$\n\\[\nx\\]\n没有时间的一句\n## 要点\n能量守恒\n## 不确定的地方\n无\n"
	out := fileCompanionRewriteNoteTimestamps(in, 10*time.Minute)
	if !strings.Contains(out, "$$\nE=mc^2\n$$") || !strings.Contains(out, "$$a+b$$") || !strings.Contains(out, "\\[\nx\\]") {
		t.Fatalf("display math rewritten:\n%s", out)
	}
	if strings.Contains(out, "时间不明 $$") || strings.Contains(out, "时间不明 E=mc") || strings.Contains(out, "时间不明 a+b") || strings.Contains(out, "时间不明 x") {
		t.Fatalf("formula marked unknown:\n%s", out)
	}
	if !strings.Contains(out, "时间不明 没有时间的一句") {
		t.Fatalf("untimed chapter kept its missing time:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "无" {
			t.Fatalf("placeholder left above the real doubt:\n%s", out)
		}
	}
	inline := fileCompanionRewriteNoteTimestamps("## 章节\n- 0:00–2:00 令 $x$ 成立\n## 不确定的地方\n无\n", 10*time.Minute)
	if !strings.Contains(inline, "0:00–2:00 令 $x$ 成立") || strings.Contains(inline, "时间不明") {
		t.Fatalf("inline formula = %s", inline)
	}
}

func TestFileCompanionBulletedDisplayMathStays(t *testing.T) {
	in := "## 章节\n- $$E=mc^2$$\n- $$\nE=mc^2\n- $$\n* \\[y\\]\n-1 不是公式\n## 要点\n- 能量守恒\n## 不确定的地方\n无\n"
	out := fileCompanionRewriteNoteTimestamps(in, 10*time.Minute)
	if !strings.Contains(out, "$$E=mc^2$$") || strings.Contains(out, "时间不明 - $$") || strings.Contains(out, "- $$") {
		t.Fatalf("bulleted formula =\n%s", out)
	}
	if !strings.Contains(out, "$$\nE=mc^2\n$$") {
		t.Fatalf("block =\n%s", out)
	}
	if !strings.Contains(out, "\\[y\\]") || strings.Contains(out, "* \\[") {
		t.Fatalf("bracket =\n%s", out)
	}
	if !strings.Contains(out, "时间不明 -1 不是公式") {
		t.Fatalf("minus rewritten =\n%s", out)
	}
	if strings.Contains(out, "时间不明 能量") || !strings.Contains(out, "- 能量守恒") {
		t.Fatalf("takeaway =\n%s", out)
	}
}

func TestFileCompanionFormulaRowStaysInDisplayMath(t *testing.T) {
	in := "## 章节\n0:00–2:00 分段\n$$\n\\begin{cases}\n0:00 & t_0 \\\\\n2:00 & t_1\n\\end{cases}\n$$\n12:05 令 $x$ 成立\n$$\nE=mc^2\n45:00 太晚\n## 不确定的地方\n无\n"
	out := fileCompanionRewriteNoteTimestamps(in, 30*time.Minute)
	if !strings.Contains(out, "0:00 & t_0") || !strings.Contains(out, "2:00 & t_1") || strings.Contains(out, "00:00 &") {
		t.Fatalf("formula row lifted:\n%s", out)
	}
	if !strings.Contains(out, "0:00–2:00 分段") || !strings.Contains(out, "12:05 令 $x$ 成立") {
		t.Fatalf("chapters =\n%s", out)
	}
	if !strings.Contains(out, "时间不明 太晚") || strings.Contains(out, "45:00") {
		t.Fatalf("open block swallowed the next chapter:\n%s", out)
	}
}

func TestFileCompanionSymbolicFormulaRowStays(t *testing.T) {
	in := "## 章节\n0:00–2:00 分段\n$$\n0:00 = t_0\n2:00 = t_1\n$$\n12:05 下一句\n$$\nE=mc^2\n45:00 太晚\n## 不确定的地方\n无\n"
	out := fileCompanionRewriteNoteTimestamps(in, 30*time.Minute)
	if !strings.Contains(out, "0:00 = t_0") || !strings.Contains(out, "2:00 = t_1") || strings.Contains(out, "00:00 =") || strings.Contains(out, "02:00 =") {
		t.Fatalf("symbolic row lifted:\n%s", out)
	}
	if !strings.Contains(out, "12:05 下一句") || !strings.Contains(out, "时间不明 太晚") || strings.Contains(out, "45:00") {
		t.Fatalf("chapter =\n%s", out)
	}
	dir := t.TempDir()
	placed := fileCompanionInsertNoteFramesInto(out, dir, []videoNoteFrame{{At: 30 * time.Second, JPEG: []byte{1}}})
	chapter := strings.Index(placed, "0:00–2:00 分段")
	img := strings.Index(placed, "![板书 00:30]")
	row := strings.Index(placed, "0:00 = t_0")
	if chapter < 0 || img < 0 || row < 0 || !(chapter < img && img < row) || !strings.Contains(placed, "$$\n0:00 = t_0") {
		t.Fatalf("picture entered the symbolic formula:\n%s", placed)
	}
}

func TestFileCompanionChatNoteHidesFramePaths(t *testing.T) {
	md := "## 章节\n0:00–2:00 机器翻译的几个时代（推断）\n\n![板书 00:56](C:\\frames\\56.jpg)\n\n![板书 01:34](C:\\frames\\94.jpg)\n\n画面未识别\n\n## 要点\n注意力\n"
	out := fileCompanionNoteProse(md)
	if strings.Contains(out, "![") || strings.Contains(out, `C:\frames`) {
		t.Fatalf("chat kept a frame path:\n%s", out)
	}
	if !strings.Contains(out, "0:00–2:00 机器翻译的几个时代（推断）") || !strings.Contains(out, "画面未识别") || !strings.Contains(out, "## 要点\n注意力") {
		t.Fatalf("prose dropped:\n%s", out)
	}
	if strings.Contains(out, "\n\n\n") {
		t.Fatalf("blank run left behind:\n%s", out)
	}
}

func TestFileCompanionTakeawayKeepsUntimedConclusion(t *testing.T) {
	in := "## 章节\n0:00–2:00 一段\n## 要点\n注意力让解码看到不同的源词。\n- 0:30 仍可跳转\n## 金句\n没有时间的金句\n## 不确定的地方\n无\n"
	out := fileCompanionRewriteNoteTimestamps(in, 30*time.Minute)
	if strings.Contains(out, "时间不明 注意力") {
		t.Fatalf("takeaway marked unknown:\n%s", out)
	}
	if !strings.Contains(out, "注意力让解码看到不同的源词。") || !strings.Contains(out, "00:30 仍可跳转") {
		t.Fatalf("takeaway dropped:\n%s", out)
	}
	idx := strings.Index(out, "## 不确定的地方")
	if idx < 0 {
		t.Fatal("missing uncertain section")
	}
	tail := out[idx:]
	if strings.Contains(tail, "注意力让解码") || strings.Contains(tail, "仍可跳转") {
		t.Fatalf("takeaway copied into uncertain:\n%s", tail)
	}
	if !strings.Contains(tail, "没有时间的金句") {
		t.Fatalf("untimed quote dropped:\n%s", tail)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "无" {
			t.Fatalf("placeholder left above the quote:\n%s", out)
		}
	}
	mixed := fileCompanionRewriteNoteTimestamps("## 章节\n没有时间\n## 不确定的地方\n无\n画面里的人名没看清\n", time.Minute)
	if strings.Contains(mixed, "\n无\n") || !strings.Contains(mixed, "画面里的人名没看清") || !strings.Contains(mixed, "时间不明 没有时间") {
		t.Fatalf("mixed = %s", mixed)
	}
	late := fileCompanionRewriteNoteTimestamps("## 要点\n45:00 太晚的结论\n## 不确定的地方\n无\n", 30*time.Minute)
	if !strings.Contains(late, "时间不明 太晚的结论") || strings.Contains(late, "45:00") {
		t.Fatalf("late takeaway = %s", late)
	}
}

func TestFileCompanionNotePromptAsksForChaptersNotFrameLog(t *testing.T) {
	prompt := buildNoteModelPrompt("talk.mp4", "video", videoNoteEvidence{
		Frames: []videoNoteFrame{
			{At: 30 * time.Second, OCR: "E=mc"},
			{At: time.Minute, OCR: "板书"},
		},
	})
	if strings.Contains(prompt, "每个画面在") || strings.Contains(prompt, "要点、金句的每一条") {
		t.Fatalf("prompt still asks for a frame log:\n%s", prompt)
	}
	if !strings.Contains(prompt, "要点只写几条结论") || !strings.Contains(prompt, "不要按分钟复述口播") || !strings.Contains(prompt, "不要为每个画面各写一行") {
		t.Fatalf("prompt = %s", prompt)
	}
	if !strings.Contains(prompt, "画面 t=30s") || !strings.Contains(prompt, "识别不确定：E=mc") {
		t.Fatalf("evidence dropped:\n%s", prompt)
	}
	if strings.Contains(prompt, "这次记录从") || strings.Contains(prompt, "另印的进度") {
		t.Fatalf("unmeasured prompt invented a clock:\n%s", prompt)
	}
	timed := buildNoteModelPrompt("talk.mp4", "video", videoNoteEvidence{
		Duration: 10 * time.Minute,
		Vision:   true,
		Frames:   []videoNoteFrame{{At: 30 * time.Second}},
	})
	if !strings.Contains(timed, "这次记录从 0:00 到 10:00（600 秒）") || !strings.Contains(timed, "画面 t= 后面的数字是这个钟的秒") || !strings.Contains(timed, "画面上另印的进度、总时长和时钟不是这个钟") {
		t.Fatalf("timed prompt = %s", timed)
	}
	if !strings.Contains(timed, "画面 t=30s") {
		t.Fatalf("timed prompt dropped the frame clock:\n%s", timed)
	}
}

func TestFileCompanionFramesFollowChapterRanges(t *testing.T) {
	in := "## 概要\n讲过了\n## 章节\n- 0:00–2:00 机器翻译的几个时代（推断）\n- 3:00–4:00 Transformer 编码器（推断）\n## 要点\n- 0:30 一点\n"
	md := fileCompanionRewriteNoteTimestamps(in, 30*time.Minute)
	dir := t.TempDir()
	frames := []videoNoteFrame{
		{At: 56 * time.Second, JPEG: []byte{1}},
		{At: 94 * time.Second, JPEG: []byte{2}},
		{At: 3*time.Minute + 30*time.Second, JPEG: []byte{3}},
	}
	out := fileCompanionInsertNoteFramesInto(md, dir, frames)
	chapter1 := strings.Index(out, "0:00–2:00 机器翻译的几个时代（推断）")
	img1 := strings.Index(out, "![板书 00:56]")
	img2 := strings.Index(out, "![板书 01:34]")
	chapter2 := strings.Index(out, "3:00–4:00 Transformer 编码器（推断）")
	img3 := strings.Index(out, "![板书 03:30]")
	points := strings.Index(out, "## 要点")
	if chapter1 < 0 || img1 < 0 || img2 < 0 || chapter2 < 0 || img3 < 0 || points < 0 ||
		!(chapter1 < img1 && img1 < img2 && img2 < chapter2 && chapter2 < img3 && img3 < points) {
		t.Fatalf("frames left their chapter:\n%s", out)
	}
	if strings.Count(out, "机器翻译的几个时代（推断）") != 1 || strings.Count(out, "Transformer 编码器（推断）") != 1 {
		t.Fatalf("chapter sentence repeated under frames:\n%s", out)
	}

	plain := "## 章节\n只有说明\n## 要点\n无\n"
	ordered := fileCompanionInsertNoteFramesInto(plain, dir, []videoNoteFrame{
		{At: 0, JPEG: []byte{1}},
		{At: 30 * time.Second, JPEG: []byte{2}},
		{At: time.Minute, JPEG: []byte{3}},
	})
	a := strings.Index(ordered, "![板书 00:00]")
	b := strings.Index(ordered, "![板书 00:30]")
	c := strings.Index(ordered, "![板书 01:00]")
	note := strings.Index(ordered, "只有说明")
	tail := strings.Index(ordered, "## 要点")
	if note < 0 || a < 0 || b < 0 || c < 0 || tail < 0 || !(note < a && a < b && b < c && c < tail) {
		t.Fatalf("untimed frames reversed:\n%s", ordered)
	}
}

func TestFileCompanionFrameStaysOutsideFormula(t *testing.T) {
	md := "## 章节\n0:00–2:00 分段\n$$\n0:00 & t_0 \\\\\n2:00 & t_1\n$$\n12:05 下一句\n## 要点\n无\n"
	if got := fileCompanionOrganizedCaption(md, 30*time.Second); got != "分段" {
		t.Fatalf("caption = %q", got)
	}
	if got := fileCompanionOrganizedCaption(md, 150*time.Second); got != "分段" {
		t.Fatalf("mid caption = %q", got)
	}
	dir := t.TempDir()
	out := fileCompanionInsertNoteFramesInto(md, dir, []videoNoteFrame{
		{At: 30 * time.Second, JPEG: []byte{1}},
		{At: 13 * time.Minute, JPEG: []byte{2}},
	})
	chapter := strings.Index(out, "0:00–2:00 分段")
	early := strings.Index(out, "![板书 00:30]")
	formula := strings.Index(out, "0:00 & t_0")
	later := strings.Index(out, "12:05 下一句")
	tail := strings.Index(out, "![板书 13:00]")
	if chapter < 0 || early < 0 || formula < 0 || later < 0 || tail < 0 ||
		!(chapter < early && early < formula && formula < later && later < tail) {
		t.Fatalf("picture entered the formula:\n%s", out)
	}
	if !strings.Contains(out, "$$\n0:00 & t_0") || strings.Count(out, "分段") != 1 {
		t.Fatalf("formula split or caption repeated:\n%s", out)
	}
}

func TestFileCompanionFrameShowsOrganizedCaption(t *testing.T) {
	md := "## 概要\n讲过了\n## 章节\n时间不明 - 0:00–2:00 机器翻译的几个时代（推断）\n时间不明 - 3:00–4:00 Transformer 编码器（推断）\n## 要点\n00:30 一点\n"
	if got := fileCompanionOrganizedCaption(md, 90*time.Second); got != "机器翻译的几个时代（推断）" {
		t.Fatalf("early caption = %q", got)
	}
	if got := fileCompanionOrganizedCaption(md, 3*time.Minute+30*time.Second); got != "Transformer 编码器（推断）" {
		t.Fatalf("caption = %q", got)
	}
	dir := t.TempDir()
	frame := videoNoteFrame{At: 3*time.Minute + 30*time.Second, JPEG: []byte{1, 2, 3}, OCR: "Thank you!\n乱码一行"}
	out := fileCompanionInsertNoteFramesInto(md, dir, []videoNoteFrame{frame})
	if strings.Contains(out, "识别不确定") || strings.Contains(out, "Thank you") || strings.Contains(out, "乱码") {
		t.Fatalf("published the raw ocr: %s", out)
	}
	if !strings.Contains(out, "![板书 03:30]") || strings.Count(out, "Transformer 编码器（推断）") != 1 {
		t.Fatalf("caption missing or repeated: %s", out)
	}
	img := strings.Index(out, "![板书 03:30]")
	sentence := strings.Index(out, "3:00–4:00 Transformer 编码器（推断）")
	if sentence < 0 || img < sentence {
		t.Fatalf("picture left its chapter: %s", out)
	}
}

func TestFileCompanionVideoNoteFrames(t *testing.T) {
	app := newVideoNoteTestApp(t)
	video := writeMedia(t, app, "board.mp4", []byte("video"))
	media := &fakeVideoNoteMedia{located: true, probe: videoNoteProbeInfo{Duration: 50 * time.Second, HasAudio: true}}
	same := bytes.Repeat([]byte{20}, 256)
	other := bytes.Repeat([]byte{200}, 256)
	media.gray = func(at time.Duration) []byte {
		if at < 45*time.Second {
			return append([]byte(nil), same...)
		}
		return append([]byte(nil), other...)
	}
	jpegBytes := tinyNoteJPEG(t)
	media.jpeg = func(at time.Duration) []byte {
		return append([]byte(nil), jpegBytes...)
	}
	restore := useVideoNoteMedia(t, media)
	defer restore()
	prevASR := fileCompanionNoteASRHook
	fileCompanionNoteASRHook = func([]byte) (string, error) { return strings.Repeat("甲", 20), nil }
	t.Cleanup(func() { fileCompanionNoteASRHook = prevASR })
	var got fileCompanionNoteModelRequest
	prevVision := fileCompanionNoteVisionHook
	fileCompanionNoteVisionHook = func() bool { return true }
	t.Cleanup(func() { fileCompanionNoteVisionHook = prevVision })
	fileCompanionNoteModelHook = func(req fileCompanionNoteModelRequest) *IMAgentResponse {
		got = req
		return &IMAgentResponse{Text: "## 概要\n画面\n## 章节\n00:30 板书\n## 要点\n00:30 $x^2$\n## 金句\n00:30 「式子」\n## 不确定的地方\n没看见的式子\n"}
	}
	if err := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: video, Text: "生成笔记", RequestID: "vision-1"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.Msg.Text, "Use OCR") || strings.Contains(got.Msg.Text, "visionFirstInstruction") {
		t.Fatal("vision request injected the attachment instruction")
	}
	if len(got.Msg.Attachments) != 0 {
		t.Fatal("frames were copied onto message attachments")
	}
	if len(got.Frames) == 0 || len(media.jpegAt) == 0 {
		t.Fatalf("frames=%d jpeg=%v", len(got.Frames), media.jpegAt)
	}
	if len(media.jpegAt) != 2 || media.jpegAt[0] != 30*time.Second || media.jpegAt[1] != 45*time.Second {
		t.Fatalf("runs = %v, want 30s then 45s", media.jpegAt)
	}
	body := readNote(t, app, video)
	if strings.Contains(body, "![") {
		t.Fatalf("vision note embedded a frame: %s", body)
	}
	sessionID := fileCompanionSessionID(mustCanonical(t, video))
	if err := app.imHandler.memory.FlushUser("file-companion:" + sessionID); err != nil {
		t.Fatal(err)
	}
	side := filepath.Join(app.GetDataDir(), "file-companion", "sessions", sessionID+".json")
	raw, err := os.ReadFile(side)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("/9j/")) || bytes.Contains(raw, []byte("之前上传了")) {
		t.Fatal("session json stored a frame")
	}

	// More than 24 distinct runs still keeps the last one.
	media.jpegAt = nil
	media.gray = func(at time.Duration) []byte {
		// Every byte changes with the sample so the mean absolute difference
		// is the step itself and each sample starts a new run.
		return bytes.Repeat([]byte{byte(at / time.Second)}, 256)
	}
	media.probe.Duration = 15 * 30 * time.Second
	fileCompanionNoteVisionHook = func() bool { return false }
	fileCompanionNoteOCRStateHook = func() (bool, bool) { return true, true }
	t.Cleanup(func() { fileCompanionNoteOCRStateHook = nil })
	fileCompanionNoteOCRHook = func([]byte) (string, error) { return "E=mc", nil }
	t.Cleanup(func() { fileCompanionNoteOCRHook = nil })
	fileCompanionNoteModelHook = func(req fileCompanionNoteModelRequest) *IMAgentResponse {
		got = req
		return &IMAgentResponse{Text: "## 概要\n板\n## 章节\n00:01 开始\n## 要点\n00:01 一点\n## 金句\n00:01 「话」\n## 不确定的地方\n无\n"}
	}
	video2 := writeMedia(t, app, "many.mp4", []byte("video"))
	if err := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: video2, Text: "生成笔记", RequestID: "ocr-1"}); err != nil {
		t.Fatal(err)
	}
	if len(media.jpegAt) != 24 {
		t.Fatalf("kept %d frames", len(media.jpegAt))
	}
	last := media.probe.Duration - 15*time.Second
	// The sampler stops before duration, so the last candidate is the greatest at < duration.
	if media.jpegAt[len(media.jpegAt)-1] < last-time.Second {
		t.Fatalf("last kept = %s want near %s", media.jpegAt[len(media.jpegAt)-1], last)
	}
	ocrBody := readNote(t, app, video2)
	if strings.Contains(ocrBody, "识别不确定") || strings.Contains(ocrBody, "E=mc") {
		t.Fatalf("published the raw ocr: %s", ocrBody)
	}
	if !strings.Contains(got.Msg.Text, "识别不确定") || !strings.Contains(got.Msg.Text, "E=mc") {
		t.Fatal("model prompt dropped the ocr evidence")
	}
	if !strings.Contains(ocrBody, "一点") {
		t.Fatalf("note dropped the organized text: %s", ocrBody)
	}
	if !strings.Contains(ocrBody, "![板书 ") {
		t.Fatalf("missing image line: %s", ocrBody)
	}
	for _, line := range strings.Split(ocrBody, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "![") && !regexpImageLine(trim) {
			t.Fatalf("image line = %q", trim)
		}
	}

	fileCompanionNoteOCRStateHook = func() (bool, bool) { return false, false }
	media.jpegAt = nil
	media.probe.Duration = 20 * time.Second
	video3 := writeMedia(t, app, "blind.mp4", []byte("video"))
	if err := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: video3, Text: "生成笔记"}); err != nil {
		t.Fatal(err)
	}
	blind := readNote(t, app, video3)
	if !strings.Contains(blind, "画面未识别") || !strings.Contains(blind, "![板书 ") {
		t.Fatalf("blind = %s", blind)
	}

	media.grayCalls = 0
	audio := writeMedia(t, app, "talk.wav", tinyPCMWav(1))
	writeSidecar(t, audio, "1\n00:00:00,000 --> 00:00:01,000\n你好\n")
	if err := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: audio, Text: "生成笔记"}); err != nil {
		t.Fatal(err)
	}
	if media.grayCalls != 0 {
		t.Fatalf("audio extracted %d frames", media.grayCalls)
	}
}

func TestFileCompanionWebFramesThinOnce(t *testing.T) {
	frames := make([]videoNoteFrame, 40)
	for i := range frames {
		frames[i] = videoNoteFrame{
			At:   time.Duration(i) * time.Second,
			Gray: bytes.Repeat([]byte{byte(i * 20)}, 256),
			JPEG: []byte{byte(i + 1)},
		}
	}
	var kept []videoNoteFrame
	for _, frame := range frames {
		kept = fileCompanionKeepWebShot(kept, frame)
	}
	if len(kept) != len(frames) {
		t.Fatalf("collapsed distinct runs = %d", len(kept))
	}
	got := dedupVideoNoteFrames(kept)
	want := dedupVideoNoteFrames(frames)
	if len(got) != fileCompanionNoteMaxFrames || len(want) != fileCompanionNoteMaxFrames {
		t.Fatalf("thin = %d and %d", len(got), len(want))
	}
	for i := range want {
		if got[i].At != want[i].At || !bytes.Equal(got[i].JPEG, want[i].JPEG) {
			t.Fatalf("frame %d = %s jpeg %v, want %s jpeg %v", i, got[i].At, got[i].JPEG, want[i].At, want[i].JPEG)
		}
	}
	if got[len(got)-1].At != frames[len(frames)-1].At {
		t.Fatalf("last = %s", got[len(got)-1].At)
	}
	var early []videoNoteFrame
	for _, frame := range frames {
		early = append(early, frame)
		early = dedupVideoNoteFrames(early)
	}
	if len(early) != len(got) {
		t.Fatal("iterative thin changed the count")
	}
	same := true
	for i := range got {
		if early[i].At != got[i].At {
			same = false
			break
		}
	}
	if same {
		t.Fatal("thinning on every frame matched a single thin of the whole recording")
	}

	sameGray := bytes.Repeat([]byte{30}, 256)
	kept = nil
	var last time.Duration
	for i := 0; i < 30; i++ {
		last = time.Duration(i) * time.Second
		kept = fileCompanionKeepWebShot(kept, videoNoteFrame{At: last, Gray: append([]byte(nil), sameGray...), JPEG: []byte{byte(i + 1)}})
	}
	if len(kept) != 1 || kept[0].At != last || kept[0].JPEG[0] != 30 {
		t.Fatalf("similar run = %+v", kept)
	}
}

func TestFileCompanionVideoNoteSilentPictureUsesFrames(t *testing.T) {
	app := newVideoNoteTestApp(t)
	video := writeMedia(t, app, "silent.mp4", []byte("video"))
	media := &fakeVideoNoteMedia{located: true, probe: videoNoteProbeInfo{Duration: 20 * time.Second, HasAudio: false, HasText: false}}
	media.gray = func(time.Duration) []byte { return bytes.Repeat([]byte{40}, 256) }
	jpegBytes := tinyNoteJPEG(t)
	media.jpeg = func(time.Duration) []byte { return append([]byte(nil), jpegBytes...) }
	defer useVideoNoteMedia(t, media)()
	fileCompanionNoteVisionHook = func() bool { return true }
	t.Cleanup(func() { fileCompanionNoteVisionHook = nil })
	fileCompanionNoteModelHook = func(req fileCompanionNoteModelRequest) *IMAgentResponse {
		if len(req.Frames) == 0 {
			t.Errorf("silent video collected no frames")
		}
		return &IMAgentResponse{Text: "## 概要\n板书\n## 章节\n00:00 开始\n## 要点\n00:00 一点\n## 金句\n00:00 「话」\n## 不确定的地方\n无\n"}
	}
	if err := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: video, Text: "生成笔记", RequestID: "silent-pic"}); err != nil {
		t.Fatal(err)
	}
	body := readNote(t, app, video)
	if strings.Contains(body, fileCompanionErrNoTrack) || strings.Contains(body, fileCompanionErrNoSpeech) {
		t.Fatalf("silent picture = %s", body)
	}
	if media.grayCalls == 0 {
		t.Fatal("silent video did not collect frames")
	}

	media.grayCalls = 0
	media.gray = func(time.Duration) []byte { return nil }
	empty := writeMedia(t, app, "empty-picture.mp4", []byte("video"))
	err := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: empty, Text: "生成笔记", RequestID: "silent-empty"})
	if err == nil || err.Error() != fileCompanionErrNoTrack {
		t.Fatalf("empty frames = %v", err)
	}
	if media.grayCalls == 0 {
		t.Fatal("empty picture returned before frame collection")
	}
}

type fakeVideoNoteMedia struct {
	mu         sync.Mutex
	installs   int
	located    bool
	installErr error
	installFn  func(context.Context) error
	probe      videoNoteProbeInfo
	gray       func(time.Duration) []byte
	jpeg       func(time.Duration) []byte
	jpegAt     []time.Duration
	grayCalls  int
	extracts   int
}

func (f *fakeVideoNoteMedia) Locate() (string, string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.located {
		return "ffmpeg", "ffprobe", true
	}
	return "", "", false
}

func (f *fakeVideoNoteMedia) Install(ctx context.Context) (string, string, error) {
	f.mu.Lock()
	f.installs++
	fn := f.installFn
	err := f.installErr
	f.mu.Unlock()
	if fn != nil {
		if waitErr := fn(ctx); waitErr != nil {
			return "", "", waitErr
		}
		f.mu.Lock()
		f.located = true
		f.mu.Unlock()
		return "ffmpeg", "ffprobe", nil
	}
	if err != nil {
		return "", "", err
	}
	f.mu.Lock()
	f.located = true
	f.mu.Unlock()
	return "ffmpeg", "ffprobe", nil
}

func (f *fakeVideoNoteMedia) Probe(context.Context, string) (videoNoteProbeInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.probe, nil
}

func (f *fakeVideoNoteMedia) ExtractWAV(context.Context, string, time.Duration, int, string) error {
	f.mu.Lock()
	f.extracts++
	f.mu.Unlock()
	return os.ErrNotExist
}

func (f *fakeVideoNoteMedia) ExtractSubs(context.Context, string, string) error {
	return os.ErrNotExist
}

func (f *fakeVideoNoteMedia) ExtractGray(_ context.Context, _ string, at time.Duration) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grayCalls++
	if f.gray == nil {
		return nil, os.ErrNotExist
	}
	return f.gray(at), nil
}

func (f *fakeVideoNoteMedia) ExtractJPEG(_ context.Context, _ string, at time.Duration) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jpegAt = append(f.jpegAt, at)
	if f.jpeg == nil {
		return []byte{0xff, 0xd8, 0xff, 0xd9}, nil
	}
	return f.jpeg(at), nil
}

func useVideoNoteMedia(t *testing.T, media videoNoteMedia) func() {
	t.Helper()
	prev := fileCompanionVideoNoteMediaHook
	fileCompanionVideoNoteMediaHook = func() videoNoteMedia { return media }
	return func() { fileCompanionVideoNoteMediaHook = prev }
}

func newVideoNoteTestApp(t *testing.T) *App {
	t.Helper()
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	st := app.companionRuntime()
	st.mu.Lock()
	st.recordEvents = true
	st.mu.Unlock()
	prev := fileCompanionNoteContextTokensHook
	fileCompanionNoteContextTokensHook = func() int { return 128000 }
	t.Cleanup(func() {
		fileCompanionNoteContextTokensHook = prev
		fileCompanionNoteModelHook = nil
		fileCompanionTurnModelHook = nil
		fileCompanionVideoNoteMediaHook = nil
	})
	return app
}

func setNoteModel(t *testing.T, text string) {
	t.Helper()
	fileCompanionNoteModelHook = func(fileCompanionNoteModelRequest) *IMAgentResponse {
		return &IMAgentResponse{Text: text}
	}
}

func writeMedia(t *testing.T, app *App, name string, body []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeSidecar(t *testing.T, media, body string) {
	t.Helper()
	path := strings.TrimSuffix(media, filepath.Ext(media)) + ".srt"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func tinyNoteJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 8, 8))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 40}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func tinyPCMWav(seconds int) []byte {
	if seconds < 1 {
		seconds = 1
	}
	pcm := make([]byte, 16000*2*seconds)
	for i := 0; i < len(pcm); i += 2 {
		binary.LittleEndian.PutUint16(pcm[i:], 20000)
	}
	return wavWrap(1, 16000, 1, 16, pcm)
}

func writePCMWav(t *testing.T, app *App, name string, rate, dataLen int) string {
	t.Helper()
	if dataLen%2 == 1 {
		dataLen++
	}
	pcm := make([]byte, dataLen)
	for i := 0; i < len(pcm); i += 2 {
		binary.LittleEndian.PutUint16(pcm[i:], 1000)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, wavWrap(1, rate, 1, 16, pcm), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func noteFiles(t *testing.T, app *App, media string) string {
	t.Helper()
	canonical := mustCanonical(t, media)
	return app.fileCompanionFindNoteMarkdown(fileCompanionSessionID(canonical))
}

func notePDF(t *testing.T, app *App, media string) string {
	t.Helper()
	md := noteFiles(t, app, media)
	return strings.TrimSuffix(md, ".md") + ".pdf"
}

func readNote(t *testing.T, app *App, media string) string {
	t.Helper()
	body, err := os.ReadFile(noteFiles(t, app, media))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func mustCanonical(t *testing.T, path string) string {
	t.Helper()
	canonical, err := fileCompanionCanonicalPath(path)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func mediaModTime(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime()
}

func assertNoteEvent(t *testing.T, app *App, requestID, body string) {
	t.Helper()
	st := app.companionRuntime()
	st.mu.Lock()
	events := append([]recordedCompanionEvent(nil), st.events...)
	st.mu.Unlock()
	for _, ev := range events {
		if ev.Name != "ai-assistant-response" || !strings.Contains(ev.Data, requestID) {
			continue
		}
		var resp IMAgentResponse
		if err := json.Unmarshal([]byte(ev.Data), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.LocalFilePath != "" && strings.Contains(resp.Text, "已生成论文解读") {
			t.Fatal("note event looked like a paper card")
		}
		if strings.Contains(resp.Text, "已生成论文解读") {
			t.Fatal("bubble was replaced with the paper sentence")
		}
		if !strings.Contains(resp.Text, "字幕内容") && !strings.Contains(body, "字幕内容") {
			t.Fatalf("event text = %s", resp.Text)
		}
		if resp.NoteCardPath == "" {
			t.Fatal("event omitted the pdf card")
		}
		return
	}
	t.Fatal("note event was not recorded")
}

func recordedProgress(app *App) string {
	st := app.companionRuntime()
	st.mu.Lock()
	defer st.mu.Unlock()
	var b strings.Builder
	for _, ev := range st.events {
		if ev.Name == "ai-assistant-progress" {
			b.WriteString(ev.Data)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func progressLine(all string) string {
	for _, line := range strings.Split(all, "\n") {
		if strings.Contains(line, "正在安装视频解码器") {
			return line
		}
	}
	return strings.TrimSpace(all)
}

func regexpImageLine(line string) bool {
	return strings.HasPrefix(line, "![") && strings.Contains(line, "](") && strings.HasSuffix(line, ")")
}

func appConfigASREnabled() corelib.AppConfig {
	return corelib.AppConfig{ASREnabled: true}
}

func TestFileCompanionNoteASRTimeoutDoesNotOverlap(t *testing.T) {
	app := newVideoNoteTestApp(t)
	var inflight atomic.Int32
	var maxInflight atomic.Int32
	var calls atomic.Int32
	entered := make(chan int, 2)
	release := make(chan struct{})
	prev := fileCompanionNoteASRHook
	fileCompanionNoteASRHook = func([]byte) (string, error) {
		n := inflight.Add(1)
		for {
			old := maxInflight.Load()
			if n <= old || maxInflight.CompareAndSwap(old, n) {
				break
			}
		}
		id := int(calls.Add(1))
		entered <- id
		<-release
		inflight.Add(-1)
		return "有字", nil
	}
	t.Cleanup(func() {
		fileCompanionNoteASRHook = prev
		select {
		case <-release:
		default:
			close(release)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		_, err := app.fileCompanionTranscribeWAV(ctx, tinyPCMWav(1), time.Second)
		errCh <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first transcription did not start")
	}
	secondEntered := make(chan struct{})
	go func() {
		_, _ = app.fileCompanionTranscribeWAV(context.Background(), tinyPCMWav(1), time.Second)
		close(secondEntered)
	}()
	select {
	case id := <-entered:
		t.Fatalf("second transcription overlapped the first, id=%d inflight=%d", id, maxInflight.Load())
	case <-time.After(400 * time.Millisecond):
	}
	if got := maxInflight.Load(); got != 1 {
		t.Fatalf("max inflight = %d", got)
	}
	close(release)
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("first transcription = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first transcription did not finish")
	}
	select {
	case <-secondEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("second transcription did not start after the first released the model")
	}
	if got := maxInflight.Load(); got != 1 {
		t.Fatalf("max inflight after release = %d", got)
	}
}

func TestFileCompanionCancelledWriteKeepsPreviousNote(t *testing.T) {
	app := newVideoNoteTestApp(t)
	video := writeMedia(t, app, "keep.mp4", []byte("video"))
	canonical := mustCanonical(t, video)
	sessionID := fileCompanionSessionID(canonical)
	userID := "file-companion:" + sessionID
	mdPath, pdfPath, err := fileCompanionNotePaths(app.GetDataDir(), sessionID, canonical)
	if err != nil {
		t.Fatal(err)
	}
	frameDir := filepath.Join(filepath.Dir(mdPath), "frames")
	framePath := filepath.Join(frameDir, "0.jpg")
	oldMD := []byte("上一份笔记\n")
	oldPDF := []byte("%PDF-1.4 old-note")
	oldFrame := []byte("OLD-FRAME")
	if err := os.MkdirAll(frameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mdPath, oldMD, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pdfPath, oldPDF, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(framePath, oldFrame, 0o644); err != nil {
		t.Fatal(err)
	}
	pointer := filepath.Join(filepath.Dir(mdPath), "latest-video-note.txt")
	if err := os.WriteFile(pointer, []byte(pdfPath), 0o644); err != nil {
		t.Fatal(err)
	}
	clearGen := app.fileCompanionCaptureChatGen(userID)
	noteText := "## 概要\n新\n## 章节\n00:01 板书\n## 要点\n00:01 一点\n## 金句\n00:01 「话」\n## 不确定的地方\n无\n"
	embed := []videoNoteFrame{{At: time.Second, JPEG: tinyNoteJPEG(t)}}
	t.Cleanup(func() { fileCompanionNotePublishHook = nil })
	assertKept := func(t *testing.T) {
		t.Helper()
		gotMD, err := os.ReadFile(mdPath)
		if err != nil {
			t.Fatal(err)
		}
		gotPDF, err := os.ReadFile(pdfPath)
		if err != nil {
			t.Fatal(err)
		}
		gotFrame, err := os.ReadFile(framePath)
		if err != nil {
			t.Fatal(err)
		}
		gotPointer, err := os.ReadFile(pointer)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(gotMD, oldMD) || !bytes.Equal(gotPDF, oldPDF) || !bytes.Equal(gotFrame, oldFrame) || string(gotPointer) != pdfPath {
			t.Fatalf("previous note changed md=%q pdf=%q frame=%q pointer=%q", gotMD, gotPDF, gotFrame, gotPointer)
		}
		for _, name := range []string{"frames.next", "frames.old"} {
			if _, statErr := os.Stat(filepath.Join(filepath.Dir(mdPath), name)); !os.IsNotExist(statErr) {
				t.Fatalf("%s left behind: %v", name, statErr)
			}
		}
		for _, path := range []string{mdPath + ".tmp", pdfPath + ".tmp", mdPath + ".bak", pdfPath + ".bak"} {
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Fatalf("%s left behind: %v", path, statErr)
			}
		}
	}
	write := func(ctx context.Context, gen uint64) error {
		_, err := fileCompanionWriteNoteFiles(app, canonical, sessionID, userID, gen, "视频笔记", noteText, embed, ctx)
		return err
	}

	t.Run("already cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := write(ctx, clearGen)
		if err == nil || err.Error() != fileCompanionErrNoteCancelled {
			t.Fatalf("err = %v", err)
		}
		assertKept(t)
	})

	t.Run("cancelled after pdf", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		fileCompanionNotePublishHook = func() { cancel() }
		defer func() { fileCompanionNotePublishHook = nil }()
		err := write(ctx, clearGen)
		if err == nil || err.Error() != fileCompanionErrNoteCancelled {
			t.Fatalf("err = %v", err)
		}
		assertKept(t)
	})

	t.Run("cleared after pdf", func(t *testing.T) {
		fileCompanionNotePublishHook = func() {
			if err := app.fileCompanionDropChat(userID, ""); err != nil {
				t.Errorf("clear = %v", err)
			}
		}
		defer func() { fileCompanionNotePublishHook = nil }()
		err := write(context.Background(), clearGen)
		if err == nil || err.Error() != fileCompanionErrNoteCancelled {
			t.Fatalf("err = %v", err)
		}
		assertKept(t)
	})

	t.Run("commit rewrites frame path", func(t *testing.T) {
		fileCompanionNotePublishHook = nil
		clearGen = app.fileCompanionCaptureChatGen(userID)
		pdf, err := fileCompanionWriteNoteFiles(app, canonical, sessionID, userID, clearGen, "视频笔记", noteText, embed, context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if pdf != pdfPath {
			t.Fatalf("pdf = %s", pdf)
		}
		body, err := os.ReadFile(mdPath)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(body, oldMD) || strings.Contains(string(body), "frames.next") || strings.Contains(string(body), fileCompanionFrameDirMark) {
			t.Fatalf("published markdown = %s", body)
		}
		var imagePath string
		for _, line := range strings.Split(string(body), "\n") {
			trim := strings.TrimSpace(line)
			if !strings.HasPrefix(trim, "![") {
				continue
			}
			if !regexpImageLine(trim) {
				t.Fatalf("image line = %q", trim)
			}
			open := strings.LastIndex(trim, "](")
			imagePath = trim[open+2 : len(trim)-1]
		}
		if imagePath == "" {
			t.Fatal("published note has no frame")
		}
		wantDir := frameDir
		if abs, absErr := filepath.Abs(wantDir); absErr == nil {
			wantDir = abs
		}
		wantFrame := wantDir + string(filepath.Separator) + "1.jpg"
		if imagePath != wantFrame {
			t.Fatalf("image path = %s want %s", imagePath, wantFrame)
		}
		if _, err := os.Stat(imagePath); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(framePath); !os.IsNotExist(err) {
			t.Fatalf("old frame still present: %v", err)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(mdPath), "frames.next")); !os.IsNotExist(err) {
			t.Fatalf("frames.next = %v", err)
		}
		gotPDF, err := os.ReadFile(pdfPath)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(gotPDF, oldPDF) || len(gotPDF) == 0 {
			t.Fatal("pdf was not replaced")
		}
		gotPointer, err := os.ReadFile(pointer)
		if err != nil || string(gotPointer) != pdfPath {
			t.Fatalf("pointer = %q err=%v", gotPointer, err)
		}
	})
}

func TestFileCompanionNoteASRSkipsWhenBudgetAlreadySpent(t *testing.T) {
	app := newVideoNoteTestApp(t)
	var calls atomic.Int32
	prev := fileCompanionNoteASRHook
	fileCompanionNoteASRHook = func([]byte) (string, error) {
		calls.Add(1)
		return "有字", nil
	}
	t.Cleanup(func() { fileCompanionNoteASRHook = prev })
	short, cancelShort := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancelShort()
	_, err := app.fileCompanionTranscribeWAV(short, tinyPCMWav(1), time.Minute)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("fresh budget = %v", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("asr started with under a second left, calls=%d", got)
	}

	release := make(chan struct{})
	fileCompanionNoteASRHook = func([]byte) (string, error) {
		calls.Add(1)
		<-release
		return "有字", nil
	}
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	errCh := make(chan error, 1)
	go func() {
		_, callErr := app.fileCompanionTranscribeWAV(context.Background(), tinyPCMWav(1), time.Second)
		errCh <- callErr
	}()
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("first transcription did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	wait, cancelWait := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancelWait()
	_, err = app.fileCompanionTranscribeWAV(wait, tinyPCMWav(1), time.Minute)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting budget = %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls after the budget expired = %d", got)
	}
	select {
	case <-release:
	default:
		close(release)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("first transcription = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first transcription did not finish")
	}
}

func TestFileCompanionWorkLimitMarksLaterSegments(t *testing.T) {
	app := newVideoNoteTestApp(t)
	prevLimit := fileCompanionWorkLimit
	fileCompanionWorkLimit = 32
	t.Cleanup(func() { fileCompanionWorkLimit = prevLimit })
	work := filepath.Join(app.GetDataDir(), "file-companion", "work", "cap")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	prevASR := fileCompanionNoteASRHook
	calls := 0
	fileCompanionNoteASRHook = func([]byte) (string, error) {
		calls++
		if err := os.WriteFile(filepath.Join(work, "pad.bin"), bytes.Repeat([]byte{'x'}, 64), 0o644); err != nil {
			return "", err
		}
		return "有字", nil
	}
	t.Cleanup(func() { fileCompanionNoteASRHook = prevASR })
	text, _, err := app.fileCompanionTranscribePieces(context.Background(), work, [][]byte{tinyPCMWav(1), tinyPCMWav(1)}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("asr calls = %d", calls)
	}
	if !strings.Contains(text, "有字") || !strings.Contains(text, fileCompanionErrSegmentUnread) {
		t.Fatalf("text = %q", text)
	}
}

func TestFileCompanionWorkLimitSkipsExtract(t *testing.T) {
	app := newVideoNoteTestApp(t)
	prevLimit := fileCompanionWorkLimit
	fileCompanionWorkLimit = 8
	t.Cleanup(func() { fileCompanionWorkLimit = prevLimit })
	work := filepath.Join(app.GetDataDir(), "file-companion", "work", "extract-cap")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "pad.bin"), bytes.Repeat([]byte{'x'}, 32), 0o644); err != nil {
		t.Fatal(err)
	}
	media := &fakeVideoNoteMedia{located: true}
	text, err := app.fileCompanionTranscribeFFmpeg(context.Background(), media, "clip.mp4", work, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if media.extracts != 0 {
		t.Fatalf("extracts = %d", media.extracts)
	}
	if text != "" {
		t.Fatalf("text = %q", text)
	}
}

func TestFileCompanionSegmentTimeoutUsesRemainingBudget(t *testing.T) {
	if got := fileCompanionSegmentTimeout(5*time.Second, time.Minute); got != 5*time.Second {
		t.Fatalf("remain = %s", got)
	}
	if got := fileCompanionSegmentTimeout(time.Hour, 10*time.Second); got != 30*time.Second {
		t.Fatalf("floor = %s", got)
	}
	if got := fileCompanionSegmentTimeout(time.Hour, time.Minute); got != time.Minute {
		t.Fatalf("segment = %s", got)
	}
	if got := fileCompanionSegmentTimeout(0, time.Minute); got != 0 {
		t.Fatalf("empty = %s", got)
	}
}

func TestFileCompanionSidecarRejectsAnotherDirectory(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "clip.mp4")
	side := filepath.Join(dir, "clip.srt")
	body := "1\n00:00:00,000 --> 00:00:01,000\nhello\n"
	if err := os.WriteFile(media, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(side, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if !fileCompanionSidecarSameDir(media, side) {
		t.Fatal("same directory rejected")
	}
	if _, _, _, _, ok := fileCompanionLoadSidecar(media); !ok {
		t.Fatal("same-directory sidecar was ignored")
	}
	other := filepath.Join(t.TempDir(), "clip.srt")
	if err := os.WriteFile(other, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if fileCompanionSidecarSameDir(media, other) {
		t.Fatal("other directory accepted")
	}
	if err := os.Remove(side); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, side); err != nil {
		t.Logf("symlink not available: %v", err)
		return
	}
	if fileCompanionSidecarSameDir(media, side) {
		t.Fatal("symlink escaped the media directory")
	}
	if _, _, _, _, ok := fileCompanionLoadSidecar(media); ok {
		t.Fatal("load accepted a sidecar outside the media directory")
	}
}

func TestFileCompanionNoteModelWaitsPastSlotBudget(t *testing.T) {
	resetDetachedSimpleLLMReadsForTest(t)
	prevBudget := fileCompanionNoteModelSlotBudget
	fileCompanionNoteModelSlotBudget = 40 * time.Millisecond
	t.Cleanup(func() { fileCompanionNoteModelSlotBudget = prevBudget })
	prevCfg := fileCompanionNoteLLMConfigHook
	t.Cleanup(func() { fileCompanionNoteLLMConfigHook = prevCfg })
	prevModel := fileCompanionNoteModelHook
	fileCompanionNoteModelHook = nil
	t.Cleanup(func() { fileCompanionNoteModelHook = prevModel })

	var hits atomic.Int32
	srv := slowSimpleLLMServer(150*time.Millisecond, &hits, detachedTestPayload)
	defer srv.Close()
	fileCompanionNoteLLMConfigHook = func() corelib.MaclawLLMConfig {
		return corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	text, err := (&App{}).fileCompanionAskNoteModel(ctx, IMUserMessage{Text: "整理笔记"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if text != "adopted-content" {
		t.Fatalf("text = %q", text)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("upstream hits = %d", got)
	}
}

func TestFileCompanionNoteModelCancelStopsTheWait(t *testing.T) {
	resetDetachedSimpleLLMReadsForTest(t)
	prevBudget := fileCompanionNoteModelSlotBudget
	fileCompanionNoteModelSlotBudget = 30 * time.Millisecond
	t.Cleanup(func() { fileCompanionNoteModelSlotBudget = prevBudget })
	prevCfg := fileCompanionNoteLLMConfigHook
	t.Cleanup(func() { fileCompanionNoteLLMConfigHook = prevCfg })
	prevModel := fileCompanionNoteModelHook
	fileCompanionNoteModelHook = nil
	t.Cleanup(func() { fileCompanionNoteModelHook = prevModel })

	var hits atomic.Int32
	srv := slowSimpleLLMServer(5*time.Second, &hits, detachedTestPayload)
	defer srv.Close()
	fileCompanionNoteLLMConfigHook = func() corelib.MaclawLLMConfig {
		return corelib.MaclawLLMConfig{URL: srv.URL, Model: "test-model"}
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(40*time.Millisecond, cancel)
	start := time.Now()
	_, err := (&App{}).fileCompanionAskNoteModel(ctx, IMUserMessage{Text: "整理笔记"}, nil)
	if err == nil {
		t.Fatal("cancelled note model returned a reply")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("cancel took %s", time.Since(start))
	}
}
