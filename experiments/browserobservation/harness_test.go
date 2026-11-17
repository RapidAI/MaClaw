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
	if !nodeContractReportOK(string(out)) {
		t.Fatal("node contract tests did not report zero failures")
	}
}

// nodeContractReportOK accepts Node's TAP summary and the spec reporter.
// Both say the run had tests and fail 0. A missing summary is not a pass.
func nodeContractReportOK(text string) bool {
	failZero := false
	sawTests := false
	for _, line := range strings.Split(text, "\n") {
		line = contractSummaryLine(line)
		if line == "fail 0" {
			failZero = true
		}
		if strings.HasPrefix(line, "tests ") && line != "tests 0" {
			sawTests = true
		}
	}
	return failZero && sawTests
}

func contractSummaryLine(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "#")
	line = strings.TrimPrefix(line, "ℹ")
	return strings.TrimSpace(line)
}

func TestNodeContractReportAcceptsBothReporters(t *testing.T) {
	tap := "TAP version 13\n# tests 20\n# fail 0\n"
	spec := "ℹ tests 20\nℹ fail 0\n"
	if !nodeContractReportOK(tap) || !nodeContractReportOK(spec) {
		t.Fatal("both node reporters must count as a passing contract run")
	}
	if nodeContractReportOK("ℹ tests 0\nℹ fail 0\n") || nodeContractReportOK("ℹ tests 20\nℹ fail 1\n") || nodeContractReportOK("") {
		t.Fatal("an empty or failing node report must not pass")
	}
}
