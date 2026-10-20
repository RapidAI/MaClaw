//go:build windows

package windows

import (
	"testing"
)

func TestCompanionPlayerCallRecognizesPlaceAndHide(t *testing.T) {
	place, ok := companionPlayerCall(`C{"name":"main.App.FileCompanionPlayerPlace","args":[],"callbackID":"1"}`)
	if !ok || !place {
		t.Fatal("place")
	}
	place, ok = companionPlayerCall(`C{"name":"guiapp.App.FileCompanionPlayerHide","args":null,"callbackID":"2"}`)
	if !ok || place {
		t.Fatal("hide")
	}
	if _, ok = companionPlayerCall(`C{"name":"main.App.FileCompanionOpenWebPage","args":[],"callbackID":"3"}`); ok {
		t.Fatal("other call")
	}
	if _, ok = companionPlayerCall(`c{"id":1}`); ok {
		t.Fatal("secure call")
	}
}

func TestPlayerCallsRunInReceiveOrder(t *testing.T) {
	var q playerCallQueue
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	var order []string
	q.bind(func(message string, skip bool) {
		order = append(order, message)
		if skip {
			t.Errorf("%s was skipped", message)
		}
		if message == "place" {
			close(entered)
			<-release
		}
		if message == "hide" {
			close(done)
		}
	})
	q.Add("place", true)
	<-entered
	q.Add("hide", false)
	close(release)
	<-done
	if len(order) != 2 || order[0] != "place" || order[1] != "hide" {
		t.Fatalf("order = %v", order)
	}
}

func TestQueuedPlaceIsSkippedWhenANewerCallIsWaiting(t *testing.T) {
	var q playerCallQueue
	var got []string
	var skips []bool
	q.bind(func(message string, skip bool) {
		got = append(got, message)
		skips = append(skips, skip)
	})
	q.mu.Lock()
	q.q = []playerCall{{message: "place", place: true}, {message: "hide", place: false}}
	q.running = true
	q.mu.Unlock()
	q.drain()
	if len(got) != 2 || got[0] != "place" || !skips[0] || got[1] != "hide" || skips[1] {
		t.Fatalf("got=%v skips=%v", got, skips)
	}
}
