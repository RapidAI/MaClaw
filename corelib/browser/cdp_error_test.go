package browser

import (
	"encoding/json"
	"testing"
)

func TestIsCDPProtocolError_DetectsSentinel(t *testing.T) {
	errJSON, _ := json.Marshal(map[string]interface{}{
		"__cdp_error__": true,
		"error":         "Execution context was destroyed",
		"code":          -32000,
	})
	if !isCDPProtocolError(errJSON) {
		t.Fatal("expected CDP protocol error to be detected")
	}
}

func TestIsCDPProtocolError_RejectsNormalResult(t *testing.T) {
	normalJSON, _ := json.Marshal(map[string]interface{}{
		"result": map[string]interface{}{
			"value": "hello",
		},
	})
	if isCDPProtocolError(normalJSON) {
		t.Fatal("expected normal result to NOT be detected as protocol error")
	}
}

func TestIsCDPProtocolError_RejectsShortJSON(t *testing.T) {
	if isCDPProtocolError([]byte(`{}`)) {
		t.Fatal("expected short JSON to NOT be detected as protocol error")
	}
	if isCDPProtocolError(nil) {
		t.Fatal("expected nil to NOT be detected as protocol error")
	}
}

func TestParseCDPProtocolError_ExtractsMessage(t *testing.T) {
	errJSON, _ := json.Marshal(map[string]interface{}{
		"__cdp_error__": true,
		"error":         "Cannot find context with specified id",
		"code":          -32000,
	})
	err := parseCDPProtocolError(errJSON)
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	want := "cdp error -32000: Cannot find context with specified id"
	if err.Error() != want {
		t.Fatalf("got %q, want %q", err.Error(), want)
	}
}

func TestParseCDPProtocolError_HandlesUnparsable(t *testing.T) {
	err := parseCDPProtocolError([]byte(`not json`))
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if err.Error() != "cdp protocol error (unparsable)" {
		t.Fatalf("unexpected error message: %v", err)
	}
}
