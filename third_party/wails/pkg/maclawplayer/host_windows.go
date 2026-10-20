//go:build windows

package maclawplayer

// topLevelCapable is true when this process can show the playback document
// as its own top-level page. The controller is created later. Reporting the
// capability first keeps that page from being framed under the app origin.
const topLevelCapable = true
