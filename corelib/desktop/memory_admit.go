package desktop

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

// AdmissionMemoryBusy is the structured kind of a desktop admission that
// waited out the memory ceiling. Callers match it with errors.Is. The
// sentence is the outcome the person reads. It is not a tool error the
// model is asked to retry.
const AdmissionMemoryBusy = "admission_memory_busy"

// MemoryBusyOutcome is that sentence.
const MemoryBusyOutcome = "这台电脑内存紧张，等了两分钟还是满的，这次先不做。"

// MemoryResumeOutcome is the sentence when an automatic resume was parked
// for memory and then gave up. It names the unblock: say 继续 once the
// machine has room.
const MemoryResumeOutcome = "内存还是满的，暂停的任务没能接着做。等这台电脑空一点后，再说一声「继续」。"

// ErrMemoryAdmission marks a MemoryAdmission. The loop ends the turn on it.
var ErrMemoryAdmission = errors.New(AdmissionMemoryBusy)

// MemoryAdmission is one failed admission. Sentence is the person-facing
// outcome. Kind is AdmissionMemoryBusy.
type MemoryAdmission struct {
	Sentence string
}

func (e *MemoryAdmission) Error() string {
	if e == nil || strings.TrimSpace(e.Sentence) == "" {
		return MemoryBusyOutcome
	}
	return e.Sentence
}

func (e *MemoryAdmission) Unwrap() error { return ErrMemoryAdmission }

func (e *MemoryAdmission) Kind() string { return AdmissionMemoryBusy }

// MemoryAdmitFraction is the container memory line above which a new desktop
// run or screen lease waits. MACLAW_DESKTOP_MEM_PERCENT is 1–99. Anything
// else uses 85%.
func MemoryAdmitFraction() float64 {
	raw := strings.TrimSpace(os.Getenv("MACLAW_DESKTOP_MEM_PERCENT"))
	if raw == "" {
		return 0.85
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil || n <= 0 || n >= 100 {
		return 0.85
	}
	return n / 100
}

// MemoryPressured reports whether used/limit is at or over the admission line.
// An unknown limit is not pressure.
func MemoryPressured(used, limit int64) bool {
	fraction := MemoryAdmitFraction()
	if limit <= 0 || fraction <= 0 {
		return false
	}
	return float64(used)/float64(limit) >= fraction
}
