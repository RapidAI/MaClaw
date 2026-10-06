package guiapp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

type petMic interface {
	Frames() <-chan []int16
	Close() error
}

type petCompanionHooks struct {
	loadConfig   func() (corelib.AppConfig, error)
	openMic      func() (petMic, error)
	spotWake     func(pcm []int16) (bool, error)
	transcribe   func(pcm []int16) (string, error)
	runTurn      func(text, follow string, allowTools bool) (petTurnOutcome, error)
	saveDocument func(path, title, body string) (string, error)
	sendDocument func(path, title, body, target string) (string, error)
	cancelTurn   func()
	speak        func(text string, gen uint64)
	stopSpeak    func()
	setState     func(state string)
	bubble       func(text string)
	record       func(user, assistant string)
}

// PetCompanionSession is the desktop pet's own voice companion.
// It listens only while the pet is visible and voice mode is armed.
type PetCompanionSession struct {
	app   *App
	hooks petCompanionHooks

	mu            sync.Mutex
	running       bool
	cancel        context.CancelFunc
	generation    uint64
	speechGen     uint64
	phase         string
	yielded       bool
	lastResult    string
	pending       string
	pendingDoc    petPendingDocument
	docPrompts    int
	imAsks        int
	docEpoch      uint64
	confirmEpoch  uint64
	dialogueEpoch uint64
	promptLine    string
	promptUntil   time.Time
	promptReplays int
	missSpeaks    int
	lastWakeText  string
	transcript    []string
}

type petTurnOutcome struct {
	Text  string
	Files []string
}

type petPendingDocument struct {
	Path  string
	Paths []string
	Title string
	Body  string
}

func (d petPendingDocument) files() []string {
	if len(d.Paths) > 0 {
		return d.Paths
	}
	if strings.TrimSpace(d.Path) != "" {
		return []string{d.Path}
	}
	return []string{""}
}

func newPetCompanionSession(app *App) *PetCompanionSession {
	s := &PetCompanionSession{app: app, phase: "off"}
	s.hooks = petCompanionHooks{
		loadConfig: func() (corelib.AppConfig, error) {
			if app == nil {
				return corelib.AppConfig{}, nil
			}
			return app.LoadConfig()
		},
		openMic: openPetMic,
		spotWake: func(pcm []int16) (bool, error) {
			if app == nil {
				return false, nil
			}
			text, err := app.TranscribeWAVBytes(pcm16ToWAV(pcm))
			s.mu.Lock()
			s.lastWakeText = text
			s.mu.Unlock()
			if err != nil || strings.TrimSpace(text) == "" {
				return false, err
			}
			return petWakeHit(text), nil
		},
		transcribe: func(pcm []int16) (string, error) {
			if app == nil {
				return "", nil
			}
			return app.TranscribeWAVBytes(pcm16ToWAV(pcm))
		},
		runTurn: func(text, follow string, allowTools bool) (petTurnOutcome, error) {
			if app == nil {
				return petTurnOutcome{}, nil
			}
			return app.runPetCompanionTurn(text, follow, allowTools)
		},
		saveDocument: func(path, title, body string) (string, error) {
			if app == nil {
				return "", errPetCompanionUnavailable
			}
			return app.savePetDocumentToLibrary(path, title, body)
		},
		sendDocument: func(path, title, body, target string) (string, error) {
			if app == nil {
				return "", errPetCompanionUnavailable
			}
			return app.sendPetDocumentToIM(path, title, body, target)
		},
		cancelTurn: func() {
			if app == nil || app.imHandler == nil {
				return
			}
			_, _ = app.imHandler.CancelSessionForUser(petCompanionUserID)
		},
		speak: func(text string, gen uint64) {
			if app == nil {
				return
			}
			go app.speakPetCompanionLine(s, text, gen)
		},
		stopSpeak: func() {
			if app != nil {
				app.emitEvent("pet-companion-stop-speech", nil)
			}
		},
		setState: func(state string) {
			if app == nil {
				return
			}
			app.SetDesktopPetState(state, 0)
		},
		bubble: func(text string) {
			if app != nil && strings.TrimSpace(text) != "" {
				app.emitEvent("pet-companion-bubble", map[string]any{"text": text})
			}
		},
	}
	return s
}

func (s *PetCompanionSession) Start() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.running = true
	s.cancel = cancel
	s.phase = "wake"
	s.generation++
	s.mu.Unlock()
	s.pose("idle")
	petLogf("start")
	if s.app != nil {
		go s.app.prewarmPetPhrases()
	}
	go s.loop(ctx)
}

func (s *PetCompanionSession) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if !s.running && s.phase == "off" {
		s.mu.Unlock()
		return
	}
	s.generation++
	s.speechGen++
	genCancel := s.cancel
	s.cancel = nil
	s.running = false
	s.phase = "off"
	s.pending = ""
	s.pendingDoc = petPendingDocument{}
	s.docPrompts = 0
	s.imAsks = 0
	s.docEpoch++
	s.confirmEpoch++
	s.promptLine = ""
	s.promptUntil = time.Time{}
	s.promptReplays = 0
	s.mu.Unlock()
	if genCancel != nil {
		genCancel()
	}
	if s.hooks.stopSpeak != nil {
		s.hooks.stopSpeak()
	}
	if s.hooks.cancelTurn != nil {
		go s.hooks.cancelTurn()
	}
	s.pose("idle")
	petLogf("stop")
}

func (s *PetCompanionSession) Poke() {
	if s == nil {
		return
	}
	s.mu.Lock()
	running := s.running
	s.mu.Unlock()
	if !running {
		s.Start()
	}
	s.enterCapture(true)
}

func (s *PetCompanionSession) SetPanelMicBusy(busy bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.yielded = busy
	s.mu.Unlock()
	petLogf("panel mic busy=%t", busy)
}

func (s *PetCompanionSession) Transcript() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.transcript, "\n")
}

func (s *PetCompanionSession) loop(ctx context.Context) {
	segmenter := newPetSegmenter()
	var mic petMic
	defer func() {
		if mic != nil {
			_ = mic.Close()
		}
	}()
	openedAt := time.Now()
	for {
		if ctx.Err() != nil {
			return
		}
		if s.panelYielded() {
			if mic != nil {
				_ = mic.Close()
				mic = nil
				s.pose("idle")
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
			continue
		}
		if mic == nil {
			opened, err := s.hooks.openMic()
			if err != nil {
				petLogf("microphone unavailable: %v", err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
				continue
			}
			mic = opened
			openedAt = time.Now()
			s.showPhasePose()
			petLogf("listening for wake word")
		}
		select {
		case <-ctx.Done():
			return
		case frame, ok := <-mic.Frames():
			if !ok {
				_ = mic.Close()
				mic = nil
				continue
			}
			s.mu.Lock()
			speaking := petGuardPlayback(s.phase, s.promptUntil, time.Now())
			s.mu.Unlock()
			segmenter.setPlayback(speaking)
			ev, hit := segmenter.push(frame)
			if !hit {
				continue
			}
			if ev.speechStart {
				s.onSpeechStart()
				continue
			}
			if len(ev.utterance) > 0 {
				s.onUtterance(ev, openedAt)
			}
		}
	}
}

func (s *PetCompanionSession) panelYielded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.yielded
}

func (s *PetCompanionSession) onSpeechStart() {
	s.mu.Lock()
	phase := s.phase
	prompting := time.Now().Before(s.promptUntil)
	if phase == "speak" || prompting {
		s.speechGen++
	}
	if phase == "dialogue" {
		s.dialogueEpoch++
	}
	s.mu.Unlock()
	if phase == "wake" {
		s.pose("listening")
		return
	}
	if phase != "speak" && !prompting {
		return
	}
	if s.hooks.stopSpeak != nil {
		s.hooks.stopSpeak()
	}
	if phase != "speak" {
		if s.hooks.setState != nil {
			s.hooks.setState("listening")
		}
		return
	}
	s.mu.Lock()
	s.phase = "capture"
	s.mu.Unlock()
	if s.hooks.setState != nil {
		s.hooks.setState("listening")
	}
}

func (s *PetCompanionSession) onUtterance(ev petAudioEvent, listenedSince time.Time) {
	s.mu.Lock()
	phase := s.phase
	s.mu.Unlock()
	if phase == "think" {
		if petHeardNothing(ev.rms, len(ev.utterance)) || s.hooks.transcribe == nil {
			return
		}
		text, err := s.hooks.transcribe(ev.utterance)
		if err != nil || !petStopPhrase(text) {
			return
		}
		s.mu.Lock()
		s.generation++
		s.speechGen++
		s.mu.Unlock()
		if s.hooks.stopSpeak != nil {
			s.hooks.stopSpeak()
		}
		if s.hooks.cancelTurn != nil {
			go s.hooks.cancelTurn()
		}
		s.rest()
		return
	}
	if phase == "wake" {
		hit, err := s.hooks.spotWake(ev.utterance)
		if err != nil {
			petLogf("wake spot failed: %v", err)
			return
		}
		if !hit {
			s.showPhasePose()
			return
		}
		petLogf("wake hit after %s", time.Since(listenedSince).Round(time.Millisecond))
		s.mu.Lock()
		command := strings.TrimSpace(petStripWake(s.lastWakeText))
		s.lastWakeText = ""
		s.mu.Unlock()
		s.enterCapture(true)
		if command != "" {
			s.acceptText(command)
			return
		}
		s.greetWake()
		return
	}
	if phase != "capture" && phase != "dialogue" && phase != "confirm" && phase != "docdest" && phase != "docim" {
		return
	}
	if petHeardNothing(ev.rms, len(ev.utterance)) {
		s.quietMiss()
		return
	}
	text, err := s.hooks.transcribe(ev.utterance)
	if err != nil {
		petLogf("transcribe failed: %v", err)
		s.missHeard(phase)
		return
	}
	text = petStripWake(text)
	if strings.TrimSpace(text) == "" {
		s.missHeard(phase)
		return
	}
	s.mu.Lock()
	prompt := s.promptLine
	s.mu.Unlock()
	if petIsPromptEcho(text, prompt) && (phase == "confirm" || phase == "docdest" || phase == "docim" || prompt == petWakeAck) {
		petLogf("ignored playback of the question")
		if prompt == petWakeAck {
			return
		}
		s.replayPromptOnce()
		return
	}
	s.acceptText(text)
}

func (s *PetCompanionSession) enterCapture(fromWake bool) {
	s.mu.Lock()
	if !s.running && !fromWake {
		s.mu.Unlock()
		return
	}
	s.phase = "capture"
	if fromWake {
		s.missSpeaks = 0
	}
	s.mu.Unlock()
	if s.hooks.setState != nil {
		s.hooks.setState("listening")
	}
	s.armCaptureWait()
}

func (s *PetCompanionSession) armCaptureWait() {
	go func() {
		timer := time.NewTimer(time.Duration(petCaptureWaitSec * float64(time.Second)))
		defer timer.Stop()
		<-timer.C
		s.mu.Lock()
		still := s.phase == "capture" && !time.Now().Before(s.promptUntil)
		s.mu.Unlock()
		if still {
			s.quietMiss()
		}
	}()
}

func (s *PetCompanionSession) greetWake() {
	if s == nil {
		return
	}
	s.deliver(petWakeAck, petUtteranceChat, false)
	s.mu.Lock()
	if s.phase == "speak" || s.phase == "capture" {
		s.phase = "capture"
	}
	s.mu.Unlock()
	s.noteSpokenPrompt(petWakeAck)
	s.armCaptureWait()
	petLogf("wake ack")
}

func (s *PetCompanionSession) quietMiss() {
	s.mu.Lock()
	if s.phase != "capture" {
		s.mu.Unlock()
		return
	}
	s.phase = "wake"
	s.mu.Unlock()
	petLogf("wake without speech; returning to rest")
	s.showPhasePose()
}

func (s *PetCompanionSession) acceptText(text string) {
	s.mu.Lock()
	phase := s.phase
	pending := s.pending
	if phase == "docdest" || phase == "docim" {
		s.mu.Unlock()
		s.routeDocument(text, phase)
		return
	}
	s.mu.Unlock()
	petLogf("hear phase=%s %s", phase, petLogText(text))
	if petGoodbye(text) {
		s.rest()
		return
	}
	s.mu.Lock()
	if petStopPhrase(text) && phase != "confirm" {
		s.generation++
		s.phase = "wake"
		s.mu.Unlock()
		if s.hooks.stopSpeak != nil {
			s.hooks.stopSpeak()
		}
		if s.hooks.cancelTurn != nil {
			go s.hooks.cancelTurn()
		}
		s.rest()
		return
	}
	if phase == "confirm" {
		s.pending = ""
		s.confirmEpoch++
		s.promptLine = ""
		s.promptUntil = time.Time{}
		s.phase = "think"
		s.mu.Unlock()
		if petAffirmative(text) && pending != "" {
			s.runCommand(pending, true)
			return
		}
		s.deliver("好，那先不做。", petUtteranceChat, true)
		return
	}
	s.phase = "think"
	follow := ""
	if petIsFollowUp(text) {
		follow = s.lastResult
	}
	s.mu.Unlock()
	if kind := petNeedsSpokenConfirm(text); kind != petConfirmNone {
		s.askConfirm(petConfirmQuestion(kind, text), text)
		return
	}
	kind := petUtteranceKindOf(text)
	if kind == petUtteranceChat {
		s.runChat(text, follow)
		return
	}
	s.runCommand(text, true)
}

func (s *PetCompanionSession) runChat(text, follow string) {
	s.runAgent(text, follow, false)
}

func (s *PetCompanionSession) runCommand(text string, allowTools bool) {
	s.runAgent(text, s.followFor(text), allowTools)
}

func (s *PetCompanionSession) followFor(text string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if petIsFollowUp(text) {
		return s.lastResult
	}
	return ""
}

func (s *PetCompanionSession) runAgent(text, follow string, allowTools bool) {
	s.mu.Lock()
	s.phase = "think"
	gen := s.generation
	s.mu.Unlock()
	pose := time.AfterFunc(time.Duration(petThinkPoseSec*float64(time.Second)), func() {
		s.mu.Lock()
		current := s.generation
		phase := s.phase
		s.mu.Unlock()
		if current != gen || phase != "think" {
			return
		}
		s.pose("thinking")
	})
	cue := time.AfterFunc(time.Duration(petThinkCueSec*float64(time.Second)), func() {
		s.mu.Lock()
		current := s.generation
		phase := s.phase
		s.mu.Unlock()
		if current != gen || phase != "think" {
			return
		}
		s.cue("我看一下")
	})
	outcome, err := s.hooks.runTurn(text, follow, allowTools)
	pose.Stop()
	cue.Stop()
	s.mu.Lock()
	if s.generation != gen || !s.running {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	if err != nil {
		line := petFailureLine(err)
		petLogf("turn failed: %v", err)
		s.mu.Lock()
		s.lastResult = line
		s.transcript = append(s.transcript, "你："+text, "码卡龙："+line)
		if len(s.transcript) > 40 {
			s.transcript = s.transcript[len(s.transcript)-40:]
		}
		s.mu.Unlock()
		s.deliver(line, petUtteranceCommand, true)
		return
	}
	reply := strings.TrimSpace(outcome.Text)
	if reply == "" {
		reply = "做好了。"
	}
	s.mu.Lock()
	s.lastResult = reply
	s.transcript = append(s.transcript, "你："+text, "码卡龙："+reply)
	if len(s.transcript) > 40 {
		s.transcript = s.transcript[len(s.transcript)-40:]
	}
	s.mu.Unlock()
	if s.hooks.record != nil {
		s.hooks.record(text, reply)
	}
	kind := petUtteranceKindOf(text)
	if !allowTools {
		kind = petUtteranceChat
	}
	docs := petDocumentFiles(outcome.Files)
	if len(docs) > 0 || (petAskedForDocument(text) && len([]rune(reply)) >= 40) {
		s.mu.Lock()
		s.pendingDoc = petPendingDocument{
			Path:  firstPetFile(docs),
			Paths: docs,
			Title: petDocumentTitle(text),
			Body:  reply,
		}
		s.docPrompts = 0
		s.imAsks = 0
		s.mu.Unlock()
		s.askDocumentDestination(petDocumentOfferLine(reply))
		return
	}
	s.deliver(reply, kind, true)
}

func firstPetFile(files []string) string {
	for _, file := range files {
		if strings.TrimSpace(file) != "" {
			return strings.TrimSpace(file)
		}
	}
	return ""
}

func (s *PetCompanionSession) askDocumentDestination(line string) {
	s.mu.Lock()
	s.docPrompts++
	s.promptReplays = 0
	prompts := s.docPrompts
	s.phase = "docdest"
	s.mu.Unlock()
	if prompts > 2 {
		s.declineDocument()
		return
	}
	if strings.TrimSpace(line) == "" {
		line = petDocumentQuestion
	}
	s.deliver(line, petUtteranceChat, false)
	s.mu.Lock()
	s.phase = "docdest"
	s.mu.Unlock()
	if s.hooks.setState != nil {
		s.hooks.setState("listening")
	}
	s.noteSpokenPrompt(line)
	s.armDocumentTimeout()
}

func (s *PetCompanionSession) routeDocument(text, phase string) {
	s.mu.Lock()
	if s.phase != "docdest" && s.phase != "docim" {
		s.mu.Unlock()
		return
	}
	s.docEpoch++
	s.mu.Unlock()
	if petGoodbye(text) || petStopPhrase(text) {
		s.dropDocument()
		return
	}
	choice, target := petDocumentChoice(text)
	petLogf("document phase=%s choice=%s target=%s text=%s", phase, choice, target, petLogText(text))
	if choice == "decline" {
		s.declineDocument()
		return
	}
	if choice == "library" {
		s.savePendingDocument()
		return
	}
	if phase == "docim" {
		if choice == "im" && target != "" {
			s.sendPendingDocument(target)
			return
		}
		if choice != "im" && petPlausibleChatName(text) {
			s.sendPendingDocument(strings.TrimSpace(text))
			return
		}
		s.askIMTarget()
		return
	}
	if choice == "im" {
		if target == "" {
			s.askIMTarget()
			return
		}
		s.sendPendingDocument(target)
		return
	}
	s.askDocumentDestination("")
}

func (s *PetCompanionSession) askIMTarget() {
	s.mu.Lock()
	s.imAsks++
	s.promptReplays = 0
	asks := s.imAsks
	s.phase = "docim"
	s.mu.Unlock()
	if asks > 2 {
		s.declineDocument()
		return
	}
	s.deliver("发到哪个聊天？", petUtteranceChat, false)
	s.noteSpokenPrompt("发到哪个聊天？")
	s.mu.Lock()
	s.phase = "docim"
	s.mu.Unlock()
	if s.hooks.setState != nil {
		s.hooks.setState("listening")
	}
	s.armDocumentTimeout()
}

func (s *PetCompanionSession) savePendingDocument() {
	s.mu.Lock()
	doc := s.pendingDoc
	s.mu.Unlock()
	if s.hooks.saveDocument == nil {
		s.holdDocument("现在还存不进文稿库。文稿先留在这儿。")
		return
	}
	var left []string
	var line string
	for _, path := range doc.files() {
		spoken, err := s.hooks.saveDocument(path, doc.Title, doc.Body)
		if err != nil {
			petLogf("mobile library: %v", err)
			left = append(left, path)
			continue
		}
		line = spoken
	}
	if len(left) > 0 {
		s.mu.Lock()
		s.pendingDoc.Paths = left
		s.pendingDoc.Path = left[0]
		s.mu.Unlock()
		s.holdDocument("没放进文稿库。发给聊天，还是先留在这儿？")
		return
	}
	s.finishDocument(line)
}

func (s *PetCompanionSession) sendPendingDocument(target string) {
	s.mu.Lock()
	doc := s.pendingDoc
	s.mu.Unlock()
	if s.hooks.sendDocument == nil {
		s.holdDocument("现在还发不出去。文稿先留在这儿。")
		return
	}
	var left []string
	var line string
	for _, path := range doc.files() {
		spoken, err := s.hooks.sendDocument(path, doc.Title, doc.Body, target)
		if err != nil {
			petLogf("im send: %v", err)
			left = append(left, path)
			continue
		}
		line = spoken
	}
	if len(left) > 0 {
		s.mu.Lock()
		s.pendingDoc.Paths = left
		s.pendingDoc.Path = left[0]
		s.mu.Unlock()
		s.holdDocument("没发出去。发给聊天，还是放进移动文稿库？")
		return
	}
	s.finishDocument(line)
}

func (s *PetCompanionSession) holdDocument(line string) {
	s.mu.Lock()
	s.phase = "docdest"
	s.docPrompts = 1
	s.imAsks = 0
	s.promptReplays = 0
	s.mu.Unlock()
	s.deliver(line, petUtteranceChat, false)
	s.mu.Lock()
	s.phase = "docdest"
	s.mu.Unlock()
	if s.hooks.setState != nil {
		s.hooks.setState("listening")
	}
	s.noteSpokenPrompt(line)
	s.armDocumentTimeout()
}

func (s *PetCompanionSession) askConfirm(question, command string) {
	s.mu.Lock()
	s.pending = command
	s.phase = "confirm"
	s.promptReplays = 0
	s.mu.Unlock()
	s.deliver(question, petUtteranceChat, false)
	s.mu.Lock()
	s.phase = "confirm"
	s.mu.Unlock()
	s.noteSpokenPrompt(question)
	s.armConfirmTimeout()
	petLogf("confirm ask %s", petLogText(question))
}

func (s *PetCompanionSession) armConfirmTimeout() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.confirmEpoch++
	epoch := s.confirmEpoch
	speechLeft := time.Until(s.promptUntil)
	sec := 30
	s.mu.Unlock()
	if s.hooks.loadConfig != nil {
		if cfg, err := s.hooks.loadConfig(); err == nil && cfg.PetContinuousTimeout > 0 {
			sec = cfg.PetContinuousTimeout
		}
	}
	wait := time.Duration(sec) * time.Second
	if speechLeft > 0 {
		wait += speechLeft
	}
	time.AfterFunc(wait, func() {
		s.dropConfirmIf(epoch)
	})
}

func (s *PetCompanionSession) dropConfirmIf(epoch uint64) {
	s.mu.Lock()
	if s.confirmEpoch != epoch || s.phase != "confirm" {
		s.mu.Unlock()
		return
	}
	s.pending = ""
	s.promptLine = ""
	s.promptUntil = time.Time{}
	s.phase = "wake"
	s.mu.Unlock()
	s.showPhasePose()
	petLogf("confirm timeout")
}

func (s *PetCompanionSession) missHeard(phase string) {
	if phase == "docdest" || phase == "docim" || phase == "confirm" {
		s.replayPromptOnce()
		return
	}
	s.mu.Lock()
	said := s.missSpeaks
	if said < 1 {
		s.missSpeaks++
	}
	s.mu.Unlock()
	if said < 1 {
		s.deliver("没听清", petUtteranceChat, false)
	}
	retry := false
	if s.hooks.loadConfig != nil {
		if cfg, err := s.hooks.loadConfig(); err == nil {
			retry = cfg.PetAutoRetryOnNoHear
		}
	}
	if phase == "capture" && retry && said < 1 {
		s.enterCapture(false)
		return
	}
	if phase == "capture" {
		s.mu.Lock()
		s.phase = "capture"
		s.mu.Unlock()
		s.quietMiss()
		return
	}
	s.mu.Lock()
	if s.phase == "speak" || s.phase == phase {
		s.phase = phase
	}
	s.mu.Unlock()
	s.showPhasePose()
	if phase == "dialogue" {
		s.armDialogueTimeout()
	}
}

func (s *PetCompanionSession) replayPromptOnce() {
	s.mu.Lock()
	if s.promptReplays >= 1 {
		s.mu.Unlock()
		return
	}
	line := s.promptLine
	phase := s.phase
	if line == "" || (phase != "docdest" && phase != "docim" && phase != "confirm") {
		s.mu.Unlock()
		return
	}
	s.promptReplays++
	s.mu.Unlock()
	s.deliver(line, petUtteranceChat, false)
	s.mu.Lock()
	if s.phase == "speak" || s.phase == phase {
		s.phase = phase
	}
	s.mu.Unlock()
	s.noteSpokenPrompt(line)
	if phase == "confirm" {
		s.armConfirmTimeout()
		return
	}
	s.armDocumentTimeout()
}

func (s *PetCompanionSession) noteSpokenPrompt(line string) {
	var cfg corelib.AppConfig
	if s.hooks.loadConfig != nil {
		cfg, _ = s.hooks.loadConfig()
	}
	quiet := cfg.PetQuietMode || strings.TrimSpace(cfg.PetReadbackMode) == "off"
	s.mu.Lock()
	s.promptLine = line
	if quiet || s.hooks.speak == nil {
		s.promptUntil = time.Time{}
		s.mu.Unlock()
		return
	}
	ms := 500 + 170*len([]rune(line))
	if ms > 8000 {
		ms = 8000
	}
	until := time.Now().Add(time.Duration(ms) * time.Millisecond)
	s.promptUntil = until
	s.mu.Unlock()
	if s.hooks.setState != nil {
		s.hooks.setState("speaking")
	}
	time.AfterFunc(time.Until(until), func() {
		s.mu.Lock()
		still := s.promptUntil.Equal(until) && (s.phase == "docdest" || s.phase == "docim" || s.phase == "confirm" || s.phase == "capture")
		s.mu.Unlock()
		if still && s.hooks.setState != nil {
			s.hooks.setState("listening")
		}
	})
}

func (s *PetCompanionSession) armDocumentTimeout() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.docEpoch++
	epoch := s.docEpoch
	speechLeft := time.Until(s.promptUntil)
	sec := 30
	s.mu.Unlock()
	if s.hooks.loadConfig != nil {
		if cfg, err := s.hooks.loadConfig(); err == nil && cfg.PetContinuousTimeout > 0 {
			sec = cfg.PetContinuousTimeout
		}
	}
	wait := time.Duration(sec) * time.Second
	if speechLeft > 0 {
		wait += speechLeft
	}
	time.AfterFunc(wait, func() {
		s.dropDocumentIf(epoch)
	})
}

func (s *PetCompanionSession) dropDocumentIf(epoch uint64) {
	s.mu.Lock()
	if s.docEpoch != epoch || (s.phase != "docdest" && s.phase != "docim") {
		s.mu.Unlock()
		return
	}
	s.pendingDoc = petPendingDocument{}
	s.docPrompts = 0
	s.imAsks = 0
	s.phase = "wake"
	s.promptLine = ""
	s.promptUntil = time.Time{}
	s.mu.Unlock()
	s.showPhasePose()
}

func (s *PetCompanionSession) dropDocument() {
	s.mu.Lock()
	s.docEpoch++
	s.pendingDoc = petPendingDocument{}
	s.docPrompts = 0
	s.imAsks = 0
	s.phase = "wake"
	s.promptLine = ""
	s.promptUntil = time.Time{}
	s.mu.Unlock()
	s.showPhasePose()
}

func (s *PetCompanionSession) declineDocument() {
	s.mu.Lock()
	s.docEpoch++
	s.pendingDoc = petPendingDocument{}
	s.docPrompts = 0
	s.imAsks = 0
	s.phase = "capture"
	s.promptLine = ""
	s.promptUntil = time.Time{}
	s.mu.Unlock()
	s.deliver("好，先留在这儿。", petUtteranceChat, true)
}

func (s *PetCompanionSession) finishDocument(line string) {
	s.mu.Lock()
	s.docEpoch++
	s.pendingDoc = petPendingDocument{}
	s.docPrompts = 0
	s.imAsks = 0
	s.phase = "capture"
	s.promptLine = ""
	s.promptUntil = time.Time{}
	s.mu.Unlock()
	if strings.TrimSpace(line) == "" {
		line = "做好了。"
	}
	s.deliver(line, petUtteranceChat, true)
}

func (s *PetCompanionSession) cue(text string) {
	cfg, _ := s.hooks.loadConfig()
	if cfg.PetQuietMode {
		if s.hooks.bubble != nil {
			s.hooks.bubble(text)
		}
		return
	}
	s.mu.Lock()
	gen := s.speechGen
	s.mu.Unlock()
	if s.hooks.speak != nil {
		s.hooks.speak(text, gen)
	}
}

func (s *PetCompanionSession) deliver(text string, kind petUtteranceKind, thenListen bool) {
	cfg, _ := s.hooks.loadConfig()
	line := petSpokenLine(cfg, kind, text)
	quiet := cfg.PetQuietMode || strings.TrimSpace(cfg.PetReadbackMode) == "off"
	s.mu.Lock()
	gen := s.speechGen
	resume := ""
	switch s.phase {
	case "confirm", "docdest", "docim":
		resume = s.phase
	}
	s.mu.Unlock()
	if line == "" {
		return
	}
	if quiet {
		if s.hooks.bubble != nil {
			s.hooks.bubble(line)
		}
	} else if s.hooks.speak != nil {
		if resume == "" {
			s.mu.Lock()
			s.phase = "speak"
			s.mu.Unlock()
		}
		if s.hooks.setState != nil {
			s.hooks.setState("speaking")
		}
		s.hooks.speak(line, gen)
	}
	if resume != "" {
		s.mu.Lock()
		s.phase = resume
		s.mu.Unlock()
		if s.hooks.setState != nil {
			s.hooks.setState("listening")
		}
		return
	}
	if !thenListen {
		return
	}
	if petCompanionContinuous(cfg) {
		s.mu.Lock()
		s.phase = "dialogue"
		s.mu.Unlock()
		if s.hooks.setState != nil {
			s.hooks.setState("listening")
		}
		s.armDialogueTimeout()
		return
	}
	s.rest()
}

func (s *PetCompanionSession) armDialogueTimeout() {
	cfg, _ := s.hooks.loadConfig()
	sec := cfg.PetContinuousTimeout
	if sec <= 0 {
		sec = 30
	}
	s.mu.Lock()
	s.dialogueEpoch++
	epoch := s.dialogueEpoch
	s.mu.Unlock()
	time.AfterFunc(time.Duration(sec)*time.Second, func() {
		s.mu.Lock()
		if s.dialogueEpoch != epoch || s.phase != "dialogue" {
			s.mu.Unlock()
			return
		}
		s.phase = "wake"
		s.pending = ""
		s.mu.Unlock()
		s.showPhasePose()
	})
}

func (s *PetCompanionSession) rest() {
	s.mu.Lock()
	s.phase = "wake"
	s.pending = ""
	s.promptLine = ""
	s.promptUntil = time.Time{}
	s.promptReplays = 0
	s.mu.Unlock()
	s.showPhasePose()
}

func (s *PetCompanionSession) pose(state string) {
	if s == nil || s.hooks.setState == nil || state == "" {
		return
	}
	s.hooks.setState(state)
}

// showPhasePose keeps wake-listening visually distinct from recording a sentence.
// Hidden, stopped, or yielded sessions have no listening pose.
func (s *PetCompanionSession) showPhasePose() {
	if s == nil {
		return
	}
	s.mu.Lock()
	phase := s.phase
	running := s.running
	yielded := s.yielded
	s.mu.Unlock()
	if !running || yielded || phase == "off" {
		s.pose("idle")
		return
	}
	switch phase {
	case "wake":
		s.pose("alert")
	case "capture", "dialogue", "confirm", "docdest", "docim":
		s.pose("listening")
	case "think":
		s.pose("thinking")
	case "speak":
		s.pose("speaking")
	default:
		s.pose("idle")
	}
}

func (a *App) runPetCompanionTurn(text, follow string, allowTools bool) (petTurnOutcome, error) {
	if a == nil || a.imHandler == nil {
		return petTurnOutcome{}, errPetCompanionUnavailable
	}
	if !allowTools {
		setPetCompanionToolMode(petCompanionUserID, petToolModeChat)
		defer setPetCompanionToolMode(petCompanionUserID, "")
	}
	prompt := petCompanionPersona
	if strings.TrimSpace(follow) != "" {
		prompt += "\n上一轮结果，只用来理解指代：" + clipPetSpeech(follow, 120)
	}
	resp := a.imHandler.HandleIMMessage(IMUserMessage{
		UserID:                 petCompanionUserID,
		Platform:               petCompanionPlatform,
		Text:                   text,
		Lang:                   "zh",
		NoWorkflowInterception: true,
		AssistantBinding: &agent.AssistantBinding{
			Mode:          "pet-companion",
			InitialPrompt: prompt,
		},
	})
	if resp == nil {
		return petTurnOutcome{}, errPetCompanionUnavailable
	}
	if strings.TrimSpace(resp.Error) != "" {
		return petTurnOutcome{}, errPetCompanionTurn(resp.Error)
	}
	if petMainWindowConfirm(resp) {
		a.imHandler.dropPetWindowConfirm()
		petLogf("window confirm")
		return petTurnOutcome{Text: petWindowConfirmLine}, nil
	}
	return petTurnOutcome{Text: strings.TrimSpace(resp.Text), Files: petFilesFromResponse(resp)}, nil
}

// dropPetWindowConfirm forgets a click-only confirmation on the pet. The main
// window's own pending confirmation is left alone, and nothing is written into
// the main transcript.
func (h *IMMessageHandler) dropPetWindowConfirm() {
	if h == nil {
		return
	}
	if h.confirmationStore != nil {
		h.confirmationStore.clear(petCompanionUserID)
	}
	h.pendingAskUser.Delete(petCompanionUserID)
}

func (a *App) savePetDocumentToLibrary(path, title, body string) (string, error) {
	if a == nil {
		return "", errPetCompanionUnavailable
	}
	path = strings.TrimSpace(path)
	if path != "" {
		if _, err := a.ImportMobileDocumentFromPath(path); err != nil {
			return "", err
		}
		return "已经放进移动文稿库了。", nil
	}
	if strings.TrimSpace(title) == "" {
		title = "宠物文稿"
	}
	if _, err := a.CreateMobileDocumentDraft(title, "", body, "note"); err != nil {
		return "", err
	}
	return "已经放进移动文稿库了。", nil
}

func (a *App) sendPetDocumentToIM(path, title, body, target string) (string, error) {
	if a == nil || a.imHandler == nil {
		return "", errPetCompanionUnavailable
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return "", errPetCompanionTurn("missing chat")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		if strings.TrimSpace(body) == "" {
			return "", errPetCompanionTurn("empty document")
		}
		file, err := os.CreateTemp("", "pet-doc-*.md")
		if err != nil {
			return "", err
		}
		path = file.Name()
		defer func() { _ = os.Remove(path) }()
		if _, err := file.WriteString(body); err != nil {
			_ = file.Close()
			return "", err
		}
		if err := file.Close(); err != nil {
			return "", err
		}
	}
	channel, group := petSplitChatTarget(target)
	if strings.TrimSpace(group) == "" {
		return "", errPetCompanionTurn("missing chat")
	}
	args := map[string]interface{}{
		"action":     "send_file",
		"path":       path,
		"group_name": group,
		"text":       title,
	}
	if channel != "" {
		args["channel"] = channel
	}
	setPetCompanionDeliveryArmed(petCompanionUserID, true)
	defer setPetCompanionDeliveryArmed(petCompanionUserID, false)
	out := a.imHandler.toolIMMessageSendFile(args)
	if !petIMSendSucceeded(out) {
		return "", errPetCompanionTurn(out)
	}
	heard := group
	if channel != "" && len([]rune(channel+group)) <= 12 {
		heard = channel + group
	}
	return "已经发到" + heard + "了。", nil
}

func petIMSendSucceeded(out string) bool {
	out = strings.TrimSpace(out)
	return strings.HasPrefix(out, "已发送文件到") || strings.HasPrefix(out, "已发送到")
}

func petFilesFromResponse(resp *IMAgentResponse) []string {
	if resp == nil {
		return nil
	}
	var files []string
	if path := strings.TrimSpace(resp.LocalFilePath); path != "" {
		files = append(files, path)
	}
	for _, path := range resp.LocalFilePaths {
		path = strings.TrimSpace(path)
		if path != "" {
			files = append(files, path)
		}
	}
	return files
}

type petCompanionError string

func (e petCompanionError) Error() string { return string(e) }

const errPetCompanionUnavailable petCompanionError = "pet companion agent is unavailable"

func errPetCompanionTurn(msg string) error { return petCompanionError(msg) }

func (a *App) speakPetCompanionLine(s *PetCompanionSession, text string, gen uint64) {
	if a == nil || strings.TrimSpace(text) == "" {
		return
	}
	wav := a.synthesizePetLine(text)
	if s != nil {
		s.mu.Lock()
		stale := s.speechGen != gen || !s.running
		s.mu.Unlock()
		if stale {
			return
		}
	}
	if wav == "" {
		return
	}
	a.emitEvent("pet-companion-speech", wav)
}

func (a *App) NotifyDesktopPetMicBusy(busy bool) {
	if a == nil {
		return
	}
	if fa := a.existingFloatingAssistant(); fa != nil && fa.companion != nil {
		fa.companion.SetPanelMicBusy(busy)
	}
}

func (a *App) attachPetConversationFile(mem *agent.ConversationMemory) {
	if a == nil || mem == nil {
		return
	}
	path := a.petConversationPath()
	if path == "" {
		return
	}
	if err := mem.UseSeparateSessionFile(petCompanionUserID, path); err != nil {
		petLogf("separate memory: %v", err)
	}
}

func (a *App) petConversationPath() string {
	if a == nil {
		return ""
	}
	return filepath.Join(a.GetDataDir(), "pet_companion_conversation.json")
}

// GetPetCompanionTranscript returns the pet's own record. It is not the
// main-window conversation.
func (a *App) GetPetCompanionTranscript() string {
	if a == nil {
		return ""
	}
	return readPetCompanionTranscript(a.petConversationPath())
}

func readPetCompanionTranscript(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return ""
	}
	var snap struct {
		Sessions map[string]struct {
			Entries []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"entries"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		return ""
	}
	session, ok := snap.Sessions[petCompanionUserID]
	if !ok {
		return ""
	}
	lines := make([]string, 0, len(session.Entries))
	for _, entry := range session.Entries {
		text := petEntryText(entry.Content)
		if text == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(entry.Role)) {
		case "user":
			lines = append(lines, "你："+text)
		case "assistant":
			lines = append(lines, "码卡龙："+text)
		}
	}
	if len(lines) > 80 {
		lines = lines[len(lines)-80:]
	}
	return strings.Join(lines, "\n")
}

func petEntryText(raw json.RawMessage) string {
	raw = bytesTrimJSON(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	var parts []map[string]any
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, part := range parts {
			if piece, ok := part["text"].(string); ok {
				b.WriteString(piece)
			}
		}
		return strings.TrimSpace(b.String())
	}
	return ""
}

func bytesTrimJSON(raw json.RawMessage) json.RawMessage {
	return json.RawMessage(strings.TrimSpace(string(raw)))
}
