package computeruse

// ApplyDisplayGeometry fills origin and scale from the captured display's
// logical geometry versus the actual screenshot pixel size.
//
// Screenshot boxes (YOLO, vision clicks) live in capture/image space with
// origin at the top-left of the captured region. InputSimulator and UIA
// FromPoint use virtual-desktop coordinates. ScaleFactor maps image pixels
// to those screen units (Retina 2.0, DPI-unaware 1.25, etc.).
func ApplyDisplayGeometry(meta *ScreenMeta, originX, originY, logicalW, logicalH, imageW, imageH int) {
	if meta == nil {
		return
	}
	meta.OriginX = originX
	meta.OriginY = originY
	if imageW > 0 {
		meta.Width = imageW
	}
	if imageH > 0 {
		meta.Height = imageH
	}
	meta.ScaleFactor = 1.0
	if logicalW > 0 && imageW > 0 {
		s := float64(imageW) / float64(logicalW)
		if s > 0.1 && s < 8 {
			meta.ScaleFactor = s
		}
	}
}

// MapCaptureToScreen converts a point in screenshot/capture pixels into
// virtual-desktop coordinates used by InputSimulator and accessibility.
func MapCaptureToScreen(meta ScreenMeta, x, y int) (int, int) {
	scale := meta.ScaleFactor
	if scale <= 0 {
		scale = 1
	}
	return meta.OriginX + int(float64(x)/scale+0.5), meta.OriginY + int(float64(y)/scale+0.5)
}

// MapScreenToCapture converts a virtual-desktop point into capture/image pixels.
func MapScreenToCapture(meta ScreenMeta, x, y int) (int, int) {
	scale := meta.ScaleFactor
	if scale <= 0 {
		scale = 1
	}
	return int(float64(x-meta.OriginX)*scale + 0.5), int(float64(y-meta.OriginY)*scale + 0.5)
}

// ScaleSize maps a screen-space size into capture pixels.
func ScaleSize(meta ScreenMeta, w, h int) (int, int) {
	scale := meta.ScaleFactor
	if scale <= 0 {
		scale = 1
	}
	return int(float64(w)*scale + 0.5), int(float64(h)*scale + 0.5)
}
