//go:build !windows

package guiapp

import "fmt"

func openPetMic() (petMic, error) {
	return nil, fmt.Errorf("desktop pet microphone capture is only implemented on Windows")
}
