//go:build arm64 && !darwin

package tensor

import (
	"encoding/binary"
	"os"
	"sync"

	"golang.org/x/sys/cpu"
)

// Android c-shared builds often start with an empty auxv, so x/sys/cpu leaves
// HasASIMDDP and HasI8MM false and the int8 GEMM stays off. /proc/cpuinfo is
// readable from an app process; /proc/self/auxv is the second source.
func armHasDotProd() bool { return armLinuxFeatures().dot }
func armHasI8MM() bool    { return armLinuxFeatures().i8mm }

type armFeatBits struct {
	dot  bool
	i8mm bool
}

var (
	armFeatOnce       sync.Once
	armFeatBitsCached armFeatBits
)

func armLinuxFeatures() armFeatBits {
	armFeatOnce.Do(func() {
		f := armFeatBits{dot: cpu.ARM64.HasASIMDDP, i8mm: cpu.ARM64.HasI8MM}
		if !f.dot || !f.i8mm {
			if text, err := os.ReadFile("/proc/cpuinfo"); err == nil {
				d, i := parseARMCPUInfoFeatures(string(text))
				f.dot = f.dot || d
				f.i8mm = f.i8mm || i
			}
		}
		if !f.dot || !f.i8mm {
			d, i := readProcAuxvARM()
			f.dot = f.dot || d
			f.i8mm = f.i8mm || i
		}
		armFeatBitsCached = f
	})
	return armFeatBitsCached
}

// AT_HWCAP / AT_HWCAP2 and the Linux arm64 feature bits. ASIMDDP is SDOT
// (Snapdragon 855 and later). I8MM is SMMLA (Snapdragon 8 Gen 1, 8 Elite).
const (
	auxvHWCAP    = 16
	auxvHWCAP2   = 26
	hwcapASIMDDP = 1 << 20
	hwcap2I8MM   = 1 << 13
)

func readProcAuxvARM() (dotprod, i8mm bool) {
	buf, err := os.ReadFile("/proc/self/auxv")
	if err != nil {
		return false, false
	}
	for len(buf) >= 16 {
		tag := binary.LittleEndian.Uint64(buf[0:8])
		val := binary.LittleEndian.Uint64(buf[8:16])
		buf = buf[16:]
		if tag == 0 {
			break
		}
		switch tag {
		case auxvHWCAP:
			if val&hwcapASIMDDP != 0 {
				dotprod = true
			}
		case auxvHWCAP2:
			if val&hwcap2I8MM != 0 {
				i8mm = true
			}
		}
	}
	return
}
