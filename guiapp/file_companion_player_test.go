package guiapp

import (
	"testing"

	"github.com/wailsapp/wails/v2/pkg/maclawcapture"
	"github.com/wailsapp/wails/v2/pkg/maclawplayer"
)

type fakePlayerHost struct {
	places []maclawplayer.Placement
	hides  int
}

func (h *fakePlayerHost) Place(url string, bounds maclawplayer.Bounds) error {
	h.places = append(h.places, maclawplayer.Placement{URL: url, Bounds: bounds, Visible: true})
	return nil
}

func (h *fakePlayerHost) Hide() { h.hides++ }

func (h *fakePlayerHost) Showing() bool { return len(h.places) > h.hides }

func (h *fakePlayerHost) View() maclawcapture.View { return nil }

func TestFileCompanionPlayerPlaceLoadsTheWatchPage(t *testing.T) {
	host := &fakePlayerHost{}
	maclawplayer.Set(host)
	t.Cleanup(func() { maclawplayer.Set(nil) })
	app := &App{}
	if !app.FileCompanionPlayerSupported() {
		t.Fatal("host not installed")
	}
	if err := app.FileCompanionPlayerPlace("https://www.youtube.com/watch?v=abc123&t=12", 8, 16, 640, 360); err != nil {
		t.Fatal(err)
	}
	if len(host.places) != 1 {
		t.Fatalf("places = %d", len(host.places))
	}
	got := host.places[0]
	if got.URL != "https://www.youtube.com/watch?v=abc123" {
		t.Fatalf("url = %s", got.URL)
	}
	if got.Bounds != (maclawplayer.Bounds{X: 8, Y: 16, W: 640, H: 360}) {
		t.Fatalf("bounds = %+v", got.Bounds)
	}
	if host.hides != 0 {
		t.Fatalf("hides = %d", host.hides)
	}
	for _, raw := range []string{
		"https://www.youtube.com/embed/HSspMfWm3Qc",
		"https://youtu.be/HSspMfWm3Qc",
		"https://m.youtube.com/shorts/HSspMfWm3Qc",
		"https://www.youtube.com/live/HSspMfWm3Qc",
		"https://www.youtube-nocookie.com/embed/HSspMfWm3Qc",
	} {
		if err := app.FileCompanionPlayerPlace(raw, 8, 16, 640, 360); err != nil {
			t.Fatal(err)
		}
		got := host.places[len(host.places)-1]
		if got.URL != "https://www.youtube.com/watch?v=HSspMfWm3Qc" {
			t.Fatalf("%s -> %s", raw, got.URL)
		}
	}
	if err := app.FileCompanionPlayerPlace("https://www.youtube.com/embed/videoseries?list=PLrAXtmErZgOeiKm4sgNOknGvNjby9ef3i&t=4", 8, 16, 640, 360); err != nil {
		t.Fatal(err)
	}
	if got := host.places[len(host.places)-1].URL; got != "https://www.youtube.com/playlist?list=PLrAXtmErZgOeiKm4sgNOknGvNjby9ef3i" {
		t.Fatalf("playlist -> %s", got)
	}
	if err := app.FileCompanionPlayerPlace("https://www.youtube.com/embed/live_stream?channel=UC_x5XG1OV2P6uZZ5FSM9Ttw", 8, 16, 640, 360); err != nil {
		t.Fatal(err)
	}
	if got := host.places[len(host.places)-1].URL; got != "https://www.youtube.com/channel/UC_x5XG1OV2P6uZZ5FSM9Ttw/live" {
		t.Fatalf("live -> %s", got)
	}
}

func TestFileCompanionTopLevelPlaybackURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://www.youtube.com/embed/abc123", "https://www.youtube.com/watch?v=abc123"},
		{"https://www.youtube.com/embed/HSspMfWm3Qc", "https://www.youtube.com/watch?v=HSspMfWm3Qc"},
		{"https://youtube.com/embed/keep-chat", "https://www.youtube.com/watch?v=keep-chat"},
		{"https://www.youtube.com/embed/abc123?t=9", "https://www.youtube.com/watch?v=abc123"},
		{"https://www.youtube.com/embed/abc123/extra", "https://www.youtube.com/embed/abc123/extra"},
		{"https://www.youtube.com/embed/ab.c", "https://www.youtube.com/embed/ab.c"},
		{"https://www.youtube.com/embed/", "https://www.youtube.com/embed/"},
		{"https://www.youtube.com/", "https://www.youtube.com/"},
		{"https://www.youtube.com/watch?v=abc123", "https://www.youtube.com/watch?v=abc123"},
		{"https://www.youtube-nocookie.com/embed/abc123", "https://www.youtube.com/watch?v=abc123"},
		{"https://youtube-nocookie.com/embed/HSspMfWm3Qc?rel=0", "https://www.youtube.com/watch?v=HSspMfWm3Qc"},
		{"https://www.youtube-nocookie.com/embed/ab.c", "https://www.youtube-nocookie.com/embed/ab.c"},
		{"https://example.com/embed/abc123", "https://example.com/embed/abc123"},
		{"https://m.youtube.com/embed/abc123", "https://m.youtube.com/embed/abc123"},
		{"https://www.youtube.com/embed/videoseries?list=PLrAXtmErZgOeiKm4sgNOknGvNjby9ef3i", "https://www.youtube.com/playlist?list=PLrAXtmErZgOeiKm4sgNOknGvNjby9ef3i"},
		{"https://www.youtube.com/embed/videoseries", "https://www.youtube.com/embed/videoseries"},
		{"https://www.youtube-nocookie.com/embed/videoseries?list=PLabcdefghij", "https://www.youtube.com/playlist?list=PLabcdefghij"},
		{"https://www.youtube.com/embed/live_stream?channel=UC_x5XG1OV2P6uZZ5FSM9Ttw", "https://www.youtube.com/channel/UC_x5XG1OV2P6uZZ5FSM9Ttw/live"},
		{"https://www.youtube.com/embed/live_stream", "https://www.youtube.com/embed/live_stream"},
		{"https://www.youtube.com/playlist?list=PLabcdefghij", "https://www.youtube.com/playlist?list=PLabcdefghij"},
	}
	for _, tc := range cases {
		if got := fileCompanionTopLevelPlaybackURL(tc.in); got != tc.want {
			t.Fatalf("%s -> %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestFileCompanionPlayerPlaceLeavesOtherPages(t *testing.T) {
	host := &fakePlayerHost{}
	maclawplayer.Set(host)
	t.Cleanup(func() { maclawplayer.Set(nil) })
	app := &App{}
	if err := app.FileCompanionPlayerPlace("https://example.com/watch?v=abc123", 1, 2, 30, 40); err != nil {
		t.Fatal(err)
	}
	if len(host.places) != 1 || host.places[0].URL != "https://example.com/watch?v=abc123" {
		t.Fatalf("places = %+v", host.places)
	}
}

func TestFileCompanionPlayerPlaceKeepsThePageWhenTheWindowIsNotShown(t *testing.T) {
	host := &fakePlayerHost{}
	maclawplayer.Set(host)
	maclawplayer.SetHostShown(func() bool { return false })
	t.Cleanup(func() {
		maclawplayer.SetHostShown(nil)
		maclawplayer.Set(nil)
	})
	app := &App{}
	if err := app.FileCompanionPlayerPlace("https://www.youtube.com/watch?v=abc123", 0, 0, 0, 40); err != nil {
		t.Fatal(err)
	}
	if len(host.places) != 0 || host.hides != 0 {
		t.Fatalf("a hidden window unloaded the player: places=%d hides=%d", len(host.places), host.hides)
	}
	if err := app.FileCompanionPlayerPlace("https://www.youtube.com/watch?v=abc123", 8, 16, 640, 360); err != nil {
		t.Fatal(err)
	}
	if len(host.places) != 1 || host.hides != 0 {
		t.Fatalf("a real slot was dropped: places=%d hides=%d", len(host.places), host.hides)
	}
	maclawplayer.SetHostShown(func() bool { return true })
	if err := app.FileCompanionPlayerPlace("https://www.youtube.com/watch?v=abc123", 0, 0, 0, 40); err != nil {
		t.Fatal(err)
	}
	if host.hides != 1 {
		t.Fatalf("a shown window left the empty slot up: hides=%d", host.hides)
	}
}

func TestFileCompanionPlayerPlaceKeepsThePageAcrossShow(t *testing.T) {
	host := &fakePlayerHost{}
	maclawplayer.Set(host)
	maclawplayer.SetHostShown(func() bool { return true })
	t.Cleanup(func() {
		maclawplayer.ResetShowPlacement()
		maclawplayer.SetHostShown(nil)
		maclawplayer.Set(nil)
	})
	app := &App{}
	if err := app.FileCompanionPlayerPlace("https://www.youtube.com/watch?v=abc123", 8, 16, 640, 360); err != nil {
		t.Fatal(err)
	}
	// The window procedure records the show before the page measures. The
	// box is still the minimized size. A place that started before this
	// show must not arm the following empty hide.
	started := maclawplayer.ShowGeneration()
	maclawplayer.NoteHostVisible()
	maclawplayer.NoteSlotPlaced(started)
	if err := app.FileCompanionPlayerPlace("https://www.youtube.com/watch?v=abc123", 0, 0, 0, 40); err != nil {
		t.Fatal(err)
	}
	if host.hides != 0 || len(host.places) != 1 {
		t.Fatalf("show unloaded the minimized slot: places=%d hides=%d", len(host.places), host.hides)
	}
	if err := app.FileCompanionPlayerPlace("https://www.youtube.com/watch?v=abc123", 8, 16, 640, 360); err != nil {
		t.Fatal(err)
	}
	if err := app.FileCompanionPlayerPlace("https://www.youtube.com/watch?v=abc123", 0, 0, 0, 40); err != nil {
		t.Fatal(err)
	}
	if host.hides != 1 {
		t.Fatalf("a slot that left after it was placed stayed up: hides=%d", host.hides)
	}
}

func TestFileCompanionPlayerPlaceKeepsThePageWhenTheCallWaited(t *testing.T) {
	host := &fakePlayerHost{}
	maclawplayer.Set(host)
	maclawplayer.SetHostShown(func() bool { return true })
	t.Cleanup(func() {
		maclawplayer.ResetShowPlacement()
		maclawplayer.SetHostShown(nil)
		maclawplayer.Set(nil)
	})
	maclawplayer.ResetShowPlacement()
	app := &App{}
	// Received while the previous layout was still on screen, then left
	// waiting until the window procedure had already recorded the show.
	received := maclawplayer.ShowGeneration()
	maclawplayer.NoteHostVisible()
	maclawplayer.PinPlaceShowGeneration(received)
	if err := app.FileCompanionPlayerPlace("https://www.youtube.com/watch?v=abc123", 8, 16, 640, 360); err != nil {
		t.Fatal(err)
	}
	maclawplayer.ClearPlaceShowGeneration()
	if err := app.FileCompanionPlayerPlace("https://www.youtube.com/watch?v=abc123", 0, 0, 0, 40); err != nil {
		t.Fatal(err)
	}
	if host.hides != 0 || len(host.places) != 1 {
		t.Fatalf("a place received before the show unloaded the minimized slot: places=%d hides=%d", len(host.places), host.hides)
	}
	maclawplayer.PinPlaceShowGeneration(maclawplayer.ShowGeneration())
	if err := app.FileCompanionPlayerPlace("https://www.youtube.com/watch?v=abc123", 8, 16, 640, 360); err != nil {
		t.Fatal(err)
	}
	maclawplayer.ClearPlaceShowGeneration()
	if err := app.FileCompanionPlayerPlace("https://www.youtube.com/watch?v=abc123", 0, 0, 0, 40); err != nil {
		t.Fatal(err)
	}
	if host.hides != 1 {
		t.Fatalf("a slot that left after the restored place stayed up: hides=%d", host.hides)
	}
}

func TestFileCompanionPlayerPlaceDoesNotArmWhileANewerCallIsWaiting(t *testing.T) {
	host := &fakePlayerHost{}
	maclawplayer.Set(host)
	maclawplayer.SetHostShown(func() bool { return true })
	t.Cleanup(func() {
		maclawplayer.ResetShowPlacement()
		maclawplayer.ClearPlaceReceipt()
		maclawplayer.SetHostShown(nil)
		maclawplayer.Set(nil)
	})
	maclawplayer.ResetShowPlacement()
	app := &App{}
	maclawplayer.NoteHostVisible()
	maclawplayer.PinPlaceShowGeneration(maclawplayer.ShowGeneration())
	// This place is already on the UI thread. The minimized box was received
	// before the host returned, so it must not find empty-hide armed.
	mine := maclawplayer.NextPlaceReceipt()
	maclawplayer.PinPlaceReceipt(mine)
	_ = maclawplayer.NextPlaceReceipt()
	if err := app.FileCompanionPlayerPlace("https://www.youtube.com/watch?v=abc123", 8, 16, 640, 360); err != nil {
		t.Fatal(err)
	}
	maclawplayer.ClearPlaceShowGeneration()
	maclawplayer.ClearPlaceReceipt()
	if err := app.FileCompanionPlayerPlace("https://www.youtube.com/watch?v=abc123", 0, 0, 0, 40); err != nil {
		t.Fatal(err)
	}
	if host.hides != 0 || len(host.places) != 1 {
		t.Fatalf("a place that waited unloaded the minimized slot: places=%d hides=%d", len(host.places), host.hides)
	}
	maclawplayer.PinPlaceShowGeneration(maclawplayer.ShowGeneration())
	latest := maclawplayer.NextPlaceReceipt()
	maclawplayer.PinPlaceReceipt(latest)
	if err := app.FileCompanionPlayerPlace("https://www.youtube.com/watch?v=abc123", 8, 16, 640, 360); err != nil {
		t.Fatal(err)
	}
	maclawplayer.ClearPlaceShowGeneration()
	maclawplayer.ClearPlaceReceipt()
	if err := app.FileCompanionPlayerPlace("https://www.youtube.com/watch?v=abc123", 0, 0, 0, 40); err != nil {
		t.Fatal(err)
	}
	if host.hides != 1 {
		t.Fatalf("a slot that left after the latest place stayed up: hides=%d", host.hides)
	}
}

func TestFileCompanionPlayerPlaceHidesAnEmptySlot(t *testing.T) {
	host := &fakePlayerHost{}
	maclawplayer.Set(host)
	t.Cleanup(func() { maclawplayer.Set(nil) })
	app := &App{}
	if err := app.FileCompanionPlayerPlace("https://example.com/watch", 0, 0, 0, 40); err != nil {
		t.Fatal(err)
	}
	if len(host.places) != 0 || host.hides != 1 {
		t.Fatalf("places=%d hides=%d", len(host.places), host.hides)
	}
}

func TestFileCompanionPlayerPlaceSkipsWhenANewerCallIsWaiting(t *testing.T) {
	host := &fakePlayerHost{}
	maclawplayer.Set(host)
	t.Cleanup(func() {
		maclawplayer.ClearSkipPlace()
		maclawplayer.Set(nil)
	})
	app := &App{}
	maclawplayer.SkipNextPlace()
	if err := app.FileCompanionPlayerPlace("https://www.youtube.com/watch?v=abc123", 0, 0, 0, 40); err != nil {
		t.Fatal(err)
	}
	if len(host.places) != 0 || host.hides != 0 {
		t.Fatalf("stale place painted or hid: places=%d hides=%d", len(host.places), host.hides)
	}
	if err := app.FileCompanionPlayerPlace("https://www.youtube.com/watch?v=abc123", 8, 16, 640, 360); err != nil {
		t.Fatal(err)
	}
	if len(host.places) != 1 || host.places[0].URL != "https://www.youtube.com/watch?v=abc123" || host.hides != 0 {
		t.Fatalf("places=%+v hides=%d", host.places, host.hides)
	}
}

func TestFileCompanionPlayerPlaceRejectsTheAppOrigin(t *testing.T) {
	host := &fakePlayerHost{}
	maclawplayer.Set(host)
	t.Cleanup(func() { maclawplayer.Set(nil) })
	app := &App{}
	if err := app.FileCompanionPlayerPlace("http://wails.localhost/index.html", 0, 0, 10, 10); err == nil {
		t.Fatal("expected the app page to be rejected")
	}
	if len(host.places) != 0 || host.hides != 0 {
		t.Fatalf("places=%d hides=%d", len(host.places), host.hides)
	}
}
