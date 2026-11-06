//go:build !windows

package guiapp

import "os"

// sealSkillSubtreeListing removes list permission from dir. The parent stays
// listable. The returned function restores owner access so the test temp
// directory can be removed.
func sealSkillSubtreeListing(dir string) (func(), error) {
	if err := os.Chmod(dir, 0); err != nil {
		return nil, err
	}
	return func() { _ = os.Chmod(dir, 0o755) }, nil
}
