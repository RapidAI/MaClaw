package needledata

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"
	"strings"
)

func ReadLocalizationPredictions(path string) ([]LocalizationPrediction, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []LocalizationPrediction
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for s.Scan() {
		var row LocalizationPrediction
		if err := json.Unmarshal(s.Bytes(), &row); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, s.Err()
}

// LocalizationPrediction is one ranked localization produced by a coding agent.
type LocalizationPrediction struct {
	CaseID          string   `json:"case_id"`
	ExpectedFiles   []string `json:"expected_files"`
	ExpectedSymbols []string `json:"expected_symbols,omitempty"`
	RankedFiles     []string `json:"ranked_files"`
	RankedSymbols   []string `json:"ranked_symbols,omitempty"`
	ToolCalls       int      `json:"tool_calls,omitempty"`
	DurationMS      int64    `json:"duration_ms,omitempty"`
	IrrelevantReads int      `json:"irrelevant_reads,omitempty"`
}

// LocalizationEvalSummary exposes the metrics needed to compare harness changes.
type LocalizationEvalSummary struct {
	Total                 int     `json:"total"`
	FileHitAt1            float64 `json:"file_hit_at_1"`
	FileHitAt3            float64 `json:"file_hit_at_3"`
	FileHitAt5            float64 `json:"file_hit_at_5"`
	FunctionHitAt1        float64 `json:"function_hit_at_1"`
	FunctionHitAt3        float64 `json:"function_hit_at_3"`
	MRR                   float64 `json:"mrr"`
	MedianToolCalls       float64 `json:"median_tool_calls"`
	MedianDurationMS      float64 `json:"median_duration_ms"`
	MedianIrrelevantReads float64 `json:"median_irrelevant_reads"`
}

func EvaluateLocalizations(rows []LocalizationPrediction) LocalizationEvalSummary {
	out := LocalizationEvalSummary{Total: len(rows)}
	if len(rows) == 0 {
		return out
	}
	var f1, f3, f5, s1, s3, rr float64
	tools, durations, reads := make([]int64, 0, len(rows)), make([]int64, 0, len(rows)), make([]int64, 0, len(rows))
	for _, row := range rows {
		if hitAt(row.RankedFiles, row.ExpectedFiles, 1) {
			f1++
		}
		if hitAt(row.RankedFiles, row.ExpectedFiles, 3) {
			f3++
		}
		if hitAt(row.RankedFiles, row.ExpectedFiles, 5) {
			f5++
		}
		if hitAt(row.RankedSymbols, row.ExpectedSymbols, 1) {
			s1++
		}
		if hitAt(row.RankedSymbols, row.ExpectedSymbols, 3) {
			s3++
		}
		rr += reciprocalRank(row.RankedFiles, row.ExpectedFiles)
		tools = append(tools, int64(row.ToolCalls))
		durations = append(durations, row.DurationMS)
		reads = append(reads, int64(row.IrrelevantReads))
	}
	n := float64(len(rows))
	out.FileHitAt1, out.FileHitAt3, out.FileHitAt5 = f1/n, f3/n, f5/n
	out.FunctionHitAt1, out.FunctionHitAt3, out.MRR = s1/n, s3/n, rr/n
	out.MedianToolCalls, out.MedianDurationMS, out.MedianIrrelevantReads = median(tools), median(durations), median(reads)
	return out
}

func hitAt(ranked, expected []string, k int) bool {
	if k > len(ranked) {
		k = len(ranked)
	}
	for _, got := range ranked[:k] {
		if containsNormalized(expected, got) {
			return true
		}
	}
	return false
}

func reciprocalRank(ranked, expected []string) float64 {
	for i, got := range ranked {
		if containsNormalized(expected, got) {
			return 1 / float64(i+1)
		}
	}
	return 0
}

func containsNormalized(values []string, target string) bool {
	target = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(target), "\\", "/"))
	for _, v := range values {
		if strings.ToLower(strings.ReplaceAll(strings.TrimSpace(v), "\\", "/")) == target {
			return true
		}
	}
	return false
}

func median(values []int64) float64 {
	if len(values) == 0 {
		return 0
	}
	v := append([]int64(nil), values...)
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	m := len(v) / 2
	if len(v)%2 == 1 {
		return float64(v[m])
	}
	return float64(v[m-1]+v[m]) / 2
}
