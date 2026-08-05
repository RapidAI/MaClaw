package jobs

import (
	"context"
	"testing"
)

func TestProbeMissingToolIsReported(t *testing.T) {
	t.Setenv("CLAWMATE_ESPTOOL", "C:\\no-such-esptool.exe")
	t.Setenv("PATH", t.TempDir())
	j, err := NewProbeJob(t.TempDir(), "COM4", nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := j.Run(context.Background())
	if err == nil || r.ErrorCode != "TOOL_UNAVAILABLE" {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}
