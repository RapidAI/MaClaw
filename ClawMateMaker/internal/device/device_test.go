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
	if !isLikelyESP("303a", "1001") || !isLikelyESP("10c4", "ea60") {
		t.Fatal("known ESP and CP210x candidates should be recognized")
	}
	if isLikelyESP("ffff", "0001", "ordinary device") {
		t.Fatal("unknown device must not be inferred")
	}
}

func TestReenumeratedPortAcceptsOnlyStableBinding(t *testing.T) {
	before := Candidate{Port: "COM3", Serial: "device-123", IsUSB: true}
	got, err := WaitForReenumeratedPort(context.Background(), before, func() ([]Candidate, error) {
		return []Candidate{{Port: "COM7", Serial: "DEVICE-123", IsUSB: true}}, nil
	}, ReenumerationPolicy{Timeout: time.Second, PollInterval: time.Millisecond})
	if err != nil || got.Port != "COM7" {
		t.Fatalf("candidate=%+v err=%v", got, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	if _, err := WaitForReenumeratedPort(ctx, Candidate{Port: "COM3", IsUSB: true}, func() ([]Candidate, error) {
		return []Candidate{{Port: "COM8", IsUSB: true}}, nil
	}, ReenumerationPolicy{Timeout: time.Second, PollInterval: time.Millisecond}); err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("unexpected unfamiliar-port result: %v", err)
	}
}

func TestReadBoundedLineSafety(t *testing.T) {
	p := &fixedReadPort{chunks: [][]byte{[]byte("a"), []byte("\n"), []byte("b")}, err: io.EOF}
	line, err := ReadBoundedLine(p, 16)
	if err != nil || line != "a\n" || p.index != 2 {
		t.Fatalf("line=%q index=%d err=%v", line, p.index, err)
	}
	if _, err := ReadBoundedLine(&fixedReadPort{chunks: [][]byte{[]byte("a"), []byte("b"), []byte("c")}}, 2); err == nil {
		t.Fatal("oversized serial frame accepted")
	}
	if IsSerialReadTimeout(io.EOF) || !IsSerialReadTimeout(errors.New("i/o timeout")) {
		t.Fatal("incorrect timeout classification")
	}
}
