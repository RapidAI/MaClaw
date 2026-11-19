package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/guiapp/petpack"
)

func TestPetDutyWAVStartsWithARiffHeader(t *testing.T) {
	buf := writeDutyWAV([]petpack.RigTone{{Hz: 440, Ms: 40}}, 1)
	if len(buf) < 44 || string(buf[0:4]) != "RIFF" || string(buf[8:12]) != "WAVE" {
		t.Fatalf("wav header len=%d", len(buf))
	}
}
