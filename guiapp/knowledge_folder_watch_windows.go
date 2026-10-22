//go:build windows

package guiapp

import _ "unsafe"

// fsnotify already implements ReadDirectoryChangesW subtree watches. The
// feature gate is unexported and defaults off. Enabling it only changes paths
// whose last element is "...", which is the form this watcher passes.
//
//go:linkname knowledgeFolderWatchEnableRecurse github.com/fsnotify/fsnotify.enableRecurse
var knowledgeFolderWatchEnableRecurse bool

func init() {
	knowledgeFolderWatchEnableRecurse = true
}

func knowledgeFolderWatchSupportsRecurse() bool { return true }
