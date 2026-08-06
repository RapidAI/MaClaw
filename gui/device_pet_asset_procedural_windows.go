//go:build windows

package main

import "image"

func renderDeviceProceduralPet(size int, skin string, phase float64) image.Image {
	return renderClawMatePetWithPose(size, skin, petFacePoseForPhase(phase, "balanced"))
}
