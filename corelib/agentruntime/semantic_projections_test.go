package agentruntime

import "testing"

func TestDocumentReadResultProjectionRemovesLegacyContinuationHints(t *testing.T) {
	got := DocumentReadResultProjection("title\n# path: C:/secret.md\nbody\n# continue: OfficeRead")
	if got != "title\nbody" {
		t.Fatalf("projection=%q", got)
	}
	if got := DocumentReadResultProjection("# pathname: keep\n# continuation: keep"); got != "# pathname: keep\n# continuation: keep" {
		t.Fatalf("similar prefixes were removed: %q", got)
	}
}

func TestDeliveryInvocationArgsOnlyWashesEmptyLegacyEnvelope(t *testing.T) {
	if got := DeliveryInvocationArgs(`{"arguments":"{}"}`); got != "{}" {
		t.Fatalf("empty envelope=%q", got)
	}
	for _, input := range []string{
		`{"arguments":"{\"artifact_id\":\"a\"}"}`,
		`{"arguments":"null"}`,
		`{"arguments":"{}","extra":true}`,
		`{"arguments":{}}`,
		`not-json`,
	} {
		if got := DeliveryInvocationArgs(input); got != input {
			t.Fatalf("contentful envelope changed: input=%q output=%q", input, got)
		}
	}
}
