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
// The address is the same document the open field already accepted. A slot
// that does not cover a pixel hides the page instead of leaving it over the
// companion window.
func (a *App) FileCompanionPlayerPlace(url string, x, y, w, h float64) error {
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
	bounds, ok := maclawplayer.BoundsFrom(x, y, w, h)
	if !ok {
		maclawplayer.Hide()
		return nil
	}
	return maclawplayer.Place(frame, bounds)
}

// FileCompanionPlayerHide removes the top-level player page.
func (a *App) FileCompanionPlayerHide() {
	if a == nil {
		return
	}
	maclawplayer.Hide()
}
