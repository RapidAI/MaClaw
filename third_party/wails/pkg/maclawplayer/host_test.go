package maclawplayer

import (
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/maclawcapture"
)

type recordHost struct {
	places []Placement
	hides  int
}

func (h *recordHost) Place(url string, bounds Bounds) error {
	h.places = append(h.places, Placement{URL: url, Bounds: bounds, Visible: true})
	return nil
}

func (h *recordHost) Hide() { h.hides++ }

func (h *recordHost) Showing() bool { return len(h.places) > h.hides }

func (h *recordHost) View() maclawcapture.View { return nil }

func TestBoundsFromRequiresAPixel(t *testing.T) {
	if _, ok := BoundsFrom(0, 0, 0, 10); ok {
		t.Fatal("zero width")
	}
	if _, ok := BoundsFrom(math.NaN(), 0, 10, 10); ok {
		t.Fatal("nan")
	}
	got, ok := BoundsFrom(-4, 8.4, 20.2, 10.6)
	if !ok {
		t.Fatal("expected a rectangle")
	}
	if got.X != 0 || got.W != 16 || got.Y != 8 || got.H != 11 {
		t.Fatalf("bounds = %+v", got)
	}
}

func TestNextNavigatesWhenThePageWasHidden(t *testing.T) {
	bounds := Bounds{X: 1, Y: 2, W: 3, H: 4}
	first, navigate := Next(Placement{}, "https://www.youtube.com/embed/abc", bounds)
	if !navigate || !first.Visible || first.URL != "https://www.youtube.com/embed/abc" {
		t.Fatalf("first = %+v navigate=%v", first, navigate)
	}
	resized, navigate := Next(first, first.URL, Bounds{W: 9, H: 9})
	if navigate || !resized.Visible || resized.Bounds.W != 9 {
		t.Fatalf("resize = %+v navigate=%v", resized, navigate)
	}
	hidden := first
	hidden.Visible = false
	again, navigate := Next(hidden, first.URL, bounds)
	if !navigate || !again.Visible || again.URL != first.URL {
		t.Fatal("showing a hidden page must request the document")
	}
	other, navigate := Next(first, "https://example.com/a", bounds)
	if !navigate || other.URL != "https://example.com/a" {
		t.Fatalf("other = %+v navigate=%v", other, navigate)
	}
}

func TestReloadCurrentWhenTheDocumentIsAlreadyLoaded(t *testing.T) {
	const page = "https://www.youtube.com/embed/abc"
	if ReloadCurrent("", page) || ReloadCurrent("about:blank", page) || ReloadCurrent(page, "") {
		t.Fatal("a blank document is a navigation")
	}
	if !ReloadCurrent(page, page) {
		t.Fatal("the loaded document must be reloaded")
	}
	if !ReloadCurrent(page+"#t=1", page) {
		t.Fatal("a fragment is the same document")
	}
	if !ReloadCurrent("https://www.youtube.com:443/embed/abc", page) {
		t.Fatal("the default port is the same document")
	}
	if ReloadCurrent(page, "https://www.youtube.com/embed/abc?autoplay=1") {
		t.Fatal("a different query is a navigation")
	}
	if ReloadCurrent(page, "https://www.youtube.com/embed/ABC") {
		t.Fatal("the video id is case sensitive")
	}
	if ReloadCurrent(page, "https://www.youtube.com/embed/other") {
		t.Fatal("a different address is a navigation")
	}
}

func TestShouldUnloadLeavesABlankDocument(t *testing.T) {
	if ShouldUnload("") || ShouldUnload("about:blank") {
		t.Fatal("blank is already unloaded")
	}
	if !ShouldUnload("https://www.youtube.com/embed/abc") {
		t.Fatal("a site document is unloaded when the slot is hidden")
	}
}

func TestAvailableBeforeInstallMatchesThePlatform(t *testing.T) {
	Set(nil)
	t.Cleanup(func() { Set(nil) })
	if Available() != topLevelCapable {
		t.Fatalf("available=%v capable=%v", Available(), topLevelCapable)
	}
}

func TestPlaceBeforeTheHostArrivesIsAppliedWhenTheHostIsInstalled(t *testing.T) {
	Set(nil)
	t.Cleanup(func() { Set(nil) })
	bounds := Bounds{X: 8, Y: 16, W: 640, H: 360}
	err := Place("https://www.youtube.com/embed/abc", bounds)
	if topLevelCapable && err != nil {
		t.Fatal(err)
	}
	if !topLevelCapable && err == nil {
		t.Fatal("expected no host")
	}
	host := &recordHost{}
	Set(host)
	if len(host.places) != 1 {
		t.Fatalf("places = %d", len(host.places))
	}
	if host.places[0].URL != "https://www.youtube.com/embed/abc" || host.places[0].Bounds != bounds {
		t.Fatalf("place = %+v", host.places[0])
	}
}

func TestHideBeforeTheHostArrivesDoesNotOpenThePage(t *testing.T) {
	Set(nil)
	t.Cleanup(func() { Set(nil) })
	_ = Place("https://www.youtube.com/embed/abc", Bounds{W: 10, H: 10})
	Hide()
	host := &recordHost{}
	Set(host)
	if len(host.places) != 0 || host.hides != 0 {
		t.Fatalf("places=%d hides=%d", len(host.places), host.hides)
	}
}

func TestClearingTheHostDropsAWaitingPage(t *testing.T) {
	Set(nil)
	t.Cleanup(func() { Set(nil) })
	_ = Place("https://www.youtube.com/embed/abc", Bounds{W: 10, H: 10})
	Set(nil)
	host := &recordHost{}
	Set(host)
	if len(host.places) != 0 {
		t.Fatalf("places = %d", len(host.places))
	}
}

func TestAPlaceHiddenBeforeTheHostDoesNotPaint(t *testing.T) {
	Set(nil)
	host := &recordHost{}
	Set(host)
	t.Cleanup(func() {
		testBeforeApply = nil
		Set(nil)
	})
	testBeforeApply = Hide
	if err := Place("https://www.youtube.com/embed/abc", Bounds{W: 10, H: 10}); err != nil {
		t.Fatal(err)
	}
	if len(host.places) != 0 || host.hides != 1 {
		t.Fatalf("places=%d hides=%d", len(host.places), host.hides)
	}
}

func TestAReplacedPlaceDoesNotPaintTheOldPage(t *testing.T) {
	Set(nil)
	host := &recordHost{}
	Set(host)
	t.Cleanup(func() {
		testBeforeApply = nil
		Set(nil)
	})
	bounds := Bounds{W: 10, H: 10}
	testBeforeApply = func() {
		testBeforeApply = nil
		if err := Place("https://example.com/next", bounds); err != nil {
			t.Errorf("next place: %v", err)
		}
	}
	if err := Place("https://www.youtube.com/embed/abc", bounds); err != nil {
		t.Fatal(err)
	}
	if host.hides != 0 || len(host.places) != 1 || host.places[0].URL != "https://example.com/next" {
		t.Fatalf("places=%+v hides=%d", host.places, host.hides)
	}
}

// blockingHost holds Place inside the host until release is closed.
// hides counts host Hide calls.
type blockingHost struct {
	entered chan struct{}
	release chan struct{}
	hides   atomic.Int32
	once    sync.Once
}

func (h *blockingHost) Place(string, Bounds) error {
	h.once.Do(func() { close(h.entered) })
	<-h.release
	return nil
}

func (h *blockingHost) Hide()                    { h.hides.Add(1) }
func (h *blockingHost) Showing() bool            { return false }
func (h *blockingHost) View() maclawcapture.View { return nil }

func TestHideDoesNotCallTheHostWhilePlaceHoldsIt(t *testing.T) {
	Set(nil)
	testBeforeApply = nil
	host := &blockingHost{entered: make(chan struct{}), release: make(chan struct{})}
	Set(host)
	var releaseOnce sync.Once
	unlock := func() { releaseOnce.Do(func() { close(host.release) }) }
	placeDone := make(chan struct{})
	hideDone := make(chan struct{})
	t.Cleanup(func() {
		unlock()
		<-placeDone
		<-hideDone
		testBeforeApply = nil
		Set(nil)
	})
	go func() {
		defer close(placeDone)
		if err := Place("https://www.youtube.com/embed/abc", Bounds{W: 10, H: 10}); err != nil {
			t.Errorf("place: %v", err)
		}
	}()
	<-host.entered
	go func() {
		defer close(hideDone)
		Hide()
	}()
	hidden := false
	for i := 0; i < 100000; i++ {
		mu.Lock()
		vis := wanted != nil && wanted.Visible
		mu.Unlock()
		if !vis {
			hidden = true
			break
		}
		runtime.Gosched()
	}
	if !hidden {
		t.Fatal("Hide did not record the page as hidden")
	}
	for i := 0; i < 256; i++ {
		if host.hides.Load() != 0 {
			t.Fatal("Hide called the host while Place still held it")
		}
		runtime.Gosched()
	}
	unlock()
	<-hideDone
	if host.hides.Load() != 1 {
		t.Fatalf("hides=%d", host.hides.Load())
	}
}
