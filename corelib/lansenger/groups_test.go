package lansenger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueryGroupsAndGetGroupInfo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/apptoken/create", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]any{"appToken": "tok", "expiresIn": 3600},
		})
	})
	mux.HandleFunc("/v2/groups/fetch", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("app_token") != "tok" {
			t.Fatalf("missing app_token")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data": map[string]any{
				"totalGroupIds": 2,
				"groupIds":      []string{"g-1", "g-2"},
			},
		})
	})
	mux.HandleFunc("/v2/groups/g-1/info/fetch", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data": map[string]any{
				"name":         "研发群",
				"description":  "工程讨论",
				"totalMembers": 12,
				"owner":        map[string]any{"staffId": "s1", "name": "Alice"},
				"state":        0,
			},
		})
	})
	mux.HandleFunc("/v2/groups/g-2/info/fetch", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data": map[string]any{
				"name":         "产品群",
				"totalMembers": 5,
				"state":        0,
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	gw := NewGateway(Config{AppID: "app", AppSecret: "sec", ApiGatewayURL: srv.URL}, nil)
	total, ids, err := gw.QueryGroups(context.Background(), 0, 100)
	if err != nil {
		t.Fatalf("QueryGroups: %v", err)
	}
	if total != 2 || len(ids) != 2 || ids[0] != "g-1" {
		t.Fatalf("QueryGroups = total=%d ids=%v", total, ids)
	}

	info, err := gw.GetGroupInfo(context.Background(), "g-1")
	if err != nil {
		t.Fatalf("GetGroupInfo: %v", err)
	}
	if info.Name != "研发群" || info.OwnerName != "Alice" || info.TotalMembers != 12 {
		t.Fatalf("GetGroupInfo = %#v", info)
	}

	list, err := gw.ListJoinedGroups(context.Background())
	if err != nil {
		t.Fatalf("ListJoinedGroups: %v", err)
	}
	if list.Total != 2 || len(list.Groups) != 2 {
		t.Fatalf("ListJoinedGroups = %#v", list)
	}
	if list.Groups[0].Name != "研发群" || list.Groups[1].Name != "产品群" {
		t.Fatalf("group names = %#v", list.Groups)
	}
}

func TestQueryGroupsPermissionDenied(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/apptoken/create", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]any{"appToken": "tok", "expiresIn": 3600},
		})
	})
	mux.HandleFunc("/v2/groups/fetch", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 10005,
			"errMsg":  "API服务无权限",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	gw := NewGateway(Config{AppID: "app", AppSecret: "sec", ApiGatewayURL: srv.URL}, nil)
	_, _, err := gw.QueryGroups(context.Background(), 0, 100)
	if err == nil || !strings.Contains(err.Error(), "10005") {
		t.Fatalf("want permission error, got %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 10005 {
		t.Fatalf("want *APIError 10005, got %T %v", err, err)
	}
}

func TestListJoinedGroupsPaginatesAndToleratesInfoFailure(t *testing.T) {
	// 105 IDs => 2 pages with page_size=100
	const n = 105
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/apptoken/create", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]any{"appToken": "tok", "expiresIn": 3600},
		})
	})
	mux.HandleFunc("/v2/groups/fetch", func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("page_offset"))
		size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
		if size <= 0 {
			size = 100
		}
		ids := make([]string, 0, size)
		for i := offset; i < n && len(ids) < size; i++ {
			ids = append(ids, fmt.Sprintf("g-%d", i))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data": map[string]any{
				"totalGroupIds": n,
				"groupIds":      ids,
			},
		})
	})
	mux.HandleFunc("/v2/groups/", func(w http.ResponseWriter, r *http.Request) {
		// path: /v2/groups/g-N/info/fetch
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 3 {
			http.NotFound(w, r)
			return
		}
		id := parts[2]
		if id == "g-3" {
			_ = json.NewEncoder(w).Encode(map[string]any{"errCode": 500, "errMsg": "boom"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data": map[string]any{
				"name":         "name-" + id,
				"totalMembers": 1,
				"state":        0,
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	gw := NewGateway(Config{AppID: "app", AppSecret: "sec", ApiGatewayURL: srv.URL}, nil)
	list, err := gw.ListJoinedGroups(context.Background())
	if err != nil {
		t.Fatalf("ListJoinedGroups: %v", err)
	}
	if list.Total != n || len(list.Groups) != n {
		t.Fatalf("got total=%d len=%d want %d", list.Total, len(list.Groups), n)
	}
	// Order preserved: first ID still first
	if list.Groups[0].GroupID != "g-0" || list.Groups[0].Name != "name-g-0" {
		t.Fatalf("first group = %#v", list.Groups[0])
	}
	// Failed detail degrades to id-only
	if list.Groups[3].GroupID != "g-3" || list.Groups[3].Name != "g-3" {
		t.Fatalf("degraded group = %#v", list.Groups[3])
	}
}

func TestQueryGroupsRetriesOnExpiredToken(t *testing.T) {
	var tokenCalls atomic.Int32
	var fetchCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/apptoken/create", func(w http.ResponseWriter, r *http.Request) {
		n := tokenCalls.Add(1)
		tok := "tok-old"
		if n >= 2 {
			tok = "tok-new"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]any{"appToken": tok, "expiresIn": 3600},
		})
	})
	mux.HandleFunc("/v2/groups/fetch", func(w http.ResponseWriter, r *http.Request) {
		fetchCalls.Add(1)
		if r.URL.Query().Get("app_token") == "tok-old" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"errCode": 42001,
				"errMsg":  "access_token expired",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data": map[string]any{
				"totalGroupIds": 1,
				"groupIds":      []string{"g-ok"},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	gw := NewGateway(Config{AppID: "app", AppSecret: "sec", ApiGatewayURL: srv.URL}, nil)
	total, ids, err := gw.QueryGroups(context.Background(), 0, 100)
	if err != nil {
		t.Fatalf("QueryGroups after token retry: %v", err)
	}
	if total != 1 || len(ids) != 1 || ids[0] != "g-ok" {
		t.Fatalf("got total=%d ids=%v", total, ids)
	}
	if tokenCalls.Load() < 2 || fetchCalls.Load() < 2 {
		t.Fatalf("expected token refresh + retry, tokenCalls=%d fetchCalls=%d", tokenCalls.Load(), fetchCalls.Load())
	}
}

func TestDedupeAndCapJoinedGroupIDs(t *testing.T) {
	// Return duplicates and more IDs than the UI cap in one page-sized chunks.
	const n = maxJoinedGroupsListed + 50
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/apptoken/create", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]any{"appToken": "tok", "expiresIn": 3600},
		})
	})
	mux.HandleFunc("/v2/groups/fetch", func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("page_offset"))
		size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
		if size <= 0 {
			size = 100
		}
		ids := make([]string, 0, size+1)
		for i := offset; i < n && len(ids) < size; i++ {
			ids = append(ids, fmt.Sprintf("g-%d", i))
		}
		// Inject a duplicate of the first id when present.
		if len(ids) > 0 {
			ids = append(ids, ids[0])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data": map[string]any{
				"totalGroupIds": n,
				"groupIds":      ids,
			},
		})
	})
	mux.HandleFunc("/v2/groups/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]any{"name": "n", "totalMembers": 1},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	gw := NewGateway(Config{AppID: "app", AppSecret: "sec", ApiGatewayURL: srv.URL}, nil)
	list, err := gw.ListJoinedGroups(context.Background())
	if err != nil {
		t.Fatalf("ListJoinedGroups: %v", err)
	}
	if len(list.Groups) != maxJoinedGroupsListed {
		t.Fatalf("listed %d, want cap %d", len(list.Groups), maxJoinedGroupsListed)
	}
	if list.Total < maxJoinedGroupsListed {
		t.Fatalf("total %d should remain at least the loaded count", list.Total)
	}
	seen := map[string]struct{}{}
	for _, g := range list.Groups {
		if _, ok := seen[g.GroupID]; ok {
			t.Fatalf("duplicate group id %q", g.GroupID)
		}
		seen[g.GroupID] = struct{}{}
	}
}

func TestListJoinedGroupsEmpty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/apptoken/create", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]any{"appToken": "tok", "expiresIn": 3600},
		})
	})
	mux.HandleFunc("/v2/groups/fetch", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data": map[string]any{
				"totalGroupIds": 0,
				"groupIds":      nil,
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	gw := NewGateway(Config{AppID: "app", AppSecret: "sec", ApiGatewayURL: srv.URL}, nil)
	list, err := gw.ListJoinedGroups(context.Background())
	if err != nil {
		t.Fatalf("ListJoinedGroups: %v", err)
	}
	if list.Groups == nil || len(list.Groups) != 0 {
		t.Fatalf("empty list must be non-nil empty slice, got %#v", list.Groups)
	}
}

func TestListJoinedGroupsContextCancelDuringInfoFetch(t *testing.T) {
	var infoCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/apptoken/create", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]any{"appToken": "tok", "expiresIn": 3600},
		})
	})
	mux.HandleFunc("/v2/groups/fetch", func(w http.ResponseWriter, r *http.Request) {
		ids := make([]string, 16)
		for i := range ids {
			ids[i] = fmt.Sprintf("g-%d", i)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]any{"totalGroupIds": 16, "groupIds": ids},
		})
	})
	mux.HandleFunc("/v2/groups/", func(w http.ResponseWriter, r *http.Request) {
		infoCalls.Add(1)
		// Slow enough that a short timeout expires mid-fetch.
		time.Sleep(80 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errCode": 0,
			"data":    map[string]any{"name": "x", "totalMembers": 1},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	gw := NewGateway(Config{AppID: "app", AppSecret: "sec", ApiGatewayURL: srv.URL}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	// After cancel, fetchGroupInfos degrades remaining rows instead of hanging.
	list, err := gw.ListJoinedGroups(ctx)
	if err != nil {
		// Context may surface if cancellation hits during QueryGroups token path.
		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context") {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	if list == nil || len(list.Groups) != 16 {
		t.Fatalf("want 16 degraded/partial rows, got %#v", list)
	}
	if infoCalls.Load() == 0 {
		t.Fatal("expected at least one GetGroupInfo attempt before cancel")
	}
}
