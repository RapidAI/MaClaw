//go:build windows

package windows

// captionDragBlocked reports that a title-bar move must be refused. Only a
// real fullscreen frame is pinned: on a maximised window a caption drag
// performs the standard Windows restore-then-move, which lets users drag the
// window to another monitor on low-resolution screens that start maximised.
func captionDragBlocked(fullscreen bool) bool {
	return fullscreen
}

func (w *Window) captionDragBlocked() bool {
	if w == nil {
		return true
	}
	return captionDragBlocked(w.IsFullScreen())
}
