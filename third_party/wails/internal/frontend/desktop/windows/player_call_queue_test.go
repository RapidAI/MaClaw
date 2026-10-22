//go:build windows

package windows

import (
	"testing"

	"github.com/wailsapp/wails/v2/pkg/maclawplayer"
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

func TestPlaceGenerationIsTheOneAtReceive(t *testing.T) {
	maclawplayer.ResetShowPlacement()
	t.Cleanup(func() {
		maclawplayer.ResetShowPlacement()
		maclawplayer.ClearPlaceShowGeneration()
	})
	var q playerCallQueue
	started := maclawplayer.ShowGeneration()
	var first, second uint64
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	q.bind(func(message string, skip bool) {
		switch message {
		case "first":
			first = maclawplayer.PlaceShowGeneration()
			close(entered)
			<-release
		case "second":
			second = maclawplayer.PlaceShowGeneration()
		case "tail":
			if maclawplayer.PlaceShowGeneration() != maclawplayer.ShowGeneration() {
				t.Errorf("receive generation leaked: pin=%d show=%d", maclawplayer.PlaceShowGeneration(), maclawplayer.ShowGeneration())
			}
			close(done)
		}
	})
	q.Add("first", true)
	<-entered
	maclawplayer.NoteHostVisible()
	close(release)
	q.Add("second", true)
	q.Add("tail", false)
	<-done
	if first != started {
		t.Fatalf("queued place gen = %d, received at %d", first, started)
	}
	if second != maclawplayer.ShowGeneration() {
		t.Fatalf("place received after the show gen = %d, show = %d", second, maclawplayer.ShowGeneration())
	}
}

func TestPlaceReceiptStaysBehindACallThatArrivedWhileItRan(t *testing.T) {
	maclawplayer.ResetShowPlacement()
	t.Cleanup(func() {
		maclawplayer.ResetShowPlacement()
		maclawplayer.ClearPlaceReceipt()
	})
	var q playerCallQueue
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	var during, after bool
	q.bind(func(message string, skip bool) {
		switch message {
		case "first":
			close(entered)
			<-release
			during = maclawplayer.PlaceReceiptStillLatest()
		case "second":
			after = maclawplayer.PlaceReceiptStillLatest()
			close(done)
		}
	})
	q.Add("first", true)
	<-entered
	q.Add("second", true)
	close(release)
	<-done
	if during {
		t.Fatal("the running place was still the latest after a newer call was received")
	}
	if !after {
		t.Fatal("the newer place was not current")
	}
}
