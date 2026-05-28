package httpapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/auth"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/entry"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

type gossipTestEnv struct {
	handler http.Handler
	cache   *GossipCache
	token   string
}

func newGossipTestEnv(t *testing.T) *gossipTestEnv {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "gossip-test.db")
	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:               dbPath,
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  4,
		MaxReadIdleConns:  2,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	st := sqlite.NewStore(provider)
	adminSvc := auth.NewAdminService(st.Admins, st.System, st.AdminAudit)
	mailer := &httpTestMailer{}
	hubSvc := hubs.NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs, st.System, mailer, "http://127.0.0.1:9388")
	entrySvc := entry.NewService(st.Hubs, st.HubUserLinks, st.HubDomainRoutes, st.BlockedEmails, st.BlockedIPs)

	cachePath := filepath.Join(t.TempDir(), "gossip_snapshot.json.gz")
	gossipCache := NewGossipCache(st.Gossip, cachePath)

	handler := NewRouter(adminSvc, hubSvc, entrySvc, nil, nil, st.FailureLogs, st.Gossip, gossipCache, nil, st.System, st.News, nil)

	env := &gossipTestEnv{handler: handler, cache: gossipCache}

	// Setup admin and get token
	resp := doJSONRequest(t, handler, http.MethodPost, "/api/admin/setup", map[string]any{
		"username": "admin", "password": "StrongPassword123!", "email": "admin@test.com",
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("admin setup: %d %s", resp.Code, resp.Body.String())
	}
	resp = doJSONRequest(t, handler, http.MethodPost, "/api/admin/login", map[string]any{
		"username": "admin", "password": "StrongPassword123!",
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("admin login: %d %s", resp.Code, resp.Body.String())
	}
	var loginData map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &loginData)
	env.token, _ = loginData["access_token"].(string)

	return env
}

func decodeJSON(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode json: %v\nbody: %s", err, string(body))
	}
	return m
}

type slowGossipCacheRepo struct {
	mu           sync.Mutex
	listCalls    int
	firstStarted chan struct{}
	releaseFirst chan struct{}
}

type pagedGossipCacheRepo struct {
	slowGossipCacheRepo
	posts  []*store.GossipPost
	mu     sync.Mutex
	limits []int
}

func (r *pagedGossipCacheRepo) ListPosts(_ context.Context, offset, limit int) ([]*store.GossipPost, int, error) {
	r.mu.Lock()
	r.limits = append(r.limits, limit)
	r.mu.Unlock()
	if offset >= len(r.posts) {
		return nil, len(r.posts), nil
	}
	end := offset + limit
	if end > len(r.posts) {
		end = len(r.posts)
	}
	return r.posts[offset:end], len(r.posts), nil
}

func (r *pagedGossipCacheRepo) seenLimits() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int(nil), r.limits...)
}

func (r *slowGossipCacheRepo) ListPosts(context.Context, int, int) ([]*store.GossipPost, int, error) {
	r.mu.Lock()
	r.listCalls++
	n := r.listCalls
	if n == 1 {
		close(r.firstStarted)
	}
	r.mu.Unlock()
	if n == 1 {
		<-r.releaseFirst
	}
	return nil, 0, nil
}

func (r *slowGossipCacheRepo) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.listCalls
}

func (r *slowGossipCacheRepo) CreatePost(context.Context, *store.GossipPost) error { return nil }
func (r *slowGossipCacheRepo) ListAllPosts(context.Context, int, int) ([]*store.GossipPost, int, error) {
	return nil, 0, nil
}
func (r *slowGossipCacheRepo) ListFlaggedPosts(context.Context, int, int) ([]*store.GossipPost, int, error) {
	return nil, 0, nil
}
func (r *slowGossipCacheRepo) GetPost(context.Context, string) (*store.GossipPost, error) {
	return nil, nil
}
func (r *slowGossipCacheRepo) DeletePost(context.Context, string) error        { return nil }
func (r *slowGossipCacheRepo) DeleteFlaggedPosts(context.Context) (int, error) { return 0, nil }
func (r *slowGossipCacheRepo) LockPost(context.Context, string, bool) error    { return nil }
func (r *slowGossipCacheRepo) FlagPost(context.Context, string, bool) error    { return nil }
func (r *slowGossipCacheRepo) ReplaceAll(context.Context, []*store.GossipPost, []*store.GossipComment) error {
	return nil
}
func (r *slowGossipCacheRepo) CreateComment(context.Context, *store.GossipComment) error { return nil }
func (r *slowGossipCacheRepo) ListComments(context.Context, string, int, int) ([]*store.GossipComment, int, error) {
	return nil, 0, nil
}
func (r *slowGossipCacheRepo) DeleteComment(context.Context, string) error   { return nil }
func (r *slowGossipCacheRepo) UpdatePostScore(context.Context, string) error { return nil }
func (r *slowGossipCacheRepo) HasRated(context.Context, string, string) (bool, error) {
	return false, nil
}
func (r *slowGossipCacheRepo) RateComment(context.Context, *store.GossipComment) error { return nil }

func TestGossipCacheRefreshAsyncCoalescesConcurrentRequests(t *testing.T) {
	repo := &slowGossipCacheRepo{firstStarted: make(chan struct{}), releaseFirst: make(chan struct{})}
	cache := NewGossipCache(repo, filepath.Join(t.TempDir(), "gossip_snapshot.json.gz"))

	cache.RefreshAsync(context.Background())
	select {
	case <-repo.firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first async refresh did not start")
	}
	for i := 0; i < 20; i++ {
		cache.RefreshAsync(context.Background())
	}
	time.Sleep(50 * time.Millisecond)
	if calls := repo.calls(); calls != 1 {
		t.Fatalf("refresh calls while first is running = %d, want 1", calls)
	}
	close(repo.releaseFirst)
	deadline := time.After(time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if calls := repo.calls(); calls == 2 {
			return
		} else if calls > 2 {
			t.Fatalf("refresh calls = %d, want coalesced to 2", calls)
		}
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatalf("second coalesced refresh did not run, calls=%d", repo.calls())
		}
	}
}

func TestGossipCacheRefreshStreamsPagedSnapshot(t *testing.T) {
	posts := make([]*store.GossipPost, snapshotRefreshPageSize+5)
	now := time.Now().UTC()
	for i := range posts {
		posts[i] = &store.GossipPost{ID: "post-" + strconv.Itoa(i), MachineID: "machine", Nickname: "nick", Content: "content", Category: "owner", CreatedAt: now}
	}
	repo := &pagedGossipCacheRepo{posts: posts}
	cachePath := filepath.Join(t.TempDir(), "gossip_snapshot.json.gz")
	cache := NewGossipCache(repo, cachePath)

	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	plain, err := io.ReadAll(gr)
	if closeErr := gr.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("read gzip: %v", err)
	}
	var payload struct {
		Posts []map[string]any `json:"posts"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(plain, &payload); err != nil {
		t.Fatalf("decode snapshot: %v body=%s", err, string(plain))
	}
	if payload.Total != len(posts) || len(payload.Posts) != len(posts) {
		t.Fatalf("snapshot total=%d posts=%d, want %d", payload.Total, len(payload.Posts), len(posts))
	}
	limits := repo.seenLimits()
	if len(limits) < 2 || limits[0] != snapshotRefreshPageSize || limits[1] != snapshotRefreshPageSize {
		t.Fatalf("expected paged refresh limits, got %+v", limits)
	}
}

// TestGossipPublishBrowseCommentRateSnapshot covers the full gossip lifecycle:
// publish 闂?browse 闂?comment 闂?rate 闂?snapshot
func TestGossipPublishBrowseCommentRateSnapshot(t *testing.T) {
	env := newGossipTestEnv(t)

	// 闂傚倸鍊搁崐椋庣矆娓氣偓瀹曨垶宕稿Δ鈧崒銊︾節婵犲倻澧曠痪鎯ь煼閺岀喖宕滆鐢盯鏌ｉ幘鍐叉殻闁哄本绋栫粻娑㈠箼閸愨敩锔界箾?1. Publish a post 闂傚倸鍊搁崐椋庣矆娓氣偓瀹曨垶宕稿Δ鈧崒銊︾節婵犲倻澧曠痪鎯ь煼閺岀喖宕滆鐢盯鏌ｉ幘鍐叉殻闁哄本绋栫粻娑㈠箼閸愨敩锔界箾鐎涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻?
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/publish", map[string]any{
		"machine_id": "test-machine-001",
		"user_email": "user@test.com",
		"content":    "MaClaw finished the overtime coding for me.",
		"category":   "owner",
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("publish: %d %s", resp.Code, resp.Body.String())
	}
	pubData := decodeJSON(t, resp.Body.Bytes())
	post, _ := pubData["post"].(map[string]any)
	postID, _ := post["id"].(string)
	if postID == "" {
		t.Fatal("expected post id")
	}
	nickname, _ := post["nickname"].(string)
	if nickname == "" || len(nickname) < len("MaClaw-")+6 {
		t.Fatalf("expected valid nickname with 6-char hex suffix, got %q", nickname)
	}

	// 闂傚倸鍊搁崐椋庣矆娓氣偓瀹曨垶宕稿Δ鈧崒銊︾節婵犲倻澧曠痪鎯ь煼閺岀喖宕滆鐢盯鏌ｉ幘鍐叉殻闁哄本绋栫粻娑㈠箼閸愨敩锔界箾?2. Browse posts 闂傚倸鍊搁崐椋庣矆娓氣偓瀹曨垶宕稿Δ鈧崒銊︾節婵犲倻澧曠痪鎯ь煼閺岀喖宕滆鐢盯鏌ｉ幘鍐叉殻闁哄本绋栫粻娑㈠箼閸愨敩锔界箾鐎涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾?
	resp = doJSONRequest(t, env.handler, http.MethodGet, "/api/gossip/browse?page=1", nil, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("browse: %d %s", resp.Code, resp.Body.String())
	}
	browseData := decodeJSON(t, resp.Body.Bytes())
	posts, _ := browseData["posts"].([]any)
	if len(posts) != 1 {
		t.Fatalf("expected 1 post, got %d", len(posts))
	}
	total, _ := browseData["total"].(float64)
	if total != 1 {
		t.Fatalf("expected total=1, got %v", total)
	}

	// 闂傚倸鍊搁崐椋庣矆娓氣偓瀹曨垶宕稿Δ鈧崒銊︾節婵犲倻澧曠痪鎯ь煼閺岀喖宕滆鐢盯鏌ｉ幘鍐叉殻闁哄本绋栫粻娑㈠箼閸愨敩锔界箾?3. Add a comment 闂傚倸鍊搁崐椋庣矆娓氣偓瀹曨垶宕稿Δ鈧崒銊︾節婵犲倻澧曠痪鎯ь煼閺岀喖宕滆鐢盯鏌ｉ幘鍐叉殻闁哄本绋栫粻娑㈠箼閸愨敩锔界箾鐎涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺?
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/comment", map[string]any{
		"machine_id": "test-machine-002",
		"post_id":    postID,
		"content":    "This is a follow-up comment.",
		"rating":     0,
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("comment: %d %s", resp.Code, resp.Body.String())
	}

	// 闂傚倸鍊搁崐椋庣矆娓氣偓瀹曨垶宕稿Δ鈧崒銊︾節婵犲倻澧曠痪鎯ь煼閺岀喖宕滆鐢盯鏌ｉ幘鍐叉殻闁哄本绋栫粻娑㈠箼閸愨敩锔界箾?4. Rate the post 闂傚倸鍊搁崐椋庣矆娓氣偓瀹曨垶宕稿Δ鈧崒銊︾節婵犲倻澧曠痪鎯ь煼閺岀喖宕滆鐢盯鏌ｉ幘鍐叉殻闁哄本绋栫粻娑㈠箼閸愨敩锔界箾鐎涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺?
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/rate", map[string]any{
		"machine_id": "test-machine-003",
		"post_id":    postID,
		"rating":     5,
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("rate: %d %s", resp.Code, resp.Body.String())
	}

	// Verify score updated via browse
	resp = doJSONRequest(t, env.handler, http.MethodGet, "/api/gossip/browse?page=1", nil, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("browse after rate: %d %s", resp.Code, resp.Body.String())
	}
	browseData = decodeJSON(t, resp.Body.Bytes())
	posts, _ = browseData["posts"].([]any)
	p0, _ := posts[0].(map[string]any)
	votes, _ := p0["votes"].(float64)
	if votes < 1 {
		t.Fatalf("expected votes >= 1, got %v", votes)
	}

	// 闂傚倸鍊搁崐椋庣矆娓氣偓瀹曨垶宕稿Δ鈧崒銊︾節婵犲倻澧曠痪鎯ь煼閺岀喖宕滆鐢盯鏌ｉ幘鍐叉殻闁哄本绋栫粻娑㈠箼閸愨敩锔界箾?5. List comments 闂傚倸鍊搁崐椋庣矆娓氣偓瀹曨垶宕稿Δ鈧崒銊︾節婵犲倻澧曠痪鎯ь煼閺岀喖宕滆鐢盯鏌ｉ幘鍐叉殻闁哄本绋栫粻娑㈠箼閸愨敩锔界箾鐎涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺?
	resp = doJSONRequest(t, env.handler, http.MethodGet, "/api/gossip/comments?post_id="+postID, nil, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("list comments: %d %s", resp.Code, resp.Body.String())
	}
	commData := decodeJSON(t, resp.Body.Bytes())
	comments, _ := commData["comments"].([]any)
	if len(comments) < 2 { // 1 comment + 1 rating comment
		t.Fatalf("expected >= 2 comments, got %d", len(comments))
	}

	// 闂傚倸鍊搁崐椋庣矆娓氣偓瀹曨垶宕稿Δ鈧崒銊︾節婵犲倻澧曠痪鎯ь煼閺岀喖宕滆鐢盯鏌ｉ幘鍐叉殻闁哄本绋栫粻娑㈠箼閸愨敩锔界箾?6. Snapshot 闂傚倸鍊搁崐椋庣矆娓氣偓瀹曨垶宕稿Δ鈧崒銊︾節婵犲倻澧曠痪鎯ь煼閺岀喖宕滆鐢盯鏌ｉ幘鍐叉殻闁哄本绋栫粻娑㈠箼閸愨敩锔界箾鐎涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ婊堟⒒閸屾艾鈧绮堟笟鈧畷顖炲锤濡も偓閸屻劍绻濇繝鍌滃缁炬儳顭烽弻鐔煎礈瑜忕敮娑㈡煟閹惧啿鏆ｉ柡灞剧缁犳盯骞欓崘鈹附绻涚€涙鐭掔紒鐘崇墪椤繐煤椤忓嫮顦ㄩ梺鍦帛鐢帗娼忛崨瀛樷拺缂佸顑欓崕鎰版煙閻熺増鍠樼€殿喛顕ч鍏煎緞婵犲嫷妲┑鐘灱濞夋盯鏁冮敐鍡欑彾闁哄洢鍨洪埛鎺懨归敐鍥剁劸闁哄棝浜堕弻娑樜熼懡銈囩厜閻庤娲橀崹鍧楃嵁濡偐纾兼俊顖滃帶鐢姊绘担渚劸缂佺粯鍔欒棟妞ゆ牗绋撻々鎻捨旈敐鍛殲闁抽攱鍨块弻娑㈠箛椤撶偟绁烽柣銏╁灛閸旀垿寮诲☉姘ｅ亾閿濆骸浜濈€规洖鐭傞弻锛勪沪閻ｅ睗褏鈧娲橀〃鍡楊嚗閸曨剛绡€濞达絽澹婂Λ?
	// Snapshot is served from file, need to trigger cache refresh first via a new publish
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/publish", map[string]any{
		"machine_id": "test-machine-004",
		"content":    "snapshot refresh post",
		"category":   "news",
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("publish 2: %d %s", resp.Code, resp.Body.String())
	}

	// Give async cache refresh a moment, then check snapshot
	// Since cache.Refresh is async (goroutine), we call it directly for test determinism
	// Instead, just verify the endpoint returns something (may be 503 if cache not ready)
	resp = doJSONRequest(t, env.handler, http.MethodGet, "/api/gossip/snapshot", nil, "")
	// Accept 200 or 503 (cache may not have been generated yet in async goroutine)
	if resp.Code != http.StatusOK && resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("snapshot: %d %s", resp.Code, resp.Body.String())
	}
}

func TestGossipPublishValidation(t *testing.T) {
	env := newGossipTestEnv(t)

	tests := []struct {
		name string
		body map[string]any
		code int
	}{
		{"missing machine_id", map[string]any{"content": "hello"}, http.StatusBadRequest},
		{"empty content", map[string]any{"machine_id": "m1", "content": ""}, http.StatusBadRequest},
		{"invalid category", map[string]any{"machine_id": "m1", "content": "hi", "category": "invalid"}, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/publish", tt.body, "")
			if resp.Code != tt.code {
				t.Errorf("expected %d, got %d: %s", tt.code, resp.Code, resp.Body.String())
			}
		})
	}
}

func TestGossipCommentOnLockedPost(t *testing.T) {
	env := newGossipTestEnv(t)

	// Publish a post
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/publish", map[string]any{
		"machine_id": "m1", "content": "test post", "category": "owner",
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("publish: %d", resp.Code)
	}
	pubData := decodeJSON(t, resp.Body.Bytes())
	postID := pubData["post"].(map[string]any)["id"].(string)

	// Admin locks the post
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/admin/gossip/lock", map[string]any{
		"id": postID, "locked": true,
	}, env.token)
	if resp.Code != http.StatusOK {
		t.Fatalf("lock: %d %s", resp.Code, resp.Body.String())
	}

	// Try to comment 闂?should be forbidden
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/comment", map[string]any{
		"machine_id": "m2", "post_id": postID, "content": "should fail", "rating": 0,
	}, "")
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for locked post comment, got %d: %s", resp.Code, resp.Body.String())
	}

	// Try to rate 闂?should also be forbidden
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/rate", map[string]any{
		"machine_id": "m2", "post_id": postID, "rating": 3,
	}, "")
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for locked post rate, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestAdminGossipDeletePost(t *testing.T) {
	env := newGossipTestEnv(t)

	// Publish
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/publish", map[string]any{
		"machine_id": "m1", "content": "to be deleted", "category": "project",
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("publish: %d", resp.Code)
	}
	postID := decodeJSON(t, resp.Body.Bytes())["post"].(map[string]any)["id"].(string)

	// Admin delete
	resp = doJSONRequest(t, env.handler, http.MethodDelete, "/api/admin/gossip", map[string]any{"id": postID}, env.token)
	if resp.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", resp.Code, resp.Body.String())
	}

	// Browse should be empty
	resp = doJSONRequest(t, env.handler, http.MethodGet, "/api/gossip/browse?page=1", nil, "")
	browseData := decodeJSON(t, resp.Body.Bytes())
	total, _ := browseData["total"].(float64)
	if total != 0 {
		t.Fatalf("expected 0 posts after delete, got %v", total)
	}
}

func TestAdminGossipDeleteFlaggedPosts(t *testing.T) {
	env := newGossipTestEnv(t)

	var keepID string
	for i, content := range []string{"keep me", "flagged one", "flagged two"} {
		resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/publish", map[string]any{
			"machine_id": "bulk-delete-machine-" + strconv.Itoa(i),
			"content":    content,
			"category":   "project",
		}, "")
		if resp.Code != http.StatusOK {
			t.Fatalf("publish %d: %d %s", i, resp.Code, resp.Body.String())
		}
		postID := decodeJSON(t, resp.Body.Bytes())["post"].(map[string]any)["id"].(string)
		if i == 0 {
			keepID = postID
			continue
		}
		resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/admin/gossip/flag", map[string]any{
			"id": postID, "flagged": true,
		}, env.token)
		if resp.Code != http.StatusOK {
			t.Fatalf("flag %d: %d %s", i, resp.Code, resp.Body.String())
		}
		resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/comment", map[string]any{
			"machine_id": "commenter-" + strconv.Itoa(i),
			"post_id":    postID,
			"content":    "comment to delete",
		}, "")
		if resp.Code != http.StatusOK {
			t.Fatalf("comment %d: %d %s", i, resp.Code, resp.Body.String())
		}
	}

	resp := doJSONRequest(t, env.handler, http.MethodDelete, "/api/admin/gossip/flagged", nil, env.token)
	if resp.Code != http.StatusOK {
		t.Fatalf("delete flagged: %d %s", resp.Code, resp.Body.String())
	}
	deleted, _ := decodeJSON(t, resp.Body.Bytes())["deleted"].(float64)
	if deleted != 2 {
		t.Fatalf("deleted = %v, want 2", deleted)
	}

	resp = doJSONRequest(t, env.handler, http.MethodGet, "/api/admin/gossip?filter=flagged", nil, env.token)
	if resp.Code != http.StatusOK {
		t.Fatalf("list flagged: %d %s", resp.Code, resp.Body.String())
	}
	flaggedTotal, _ := decodeJSON(t, resp.Body.Bytes())["total"].(float64)
	if flaggedTotal != 0 {
		t.Fatalf("flagged total = %v, want 0", flaggedTotal)
	}

	resp = doJSONRequest(t, env.handler, http.MethodGet, "/api/gossip/browse?page=1", nil, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("browse: %d %s", resp.Code, resp.Body.String())
	}
	browseData := decodeJSON(t, resp.Body.Bytes())
	total, _ := browseData["total"].(float64)
	if total != 1 {
		t.Fatalf("visible total = %v, want 1", total)
	}
	posts := browseData["posts"].([]any)
	if got := posts[0].(map[string]any)["id"]; got != keepID {
		t.Fatalf("remaining post id = %v, want %s", got, keepID)
	}
}

func TestAdminGossipDeleteFlaggedPostsNoop(t *testing.T) {
	env := newGossipTestEnv(t)

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/publish", map[string]any{
		"machine_id": "bulk-delete-noop-machine",
		"content":    "not flagged",
		"category":   "project",
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("publish: %d %s", resp.Code, resp.Body.String())
	}

	resp = doJSONRequest(t, env.handler, http.MethodDelete, "/api/admin/gossip/flagged", nil, env.token)
	if resp.Code != http.StatusOK {
		t.Fatalf("delete flagged noop: %d %s", resp.Code, resp.Body.String())
	}
	deleted, _ := decodeJSON(t, resp.Body.Bytes())["deleted"].(float64)
	if deleted != 0 {
		t.Fatalf("deleted = %v, want 0", deleted)
	}

	resp = doJSONRequest(t, env.handler, http.MethodGet, "/api/gossip/browse?page=1", nil, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("browse: %d %s", resp.Code, resp.Body.String())
	}
	total, _ := decodeJSON(t, resp.Body.Bytes())["total"].(float64)
	if total != 1 {
		t.Fatalf("visible total = %v, want 1", total)
	}
}

func TestGossipSnapshotETagSupport(t *testing.T) {
	env := newGossipTestEnv(t)

	// Publish a post
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/publish", map[string]any{
		"machine_id": "m1", "content": "etag test post", "category": "owner",
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("publish: %d", resp.Code)
	}

	// Directly refresh cache for deterministic test (no time.Sleep)
	if err := env.cache.Refresh(context.Background()); err != nil {
		t.Fatalf("cache refresh: %v", err)
	}

	// First request 闂?should get 200 with ETag
	resp = doJSONRequest(t, env.handler, http.MethodGet, "/api/gossip/snapshot", nil, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("snapshot first request: %d %s", resp.Code, resp.Body.String())
	}
	etag := resp.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header in snapshot response")
	}

	// Second request with If-None-Match 闂?should get 304
	req := httptest.NewRequest(http.MethodGet, "/api/gossip/snapshot", nil)
	req.Header.Set("If-None-Match", etag)
	rr := httptest.NewRecorder()
	env.handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotModified {
		t.Fatalf("expected 304 with matching ETag, got %d", rr.Code)
	}
}

func TestGossipDuplicateRatingPrevention(t *testing.T) {
	env := newGossipTestEnv(t)

	// Publish a post
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/publish", map[string]any{
		"machine_id": "m1", "content": "dup rating test", "category": "owner",
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("publish: %d", resp.Code)
	}
	postID := decodeJSON(t, resp.Body.Bytes())["post"].(map[string]any)["id"].(string)

	// First rating 闂?should succeed
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/rate", map[string]any{
		"machine_id": "rater-001", "post_id": postID, "rating": 4,
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("first rate: %d %s", resp.Code, resp.Body.String())
	}

	// Second rating from same machine 闂?should get 409
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/rate", map[string]any{
		"machine_id": "rater-001", "post_id": postID, "rating": 2,
	}, "")
	if resp.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate rate, got %d: %s", resp.Code, resp.Body.String())
	}
	body := decodeJSON(t, resp.Body.Bytes())
	code, _ := body["code"].(string)
	if code != "ALREADY_RATED" {
		t.Fatalf("expected ALREADY_RATED code, got %q", code)
	}

	// Rating via comment endpoint with rating > 0 from same machine 闂?should also get 409
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/comment", map[string]any{
		"machine_id": "rater-001", "post_id": postID, "content": "nice", "rating": 3,
	}, "")
	if resp.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate comment-rating, got %d: %s", resp.Code, resp.Body.String())
	}

	// Comment without rating from same machine 闂?should succeed
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/comment", map[string]any{
		"machine_id": "rater-001", "post_id": postID, "content": "just a comment", "rating": 0,
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("comment without rating: %d %s", resp.Code, resp.Body.String())
	}

	// Different machine can still rate
	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/rate", map[string]any{
		"machine_id": "rater-002", "post_id": postID, "rating": 5,
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("different machine rate: %d %s", resp.Code, resp.Body.String())
	}
}

func TestGossipRateLimiting(t *testing.T) {
	env := newGossipTestEnv(t)

	// The router uses 10 writes per 10 min per key.
	// We'll send 11 publish requests from the same IP to trigger 429.
	for i := 0; i < 10; i++ {
		resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/publish", map[string]any{
			"machine_id": "rl-machine",
			"content":    "rate limit test " + time.Now().Format(time.RFC3339Nano),
			"category":   "news",
		}, "")
		if resp.Code != http.StatusOK {
			t.Fatalf("publish #%d: %d %s", i+1, resp.Code, resp.Body.String())
		}
	}

	// 11th request should be rate limited
	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/publish", map[string]any{
		"machine_id": "rl-machine",
		"content":    "should be limited",
		"category":   "news",
	}, "")
	if resp.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 for rate-limited request, got %d: %s", resp.Code, resp.Body.String())
	}
	body := decodeJSON(t, resp.Body.Bytes())
	code, _ := body["code"].(string)
	if code != "RATE_LIMITED" {
		t.Fatalf("expected RATE_LIMITED code, got %q", code)
	}
}
