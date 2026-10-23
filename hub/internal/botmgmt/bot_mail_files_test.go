package botmgmt

import (
	"context"
	"errors"
	"testing"
)

// A file shared mid-run is readable by the owner as soon as the message
// is stored, and only through a message of that owner.
func TestSharedFileIsReadableWithTheMessage(t *testing.T) {
	svc, _ := dispatchFixture(t)
	svc.BotDispatch = false
	svc.MailFileDir = t.TempDir()
	ctx := context.Background()
	f, err := svc.StoreBotMailFile("tenant-a", `C:\x\orders.csv`, "text/csv", []byte("a,b\n1,2\n"))
	if err != nil || f.Name != "orders.csv" {
		t.Fatalf("store: %+v %v", f, err)
	}
	note, err := svc.PostBotReportFromInstance(ctx, "tenant-a", "alice", "inst_pub", BotCrossNote{Target: "main", Topic: "artifact", Text: "导出了", Artifacts: []string{"/home/d/orders.csv"}, Files: []BotMailFile{f}})
	if err != nil || len(note.Files) != 1 {
		t.Fatalf("report: %+v %v", note, err)
	}
	got, data, err := svc.ReadBotMailFile(ctx, "tenant-a", "alice", "", note.ID, "orders.csv")
	if err != nil || string(data) != "a,b\n1,2\n" || got.MIME != "text/csv" {
		t.Fatalf("read: %+v %q %v", got, data, err)
	}
	if _, _, err := svc.ReadBotMailFile(ctx, "tenant-a", "mallory", "", note.ID, "orders.csv"); !errors.Is(err, ErrNotFound) && err == nil {
		t.Fatal("another user read the file")
	}
	if _, _, err := svc.ReadBotMailFile(ctx, "tenant-a", "alice", "bot_other", note.ID, "orders.csv"); err == nil {
		t.Fatal("a bot outside the message read the file")
	}
}
