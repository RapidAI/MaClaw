package session

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestListRecentOwnedIgnoresNewerForeignSessions(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	if err := store.Persist(SessionDocument{
		SessionID: "user-1_1001", Timestamp: now, Platform: "gui", Topic: "mine",
		FullText: "user talked about the morning weather",
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		if err := store.Persist(SessionDocument{
			SessionID: "user-2_" + strconv.Itoa(2000+i),
			Timestamp: now.Add(time.Duration(i+1) * time.Second),
			Platform:  "gui",
			Topic:     "foreign",
			FullText:  "other user talked about secret foreign chat",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Persist(SessionDocument{
		SessionID: "user_extra_3003", Timestamp: now.Add(time.Minute), Platform: "gui", Topic: "collision",
		FullText: "prefix collision must not leak",
	}); err != nil {
		t.Fatal(err)
	}

	owned, err := store.ListRecentOwned("user-1", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(owned) != 1 || owned[0].SessionID != "user-1_1001" {
		t.Fatalf("owned=%#v", owned)
	}
	if owned[0].Snippet == "" || !strings.Contains(owned[0].Snippet, "morning weather") {
		t.Fatalf("owned snippet=%q", owned[0].Snippet)
	}

	hits, err := store.SearchOwned(`"morning weather"`, "user-1", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].SessionID != "user-1_1001" {
		t.Fatalf("search owned=%#v", hits)
	}
	foreign, err := store.SearchOwned(`"secret foreign"`, "user-1", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(foreign) != 1 || foreign[0].SessionID != "" || foreign[0].Snippet != "no results found" {
		t.Fatalf("foreign search leaked=%#v", foreign)
	}

	if err := store.Persist(SessionDocument{
		SessionID: "user-1_1001x", Timestamp: now.Add(2 * time.Minute), Platform: "gui", Topic: "junk",
		FullText: "trailing junk after digits must not count as owned",
	}); err != nil {
		t.Fatal(err)
	}
	owned, err = store.ListRecentOwned("user-1", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(owned) != 1 || owned[0].SessionID != "user-1_1001" {
		t.Fatalf("junk suffix leaked=%#v", owned)
	}

	shorter, err := store.ListRecentOwned("user", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(shorter) != 0 {
		t.Fatalf("shorter principal leaked prefix collision=%#v", shorter)
	}
}
