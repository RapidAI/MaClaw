package desktopbot

import "testing"

func TestXConnectionFailedReadsTheResultNotTheCommand(t *testing.T) {
	yes := []string{
		"firefox: cannot open display",
		"Error: Can't open display: :21",
		"qt.qpa.xcb: could not connect to display",
		"no display name and no $DISPLAY environment variable",
		"Missing X server or $DISPLAY",
	}
	for _, line := range yes {
		if !XConnectionFailed(line) {
			t.Errorf("expected an X connection failure: %q", line)
		}
	}
	for _, line := range []string{"ValueError: bad batch", "firefox", "train.py", "cannot open file"} {
		if XConnectionFailed(line) {
			t.Errorf("not an X connection failure: %q", line)
		}
	}
}
