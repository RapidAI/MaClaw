package needledata

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ExportOptions struct {
	IncludeTypes map[string]bool
	OnlySuccess  bool
	MaxRecords   int
	HoldoutRatio float64
	Deduplicate  bool
}

func ReadEvents(paths []string) ([]Event, error) {
	var events []Event
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			files, err := filepath.Glob(filepath.Join(path, "*.jsonl"))
			if err != nil {
				return nil, err
			}
			sort.Strings(files)
			sub, err := ReadEvents(files)
			if err != nil {
				return nil, err
			}
			events = append(events, sub...)
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		sub, scanErr := readEventsFrom(f)
		closeErr := f.Close()
		if scanErr != nil {
			return nil, fmt.Errorf("%s: %w", path, scanErr)
		}
		if closeErr != nil {
			return nil, closeErr
		}
		events = append(events, sub...)
	}
	return events, nil
}

func readEventsFrom(r io.Reader) ([]Event, error) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var events []Event
	for s.Scan() {
		line := cleanJSONLLine(s.Text())
		if line == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, s.Err()
}

func EventsToTrainingRecords(events []Event, opts ExportOptions) []TrainingRecord {
	out := make([]TrainingRecord, 0, len(events))
	seen := map[string]bool{}
	for _, e := range events {
		if len(opts.IncludeTypes) > 0 && !opts.IncludeTypes[e.Type] {
			continue
		}
		if opts.OnlySuccess && (!e.Outcome.Success || e.Outcome.UserCorrected) {
			continue
		}
		if e.FinalDecision.Name == "" {
			continue
		}
		rec := eventToTrainingRecord(e)
		if opts.Deduplicate {
			key := leakageKey(rec)
			if key != "" && seen[key] {
				continue
			}
			seen[key] = true
		}
		out = append(out, rec)
		if opts.MaxRecords > 0 && len(out) >= opts.MaxRecords {
			break
		}
	}
	return out
}

func DeduplicateTrainingRecords(records []TrainingRecord) []TrainingRecord {
	out := make([]TrainingRecord, 0, len(records))
	seen := map[string]bool{}
	for _, rec := range records {
		key := leakageKey(rec)
		if key == "" {
			out = append(out, rec)
			continue
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, rec)
	}
	return out
}

func eventToTrainingRecord(e Event) TrainingRecord {
	tools := make([]ToolSpec, 0, len(e.Input.AvailableTools))
	for _, t := range e.Input.AvailableTools {
		tools = append(tools, ToolSpec{Name: t.Name, Description: t.Description, Parameters: map[string]any{"required": t.Required}})
	}
	content := e.Input.UserText
	if strings.TrimSpace(e.Input.ShortContext) != "" {
		content = e.Input.ShortContext + "\n\nUser: " + content
	}
	return TrainingRecord{
		ID:   e.EventID,
		Task: e.Type,
		Messages: []ChatMessage{
			{Role: "system", Content: promptForEventType(e.Type)},
			{Role: "user", Content: content},
		},
		Tools:    tools,
		Expected: e.FinalDecision,
	}
}

func promptForEventType(t string) string {
	switch t {
	case EventWorkflowReview:
		return reviewSystemPrompt()
	case EventMemoryExtractGate:
		return memoryGatePrompt()
	case EventSmartApproval:
		return "Classify whether the flagged tool operation is safe, unsafe, or unknown."
	default:
		return intentSystemPrompt()
	}
}

func WriteJSONL(path string, records []TrainingRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, rec := range records {
		if err := enc.Encode(rec); err != nil {
			return err
		}
	}
	return w.Flush()
}

func WriteNeedleJSONL(path string, records []TrainingRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, rec := range records {
		ex, err := TrainingRecordToNeedleExample(rec)
		if err != nil {
			return err
		}
		if err := enc.Encode(ex); err != nil {
			return err
		}
	}
	return w.Flush()
}

func SplitHoldout(records []TrainingRecord, ratio float64) (train, eval []TrainingRecord) {
	if ratio <= 0 || ratio >= 1 || len(records) < 2 {
		return records, nil
	}
	groups := map[string][]TrainingRecord{}
	var order []string
	for _, rec := range records {
		key := strings.TrimSpace(rec.Task) + "\x00" + strings.TrimSpace(rec.Expected.Name)
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], rec)
	}
	sort.Strings(order)
	for _, key := range order {
		items := groups[key]
		if len(items) < 2 {
			train = append(train, items...)
			continue
		}
		n := int(float64(len(items)) * ratio)
		if n < 1 {
			n = 1
		}
		if n >= len(items) {
			n = len(items) - 1
		}
		eval = append(eval, items[:n]...)
		train = append(train, items[n:]...)
	}
	if len(eval) == 0 {
		eval = append(eval, train[0])
		train = train[1:]
	}
	return train, eval
}
