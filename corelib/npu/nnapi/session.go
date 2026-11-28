package nnapi

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Result codes from NeuralNetworksTypes.h. Timeouts are hints the driver may honor.
const (
	nnMissedDeadlineTransient    int32 = 10
	nnResourceExhaustedTransient int32 = 12
)

// Session is a compiled accelerator graph. On non-Android builds Compile
// never returns one.
type Session struct {
	mu      sync.Mutex
	device  string
	run     func([]ExecIn) ([]ExecOut, error)
	release func()
	inputs  int
}

// Device is the accelerator name the graph was compiled for.
func (s *Session) Device() string {
	if s == nil {
		return ""
	}
	return s.device
}

// Run executes the compiled graph. ins follows Builder input order.
func (s *Session) Run(ins []ExecIn) ([]ExecOut, error) {
	if s == nil {
		return nil, fmt.Errorf("nnapi: no session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run == nil {
		return nil, fmt.Errorf("nnapi: no session")
	}
	if len(ins) != s.inputs {
		return nil, fmt.Errorf("nnapi: %d inputs, session wants %d", len(ins), s.inputs)
	}
	return s.run(ins)
}

// Release frees the driver graph. It is safe to call twice.
func (s *Session) Release() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.release != nil {
		s.release()
		s.release = nil
		s.run = nil
	}
}

// Disabled reports MACLAW_VOICE_NPU=0 or a model-specific off switch.
// The default is to try the accelerator and keep the CPU kernels when it
// cannot compile the graph.
func Disabled(specific string) bool {
	if envOff("MACLAW_VOICE_NPU") {
		return true
	}
	return specific != "" && envOff(specific)
}

func envOff(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "0", "false", "off", "no":
		return true
	default:
		return false
	}
}

// execError is a failed Execution_compute. The code is the NNAPI result.
type execError struct {
	code int32
}

func (e *execError) Error() string {
	if e == nil {
		return "nnapi: compute"
	}
	return fmt.Sprintf("nnapi: compute %d", e.code)
}

// Transient reports a deadline or resource miss. The compiled graph is still
// usable; the next call can try it. Any other failure is permanent for that session.
func Transient(err error) bool {
	var e *execError
	if !errors.As(err, &e) || e == nil {
		return false
	}
	return e.code == nnMissedDeadlineTransient || e.code == nnResourceExhaustedTransient
}
