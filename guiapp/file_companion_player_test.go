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
