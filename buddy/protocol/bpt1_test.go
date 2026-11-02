package protocol

import "testing"

func TestBPT1RoundTrip(t *testing.T) {
	src := BPT1Chunk{
		Frame: 1, Frames: 8, Width: 128, Height: 128, FrameMS: 450,
		Offset: 1024, Total: 49152, CRC32: 0x01020304,
		Data: []byte{9, 0x0a, 7, 6},
	}
	raw, err := EncodeBPT1(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 28+len(src.Data) {
		t.Fatalf("encoded %d bytes, want %d with no extra newline", len(raw), 28+len(src.Data))
	}
	got, n, err := DecodeBPT1(raw)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(raw) || got.Frame != 1 || got.Offset != 1024 || string(got.Data) != string(src.Data) {
		t.Fatalf("decoded %+v n=%d", got, n)
	}
	if _, err := EncodeBPT1(BPT1Chunk{Frame: 0, Frames: 1, Data: nil}); err == nil {
		t.Fatal("empty payload accepted")
	}
}
