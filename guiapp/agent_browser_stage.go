package guiapp

import (
	"errors"
	"hash/fnv"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

const agentBrowserStageEvent = "agent-browser-stage"

// AgentBrowserStageView is the live page painted in the assistant preview.
type AgentBrowserStageView struct {
	Open      bool   `json:"open"`
	SessionID string `json:"sessionId"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	Frame     string `json:"frame"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	UserHold  bool   `json:"userHold"`
	AskReason string `json:"askReason"`
	Seq       int64  `json:"seq"`
}

type agentBrowserStage struct {
	mu        sync.Mutex
	app       *App
	open      bool
	dismissed bool
	sessionID string
	url       string
	title     string
	frame     string
	width     int
	height    int
	askReason string
	frameHash uint64
	seq       int64
	pumping   bool
	stop      chan struct{}
}

var liveAgentBrowser agentBrowserStage

func (a *App) installAgentBrowserStage() {
	if a == nil {
		return
	}
	// Tests construct App and call startup. They must keep the headed launch
	// the suite already expects, and must not start a frame pump.
	if testing.Testing() {
		return
	}
	liveAgentBrowser.mu.Lock()
	liveAgentBrowser.app = a
	liveAgentBrowser.mu.Unlock()
	browser.EnableGUIEmbed(true)
	browser.SetBrowserStageNotifier(liveAgentBrowser.note)
	log.Printf("[browser] preview stage on; managed browser launches stay inside the assistant preview")
}

func (st *agentBrowserStage) note(n browser.BrowserStageNotice) {
	st.mu.Lock()
	st.dismissed = false
	st.open = true
	if id := strings.TrimSpace(n.SessionID); id != "" {
		st.sessionID = id
	}
	if url := strings.TrimSpace(n.URL); url != "" {
		st.url = url
	}
	if title := strings.TrimSpace(n.Title); title != "" {
		st.title = title
	}
	if reason := strings.TrimSpace(n.Reason); reason != "" {
		st.askReason = reason
	}
	if n.Kind == "session" {
		st.askReason = ""
	}
	st.mu.Unlock()
	st.ensurePump()
	st.emit(false)
}

func (st *agentBrowserStage) setHold(hold bool) {
	st.mu.Lock()
	st.open = st.open || hold
	if !hold && st.askReason != "" {
		st.askReason = ""
	}
	st.mu.Unlock()
}

func (st *agentBrowserStage) dismiss() {
	st.mu.Lock()
	st.open = false
	st.dismissed = true
	st.mu.Unlock()
	browser.SetUserHoldsBrowser(false, "")
	st.emit(false)
}

func (st *agentBrowserStage) snapshot(includeFrame bool) AgentBrowserStageView {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.seq++
	view := AgentBrowserStageView{
		Open:      st.open && !st.dismissed,
		SessionID: st.sessionID,
		URL:       st.url,
		Title:     st.title,
		Width:     st.width,
		Height:    st.height,
		AskReason: st.askReason,
		Seq:       st.seq,
	}
	held, _ := browser.UserHoldsBrowser()
	view.UserHold = held
	if includeFrame {
		view.Frame = st.frame
	}
	return view
}

func (st *agentBrowserStage) emit(includeFrame bool) {
	st.mu.Lock()
	app := st.app
	st.mu.Unlock()
	if app == nil {
		return
	}
	app.emitEvent(agentBrowserStageEvent, st.snapshot(includeFrame))
}

func (st *agentBrowserStage) ensurePump() {
	st.mu.Lock()
	if st.pumping {
		st.mu.Unlock()
		return
	}
	st.pumping = true
	stop := make(chan struct{})
	st.stop = stop
	st.mu.Unlock()
	go st.loop(stop)
}

func (st *agentBrowserStage) loop(stop <-chan struct{}) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	var lastEmit time.Time
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			st.mu.Lock()
			if st.dismissed || !st.open || st.sessionID == "" {
				st.mu.Unlock()
				continue
			}
			sessionID := st.sessionID
			st.mu.Unlock()
			held, _ := browser.UserHoldsBrowser()
			interval := 700 * time.Millisecond
			if held {
				interval = 220 * time.Millisecond
			}
			if !lastEmit.IsZero() && time.Since(lastEmit) < interval {
				continue
			}
			frame, w, h, url, title, err := grabAgentBrowserFrame(sessionID)
			if err != nil {
				continue
			}
			sum := hashFrame(frame)
			st.mu.Lock()
			if url != "" {
				st.url = url
			}
			if title != "" {
				st.title = title
			}
			changed := frame != "" && sum != st.frameHash
			if changed {
				st.frame = frame
				st.frameHash = sum
				if w > 0 {
					st.width = w
				}
				if h > 0 {
					st.height = h
				}
			}
			st.mu.Unlock()
			lastEmit = time.Now()
			st.emit(changed)
		}
	}
}

func grabAgentBrowserFrame(sessionID string) (string, int, int, string, string, error) {
	sess, err := browser.GetAgentSession(sessionID)
	if err != nil {
		return "", 0, 0, "", "", err
	}
	state := sess.State()
	frame, w, h, err := sess.ViewportJPEG(55)
	if err != nil {
		return "", 0, 0, state.CurrentURL, state.CurrentTitle, err
	}
	return frame, w, h, state.CurrentURL, state.CurrentTitle, nil
}

func hashFrame(frame string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(frame))
	return h.Sum64()
}

// AgentBrowserState returns the current preview, including the latest frame.
func (a *App) AgentBrowserState() AgentBrowserStageView {
	return liveAgentBrowser.snapshot(true)
}

// AgentBrowserSetControl gives the preview to the person or hands it back.
// While they hold it, mutating browser actions wait.
func (a *App) AgentBrowserSetControl(hold bool) error {
	reason := ""
	if hold {
		reason = "preview"
	}
	browser.SetUserHoldsBrowser(hold, reason)
	liveAgentBrowser.setHold(hold)
	liveAgentBrowser.emit(false)
	return nil
}

// AgentBrowserInput applies one pointer or key event from the preview.
func (a *App) AgentBrowserInput(payload string) error {
	in, err := browser.ParseViewportInput(payload)
	if err != nil {
		return err
	}
	liveAgentBrowser.mu.Lock()
	sessionID := liveAgentBrowser.sessionID
	liveAgentBrowser.mu.Unlock()
	if strings.TrimSpace(sessionID) == "" {
		return errAgentBrowserNoSession
	}
	sess, err := browser.GetAgentSession(sessionID)
	if err != nil {
		return err
	}
	return sess.ApplyViewportInput(in)
}

// AgentBrowserClose hides the preview. The browser keeps running. The next
// session or a login/captcha notice opens it again. A hold is released so
// the agent is not left waiting.
func (a *App) AgentBrowserClose() {
	liveAgentBrowser.dismiss()
}

var errAgentBrowserNoSession = errors.New("browser preview has no session")
