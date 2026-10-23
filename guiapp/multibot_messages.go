package guiapp

// multibot_messages.go is the main bot's end of the Hub message broker.
// Hub is the durable broker: every bot-to-bot message (main ↔ teammate,
// peer ↔ peer) is a kind "report" note stored in the target bot's Hub
// mailbox until that bot acks it. Remote bots send with MaClawSrv's
// desktop notify_main and read their own mailbox at each turn start (and
// with check_inbox), and Hub itself reports when a delegated run ends, so
// none of that needs this GUI. This file only reads the mailbox of the
// account primary (the relay identity of this local main bot): unacked
// messages are replayed on every read, so whatever arrived while the GUI
// was closed is delivered on reconnect; a message is acked on the read
// after it was handed to the main bot (at-least-once; the Bus dedupes a
// replay by note id). Delivery: idle main bot → one synthetic turn
// ("multibot-wake", sent by the frontend into the main chat); busy → queued
// until its turn ends. The main bot answers with send_to_bot(reply_to=...),
// which posts a report into the teammate's mailbox.

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/botregistry"
)

// multiBotWakeDebounce batches a burst (finished + file delivered) into one wake.
const multiBotWakeDebounce = 1500 * time.Millisecond

// multiBotNoteHub is the optional Hub surface for reports. appMultiBotHub
// implements it; a hub without it simply never yields reports.
type multiBotNoteHub interface {
	// mailbox acks ack and returns the primary's still-unacked messages.
	mailbox(ctx context.Context, ack []string) ([]DesktopBotCrossNote, error)
	report(ctx context.Context, fromID, targetID, topic, text, replyTo string, artifacts []string) (DesktopBotCrossNote, error)
}

// multiBotMailFiles is the optional Hub surface for files shared mid-run.
type multiBotMailFiles interface {
	mailFile(ctx context.Context, noteID, name string) (string, string, []byte, error)
}

// MultiBotWake is the payload of the "multibot-wake" event.
type MultiBotWake struct {
	Prompt  string   `json:"prompt"`
	Display string   `json:"display"`
	IDs     []string `json:"ids"`
}

func isTeammateWake(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "[队友消息")
}

func (mb *multiBot) initMessages() {
	mb.bus = botregistry.NewBus(botregistry.BusLimits{})
	mb.inbox = botregistry.NewInbox(mb.queueWake)
	mb.reg.Subscribe(mb.onRegistryEvent)
}

func (mb *multiBot) hubNotes() multiBotNoteHub {
	if mb == nil || mb.hub == nil {
		return nil
	}
	h, _ := mb.hub.(multiBotNoteHub)
	return h
}

// receive submits one message addressed to the main bot and delivers it.
func (mb *multiBot) receive(m botregistry.Message) (botregistry.Message, error) {
	if m.FromName == "" {
		if b, ok := mb.reg.Get(m.From); ok {
			m.FromName = b.Name
		}
	}
	m.To = multiBotSelfID
	stored, err := mb.bus.Submit(m)
	if err != nil {
		if !errors.Is(err, botregistry.ErrDuplicate) {
			log.Printf("[multibot] drop message from %s: %v", m.From, err)
		}
		return stored, err
	}
	if stored.Via != "watch" {
		mb.reg.Notify(botregistry.EventMessage, stored.From, string(stored.Kind)+": "+clipRunes(stored.Text, 200))
	}
	mb.inbox.Deliver(stored)
	return stored, nil
}

// queueWake is the Inbox's wake: it debounces, then emits one event.
func (mb *multiBot) queueWake(msgs []botregistry.Message) {
	mb.mu.Lock()
	mb.wakeBuf = append(mb.wakeBuf, msgs...)
	if mb.wakeTimer == nil {
		mb.wakeTimer = time.AfterFunc(multiBotWakeDebounce, mb.flushWake)
	}
	mb.mu.Unlock()
}

func (mb *multiBot) flushWake() {
	mb.mu.Lock()
	msgs := mb.wakeBuf
	mb.wakeBuf = nil
	mb.wakeTimer = nil
	mb.mu.Unlock()
	if len(msgs) == 0 || mb.emit == nil {
		return
	}
	ids := make([]string, 0, len(msgs))
	for _, m := range msgs {
		ids = append(ids, m.ID)
	}
	mb.emit("multibot-wake", MultiBotWake{Prompt: botregistry.WakePrompt(msgs), Display: botregistry.WakeDisplay(msgs), IDs: ids})
}

// onRegistryEvent turns a teammate's blocked state or delivered file into a message.
func (mb *multiBot) onRegistryEvent(ev botregistry.Event) {
	if ev.BotID == multiBotSelfID || ev.BotID == "" {
		return
	}
	var kind botregistry.MessageKind
	text := strings.TrimSpace(ev.Detail)
	// Run ends (done / failed) come from Hub's own report on delegated runs,
	// so they reach the main bot with the GUI closed too. Here only what Hub
	// does not report: a mid-run "needs the person" and the local artifact
	// id of a delivered file.
	switch ev.Kind {
	case botregistry.EventNeedsUser:
		kind, text = botregistry.MsgBlocked, firstNonEmptyMB(text, "需要人处理 / needs the person")
	case botregistry.EventArtifact:
		kind = botregistry.MsgArtifact
	default:
		return
	}
	if ev.Task != "" && ev.Kind != botregistry.EventArtifact {
		text = clipRunes(ev.Task, 80) + " — " + text
	}
	m := botregistry.Message{From: ev.BotID, FromName: ev.Name, Kind: kind, Text: text, RunID: ev.RunID, Via: "watch",
		ExternalID: "ev:" + string(ev.Kind) + ":" + ev.BotID + ":" + ev.RunID + ":" + ev.Detail}
	if kind == botregistry.MsgArtifact {
		if id := strings.Fields(text); len(id) > 0 {
			m.Artifacts = []string{id[0]}
		}
	}
	// Registry callbacks run under no lock of ours, but keep Notify re-entry
	// (receive emits EventMessage only for non-watch messages) off this path.
	go func() { _, _ = mb.receive(m) }()
}

func firstNonEmptyMB(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// pollReports reads the primary's Hub mailbox: it acks what the previous
// read handed over, then delivers whatever is still unacked (the backlog
// after a restart included).
func (mb *multiBot) pollReports(ctx context.Context) {
	h := mb.hubNotes()
	if h == nil {
		return
	}
	mb.mu.Lock()
	ack := append([]string(nil), mb.pendingAck...)
	mb.mu.Unlock()
	notes, err := h.mailbox(ctx, ack)
	if err != nil {
		return
	}
	mb.mu.Lock()
	mb.pendingAck = mb.pendingAck[len(ack):]
	mb.mu.Unlock()
	relay := ""
	if r, err := mb.hubRelay(ctx); err == nil {
		relay = r
	}
	for _, n := range notes {
		taken := true
		if n.From != relay {
			arts := append(append([]string(nil), n.Artifacts...), mb.fetchMailFiles(ctx, n)...)
			_, err := mb.receive(botregistry.Message{
				From: n.From, Kind: botregistry.NormalizeKind(n.Topic), Text: n.Text, Artifacts: arts,
				RunID: n.RunID, ReplyTo: n.ReplyTo, Via: "hub", ExternalID: "note:" + n.ID,
			})
			// Rate/loop refusals are final for this message; only a bad
			// local state would leave it for the next read.
			taken = err == nil || errors.Is(err, botregistry.ErrDuplicate) || errors.Is(err, botregistry.ErrRateLimited) || errors.Is(err, botregistry.ErrLoop) || errors.Is(err, botregistry.ErrBadMessage)
		}
		if taken {
			mb.mu.Lock()
			mb.pendingAck = append(mb.pendingAck, n.ID)
			mb.mu.Unlock()
		}
	}
}

// startTeamInbox starts the watcher at app start so reports wake the main
// bot even before it has used a team tool. Safe to call more than once.
func (a *App) startTeamInbox() {
	if mb := a.multiBot(); mb != nil {
		mb.ensureWatch()
	}
}

func (h appMultiBotHub) mailbox(ctx context.Context, ack []string) ([]DesktopBotCrossNote, error) {
	return h.a.desktopBotMailbox("main", ack)
}

func (h appMultiBotHub) mailFile(ctx context.Context, noteID, name string) (string, string, []byte, error) {
	return h.a.desktopBotMailFile(noteID, name)
}

// fetchMailFiles stores the files a teammate shared with message n as local
// artifacts right away (not at its run end) and returns "art_id=path" entries.
// A file that cannot be fetched now is retried on the next read only if the
// message is still unacked; the desktop path stays in the message either way.
func (mb *multiBot) fetchMailFiles(ctx context.Context, n DesktopBotCrossNote) []string {
	if len(n.Files) == 0 || mb.artifacts == nil || mb.hub == nil {
		return nil
	}
	h, ok := mb.hub.(multiBotMailFiles)
	if !ok {
		return nil
	}
	out := []string{}
	for _, f := range n.Files {
		name, mime, data, err := h.mailFile(ctx, n.ID, f.Name)
		if err != nil || len(data) == 0 {
			continue
		}
		art, err := mb.artifacts.PutBytes(firstNonEmptyMB(name, f.Name), firstNonEmptyMB(mime, f.MIME), n.From, n.RunID, data)
		if err != nil {
			log.Printf("[multibot] store shared file: %v", err)
			continue
		}
		out = append(out, art.ID+"="+art.Path)
	}
	return out
}

func (h appMultiBotHub) report(ctx context.Context, fromID, targetID, topic, text, replyTo string, artifacts []string) (DesktopBotCrossNote, error) {
	return h.a.postDesktopBotReport(fromID, targetID, topic, text, replyTo, artifacts)
}
