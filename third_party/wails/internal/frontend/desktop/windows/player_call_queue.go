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
	// showGen is the generation when this Place was received. A show can
	// land while the call waits behind another one. The generation at run
	// time is already the next one, and this box is still the previous layout.
	showGen uint64
	genSet  bool
	// receipt is the order this Place or Hide was queued. It is assigned under
	// the queue lock. A counter taken before that lock can invert run order,
	// and the call that runs last would not count as the latest. The running
	// call arms empty-hide only while this is still the latest receipt.
	receipt    uint64
	receiptSet bool
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
	call := playerCall{message: message, place: place}
	if place {
		// Before the call can wait on the queue lock. NoteHostVisible runs
		// on the window thread and can land while this Place is still queued.
		// Sampling after the lock would stamp a show that happened during
		// that wait onto the previous layout.
		call.showGen = maclawplayer.ShowGeneration()
		call.genSet = true
	}
	q.mu.Lock()
	// Same lock as append, so the last call in the queue holds the latest
	// receipt. Taken outside the lock, two Adds can append in the opposite
	// order and the one that runs last never arms.
	call.receipt = maclawplayer.NextPlaceReceipt()
	call.receiptSet = true
	q.q = append(q.q, call)
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
		if call.genSet {
			maclawplayer.PinPlaceShowGeneration(call.showGen)
		}
		if call.receiptSet {
			maclawplayer.PinPlaceReceipt(call.receipt)
		}
		q.safeRun(run, call.message, skip)
		if call.genSet {
			maclawplayer.ClearPlaceShowGeneration()
		}
		if call.receiptSet {
			maclawplayer.ClearPlaceReceipt()
		}
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
