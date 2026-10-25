//go:build !windows

package guiapp

import "errors"

func shellFileTypeIconPNG(path string) ([]byte, error) {
	return nil, errors.New("file type icons are provided by the host operating system")
}
