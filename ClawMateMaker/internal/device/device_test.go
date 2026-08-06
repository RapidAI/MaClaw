package device

import (
	"context"
	"strings"
	"testing"
	"time"
)

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
