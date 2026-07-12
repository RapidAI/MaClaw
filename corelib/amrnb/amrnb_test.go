package amrnb

import (
	"strings"
	"testing"
)

func TestEncodeS16ProducesAMRNB(t *testing.T) {
	pcm := make([]int16, SampleRate/2)
	data, err := EncodeS16(pcm)
	if err != nil {
		if strings.Contains(err.Error(), "unavailable without cgo") {
			t.Skip(err.Error())
		}
		t.Fatalf("EncodeS16() error = %v", err)
	}
	if string(data[:6]) != "#!AMR\n" {
		t.Fatalf("AMR header = %q", data[:6])
	}
	if len(data) <= 6 {
		t.Fatalf("AMR payload too short: %d", len(data))
	}
}
