package gopdf

import "testing"

// Pure-geometry tests for listMarkerLayout (no font / PDF required).

func TestListMarkerLayout_MultiDigitWiderThanSingle(t *testing.T) {
	const fontSize, unit = 12.0, 1.0
	_, _, singleIndent := listMarkerLayout(fontSize, unit, 500, 8) // ~"1."
	_, _, multiIndent := listMarkerLayout(fontSize, unit, 500, 16) // ~"10."
	if multiIndent <= singleIndent {
		t.Fatalf("multi-digit indent (%.2f) should exceed single-digit indent (%.2f)", multiIndent, singleIndent)
	}
	leftPad, markerCol, contentIndent := listMarkerLayout(fontSize, unit, 500, 16)
	if contentIndent < leftPad+markerCol {
		t.Fatalf("contentIndent %.2f < leftPad+markerCol %.2f", contentIndent, leftPad+markerCol)
	}
	if markerCol < 16 {
		t.Fatalf("markerCol %.2f must be >= maxMarkerW 16", markerCol)
	}
}

func TestListMarkerLayout_NeverShrinksBelowMarker(t *testing.T) {
	const maxW = 40.0
	leftPad, markerCol, contentIndent := listMarkerLayout(12, 1, 90, maxW)
	if markerCol < maxW {
		t.Fatalf("markerCol %.2f < maxMarkerW %.2f on tight box", markerCol, maxW)
	}
	if leftPad+markerCol > contentIndent+1e-9 {
		t.Fatalf("marker column spills into content: leftPad+markerCol=%.2f contentIndent=%.2f",
			leftPad+markerCol, contentIndent)
	}
}

func TestListMarkerLayout_ContentWidthNonNegative(t *testing.T) {
	// Pathological: marker wider than the box must not yield contentIndent > boxW.
	const boxW, maxW = 30.0, 40.0
	leftPad, markerCol, contentIndent := listMarkerLayout(12, 1, boxW, maxW)
	if contentIndent > boxW {
		t.Fatalf("contentIndent %.2f > boxW %.2f", contentIndent, boxW)
	}
	if markerCol > boxW {
		t.Fatalf("markerCol %.2f > boxW %.2f", markerCol, boxW)
	}
	if leftPad+markerCol > contentIndent+1e-9 {
		t.Fatalf("marker spills into content area")
	}
}

func TestListMarkerLayout_ResidualGoesToContent(t *testing.T) {
	// Marker fits but default indent does not: residual width must become content,
	// not dead space after the marker column.
	const boxW, maxW = 50.0, 45.0
	_, markerCol, contentIndent := listMarkerLayout(12, 1, boxW, maxW)
	if markerCol != maxW {
		t.Fatalf("markerCol %.2f want %.2f", markerCol, maxW)
	}
	contentW := boxW - contentIndent
	if contentW <= 0 {
		t.Fatalf("expected positive content width, got %.2f (contentIndent=%.2f)", contentW, contentIndent)
	}
	if contentIndent < markerCol {
		t.Fatalf("contentIndent %.2f < markerCol %.2f", contentIndent, markerCol)
	}
}
