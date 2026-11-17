package browserobservation

import (
	"os"
	"os/exec"
	"strings"
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
	out, err := cmd.CombinedOutput()
	os.Stdout.Write(out)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if !strings.Contains(text, "\n# fail 0\n") && !strings.HasSuffix(text, "\n# fail 0\n") {
		t.Fatal("node contract tests did not report zero failures")
	}
}
