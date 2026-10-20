//go:build windows

package windows

import (
	"log"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/maclawplayer"
)

// playerCall is one companion Place or Hide in the order the page sent it.
type playerCall struct {
	message string
	place   bool
}

// playerCallQueue runs companion player calls one at a time, in receive order.
// processMessage starts a goroutine per binding call. A Hide that arrives
// second can then finish before the Place goroutine starts, and that Place
// paints the page the user has already left.
type playerCallQueue struct {
	mu      sync.Mutex
	q       []playerCall
	running bool
	run     func(message string, skip bool)
}

func companionPlayerCall(message string) (place bool, ok bool) {
	if message == "" || message[0] != 'C' {
		return false, false
	}
	switch {
	case strings.Contains(message, `"name":"main.App.FileCompanionPlayerPlace"`),
		strings.Contains(message, `"name":"guiapp.App.FileCompanionPlayerPlace"`):
		return true, true
	case strings.Contains(message, `"name":"main.App.FileCompanionPlayerHide"`),
		strings.Contains(message, `"name":"guiapp.App.FileCompanionPlayerHide"`):
		return false, true
	default:
		return false, false
	}
}

func (f *Frontend) enqueueCompanionPlayerCall(message string, place bool) {
	f.playerCalls.bind(func(msg string, skip bool) {
		if skip {
			maclawplayer.SkipNextPlace()
		}
		defer maclawplayer.ClearSkipPlace()
		f.dispatchMessage(msg)
	})
	f.playerCalls.Add(message, place)
}

func (q *playerCallQueue) bind(run func(string, bool)) {
	q.mu.Lock()
	if q.run == nil {
		q.run = run
	}
	q.mu.Unlock()
}

func (q *playerCallQueue) Add(message string, place bool) {
	q.mu.Lock()
	q.q = append(q.q, playerCall{message: message, place: place})
	start := !q.running
	if start {
		q.running = true
	}
	q.mu.Unlock()
	if start {
		go q.drain()
	}
}

func (q *playerCallQueue) drain() {
	for {
		q.mu.Lock()
		if len(q.q) == 0 {
			q.running = false
			q.mu.Unlock()
			return
		}
		call := q.q[0]
		q.q = q.q[1:]
		// A Place with anything queued behind it is already out of date.
		// Hide still runs, so a leave is not dropped in front of a later show.
		skip := call.place && len(q.q) > 0
		run := q.run
		q.mu.Unlock()
		q.safeRun(run, call.message, skip)
	}
}

func (q *playerCallQueue) safeRun(run func(string, bool), message string, skip bool) {
	defer func() {
		if recover() != nil {
			log.Printf("[companion-player] call failed")
		}
	}()
	if run != nil {
		run(message, skip)
	}
}
