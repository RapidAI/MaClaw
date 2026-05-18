//go:build windows

package main

import "testing"

func TestIdleMillisecondsFromWindowsTicks(t *testing.T) {
	const wrap = uint64(1) << 32
	tests := []struct {
		name        string
		now         uint64
		lastInput32 uint32
		want        uint32
	}{
		{name: "before wrap", now: 120_000, lastInput32: 118_500, want: 1_500},
		{name: "after wrap", now: wrap + 120_000, lastInput32: uint32(118_500), want: 1_500},
		{name: "across wrap boundary", now: 750, lastInput32: 0xfffffff0, want: 766},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := idleMillisecondsFromWindowsTicks(tt.now, tt.lastInput32); got != tt.want {
				t.Fatalf("idle ms = %d, want %d", got, tt.want)
			}
		})
	}
}
