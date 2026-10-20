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

func TestFileCompanionPlayerPlaceIsTopLevelEmbed(t *testing.T) {
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
	if got.URL != "https://www.youtube.com/embed/abc123" {
		t.Fatalf("url = %s", got.URL)
	}
	if got.Bounds != (maclawplayer.Bounds{X: 8, Y: 16, W: 640, H: 360}) {
		t.Fatalf("bounds = %+v", got.Bounds)
	}
	if host.hides != 0 {
		t.Fatalf("hides = %d", host.hides)
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
