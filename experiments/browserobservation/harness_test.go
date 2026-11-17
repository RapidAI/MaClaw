package browserobservation

import (
	"os"
	"os/exec"
	"testing"
)

// TestTransportContract runs the local fake-server checks. Those checks are
// not observed calls to GLM 5.3 Flash.
func TestTransportContract(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Fatal("node is required to run the harness contract tests")
	}
	cmd := exec.Command("node", "--test", "agent-gate.test.mjs", "transport.test.mjs")
	cmd.Dir = "."
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
}
