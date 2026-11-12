package botmgmt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type routeSrv struct {
	turnKeySrv
	rmu    sync.Mutex
	asked  []map[string]any
	answer string
	delay  time.Duration
}

func (s *routeSrv) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/bot-threads/route") {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		s.rmu.Lock()
		s.asked = append(s.asked, body)
		answer, delay := s.answer, s.delay
		s.rmu.Unlock()
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		_, _ = w.Write([]byte(answer))
		return
	}
	s.handler(w, r)
}

func (s *routeSrv) sessions(t *testing.T) []string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, raw := range s.messages {
		var body struct {
			Key string `json:"client_session_key"`
		}
		_ = json.Unmarshal([]byte(raw), &body)
		out = append(out, body.Key)
	}
	return out
}

func TestHubRoutesMessageByContentOnServer(t *testing.T) {
	fake := &routeSrv{answer: `{"session":"bot_alice#t-old","title":"整理上周订单报表","via":"content","confidence":0.9}`}
	srv := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)

	admission, err := svc.AdmitDesktopMessageIn(context.Background(), "tenant-a", "alice", "bot_alice", "把报表金额改成两位小数", "execute", "k-route", "")
	if err != nil {
		t.Fatal(err)
	}
	if admission.Session != "bot_alice#t-old" || admission.RoutedVia != "content" || admission.RoutedTitle != "整理上周订单报表" {
		t.Fatalf("admission = %+v", admission)
	}
	if got := fake.sessions(t); len(got) != 1 || got[0] != "bot_alice#t-old" {
		t.Fatalf("srv message sessions = %v", got)
	}
	fake.rmu.Lock()
	asked := fake.asked
	fake.rmu.Unlock()
	if len(asked) != 1 || asked[0]["bot_id"] != "bot_alice" || asked[0]["content"] != "把报表金额改成两位小数" {
		t.Fatalf("route asks = %v", asked)
	}
}

func TestHubExplicitPickSkipsContentRouting(t *testing.T) {
	fake := &routeSrv{answer: `{"session":"bot_alice#t-old","via":"content","confidence":0.9}`}
	srv := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer srv.Close()
	svc := newTurnKeyService(t, srv)
	admission, err := svc.AdmitDesktopMessageIn(context.Background(), "tenant-a", "alice", "bot_alice", "另外一件事", "execute", "k-new", "bot_alice")
	if err != nil {
		t.Fatal(err)
	}
	if admission.Session != "bot_alice" || admission.RoutedVia != "" {
		t.Fatalf("admission = %+v", admission)
	}
	fake.rmu.Lock()
	n := len(fake.asked)
	fake.rmu.Unlock()
	if n != 0 {
		t.Fatalf("routing asked %d times for an explicit pick", n)
	}
}

func TestHubRouteTimeoutAndUnsureGoToMain(t *testing.T) {
	prev := desktopRouteBudget
	desktopRouteBudget = 100 * time.Millisecond
	t.Cleanup(func() { desktopRouteBudget = prev })
	for name, fake := range map[string]*routeSrv{
		"timeout": {answer: `{"session":"bot_alice#t-old","via":"content","confidence":1}`, delay: time.Second},
		"unsure":  {answer: `{"session":"bot_alice","via":"main","confidence":0.4}`},
		"foreign": {answer: `{"session":"bot_other#t-x","via":"content","confidence":1}`},
	} {
		srv := httptest.NewServer(http.HandlerFunc(fake.handle))
		svc := newTurnKeyService(t, srv)
		start := time.Now()
		admission, err := svc.AdmitDesktopMessageIn(context.Background(), "tenant-a", "alice", "bot_alice", "查天气", "execute", "k-"+name, "")
		srv.Close()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if admission.Session != "bot_alice" || admission.RoutedVia != "" {
			t.Fatalf("%s: admission = %+v", name, admission)
		}
		if name == "timeout" && time.Since(start) > 900*time.Millisecond {
			t.Fatalf("timeout held the post for %v", time.Since(start))
		}
	}
}
