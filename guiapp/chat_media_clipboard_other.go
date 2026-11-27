//go:build !windows

package guiapp

import "fmt"

func writeChatMediaClipboardPlatform(chatMediaClipboard) error {
	return fmt.Errorf("copying images and files to the clipboard is not supported on this system")
}
