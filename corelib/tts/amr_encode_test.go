package tts

import "testing"

func TestEncodeWAVToAMR(t *testing.T) {
	wav := generateTestWAV(16000, 1, 0.2, 440)
	amr, err := EncodeWAVToAMR(wav)
	if err != nil {
		t.Fatalf("EncodeWAVToAMR() error = %v", err)
	}
	if string(amr[:6]) != "#!AMR\n" {
		t.Fatalf("AMR header = %q", amr[:6])
	}
	if len(amr) <= 6 {
		t.Fatalf("AMR payload too short: %d", len(amr))
	}
}
