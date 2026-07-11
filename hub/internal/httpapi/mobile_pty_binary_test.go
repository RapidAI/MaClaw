package httpapi

import (
	"bytes"
	"testing"
)

func TestMobilePtyBinaryRoundTrip(t *testing.T) {
	payload := []byte{0x03, 0x04, 'a', 'b'}
	enc, err := mobilePtyBinaryEncodeInput("mobssh_1", payload, true)
	if err != nil {
		t.Fatal(err)
	}
	if !mobilePtyBinaryIsMagic(enc) {
		t.Fatal("magic")
	}
	frame, err := mobilePtyBinaryDecode(enc)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Type != mobilePtyBinaryTypeIn || !frame.Raw() {
		t.Fatalf("frame=%+v", frame)
	}
	if frame.SessionID != "mobssh_1" {
		t.Fatalf("sid=%q", frame.SessionID)
	}
	if !bytes.Equal(frame.Payload, payload) {
		t.Fatalf("payload=%v", frame.Payload)
	}
}

func TestMobilePtyBinaryAckAndOut(t *testing.T) {
	ack, err := mobilePtyBinaryEncodeAck("s1", false, "boom")
	if err != nil {
		t.Fatal(err)
	}
	f, err := mobilePtyBinaryDecode(ack)
	if err != nil || f.Type != mobilePtyBinaryTypeAck || !f.Error() {
		t.Fatalf("ack=%+v err=%v", f, err)
	}
	if string(f.Payload) != "boom" {
		t.Fatalf("payload=%q", f.Payload)
	}
	out, err := mobilePtyBinaryEncodeOutput("s1", []byte("hello\n"))
	if err != nil {
		t.Fatal(err)
	}
	f2, err := mobilePtyBinaryDecode(out)
	if err != nil || f2.Type != mobilePtyBinaryTypeOut {
		t.Fatal(err)
	}
	if string(f2.Payload) != "hello\n" {
		t.Fatalf("out=%q", f2.Payload)
	}
}

func TestMobilePtyBinaryRejectsBadMagic(t *testing.T) {
	if _, err := mobilePtyBinaryDecode([]byte("XXXX....")); err == nil {
		t.Fatal("want error")
	}
}

func TestMobileRealtimeCapsIncludePtyBinary(t *testing.T) {
	if !mobileRealtimeCapsIncludePtyBinary([]any{"json", "pty_binary"}) {
		t.Fatal("slice any")
	}
	if !mobileRealtimeCapsIncludePtyBinary([]string{"pty_binary"}) {
		t.Fatal("slice string")
	}
	if mobileRealtimeCapsIncludePtyBinary([]any{"json"}) {
		t.Fatal("no binary")
	}
}

func TestMobileRealtimeMaybeBinaryPtyOut(t *testing.T) {
	bin, ok := mobileRealtimeMaybeBinaryPtyOut(map[string]any{
		"type":         "ssh_session",
		"session_id":   "mobssh_x",
		"output_chunk": "line\n",
	})
	if !ok || !mobilePtyBinaryIsMagic(bin) {
		t.Fatalf("ok=%v bin=%v", ok, bin)
	}
	f, err := mobilePtyBinaryDecode(bin)
	if err != nil || f.Type != mobilePtyBinaryTypeOut || string(f.Payload) != "line\n" {
		t.Fatalf("f=%+v err=%v", f, err)
	}
	if _, ok := mobileRealtimeMaybeBinaryPtyOut(map[string]any{"type": "ssh_task"}); ok {
		t.Fatal("non-session should skip")
	}
}
