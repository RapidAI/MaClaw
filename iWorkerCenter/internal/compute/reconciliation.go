package compute

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ReconciliationResult holds the comparison between local and Cloud monthly totals.
type ReconciliationResult struct {
	Month              string `json:"month"`
	LocalTotalTokens   int64  `json:"local_total_tokens"`
	CloudTotalTokens   int64  `json:"cloud_total_tokens"`
	Difference         int64  `json:"difference"`          // local - cloud (signed)
	CloudDataAvailable bool   `json:"cloud_data_available"`
}

// Reconciler compares local monthly token totals with Cloud data.
type Reconciler struct {
	costEngine *CostEngine
	cloudURL   string // e.g. "https://cloud.example.com"
	centerID   string
	secret     string
	client     *http.Client
}

// NewReconciler creates a new Reconciler.
func NewReconciler(costEngine *CostEngine, cloudURL, centerID, secret string) *Reconciler {
	return &Reconciler{
		costEngine: costEngine,
		cloudURL:   cloudURL,
		centerID:   centerID,
		secret:     secret,
		client:     &http.Client{Timeout: 30 * time.Second},
	}
}

// Reconcile compares local monthly totals with Cloud for the given month (YYYY-MM).
func (r *Reconciler) Reconcile(ctx context.Context, month string) (*ReconciliationResult, error) {
	// Get local totals.
	summaries, err := r.costEngine.QueryCostSummaries(ctx, CostFilter{
		PeriodType: "monthly",
		Start:      month,
		End:        month,
	})
	if err != nil {
		return nil, fmt.Errorf("query local summaries: %w", err)
	}

	var localTotal int64
	for _, s := range summaries {
		localTotal += s.TotalTokens
	}

	// Fetch Cloud totals.
	cloudTotal, cloudAvailable := r.fetchCloudMonthlyUsage(ctx, month)

	diff := localTotal - cloudTotal

	return &ReconciliationResult{
		Month:              month,
		LocalTotalTokens:   localTotal,
		CloudTotalTokens:   cloudTotal,
		Difference:         diff,
		CloudDataAvailable: cloudAvailable,
	}, nil
}

func (r *Reconciler) fetchCloudMonthlyUsage(ctx context.Context, month string) (int64, bool) {
	if r.cloudURL == "" || r.centerID == "" {
		return 0, false
	}

	url := fmt.Sprintf("%s/api/centers/%s/monthly-usage?month=%s",
		r.cloudURL, r.centerID, month)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, false
	}
	req.Header.Set("X-Center-Secret", r.secret)

	resp, err := r.client.Do(req)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, false
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
	if err != nil {
		return 0, false
	}

	var result struct {
		TotalTokens int64 `json:"total_tokens"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, false
	}

	return result.TotalTokens, true
}
