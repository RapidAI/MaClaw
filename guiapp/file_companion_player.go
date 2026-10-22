package guiapp

import (
	"errors"

	"github.com/wailsapp/wails/v2/pkg/maclawplayer"
)

// FileCompanionPlayerSupported reports whether this process shows a web page
// as its own top-level document. On Windows that is true before the WebView
// exists, so the page is not framed under the app origin while the controller
// is still being created. A place requested in that interval is kept and
// applied when the host is installed.
func (a *App) FileCompanionPlayerSupported() bool {
	if a == nil {
		return false
	}
	return maclawplayer.Available()
}

// FileCompanionPlayerPlace shows url in the top-level player.
// YouTube's embed address stays the session identity. This WebView loads the
// watch page: the embed player requires a parent Referer, and a top-level
// navigation has none (错误 153). A slot that does not cover a pixel hides
// the page instead of leaving it over the companion window.
func (a *App) FileCompanionPlayerPlace(url string, x, y, w, h float64) error {
	// A newer Place or Hide was already received. Binding calls run on
	// separate goroutines, so this older call can start last and would paint
	// over the page the user has already left.
	if maclawplayer.ConsumeSkipPlace() {
		return nil
	}
	if a == nil {
		return errors.New("app unavailable")
	}
	if sentence := fileCompanionRejectWebInput(url); sentence != "" {
		return errors.New(sentence)
	}
	frame, err := fileCompanionWebFrameURL(url)
	if err != nil {
		return err
	}
	// The queue stamps this from when the call was received. Reading the
	// live generation here would see a show that happened while the call
	// waited, and the minimized box measured after that show would unload
	// the page. A direct call has no stamp and uses the live generation.
	showGen := maclawplayer.PlaceShowGeneration()
	bounds, ok := maclawplayer.BoundsFrom(x, y, w, h)
	if !ok {
		// Hide unloads the document. A minimize or Alt+F4 shrinks the slot
		// before the page has marked the window hidden, so this empty box is
		// the window going away and HostShown skips it. The show message
		// clears that flag before the page measures, so the same empty box
		// arrives while the window already counts as shown. It is still the
		// minimized size until a positive Place received after that show is
		// still the latest receipt when the host returns.
		if maclawplayer.HostShown() && maclawplayer.EmptyHideReady() {
			maclawplayer.Hide()
		}
		return nil
	}
	if err := maclawplayer.Place(fileCompanionTopLevelPlaybackURL(frame), bounds); err != nil {
		return err
	}
	// The host call waits on the UI thread. The minimized box is posted
	// during that wait. Arming before the wait lets that box unload the
	// page. A place that is no longer the latest receipt measured the
	// previous layout.
	if maclawplayer.HostShown() && maclawplayer.PlaceReceiptStillLatest() {
		maclawplayer.NoteSlotPlaced(showGen)
	}
	return nil
}

// FileCompanionPlayerHide removes the top-level player page.
func (a *App) FileCompanionPlayerHide() {
	if a == nil {
		return
	}
	maclawplayer.Hide()
}
