package needledata

import "strings"

type ThresholdPoint struct {
	Threshold     float64 `json:"threshold"`
	Total         int     `json:"total"`
	Accepted      int     `json:"accepted"`
	Matched       int     `json:"matched"`
	Accuracy      float64 `json:"accuracy"`
	AcceptedRatio float64 `json:"accepted_ratio"`
	Score         float64 `json:"score"`
}

type TaskThresholdCalibration struct {
	Task             string           `json:"task"`
	Thresholds       []ThresholdPoint `json:"thresholds"`
	Recommended      ThresholdPoint   `json:"recommended"`
	RecommendedFound bool             `json:"recommended_found"`
}

func CalibrateThresholds(records []TrainingRecord, predictions map[string]PredictionRecord, thresholds []float64) []ThresholdPoint {
	if len(thresholds) == 0 {
		thresholds = DefaultThresholds()
	}
	out := make([]ThresholdPoint, 0, len(thresholds))
	for _, threshold := range thresholds {
		point := ThresholdPoint{Threshold: threshold, Total: len(records)}
		for _, rec := range records {
			pred, ok := predictions[rec.ID]
			if !ok || strings.TrimSpace(pred.Decision.Name) == "" || pred.Decision.Confidence < threshold {
				continue
			}
			point.Accepted++
			if normalizeDecisionName(rec.Expected.Name) == normalizeDecisionName(pred.Decision.Name) {
				point.Matched++
			}
		}
		point.Accuracy = ratio(point.Matched, point.Accepted)
		point.AcceptedRatio = ratio(point.Accepted, point.Total)
		point.Score = point.Accuracy * point.AcceptedRatio
		out = append(out, point)
	}
	return out
}

func DefaultThresholds() []float64 {
	return []float64{0.50, 0.55, 0.60, 0.65, 0.70, 0.75, 0.78, 0.80, 0.85, 0.90, 0.95}
}

func CalibrateThresholdsByTask(records []TrainingRecord, predictions map[string]PredictionRecord, thresholds []float64, minAccuracy, minAcceptedRatio float64) map[string]TaskThresholdCalibration {
	groups := map[string][]TrainingRecord{}
	for _, rec := range records {
		task := strings.TrimSpace(rec.Task)
		if task == "" {
			task = "unknown"
		}
		groups[task] = append(groups[task], rec)
	}
	out := make(map[string]TaskThresholdCalibration, len(groups))
	for task, group := range groups {
		points := CalibrateThresholds(group, predictions, thresholds)
		best, ok := BestThreshold(points, minAccuracy, minAcceptedRatio)
		out[task] = TaskThresholdCalibration{Task: task, Thresholds: points, Recommended: best, RecommendedFound: ok}
	}
	return out
}

func BestThreshold(points []ThresholdPoint, minAccuracy, minAcceptedRatio float64) (ThresholdPoint, bool) {
	var best ThresholdPoint
	found := false
	for _, point := range points {
		if point.Accepted == 0 || point.Accuracy < minAccuracy || point.AcceptedRatio < minAcceptedRatio {
			continue
		}
		if !found || point.Score > best.Score || (point.Score == best.Score && point.AcceptedRatio > best.AcceptedRatio) {
			best = point
			found = true
		}
	}
	return best, found
}
