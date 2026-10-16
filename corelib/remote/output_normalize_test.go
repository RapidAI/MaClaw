package remote

import "testing"

func TestCompactPtyOutputKeepsColumns(t *testing.T) {
	in := "\x1b[?2004hFilesystem  1K-blocks   \n/dev/sda1  1000      \n"
	got := CompactPtyOutput(in)
	if got != "Filesystem  1K-blocks\n/dev/sda1  1000" {
		t.Fatalf("compact = %q", got)
	}
}

func TestStripLeadingCommandEchoDropsWrappedEcho(t *testing.T) {
	command := "uptime && free -h"
	output := "root@api2:~# uptime && free\n -h\n 18:00 up 1 day\n"
	got := StripLeadingCommandEcho(output, command)
	if got != "18:00 up 1 day" {
		t.Fatalf("stripped = %q", got)
	}
}
