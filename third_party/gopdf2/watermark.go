package gopdf

import (
	"math"
)

// WatermarkOption configures how a text watermark is rendered.
type WatermarkOption struct {
	// Text is the watermark string to display.
	Text string

	// FontFamily is the font family to use (must be pre-loaded via AddTTFFont).
	FontFamily string

	// FontSize is the font size in points. Default: 48.
	FontSize float64

	// Angle is the rotation angle in degrees. Default: 45.
	Angle float64

	// Color is the RGB color of the watermark text. Default: light gray (200,200,200).
	Color [3]uint8

	// Opacity is the transparency level (0.0 = invisible, 1.0 = opaque). Default: 0.3.
	Opacity float64

	// Repeat tiles the watermark across the page when true.
	Repeat bool

	// RepeatSpacingX is the horizontal spacing between repeated watermarks. Default: 150.
	RepeatSpacingX float64

	// RepeatSpacingY is the vertical spacing between repeated watermarks. Default: 150.
	RepeatSpacingY float64
}

func (o *WatermarkOption) defaults() {
	if o.FontSize <= 0 {
		o.FontSize = 48
	}
	if o.Angle == 0 {
		o.Angle = 45
	}
	if o.Color == [3]uint8{0, 0, 0} {
		o.Color = [3]uint8{200, 200, 200}
	}
	if o.Opacity <= 0 {
		o.Opacity = 0.3
	}
	if o.RepeatSpacingX <= 0 {
		o.RepeatSpacingX = 150
	}
	if o.RepeatSpacingY <= 0 {
		o.RepeatSpacingY = 150
	}
}

// AddWatermarkText adds a text watermark to the current page.
// The watermark is rendered with the specified font, color, opacity, and rotation.
// If Repeat is true, the watermark is tiled across the entire page.
//
// Example:
//
//	pdf.SetPage(1)
//	pdf.AddWatermarkText(WatermarkOption{
//	    Text:       "CONFIDENTIAL",
//	    FontFamily: "myfont",
//	    FontSize:   48,
//	    Opacity:    0.3,
//	    Angle:      45,
//	})
func (gp *GoPdf) AddWatermarkText(opt WatermarkOption) error {
	if opt.Text == "" {
		return ErrEmptyString
	}
	if opt.FontFamily == "" {
		return ErrMissingFontFamily
	}
	opt.defaults()

	// Save current state.
	var origFontFamily string
	origFontSize := gp.curr.FontSize
	origFontStyle := gp.curr.FontStyle
	if gp.curr.FontISubset != nil {
		origFontFamily = gp.curr.FontISubset.GetFamily()
	}

	// Set watermark font.
	if err := gp.SetFont(opt.FontFamily, "", opt.FontSize); err != nil {
		return err
	}

	// Set transparency.
	if err := gp.SetTransparency(Transparency{
		Alpha:         opt.Opacity,
		BlendModeType: "",
	}); err != nil {
		return err
	}

	gp.SetTextColor(opt.Color[0], opt.Color[1], opt.Color[2])

	// Measure text width for centering.
	textW, err := gp.MeasureTextWidth(opt.Text)
	if err != nil {
		return err
	}

	pageW := gp.config.PageSize.W
	pageH := gp.config.PageSize.H

	// Check if current page has custom size.
	if gp.curr.pageSize != nil {
		pageW = gp.curr.pageSize.W
		pageH = gp.curr.pageSize.H
	}

	gp.SaveGraphicsState()

	if opt.Repeat {
		// Tile watermarks across the page.
		for y := opt.RepeatSpacingY; y < pageH; y += opt.RepeatSpacingY + opt.FontSize {
			for x := opt.RepeatSpacingX / 2; x < pageW; x += opt.RepeatSpacingX + textW {
				gp.Rotate(opt.Angle, x, y)
				gp.SetXY(x-textW/2, y-opt.FontSize/2)
				_ = gp.Text(opt.Text)
				gp.RotateReset()
			}
		}
	} else {
		// Single centered watermark.
		cx := pageW / 2
		cy := pageH / 2
		gp.Rotate(opt.Angle, cx, cy)
		gp.SetXY(cx-textW/2, cy-opt.FontSize/2)
		_ = gp.Text(opt.Text)
		gp.RotateReset()
	}

	gp.RestoreGraphicsState()
	gp.ClearTransparency()

	// Restore original font.
	if origFontFamily != "" {
		_ = gp.SetFontWithStyle(origFontFamily, origFontStyle, origFontSize)
	}

	return nil
}

// AddWatermarkImage adds an image watermark to the current page.
// The image is placed at the center of the page with the specified opacity.
//
// Parameters:
//   - imgPath: path to the image file (JPEG or PNG)
//   - opacity: transparency level (0.0 = invisible, 1.0 = opaque)
//   - imgW, imgH: desired width and height of the watermark image in document units.
//     If both are 0, the image is placed at its natural size.
//   - angle: rotation angle in degrees (0 = no rotation)
func (gp *GoPdf) AddWatermarkImage(imgPath string, opacity float64, imgW, imgH float64, angle float64) error {
	if opacity <= 0 {
		opacity = 0.3
	}

	pageW := gp.config.PageSize.W
	pageH := gp.config.PageSize.H
	if gp.curr.pageSize != nil {
		pageW = gp.curr.pageSize.W
		pageH = gp.curr.pageSize.H
	}

	// Default image size if not specified.
	if imgW <= 0 {
		imgW = pageW / 3
	}
	if imgH <= 0 {
		imgH = pageH / 3
	}

	cx := pageW/2 - imgW/2
	cy := pageH/2 - imgH/2

	gp.SaveGraphicsState()

	if err := gp.SetTransparency(Transparency{
		Alpha:         opacity,
		BlendModeType: "",
	}); err != nil {
		gp.RestoreGraphicsState()
		return err
	}

	if angle != 0 {
		gp.Rotate(angle, pageW/2, pageH/2)
	}

	err := gp.Image(imgPath, cx, cy, &Rect{W: imgW, H: imgH})

	if angle != 0 {
		gp.RotateReset()
	}

	gp.RestoreGraphicsState()
	gp.ClearTransparency()

	return err
}

// AddWatermarkTextAllPages adds a text watermark to all pages in the document.
func (gp *GoPdf) AddWatermarkTextAllPages(opt WatermarkOption) error {
	numPages := gp.GetNumberOfPages()
	for i := 1; i <= numPages; i++ {
		if err := gp.SetPage(i); err != nil {
			return err
		}
		if err := gp.AddWatermarkText(opt); err != nil {
			return err
		}
	}
	return nil
}

// AddWatermarkImageAllPages adds an image watermark to all pages in the document.
func (gp *GoPdf) AddWatermarkImageAllPages(imgPath string, opacity float64, imgW, imgH float64, angle float64) error {
	numPages := gp.GetNumberOfPages()
	for i := 1; i <= numPages; i++ {
		if err := gp.SetPage(i); err != nil {
			return err
		}
		if err := gp.AddWatermarkImage(imgPath, opacity, imgW, imgH, angle); err != nil {
			return err
		}
	}
	return nil
}

// diagonalAngle calculates the diagonal angle of a rectangle (useful for watermark rotation).
func diagonalAngle(w, h float64) float64 {
	return math.Atan2(h, w) * 180 / math.Pi
}
