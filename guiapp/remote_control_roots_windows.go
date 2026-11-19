//go:build windows

package guiapp

import "golang.org/x/sys/windows"

// remoteControlListRoots reports the top-level entries a directory browser
// can jump to: every drive letter the OS reports. GetLogicalDrives reads a
// kernel bitmask — instant, and it never blocks on a stalled network-mapped
// drive the way per-letter os.Stat can (the Hub call budget is 20s).
func remoteControlListRoots() []string {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}
	roots := make([]string, 0, 26)
	for i := 0; i < 26; i++ {
		if mask&(1<<i) != 0 {
			roots = append(roots, string(rune('A'+i))+`:\`)
		}
	}
	return roots
}
