package compute

import (
	"context"
	"log"
	"time"
)

// CostCron runs periodic cost summary generation.
type CostCron struct {
	engine *CostEngine
	stop   chan struct{}
}

// NewCostCron creates a new CostCron scheduler.
func NewCostCron(engine *CostEngine) *CostCron {
	return &CostCron{engine: engine, stop: make(chan struct{})}
}

// Start begins the background cron loop. It checks every minute whether it's
// time to generate daily (00:05 UTC) or monthly (1st of month, 00:05 UTC)
// summaries.
func (c *CostCron) Start() {
	go c.run()
}

// Stop signals the cron loop to exit.
func (c *CostCron) Stop() {
	close(c.stop)
}

func (c *CostCron) run() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	var lastDailyRun, lastMonthlyRun string

	for {
		select {
		case <-c.stop:
			return
		case now := <-ticker.C:
			utcNow := now.UTC()
			// Daily summary at 00:05 UTC.
			if utcNow.Hour() == 0 && utcNow.Minute() >= 5 && utcNow.Minute() < 6 {
				dayKey := utcNow.Format("2006-01-02")
				if dayKey != lastDailyRun {
					lastDailyRun = dayKey
					yesterday := utcNow.AddDate(0, 0, -1)
					c.runWithTimeout(5*time.Minute, func(ctx context.Context) {
						if err := c.engine.GenerateDailySummary(ctx, yesterday); err != nil {
							log.Printf("[cost-cron] daily summary error: %v", err)
						} else {
							log.Printf("[cost-cron] daily summary generated for %s", yesterday.Format("2006-01-02"))
						}
					})
				}
			}
			// Monthly summary on the 1st at 00:05 UTC.
			if utcNow.Day() == 1 && utcNow.Hour() == 0 && utcNow.Minute() >= 5 && utcNow.Minute() < 6 {
				monthKey := utcNow.Format("2006-01")
				if monthKey != lastMonthlyRun {
					lastMonthlyRun = monthKey
					lastMonth := utcNow.AddDate(0, -1, 0)
					c.runWithTimeout(10*time.Minute, func(ctx context.Context) {
						if err := c.engine.GenerateMonthlySummary(ctx, lastMonth); err != nil {
							log.Printf("[cost-cron] monthly summary error: %v", err)
						} else {
							log.Printf("[cost-cron] monthly summary generated for %s", lastMonth.Format("2006-01"))
						}
					})
				}
			}
		}
	}
}

// runWithTimeout executes fn with a context that has the given timeout.
// The context is always cancelled when fn returns, preventing resource leaks.
func (c *CostCron) runWithTimeout(timeout time.Duration, fn func(ctx context.Context)) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	fn(ctx)
}
