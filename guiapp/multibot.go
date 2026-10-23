package guiapp

// multibot.go is the main bot's team view: a registry of every bot on the
// account (this GUI's own local bot plus the Hub desktop bots), their live
// state, control (assign / notify / stop / pause / resume), artifact handoff
// between local files and remote desktops, and the event inbox that tells the
// main bot when a teammate finished, failed or needs the person.
//
// The local main bot is itself a registry entry (id multiBotSelfID). It does
// its own work through its normal agent loop (local files, shell, HTTP) and
// coordinates the others through the tools in multibot_tools.go. Remote work
// runs on Hub asynchronously, so the main bot never blocks on a teammate: it
// hands work off, keeps talking to the person, and learns about results from
// the inbox (a background watcher keeps it fresh while anything is active).

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/botregistry"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

const (
	multiBotSelfID        = "local:main"
	multiBotWatchActive   = 10 * time.Second
	multiBotWatchIdle     = 60 * time.Second
	multiBotRefreshMaxAge = 5 * time.Second
)

// multiBotHub is the slice of Hub the team view needs. *App implements it
// through the existing desktop-bot routes; tests use a fake.
type multiBotHub interface {
	roster(ctx context.Context) ([]desktopBotRosterEntry, error)
	activeRuns(ctx context.Context, botID string) ([]DesktopBotActiveRun, error)
	cross(ctx context.Context, fromID, kind, targetID, text, order string) error
	cancelRun(ctx context.Context, botID, runID string) error
	// pauseRun/resumeRun hold and release a running run at a step boundary
	// on the server; errDesktopBotPauseUnsupported on an older Hub.
	pauseRun(ctx context.Context, botID, runID, reason string) error
	resumeRun(ctx context.Context, botID, runID string) error
	uploadFile(ctx context.Context, botID, path string) (string, error)
}

// multiBot is one App's team state.
type multiBot struct {
	reg       *botregistry.Registry
	hub       multiBotHub
	artifacts *botregistry.Artifacts
	emit      func(name string, data ...interface{})

	mu          sync.Mutex
	lastRefresh time.Time
	readSeq     int64
	selfTurns   int
	watching    bool
	stopWatch   chan struct{}

	// bus and inbox carry teammate messages to the main bot (multibot_messages.go).
	bus        *botregistry.Bus
	inbox      *botregistry.Inbox
	pendingAck []string
	wakeBuf    []botregistry.Message
	wakeTimer  *time.Timer
	lastNotes  time.Time
}

var multiBots sync.Map // *App -> *multiBot

func (a *App) multiBot() *multiBot {
	if a == nil {
		return nil
	}
	if v, ok := multiBots.Load(a); ok {
		return v.(*multiBot)
	}
	mb := newMultiBot(appMultiBotHub{a}, filepath.Join(maclawpath.DataDir(), "bot_artifacts"), a.emitEvent)
	actual, loaded := multiBots.LoadOrStore(a, mb)
	if !loaded {
		desktopBotRosterLiveSource = mb.rosterLine
	}
	return actual.(*multiBot)
}

func newMultiBot(hub multiBotHub, artifactDir string, emit func(string, ...interface{})) *multiBot {
	reg := botregistry.New()
	reg.SetSelf(multiBotSelfID)
	reg.Upsert(botregistry.Bot{
		ID:           multiBotSelfID,
		Name:         "主 Bot",
		Primary:      true,
		Location:     botregistry.LocationLocal,
		Capabilities: append(botregistry.LocalCapabilities(), botregistry.CapCoordinator),
		State:        botregistry.StateIdle,
	})
	mb := &multiBot{reg: reg, hub: hub, emit: emit}
	mb.initMessages()
	if store, err := botregistry.NewArtifacts(artifactDir); err == nil {
		mb.artifacts = store
	} else {
		log.Printf("[multibot] artifact store unavailable: %v", err)
	}
	reg.Subscribe(func(ev botregistry.Event) {
		if mb.emit != nil {
			mb.emit("multibot-event", ev)
		}
		log.Printf("[multibot] event %s bot=%s run=%s", ev.Kind, ev.BotID, ev.RunID)
	})
	return mb
}

// beginSelfTurn marks the main bot busy with a turn and returns the end
// func. Overlapping turns keep it running until the last one ends.
func (mb *multiBot) beginSelfTurn(task string) func() {
	if mb == nil {
		return func() {}
	}
	mb.mu.Lock()
	mb.selfTurns++
	mb.mu.Unlock()
	if !isTeammateWake(task) {
		mb.bus.PersonSpoke()
	}
	endInbox := mb.inbox.Begin()
	task = clipRunes(strings.TrimSpace(task), 120)
	mb.reg.Update(multiBotSelfID, func(b *botregistry.Bot) {
		b.State = botregistry.StateRunning
		b.Task = task
		b.Step = ""
	})
	return func() {
		mb.mu.Lock()
		mb.selfTurns--
		left := mb.selfTurns
		mb.mu.Unlock()
		defer endInbox()
		if left > 0 {
			return
		}
		mb.reg.Update(multiBotSelfID, func(b *botregistry.Bot) {
			if b.Paused {
				b.State = botregistry.StatePaused
			} else {
				b.State = botregistry.StateIdle
			}
			b.Step = ""
		})
	}
}

// noteMainBotTurn is called around each local agent turn.
func (a *App) noteMainBotTurn(task string) func() {
	return a.multiBot().beginSelfTurn(task)
}

// refresh pulls the roster and each remote bot's active runs from Hub.
// A fresh result (within multiBotRefreshMaxAge) is reused unless force.
func (mb *multiBot) refresh(ctx context.Context, force bool) error {
	mb.mu.Lock()
	if !force && time.Since(mb.lastRefresh) < multiBotRefreshMaxAge {
		mb.mu.Unlock()
		return nil
	}
	mb.mu.Unlock()
	roster, err := mb.hub.roster(ctx)
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, entry := range roster {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			continue
		}
		keep[id] = true
		bot, existed := mb.reg.Get(id)
		if !existed {
			bot = botregistry.Bot{ID: id, Location: botregistry.LocationRemote, Capabilities: botregistry.RemoteCapabilities()}
		}
		bot.Name = strings.TrimSpace(entry.Name)
		bot.Primary = false // the local main bot coordinates; Hub's primary is its relay identity
		runs, rerr := mb.hub.activeRuns(ctx, id)
		if rerr != nil {
			bot.State = botregistry.StateOffline
			bot.LastError = rerr.Error()
		} else {
			views := make([]botregistry.RunView, 0, len(runs))
			for _, r := range runs {
				views = append(views, botregistry.RunView{RunID: r.RunID, Status: r.Status, Task: r.Task, Step: r.Step, NextStep: r.NextStep, WaitingForUser: r.WaitingForUser, Stale: r.Stale, Paused: r.Paused})
			}
			botregistry.ApplyRuns(&bot, views)
		}
		mb.reg.Upsert(bot)
	}
	mb.reg.Prune(keep)
	mb.mu.Lock()
	mb.lastRefresh = time.Now()
	mb.mu.Unlock()
	return nil
}

// hubRelay is the Hub bot id cross notes are sent from: Hub accepts a note
// only from the account's primary bot.
func (mb *multiBot) hubRelay(ctx context.Context) (string, error) {
	roster, err := mb.hub.roster(ctx)
	if err != nil {
		return "", err
	}
	for _, e := range roster {
		if e.Primary && strings.TrimSpace(e.ID) != "" {
			return strings.TrimSpace(e.ID), nil
		}
	}
	return "", fmt.Errorf("no primary desktop bot is set on this account; mark one as primary in Bot 管理 so the main bot can hand work to the others")
}

// ensureWatch starts the background watcher once. It polls fast while any
// bot is busy and slowly otherwise, so finished/blocked events reach the main
// bot without the person asking.
func (mb *multiBot) ensureWatch() {
	mb.mu.Lock()
	if mb.watching {
		mb.mu.Unlock()
		return
	}
	mb.watching = true
	mb.stopWatch = make(chan struct{})
	stop := mb.stopWatch
	mb.mu.Unlock()
	go func() {
		var lastFull time.Time
		for {
			// Reports from teammates arrive as Hub notes and are polled every
			// tick; the roster/run refresh runs at the active pace only while
			// a teammate is busy.
			full := multiBotWatchIdle
			for _, b := range mb.reg.List() {
				if b.ID != multiBotSelfID && (b.State == botregistry.StateRunning || b.State == botregistry.StateQueued) {
					full = multiBotWatchActive
					break
				}
			}
			select {
			case <-stop:
				return
			case <-time.After(multiBotWatchActive):
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if time.Since(lastFull) >= full {
				lastFull = time.Now()
				if err := mb.refresh(ctx, true); err != nil {
					log.Printf("[multibot] refresh: %v", err)
				}
			}
			mb.pollReports(ctx)
			cancel()
		}
	}()
}

func (mb *multiBot) close() {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if mb.watching {
		close(mb.stopWatch)
		mb.watching = false
	}
}

// unread returns events the main bot has not read yet and marks them read.
func (mb *multiBot) unread(peek bool) []botregistry.Event {
	mb.mu.Lock()
	after := mb.readSeq
	mb.mu.Unlock()
	evs := mb.reg.Events(after)
	if !peek && len(evs) > 0 {
		mb.mu.Lock()
		if last := evs[len(evs)-1].Seq; last > mb.readSeq {
			mb.readSeq = last
		}
		mb.mu.Unlock()
	}
	return evs
}

// rosterLine is the registry's live state for one bot id, for the persona
// reading prompt. Empty when the registry has not seen the bot.
func (mb *multiBot) rosterLine(id string) string {
	if mb == nil {
		return ""
	}
	b, ok := mb.reg.Get(id)
	if !ok {
		return ""
	}
	parts := []string{"location=" + string(b.Location), "state=" + string(b.State)}
	if b.Task != "" {
		parts = append(parts, "task="+clipRunes(b.Task, 60))
	}
	if b.Step != "" {
		parts = append(parts, "step="+clipRunes(b.Step, 60))
	}
	return strings.Join(parts, " ")
}

// recordDeliveryArtifacts stores a remote run's reply files so the main bot
// can hand them to another bot or the person.
func (mb *multiBot) recordDeliveryArtifacts(botID, runID string, files []DesktopBotFile) {
	if mb == nil || mb.artifacts == nil {
		return
	}
	for _, f := range files {
		data, err := base64.StdEncoding.DecodeString(f.Data)
		if err != nil || len(data) == 0 {
			continue
		}
		art, err := mb.artifacts.PutBytes(f.Name, f.MIME, botID, runID, data)
		if err != nil {
			log.Printf("[multibot] store artifact: %v", err)
			continue
		}
		mb.reg.Notify(botregistry.EventArtifact, botID, art.ID+" "+art.Name)
	}
}

// appMultiBotHub adapts the existing desktop-bot Hub calls.
type appMultiBotHub struct{ a *App }

func (h appMultiBotHub) roster(ctx context.Context) ([]desktopBotRosterEntry, error) {
	r, err := h.a.loadDesktopBotRoster()
	if err != nil {
		return nil, err
	}
	return r.Bots, nil
}

func (h appMultiBotHub) activeRuns(ctx context.Context, botID string) ([]DesktopBotActiveRun, error) {
	return h.a.ListDesktopBotActiveRuns(botID)
}

func (h appMultiBotHub) cross(ctx context.Context, fromID, kind, targetID, text, order string) error {
	_, err := h.a.postOneDesktopBotCross(fromID, kind, targetID, text, order, "")
	return err
}

func (h appMultiBotHub) cancelRun(ctx context.Context, botID, runID string) error {
	return h.a.CancelDesktopBotRun(botID, runID)
}

func (h appMultiBotHub) pauseRun(ctx context.Context, botID, runID, reason string) error {
	_, err := h.a.PauseDesktopBotRun(botID, runID, reason, 0)
	return err
}

func (h appMultiBotHub) resumeRun(ctx context.Context, botID, runID string) error {
	_, err := h.a.ResumeDesktopBotRun(botID, runID)
	return err
}

// uploadFile sends a local file to the bot's desktop in chunks and returns
// the path on that desktop.
func (h appMultiBotHub) uploadFile(ctx context.Context, botID, path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	uploadID := fmt.Sprintf("mb-%d", time.Now().UnixNano())
	name := filepath.Base(path)
	total := int64(len(data))
	var last *DesktopBotMaterialUpload
	for off := int64(0); off < total || (total == 0 && off == 0); {
		end := off + desktopBotMaterialChunkMax
		if end > total {
			end = total
		}
		out, err := h.a.UploadDesktopBotMaterialChunk(botID, uploadID, name, off, total, base64.StdEncoding.EncodeToString(data[off:end]))
		if err != nil {
			return "", err
		}
		last = out
		if out.Received > off {
			off = out.Received
		} else {
			off = end
		}
		if total == 0 || out.Done {
			break
		}
	}
	if last == nil || strings.TrimSpace(last.Path) == "" {
		return "", fmt.Errorf("upload finished without a desktop path")
	}
	return last.Path, nil
}
