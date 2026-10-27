package botmgmt

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRoomAttentionMentionsAndAlreadyAnswered(t *testing.T) {
	svc := assignFixture(t)
	ctx := context.Background()
	name := "项目群"
	room, err := svc.SaveRoom(ctx, "tenant-a", "alice", "", RoomInput{Name: &name, Members: []string{"主", "A", "B"}})
	if err != nil || len(room.Members) != 3 {
		t.Fatalf("room = %+v %v", room, err)
	}
	// No mention: only the main bot (attention all by default) hears it.
	_, woken, err := svc.PostRoomMessage(ctx, "tenant-a", "alice", room.ID, "今天进度怎么样？")
	if err != nil || len(woken) != 1 || woken[0] != "bot_main" {
		t.Fatalf("woken = %v %v", woken, err)
	}
	// A mention wakes only the named bot.
	_, woken, _ = svc.PostRoomMessage(ctx, "tenant-a", "alice", room.ID, "@A 稿子写到哪了")
	if len(woken) != 1 || woken[0] != "bot_a" {
		t.Fatalf("mention woken = %v", woken)
	}
	box, _ := svc.BotMailbox(ctx, "tenant-a", "alice", "bot_a", nil, false)
	if len(box) != 1 || !strings.Contains(box[0].Text, "action=say room="+room.ID) || !strings.Contains(box[0].Text, "今天进度怎么样") {
		t.Fatalf("mail = %+v", box)
	}
	// Attention all on B: both hear the next message, and the hint names the other.
	if _, err := svc.SaveRoom(ctx, "tenant-a", "alice", room.ID, RoomInput{Attention: map[string]string{"B": "all", "主": "off"}}); err != nil {
		t.Fatal(err)
	}
	_, woken, _ = svc.PostRoomMessage(ctx, "tenant-a", "alice", room.ID, "大家有什么问题")
	if len(woken) != 1 || woken[0] != "bot_b" {
		t.Fatalf("attention woken = %v", woken)
	}
	first, err := svc.SayFromInstance(ctx, "tenant-a", "alice", "inst_b", room.ID, "我这边没有问题，封面已经做好。", "")
	if err != nil || len(first.AnsweredBy) != 0 {
		t.Fatalf("first say %+v %v", first, err)
	}
	second, err := svc.SayFromInstance(ctx, "tenant-a", "alice", "inst_a", room.ID, "我还差结尾一段。", "")
	if err != nil || len(second.AnsweredBy) != 1 || second.AnsweredBy[0] != "bot_b" {
		t.Fatalf("second say %+v %v", second, err)
	}
	var said *AlreadySaidError
	if _, err := svc.SayFromInstance(ctx, "tenant-a", "alice", "inst_main", room.ID, "我这边没有问题，封面已经做好", ""); !errors.As(err, &said) {
		t.Fatalf("repeat err = %v", err)
	}
	items, _ := svc.ListSaid(ctx, "tenant-a", "alice", room.ID)
	if len(items) != 5 {
		t.Fatalf("transcript = %d", len(items))
	}
	if _, err := svc.SayFromInstance(ctx, "tenant-a", "alice", "inst_a", "room_nope", "x", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown room err = %v", err)
	}
	if err := svc.DeleteRoom(ctx, "tenant-a", "alice", room.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRoomsKillSwitchAndValidation(t *testing.T) {
	svc := assignFixture(t)
	ctx := context.Background()
	if _, err := svc.SaveRoom(ctx, "tenant-a", "alice", "", RoomInput{Members: []string{"nobody"}}); !errors.Is(err, ErrNoSuchBot) {
		t.Fatalf("err = %v", err)
	}
	if _, err := svc.SaveRoom(ctx, "tenant-a", "alice", "", RoomInput{Members: []string{"A"}, Attention: map[string]string{"A": "loud"}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v", err)
	}
	t.Setenv("MACLAW_BOT_ROOMS", "off")
	if _, err := svc.SaveRoom(ctx, "tenant-a", "alice", "", RoomInput{Members: []string{"A"}}); err == nil {
		t.Fatal("rooms while off")
	}
}
