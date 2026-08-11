package enterpriseknowledge

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestAppendAutoRecallNoopEmpty(t *testing.T) {
	var b strings.Builder
	AppendAutoRecall(nil, &b, "hello", nil, 0)
	if b.Len() != 0 {
		t.Fatalf("expected empty, got %q", b.String())
	}
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	AppendAutoRecall(c, &b, "hello policy", nil, 0)
	if strings.Contains(b.String(), agent.EnterpriseKnowledgeAutoRecallHeader) {
		t.Fatalf("unexpected header with empty store: %q", b.String())
	}
}

func TestAppendAutoRecallFromDataDirNoop(t *testing.T) {
	var b strings.Builder
	AppendAutoRecallFromDataDir(t.TempDir(), &b, "anything", nil, 0)
	if b.Len() != 0 {
		t.Fatalf("expected empty: %q", b.String())
	}
}
