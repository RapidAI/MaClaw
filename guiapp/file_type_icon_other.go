//go:build !windows

package guiapp

func shellFileTypeIconPNG(path string) ([]byte, error) {
	return nil, ErrFileTypeIconUnsupported
}
