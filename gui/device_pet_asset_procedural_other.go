//go:build !windows

package main

import (
	"image"
	"image/color"
	"math"
)

// renderDeviceProceduralPet provides the deterministic device animation on
// hosts without the Windows floating-window renderer.
func renderDeviceProceduralPet(size int, skin string, phase float64) image.Image {
	if size <= 0 {
		size = devicePetAssetWidth
	}
	frame := image.NewNRGBA(image.Rect(0, 0, size, size))
	accent := color.NRGBA{R: 99, G: 102, B: 241, A: 255}
	body := color.NRGBA{R: 111, G: 125, B: 92, A: 255}
	if skin == "mini-claw" {
		accent = color.NRGBA{R: 37, G: 99, B: 235, A: 255}
		body = color.NRGBA{R: 191, G: 219, B: 254, A: 255}
	} else if skin == "dev-claw" {
		accent = color.NRGBA{R: 34, G: 211, B: 238, A: 255}
		body = color.NRGBA{R: 30, G: 41, B: 59, A: 255}
	}
	center := float64(size) / 2
	headY := float64(size)*0.45 + math.Sin(phase*2)*float64(size)*0.012
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float64(x)-center, float64(y)-float64(size)*0.69
			if dx*dx/(float64(size)*0.31*float64(size)*0.31)+dy*dy/(float64(size)*0.18*float64(size)*0.18) <= 1 {
				frame.SetNRGBA(x, y, body)
			}
			dx, dy = float64(x)-center, float64(y)-headY
			if dx*dx+dy*dy <= float64(size)*0.31*float64(size)*0.31 {
				frame.SetNRGBA(x, y, color.NRGBA{R: 248, G: 250, B: 252, A: 255})
			}
		}
	}
	eyeY := int(headY - float64(size)*0.04)
	eyeOffset := int(float64(size)*0.12 + math.Sin(phase)*float64(size)*0.01)
	renderDevicePetDot(frame, int(center)-eyeOffset, eyeY, int(float64(size)*0.045), color.NRGBA{R: 45, G: 55, B: 72, A: 255})
	renderDevicePetDot(frame, int(center)+eyeOffset, eyeY, int(float64(size)*0.045), color.NRGBA{R: 45, G: 55, B: 72, A: 255})
	renderDevicePetDot(frame, int(center), int(headY-float64(size)*0.39), int(float64(size)*0.025), accent)
	return frame
}

func renderDevicePetDot(frame *image.NRGBA, cx, cy, radius int, fill color.NRGBA) {
	for y := cy - radius; y <= cy+radius; y++ {
		for x := cx - radius; x <= cx+radius; x++ {
			if image.Pt(x, y).In(frame.Bounds()) && (x-cx)*(x-cx)+(y-cy)*(y-cy) <= radius*radius {
				frame.SetNRGBA(x, y, fill)
			}
		}
	}
}
