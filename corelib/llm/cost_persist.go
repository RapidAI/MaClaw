package llm

// Durable daily LLM cost under ~/.maclaw/stats/llm_cost_daily.json.
// Multi-process safe at the instance level: each host-pid writes its own slot;
// fleet/CLI totals sum instances for today.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

// CostDailyStatsPath is the durable daily cost file.
func CostDailyStatsPath() string {
	return filepath.Join(maclawpath.DataDir(), "stats", "llm_cost_daily.json")
}

// ModelCostBucket is spend for one model id within a day/process.
type ModelCostBucket struct {
	CostUSD      float64 `json:"cost_usd"`
	Calls        int     `json:"calls"`
	InputTokens  int64   `json:"input_tokens,omitempty"`
	OutputTokens int64   `json:"output_tokens,omitempty"`
}

// CostInstanceSnapshot is one process's contribution for a calendar day.
type CostInstanceSnapshot struct {
	CostUSD      float64                    `json:"cost_usd"`
	Calls        int                        `json:"calls"`
	InputTokens  int64                      `json:"input_tokens,omitempty"`
	OutputTokens int64                      `json:"output_tokens,omitempty"`
	ByModel      map[string]ModelCostBucket `json:"by_model,omitempty"`
	UpdatedAt    string                     `json:"updated_at,omitempty"`
}

// CostDailyDiskFile is the on-disk shape.
type CostDailyDiskFile struct {
	Date      string                          `json:"date"` // YYYY-MM-DD
	Instances map[string]CostInstanceSnapshot `json:"instances,omitempty"`
	UpdatedAt string                          `json:"updated_at,omitempty"`
}

// CostDailyFleetView is the summed view for CLI / doctor.
type CostDailyFleetView struct {
	Date         string                           `json:"date"`
	CostUSD      float64                          `json:"cost_usd"`
	Calls        int                              `json:"calls"`
	InputTokens  int64                            `json:"input_tokens"`
	OutputTokens int64                            `json:"output_tokens"`
	Instances    int                              `json:"instances"`
	ByInstance   map[string]CostInstanceSnapshot  `json:"by_instance,omitempty"`
	ByModel      map[string]ModelCostBucket       `json:"by_model,omitempty"`
	Path         string                           `json:"path,omitempty"`
}

var (
	costInstanceIDOnce sync.Once
	costInstanceID     string

	// Debounce state for this process's daily cost slot.
	dailyDebounceMu   sync.Mutex
	dailyPersistTimer *time.Timer
	dailyPersistSnap  dailyPersistSnapshot
	dailyPersistDirty bool
	// Serialize RMW of llm_cost_daily.json within this process.
	dailyFileMu sync.Mutex
)

const dailyPersistDebounce = 800 * time.Millisecond

type dailyPersistSnapshot struct {
	gen     uint64
	date    string
	cost    float64
	calls   int
	inTok   int64
	outTok  int64
	byModel map[string]ModelCostBucket
}

func costProcessInstanceID() string {
	costInstanceIDOnce.Do(func() {
		host, _ := os.Hostname()
		if host == "" {
			host = "host"
		}
		costInstanceID = fmt.Sprintf("%s-%d", host, os.Getpid())
	})
	return costInstanceID
}

// LoadCostDailyFleet reads and sums today's durable cost across instances.
// Includes this process's pending debounced snapshot so CLI/doctor/heartbeat
// see live totals without waiting for the debounce timer.
func LoadCostDailyFleet() CostDailyFleetView {
	path := CostDailyStatsPath()
	today := time.Now().Format("2006-01-02")
	view := CostDailyFleetView{
		Date: today,
		Path: path,
	}
	data, err := os.ReadFile(path)
	if err == nil && len(data) > 0 {
		var file CostDailyDiskFile
		if json.Unmarshal(data, &file) == nil && file.Date == today {
			view.Date = file.Date
			if file.Instances != nil {
				view.ByInstance = file.Instances
			}
		}
	}
	if view.ByInstance == nil {
		view.ByInstance = make(map[string]CostInstanceSnapshot)
	}
	// Overlay pending in-process snap (absolute totals for this host-pid).
	dailyDebounceMu.Lock()
	if dailyPersistDirty && dailyPersistSnap.date == today {
		id := costProcessInstanceID()
		view.ByInstance[id] = CostInstanceSnapshot{
			CostUSD:      dailyPersistSnap.cost,
			Calls:        dailyPersistSnap.calls,
			InputTokens:  dailyPersistSnap.inTok,
			OutputTokens: dailyPersistSnap.outTok,
			ByModel:      copyModelBuckets(dailyPersistSnap.byModel),
		}
	}
	dailyDebounceMu.Unlock()

	view.ByModel = make(map[string]ModelCostBucket)
	for _, inst := range view.ByInstance {
		view.CostUSD += inst.CostUSD
		view.Calls += inst.Calls
		view.InputTokens += inst.InputTokens
		view.OutputTokens += inst.OutputTokens
		view.Instances++
		for model, b := range inst.ByModel {
			cur := view.ByModel[model]
			cur.CostUSD += b.CostUSD
			cur.Calls += b.Calls
			cur.InputTokens += b.InputTokens
			cur.OutputTokens += b.OutputTokens
			view.ByModel[model] = cur
		}
	}
	if len(view.ByInstance) == 0 {
		view.ByInstance = nil
	}
	if len(view.ByModel) == 0 {
		view.ByModel = nil
	}
	return view
}

// FormatCostDailyFleetLine is a one-line operator summary.
func FormatCostDailyFleetLine() string {
	v := LoadCostDailyFleet()
	if v.Calls <= 0 && v.CostUSD <= 0 {
		return ""
	}
	return fmt.Sprintf("llm-cost today=$%.4f calls=%d instances=%d", v.CostUSD, v.Calls, v.Instances)
}

// copyModelBuckets returns a shallow copy of by-model buckets for durable write.
func copyModelBuckets(src map[string]ModelCostBucket) map[string]ModelCostBucket {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]ModelCostBucket, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// persistDailyInstance schedules a debounced write of this process's slot.
// In-memory CostTracker counters update immediately; disk lags by at most
// dailyPersistDebounce (budget gates use process-local counters first).
//
// Snapshots are applied monotonically by (gen, date, calls) so concurrent
// Record after unlock cannot let an older absolute total overwrite a newer one.
// A new CostTracker gets a new gen and always replaces the pending snap.
func persistDailyInstance(gen uint64, date string, cost float64, calls int, inTok, outTok int64, byModel map[string]ModelCostBucket) {
	dailyDebounceMu.Lock()
	if dailyPersistDirty && dailyPersistSnap.gen == gen && dailyPersistSnap.date == date && calls < dailyPersistSnap.calls {
		// Same tracker, reordered concurrent persist of an older total — drop.
		dailyDebounceMu.Unlock()
		return
	}
	dailyPersistSnap = dailyPersistSnapshot{
		gen:     gen,
		date:    date,
		cost:    cost,
		calls:   calls,
		inTok:   inTok,
		outTok:  outTok,
		byModel: copyModelBuckets(byModel),
	}
	dailyPersistDirty = true
	if dailyPersistTimer == nil {
		dailyPersistTimer = time.AfterFunc(dailyPersistDebounce, func() {
			_ = FlushDailyCostPersist()
		})
	}
	dailyDebounceMu.Unlock()
}

// FlushDailyCostPersist forces any pending debounced daily cost write now.
// Holds dailyFileMu for the whole snap→write so a stale timer cannot overwrite
// a newer flush (concurrent Record still updates memory + dirty for next flush).
func FlushDailyCostPersist() error {
	// Lock order: file → debounce (Record only takes debounce).
	dailyFileMu.Lock()
	defer dailyFileMu.Unlock()

	dailyDebounceMu.Lock()
	if dailyPersistTimer != nil {
		dailyPersistTimer.Stop()
		dailyPersistTimer = nil
	}
	if !dailyPersistDirty {
		dailyDebounceMu.Unlock()
		return nil
	}
	snap := dailyPersistSnap
	byModel := copyModelBuckets(snap.byModel)
	date, cost, calls, inTok, outTok := snap.date, snap.cost, snap.calls, snap.inTok, snap.outTok
	dailyPersistDirty = false
	dailyDebounceMu.Unlock()

	if err := writeDailyInstanceLocked(date, cost, calls, inTok, outTok, byModel); err != nil {
		dailyDebounceMu.Lock()
		if !dailyPersistDirty {
			dailyPersistSnap = dailyPersistSnapshot{
				gen: snap.gen,
				date: date, cost: cost, calls: calls, inTok: inTok, outTok: outTok,
				byModel: byModel,
			}
			dailyPersistDirty = true
		}
		if dailyPersistTimer == nil {
			dailyPersistTimer = time.AfterFunc(dailyPersistDebounce, func() {
				_ = FlushDailyCostPersist()
			})
		}
		dailyDebounceMu.Unlock()
		return err
	}
	return nil
}

// writeDailyInstanceLocked performs RMW of this process slot. Caller holds dailyFileMu.
func writeDailyInstanceLocked(date string, cost float64, calls int, inTok, outTok int64, byModel map[string]ModelCostBucket) error {
	path := CostDailyStatsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	file := CostDailyDiskFile{Date: date, Instances: map[string]CostInstanceSnapshot{}}
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		var existing CostDailyDiskFile
		if json.Unmarshal(data, &existing) == nil && existing.Date == date {
			file = existing
			if file.Instances == nil {
				file.Instances = make(map[string]CostInstanceSnapshot)
			}
		}
	}
	id := costProcessInstanceID()
	file.Date = date
	file.Instances[id] = CostInstanceSnapshot{
		CostUSD:      cost,
		Calls:        calls,
		InputTokens:  inTok,
		OutputTokens: outTok,
		ByModel:      copyModelBuckets(byModel),
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	file.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
