package desktop

import "strconv"

// Host RAM bands use GiB thresholds a little under the number printed on the
// machine. MemTotal is what Linux can use, so an 8GB machine reports about
// 7.6GiB and a 16GB machine about 15.5GiB.
const (
	hostFitMinRAM = 7 << 30
	hostFit16     = 14 << 30
	hostFit32     = 28 << 30
)

// ErrHostTooSmall means the machine cannot run a full desktop container
// and still leave room for the operating system and MaClawSrv.
var ErrHostTooSmall = errHostTooSmall{}

type errHostTooSmall struct{}

func (errHostTooSmall) Error() string {
	return "这台机器内存不到 8G，装不下完整桌面"
}

// FitHostResources picks one desktop container's memory, CPU, and shared
// memory from the machine it will run on. Memory stays between 3G and 6G.
// CPU is half the machine, and stays between 1.5 and 4.
func FitHostResources(ramBytes uint64, logicalCPUs int) (Resources, error) {
	if ramBytes < hostFitMinRAM {
		return Resources{}, ErrHostTooSmall
	}
	memory, shm := "3g", "1g"
	switch {
	case ramBytes >= hostFit32:
		memory, shm = "6g", "2g"
	case ramBytes >= hostFit16:
		memory, shm = "4g", "1g"
	}
	cpus := DefaultCPUs
	if logicalCPUs > 0 {
		value := float64(logicalCPUs) / 2
		if value < 1.5 {
			value = 1.5
		}
		if value > 4 {
			value = 4
		}
		cpus = strconv.FormatFloat(value, 'f', -1, 64)
	}
	return NormalizeResources("", memory, cpus, shm)
}
