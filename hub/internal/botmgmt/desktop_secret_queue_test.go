package botmgmt

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestQueuedSecretStaysOffTheRecord(t *testing.T) {
	item := &desktopQueueItem{
		ClientMessageID: "m1",
		Content:         "SSH_PASSWORD=（已保存为 SSH_PASSWORD，在环境变量里，不要再索要，不要打印）",
		Secrets:         map[string]string{"SSH_PASSWORD": "s3cret-value"},
		EnqueuedAt:      time.Now(),
		State:           "queued",
	}
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "s3cret-value") {
		t.Fatalf("persisted queue %s", raw)
	}
	var back desktopQueueItem
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Secrets) != 0 {
		t.Fatalf("restored secrets %+v", back.Secrets)
	}
	if back.Content == "" {
		t.Fatal("the redacted text was dropped")
	}
}
