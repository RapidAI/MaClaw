//go:build !windows && !linux

package tool

import "time"

func sampleCommandTreeCPUByPID(int) (map[int]time.Duration, bool) {
	return nil, false
}
