package guiapp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestFileCompanionYouTubeFrameIsEmbed(t *testing.T) {
	app := newVideoNoteTestApp(t)
	cases := []struct {
		in, want string
	}{
		{"https://www.youtube.com/watch?v=abc123&t=12", "https://www.youtube.com/embed/abc123"},
		{"https://youtu.be/abc123?t=3", "https://www.youtube.com/embed/abc123"},
		{"https://m.youtube.com/watch?v=abc123", "https://www.youtube.com/embed/abc123"},
		{"https://www.youtube.com/shorts/abc123", "https://www.youtube.com/embed/abc123"},
		{"https://www.youtube.com/live/abc123", "https://www.youtube.com/embed/abc123"},
		{"https://www.youtube.com/embed/abc123?t=9", "https://www.youtube.com/embed/abc123"},
		{"https://www.youtube-nocookie.com/embed/abc123", "https://www.youtube-nocookie.com/embed/abc123"},
		{"https://www.youtube.com/embed/videoseries?list=PLrAXtmErZgOeiKm4sgNOknGvNjby9ef3i", "https://www.youtube.com/embed/videoseries?list=PLrAXtmErZgOeiKm4sgNOknGvNjby9ef3i"},
		{"https://www.youtube.com/embed/live_stream?channel=UC_x5XG1OV2P6uZZ5FSM9Ttw", "https://www.youtube.com/embed/live_stream?channel=UC_x5XG1OV2P6uZZ5FSM9Ttw"},
		{"https://www.youtube.com/watch?v=keep-chat&list=PL1&t=1", "https://www.youtube.com/embed/keep-chat"},
		{"https://example.com/watch?v=abc123", "https://example.com/watch?v=abc123"},
		{"https://www.youtube.com/", "https://www.youtube.com/"},
	}
	var shared string
	for _, tc := range cases {
		opened, err := app.FileCompanionOpenWebPage(tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.in, err)
		}
		if opened.FrameURL != tc.want || opened.URL != tc.want {
			t.Fatalf("%s -> %q", tc.in, opened.FrameURL)
		}
		if strings.Contains(opened.FrameURL, "?t=") || strings.Contains(opened.FrameURL, "&t=") {
			t.Fatalf("frame kept a preview token: %s", opened.FrameURL)
		}
		if tc.want == "https://www.youtube.com/embed/abc123" {
			if shared == "" {
				shared = opened.SessionID
			} else if opened.SessionID != shared {
				t.Fatalf("%s session %s, want %s", tc.in, opened.SessionID, shared)
			}
		}
	}
}

func TestFileCompanionOpenReferenceSplitsArxivAndWeb(t *testing.T) {
	app := newVideoNoteTestApp(t)
	prev := arxivPDFBaseURL
	arxivPDFBaseURL = "http://127.0.0.1:1/pdf/"
	t.Cleanup(func() { arxivPDFBaseURL = prev })

	youtube, err := app.FileCompanionOpenReference("https://www.youtube.com/watch?v=abc123&t=12")
	if err != nil {
		t.Fatal(err)
	}
	if youtube.Kind != "web" || youtube.Path != "" {
		t.Fatalf("youtube = %+v", youtube)
	}
	if strings.Contains(youtube.FrameURL, "t=") || strings.Contains(youtube.URL, "t=") {
		t.Fatalf("frame kept a preview token: %s", youtube.FrameURL)
	}
	if _, err := os.Stat(youtube.FrameURL); err == nil {
		t.Fatal("youtube url was treated as a local file")
	}

	short, err := app.FileCompanionOpenWebPage("https://youtu.be/abc123")
	if err != nil {
		t.Fatal(err)
	}
	if short.Kind != "web" || short.SessionID == "" {
		t.Fatalf("youtu.be = %+v", short)
	}

	fileErr := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: youtube.FrameURL, Text: "生成笔记"})
	if fileErr == nil {
		t.Fatal("opening a page path sent a local-file turn")
	}

	local, err := app.FileCompanionOpenReference("file:///C:/videos/a.mp4")
	if err == nil || local.Error != fileCompanionErrOpenFileScheme {
		t.Fatalf("file scheme = %+v %v", local, err)
	}
	self, err := app.FileCompanionOpenReference("http://wails.localhost/index.html")
	if err == nil || self.Error != fileCompanionErrSelfPage {
		t.Fatalf("self page = %+v %v", self, err)
	}
	wails, err := app.FileCompanionOpenWebPage("wails://wails/index.html")
	if err == nil || wails.Error != fileCompanionErrSelfPage {
		t.Fatalf("wails = %+v %v", wails, err)
	}

	body := []byte("%PDF-1.7 fake paper body")
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	arxivPDFBaseURL = srv.URL + "/pdf/"
	dir := t.TempDir()
	prevDir := arxivPaperDirHook
	arxivPaperDirHook = func() (string, error) { return dir, nil }
	t.Cleanup(func() { arxivPaperDirHook = prevDir })

	before := hits
	if _, err := app.FileCompanionOpenWebPage("https://youtu.be/abc123"); err != nil {
		t.Fatal(err)
	}
	if hits != before {
		t.Fatal("youtu.be started an arXiv download")
	}
	pageOnly, pageErr := app.FileCompanionOpenWebPage("2510.16079")
	if pageErr == nil || pageOnly.Path != "" {
		t.Fatalf("web page downloaded an arXiv id: %+v %v", pageOnly, pageErr)
	}
	if hits != before {
		t.Fatal("OpenWebPage downloaded a paper")
	}
	paper, err := app.FileCompanionOpenReference("2510.16079")
	if err != nil || paper.Kind != "arxiv" || !strings.HasSuffix(paper.Path, "2510.16079.pdf") {
		t.Fatalf("arxiv = %+v %v", paper, err)
	}
	absLink, err := app.FileCompanionOpenReference("https://arxiv.org/abs/2510.16079")
	if err != nil || absLink.Kind != "arxiv" || absLink.Path == "" {
		t.Fatalf("abs link = %+v %v", absLink, err)
	}
	if hits == before {
		t.Fatal("arxiv id did not download")
	}
}

func TestFileCompanionWebNoteUsesFakeCapture(t *testing.T) {
	app := newVideoNoteTestApp(t)
	opened, err := app.FileCompanionOpenWebPage("https://www.youtube.com/watch?v=lecture")
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(app.GetDataDir(), "file-companion", "work", opened.SessionID)
	if err := app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note", RequestID: "web-off"}); err == nil || !strings.Contains(err.Error(), fileCompanionErrWebPlayerHidden) {
		t.Fatalf("missing player rect = %v", err)
	}
	if _, statErr := os.Stat(work); !os.IsNotExist(statErr) {
		t.Fatal("a note without a player rectangle created a recording")
	}

	release := make(chan struct{})
	started := make(chan struct{})
	prevStopASR := fileCompanionNoteASRHook
	fileCompanionNoteASRHook = func([]byte) (string, error) { return "一句", nil }
	t.Cleanup(func() { fileCompanionNoteASRHook = prevStopASR })
	fileCompanionNoteModelHook = func(fileCompanionNoteModelRequest) *IMAgentResponse {
		return &IMAgentResponse{Text: "## 概要\n停止后写入\n## 章节\n00:00 开始\n## 要点\n00:00 一点\n## 金句\n00:00 「话」\n## 不确定的地方\n无\n"}
	}
	fileCompanionWebCaptureHook = func() ([]webNoteShot, []byte, bool) {
		close(started)
		<-release
		return nil, tinyPCMWav(1), false
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note", RequestID: "web-stop", RectW: 960, RectH: 540})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("capture did not start")
	}
	if _, statErr := os.Stat(work); statErr != nil {
		t.Fatalf("recording missing while capturing: %v", statErr)
	}
	if err := app.CancelFileCompanionWebTurn(opened.SessionID, 0); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("stop = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not finish the note")
	}
	stopped := readWebNote(t, app, opened.SessionID)
	if !strings.Contains(stopped, "停止后写入") || strings.Contains(stopped, fileCompanionErrNoteCancelled) {
		t.Fatalf("stop note = %s", stopped)
	}
	if _, statErr := os.Stat(work); !os.IsNotExist(statErr) {
		t.Fatal("stop left the recording")
	}

	var posts int
	prevTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "asr/transcribe") {
			posts++
		}
		return nil, os.ErrClosed
	})
	t.Cleanup(func() { http.DefaultTransport = prevTransport })
	prevASR := fileCompanionNoteASRHook
	fileCompanionNoteASRHook = func([]byte) (string, error) { return "网页里说了一句", nil }
	t.Cleanup(func() { fileCompanionNoteASRHook = prevASR })
	fileCompanionNoteModelHook = func(req fileCompanionNoteModelRequest) *IMAgentResponse {
		if !strings.Contains(req.Msg.Text, "网页里说了一句") {
			t.Errorf("prompt missed the transcript")
		}
		return &IMAgentResponse{Text: "## 概要\n网页里说了一句\n## 章节\n00:03 开始\n## 要点\n00:03 一点\n## 金句\n00:03 「话」\n## 不确定的地方\n无\n"}
	}
	fileCompanionWebCaptureHook = func() ([]webNoteShot, []byte, bool) {
		black := bytesRepeat(2, 256)
		return []webNoteShot{{At: 0, Gray: black}, {At: 3 * time.Second, Gray: black}}, tinyPCMWav(1), false
	}
	t.Cleanup(func() { fileCompanionWebCaptureHook = nil })
	if err := app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note", RequestID: "web-black", RectW: 960, RectH: 540}); err != nil {
		t.Fatal(err)
	}
	body := readWebNote(t, app, opened.SessionID)
	if !strings.Contains(body, "画面无法采集") || !strings.Contains(body, "网页里说了一句") {
		t.Fatalf("black note = %s", body)
	}
	if _, statErr := os.Stat(work); !os.IsNotExist(statErr) {
		t.Fatal("recording remained after the note")
	}
	if posts != 0 {
		t.Fatalf("remote ASR posts = %d", posts)
	}
	fileCompanionWebCaptureHook = func() ([]webNoteShot, []byte, bool) {
		return []webNoteShot{{At: 0, Gray: bytesRepeat(2, 256)}}, nil, true
	}
	fileCompanionNoteModelHook = func(fileCompanionNoteModelRequest) *IMAgentResponse {
		return &IMAgentResponse{Text: "## 概要\n无声\n## 章节\n00:00 开始\n## 要点\n00:00 一点\n## 金句\n00:00 「话」\n## 不确定的地方\n无\n"}
	}
	if err := app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note", RequestID: "web-silent", RectW: 960, RectH: 540}); err != nil {
		t.Fatal(err)
	}
	silent := readWebNote(t, app, opened.SessionID)
	if !strings.Contains(silent, fileCompanionErrNoAudio) || !strings.Contains(silent, fileCompanionErrBlackFrame) {
		t.Fatalf("silent note = %s", silent)
	}
	if _, statErr := os.Stat(work); !os.IsNotExist(statErr) {
		t.Fatal("silent note left the recording")
	}
	act := companionProcessAudioActivate(uint32(os.Getpid()))
	if act.TargetPID != uint32(os.Getpid()) || !act.IncludeProcessTree {
		t.Fatalf("activation = %+v", act)
	}
	if act.StreamFlags&audclntStreamFlagsLoopback != 0 {
		t.Fatal("process loopback set the machine-wide flag")
	}
	if app.FileCompanionWebViewContainsFullscreen() {
		t.Fatal("unbound webview reported fullscreen")
	}
}

func TestFileCompanionWebSilentPCMIsNoAudio(t *testing.T) {
	app := newVideoNoteTestApp(t)
	opened, err := app.FileCompanionOpenWebPage("https://www.youtube.com/watch?v=silentpcm")
	if err != nil {
		t.Fatal(err)
	}
	prevASR := fileCompanionNoteASRHook
	fileCompanionNoteASRHook = func([]byte) (string, error) {
		t.Errorf("silence was sent to ASR")
		return "不该出现", nil
	}
	t.Cleanup(func() { fileCompanionNoteASRHook = prevASR })
	fileCompanionNoteModelHook = func(fileCompanionNoteModelRequest) *IMAgentResponse {
		return &IMAgentResponse{Text: "## 概要\n只有画面\n## 章节\n5:01 黑板\n## 要点\n5:01 一点\n## 金句\n05:01 「话」\n## 不确定的地方\n无\n"}
	}
	t.Cleanup(func() { fileCompanionNoteModelHook = nil })
	silent := pcm16MonoWAV(make([]byte, 16000*2), 16000)
	if !wavPCMSilent(silent) || wavPCMSilent(tinyPCMWav(1)) {
		t.Fatal("silence check misread the fixtures")
	}
	fileCompanionWebCaptureHook = func() ([]webNoteShot, []byte, bool) {
		return []webNoteShot{{At: 0, Gray: bytesRepeat(40, 256)}}, silent, false
	}
	t.Cleanup(func() { fileCompanionWebCaptureHook = nil })
	if err := app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note", RequestID: "web-zero-pcm", RectW: 960, RectH: 540}); err != nil {
		t.Fatal(err)
	}
	body := readWebNote(t, app, opened.SessionID)
	if !strings.Contains(body, fileCompanionErrNoAudio) || strings.Contains(body, "不该出现") || strings.Contains(body, "5:01") || strings.Contains(body, "05:01") {
		t.Fatalf("silent pcm note = %s", body)
	}
	if !strings.Contains(body, "时间不明 黑板") || !strings.Contains(body, "时间不明 「话」") {
		t.Fatalf("empty recording kept the clock:\n%s", body)
	}
	fileCompanionNoteModelHook = func(fileCompanionNoteModelRequest) *IMAgentResponse {
		return &IMAgentResponse{Text: "## 概要\n看了十分钟\n## 章节\n5:01 仍在采集里\n45:00 超出\n## 要点\n5:01 一点\n## 金句\n05:01 「话」\n## 不确定的地方\n无\n"}
	}
	fileCompanionWebCaptureHook = func() ([]webNoteShot, []byte, bool) {
		shots := make([]webNoteShot, 4)
		for i := range shots {
			shots[i] = webNoteShot{At: time.Duration(i) * 200 * time.Second, Gray: bytesRepeat(byte(40+i*40), 256)}
		}
		return shots, nil, true
	}
	if err := app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note", RequestID: "web-span", RectW: 960, RectH: 540}); err != nil {
		t.Fatal(err)
	}
	spanned := readWebNote(t, app, opened.SessionID)
	if !strings.Contains(spanned, fileCompanionErrNoAudio) || strings.Contains(spanned, fileCompanionErrSlideSpoken) || !strings.Contains(spanned, "05:01 仍在采集里") || !strings.Contains(spanned, "时间不明 超出") || strings.Contains(spanned, "45:00") {
		t.Fatalf("frame span = %s", spanned)
	}
	var prompted string
	fileCompanionNoteModelHook = func(req fileCompanionNoteModelRequest) *IMAgentResponse {
		prompted = req.Msg.Text
		return &IMAgentResponse{Text: "## 概要\n黑屏\n## 章节\n0:30 黑板\n## 要点\n无\n## 金句\n0:00 「话」\n## 不确定的地方\n无\n"}
	}
	fileCompanionWebCaptureHook = func() ([]webNoteShot, []byte, bool) {
		return []webNoteShot{
			{At: 0, Gray: bytesRepeat(2, 256)},
			{At: 30 * time.Second, Gray: bytesRepeat(1, 256)},
		}, pcm16MonoWAV(make([]byte, 16000*2*60), 16000), false
	}
	if err := app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note", RequestID: "web-silent-black", RectW: 960, RectH: 540}); err != nil {
		t.Fatal(err)
	}
	black := readWebNote(t, app, opened.SessionID)
	if !strings.Contains(black, fileCompanionErrNoAudio) || !strings.Contains(black, fileCompanionErrBlackFrame) || !strings.Contains(black, "时间不明 黑板") || strings.Contains(black, "0:30") || strings.Contains(black, "00:30") {
		t.Fatalf("silent black note = %s", black)
	}
	if strings.Contains(prompted, "这次记录从") {
		t.Fatalf("silence with no frame was offered as a timeline:\n%s", prompted)
	}
	fileCompanionNoteModelHook = func(req fileCompanionNoteModelRequest) *IMAgentResponse {
		prompted = req.Msg.Text
		return &IMAgentResponse{Text: "## 概要\n只有一帧\n## 章节\n0:02 仍在画面里\n0:30 黑板\n## 要点\n无\n## 金句\n0:00 「话」\n## 不确定的地方\n无\n"}
	}
	fileCompanionWebCaptureHook = func() ([]webNoteShot, []byte, bool) {
		return []webNoteShot{
			{At: 3 * time.Second, Gray: bytesRepeat(40, 256)},
			{At: 50 * time.Second, Gray: bytesRepeat(1, 256)},
		}, pcm16MonoWAV(make([]byte, 16000*2*60), 16000), false
	}
	if err := app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note", RequestID: "web-silent-tail", RectW: 960, RectH: 540}); err != nil {
		t.Fatal(err)
	}
	tail := readWebNote(t, app, opened.SessionID)
	if !strings.Contains(tail, fileCompanionErrNoAudio) || strings.Contains(tail, fileCompanionErrBlackFrame) || !strings.Contains(tail, "00:02 仍在画面里") || !strings.Contains(tail, "时间不明 黑板") || strings.Contains(tail, "0:30") || strings.Contains(tail, "00:30") {
		t.Fatalf("silent tail note = %s", tail)
	}
	if !strings.Contains(prompted, "这次记录从 0:00 到 00:03（3 秒）") || strings.Contains(prompted, "60 秒") || strings.Contains(prompted, "01:00") {
		t.Fatalf("silent tail prompt = %s", prompted)
	}
}

func TestFileCompanionWebPlayerLeftAdvancesGeneration(t *testing.T) {
	app := newVideoNoteTestApp(t)
	opened, err := app.FileCompanionOpenWebPage("https://example.com/left")
	if err != nil {
		t.Fatal(err)
	}
	watch := app.fileCompanionPlayerWatch()
	before := app.fileCompanionWebPlayerGen()
	if err := app.FileCompanionWebPlayerLeft(opened.SessionID, 8); err != nil {
		t.Fatal(err)
	}
	if app.fileCompanionWebPlayerGen() == before {
		t.Fatal("player generation did not move")
	}
	if watch.Err() == nil {
		t.Fatal("player watch still open")
	}
	st := app.companionRuntime()
	st.noteMu.Lock()
	stopped := st.webStopAt[opened.SessionID]
	next := st.webPlayerWatch
	st.noteMu.Unlock()
	if stopped != 8 {
		t.Fatalf("stop epoch = %d", stopped)
	}
	if next == nil || next.Err() != nil {
		t.Fatal("next player watch was not installed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = withCaptureDrop(ctx, nil, watch)
	if !captureLeft(ctx) {
		t.Fatal("capture still belongs to the player that left")
	}
}

func TestFileCompanionWebStopBeforeCaptureSkipsRecording(t *testing.T) {
	app := newVideoNoteTestApp(t)
	opened, err := app.FileCompanionOpenWebPage("https://example.com/early-stop")
	if err != nil {
		t.Fatal(err)
	}
	var calls int
	prevASR := fileCompanionNoteASRHook
	fileCompanionNoteASRHook = func([]byte) (string, error) { return "后一次", nil }
	t.Cleanup(func() { fileCompanionNoteASRHook = prevASR })
	prevModel := fileCompanionNoteModelHook
	fileCompanionNoteModelHook = func(req fileCompanionNoteModelRequest) *IMAgentResponse {
		text := "## 概要\n提前停止\n## 章节\n00:00 开始\n## 要点\n00:00 一点\n## 金句\n00:00 「话」\n## 不确定的地方\n无\n"
		if strings.Contains(req.Msg.Text, "后一次") {
			text = "## 概要\n后一次写入\n## 章节\n00:00 开始\n## 要点\n00:00 一点\n## 金句\n00:00 「话」\n## 不确定的地方\n无\n"
		}
		return &IMAgentResponse{Text: text}
	}
	t.Cleanup(func() { fileCompanionNoteModelHook = prevModel })
	prevHook := fileCompanionWebCaptureHook
	fileCompanionWebCaptureHook = func() ([]webNoteShot, []byte, bool) {
		calls++
		return []webNoteShot{{At: 0, Gray: bytesRepeat(40, 256), JPEG: tinyNoteJPEG(t)}}, tinyPCMWav(1), false
	}
	t.Cleanup(func() { fileCompanionWebCaptureHook = prevHook })

	const early int64 = 4
	if err := app.CancelFileCompanionWebTurn(opened.SessionID, early); err != nil {
		t.Fatal(err)
	}
	if err := app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{
		SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note",
		RequestID: "web-early", RectW: 960, RectH: 540, CaptureEpoch: early,
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("capture calls after an early stop = %d", calls)
	}
	body := readWebNote(t, app, opened.SessionID)
	if !strings.Contains(body, "提前停止") || !strings.Contains(body, fileCompanionErrBlackFrame) || !strings.Contains(body, fileCompanionErrNoAudio) {
		t.Fatalf("early stop note = %s", body)
	}
	if strings.Contains(body, fileCompanionErrNoteCancelled) {
		t.Fatalf("early stop discarded the note: %s", body)
	}

	if err := app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{
		SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note",
		RequestID: "web-next", RectW: 960, RectH: 540, CaptureEpoch: early + 1,
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("next capture calls = %d", calls)
	}
	next := readWebNote(t, app, opened.SessionID)
	if !strings.Contains(next, "后一次写入") {
		t.Fatalf("next note = %s", next)
	}
}

func TestFileCompanionWebNoteNilHookUsesCapture(t *testing.T) {
	app := newVideoNoteTestApp(t)
	opened, err := app.FileCompanionOpenWebPage("https://www.youtube.com/watch?v=live")
	if err != nil {
		t.Fatal(err)
	}
	fileCompanionWebCaptureHook = nil
	t.Cleanup(func() { fileCompanionWebCaptureHook = nil })
	fileCompanionCaptureViewCalls.Store(0)
	fileCompanionCaptureAudioCalls.Store(0)
	fileCompanionNoteModelHook = func(fileCompanionNoteModelRequest) *IMAgentResponse {
		return &IMAgentResponse{Text: "## 概要\n无画面\n## 章节\n00:00 开始\n## 要点\n00:00 一点\n## 金句\n00:00 「话」\n## 不确定的地方\n无\n"}
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{
			SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note", RequestID: "web-live", RectW: 960, RectH: 540,
		})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for fileCompanionCaptureViewCalls.Load() == 0 || fileCompanionCaptureAudioCalls.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("capture calls view=%d audio=%d", fileCompanionCaptureViewCalls.Load(), fileCompanionCaptureAudioCalls.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := app.CancelFileCompanionWebTurn(opened.SessionID, 0); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("nil hook note = %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("nil hook capture did not stop")
	}
	body := readWebNote(t, app, opened.SessionID)
	if !strings.Contains(body, fileCompanionErrBlackFrame) || !strings.Contains(body, fileCompanionErrNoAudio) {
		t.Fatalf("nil hook note = %s", body)
	}
	if strings.Contains(body, fileCompanionErrNoteCancelled) {
		t.Fatalf("nil hook was cancelled: %s", body)
	}
	work := filepath.Join(app.GetDataDir(), "file-companion", "work", opened.SessionID)
	if _, statErr := os.Stat(work); !os.IsNotExist(statErr) {
		t.Fatal("nil hook left the recording")
	}
}

func TestFileCompanionDiscardWebNoteWritesNothing(t *testing.T) {
	app := newVideoNoteTestApp(t)
	opened, err := app.FileCompanionOpenWebPage("https://www.youtube.com/watch?v=close-tab")
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	started := make(chan struct{})
	fileCompanionNoteModelHook = func(fileCompanionNoteModelRequest) *IMAgentResponse {
		t.Error("discarded note called the model")
		return &IMAgentResponse{Text: "## 概要\n不该写入\n"}
	}
	fileCompanionWebCaptureHook = func() ([]webNoteShot, []byte, bool) {
		close(started)
		<-release
		return []webNoteShot{{At: 0, Gray: bytesRepeat(40, 256), JPEG: []byte{0xff, 0xd8}}}, tinyPCMWav(1), false
	}
	t.Cleanup(func() {
		fileCompanionWebCaptureHook = nil
		fileCompanionNoteModelHook = nil
	})
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{
			SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note", RequestID: "web-close", RectW: 960, RectH: 540,
		})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("capture did not start")
	}
	if err := app.FileCompanionDiscardWebNote(opened.SessionID); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), fileCompanionErrNoteCancelled) {
			t.Fatalf("discard = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("discard did not finish")
	}
	if md := app.fileCompanionFindNoteMarkdown(opened.SessionID); md != "" {
		if _, statErr := os.Stat(md); statErr == nil {
			t.Fatal("closing the tab wrote a note")
		}
	}
	work := filepath.Join(app.GetDataDir(), "file-companion", "work", opened.SessionID)
	if _, statErr := os.Stat(work); !os.IsNotExist(statErr) {
		t.Fatal("discard left the recording")
	}
}

func TestFileCompanionDiscardWebNoteDuringModelWritesNothing(t *testing.T) {
	app := newVideoNoteTestApp(t)
	opened, err := app.FileCompanionOpenWebPage("https://www.youtube.com/watch?v=close-late")
	if err != nil {
		t.Fatal(err)
	}
	prevASR := fileCompanionNoteASRHook
	fileCompanionNoteASRHook = func([]byte) (string, error) { return "一句", nil }
	prevOCR := fileCompanionNoteOCRStateHook
	fileCompanionNoteOCRStateHook = func() (bool, bool) { return false, false }
	release := make(chan struct{})
	started := make(chan struct{})
	fileCompanionNoteModelHook = func(fileCompanionNoteModelRequest) *IMAgentResponse {
		close(started)
		<-release
		return &IMAgentResponse{Text: "## 概要\n不该写入\n## 章节\n00:00 开始\n## 要点\n00:00 一点\n## 金句\n00:00 「话」\n## 不确定的地方\n无\n"}
	}
	jpeg := tinyNoteJPEG(t)
	fileCompanionWebCaptureHook = func() ([]webNoteShot, []byte, bool) {
		return []webNoteShot{{At: 0, Gray: bytesRepeat(40, 256), JPEG: jpeg}}, tinyPCMWav(1), false
	}
	t.Cleanup(func() {
		fileCompanionNoteASRHook = prevASR
		fileCompanionNoteOCRStateHook = prevOCR
		fileCompanionNoteModelHook = nil
		fileCompanionWebCaptureHook = nil
		select {
		case <-release:
		default:
			close(release)
		}
	})
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{
			SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note", RequestID: "web-close-model", RectW: 960, RectH: 540,
		})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("model did not start")
	}
	if err := app.FileCompanionDiscardWebNote(opened.SessionID); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), fileCompanionErrNoteCancelled) {
			t.Fatalf("discard during model = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("discard during model did not finish")
	}
	if md := app.fileCompanionFindNoteMarkdown(opened.SessionID); md != "" {
		if _, statErr := os.Stat(md); statErr == nil {
			t.Fatal("closing the tab during the note wrote a file")
		}
	}
}

func TestFileCompanionWebStopDuringModelStillWrites(t *testing.T) {
	app := newVideoNoteTestApp(t)
	opened, err := app.FileCompanionOpenWebPage("https://www.youtube.com/watch?v=stop-late")
	if err != nil {
		t.Fatal(err)
	}
	prevASR := fileCompanionNoteASRHook
	fileCompanionNoteASRHook = func([]byte) (string, error) { return "一句", nil }
	prevOCR := fileCompanionNoteOCRStateHook
	fileCompanionNoteOCRStateHook = func() (bool, bool) { return false, false }
	release := make(chan struct{})
	started := make(chan struct{})
	fileCompanionNoteModelHook = func(fileCompanionNoteModelRequest) *IMAgentResponse {
		close(started)
		<-release
		return &IMAgentResponse{Text: "## 概要\n停止后仍写入\n## 章节\n00:00 开始\n## 要点\n00:00 一点\n## 金句\n00:00 「话」\n## 不确定的地方\n无\n"}
	}
	jpeg := tinyNoteJPEG(t)
	fileCompanionWebCaptureHook = func() ([]webNoteShot, []byte, bool) {
		return []webNoteShot{{At: 0, Gray: bytesRepeat(40, 256), JPEG: jpeg}}, tinyPCMWav(1), false
	}
	t.Cleanup(func() {
		fileCompanionNoteASRHook = prevASR
		fileCompanionNoteOCRStateHook = prevOCR
		fileCompanionNoteModelHook = nil
		fileCompanionWebCaptureHook = nil
		select {
		case <-release:
		default:
			close(release)
		}
	})
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{
			SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note", RequestID: "web-stop-model", RectW: 960, RectH: 540,
		})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("model did not start")
	}
	if err := app.CancelFileCompanionWebTurn(opened.SessionID, 0); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("stop during model = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop during model did not finish")
	}
	body := readWebNote(t, app, opened.SessionID)
	if !strings.Contains(body, "停止后仍写入") || strings.Contains(body, fileCompanionErrNoteCancelled) {
		t.Fatalf("stop during model note = %s", body)
	}
}

func TestWebPlaybackRewindStartsAtZero(t *testing.T) {
	if !strings.Contains(webPlaybackRewindScript, "seekTo") || !strings.Contains(webPlaybackRewindScript, "playVideo") || !strings.Contains(webPlaybackRewindScript, "ad-showing") {
		t.Fatalf("rewind script = %s", webPlaybackRewindScript)
	}
	if strings.Contains(webPlaybackRewindScript, "navigate") || strings.Contains(webPlaybackRewindScript, "location") || strings.Contains(webPlaybackRewindScript, "href") {
		t.Fatal("rewind script changes the page")
	}
	if fileCompanionWebSeekSettled("ad") || fileCompanionWebSeekSettled("err") || !fileCompanionWebSeekSettled("seek") || !fileCompanionWebSeekSettled("none") || !fileCompanionWebSeekSettled("") {
		t.Fatal("seek settled on the wrong script result")
	}
	if got := fileCompanionRewindResult("", errors.New("timeout")); got != "err" {
		t.Fatalf("script error = %q", got)
	}
	if got := fileCompanionRewindResult(`"seek"`, nil); got != "seek" {
		t.Fatalf("script seek = %q", got)
	}
	if got := fileCompanionRewindResult("", nil); got != "none" {
		t.Fatalf("empty script = %q", got)
	}
	if !strings.Contains(webPlaybackRewindScript, `return "err"`) {
		t.Fatal("rewind script reports a failure as no player")
	}
	timeAt := strings.Index(webPlaybackRewindScript, "currentTime=0")
	seekReturn := strings.Index(webPlaybackRewindScript, `return "seek"`)
	adReturn := strings.Index(webPlaybackRewindScript, `return "ad"`)
	playAPI := strings.Index(webPlaybackRewindScript, "playVideo()")
	seekAPI := strings.Index(webPlaybackRewindScript, "seekTo(")
	elemPlay := strings.Index(webPlaybackRewindScript, "v.play()")
	if timeAt < 0 || seekReturn < timeAt || adReturn < 0 || adReturn > timeAt || playAPI < 0 || seekAPI < playAPI || elemPlay < 0 || timeAt < elemPlay {
		t.Fatal("rewind seeks while the player is still ended")
	}
	if wait, stop := fileCompanionRewindWait("err", fileCompanionWebAdWait, 0); !wait || stop {
		t.Fatal("a script failure after a long ad was not retried")
	}
	if wait, _ := fileCompanionRewindWait("err", 0, fileCompanionWebSeekRetry); wait {
		t.Fatal("script failures retried past the limit")
	}
	if wait, stop := fileCompanionRewindWait("ad", 0, fileCompanionWebSeekRetry); !wait || !stop {
		t.Fatal("an advertisement was not waited out")
	}
	if got := fileCompanionScriptString(`"seek"`); got != "seek" {
		t.Fatalf("script string = %q", got)
	}
	if got := fileCompanionScriptString("video"); got != "video" {
		t.Fatalf("bare script string = %q", got)
	}
}

func TestFileCompanionRewindErrorRetriesOnTheEndScreen(t *testing.T) {
	app := newVideoNoteTestApp(t)
	opened, err := app.FileCompanionOpenWebPage("https://www.youtube.com/watch?v=rewind-err")
	if err != nil {
		t.Fatal(err)
	}
	prevCap := fileCompanionWebCaptureHook
	prevEnded := fileCompanionPlaybackEndedHook
	prevRewind := fileCompanionRewindWebPlayerHook
	fileCompanionWebCaptureHook = nil
	calls := 0
	fileCompanionRewindWebPlayerHook = func(context.Context) string {
		calls++
		if calls == 1 {
			return "err"
		}
		return "seek"
	}
	fileCompanionPlaybackEndedHook = func(context.Context) bool { return true }
	fileCompanionNoteModelHook = func(fileCompanionNoteModelRequest) *IMAgentResponse {
		return &IMAgentResponse{Text: "## 概要\n重试后写入\n## 章节\n00:00 开始\n## 要点\n00:00 一点\n## 金句\n00:00 「话」\n## 不确定的地方\n无\n"}
	}
	t.Cleanup(func() {
		fileCompanionWebCaptureHook = prevCap
		fileCompanionPlaybackEndedHook = prevEnded
		fileCompanionRewindWebPlayerHook = prevRewind
		fileCompanionNoteModelHook = nil
	})
	start := time.Now()
	err = app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{
		SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note", RequestID: "web-rewind-err", RectW: 960, RectH: 540,
	})
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	// One failure, the first seek, then two more while the end screen remains.
	if calls != 4 || elapsed < 3*time.Second || elapsed > 12*time.Second {
		t.Fatalf("rewind calls=%d elapsed=%s", calls, elapsed)
	}
	body := readWebNote(t, app, opened.SessionID)
	if !strings.Contains(body, "重试后写入") {
		t.Fatalf("rewind retry note = %s", body)
	}
}

func TestFileCompanionWaitForSeekLeavesEndedScreen(t *testing.T) {
	prev := fileCompanionPlaybackEndedHook
	t.Cleanup(func() { fileCompanionPlaybackEndedHook = prev })
	checks := 0
	fileCompanionPlaybackEndedHook = func(context.Context) bool {
		checks++
		return checks < 2
	}
	started := time.Now()
	if fileCompanionWaitForSeek(context.Background(), nil, nil) {
		t.Fatal("a seek that leaves the end screen cancelled the note")
	}
	elapsed := time.Since(started)
	if checks != 2 || elapsed < 1500*time.Millisecond || elapsed > 3500*time.Millisecond {
		t.Fatalf("seek wait checks=%d elapsed=%s", checks, elapsed)
	}

	checks = 0
	fileCompanionPlaybackEndedHook = func(context.Context) bool {
		checks++
		return true
	}
	started = time.Now()
	if fileCompanionWaitForSeek(context.Background(), nil, nil) {
		t.Fatal("a seek that stays on the end screen cancelled the note")
	}
	elapsed = time.Since(started)
	if checks != 3 || elapsed < 2500*time.Millisecond {
		t.Fatalf("end screen wait checks=%d elapsed=%s", checks, elapsed)
	}
}

func TestFileCompanionSeekSettleDoesNotStopOnEnded(t *testing.T) {
	prev := fileCompanionPlaybackEndedHook
	t.Cleanup(func() { fileCompanionPlaybackEndedHook = prev })
	fileCompanionPlaybackEndedHook = func(context.Context) bool { return true }
	if fileCompanionWaitCapture(context.Background(), 40*time.Millisecond, nil, nil, false) {
		t.Fatal("stale ended-mode stopped the seek")
	}
	started := time.Now()
	if !fileCompanionWaitCapture(context.Background(), 3*time.Second, nil, nil, true) {
		t.Fatal("ended-mode did not stop the recording")
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("ended-mode waited out the whole gap")
	}
}

func TestWebPlaybackHasEnded(t *testing.T) {
	if !strings.Contains(webPlaybackEndedScript, "ad-showing") || !strings.Contains(webPlaybackEndedScript, "ended-mode") || !strings.Contains(webPlaybackEndedScript, "isFinite") || !strings.Contains(webPlaybackEndedScript, "atStart") {
		t.Fatalf("playback script = %s", webPlaybackEndedScript)
	}
	if strings.Contains(webPlaybackEndedScript, "navigate") || strings.Contains(webPlaybackEndedScript, "location") {
		t.Fatal("playback script changes the page")
	}
	cases := []struct {
		raw  string
		want bool
	}{
		{`{"player":true,"ad":false,"endedMode":true,"ended":false,"finite":true,"nearEnd":false}`, false},
		{`{"player":true,"ad":false,"endedMode":true,"ended":true,"finite":true,"nearEnd":true,"atStart":false}`, true},
		{`{"player":true,"ad":false,"endedMode":true,"ended":true,"finite":true,"nearEnd":true,"atStart":true}`, false},
		{`{"player":true,"ad":true,"endedMode":true,"ended":true,"finite":true,"nearEnd":true}`, false},
		{`{"player":true,"ad":false,"endedMode":false,"ended":true,"finite":true,"nearEnd":true}`, false},
		{`{"player":false,"ad":false,"endedMode":false,"ended":true,"finite":true,"nearEnd":true}`, true},
		{`{"player":false,"ad":false,"endedMode":false,"ended":true,"finite":false,"nearEnd":false}`, false},
		{`{"player":false,"ad":false,"endedMode":false,"ended":true,"finite":true,"nearEnd":false}`, false},
		{`{"player":false,"ad":false,"endedMode":false,"ended":false,"finite":true,"nearEnd":true}`, false},
		{"", false},
		{"null", false},
		{"true", false},
		{"not json", false},
	}
	for _, tc := range cases {
		if got := webPlaybackHasEnded(tc.raw); got != tc.want {
			t.Fatalf("webPlaybackHasEnded(%s) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestFileCompanionWebNoteStopsWhenPlaybackEnds(t *testing.T) {
	app := newVideoNoteTestApp(t)
	opened, err := app.FileCompanionOpenWebPage("https://www.youtube.com/watch?v=ended")
	if err != nil {
		t.Fatal(err)
	}
	prevCap := fileCompanionWebCaptureHook
	fileCompanionWebCaptureHook = nil
	prevEnded := fileCompanionPlaybackEndedHook
	fileCompanionPlaybackEndedHook = func(context.Context) bool { return true }
	fileCompanionNoteModelHook = func(fileCompanionNoteModelRequest) *IMAgentResponse {
		return &IMAgentResponse{Text: "## 概要\n播放结束写入\n## 章节\n00:00 开始\n## 要点\n00:00 一点\n## 金句\n00:00 「话」\n## 不确定的地方\n无\n"}
	}
	t.Cleanup(func() {
		fileCompanionWebCaptureHook = prevCap
		fileCompanionPlaybackEndedHook = prevEnded
		fileCompanionNoteModelHook = nil
	})
	start := time.Now()
	err = app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{
		SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note", RequestID: "web-ended", RectW: 960, RectH: 540,
	})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("ended playback kept recording for %s", time.Since(start))
	}
	body := readWebNote(t, app, opened.SessionID)
	if !strings.Contains(body, "播放结束写入") || strings.Contains(body, fileCompanionErrNoteCancelled) {
		t.Fatalf("ended note = %s", body)
	}
}

func TestFileCompanionWebStopDuringCaptureStillWrites(t *testing.T) {
	app := newVideoNoteTestApp(t)
	opened, err := app.FileCompanionOpenWebPage("https://www.youtube.com/watch?v=stop-listen")
	if err != nil {
		t.Fatal(err)
	}
	prevCap := fileCompanionWebCaptureHook
	fileCompanionWebCaptureHook = nil
	prevEnded := fileCompanionPlaybackEndedHook
	fileCompanionPlaybackEndedHook = func(context.Context) bool { return false }
	fileCompanionNoteModelHook = func(fileCompanionNoteModelRequest) *IMAgentResponse {
		return &IMAgentResponse{Text: "## 概要\n录音中停止仍写入\n## 章节\n00:00 开始\n## 要点\n00:00 一点\n## 金句\n00:00 「话」\n## 不确定的地方\n无\n"}
	}
	t.Cleanup(func() {
		fileCompanionWebCaptureHook = prevCap
		fileCompanionPlaybackEndedHook = prevEnded
		fileCompanionNoteModelHook = nil
	})
	fileCompanionCaptureViewCalls.Store(0)
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{
			SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note", RequestID: "web-stop-listen", RectW: 960, RectH: 540,
		})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for fileCompanionCaptureViewCalls.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("capture did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := app.StopFileCompanionWebNote(opened.SessionID, 0); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("stop during capture = %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("stop during capture did not finish")
	}
	body := readWebNote(t, app, opened.SessionID)
	if !strings.Contains(body, "录音中停止仍写入") || strings.Contains(body, fileCompanionErrNoteCancelled) {
		t.Fatalf("stop during capture note = %s", body)
	}
}

func TestFileCompanionWebStopDuringWriteDropsTheNote(t *testing.T) {
	app := newVideoNoteTestApp(t)
	opened, err := app.FileCompanionOpenWebPage("https://www.youtube.com/watch?v=stop-write")
	if err != nil {
		t.Fatal(err)
	}
	prevASR := fileCompanionNoteASRHook
	fileCompanionNoteASRHook = func([]byte) (string, error) { return "一句", nil }
	prevOCR := fileCompanionNoteOCRStateHook
	fileCompanionNoteOCRStateHook = func() (bool, bool) { return false, false }
	release := make(chan struct{})
	started := make(chan struct{})
	fileCompanionNoteModelHook = func(fileCompanionNoteModelRequest) *IMAgentResponse {
		close(started)
		<-release
		return &IMAgentResponse{Text: "## 概要\n不该写入\n## 章节\n00:00 开始\n## 要点\n00:00 一点\n## 金句\n00:00 「话」\n## 不确定的地方\n无\n"}
	}
	jpeg := tinyNoteJPEG(t)
	prevCap := fileCompanionWebCaptureHook
	fileCompanionWebCaptureHook = func() ([]webNoteShot, []byte, bool) {
		return []webNoteShot{{At: 0, Gray: bytesRepeat(40, 256), JPEG: jpeg}}, tinyPCMWav(1), false
	}
	t.Cleanup(func() {
		fileCompanionNoteASRHook = prevASR
		fileCompanionNoteOCRStateHook = prevOCR
		fileCompanionNoteModelHook = nil
		fileCompanionWebCaptureHook = prevCap
		select {
		case <-release:
		default:
			close(release)
		}
	})
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{
			SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note", RequestID: "web-stop-write", RectW: 960, RectH: 540,
		})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("model did not start")
	}
	if err := app.StopFileCompanionWebNote(opened.SessionID, 0); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), fileCompanionErrNoteCancelled) {
			t.Fatalf("stop during write = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop during write did not finish")
	}
	if md := app.fileCompanionFindNoteMarkdown(opened.SessionID); md != "" {
		if _, statErr := os.Stat(md); statErr == nil {
			t.Fatal("stop during write still wrote a note")
		}
	}
}

func TestFileCompanionWebStopDuringListenStillWrites(t *testing.T) {
	app := newVideoNoteTestApp(t)
	opened, err := app.FileCompanionOpenWebPage("https://www.youtube.com/watch?v=stop-listen-asr")
	if err != nil {
		t.Fatal(err)
	}
	prevASR := fileCompanionNoteASRHook
	release := make(chan struct{})
	started := make(chan struct{})
	fileCompanionNoteASRHook = func([]byte) (string, error) {
		close(started)
		<-release
		return "一句", nil
	}
	prevOCR := fileCompanionNoteOCRStateHook
	fileCompanionNoteOCRStateHook = func() (bool, bool) { return false, false }
	fileCompanionNoteModelHook = func(fileCompanionNoteModelRequest) *IMAgentResponse {
		return &IMAgentResponse{Text: "## 概要\n识别中停止仍写入\n## 章节\n00:00 开始\n## 要点\n00:00 一点\n## 金句\n00:00 「话」\n## 不确定的地方\n无\n"}
	}
	jpeg := tinyNoteJPEG(t)
	prevCap := fileCompanionWebCaptureHook
	fileCompanionWebCaptureHook = func() ([]webNoteShot, []byte, bool) {
		return []webNoteShot{{At: 0, Gray: bytesRepeat(40, 256), JPEG: jpeg}}, tinyPCMWav(1), false
	}
	t.Cleanup(func() {
		fileCompanionNoteASRHook = prevASR
		fileCompanionNoteOCRStateHook = prevOCR
		fileCompanionNoteModelHook = nil
		fileCompanionWebCaptureHook = prevCap
		select {
		case <-release:
		default:
			close(release)
		}
	})
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{
			SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note", RequestID: "web-stop-asr", RectW: 960, RectH: 540,
		})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("recognition did not start")
	}
	st := app.companionRuntime()
	st.mu.Lock()
	sawSpeech := false
	for _, ev := range st.events {
		if strings.Contains(ev.Data, "正在识别语音") {
			sawSpeech = true
			break
		}
	}
	st.mu.Unlock()
	if !sawSpeech {
		t.Fatal("web recognition left the status on 正在记录声音")
	}
	if err := app.StopFileCompanionWebNote(opened.SessionID, 0); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("stop during recognition = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop during recognition did not finish")
	}
	body := readWebNote(t, app, opened.SessionID)
	if !strings.Contains(body, "识别中停止仍写入") || strings.Contains(body, fileCompanionErrNoteCancelled) {
		t.Fatalf("stop during recognition note = %s", body)
	}
}

func TestFileCompanionWebNoteASRNotReadyWritesNothing(t *testing.T) {
	app := newVideoNoteTestApp(t)
	opened, err := app.FileCompanionOpenWebPage("https://www.youtube.com/watch?v=no-asr")
	if err != nil {
		t.Fatal(err)
	}
	prevReady := fileCompanionNoteASRReadyHook
	fileCompanionNoteASRReadyHook = func() bool { return false }
	prevASR := fileCompanionNoteASRHook
	fileCompanionNoteASRHook = nil
	fileCompanionNoteModelHook = func(fileCompanionNoteModelRequest) *IMAgentResponse {
		t.Error("asr not ready called the model")
		return &IMAgentResponse{Text: "## 概要\n不该写入\n"}
	}
	fileCompanionWebCaptureHook = func() ([]webNoteShot, []byte, bool) {
		return []webNoteShot{{At: 0, Gray: bytesRepeat(40, 256), JPEG: tinyNoteJPEG(t)}}, tinyPCMWav(1), false
	}
	t.Cleanup(func() {
		fileCompanionNoteASRReadyHook = prevReady
		fileCompanionNoteASRHook = prevASR
		fileCompanionNoteModelHook = nil
		fileCompanionWebCaptureHook = nil
	})
	err = app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{
		SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note", RequestID: "web-no-asr", RectW: 960, RectH: 540,
	})
	if err == nil || !strings.Contains(err.Error(), fileCompanionErrASRNotReady) {
		t.Fatalf("asr not ready = %v", err)
	}
	if md := app.fileCompanionFindNoteMarkdown(opened.SessionID); md != "" {
		if _, statErr := os.Stat(md); statErr == nil {
			t.Fatal("asr not ready wrote a note")
		}
	}
}

func TestFileCompanionWebNoteSlidePrefixAndStamp(t *testing.T) {
	app := newVideoNoteTestApp(t)
	opened, err := app.FileCompanionOpenWebPage("https://www.youtube.com/watch?v=slides")
	if err != nil {
		t.Fatal(err)
	}
	prevASR := fileCompanionNoteASRHook
	fileCompanionNoteASRHook = func([]byte) (string, error) { return "ok", nil }
	prevOCR := fileCompanionNoteOCRStateHook
	fileCompanionNoteOCRStateHook = func() (bool, bool) { return false, false }
	jpeg := tinyNoteJPEG(t)
	fileCompanionWebCaptureHook = func() ([]webNoteShot, []byte, bool) {
		shots := make([]webNoteShot, 4)
		for i := range shots {
			shots[i] = webNoteShot{At: time.Duration(i*3) * time.Second, Gray: bytesRepeat(byte(40+i*40), 256), JPEG: jpeg}
		}
		return shots, tinyPCMWav(1), false
	}
	fileCompanionNoteModelHook = func(req fileCompanionNoteModelRequest) *IMAgentResponse {
		if !strings.Contains(req.Msg.Text, fileCompanionErrSlideSpoken) {
			t.Errorf("prompt missed the slide prefix: %s", req.Msg.Text)
		}
		return &IMAgentResponse{Text: "## 概要\n画面\n## 章节\n00:00 开始\n## 要点\n00:30 太晚\n## 金句\n00:00 「话」\n## 不确定的地方\n无\n"}
	}
	t.Cleanup(func() {
		fileCompanionNoteASRHook = prevASR
		fileCompanionNoteOCRStateHook = prevOCR
		fileCompanionNoteModelHook = nil
		fileCompanionWebCaptureHook = nil
	})
	if err := app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{
		SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note", RequestID: "web-slides", RectW: 960, RectH: 540,
	}); err != nil {
		t.Fatal(err)
	}
	body := readWebNote(t, app, opened.SessionID)
	if !strings.Contains(body, fileCompanionErrSlideSpoken) {
		t.Fatalf("slide note = %s", body)
	}
	if strings.Contains(body, "00:30") || !strings.Contains(body, fileCompanionErrTimeUnknown) {
		t.Fatalf("stamp rewrite = %s", body)
	}
	st := app.companionRuntime()
	st.mu.Lock()
	defer st.mu.Unlock()
	sawWrite := false
	for _, ev := range st.events {
		if strings.Contains(ev.Data, fileCompanionNoteProgressWrite) {
			sawWrite = true
			break
		}
	}
	if !sawWrite {
		t.Fatal("web note did not say it was writing")
	}
}

func TestFileCompanionWebFollowUpUsesNoteExcerpt(t *testing.T) {
	app := newVideoNoteTestApp(t)
	opened, err := app.FileCompanionOpenWebPage("https://www.youtube.com/watch?v=lecture-follow")
	if err != nil {
		t.Fatal(err)
	}
	var before string
	prevTurn := fileCompanionTurnModelHook
	fileCompanionTurnModelHook = func(msg IMUserMessage) *IMAgentResponse {
		before = msg.Text
		return &IMAgentResponse{Text: "还没有笔记"}
	}
	t.Cleanup(func() { fileCompanionTurnModelHook = prevTurn })
	if err := app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{
		SessionID: opened.SessionID, Text: "这个页面在讲什么", RequestID: "ask-1",
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(before, "已生成的笔记摘录") {
		t.Fatal("question before a note attached an excerpt")
	}

	jpeg := tinyNoteJPEG(t)
	prevCap := fileCompanionWebCaptureHook
	fileCompanionWebCaptureHook = func() ([]webNoteShot, []byte, bool) {
		return []webNoteShot{{At: 0, Gray: bytesRepeat(40, 256), JPEG: jpeg}}, nil, true
	}
	t.Cleanup(func() { fileCompanionWebCaptureHook = prevCap })
	marker := "UNIQUE_WEB_NOTE_SENTENCE"
	prevModel := fileCompanionNoteModelHook
	fileCompanionNoteModelHook = func(fileCompanionNoteModelRequest) *IMAgentResponse {
		return &IMAgentResponse{Text: "## 概要\n" + marker + "\n## 章节\n00:00 开始\n## 要点\n00:00 一点\n## 金句\n00:00 「话」\n## 不确定的地方\n无\n"}
	}
	t.Cleanup(func() { fileCompanionNoteModelHook = prevModel })
	if err := app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{
		SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note", RequestID: "note-1", RectW: 960, RectH: 540,
	}); err != nil {
		t.Fatal(err)
	}

	var follow string
	fileCompanionTurnModelHook = func(msg IMUserMessage) *IMAgentResponse {
		follow = msg.Text
		return &IMAgentResponse{Text: "摘录已看到"}
	}
	if err := app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{
		SessionID: opened.SessionID, Text: "笔记里的第一点是什么", RequestID: "ask-2",
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(follow, marker) != 1 {
		t.Fatalf("follow marker = %d", strings.Count(follow, marker))
	}
	if strings.Contains(follow, "![") {
		t.Fatalf("follow-up quoted a frame path: %s", follow)
	}
	if !strings.Contains(follow, "已生成的笔记摘录") || !strings.Contains(follow, "笔记里的第一点是什么") {
		t.Fatalf("follow-up = %q", follow)
	}

	var second string
	fileCompanionNoteModelHook = func(req fileCompanionNoteModelRequest) *IMAgentResponse {
		second = req.Msg.Text
		return &IMAgentResponse{Text: "## 概要\n第二版\n## 章节\n00:00 开始\n## 要点\n00:00 一点\n## 金句\n00:00 「话」\n## 不确定的地方\n无\n"}
	}
	if err := app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{
		SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note", RequestID: "note-2", RectW: 960, RectH: 540,
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(second, marker) || strings.Contains(second, "已生成的笔记摘录") {
		t.Fatal("regenerating a web note attached the old note")
	}
}

func TestFileCompanionWebCancelLeavesTheChat(t *testing.T) {
	app := newVideoNoteTestApp(t)
	t.Cleanup(func() {
		if app.imHandler != nil && app.imHandler.memory != nil {
			app.imHandler.memory.Stop()
		}
	})
	opened, err := app.FileCompanionOpenWebPage("https://example.com/watch")
	if err != nil {
		t.Fatal(err)
	}
	sessionID := opened.SessionID
	if sessionID == "" {
		t.Fatal("missing session")
	}
	userID := "file-companion:" + sessionID
	if err := app.ensureFileCompanionHandler(userID, sessionID); err != nil {
		t.Fatal(err)
	}
	app.imHandler.memory.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "上一问"},
		{Role: "assistant", Content: "旧回答"},
	})
	gen := app.fileCompanionChatGen(userID)
	if err := app.CancelFileCompanionWebGeneration(sessionID, "req-web"); err != nil {
		t.Fatal(err)
	}
	if got := app.fileCompanionChatGen(userID); got != gen {
		t.Fatalf("chat gen %d -> %d", gen, got)
	}
	loaded := app.imHandler.memory.Load(userID)
	if len(loaded) != 2 || loaded[0].Content != "上一问" || loaded[1].Content != "旧回答" {
		t.Fatalf("memory = %#v", loaded)
	}
	if !app.fileCompanionChatStopped("req-web") {
		t.Fatal("cancel did not mark the request")
	}
	if app.fileCompanionChatStopped("req-next") {
		t.Fatal("cancel marked a later request")
	}
	if err := app.CancelFileCompanionWebGeneration("", "req-empty"); err != nil {
		t.Fatal(err)
	}
	if app.fileCompanionChatStopped("req-empty") {
		t.Fatal("an unknown session marked a request")
	}
}

func TestFileCompanionWebCancelBeforeTheNoteStartsWritesNothing(t *testing.T) {
	app := newVideoNoteTestApp(t)
	t.Cleanup(func() {
		if app.imHandler != nil && app.imHandler.memory != nil {
			app.imHandler.memory.Stop()
		}
	})
	opened, err := app.FileCompanionOpenWebPage("https://example.com/watch")
	if err != nil {
		t.Fatal(err)
	}
	prevCap := fileCompanionWebCaptureHook
	prevModel := fileCompanionNoteModelHook
	prevASR := fileCompanionNoteASRHook
	prevOCR := fileCompanionNoteOCRStateHook
	fileCompanionWebCaptureHook = func() ([]webNoteShot, []byte, bool) {
		t.Error("cancelled note captured")
		return nil, nil, true
	}
	fileCompanionNoteModelHook = func(fileCompanionNoteModelRequest) *IMAgentResponse {
		t.Error("cancelled note called the model")
		return &IMAgentResponse{Text: "不该写"}
	}
	t.Cleanup(func() {
		fileCompanionWebCaptureHook = prevCap
		fileCompanionNoteModelHook = prevModel
		fileCompanionNoteASRHook = prevASR
		fileCompanionNoteOCRStateHook = prevOCR
	})
	if err := app.CancelFileCompanionWebGeneration(opened.SessionID, "req-early"); err != nil {
		t.Fatal(err)
	}
	err = app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{
		SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note", RequestID: "req-early", RectW: 960, RectH: 540,
	})
	if err == nil || !strings.Contains(err.Error(), fileCompanionErrNoteCancelled) {
		t.Fatalf("early cancel = %v", err)
	}
	if md := app.fileCompanionFindNoteMarkdown(opened.SessionID); md != "" {
		if _, statErr := os.Stat(md); statErr == nil {
			t.Fatal("early cancel wrote a note")
		}
	}
	app.fileCompanionSetWebNotePhase(opened.SessionID, webNotePhaseRecording)
	if err := app.CancelFileCompanionWebGeneration(opened.SessionID, "req-rec"); err != nil {
		t.Fatal(err)
	}
	st := app.companionRuntime()
	st.noteMu.Lock()
	armed := len(st.noteStopped[opened.SessionID])
	st.noteMu.Unlock()
	if armed != 0 {
		t.Fatalf("recording cancel armed %d note stops", armed)
	}
	app.fileCompanionSetWebNotePhase(opened.SessionID, webNotePhaseIdle)

	fileCompanionWebCaptureHook = func() ([]webNoteShot, []byte, bool) {
		return []webNoteShot{{At: 0, Gray: bytesRepeat(40, 256), JPEG: tinyNoteJPEG(t)}}, tinyPCMWav(1), false
	}
	fileCompanionNoteASRHook = func([]byte) (string, error) { return "讲完了", nil }
	fileCompanionNoteOCRStateHook = func() (bool, bool) { return false, false }
	fileCompanionNoteModelHook = func(fileCompanionNoteModelRequest) *IMAgentResponse {
		return &IMAgentResponse{Text: "## 概要\n写出来\n## 章节\n00:00 开始\n## 要点\n00:00 一点\n## 金句\n00:00 「话」\n## 不确定的地方\n无\n"}
	}
	if err := app.SendFileCompanionWebMessage(FileCompanionWebTurnRequest{
		SessionID: opened.SessionID, Text: "记笔记", Purpose: "video-note", RequestID: "req-next", RectW: 960, RectH: 540,
	}); err != nil {
		t.Fatal(err)
	}
	body := readWebNote(t, app, opened.SessionID)
	if !strings.Contains(body, "写出来") {
		t.Fatalf("later note = %s", body)
	}
}

func TestFileCompanionNoteCancelStaysOnItsRequest(t *testing.T) {
	app := &App{}
	const session = "sess-note"

	ctxLive, cancelLive := app.fileCompanionBeginNote(session, context.Background(), "req-live")
	app.fileCompanionMarkNoteStopped(session, "req-other")
	if ctxLive.Err() != nil {
		t.Fatal("cancel of another request stopped the live note")
	}
	if !noteStopContains(app, session, "req-other") {
		t.Fatal("cancel of another request was forgotten")
	}
	if noteStopContains(app, session, "req-live") {
		t.Fatal("the live note was marked stopped")
	}
	ctxOther, cancelOther := app.fileCompanionBeginNote(session, context.Background(), "req-other")
	if ctxOther.Err() == nil {
		t.Fatal("late begin of the cancelled request ran")
	}
	if ctxLive.Err() != nil {
		t.Fatal("late begin replaced the live note")
	}
	app.fileCompanionEndNote(session, "req-other", cancelOther)
	st := app.companionRuntime()
	st.noteMu.Lock()
	live := ""
	if st.noteCancelID != nil {
		live = st.noteCancelID[session]
	}
	st.noteMu.Unlock()
	if live != "req-live" {
		t.Fatalf("live id = %q", live)
	}

	app.fileCompanionMarkNoteStopped(session, "req-live")
	if ctxLive.Err() == nil {
		t.Fatal("matching cancel left the live note running")
	}
	if noteStopContains(app, session, "req-live") || noteStopContains(app, session, "req-other") {
		t.Fatal("matching cancel left a stop mark")
	}
	app.fileCompanionEndNote(session, "req-live", cancelLive)

	app.fileCompanionMarkNoteStopped(session, "req-a")
	app.fileCompanionMarkNoteStopped(session, "req-b")
	ctxA, cancelA := app.fileCompanionBeginNote(session, context.Background(), "req-a")
	ctxB, cancelB := app.fileCompanionBeginNote(session, context.Background(), "req-b")
	if ctxA.Err() == nil || ctxB.Err() == nil {
		t.Fatal("a second cancel erased the first")
	}
	ctxC, cancelC := app.fileCompanionBeginNote(session, context.Background(), "req-c")
	if ctxC.Err() != nil {
		t.Fatal("two cancels stopped a later request")
	}
	app.fileCompanionEndNote(session, "req-a", cancelA)
	app.fileCompanionEndNote(session, "req-b", cancelB)
	if ctxC.Err() != nil {
		t.Fatal("ending a cancelled request stopped the later note")
	}
	app.fileCompanionEndNote(session, "req-c", cancelC)

	app.fileCompanionMarkNoteStopped(session, "req-early")
	ctxEarly, cancelEarly := app.fileCompanionBeginNote(session, context.Background(), "req-early")
	if ctxEarly.Err() == nil {
		t.Fatal("pre-begin cancel did not stop that request")
	}
	ctxNext, cancelNext := app.fileCompanionBeginNote(session, context.Background(), "req-next")
	if ctxNext.Err() != nil {
		t.Fatal("pre-begin cancel stopped a later request")
	}
	app.fileCompanionEndNote(session, "req-early", cancelEarly)
	if ctxNext.Err() != nil {
		t.Fatal("ending the stopped request cancelled the later note")
	}
	st.noteMu.Lock()
	live = ""
	if st.noteCancelID != nil {
		live = st.noteCancelID[session]
	}
	hasCancel := st.noteCancel != nil && st.noteCancel[session] != nil
	st.noteMu.Unlock()
	if live != "req-next" || !hasCancel {
		t.Fatalf("later note lost: id=%q cancel=%v", live, hasCancel)
	}
	app.fileCompanionEndNote(session, "req-next", cancelNext)

	app.fileCompanionMarkNoteStopped(session, "req-armed")
	ctxKeep, cancelKeep := app.fileCompanionBeginNote(session, context.Background(), "req-keep")
	ctxArmed, cancelArmed := app.fileCompanionBeginNote(session, context.Background(), "req-armed")
	if ctxArmed.Err() == nil {
		t.Fatal("armed request started")
	}
	if ctxKeep.Err() != nil {
		t.Fatal("armed request replaced the live note")
	}
	app.fileCompanionEndNote(session, "req-armed", cancelArmed)
	if ctxKeep.Err() != nil {
		t.Fatal("ending the armed request cancelled the live note")
	}
	app.fileCompanionEndNote(session, "req-keep", cancelKeep)
	st.noteMu.Lock()
	_, still := st.noteCancel[session]
	st.noteMu.Unlock()
	if still {
		t.Fatal("ending the live note left its cancel")
	}

	ctxAny, cancelAny := app.fileCompanionBeginNote(session, context.Background(), "req-any")
	app.fileCompanionMarkNoteStopped(session, "")
	if ctxAny.Err() == nil {
		t.Fatal("empty request id left the live note running")
	}
	if noteStopContains(app, session, "req-any") {
		t.Fatal("empty request id armed a note stop")
	}
	app.fileCompanionEndNote(session, "req-any", cancelAny)
}

func noteStopContains(app *App, sessionID, requestID string) bool {
	st := app.companionRuntime()
	st.noteMu.Lock()
	defer st.noteMu.Unlock()
	_, ok := st.noteStopped[sessionID][requestID]
	return ok
}

func TestFileCompanionWebCancelLeavesAnotherNote(t *testing.T) {
	app := newVideoNoteTestApp(t)
	t.Cleanup(func() {
		if app.imHandler != nil && app.imHandler.memory != nil {
			app.imHandler.memory.Stop()
		}
	})
	opened, err := app.FileCompanionOpenWebPage("https://example.com/watch")
	if err != nil {
		t.Fatal(err)
	}
	sessionID := opened.SessionID
	noteCtx, noteCancel := app.fileCompanionBeginNote(sessionID, context.Background(), "req-note")
	defer app.fileCompanionEndNote(sessionID, "req-note", noteCancel)
	capCtx, capCancel := app.fileCompanionBeginWebCapture(sessionID, noteCtx, 3, "req-note")
	defer app.fileCompanionEndWebCapture(sessionID, "req-note", capCancel)
	app.fileCompanionSetWebNotePhase(sessionID, webNotePhaseRecording)
	defer app.fileCompanionSetWebNotePhase(sessionID, webNotePhaseIdle)

	if err := app.CancelFileCompanionWebGeneration(sessionID, "req-chat"); err != nil {
		t.Fatal(err)
	}
	if noteCtx.Err() != nil || capCtx.Err() != nil {
		t.Fatal("chat cancel stopped the recording note")
	}
	if !noteStopContains(app, sessionID, "req-chat") {
		t.Fatal("chat cancel was forgotten")
	}
	if noteStopContains(app, sessionID, "req-note") {
		t.Fatal("chat cancel marked the live note")
	}
	if err := app.CancelFileCompanionWebGeneration(sessionID, "req-note"); err != nil {
		t.Fatal(err)
	}
	if capCtx.Err() == nil {
		t.Fatal("note cancel left the recording running")
	}
	if noteCtx.Err() != nil {
		t.Fatal("recording cancel dropped the note")
	}
	app.fileCompanionSetWebNotePhase(sessionID, webNotePhaseWriting)
	if err := app.CancelFileCompanionWebGeneration(sessionID, "req-chat"); err != nil {
		t.Fatal(err)
	}
	if noteCtx.Err() != nil {
		t.Fatal("chat cancel during writing dropped the note")
	}
	if err := app.CancelFileCompanionWebGeneration(sessionID, "req-note"); err != nil {
		t.Fatal(err)
	}
	if noteCtx.Err() == nil {
		t.Fatal("writing cancel left the note running")
	}
}

func TestFileCompanionStoppedWebNoteLeavesTheLiveRecording(t *testing.T) {
	app := newVideoNoteTestApp(t)
	t.Cleanup(func() {
		if app.imHandler != nil && app.imHandler.memory != nil {
			app.imHandler.memory.Stop()
		}
	})
	opened, err := app.FileCompanionOpenWebPage("https://example.com/watch")
	if err != nil {
		t.Fatal(err)
	}
	sessionID := opened.SessionID
	liveCtx, liveCancel := app.fileCompanionBeginNote(sessionID, context.Background(), "req-live")
	defer app.fileCompanionEndNote(sessionID, "req-live", liveCancel)
	capCtx, capCancel := app.fileCompanionBeginWebCapture(sessionID, liveCtx, 4, "req-live")
	defer app.fileCompanionEndWebCapture(sessionID, "req-live", capCancel)
	app.fileCompanionSetWebNotePhase(sessionID, webNotePhaseRecording)
	defer app.fileCompanionSetWebNotePhase(sessionID, webNotePhaseIdle)
	work := filepath.Join(app.GetDataDir(), "file-companion", "work", sessionID)
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(work, "keep.txt")
	if err := os.WriteFile(keep, []byte("live"), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := fileCompanionWebCaptureHook
	fileCompanionWebCaptureHook = func() ([]webNoteShot, []byte, bool) {
		t.Error("stopped note started a capture")
		return nil, nil, true
	}
	t.Cleanup(func() { fileCompanionWebCaptureHook = prev })
	app.fileCompanionMarkNoteStopped(sessionID, "req-late")
	err = app.FileCompanionWebNote(FileCompanionWebNoteRequest{
		URL: "https://example.com/watch", SessionID: sessionID, RequestID: "req-late", RectW: 960, RectH: 540, CaptureEpoch: 9,
	})
	if err == nil || !strings.Contains(err.Error(), fileCompanionErrNoteCancelled) {
		t.Fatalf("stopped note = %v", err)
	}
	if liveCtx.Err() != nil || capCtx.Err() != nil {
		t.Fatal("stopped note tore down the live recording")
	}
	if phase := app.fileCompanionWebNotePhase(sessionID); phase != webNotePhaseRecording {
		t.Fatalf("phase = %d", phase)
	}
	body, err := os.ReadFile(keep)
	if err != nil || string(body) != "live" {
		t.Fatalf("work file = %q %v", body, err)
	}
	canonical := filepath.Join(t.TempDir(), "clip.mp4")
	localSession := fileCompanionSessionID(canonical)
	localCtx, localCancel := app.fileCompanionBeginNote(localSession, context.Background(), "req-local-live")
	defer app.fileCompanionEndNote(localSession, "req-local-live", localCancel)
	localWork := filepath.Join(app.GetDataDir(), "file-companion", "work", localSession)
	if err := os.MkdirAll(localWork, 0o755); err != nil {
		t.Fatal(err)
	}
	localKeep := filepath.Join(localWork, "keep.txt")
	if err := os.WriteFile(localKeep, []byte("live"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.fileCompanionMarkNoteStopped(localSession, "req-local")
	err = app.fileCompanionRunVideoNote(canonical, canonical, FileCompanionTurnRequest{RequestID: "req-local", Text: "记笔记"})
	if err == nil || !strings.Contains(err.Error(), fileCompanionErrNoteCancelled) {
		t.Fatalf("stopped local note = %v", err)
	}
	if localCtx.Err() != nil {
		t.Fatal("stopped local note cancelled the live note")
	}
	body, err = os.ReadFile(localKeep)
	if err != nil || string(body) != "live" {
		t.Fatalf("local work file = %q %v", body, err)
	}
}

func TestFileCompanionEndWebCaptureKeepsTheNewerAttempt(t *testing.T) {
	app := &App{}
	const session = "sess-cap"
	ctx1, cancel1 := app.fileCompanionBeginWebCapture(session, context.Background(), 1, "req-1")
	ctx2, cancel2 := app.fileCompanionBeginWebCapture(session, context.Background(), 2, "req-2")
	if ctx1.Err() == nil {
		t.Fatal("the new capture left the previous one running")
	}
	app.fileCompanionEndWebCapture(session, "req-1", cancel1)
	if ctx2.Err() != nil {
		t.Fatal("ending the previous capture cancelled the new one")
	}
	if _, matched := app.fileCompanionStopWebCapture(session, 2); !matched || ctx2.Err() == nil {
		t.Fatal("stop missed the live capture")
	}
	app.fileCompanionEndWebCapture(session, "req-2", cancel2)
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

func readWebNote(t *testing.T, app *App, sessionID string) string {
	t.Helper()
	body, err := os.ReadFile(app.fileCompanionFindNoteMarkdown(sessionID))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
