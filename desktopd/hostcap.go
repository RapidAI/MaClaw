package desktopd

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

// HostCapacity is the memory and CPU of the machine desktopd is running on.
// RAM is 0 when it cannot be read; the hub then keeps the desktop's current size.
func HostCapacity() (ramBytes uint64, logicalCPUs int) {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, runtime.NumCPU()
	}
	return ParseMemTotal(string(raw)), runtime.NumCPU()
}

// ParseMemTotal reads the MemTotal line. The value is KiB in the file and
// bytes here.
func ParseMemTotal(text string) uint64 {
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024
	}
	return 0
}
