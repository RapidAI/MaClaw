// Package botregistry is the main bot's view of every bot on the account:
// where it runs, what it can do, what it is doing, and what changed.
//
// The main (primary) bot is itself an entry, so it reports its own task and
// state like any other bot while it coordinates the rest. The registry is a
// cache: remote bots are refreshed from Hub, the local bot from the GUI's own
// agent loop. Transitions that the main bot should hear about (finished,
// blocked, needs the person, failed) become Events in an inbox it drains.
package botregistry

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// Location says where a bot executes.
type Location string

const (
	// LocationLocal runs inside MaClaw GUI on the person's own computer.
	LocationLocal Location = "local"
	// LocationRemote runs on a Hub/MaClawSrv desktop.
	LocationRemote Location = "remote"
)

// Capability names one thing a bot can do. The main bot routes work by these.
type Capability string

const (
	CapFileRead    Capability = "file_read"
	CapFileWrite   Capability = "file_write"
	CapShell       Capability = "shell"
	CapHTTPFetch   Capability = "http_fetch"
	CapBrowser     Capability = "browser"
	CapDesktop     Capability = "desktop"
	CapPublish     Capability = "publish"
	CapCoordinator Capability = "coordinator"
)

// LocalCapabilities is what a bot inside MaClaw GUI can do on this machine.
func LocalCapabilities() []Capability {
	return []Capability{CapFileRead, CapFileWrite, CapShell, CapHTTPFetch}
}

// RemoteCapabilities is what a Hub desktop bot can do.
func RemoteCapabilities() []Capability {
	return []Capability{CapBrowser, CapDesktop, CapPublish, CapHTTPFetch}
}

// State is one bot's coarse condition.
type State string

const (
	StateIdle        State = "idle"
	StateRunning     State = "running"
	StateWaitingUser State = "waiting_user"
	StateQueued      State = "queued"
	StatePaused      State = "paused"
	StateError       State = "error"
	StateOffline     State = "offline"
)

// Bot is one registry entry.
type Bot struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Primary      bool         `json:"primary"`
	Self         bool         `json:"self,omitempty"`
	Location     Location     `json:"location"`
	Capabilities []Capability `json:"capabilities"`
	State        State        `json:"state"`
	Task         string       `json:"task,omitempty"`
	Step         string       `json:"step,omitempty"`
	NextStep     string       `json:"next_step,omitempty"`
	RunID        string       `json:"run_id,omitempty"`
	StopReason   string       `json:"stop_reason,omitempty"`
	Queue        int          `json:"queue,omitempty"`
	Paused       bool         `json:"paused,omitempty"`
	Held         []string     `json:"held,omitempty"`
	LastError    string       `json:"last_error,omitempty"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// Has reports whether the bot declares a capability.
func (b Bot) Has(c Capability) bool {
	for _, have := range b.Capabilities {
		if have == c {
			return true
		}
	}
	return false
}

// EventKind is what happened to a bot.
type EventKind string

const (
	EventStarted     EventKind = "started"
	EventFinished    EventKind = "finished"
	EventNeedsUser   EventKind = "needs_user"
	EventFailed      EventKind = "failed"
	EventStopped     EventKind = "stopped"
	EventPaused      EventKind = "paused"
	EventResumed     EventKind = "resumed"
	EventMessage     EventKind = "message"
	EventArtifact    EventKind = "artifact"
	EventAppeared    EventKind = "appeared"
	EventDisappeared EventKind = "disappeared"
)

// Event is one notification for the main bot.
type Event struct {
	Seq    int64     `json:"seq"`
	Kind   EventKind `json:"kind"`
	BotID  string    `json:"bot_id"`
	Name   string    `json:"name,omitempty"`
	RunID  string    `json:"run_id,omitempty"`
	Task   string    `json:"task,omitempty"`
	Detail string    `json:"detail,omitempty"`
	At     time.Time `json:"at"`
}

const inboxMax = 200

// Registry holds bots and the event inbox. The zero value is not usable;
// call New.
type Registry struct {
	mu     sync.Mutex
	bots   map[string]*Bot
	self   string
	seq    int64
	inbox  []Event
	subs   map[int]func(Event)
	subSeq int
	now    func() time.Time
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{bots: map[string]*Bot{}, subs: map[int]func(Event){}, now: time.Now}
}

// SetClock replaces the clock (tests).
func (r *Registry) SetClock(now func() time.Time) {
	r.mu.Lock()
	r.now = now
	r.mu.Unlock()
}

// SetSelf marks which entry is this process's own bot.
func (r *Registry) SetSelf(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.self = strings.TrimSpace(id)
	for bid, b := range r.bots {
		b.Self = bid == r.self
	}
}

// Self returns the id of this process's own bot.
func (r *Registry) Self() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.self
}

// Get returns a copy of one bot.
func (r *Registry) Get(id string) (Bot, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.bots[strings.TrimSpace(id)]
	if !ok {
		return Bot{}, false
	}
	return copyBot(b), true
}

// List returns copies of all bots: self first, then primary, then by name.
func (r *Registry) List() []Bot {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Bot, 0, len(r.bots))
	for _, b := range r.bots {
		out = append(out, copyBot(b))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Self != out[j].Self {
			return out[i].Self
		}
		if out[i].Primary != out[j].Primary {
			return out[i].Primary
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Upsert stores a bot's latest observation and raises events for the
// transitions the main bot cares about. Paused and Held survive refreshes:
// they are this registry's own control state, not something Hub reports.
func (r *Registry) Upsert(next Bot) {
	next.ID = strings.TrimSpace(next.ID)
	if next.ID == "" {
		return
	}
	r.mu.Lock()
	next.UpdatedAt = r.now()
	next.Self = next.ID == r.self
	prev, existed := r.bots[next.ID]
	if existed {
		next.Paused = prev.Paused
		next.Held = append([]string(nil), prev.Held...)
		if next.Paused && next.State == StateIdle {
			next.State = StatePaused
		}
	} else if next.State == "" {
		next.State = StateIdle
	}
	if next.State == "" {
		next.State = StateIdle
	}
	stored := copyBot(&next)
	r.bots[next.ID] = &stored
	var events []Event
	if !existed {
		if r.seq > 0 || len(r.bots) > 1 {
			events = append(events, r.eventLocked(EventAppeared, stored, ""))
		}
	} else {
		events = append(events, r.transitionLocked(*prev, stored)...)
	}
	subs := r.subscribersLocked()
	r.mu.Unlock()
	r.fanout(subs, events)
}

// Prune drops remote bots not in keep (deleted on Hub). Local and self stay.
func (r *Registry) Prune(keep map[string]bool) {
	r.mu.Lock()
	var events []Event
	for id, b := range r.bots {
		if keep[id] || b.Location == LocationLocal || id == r.self {
			continue
		}
		events = append(events, r.eventLocked(EventDisappeared, *b, ""))
		delete(r.bots, id)
	}
	subs := r.subscribersLocked()
	r.mu.Unlock()
	r.fanout(subs, events)
}

// Update edits one bot in place under the lock and raises transition events.
func (r *Registry) Update(id string, fn func(*Bot)) bool {
	r.mu.Lock()
	b, ok := r.bots[strings.TrimSpace(id)]
	if !ok {
		r.mu.Unlock()
		return false
	}
	prev := copyBot(b)
	fn(b)
	b.UpdatedAt = r.now()
	events := r.transitionLocked(prev, copyBot(b))
	subs := r.subscribersLocked()
	r.mu.Unlock()
	r.fanout(subs, events)
	return true
}

// Notify records a free-form event (message, artifact) for the inbox.
func (r *Registry) Notify(kind EventKind, botID, detail string) Event {
	r.mu.Lock()
	b, ok := r.bots[strings.TrimSpace(botID)]
	var bot Bot
	if ok {
		bot = copyBot(b)
	} else {
		bot = Bot{ID: strings.TrimSpace(botID)}
	}
	ev := r.eventLocked(kind, bot, detail)
	subs := r.subscribersLocked()
	r.mu.Unlock()
	r.fanout(subs, []Event{ev})
	return ev
}

// Pause holds new work for a bot. A running run continues; only new
// assignments are held until Resume.
func (r *Registry) Pause(id string) bool {
	return r.Update(id, func(b *Bot) {
		b.Paused = true
		if b.State == StateIdle {
			b.State = StatePaused
		}
	})
}

// Resume clears the hold and returns the held orders, oldest first, for the
// caller to dispatch.
func (r *Registry) Resume(id string) ([]string, bool) {
	var held []string
	ok := r.Update(id, func(b *Bot) {
		held = b.Held
		b.Held = nil
		b.Paused = false
		if b.State == StatePaused {
			b.State = StateIdle
		}
	})
	return held, ok
}

// Hold queues an order for a paused bot. It returns false when the bot is
// not paused (the caller should dispatch now) or unknown.
func (r *Registry) Hold(id, order string) bool {
	held := false
	r.Update(id, func(b *Bot) {
		if !b.Paused {
			return
		}
		b.Held = append(b.Held, order)
		held = true
	})
	return held
}

// Events returns inbox events with Seq > after, oldest first.
func (r *Registry) Events(after int64) []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Event{}
	for _, ev := range r.inbox {
		if ev.Seq > after {
			out = append(out, ev)
		}
	}
	return out
}

// LastSeq is the newest event's sequence number.
func (r *Registry) LastSeq() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seq
}

// Subscribe calls fn for every new event outside the lock. The returned
// func unsubscribes.
func (r *Registry) Subscribe(fn func(Event)) func() {
	r.mu.Lock()
	r.subSeq++
	id := r.subSeq
	r.subs[id] = fn
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.subs, id)
		r.mu.Unlock()
	}
}

func (r *Registry) transitionLocked(prev, next Bot) []Event {
	var out []Event
	if prev.Paused != next.Paused {
		if next.Paused {
			out = append(out, r.eventLocked(EventPaused, next, ""))
		} else {
			out = append(out, r.eventLocked(EventResumed, next, ""))
		}
	}
	if prev.State == next.State && prev.RunID == next.RunID {
		return out
	}
	switch next.State {
	case StateRunning:
		if prev.State != StateRunning || prev.RunID != next.RunID {
			out = append(out, r.eventLocked(EventStarted, next, next.Step))
		}
	case StateWaitingUser:
		out = append(out, r.eventLocked(EventNeedsUser, next, firstNonEmpty(next.StopReason, next.Step)))
	case StateError:
		out = append(out, r.eventLocked(EventFailed, next, firstNonEmpty(next.LastError, next.StopReason)))
	case StateIdle, StatePaused:
		if prev.State == StateRunning || prev.State == StateQueued {
			ev := r.eventLocked(EventFinished, next, next.StopReason)
			ev.RunID = firstNonEmpty(next.RunID, prev.RunID)
			ev.Task = firstNonEmpty(next.Task, prev.Task)
			r.inbox[len(r.inbox)-1] = ev
			out = append(out, ev)
		}
	}
	return out
}

func (r *Registry) eventLocked(kind EventKind, b Bot, detail string) Event {
	r.seq++
	ev := Event{Seq: r.seq, Kind: kind, BotID: b.ID, Name: b.Name, RunID: b.RunID, Task: b.Task, Detail: strings.TrimSpace(detail), At: r.now()}
	r.inbox = append(r.inbox, ev)
	if len(r.inbox) > inboxMax {
		r.inbox = append([]Event(nil), r.inbox[len(r.inbox)-inboxMax:]...)
	}
	return ev
}

func (r *Registry) subscribersLocked() []func(Event) {
	out := make([]func(Event), 0, len(r.subs))
	for _, fn := range r.subs {
		out = append(out, fn)
	}
	return out
}

func (r *Registry) fanout(subs []func(Event), events []Event) {
	for _, ev := range events {
		for _, fn := range subs {
			fn(ev)
		}
	}
}

func copyBot(b *Bot) Bot {
	c := *b
	c.Capabilities = append([]Capability(nil), b.Capabilities...)
	c.Held = append([]string(nil), b.Held...)
	return c
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// Resolve finds bots by id, exact name, or "all" (every bot except self).
// It returns ok=false for an unknown or ambiguous name.
func (r *Registry) Resolve(target string) ([]Bot, bool) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, false
	}
	bots := r.List()
	self := r.Self()
	switch strings.ToLower(target) {
	case "all", "*", "所有", "全部":
		out := []Bot{}
		for _, b := range bots {
			if b.ID != self {
				out = append(out, b)
			}
		}
		return out, len(out) > 0
	case "self", "me", "main", "主bot", "自己":
		for _, b := range bots {
			if b.ID == self {
				return []Bot{b}, true
			}
		}
		return nil, false
	}
	for _, b := range bots {
		if b.ID == target {
			return []Bot{b}, true
		}
	}
	var match []Bot
	for _, b := range bots {
		if strings.EqualFold(strings.TrimSpace(b.Name), target) {
			match = append(match, b)
		}
	}
	if len(match) != 1 {
		return nil, false
	}
	return match, true
}
