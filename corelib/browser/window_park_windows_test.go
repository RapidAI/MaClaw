//go:build windows

package browser

import (
	"os"
	"strings"
	"testing"
)

func TestParkLoopWakeDoesNotBlock(t *testing.T) {
	wakeParkLoop()
	wakeParkLoop()
	select {
	case <-parkLoopWake:
	default:
		t.Fatal("a wake must be waiting")
	}
	select {
	case <-parkLoopWake:
		t.Fatal("a second wake must not queue behind the first")
	default:
	}
}

func TestQueryProcessCommandLineSeesThisProcess(t *testing.T) {
	cmd, ok := queryProcessCommandLine(uint32(os.Getpid()))
	if !ok || !strings.Contains(strings.ToLower(cmd), ".exe") {
		t.Fatalf("command line ok=%v %q", ok, cmd)
	}
}
