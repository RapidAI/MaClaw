//go:build windows

package guiapp

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

// errCOMApartmentBusy means this OS thread was already initialized, so the
// balance probe cannot start from a count of zero.
var errCOMApartmentBusy = errors.New("com apartment busy")

func TestAIAssistantFileTypeIconDataURLUsesShellAssociation(t *testing.T) {
	app := &App{}
	dir := t.TempDir()
	const n = 8
	urls := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			urls[i], errs[i] = app.AIAssistantFileTypeIconDataURL(filepath.Join(dir, fmt.Sprintf("doc-%d.pdf", i)))
		}(i)
	}
	close(start)
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("pdf icon %d: %v", i, errs[i])
		}
		if urls[i] == "" || urls[i] != urls[0] {
			t.Fatalf("pdf icon %d did not share the type icon", i)
		}
	}
	first := urls[0]
	pngBytes := decodeFileTypeIcon(t, first)
	img := decodeIconImage(t, pngBytes)
	if img.Bounds().Dx() < 16 || img.Bounds().Dy() < 16 || img.Bounds().Dx() > shellIconMaxEdge || img.Bounds().Dy() > shellIconMaxEdge {
		t.Fatalf("pdf icon bounds = %v", img.Bounds())
	}
	if !shellIconVisible(img) {
		t.Fatal("pdf icon has no visible pixels")
	}

	otherPDF := filepath.Join(t.TempDir(), "other", "notes.pdf")
	second, err := app.AIAssistantFileTypeIconDataURL(otherPDF)
	if err != nil {
		t.Fatalf("cached pdf icon: %v", err)
	}
	if second != first {
		t.Fatal("same extension should reuse one shell icon")
	}

	notepad := filepath.Join(os.Getenv("SystemRoot"), "System32", "notepad.exe")
	exeIcon, err := app.AIAssistantFileTypeIconDataURL(notepad)
	if err != nil {
		t.Fatalf("notepad icon: %v", err)
	}
	if exeIcon == first {
		t.Fatal("executable icon should differ from the pdf association")
	}
	exeImg := decodeIconImage(t, decodeFileTypeIcon(t, exeIcon))
	if !shellIconVisible(exeImg) {
		t.Fatal("notepad icon has no visible pixels")
	}
}

// The cloud drive lists Hub files, so a row has a type but no path on this
// machine. A bare type name must still resolve the association.
func TestAIAssistantFileTypeIconDataURLAnswersBareTypeName(t *testing.T) {
	app := &App{}
	url, err := app.AIAssistantFileTypeIconDataURL("file.pdf")
	if err != nil {
		t.Fatalf("bare pdf icon: %v", err)
	}
	img := decodeIconImage(t, decodeFileTypeIcon(t, url))
	if !shellIconVisible(img) {
		t.Fatal("bare pdf icon has no visible pixels")
	}
}

// A suffix no application claims resolves to the shell's blank sheet, which
// says nothing about the type. The cloud drive's variant reports that as "no
// icon" so the caller keeps its own per-type drawing.
func TestAIAssistantFileTypeIconDataURLIfAssociatedSkipsBlankSheet(t *testing.T) {
	app := &App{}
	url, err := app.AIAssistantFileTypeIconDataURLIfAssociated(genericFileTypeIconProbeName)
	if err != nil {
		t.Fatalf("unassociated suffix: %v", err)
	}
	if url != "" {
		t.Fatalf("unassociated suffix returned an icon of %d bytes, want none", len(url))
	}

	// A type with a real association still answers, so the check above is not
	// just rejecting everything.
	pdfURL, err := app.AIAssistantFileTypeIconDataURLIfAssociated("file.pdf")
	if err != nil {
		t.Fatalf("pdf icon: %v", err)
	}
	if pdfURL == "" {
		t.Fatal("pdf should have an associated icon")
	}
}

// The shared attachment path keeps the shell's answer even when it is the blank
// sheet: the attachment strip has its own text badge and must not change.
func TestAIAssistantFileTypeIconDataURLStillAnswersBlankSheet(t *testing.T) {
	app := &App{}
	url, err := app.AIAssistantFileTypeIconDataURL(genericFileTypeIconProbeName)
	if err != nil {
		t.Fatalf("unassociated suffix: %v", err)
	}
	if url == "" {
		t.Fatal("the shared attachment path should still return the shell icon")
	}
}

// Concurrent callers that wait on another lookup must see the same verdict as
// the owner, not a default.
func TestAIAssistantFileTypeIconDataURLIfAssociatedSharesTheVerdict(t *testing.T) {
	app := &App{}
	const n = 8
	urls := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			urls[i], errs[i] = app.AIAssistantFileTypeIconDataURLIfAssociated(genericFileTypeIconProbeName)
		}(i)
	}
	close(start)
	wg.Wait()
	for i := range urls {
		if errs[i] != nil {
			t.Fatalf("caller %d: %v", i, errs[i])
		}
		if urls[i] != "" {
			t.Fatalf("caller %d got an icon for an unassociated suffix", i)
		}
	}
}

func decodeFileTypeIcon(t *testing.T, dataURL string) []byte {
	t.Helper()
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(dataURL, prefix) {
		t.Fatalf("icon = %q, want a png data URL", dataURL[:min(len(dataURL), 32)])
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(dataURL, prefix))
	if err != nil {
		t.Fatalf("decode data URL: %v", err)
	}
	return raw
}

func decodeIconImage(t *testing.T, raw []byte) *image.NRGBA {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	if nrgba, ok := img.(*image.NRGBA); ok {
		return nrgba
	}
	out := image.NewNRGBA(img.Bounds())
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			out.Set(x, y, img.At(x, y))
		}
	}
	return out
}

func TestShellFileTypeIconCOMInitBalancesSFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "probe.pdf")
	// Other tests can return an OS thread to the pool still inside a COM
	// apartment. Park those threads and try another until one starts at zero.
	var parked []chan struct{}
	t.Cleanup(func() {
		for _, release := range parked {
			close(release)
		}
	})
	var last error
	for attempt := 0; attempt < 8; attempt++ {
		release := make(chan struct{})
		errCh := make(chan error, 1)
		go func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil {
				errCh <- errCOMApartmentBusy
				<-release
				return
			}
			errCh <- balanceShellFileTypeIconCOM(path)
		}()
		err := <-errCh
		if err == nil {
			return
		}
		if !errors.Is(err, errCOMApartmentBusy) {
			t.Fatal(err)
		}
		last = err
		parked = append(parked, release)
	}
	t.Fatalf("no clean COM thread: %v", last)
}

// balanceShellFileTypeIconCOM checks the lookup against a caller that already
// holds one COM init on this thread. The caller releases nothing; this does.
func balanceShellFileTypeIconCOM(path string) error {
	if _, err := shellFileTypeIconPNG(path); err != nil {
		windows.CoUninitialize()
		return err
	}
	// Our init is still held, so this call is S_FALSE and adds one. The
	// lookup must not have dropped the caller's init or left its own behind.
	mid := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED)
	if mid == nil {
		windows.CoUninitialize()
		windows.CoUninitialize()
		return errors.New("icon lookup released the caller's COM initialization")
	}
	if mid != syscall.Errno(1) {
		windows.CoUninitialize()
		return fmt.Errorf("COM after icon lookup: %v", mid)
	}
	windows.CoUninitialize() // drop the probe
	windows.CoUninitialize() // drop the caller's init

	leaked := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED)
	if leaked != nil {
		windows.CoUninitialize()
		if leaked == syscall.Errno(1) {
			windows.CoUninitialize()
		}
		return fmt.Errorf("icon lookup left COM initialized: %v", leaked)
	}
	windows.CoUninitialize()
	return nil
}

func TestBgraToNRGBASwapsChannelsAndRejectsShortBuffers(t *testing.T) {
	img, err := bgraToNRGBA([]byte{10, 20, 30, 255}, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	px := img.NRGBAAt(0, 0)
	if px.R != 30 || px.G != 20 || px.B != 10 || px.A != 255 {
		t.Fatalf("pixel = %+v", px)
	}
	if _, err := bgraToNRGBA([]byte{1, 2, 3}, 1, 1); err == nil {
		t.Fatal("expected a short bitmap error")
	}
	if _, err := bgraToNRGBA(nil, 0, 1); err == nil {
		t.Fatal("expected a zero-size bitmap error")
	}
}

func TestUnpremultiplyAndTrimSkipCopiesWhenNothingChanges(t *testing.T) {
	opaque := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	opaque.SetNRGBA(0, 0, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	if unpremultiplyIcon(opaque) != opaque {
		t.Fatal("opaque icon should be returned without a copy")
	}

	partial := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	partial.SetNRGBA(0, 0, color.NRGBA{R: 128, G: 0, B: 0, A: 128})
	out := unpremultiplyIcon(partial)
	if out == partial {
		t.Fatal("partial alpha should be unpremultiplied into a new image")
	}
	if got := out.NRGBAAt(0, 0); got.R != 255 || got.A != 128 {
		t.Fatalf("unpremultiplied pixel = %+v", got)
	}

	full := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			full.SetNRGBA(x, y, color.NRGBA{R: 1, G: 2, B: 3, A: 255})
		}
	}
	if trimIconAlpha(full) != full {
		t.Fatal("full-bleed icon should not be cropped")
	}
	empty := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	if trimIconAlpha(empty) != empty {
		t.Fatal("blank icon should stay unchanged")
	}

	hot := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	hot.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 128})
	if got := unpremultiplyIcon(hot).NRGBAAt(0, 0); got.R != 255 {
		t.Fatalf("unpremultiplied channel = %d, want 255", got.R)
	}
}

func TestShellIconVisibleRejectsBlankImages(t *testing.T) {
	if shellIconVisible(nil) || shellIconVisible(image.NewNRGBA(image.Rect(0, 0, 32, 32))) {
		t.Fatal("blank icon should be rejected")
	}
	tiny := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	tiny.SetNRGBA(1, 1, color.NRGBA{A: 255})
	if shellIconVisible(tiny) {
		t.Fatal("tiny icon should be rejected")
	}
	glyph := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	glyph.SetNRGBA(8, 8, color.NRGBA{R: 200, A: 255})
	if !shellIconVisible(glyph) {
		t.Fatal("glyph should be visible")
	}
}
