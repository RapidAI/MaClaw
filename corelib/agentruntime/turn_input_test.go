package agentruntime

import (
	"encoding/json"
	"testing"
)

func TestTurnInputIsTransportNeutralAndOmitsHostPayload(t *testing.T) {
	input := TurnInput{
		SystemPrompt:  "system",
		UserText:      "hello",
		History:       []map[string]any{{"role": "user", "content": "prior"}},
		Attachments:   []map[string]any{{"type": "image", "file_name": "a.png"}},
		Platform:      "desktop",
		MinIterations: 2,
		Callbacks:     &TurnCallbacks{OnToken: func(string) {}},
		HostPayload:   struct{ Secret string }{Secret: "must-not-cross-runtime"},
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" || containsJSON(encoded, "must-not-cross-runtime") {
		t.Fatalf("host payload leaked into transport JSON: %s", encoded)
	}
	var decoded TurnInput
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.UserText != input.UserText || decoded.SystemPrompt != input.SystemPrompt || decoded.Platform != input.Platform || decoded.MinIterations != input.MinIterations {
		t.Fatalf("round-trip changed neutral fields: %#v", decoded)
	}
}

func containsJSON(encoded []byte, want string) bool {
	for i := 0; i+len(want) <= len(encoded); i++ {
		if string(encoded[i:i+len(want)]) == want {
			return true
		}
	}
	return false
}

func TestDecodeTurnInputAcceptsValueAndPointer(t *testing.T) {
	want := TurnInput{UserText: "hello"}
	for name, value := range map[string]any{
		"value":   want,
		"pointer": &want,
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := DecodeTurnInput(value)
			if !ok || got.UserText != want.UserText {
				t.Fatalf("DecodeTurnInput(%T) = %#v, %v", value, got, ok)
			}
		})
	}
	if _, ok := DecodeTurnInput((*TurnInput)(nil)); ok {
		t.Fatal("nil TurnInput pointer must be rejected")
	}
	if _, ok := DecodeTurnInput(struct{}{}); ok {
		t.Fatal("unrelated payload must be rejected")
	}
}
