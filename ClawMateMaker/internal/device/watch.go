package device

import (
	"context"
	"sort"
	"strings"
	"time"
)

// ChangeEvent describes a completed candidate-list transition. It contains
// only public USB/serial metadata and never opens a serial endpoint.
type ChangeEvent struct {
	Previous   []Candidate `json:"previous"`
	Candidates []Candidate `json:"candidates"`
	DetectedAt time.Time   `json:"detectedAt"`
}

// WatchPolicy controls the conservative polling fallback used across desktop
// platforms. The serial enumeration API is supplied by the host OS, so this
// detects USB CDC and USB-UART arrival/removal without requiring drivers,
// elevation, or access to the serial data stream.
type WatchPolicy struct {
	PollInterval time.Duration
}

func DefaultWatchPolicy() WatchPolicy { return WatchPolicy{PollInterval: time.Second} }

// WatchCandidates emits only when the normalized candidate snapshot changes.
// A successful first enumeration establishes the baseline and deliberately
// emits nothing: existing devices are handled by the initial UI discovery.
// Enumeration failures retain the previous baseline and are retried later.
func WatchCandidates(ctx context.Context, list CandidateLister, policy WatchPolicy, onChange func(ChangeEvent)) {
	if list == nil || onChange == nil {
		return
	}
	if policy.PollInterval <= 0 {
		policy.PollInterval = DefaultWatchPolicy().PollInterval
	}

	var previous []Candidate
	haveBaseline := false
	poll := func() {
		candidates, err := list()
		if err != nil {
			return
		}
		candidates = normalizeCandidates(candidates)
		if !haveBaseline {
			previous, haveBaseline = candidates, true
			return
		}
		if sameCandidates(previous, candidates) {
			return
		}
		event := ChangeEvent{Previous: append([]Candidate(nil), previous...), Candidates: append([]Candidate(nil), candidates...), DetectedAt: time.Now().UTC()}
		previous = candidates
		onChange(event)
	}

	poll()
	ticker := time.NewTicker(policy.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			poll()
		}
	}
}

func normalizeCandidates(candidates []Candidate) []Candidate {
	result := append([]Candidate(nil), candidates...)
	for i := range result {
		result[i].Port = strings.TrimSpace(result[i].Port)
		result[i].Name = strings.TrimSpace(result[i].Name)
		result[i].VendorID = strings.ToUpper(strings.TrimSpace(result[i].VendorID))
		result[i].ProductID = strings.ToUpper(strings.TrimSpace(result[i].ProductID))
		result[i].Serial = strings.TrimSpace(result[i].Serial)
		result[i].Description = strings.TrimSpace(result[i].Description)
	}
	sort.Slice(result, func(i, j int) bool {
		return candidateKey(result[i]) < candidateKey(result[j])
	})
	return result
}

func sameCandidates(left, right []Candidate) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func candidateKey(candidate Candidate) string {
	return strings.Join([]string{candidate.Port, candidate.Name, candidate.VendorID, candidate.ProductID, candidate.Serial, candidate.Description}, "\x00")
}
