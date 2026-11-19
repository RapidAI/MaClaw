package agentservice

import (
	"strings"
	"testing"
)

func TestScheduleFiringMustNotRearm(t *testing.T) {
	meta := map[string]string{"bot_origin": "schedule"}
	if got := scheduleFiringMustNotRearm(meta, map[string]interface{}{"action": "create"}); !strings.Contains(got, "不要再登记") {
		t.Fatalf("create %q", got)
	}
	if got := scheduleFiringMustNotRearm(meta, nil); !strings.Contains(got, "不要再登记") {
		t.Fatalf("default %q", got)
	}
	if got := scheduleFiringMustNotRearm(meta, map[string]interface{}{"action": "cancel"}); !strings.Contains(got, "不要再登记") {
		t.Fatalf("cancel %q", got)
	}
	if got := scheduleFiringMustNotRearm(meta, map[string]interface{}{"action": "list"}); got != "" {
		t.Fatalf("list %q", got)
	}
	if got := scheduleFiringMustNotRearm(nil, map[string]interface{}{"action": "create"}); got != "" {
		t.Fatalf("chat %q", got)
	}
}
