package botmgmt

import (
	"context"
	"testing"
)

// A server-started turn's files ride on its said note, are listed with it,
// and can be read back by the person's GUI.
func TestSayKeepsTheSpeechKind(t *testing.T) {
	svc := assignFixture(t)
	ctx := context.Background()
	note, err := svc.SayFilesKindFromInstance(ctx, "tenant-a", "alice", "inst_a", "", "后台任务结束了，下面是最后的输出\ntrain=1", "k-process", nil, "process")
	if err != nil || note.MessageKind != "process" {
		t.Fatalf("note %+v %v", note, err)
	}
	said, err := svc.ListSaid(ctx, "tenant-a", "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, n := range said {
		if n.ID == note.ID && n.MessageKind == "process" && n.Kind == "say" {
			found = true
		}
	}
	if !found {
		t.Fatalf("said %+v", said)
	}
	plain, err := svc.SayFromInstance(ctx, "tenant-a", "alice", "inst_a", "", "结果是 F1 0.81，建议停在这一轮。", "k-outcome")
	if err != nil || plain.MessageKind != "" {
		t.Fatalf("untagged say %+v %v", plain, err)
	}
}

func TestSayFilesFromInstanceCarriesReadableFiles(t *testing.T) {
	svc := assignFixture(t)
	svc.MailFileDir = t.TempDir()
	ctx := context.Background()
	f, err := svc.StoreBotMailFile("tenant-a", "REPORT.md", "text/markdown", []byte("# report\nF1 0.79"))
	if err != nil {
		t.Fatal(err)
	}
	note, err := svc.SayFilesFromInstance(ctx, "tenant-a", "alice", "inst_a", "", "训练结束，报告见附件。", "k1", []BotMailFile{f})
	if err != nil || len(note.Files) != 1 {
		t.Fatalf("note = %+v %v", note, err)
	}
	said, err := svc.ListSaid(ctx, "tenant-a", "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range said {
		if n.ID == note.ID && len(n.Files) == 1 && n.Files[0].Name == "REPORT.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("said = %+v", said)
	}
	got, data, err := svc.ReadBotMailFile(ctx, "tenant-a", "alice", "", note.ID, "REPORT.md")
	if err != nil || got.Name != "REPORT.md" || string(data) != "# report\nF1 0.79" {
		t.Fatalf("read = %+v %q %v", got, data, err)
	}
}
