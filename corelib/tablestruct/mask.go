package tablestruct

import (
	"html"
	"sort"
	"strings"
)

const (
	classHorizontal = 1
	classVertical   = 2
)

// Line is one OCR line in the table crop, origin at the crop's top-left.
type Line struct {
	Text string
	X, Y int
	W, H int
}

// Separators finds horizontal and vertical ruling lines on a class mask.
// Class 1 is a horizontal line and class 2 is a vertical line, matching
// the wired UNET used by RapidTable. Positions are pixel indexes in the
// mask. A line must span at least minHSpan or minVSpan pixels.
func Separators(mask []int, width, height, minHSpan, minVSpan int) (rows, cols []int) {
	if width < 2 || height < 2 || len(mask) != width*height {
		return nil, nil
	}
	hMask := make([]byte, len(mask))
	vMask := make([]byte, len(mask))
	for i, v := range mask {
		if v == classHorizontal {
			hMask[i] = 1
		} else if v == classVertical {
			vMask[i] = 1
		}
	}
	rows = lineCenters(components(hMask, width, height), true, minHSpan)
	cols = lineCenters(components(vMask, width, height), false, minVSpan)
	return rows, cols
}

// HTML builds a grid from separator positions in crop pixels.
// rows and cols are the ruling lines. Each gap between two neighboring
// lines is one cell. Text is the OCR lines whose centers fall in that cell.
func HTML(rows, cols []int, lines []Line) string {
	if len(rows) < 2 || len(cols) < 2 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<table>")
	for r := 0; r < len(rows)-1; r++ {
		b.WriteString("<tr>")
		for c := 0; c < len(cols)-1; c++ {
			b.WriteString("<td>")
			b.WriteString(html.EscapeString(cellText(lines, cols[c], rows[r], cols[c+1], rows[r+1])))
			b.WriteString("</td>")
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</table>")
	return b.String()
}

func cellText(lines []Line, x0, y0, x1, y1 int) string {
	var parts []string
	for _, line := range lines {
		if strings.TrimSpace(line.Text) == "" || line.W < 1 || line.H < 1 {
			continue
		}
		cx := line.X + line.W/2
		cy := line.Y + line.H/2
		if cx >= x0 && cx < x1 && cy >= y0 && cy < y1 {
			parts = append(parts, strings.TrimSpace(line.Text))
		}
	}
	return strings.Join(parts, " ")
}

type component struct {
	minX, minY, maxX, maxY int
	n                      int
}

func components(mask []byte, width, height int) []component {
	seen := make([]byte, len(mask))
	var out []component
	queue := make([]int, 0, 64)
	dirs := [8][2]int{{-1, -1}, {0, -1}, {1, -1}, {-1, 0}, {1, 0}, {-1, 1}, {0, 1}, {1, 1}}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			start := y*width + x
			if mask[start] == 0 || seen[start] != 0 {
				continue
			}
			comp := component{minX: x, minY: y, maxX: x, maxY: y}
			queue = append(queue[:0], start)
			seen[start] = 1
			for len(queue) > 0 {
				cur := queue[len(queue)-1]
				queue = queue[:len(queue)-1]
				cx, cy := cur%width, cur/width
				comp.n++
				if cx < comp.minX {
					comp.minX = cx
				}
				if cy < comp.minY {
					comp.minY = cy
				}
				if cx > comp.maxX {
					comp.maxX = cx
				}
				if cy > comp.maxY {
					comp.maxY = cy
				}
				for _, d := range dirs {
					nx, ny := cx+d[0], cy+d[1]
					if nx < 0 || ny < 0 || nx >= width || ny >= height {
						continue
					}
					ni := ny*width + nx
					if mask[ni] == 0 || seen[ni] != 0 {
						continue
					}
					seen[ni] = 1
					queue = append(queue, ni)
				}
			}
			if comp.n >= 8 {
				out = append(out, comp)
			}
		}
	}
	return out
}

func lineCenters(comps []component, horizontal bool, minSpan int) []int {
	var centers []int
	for _, comp := range comps {
		span := comp.maxX - comp.minX + 1
		cross := comp.maxY - comp.minY + 1
		if !horizontal {
			span, cross = cross, span
		}
		if span < minSpan || span < cross {
			continue
		}
		if horizontal {
			centers = append(centers, (comp.minY+comp.maxY)/2)
		} else {
			centers = append(centers, (comp.minX+comp.maxX)/2)
		}
	}
	sort.Ints(centers)
	if len(centers) == 0 {
		return nil
	}
	merged := []int{centers[0]}
	for _, c := range centers[1:] {
		if c-merged[len(merged)-1] <= 3 {
			merged[len(merged)-1] = (merged[len(merged)-1] + c) / 2
			continue
		}
		merged = append(merged, c)
	}
	return merged
}
