package guiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestPetDocumentChoice(t *testing.T) {
	cases := []struct {
		text   string
		choice string
		target string
	}{
		{"放进移动文稿库", "library", ""},
		{"保存到手机", "library", ""},
		{"发给聊天", "im", ""},
		{"发到产品群", "im", "产品群"},
		{"发到产品群吧", "im", "产品群"},
		{"保存到产品群", "im", "产品群"},
		{"保存到一半了", "", ""},
		{"发到这里", "im", ""},
		{"保存到手机上", "library", ""},
		{"放到文稿库", "library", ""},
		{"放到产品群", "im", "产品群"},
		{"发给聊天记录群", "im", "聊天记录群"},
		{"发到文稿库", "library", ""},
		{"发给别人", "im", ""},
		{"先留着吧", "decline", ""},
		{"先保存着吧", "decline", ""},
		{"先保存到文稿库", "library", ""},
		{"发到微信家族群", "im", "微信家族群"},
		{"不要发，放进文稿库", "library", ""},
		{"不用放文稿库，发到产品群", "im", "产品群"},
		{"聊天记录群", "", ""},
		{"我们聊天吧", "", ""},
		{"今天天气不错", "", ""},
	}
	for _, tc := range cases {
		choice, target := petDocumentChoice(tc.text)
		if choice != tc.choice || target != tc.target {
			t.Fatalf("%q -> %q %q, want %q %q", tc.text, choice, target, tc.choice, tc.target)
		}
	}
}

func TestPetDocumentOfferAsksBeforeLibraryOrIM(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "capture"
	s.hooks.loadConfig = func() (corelib.AppConfig, error) {
		return corelib.AppConfig{PetConversationMode: "voice-turn", PetReadbackMode: "summary", PetVoiceReadback: true}, nil
	}
	s.hooks.openMic = func() (petMic, error) { return nil, errPetCompanionUnavailable }
	s.hooks.runTurn = func(text, follow string, allowTools bool) (petTurnOutcome, error) {
		return petTurnOutcome{Text: "周报：本周完成了登录页。", Files: []string{`C:\docs\weekly.md`}}, nil
	}
	s.hooks.setState = func(string) {}
	var spoken []string
	s.hooks.speak = func(text string, _ uint64) { spoken = append(spoken, text) }
	var saved, sentTarget string
	s.hooks.saveDocument = func(path, title, body string) (string, error) {
		saved = path
		return "已经放进移动文稿库了。", nil
	}
	s.hooks.sendDocument = func(path, title, body, target string) (string, error) {
		sentTarget = target
		return "已经发到聊天了。", nil
	}

	s.acceptText("写一份周报")
	if s.phase != "docdest" {
		t.Fatalf("phase = %q, want docdest", s.phase)
	}
	if len(spoken) != 1 || !strings.Contains(spoken[0], "移动文稿库") {
		t.Fatalf("spoken = %#v", spoken)
	}
	if saved != "" || sentTarget != "" {
		t.Fatalf("document moved before the user chose: saved=%q sent=%q", saved, sentTarget)
	}

	s.acceptText("放进文稿库")
	if saved != `C:\docs\weekly.md` {
		t.Fatalf("saved = %q", saved)
	}
	if sentTarget != "" {
		t.Fatalf("library choice also sent to %q", sentTarget)
	}
}

func TestPetDocumentIMAsksForChatName(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "docdest"
	s.pendingDoc = petPendingDocument{Path: `C:\docs\weekly.md`, Title: "周报"}
	s.hooks.loadConfig = func() (corelib.AppConfig, error) {
		return corelib.AppConfig{PetConversationMode: "continuous", PetReadbackMode: "summary", PetVoiceReadback: true}, nil
	}
	s.hooks.setState = func(string) {}
	var spoken []string
	s.hooks.speak = func(text string, _ uint64) { spoken = append(spoken, text) }
	var sent string
	s.hooks.sendDocument = func(path, title, body, target string) (string, error) {
		sent = target
		return "已经发到聊天了。", nil
	}

	s.acceptText("发给聊天")
	if s.phase != "docim" {
		t.Fatalf("phase = %q, want docim", s.phase)
	}
	if sent != "" {
		t.Fatalf("sent before a chat name: %q", sent)
	}

	s.acceptText("产品群")
	if sent != "产品群" {
		t.Fatalf("sent = %q", sent)
	}
}

func TestPetDocumentIgnoresNonDocuments(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "capture"
	s.hooks.loadConfig = func() (corelib.AppConfig, error) {
		return corelib.AppConfig{PetConversationMode: "voice-turn", PetReadbackMode: "summary", PetVoiceReadback: true}, nil
	}
	s.hooks.runTurn = func(text, follow string, allowTools bool) (petTurnOutcome, error) {
		return petTurnOutcome{Text: "截图好了。", Files: []string{`C:\docs\screen.png`}}, nil
	}
	s.hooks.setState = func(string) {}
	var spoken []string
	s.hooks.speak = func(text string, _ uint64) { spoken = append(spoken, text) }

	s.acceptText("看看屏幕")
	if s.phase == "docdest" || s.phase == "docim" {
		t.Fatalf("screenshot entered document choice, phase %q spoken %#v", s.phase, spoken)
	}
	for _, line := range spoken {
		if strings.Contains(line, "文稿库") {
			t.Fatalf("asked about the library for a screenshot: %#v", spoken)
		}
	}
}

func TestPetDocumentSendFailureKeepsTheFile(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "docdest"
	s.pendingDoc = petPendingDocument{Path: `C:\docs\weekly.md`, Title: "周报"}
	s.hooks.loadConfig = func() (corelib.AppConfig, error) {
		return corelib.AppConfig{PetConversationMode: "voice-turn", PetReadbackMode: "summary", PetVoiceReadback: true}, nil
	}
	s.hooks.setState = func(string) {}
	var spoken []string
	s.hooks.speak = func(text string, _ uint64) { spoken = append(spoken, text) }
	s.hooks.sendDocument = func(path, title, body, target string) (string, error) {
		return "", errPetCompanionTurn("找不到群")
	}

	s.acceptText("发到产品群")
	if s.pendingDoc.Path != `C:\docs\weekly.md` {
		t.Fatalf("pending = %+v", s.pendingDoc)
	}
	if s.phase != "docdest" {
		t.Fatalf("phase = %q", s.phase)
	}
	if len(spoken) != 1 || !strings.Contains(spoken[0], "没发出去") {
		t.Fatalf("spoken = %#v", spoken)
	}
}

func TestPetDocumentUnclearTwiceLeavesItLocal(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "capture"
	s.hooks.loadConfig = func() (corelib.AppConfig, error) {
		return corelib.AppConfig{PetConversationMode: "voice-turn", PetReadbackMode: "summary", PetVoiceReadback: true}, nil
	}
	s.hooks.runTurn = func(text, follow string, allowTools bool) (petTurnOutcome, error) {
		return petTurnOutcome{Text: "周报：本周完成了登录页。", Files: []string{`C:\docs\weekly.md`}}, nil
	}
	s.hooks.setState = func(string) {}
	s.hooks.speak = func(string, uint64) {}
	s.hooks.saveDocument = func(path, title, body string) (string, error) {
		t.Fatal("unclear answer saved the document")
		return "", nil
	}
	s.hooks.sendDocument = func(path, title, body, target string) (string, error) {
		t.Fatal("unclear answer sent the document")
		return "", nil
	}

	s.acceptText("写一份周报")
	s.acceptText("今天天气不错")
	s.acceptText("嗯")
	if s.phase == "docdest" || s.phase == "docim" {
		t.Fatalf("phase = %q", s.phase)
	}
	if s.pendingDoc.Path != "" || len(s.pendingDoc.Paths) > 0 {
		t.Fatalf("pending = %+v", s.pendingDoc)
	}
}

func TestPetDocumentChatNameIsNotAChannelWord(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "docim"
	s.pendingDoc = petPendingDocument{Path: `C:\docs\weekly.md`, Title: "周报"}
	s.hooks.loadConfig = func() (corelib.AppConfig, error) {
		return corelib.AppConfig{PetConversationMode: "voice-turn", PetReadbackMode: "summary", PetVoiceReadback: true}, nil
	}
	s.hooks.setState = func(string) {}
	s.hooks.speak = func(string, uint64) {}
	var sent string
	s.hooks.sendDocument = func(path, title, body, target string) (string, error) {
		sent = target
		return "已经发到聊天了。", nil
	}

	s.acceptText("聊天记录群")
	if sent != "聊天记录群" {
		t.Fatalf("sent = %q", sent)
	}
}

func TestPetDocumentStopDropsQuietly(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "docdest"
	s.pendingDoc = petPendingDocument{Path: `C:\docs\weekly.md`, Title: "周报"}
	s.hooks.setState = func(string) {}
	var spoken []string
	s.hooks.speak = func(text string, _ uint64) { spoken = append(spoken, text) }

	s.acceptText("停下")
	if s.phase != "wake" || s.pendingDoc.Path != "" {
		t.Fatalf("phase %q pending %+v", s.phase, s.pendingDoc)
	}
	if len(spoken) != 0 {
		t.Fatalf("stop announced itself: %#v", spoken)
	}
}

func TestPetDocumentTimeoutLeavesItQuietly(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "docdest"
	s.pendingDoc = petPendingDocument{Path: `C:\docs\weekly.md`, Title: "周报"}
	s.hooks.loadConfig = func() (corelib.AppConfig, error) {
		return corelib.AppConfig{PetContinuousTimeout: 1, PetReadbackMode: "summary", PetVoiceReadback: true}, nil
	}
	s.hooks.setState = func(string) {}
	var spoken []string
	s.hooks.speak = func(text string, _ uint64) { spoken = append(spoken, text) }

	s.askDocumentDestination("")
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		phase, path := s.phase, s.pendingDoc.Path
		s.mu.Unlock()
		if phase == "wake" && path == "" {
			for _, line := range spoken {
				if strings.Contains(line, "留在") {
					t.Fatalf("timeout announced itself: %#v", spoken)
				}
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("phase %q pending %+v spoken %#v", s.phase, s.pendingDoc, spoken)
}

func TestPetSplitChatTarget(t *testing.T) {
	channel, group := petSplitChatTarget("微信家族群")
	if channel != "微信" || group != "家族群" {
		t.Fatalf("wechat = %q %q", channel, group)
	}
	channel, group = petSplitChatTarget("蓝信里的产品群")
	if channel != "蓝信" || group != "产品群" {
		t.Fatalf("lansenger = %q %q", channel, group)
	}
	channel, group = petSplitChatTarget("微信里面的产品群")
	if channel != "微信" || group != "产品群" {
		t.Fatalf("inside = %q %q", channel, group)
	}
	channel, group = petSplitChatTarget("产品群")
	if channel != "" || group != "产品群" {
		t.Fatalf("plain = %q %q", channel, group)
	}
	if _, group = petSplitChatTarget("qq群"); group != "" {
		t.Fatalf("qq群 group = %q", group)
	}
}

func TestPetPromptEchoIsNotAnAnswer(t *testing.T) {
	if !petIsPromptEcho(petDocumentQuestion, petDocumentQuestion) {
		t.Fatal("the question itself should be ignored")
	}
	if petIsPromptEcho("发给聊天", petDocumentQuestion) || petIsPromptEcho("放进文稿库", petDocumentQuestion) {
		t.Fatal("a real choice was treated as echo")
	}

	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "docdest"
	s.pendingDoc = petPendingDocument{Path: `C:\docs\weekly.md`, Title: "周报"}
	s.promptLine = petDocumentQuestion
	s.promptUntil = time.Now().Add(5 * time.Second)
	s.hooks.loadConfig = func() (corelib.AppConfig, error) {
		return corelib.AppConfig{PetConversationMode: "voice-turn", PetReadbackMode: "summary", PetVoiceReadback: true}, nil
	}
	s.hooks.setState = func(string) {}
	s.hooks.speak = func(string, uint64) {}
	s.hooks.transcribe = func([]int16) (string, error) { return petDocumentQuestion, nil }
	s.hooks.saveDocument = func(string, string, string) (string, error) {
		t.Fatal("echo saved the document")
		return "", nil
	}
	s.hooks.sendDocument = func(string, string, string, string) (string, error) {
		t.Fatal("echo sent the document")
		return "", nil
	}
	pcm := make([]int16, 4000)
	for i := range pcm {
		pcm[i] = 8000
	}
	s.onUtterance(petAudioEvent{utterance: pcm, rms: 0.2}, time.Now())
	if s.phase != "docdest" || s.pendingDoc.Path == "" {
		t.Fatalf("echo changed the wait: phase %q pending %+v", s.phase, s.pendingDoc)
	}
}

func TestPetDocumentRequestDoesNotMatchPassword(t *testing.T) {
	reply := strings.Repeat("密", 40)
	if petShouldOfferDocument("改一下password", reply, nil) {
		t.Fatal("password looked like a document request")
	}
	if !petShouldOfferDocument("写一份周报", reply, nil) {
		t.Fatal("a weekly report should be offered")
	}
	if petDocumentTitle("帮我写一份周报") != "周报" {
		t.Fatalf("title = %q", petDocumentTitle("帮我写一份周报"))
	}
}

func TestPetIMSendSucceeded(t *testing.T) {
	if !petIMSendSucceeded("已发送文件到 蓝信→产品群\n文件名: weekly.md\n大小: 12 字节") {
		t.Fatal("real send acknowledgement was treated as failure")
	}
	if petIMSendSucceeded("找不到名为产品群的会话") {
		t.Fatal("lookup failure was treated as success")
	}
}

func TestPetCompanionBlocksUnaskedIMSend(t *testing.T) {
	if !petCompanionOutboundBlocked(petCompanionUserID, "send_file", `{"path":"weekly.md"}`, "写一份周报") {
		t.Fatal("generating a document must not send the file")
	}
	if !petCompanionOutboundBlocked(petCompanionUserID, "send_file", `{"path":"weekly.md"}`, "写一份周报发到产品群") {
		t.Fatal("naming a chat while writing still has to ask")
	}
	if !petCompanionOutboundBlocked(petCompanionUserID, "send_file", `{"path":"weekly.pdf"}`, "生成pdf发到产品群") {
		t.Fatal("generating a pdf must not send it before the user chooses")
	}
	if petCompanionOutboundBlocked(petCompanionUserID, "send_file", `{"path":"weekly.pdf"}`, "把这个pdf发到产品群") {
		t.Fatal("sending an existing pdf is not document generation")
	}
	if !petCompanionOutboundBlocked(petCompanionUserID, "im_message", `{"action":"send_file","path":"weekly.md"}`, "写一份周报") {
		t.Fatal("im_message send_file must wait for the user")
	}
	if petCompanionOutboundBlocked(petCompanionUserID, "im_message", `{"action":"send","text":"开会了"}`, "发到产品群说开会了") {
		t.Fatal("an explicit text send is not a document choice")
	}
	if petCompanionOutboundBlocked(petCompanionUserID, "send_file", `{"path":"shot.png"}`, "把截图发到产品群") {
		t.Fatal("an explicit file send should be allowed")
	}
	setPetCompanionDeliveryArmed(petCompanionUserID, true)
	if petCompanionOutboundBlocked(petCompanionUserID, "send_file", `{"path":"weekly.md"}`, "写一份周报") {
		t.Fatal("an answered send should be allowed")
	}
	setPetCompanionDeliveryArmed(petCompanionUserID, false)
	if petCompanionOutboundBlocked("desktop-user", "im_message", `{"action":"send_file"}`, "写一份周报") {
		t.Fatal("main assistant sends are unrelated")
	}
}

func TestPetDocumentMissedTranscriptReplaysTheQuestion(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "docdest"
	s.pendingDoc = petPendingDocument{Path: `C:\docs\weekly.md`, Title: "周报"}
	s.promptLine = petDocumentQuestion
	s.hooks.loadConfig = func() (corelib.AppConfig, error) {
		return corelib.AppConfig{PetConversationMode: "voice-turn", PetReadbackMode: "summary", PetVoiceReadback: true}, nil
	}
	s.hooks.setState = func(string) {}
	var spoken []string
	s.hooks.speak = func(text string, _ uint64) { spoken = append(spoken, text) }
	s.hooks.transcribe = func([]int16) (string, error) { return "", errPetCompanionTurn("no words") }
	pcm := make([]int16, 4000)
	for i := range pcm {
		pcm[i] = 8000
	}
	s.onUtterance(petAudioEvent{utterance: pcm, rms: 0.2}, time.Now())
	if s.phase != "docdest" || s.pendingDoc.Path == "" {
		t.Fatalf("missed transcript left the question: phase %q pending %+v", s.phase, s.pendingDoc)
	}
	if len(spoken) != 1 || strings.Contains(spoken[0], "没听清") || !strings.Contains(spoken[0], "文稿库") {
		t.Fatalf("spoken = %#v", spoken)
	}
	s.onUtterance(petAudioEvent{utterance: pcm, rms: 0.2}, time.Now())
	if len(spoken) != 1 {
		t.Fatalf("replayed more than once: %#v", spoken)
	}
}

func TestPetDocumentQuestionBargeInStopsSpeech(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.phase = "docdest"
	s.promptUntil = time.Now().Add(2 * time.Second)
	stopped := false
	s.hooks.stopSpeak = func() { stopped = true }
	var state string
	s.hooks.setState = func(next string) { state = next }

	s.onSpeechStart()
	if !stopped {
		t.Fatal("barge-in left the question playing")
	}
	if s.phase != "docdest" {
		t.Fatalf("phase = %q", s.phase)
	}
	if state != "listening" {
		t.Fatalf("state = %q", state)
	}
}

func TestReadPetCompanionTranscript(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pet_companion_conversation.json")
	body := `{
	  "sessions": {
	    "desktop-user": {"entries": [{"role":"user","content":"主窗口的话"}]},
	    "desktop-pet": {"entries": [
	      {"role":"user","content":"写一份周报"},
	      {"role":"assistant","content":[{"type":"text","text":"周报好了。"}]},
	      {"role":"tool","content":"ignored"}
	    ]}
	  }
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := readPetCompanionTranscript(path)
	if strings.Contains(got, "主窗口") || strings.Contains(got, "ignored") {
		t.Fatalf("transcript leaked other sessions: %q", got)
	}
	if got != "你：写一份周报\n码卡龙：周报好了。" {
		t.Fatalf("transcript = %q", got)
	}
}

func TestPetConfirmEchoDoesNotRun(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "capture"
	s.hooks.loadConfig = func() (corelib.AppConfig, error) {
		return corelib.AppConfig{PetConversationMode: "voice-turn", PetReadbackMode: "summary", PetVoiceReadback: true, PetContinuousTimeout: 30}, nil
	}
	s.hooks.setState = func(string) {}
	s.hooks.speak = func(string, uint64) {}
	var ran string
	s.hooks.runTurn = func(text, _ string, _ bool) (petTurnOutcome, error) {
		ran = text
		return petTurnOutcome{Text: "删掉了。"}, nil
	}

	s.acceptText("把这个文件删掉")
	if s.phase != "confirm" || ran != "" {
		t.Fatalf("ran %q phase %q", ran, s.phase)
	}
	s.hooks.transcribe = func([]int16) (string, error) { return "删掉这个，对吗？", nil }
	s.onUtterance(petAudioEvent{utterance: make([]int16, 8000), rms: 0.05}, time.Now())
	if ran != "" || s.phase != "confirm" {
		t.Fatalf("echo ran %q phase %q", ran, s.phase)
	}
	s.acceptText("好")
	if ran != "把这个文件删掉" {
		t.Fatalf("yes did not run, ran %q", ran)
	}
}

func TestPetConfirmTimeoutDropsQuietly(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "capture"
	s.hooks.loadConfig = func() (corelib.AppConfig, error) {
		return corelib.AppConfig{PetConversationMode: "voice-turn", PetReadbackMode: "summary", PetVoiceReadback: true, PetContinuousTimeout: 1}, nil
	}
	s.hooks.setState = func(string) {}
	var spoken []string
	s.hooks.speak = func(text string, _ uint64) { spoken = append(spoken, text) }
	var ran string
	s.hooks.runTurn = func(text, _ string, _ bool) (petTurnOutcome, error) {
		ran = text
		return petTurnOutcome{Text: "发出去了。"}, nil
	}

	s.acceptText("发给小林")
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		phase, pending := s.phase, s.pending
		s.mu.Unlock()
		if phase == "wake" && pending == "" {
			if ran != "" {
				t.Fatalf("timeout still sent: %q", ran)
			}
			for _, line := range spoken {
				if strings.Contains(line, "先不做") {
					t.Fatalf("timeout announced itself: %#v", spoken)
				}
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("confirm stayed phase %q pending %q spoken %#v", s.phase, s.pending, spoken)
}

func TestPetWakePoseDiffersFromRecording(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "wake"
	var states []string
	s.hooks.setState = func(next string) { states = append(states, next) }

	s.showPhasePose()
	s.enterCapture(true)
	s.quietMiss()
	if len(states) < 3 || states[0] != "alert" || states[1] != "listening" || states[2] != "alert" {
		t.Fatalf("poses = %#v", states)
	}
}

func TestPetStopDropsUnansweredDocument(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "docdest"
	s.pendingDoc = petPendingDocument{Path: `C:\docs\weekly.md`, Title: "周报"}
	s.promptLine = petDocumentQuestion
	s.pending = "写一份周报"
	var states []string
	s.hooks.setState = func(next string) { states = append(states, next) }
	s.hooks.stopSpeak = func() {}
	s.hooks.cancelTurn = func() {}

	s.Stop()
	if s.phase != "off" || s.pendingDoc.Path != "" || s.promptLine != "" || s.pending != "" {
		t.Fatalf("phase %q pending %q prompt %q doc %+v", s.phase, s.pending, s.promptLine, s.pendingDoc)
	}
	if len(states) != 1 || states[0] != "idle" {
		t.Fatalf("poses = %#v", states)
	}
}

func TestFloatingVoiceCompanionChanged(t *testing.T) {
	base := corelib.AppConfig{PetVoiceInput: false, PetConversationMode: "text-first"}
	if floatingVoiceCompanionChanged(base, base) {
		t.Fatal("unchanged config rearmed the mic")
	}
	voice := base
	voice.PetVoiceInput = true
	if !floatingVoiceCompanionChanged(base, voice) {
		t.Fatal("voice toggle did not rearm")
	}
	mode := base
	mode.PetConversationMode = "continuous"
	if !floatingVoiceCompanionChanged(base, mode) {
		t.Fatal("conversation mode did not rearm")
	}
	quiet := base
	quiet.PetQuietMode = true
	if floatingVoiceCompanionChanged(base, quiet) {
		t.Fatal("quiet mode closed the mic")
	}
}
