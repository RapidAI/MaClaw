//go:build windows

package systray

import (
	"strings"
	"testing"
)

func TestCopyNULTerminatedUTF16FitsAndClearsTheTail(t *testing.T) {
	var dst [8]uint16
	for i := range dst {
		dst[i] = 0xFFFF
	}
	if err := copyNULTerminatedUTF16(dst[:], "任务"); err != nil {
		t.Fatal(err)
	}
	if dst[0] == 0 || dst[2] != 0 || dst[7] != 0 {
		t.Fatalf("dst = %v", dst)
	}
}

func TestCopyNULTerminatedUTF16CutsWithoutDroppingTheNUL(t *testing.T) {
	var dst [4]uint16
	if err := copyNULTerminatedUTF16(dst[:], strings.Repeat("项", 10)); err != nil {
		t.Fatal(err)
	}
	if dst[3] != 0 {
		t.Fatalf("missing NUL: %v", dst)
	}
	if dst[0] == 0 {
		t.Fatal("expected truncated text")
	}
}
