// Package device discovers serial candidates without opening them.
package device

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"

	"go.bug.st/serial/enumerator"
)

type Candidate struct {
	Port        string `json:"port"`
	Name        string `json:"name"`
	VendorID    string `json:"vendorId,omitempty"`
	ProductID   string `json:"productId,omitempty"`
	Serial      string `json:"serial,omitempty"`
	IsUSB       bool   `json:"isUsb"`
	LikelyEsp   bool   `json:"likelyEsp"`
	Description string `json:"description,omitempty"`
}

type CandidateLister func() ([]Candidate, error)

type ReenumerationPolicy struct {
	Timeout      time.Duration
	PollInterval time.Duration
}

func DefaultReenumerationPolicy() ReenumerationPolicy {
	return ReenumerationPolicy{Timeout: 12 * time.Second, PollInterval: 250 * time.Millisecond}
}

func ListCandidates() ([]Candidate, error) {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, fmt.Errorf("enumerate serial ports: %w", err)
	}
	result := make([]Candidate, 0, len(ports))
	for _, p := range ports {
		candidate := Candidate{Port: p.Name, Name: p.Name, VendorID: strings.ToUpper(p.VID), ProductID: strings.ToUpper(p.PID), Serial: p.SerialNumber, IsUSB: p.IsUSB, Description: p.Product}
		candidate.LikelyEsp = isLikelyESP(candidate.VendorID, candidate.ProductID, p.Product)
		result = append(result, candidate)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Port < result[j].Port })
	return result, nil
}

func WaitForReenumeratedPort(ctx context.Context, before Candidate, list CandidateLister, policy ReenumerationPolicy) (Candidate, error) {
	if list == nil {
		return Candidate{}, errors.New("serial candidate lister is unavailable")
	}
	if strings.TrimSpace(before.Port) == "" {
		return Candidate{}, errors.New("original serial port is required")
	}
	if policy.Timeout <= 0 {
		policy.Timeout = DefaultReenumerationPolicy().Timeout
	}
	if policy.PollInterval <= 0 {
		policy.PollInterval = DefaultReenumerationPolicy().PollInterval
	}
	deadline := time.NewTimer(policy.Timeout)
	defer deadline.Stop()
	for {
		candidates, err := list()
		if err == nil {
			if matched, ok := reenumeratedMatch(before, candidates); ok {
				return matched, nil
			}
		}
		timer := time.NewTimer(policy.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Candidate{}, fmt.Errorf("wait for device serial re-enumeration: %w", ctx.Err())
		case <-deadline.C:
			timer.Stop()
			return Candidate{}, errors.New("timed out waiting for the same device serial endpoint after reset")
		case <-timer.C:
		}
	}
}

func reenumeratedMatch(before Candidate, candidates []Candidate) (Candidate, bool) {
	serial := strings.TrimSpace(before.Serial)
	if serial != "" {
		var matches []Candidate
		for _, candidate := range candidates {
			if candidate.IsUSB && strings.EqualFold(strings.TrimSpace(candidate.Serial), serial) {
				matches = append(matches, candidate)
			}
		}
		if len(matches) == 1 {
			return matches[0], true
		}
		return Candidate{}, false
	}
	for _, candidate := range candidates {
		if candidate.Port == before.Port {
			return candidate, true
		}
	}
	return Candidate{}, false
}

func isLikelyESP(vid, pid string, parts ...string) bool {
	vid = strings.ToUpper(vid)
	pid = strings.ToUpper(pid)
	if vid == "303A" {
		return true
	}
	for _, part := range parts {
		if strings.Contains(strings.ToLower(part), "espressif") || strings.Contains(strings.ToLower(part), "esp32") {
			return true
		}
	}
	return (vid == "10C4" && pid == "EA60") || (vid == "1A86" && (pid == "7523" || pid == "55D4")) || (vid == "0403" && pid == "6001")
}

func Platform() string { return runtime.GOOS }

func validSerialPort(port string) bool {
	if strings.HasPrefix(strings.ToUpper(port), "COM") {
		for _, r := range port[3:] {
			if r < '0' || r > '9' {
				return false
			}
		}
		return len(port) > 3
	}
	return strings.HasPrefix(port, "/dev/tty") || strings.HasPrefix(port, "/dev/cu.")
}
