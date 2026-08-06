package device

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"go.bug.st/serial"
)

type fixedReadPort struct {
	chunks [][]byte
	index  int
	err    error
}

func (p *fixedReadPort) SetMode(*serial.Mode) error { return nil }
func (p *fixedReadPort) Read(dst []byte) (int, error) {
	if p.index >= len(p.chunks) {
		return 0, p.err
	}
	n := copy(dst, p.chunks[p.index])
	p.index++
	return n, nil
}
func (p *fixedReadPort) Write([]byte) (int, error)                            { return 0, nil }
func (p *fixedReadPort) Drain() error                                         { return nil }
func (p *fixedReadPort) ResetInputBuffer() error                              { return nil }
func (p *fixedReadPort) ResetOutputBuffer() error                             { return nil }
func (p *fixedReadPort) SetDTR(bool) error                                    { return nil }
func (p *fixedReadPort) SetRTS(bool) error                                    { return nil }
func (p *fixedReadPort) GetModemStatusBits() (*serial.ModemStatusBits, error) { return nil, nil }
func (p *fixedReadPort) SetReadTimeout(time.Duration) error                   { return nil }
func (p *fixedReadPort) Close() error                                         { return nil }
func (p *fixedReadPort) Break(time.Duration) error                            { return nil }

func TestLikelyESPCandidates(t *testing.T) {
	if !isLikelyESP("303a", "1001") {
		t.Fatal("Espressif USB should be candidate")
	}
	if !isLikelyESP("10c4", "ea60") {
		t.Fatal("CP210x should be candidate")
	}
	if isLikelyESP("ffff", "0001", "ordinary device") {
		t.Fatal("unknown device must not be inferred")
	}
}

func TestReenumeratedPortAcceptsSameUSBSerialWithNewName(t *testing.T) {
	before := Candidate{Port: "COM3", Serial: "device-123", IsUSB: true}
	got, err := WaitForReenumeratedPort(context.Background(), before, func() ([]Candidate, error) {
		return []Candidate{{Port: "COM7", Serial: "DEVICE-123", IsUSB: true}}, nil
	}, ReenumerationPolicy{Timeout: time.Second, PollInterval: time.Millisecond})
	if err != nil || got.Port != "COM7" {
		t.Fatalf("candidate=%+v err=%v", got, err)
	}
}

func TestReenumeratedPortWithoutSerialRequiresOriginalPort(t *testing.T) {
	before := Candidate{Port: "/dev/cu.usbmodem10", IsUSB: true}
	got, err := WaitForReenumeratedPort(context.Background(), before, func() ([]Candidate, error) {
		return []Candidate{{Port: "/dev/cu.usbmodem10", IsUSB: true}, {Port: "/dev/cu.usbmodem11", IsUSB: true}}, nil
	}, ReenumerationPolicy{Timeout: time.Second, PollInterval: time.Millisecond})
	if err != nil || got.Port != before.Port {
		t.Fatalf("candidate=%+v err=%v", got, err)
	}
}

func TestReenumeratedPortRejectsUnfamiliarEndpoint(t *testing.T) {
	before := Candidate{Port: "COM3", IsUSB: true}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	_, err := WaitForReenumeratedPort(ctx, before, func() ([]Candidate, error) {
		return []Candidate{{Port: "COM8", IsUSB: true}}, nil
	}, ReenumerationPolicy{Timeout: time.Second, PollInterval: time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("err=%v", err)
	}
}

func TestReadBoundedLineDoesNotReadPastNewline(t *testing.T) {
	p := &fixedReadPort{chunks: [][]byte{[]byte("a"), []byte("\n"), []byte("b")}, err: io.EOF}
	line, err := ReadBoundedLine(p, 16)
	if err != nil || line != "a\n" || p.index != 2 {
		t.Fatalf("line=%q index=%d err=%v", line, p.index, err)
	}
}

func TestReadBoundedLineRejectsOversizedFrame(t *testing.T) {
	p := &fixedReadPort{chunks: [][]byte{[]byte("a"), []byte("b"), []byte("c")}}
	if _, err := ReadBoundedLine(p, 2); err == nil {
		t.Fatal("oversized serial frame accepted")
	}
}

func TestIsSerialReadTimeoutRejectsEOFAndRecognizesTimeout(t *testing.T) {
	if IsSerialReadTimeout(io.EOF) || !IsSerialReadTimeout(errors.New("i/o timeout")) {
		t.Fatal("incorrect timeout classification")
	}
}

func TestReadBoundedLineConvertsIdleReadToTimeout(t *testing.T) {
	p := &fixedReadPort{}
	if _, err := ReadBoundedLine(p, 16); !IsSerialReadTimeout(err) {
		t.Fatalf("idle serial read error=%v", err)
	}
}
