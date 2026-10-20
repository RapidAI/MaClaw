package dispatcher

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

func (d *Dispatcher) processDragAndDropMessage(message string) (string, error) {
	switch message[1] {
	case 'D':
		msg := strings.SplitN(message[3:], ":", 3)
		if len(msg) != 3 {
			return "", errors.New("Invalid drag and drop Message: " + message)
		}

		// DragEvent.x and DragEvent.y are CSS pixels. At a display scale other
		// than 100% they are fractional, and Atoi rejects the whole drop
		// before the window's drop-target check runs.
		x, err := dropCoordinate(msg[0])
		if err != nil {
			return "", errors.New("Invalid x coordinate in drag and drop Message: " + message)
		}

		y, err := dropCoordinate(msg[1])
		if err != nil {
			return "", errors.New("Invalid y coordinate in drag and drop Message: " + message)
		}

		paths := strings.Split(msg[2], "\n")
		if len(paths) < 1 {
			return "", errors.New("Invalid drag and drop Message: " + message)
		}

		d.events.Emit("wails:file-drop", x, y, paths)
	default:
		return "", errors.New("Invalid drag and drop Message: " + message)
	}

	return "", nil
}

// dropCoordinate accepts the integer coordinates Linux and macOS send and the
// fractional CSS pixels Windows WebView2 sends.
func dropCoordinate(raw string) (int, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		if err == nil {
			err = errors.New("coordinate is not finite")
		}
		return 0, err
	}
	rounded := math.Round(value)
	if rounded > math.MaxInt32 || rounded < math.MinInt32 {
		return 0, errors.New("coordinate out of range")
	}
	return int(rounded), nil
}
