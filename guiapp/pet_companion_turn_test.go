package guiapp

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestPetFalseWakeAndSilenceStayQuiet(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "wake"
	s.hooks.setState = func(string) {}
	s.hooks.loadConfig = func() (corelib.AppConfig, error) {
		return corelib.AppConfig{PetConversationMode: "voice-turn", PetReadbackMode: "summary", PetVoiceReadback: true}, nil
	}
	var spoken []string
	s.hooks.speak = func(text string, _ uint64) { spoken = append(spoken, text) }
	s.hooks.spotWake = func([]int16) (bool, error) { return false, nil }

	s.onUtterance(petAudioEvent{utterance: make([]int16, 8000), rms: 0.05}, time.Now())
	if s.phase != "wake" || len(spoken) != 0 {
		t.Fatalf("false wake spoke phase %q %#v", s.phase, spoken)
	}

	s.hooks.spotWake = func([]int16) (bool, error) { return true, nil }
	s.onUtterance(petAudioEvent{utterance: make([]int16, 8000), rms: 0.05}, time.Now())
	if s.phase != "capture" || len(spoken) != 1 || spoken[0] != petWakeAck {
		t.Fatalf("wake phase %q spoken %#v", s.phase, spoken)
	}

	s.onUtterance(petAudioEvent{utterance: make([]int16, 800), rms: 0.001}, time.Now())
	if s.phase != "wake" || len(spoken) != 1 {
		t.Fatalf("silence after wake spoke phase %q %#v", s.phase, spoken)
	}
}

func TestPetWakeWithCommandSkipsTheGreeting(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "wake"
	s.lastWakeText = "码卡龙，查一下天气"
	s.hooks.setState = func(string) {}
	s.hooks.loadConfig = func() (corelib.AppConfig, error) {
		return corelib.AppConfig{PetConversationMode: "voice-turn", PetReadbackMode: "summary", PetVoiceReadback: true}, nil
	}
	var spoken []string
	s.hooks.speak = func(text string, _ uint64) { spoken = append(spoken, text) }
	var ran string
	s.hooks.runTurn = func(text, _ string, _ bool) (petTurnOutcome, error) {
		ran = text
		return petTurnOutcome{Text: "查到了。"}, nil
	}
	s.hooks.spotWake = func([]int16) (bool, error) { return true, nil }

	s.onUtterance(petAudioEvent{utterance: make([]int16, 8000), rms: 0.05}, time.Now())
	if ran != "查一下天气" {
		t.Fatalf("ran %q", ran)
	}
	for _, line := range spoken {
		if strings.Contains(line, "主人") {
			t.Fatalf("greeting covered the command: %#v", spoken)
		}
	}
}

func TestPetWakeListensBeforeRecognition(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "wake"
	var state string
	s.hooks.setState = func(next string) { state = next }

	s.onSpeechStart()
	if s.phase != "wake" || state != "listening" {
		t.Fatalf("phase %q state %q", s.phase, state)
	}
}

func TestPetPhraseCachePlaysWithoutSynthesizingAgain(t *testing.T) {
	rememberPetPhrase("default", petWakeAck, "cached-wav")
	t.Cleanup(func() { rememberPetPhrase("default", petWakeAck, "") })
	if got := lookupPetPhrase("default", petWakeAck); got != "cached-wav" {
		t.Fatalf("cache %q", got)
	}
	if !petIsStockPhrase(petWakeAck) || !petIsStockPhrase("我看一下") {
		t.Fatal("stock phrases were not marked for caching")
	}
}

func TestPetUnclearSpeechIsSaidOnceThenRests(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "capture"
	s.hooks.setState = func(string) {}
	s.hooks.loadConfig = func() (corelib.AppConfig, error) {
		return corelib.AppConfig{PetConversationMode: "continuous", PetReadbackMode: "summary", PetVoiceReadback: true}, nil
	}
	var spoken []string
	s.hooks.speak = func(text string, _ uint64) { spoken = append(spoken, text) }
	s.hooks.transcribe = func([]int16) (string, error) { return "", nil }
	heard := petAudioEvent{utterance: make([]int16, 8000), rms: 0.05}

	s.onUtterance(heard, time.Now())
	if s.phase != "wake" || len(spoken) != 1 || spoken[0] != "没听清" {
		t.Fatalf("phase %q spoken %#v", s.phase, spoken)
	}
	s.phase = "capture"
	s.onUtterance(heard, time.Now())
	if len(spoken) != 1 || s.phase != "wake" {
		t.Fatalf("asked again phase %q spoken %#v", s.phase, spoken)
	}
}

func TestPetUnclearSpeechRetriesOnceWhenAsked(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "capture"
	s.hooks.setState = func(string) {}
	s.hooks.loadConfig = func() (corelib.AppConfig, error) {
		return corelib.AppConfig{PetConversationMode: "voice-turn", PetReadbackMode: "summary", PetVoiceReadback: true, PetAutoRetryOnNoHear: true}, nil
	}
	var spoken []string
	s.hooks.speak = func(text string, _ uint64) { spoken = append(spoken, text) }
	s.hooks.transcribe = func([]int16) (string, error) { return "  ", nil }
	heard := petAudioEvent{utterance: make([]int16, 8000), rms: 0.05}

	s.onUtterance(heard, time.Now())
	if s.phase != "capture" || len(spoken) != 1 {
		t.Fatalf("retry phase %q spoken %#v", s.phase, spoken)
	}
	s.onUtterance(heard, time.Now())
	if s.phase != "wake" || len(spoken) != 1 {
		t.Fatalf("second miss phase %q spoken %#v", s.phase, spoken)
	}
}

func TestPetSegmenterKeepsMidPhrasePauses(t *testing.T) {
	seg := newPetSegmenter()
	if _, hit := seg.push(make([]int16, 160)); hit {
		t.Fatal("silence started an utterance")
	}
	ev, hit := seg.push(loudPetFrame(320))
	if !hit || !ev.speechStart {
		t.Fatal("speech onset was missed")
	}
	if _, hit = seg.push(make([]int16, 8000)); hit {
		t.Fatal("half-second pause split the phrase")
	}
	if _, hit = seg.push(loudPetFrame(320)); hit {
		t.Fatal("continued speech was committed early")
	}
	ev, hit = seg.push(make([]int16, petSampleRate))
	if !hit || ev.speechStart || len(ev.utterance) < 8000 {
		t.Fatalf("commit hit=%v start=%v samples=%d", hit, ev.speechStart, len(ev.utterance))
	}
}

func loudPetFrame(n int) []int16 {
	frame := make([]int16, n)
	for i := range frame {
		frame[i] = 4000
	}
	return frame
}

func TestPetDialogueIdleWaitsOutAnUtterance(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "dialogue"
	s.hooks.setState = func(string) {}
	s.hooks.loadConfig = func() (corelib.AppConfig, error) {
		return corelib.AppConfig{PetConversationMode: "continuous", PetContinuousTimeout: 1, PetReadbackMode: "summary", PetVoiceReadback: true}, nil
	}
	var spoken []string
	s.hooks.speak = func(text string, _ uint64) { spoken = append(spoken, text) }

	s.armDialogueTimeout()
	s.onSpeechStart()
	time.Sleep(1500 * time.Millisecond)
	if s.phase != "dialogue" {
		t.Fatalf("speech was cut by the idle timer, phase %q", s.phase)
	}
	if len(spoken) != 0 {
		t.Fatalf("idle timer spoke %#v", spoken)
	}
}

func TestPetDialogueIdleEndsQuietly(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "dialogue"
	s.hooks.setState = func(string) {}
	s.hooks.loadConfig = func() (corelib.AppConfig, error) {
		return corelib.AppConfig{PetConversationMode: "continuous", PetContinuousTimeout: 1, PetReadbackMode: "summary", PetVoiceReadback: true}, nil
	}
	var spoken []string
	s.hooks.speak = func(text string, _ uint64) { spoken = append(spoken, text) }

	s.armDialogueTimeout()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		phase := s.phase
		s.mu.Unlock()
		if phase == "wake" {
			if len(spoken) != 0 {
				t.Fatalf("idle ending spoke %#v", spoken)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("dialogue stayed open, phase %q", s.phase)
}

func TestPetGoodbyeAndVoiceTurnHandBackTheFloor(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "dialogue"
	s.hooks.setState = func(string) {}
	s.hooks.loadConfig = func() (corelib.AppConfig, error) {
		return corelib.AppConfig{PetConversationMode: "continuous", PetReadbackMode: "summary", PetVoiceReadback: true}, nil
	}
	var spoken []string
	s.hooks.speak = func(text string, _ uint64) { spoken = append(spoken, text) }

	s.acceptText("再见")
	if s.phase != "wake" || len(spoken) != 0 {
		t.Fatalf("goodbye phase %q spoken %#v", s.phase, spoken)
	}

	s.hooks.loadConfig = func() (corelib.AppConfig, error) {
		return corelib.AppConfig{PetConversationMode: "voice-turn", PetReadbackMode: "summary", PetVoiceReadback: true}, nil
	}
	s.phase = "capture"
	s.hooks.runTurn = func(string, string, bool) (petTurnOutcome, error) {
		return petTurnOutcome{Text: "查到了。"}, nil
	}
	s.acceptText("查一下天气")
	if s.phase != "wake" {
		t.Fatalf("voice-turn stayed in %q", s.phase)
	}
	joined := strings.Join(spoken, " ")
	if !strings.Contains(joined, "查到了") || strings.Contains(joined, "还需要什么") {
		t.Fatalf("report %#v", spoken)
	}
}

func TestPetBargeInStopsSpeechWithoutCancellingWork(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "speak"
	s.generation = 3
	cancelled := false
	stopped := false
	s.hooks.cancelTurn = func() { cancelled = true }
	s.hooks.stopSpeak = func() { stopped = true }
	s.hooks.setState = func(string) {}
	var spoken []string
	s.hooks.speak = func(text string, _ uint64) { spoken = append(spoken, text) }

	s.onSpeechStart()
	if !stopped || cancelled {
		t.Fatalf("stopped=%v cancelled=%v", stopped, cancelled)
	}
	if s.phase != "capture" || s.generation != 3 {
		t.Fatalf("phase %q generation %d", s.phase, s.generation)
	}
	for _, line := range spoken {
		if strings.Contains(line, "我停了") {
			t.Fatalf("barge-in announced itself: %#v", spoken)
		}
	}
}

func TestPetNoiseDuringWorkDoesNotCancel(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "think"
	s.generation = 2
	cancelled := false
	s.hooks.cancelTurn = func() { cancelled = true }
	s.hooks.stopSpeak = func() {}
	s.hooks.transcribe = func([]int16) (string, error) { return "嗯", nil }

	s.onUtterance(petAudioEvent{utterance: make([]int16, 8000), rms: 0.05}, time.Now())
	if cancelled || s.generation != 2 || s.phase != "think" {
		t.Fatalf("noise cancelled work cancelled=%v gen=%d phase=%q", cancelled, s.generation, s.phase)
	}
}

func TestPetStopDuringWorkCancelsQuietly(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "think"
	s.generation = 2
	cancelled := make(chan struct{}, 1)
	s.hooks.cancelTurn = func() { cancelled <- struct{}{} }
	s.hooks.stopSpeak = func() {}
	s.hooks.setState = func(string) {}
	var spoken []string
	s.hooks.speak = func(text string, _ uint64) { spoken = append(spoken, text) }
	s.hooks.transcribe = func([]int16) (string, error) { return "停下", nil }

	s.onUtterance(petAudioEvent{utterance: make([]int16, 8000), rms: 0.05}, time.Now())
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel the pet turn")
	}
	if s.generation == 2 {
		t.Fatalf("stop did not cancel cancelled=%v gen=%d", cancelled, s.generation)
	}
	if s.phase != "wake" {
		t.Fatalf("phase %q", s.phase)
	}
	for _, line := range spoken {
		if strings.Contains(line, "我停了") || strings.Contains(line, "停下") {
			t.Fatalf("stop announced itself: %#v", spoken)
		}
	}
}

func TestPetFollowUpUsesLastResultAndChatSkipsTools(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "dialogue"
	s.lastResult = "周报一共三段。"
	s.hooks.loadConfig = func() (corelib.AppConfig, error) {
		return corelib.AppConfig{PetConversationMode: "continuous", PetReadbackMode: "summary", PetVoiceReadback: true}, nil
	}
	s.hooks.setState = func(string) {}
	s.hooks.speak = func(string, uint64) {}
	var follow string
	var tools bool
	s.hooks.runTurn = func(text, got string, allow bool) (petTurnOutcome, error) {
		follow = got
		tools = allow
		return petTurnOutcome{Text: "好，缩短了。"}, nil
	}

	s.acceptText("再短一点")
	if follow != "周报一共三段。" || !tools {
		t.Fatalf("follow %q tools %v", follow, tools)
	}

	s.phase = "dialogue"
	s.hooks.runTurn = func(text, got string, allow bool) (petTurnOutcome, error) {
		follow = got
		tools = allow
		return petTurnOutcome{Text: "在呢。"}, nil
	}
	s.acceptText("你好")
	if tools {
		t.Fatal("chat turn was given tools")
	}
}

func TestPetWindowConfirmStopsWithoutTouchingTheMainSession(t *testing.T) {
	if !petMainWindowConfirm(&IMAgentResponse{Confirmation: &IMResponseConfirmation{Summary: "确认删除？"}}) {
		t.Fatal("click confirmation was treated as a spoken answer")
	}
	if !petMainWindowConfirm(&IMAgentResponse{DesktopUserControl: true}) {
		t.Fatal("desktop handoff was treated as a spoken answer")
	}
	if petMainWindowConfirm(&IMAgentResponse{Text: "查到了。"}) {
		t.Fatal("ordinary result asked for a window click")
	}

	h := &IMMessageHandler{confirmationStore: newAIConfirmationStore("")}
	h.confirmationStore.set(&pendingConfirmation{UserID: petCompanionUserID, ID: "pet", Status: confirmationStatusPending})
	h.confirmationStore.set(&pendingConfirmation{UserID: desktopUserID, ID: "main", Status: confirmationStatusPending})
	h.pendingAskUser.Store(petCompanionUserID, &pendingAskUserState{Question: "点一下"})
	h.dropPetWindowConfirm()
	if h.confirmationStore.get(petCompanionUserID) != nil {
		t.Fatal("pet confirmation stayed pending")
	}
	if _, still := h.pendingAskUser.Load(petCompanionUserID); still {
		t.Fatal("pet ask stayed pending")
	}
	if got := h.confirmationStore.get(desktopUserID); got == nil || got.ID != "main" {
		t.Fatal("main-window confirmation was cleared")
	}
}

func TestPetMotionSoundDucksWhileSpeaking(t *testing.T) {
	if !petMotionSoundHeard(true, false, false, "idle") {
		t.Fatal("idle motion sound was silenced")
	}
	for _, state := range []string{"speaking", "alert", "listening", "thinking"} {
		if petMotionSoundHeard(true, false, false, state) {
			t.Fatalf("state %s still played motion sound", state)
		}
	}
	if petMotionSoundHeard(true, true, false, "idle") {
		t.Fatal("quiet mode still played motion sound")
	}
}

func TestPetFailureSaysWhyAndResultSkipsBareDone(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "capture"
	s.hooks.loadConfig = func() (corelib.AppConfig, error) {
		return corelib.AppConfig{PetConversationMode: "voice-turn", PetReadbackMode: "summary", PetVoiceReadback: true}, nil
	}
	s.hooks.setState = func(string) {}
	var spoken []string
	s.hooks.speak = func(text string, _ uint64) { spoken = append(spoken, text) }
	s.hooks.runTurn = func(string, string, bool) (petTurnOutcome, error) {
		return petTurnOutcome{}, errPetCompanionTurn("找不到群「产品」")
	}
	s.runAgent("发给产品群", "", true)
	if len(spoken) != 1 || !strings.Contains(spoken[0], "找不到群") || strings.Contains(spoken[0], "tool") {
		t.Fatalf("failure line %#v", spoken)
	}

	spoken = nil
	s.phase = "capture"
	s.hooks.runTurn = func(string, string, bool) (petTurnOutcome, error) {
		return petTurnOutcome{Text: "做好了。北京今天晴。"}, nil
	}
	s.runAgent("查一下天气", "", true)
	if len(spoken) != 1 || spoken[0] != "北京今天晴。" {
		t.Fatalf("spoken result %#v", spoken)
	}

	if petFailureLine(errPetCompanionTurn("daily_llm_budget_exceeded")) != "今天的用量到了。" {
		t.Fatal("budget failure was not spoken")
	}
	if petFailureLine(errPetCompanionTurn("runtime error: something.go:12")) != "这步没做成。" {
		t.Fatal("internal error was spoken aloud")
	}
}

func TestPetChatTurnDoesNotAttachTools(t *testing.T) {
	setPetCompanionToolMode(petCompanionUserID, petToolModeChat)
	defer setPetCompanionToolMode(petCompanionUserID, "")
	h := &IMMessageHandler{}
	set := h.prepareAgentLoopTools(petCompanionUserID, "你好", nil, agentLoopPhase{})
	if len(set.Tools) != 0 || len(set.BaseTools) != 0 {
		t.Fatalf("chat tools %#v", set.Tools)
	}
	callbacks := &sharedAgentLoopCallbacks{
		userID:          petCompanionUserID,
		semanticSurface: &semanticCallSurface{},
		tools: []map[string]interface{}{
			{"type": "function", "function": map[string]interface{}{"name": "send_file"}},
		},
	}
	if got := callbacks.BuildToolsForModelRequest("你好", 1); len(got) != 0 {
		t.Fatalf("semantic chat tools %#v", got)
	}
	setPetCompanionToolMode(petCompanionUserID, "")
	if petCompanionToolsDisabled(desktopUserID) {
		t.Fatal("main window was treated as pet chat")
	}
	command := &sharedAgentLoopCallbacks{
		userID: petCompanionUserID,
		tools: []map[string]interface{}{
			{"type": "function", "function": map[string]interface{}{"name": "web_search"}},
		},
	}
	if got := command.BuildToolsForModelRequest("查一下天气", 1); len(got) == 0 {
		t.Fatal("command turn lost its tools")
	}
}

func TestPetFastTurnDoesNotFlashThinking(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "capture"
	s.hooks.loadConfig = func() (corelib.AppConfig, error) {
		return corelib.AppConfig{PetConversationMode: "voice-turn", PetReadbackMode: "summary", PetVoiceReadback: true}, nil
	}
	s.hooks.speak = func(string, uint64) {}
	var states []string
	s.hooks.setState = func(next string) { states = append(states, next) }
	s.hooks.runTurn = func(string, string, bool) (petTurnOutcome, error) {
		return petTurnOutcome{Text: "好了。"}, nil
	}

	s.runAgent("查一下天气", "", true)
	time.Sleep(1200 * time.Millisecond)
	for _, state := range states {
		if state == "thinking" {
			t.Fatalf("fast turn flashed thinking: %#v", states)
		}
	}
}

func TestPetSlowTurnShowsThinkingThenOneCue(t *testing.T) {
	s := newPetCompanionSession(nil)
	s.running = true
	s.phase = "capture"
	s.hooks.loadConfig = func() (corelib.AppConfig, error) {
		return corelib.AppConfig{PetConversationMode: "voice-turn", PetReadbackMode: "summary", PetVoiceReadback: true}, nil
	}
	var states []string
	s.hooks.setState = func(next string) { states = append(states, next) }
	var spoken []string
	s.hooks.speak = func(text string, _ uint64) { spoken = append(spoken, text) }
	s.hooks.runTurn = func(string, string, bool) (petTurnOutcome, error) {
		time.Sleep(4300 * time.Millisecond)
		return petTurnOutcome{Text: "查到了。"}, nil
	}

	s.runAgent("查一下天气", "", true)
	thought := false
	for _, state := range states {
		if state == "thinking" {
			thought = true
		}
	}
	if !thought {
		t.Fatalf("slow turn never showed thinking: %#v", states)
	}
	cues := 0
	for _, line := range spoken {
		if strings.Contains(line, "我看一下") {
			cues++
		}
	}
	if cues != 1 {
		t.Fatalf("cue count %d spoken %#v", cues, spoken)
	}
}
