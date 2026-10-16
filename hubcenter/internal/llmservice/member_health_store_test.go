package llmservice

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type lockedHealthSettings struct {
	mu   sync.Mutex
	data map[string]string
}

func (s *lockedHealthSettings) Set(_ context.Context, key, val string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = map[string]string{}
	}
	s.data[key] = val
	return nil
}

func (s *lockedHealthSettings) Get(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data[key], nil
}

func (s *lockedHealthSettings) List(context.Context) ([]*store.SystemSettingEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*store.SystemSettingEntry, 0, len(s.data))
	for key, value := range s.data {
		out = append(out, &store.SystemSettingEntry{Key: key, ValueJSON: value})
	}
	return out, nil
}

func (s *lockedHealthSettings) snapshot(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data[key]
}

func TestMemberHealthPersistsAndGroupsNodes(t *testing.T) {
	resetMemberHealth()
	t.Cleanup(resetMemberHealth)
	settings := &lockedHealthSettings{}
	bindMemberHealth("hc-1", settings)
	recordMemberHealthAttempt("Pool-A-1", 200, 40*time.Millisecond, "")
	recordMemberHealthAttempt("pool-a-1", 429, 10*time.Millisecond, "slow down")
	recordMemberHealthAttempt("pool-a-1", 503, 5*time.Millisecond, "upstream 503")
	recordMemberHealthTokens("pool-a-1", 5, 7)
	flushMemberHealth()
	saved := settings.snapshot(memberHealthSettingKey("hc-1"))
	if !strings.Contains(saved, `"requests":3`) || !strings.Contains(saved, `"revision":`) || strings.Contains(saved, "sk-live") {
		t.Fatalf("saved = %s", saved)
	}
	if MemberHealthIncomingNewer(saved, `{"revision":1,"days":{}}`) {
		t.Fatal("older snapshot was treated as newer")
	}
	if !MemberHealthIncomingNewer(saved, `{"revision":99,"days":{"2026-09-30":{}}}`) {
		t.Fatal("higher revision was not newer")
	}
	bindMemberHealth("hc-1", settings)
	day, _ := memberHealthDay(time.Now())
	report, err := ListMemberHealth(context.Background(), day, []AccessNodeView{{NodeID: "hc-1", Self: true}, {NodeID: "hc-2"}})
	if err != nil {
		t.Fatal(err)
	}
	if report.HealthScope != "persisted" || report.Timezone != "Asia/Shanghai" || report.Day != day {
		t.Fatalf("report = %+v", report)
	}
	if len(report.Nodes) != 2 || !report.Nodes[0].Self || report.Nodes[0].NodeID != "hc-1" {
		t.Fatalf("nodes = %+v", report.Nodes)
	}
	if len(report.Nodes[0].Members) != 1 {
		t.Fatalf("members = %+v", report.Nodes[0].Members)
	}
	member := report.Nodes[0].Members[0]
	if member.ID != "pool-a-1" || member.Requests != 3 || member.Success != 1 || member.Status429 != 1 || member.Status5xx != 1 || member.InputTokens != 5 || member.OutputTokens != 7 || member.LastError != "upstream 503" || member.AvgLatencyMs <= 0 {
		t.Fatalf("member = %+v", member)
	}
	if len(report.Nodes[1].Members) != 0 || report.Nodes[1].NodeID != "hc-2" {
		t.Fatalf("peer = %+v", report.Nodes[1])
	}
}

func TestMemberHealthPeriodTrafficSplitsArrayMembers(t *testing.T) {
	resetMemberHealth()
	t.Cleanup(resetMemberHealth)
	day, _ := memberHealthDay(time.Now())
	settings := &lockedHealthSettings{}
	peer := `{"node_id":"hc-3","revision":2,"days":{"` + day + `":{"gbf-openrouter-openrouter-free":{"input_tokens":10,"output_tokens":4}}}}`
	if err := settings.Set(context.Background(), memberHealthSettingKey("hc-3"), peer); err != nil {
		t.Fatal(err)
	}
	bindMemberHealth("hc-1", settings)
	recordMemberHealthTokens("GBF-Kilo", 20, 6)
	// hc-1 is also listed so a saved self snapshot must not be added on top of memory.
	if err := settings.Set(context.Background(), memberHealthSettingKey("hc-1"), `{"node_id":"hc-1","revision":1,"days":{"`+day+`":{"gbf-kilo":{"input_tokens":100,"output_tokens":100}}}}`); err != nil {
		t.Fatal(err)
	}
	got := MemberHealthPeriodTraffic(context.Background(), "Asia/Shanghai", time.Now(), []AccessNodeView{{NodeID: "hc-1"}, {NodeID: "hc-3"}})
	kilo := got["gbf-kilo"]
	if kilo.Day.InputTokens != 20 || kilo.Day.OutputTokens != 6 || kilo.Week.InputTokens != 20 || kilo.Month.TotalTokens != 26 {
		t.Fatalf("self = %+v", kilo)
	}
	open := got["gbf-openrouter-openrouter-free"]
	if open.Day.InputTokens != 10 || open.Day.OutputTokens != 4 || open.Day.TotalTokens != 14 {
		t.Fatalf("peer = %+v", open)
	}
}

func TestMemberHealthPeriodTrafficWeekCrossesMonth(t *testing.T) {
	resetMemberHealth()
	t.Cleanup(resetMemberHealth)
	settings := &lockedHealthSettings{}
	raw := `{"node_id":"hc-2","revision":1,"days":{"2026-09-01":{"gbf-a":{"input_tokens":50,"output_tokens":50}},"2026-09-28":{"gbf-a":{"input_tokens":5,"output_tokens":1}},"2026-10-01":{"gbf-a":{"input_tokens":7,"output_tokens":2}},"2026-10-02":{"gbf-a":{"input_tokens":100,"output_tokens":100}}}}`
	if err := settings.Set(context.Background(), memberHealthSettingKey("hc-2"), raw); err != nil {
		t.Fatal(err)
	}
	bindMemberHealth("hc-1", settings)
	now := time.Date(2026, 10, 1, 15, 0, 0, 0, TrafficLocation("Asia/Shanghai"))
	got := MemberHealthPeriodTraffic(context.Background(), "Asia/Shanghai", now, []AccessNodeView{{NodeID: "hc-2"}})
	row := got["gbf-a"]
	if row.Day.InputTokens != 7 || row.Day.OutputTokens != 2 {
		t.Fatalf("day = %+v, want only 2026-10-01", row.Day)
	}
	if row.Week.InputTokens != 12 || row.Week.OutputTokens != 3 || row.Week.TotalTokens != 15 {
		t.Fatalf("week = %+v, want Monday 2026-09-28 through 2026-10-01", row.Week)
	}
	if row.Month.InputTokens != 7 || row.Month.OutputTokens != 2 || row.Month.TotalTokens != 9 {
		t.Fatalf("month = %+v, want October only", row.Month)
	}
}
