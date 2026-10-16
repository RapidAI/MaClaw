package llmservice

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

const (
	// MemberHealthSettingPrefix is the system-setting key prefix for one node's
	// saved member health. Each node writes only its own key. Replication can
	// deliver those writes out of order, so an older snapshot must not replace
	// a newer one.
	MemberHealthSettingPrefix = "llm_member_health:"
	memberHealthKeepDays      = 14
	memberHealthFlushEvery    = 20
)

// MemberHealthMember is one member's saved totals for a node and a day.
type MemberHealthMember struct {
	ID           string    `json:"id"`
	Requests     int       `json:"requests"`
	Success      int       `json:"success"`
	Status429    int       `json:"status_429"`
	Status5xx    int       `json:"status_5xx"`
	AvgLatencyMs int64     `json:"avg_latency_ms"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	LastError    string    `json:"last_error,omitempty"`
	LastErrorAt  time.Time `json:"last_error_at,omitempty"`
}

// MemberHealthNode is one HubCenter node's saved member totals.
type MemberHealthNode struct {
	NodeID  string               `json:"node_id"`
	Self    bool                 `json:"self,omitempty"`
	Members []MemberHealthMember `json:"members"`
}

// MemberHealthReport is the persisted per-node health for one calendar day.
type MemberHealthReport struct {
	Day         string             `json:"day"`
	Timezone    string             `json:"timezone"`
	HealthScope string             `json:"health_scope"`
	Nodes       []MemberHealthNode `json:"nodes"`
}

type memberHealthBucket struct {
	Requests     int       `json:"requests"`
	Success      int       `json:"success"`
	Status429    int       `json:"status_429"`
	Status5xx    int       `json:"status_5xx"`
	LatencyMSSum int64     `json:"latency_ms_sum"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	LastError    string    `json:"last_error,omitempty"`
	LastErrorAt  time.Time `json:"last_error_at,omitempty"`
}

type memberHealthFile struct {
	NodeID   string                                    `json:"node_id"`
	Revision int64                                     `json:"revision"`
	Days     map[string]map[string]*memberHealthBucket `json:"days"`
}

var memberHealthPersistMu sync.Mutex

var memberHealthStore = struct {
	sync.Mutex
	nodeID   string
	settings store.SystemSettingsRepository
	file     memberHealthFile
	dirty    bool
	pending  int
	flushing bool
}{}

// BindMemberHealth loads this node's saved daily health and keeps later
// attempts in the same record. An empty node ID leaves the counts in memory.
func (s *Service) BindMemberHealth(nodeID string) {
	var settings store.SystemSettingsRepository
	if s != nil {
		settings = s.system
	}
	bindMemberHealth(strings.TrimSpace(nodeID), settings)
}

func bindMemberHealth(nodeID string, settings store.SystemSettingsRepository) {
	memberHealthStore.Lock()
	defer memberHealthStore.Unlock()
	memberHealthStore.nodeID = nodeID
	memberHealthStore.settings = settings
	memberHealthStore.file = memberHealthFile{NodeID: nodeID, Days: map[string]map[string]*memberHealthBucket{}}
	memberHealthStore.dirty = false
	memberHealthStore.pending = 0
	if settings == nil || nodeID == "" {
		return
	}
	raw, err := settings.Get(context.Background(), memberHealthSettingKey(nodeID))
	if err != nil || strings.TrimSpace(raw) == "" {
		return
	}
	var file memberHealthFile
	if json.Unmarshal([]byte(raw), &file) != nil || file.Days == nil {
		return
	}
	memberHealthStore.file.Days = file.Days
	memberHealthStore.file.NodeID = nodeID
	memberHealthStore.file.Revision = file.Revision
}

func resetMemberHealth() {
	memberHealthStore.Lock()
	memberHealthStore.nodeID = ""
	memberHealthStore.settings = nil
	memberHealthStore.file = memberHealthFile{Days: map[string]map[string]*memberHealthBucket{}}
	memberHealthStore.dirty = false
	memberHealthStore.pending = 0
	memberHealthStore.flushing = false
	memberHealthStore.Unlock()
}

func memberHealthSettingKey(nodeID string) string {
	return MemberHealthSettingPrefix + strings.TrimSpace(nodeID)
}

func memberHealthDay(now time.Time) (string, time.Time) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	local := now.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return start.Format("2006-01-02"), start
}

func recordMemberHealthAttempt(providerID string, status int, latency time.Duration, errMsg string) {
	providerID = providerIDKey(providerID)
	if providerID == "" {
		return
	}
	ms := latency.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	memberHealthStore.Lock()
	bucket := memberHealthBucketFor(providerID, time.Now())
	if bucket == nil {
		memberHealthStore.Unlock()
		return
	}
	bucket.Requests++
	switch {
	case status >= 200 && status < 400:
		bucket.Success++
	case status == 429:
		bucket.Status429++
	case status >= 500:
		bucket.Status5xx++
	}
	bucket.LatencyMSSum += ms
	if errMsg = trimMemberError(errMsg); errMsg != "" {
		bucket.LastError = errMsg
		bucket.LastErrorAt = time.Now().UTC()
	}
	memberHealthTouchLocked()
	memberHealthStore.Unlock()
}

func recordMemberHealthTokens(providerID string, inputTokens, outputTokens int64) {
	if inputTokens == 0 && outputTokens == 0 {
		return
	}
	providerID = providerIDKey(providerID)
	if providerID == "" {
		return
	}
	memberHealthStore.Lock()
	bucket := memberHealthBucketFor(providerID, time.Now())
	if bucket == nil {
		memberHealthStore.Unlock()
		return
	}
	if inputTokens > 0 {
		bucket.InputTokens += inputTokens
	}
	if outputTokens > 0 {
		bucket.OutputTokens += outputTokens
	}
	memberHealthTouchLocked()
	memberHealthStore.Unlock()
}

func memberHealthBucketFor(providerID string, now time.Time) *memberHealthBucket {
	if memberHealthStore.file.Days == nil {
		memberHealthStore.file.Days = map[string]map[string]*memberHealthBucket{}
	}
	day, start := memberHealthDay(now)
	dayBuckets := memberHealthStore.file.Days[day]
	if dayBuckets == nil {
		dayBuckets = map[string]*memberHealthBucket{}
		memberHealthStore.file.Days[day] = dayBuckets
	}
	bucket := dayBuckets[providerID]
	if bucket == nil {
		bucket = &memberHealthBucket{}
		dayBuckets[providerID] = bucket
	}
	cutoff := start.AddDate(0, 0, -(memberHealthKeepDays - 1)).Format("2006-01-02")
	for saved := range memberHealthStore.file.Days {
		if saved < cutoff {
			delete(memberHealthStore.file.Days, saved)
		}
	}
	return bucket
}

// memberHealthTouchLocked marks the record dirty. Callers hold memberHealthStore.
// Saves are coalesced: every 20 updates, or 2 seconds after the first pending
// update. Overlapping saves take a fresh snapshot under one lock so an older
// snapshot cannot land after a newer one.
func memberHealthTouchLocked() {
	memberHealthStore.dirty = true
	memberHealthStore.file.Revision++
	memberHealthStore.pending++
	if memberHealthStore.settings == nil || memberHealthStore.nodeID == "" {
		return
	}
	immediate := memberHealthStore.pending >= memberHealthFlushEvery
	if immediate {
		memberHealthStore.pending = 0
	}
	if memberHealthStore.flushing {
		return
	}
	memberHealthStore.flushing = true
	if immediate {
		go persistMemberHealthLatest()
		return
	}
	time.AfterFunc(2*time.Second, persistMemberHealthLatest)
}

func memberHealthSnapshotLocked() (memberHealthFile, bool) {
	raw, err := json.Marshal(memberHealthStore.file)
	if err != nil {
		return memberHealthFile{}, false
	}
	var copy memberHealthFile
	if json.Unmarshal(raw, &copy) != nil || copy.Days == nil {
		return memberHealthFile{}, false
	}
	copy.NodeID = memberHealthStore.nodeID
	return copy, true
}

func writeMemberHealthSnapshot(settings store.SystemSettingsRepository, nodeID string, file memberHealthFile) error {
	if settings == nil || strings.TrimSpace(nodeID) == "" {
		return nil
	}
	file.NodeID = nodeID
	raw, err := json.Marshal(file)
	if err != nil {
		return err
	}
	return settings.Set(context.Background(), memberHealthSettingKey(nodeID), string(raw))
}

func persistMemberHealthLatest() {
	memberHealthPersistMu.Lock()
	defer memberHealthPersistMu.Unlock()
	for {
		memberHealthStore.Lock()
		if !memberHealthStore.dirty || memberHealthStore.settings == nil || memberHealthStore.nodeID == "" {
			memberHealthStore.flushing = false
			memberHealthStore.Unlock()
			return
		}
		snapshot, ok := memberHealthSnapshotLocked()
		settings := memberHealthStore.settings
		nodeID := memberHealthStore.nodeID
		if !ok {
			memberHealthStore.flushing = false
			memberHealthStore.Unlock()
			return
		}
		memberHealthStore.dirty = false
		memberHealthStore.pending = 0
		memberHealthStore.Unlock()

		if err := writeMemberHealthSnapshot(settings, nodeID, snapshot); err != nil {
			memberHealthStore.Lock()
			memberHealthStore.dirty = true
			memberHealthStore.flushing = false
			memberHealthStore.Unlock()
			time.AfterFunc(2*time.Second, func() {
				memberHealthStore.Lock()
				defer memberHealthStore.Unlock()
				if !memberHealthStore.dirty || memberHealthStore.flushing || memberHealthStore.settings == nil || memberHealthStore.nodeID == "" {
					return
				}
				memberHealthStore.flushing = true
				go persistMemberHealthLatest()
			})
			return
		}

		memberHealthStore.Lock()
		if memberHealthStore.dirty && memberHealthStore.settings != nil && memberHealthStore.nodeID != "" {
			memberHealthStore.Unlock()
			continue
		}
		memberHealthStore.flushing = false
		memberHealthStore.Unlock()
		return
	}
}

func flushMemberHealth() {
	persistMemberHealthLatest()
}

// MemberHealthIncomingNewer reports whether incoming is a later snapshot than
// the copy already stored for the same node. Equal or older revisions are not
// newer, so a replayed replica cannot roll the series backward.
func MemberHealthIncomingNewer(localRaw, incomingRaw string) bool {
	incomingRev, incomingOK := memberHealthRevision(incomingRaw)
	if !incomingOK {
		return false
	}
	localRev, localOK := memberHealthRevision(localRaw)
	if !localOK {
		return true
	}
	return incomingRev > localRev
}

func memberHealthRevision(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	var file memberHealthFile
	if json.Unmarshal([]byte(raw), &file) != nil || file.Days == nil {
		return 0, false
	}
	return file.Revision, true
}

// ListMemberHealth returns saved totals for one Asia/Shanghai day.
// nodes lists the cluster; this node's totals come from memory, and every
// other node is read from the last snapshot it saved.
func ListMemberHealth(ctx context.Context, day string, nodes []AccessNodeView) (MemberHealthReport, error) {
	day = strings.TrimSpace(day)
	if day == "" {
		day, _ = memberHealthDay(time.Now())
	}
	flushMemberHealth()
	report := MemberHealthReport{
		Day:         day,
		Timezone:    "Asia/Shanghai",
		HealthScope: "persisted",
		Nodes:       []MemberHealthNode{},
	}
	memberHealthStore.Lock()
	selfID := memberHealthStore.nodeID
	selfMembers := memberHealthMembersLocked(day, selfID)
	settings := memberHealthStore.settings
	memberHealthStore.Unlock()

	seen := map[string]bool{}
	if selfID == "" {
		selfID = "local"
	}
	report.Nodes = append(report.Nodes, MemberHealthNode{NodeID: selfID, Self: true, Members: selfMembers})
	seen[strings.ToLower(selfID)] = true

	for _, node := range nodes {
		id := strings.TrimSpace(node.NodeID)
		if id == "" || seen[strings.ToLower(id)] {
			continue
		}
		seen[strings.ToLower(id)] = true
		report.Nodes = append(report.Nodes, MemberHealthNode{
			NodeID:  id,
			Members: loadMemberHealthMembers(ctx, settings, id, day),
		})
	}
	sort.SliceStable(report.Nodes, func(i, j int) bool {
		if report.Nodes[i].Self != report.Nodes[j].Self {
			return report.Nodes[i].Self
		}
		return report.Nodes[i].NodeID < report.Nodes[j].NodeID
	})
	return report, nil
}

func memberHealthMembersLocked(day, _ string) []MemberHealthMember {
	buckets := memberHealthStore.file.Days[day]
	return bucketsToMembers(buckets)
}

func loadMemberHealthMembers(ctx context.Context, settings store.SystemSettingsRepository, nodeID, day string) []MemberHealthMember {
	if settings == nil || strings.TrimSpace(nodeID) == "" {
		return []MemberHealthMember{}
	}
	raw, err := settings.Get(ctx, memberHealthSettingKey(nodeID))
	if err != nil || strings.TrimSpace(raw) == "" {
		return []MemberHealthMember{}
	}
	var file memberHealthFile
	if json.Unmarshal([]byte(raw), &file) != nil || file.Days == nil {
		return []MemberHealthMember{}
	}
	return bucketsToMembers(file.Days[day])
}

func bucketsToMembers(buckets map[string]*memberHealthBucket) []MemberHealthMember {
	if len(buckets) == 0 {
		return []MemberHealthMember{}
	}
	ids := make([]string, 0, len(buckets))
	for id := range buckets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]MemberHealthMember, 0, len(ids))
	for _, id := range ids {
		bucket := buckets[id]
		if bucket == nil {
			continue
		}
		var avg int64
		if bucket.Requests > 0 {
			avg = bucket.LatencyMSSum / int64(bucket.Requests)
		}
		out = append(out, MemberHealthMember{
			ID:           id,
			Requests:     bucket.Requests,
			Success:      bucket.Success,
			Status429:    bucket.Status429,
			Status5xx:    bucket.Status5xx,
			AvgLatencyMs: avg,
			InputTokens:  bucket.InputTokens,
			OutputTokens: bucket.OutputTokens,
			LastError:    bucket.LastError,
			LastErrorAt:  bucket.LastErrorAt,
		})
	}
	return out
}

// MemberHealthPeriodTraffic sums saved per-member tokens into the same
// day/week/month windows as the provider traffic cards. nodes are the cluster
// peers; this process contributes its in-memory totals and does not re-read
// its own snapshot. Historical array usage is still booked on the logical
// array id, so these totals are what each upstream actually served.
func MemberHealthPeriodTraffic(ctx context.Context, timezone string, now time.Time, nodes []AccessNodeView) map[string]ProviderPeriodTraffic {
	loc := TrafficLocation(timezone)
	if now.IsZero() {
		now = time.Now()
	}
	dayStart, weekStart, monthStart := ProviderTrafficBounds(now, loc)
	healthLoc := TrafficLocation("Asia/Shanghai")
	monthLabel := monthStart.In(healthLoc).Format("2006-01-02")
	weekLabel := weekStart.In(healthLoc).Format("2006-01-02")
	dayLabel := dayStart.In(healthLoc).Format("2006-01-02")
	floor := monthLabel
	if weekLabel < floor {
		floor = weekLabel
	}
	out := map[string]ProviderPeriodTraffic{}
	for _, file := range memberHealthFiles(ctx, nodes) {
		for day, buckets := range file.Days {
			day = strings.TrimSpace(day)
			if len(day) != len("2006-01-02") || day < floor || day > dayLabel {
				continue
			}
			inMonth := day >= monthLabel
			inWeek := day >= weekLabel
			inDay := day == dayLabel
			for id, bucket := range buckets {
				id = providerIDKey(id)
				if id == "" || bucket == nil || (bucket.InputTokens == 0 && bucket.OutputTokens == 0) {
					continue
				}
				row := out[id]
				if inMonth {
					addHealthTokens(&row.Month, bucket.InputTokens, bucket.OutputTokens)
				}
				if inWeek {
					addHealthTokens(&row.Week, bucket.InputTokens, bucket.OutputTokens)
				}
				if inDay {
					addHealthTokens(&row.Day, bucket.InputTokens, bucket.OutputTokens)
				}
				out[id] = row
			}
		}
	}
	return out
}

func addHealthTokens(dst *TokenTraffic, input, output int64) {
	if input < 0 {
		input = 0
	}
	if output < 0 {
		output = 0
	}
	dst.InputTokens += input
	dst.OutputTokens += output
	dst.TotalTokens += input + output
}

func memberHealthFiles(ctx context.Context, nodes []AccessNodeView) map[string]memberHealthFile {
	memberHealthStore.Lock()
	settings := memberHealthStore.settings
	selfID := strings.TrimSpace(memberHealthStore.nodeID)
	selfSnap, selfOK := memberHealthSnapshotLocked()
	memberHealthStore.Unlock()

	files := map[string]memberHealthFile{}
	seen := map[string]bool{}
	if selfOK && selfID != "" && (len(selfSnap.Days) > 0 || selfSnap.Revision > 0) {
		key := strings.ToLower(selfID)
		files[key] = selfSnap
		seen[key] = true
	}
	for _, node := range nodes {
		id := strings.TrimSpace(node.NodeID)
		key := strings.ToLower(id)
		if id == "" || seen[key] {
			continue
		}
		seen[key] = true
		file, ok := loadMemberHealthFile(ctx, settings, id)
		if !ok {
			continue
		}
		files[key] = file
	}
	return files
}

func loadMemberHealthFile(ctx context.Context, settings store.SystemSettingsRepository, nodeID string) (memberHealthFile, bool) {
	if settings == nil || strings.TrimSpace(nodeID) == "" {
		return memberHealthFile{}, false
	}
	raw, err := settings.Get(ctx, memberHealthSettingKey(nodeID))
	if err != nil || strings.TrimSpace(raw) == "" {
		return memberHealthFile{}, false
	}
	var file memberHealthFile
	if json.Unmarshal([]byte(raw), &file) != nil || file.Days == nil {
		return memberHealthFile{}, false
	}
	return file, true
}
