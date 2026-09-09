package database

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestFavoriteStoreRoundTripAndOwnerIsolation(t *testing.T) {
	store, err := NewFavoriteStore(filepath.Join(t.TempDir(), "database_favorites.json"))
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.Save(QueryFavorite{Name: "orders", SQL: "SELECT id FROM orders WHERE id = :id", OwnerID: "owner-a"})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == "" {
		t.Fatal("missing favorite id")
	}
	if _, err := store.Get(saved.ID, "owner-b"); err == nil || !strings.Contains(err.Error(), "another owner") {
		t.Fatalf("cross-owner get = %v", err)
	}
	got, err := store.Get(saved.ID, "owner-a")
	if err != nil || got.SQL != saved.SQL {
		t.Fatalf("get = %+v err=%v", got, err)
	}
	if _, err := store.Save(QueryFavorite{Name: "write", SQL: "UPDATE t SET a=1 WHERE id=1", OwnerID: "owner-a"}); err == nil {
		t.Fatal("write SQL must not be saved as a favorite")
	}
}

func TestHandleToolFavorites(t *testing.T) {
	m := NewManager(nil, nil)
	defer m.Close()
	ctx := WithRequestScope(context.Background(), RequestScope{OwnerID: "owner", SessionID: "session"})
	saved := mustToolResult[QueryFavorite](t, HandleTool(ctx, m, map[string]interface{}{
		"action": "save_favorite", "favorite_name": "q1", "sql": "SELECT 1 AS n",
	}))
	if saved.ID == "" {
		t.Fatal("save_favorite id")
	}
	listRaw := HandleTool(ctx, m, map[string]interface{}{"action": "list_favorites"})
	if !strings.Contains(listRaw, `"q1"`) {
		t.Fatalf("list = %s", listRaw)
	}
	other := WithRequestScope(context.Background(), RequestScope{OwnerID: "other", SessionID: "session"})
	got := HandleTool(other, m, map[string]interface{}{"action": "delete_favorite", "favorite_id": saved.ID})
	if !strings.Contains(got, "another owner") && !strings.Contains(got, "permission") {
		t.Fatalf("cross-owner delete = %s", got)
	}
}

func TestLookupFavoriteSQLRejectsOtherProfileAndNilStore(t *testing.T) {
	if _, err := lookupFavoriteSQL(nil, "fav", "owner", "conn"); err == nil {
		t.Fatal("nil manager must fail")
	}
	m := NewManager(nil, nil)
	defer m.Close()
	saved, err := m.favoriteStore().Save(QueryFavorite{Name: "q1", SQL: "SELECT 1 AS n", OwnerID: "owner", ProfileID: "crm"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lookupFavoriteSQL(m, saved.ID, "owner", "missing-conn"); err == nil || !strings.Contains(err.Error(), "another profile") {
		t.Fatalf("cross-profile lookup = %v", err)
	}
	sqlText, err := lookupFavoriteSQL(m, saved.ID, "owner", "")
	if err != nil || sqlText != "SELECT 1 AS n" {
		t.Fatalf("unbound connection lookup = %q err=%v", sqlText, err)
	}
}

func TestHandleToolRejectsReplicaArgs(t *testing.T) {
	m := NewManager(nil, nil)
	got := HandleTool(context.Background(), m, map[string]interface{}{"action": "connect", "profile_id": "x", "replica_host": "db"})
	if !strings.Contains(got, "replica routing must be configured on the profile") {
		t.Fatalf("got %s", got)
	}
}
