package compute

import (
	"context"
	"log"
	"time"
)

// CostCron runs periodic cost summary generation at the Center level.
type CostCron struct {
	engine *CostEngine
	stop   chan struct{}
}

// NewCostCron creates a new CostCron scheduler.
func NewCostCron(engine *CostEngine) *CostCron {
	return &CostCron{engine: engine, stop: make(chan struct{})}
}

// Start begins the background cron loop.
// Daily summaries at 00:05 local time, monthly on the 1st at 00:05.
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
			// Use local time for Center.
			if now.Hour() == 0 && now.Minute() >= 5 && now.Minute() < 6 {
				dayKey := now.Format("2006-01-02")
				if dayKey != lastDailyRun {
					lastDailyRun = dayKey
					yesterday := now.AddDate(0, 0, -1)
					c.runWithTimeout(5*time.Minute, func(ctx context.Context) {
						if err := c.engine.GenerateDailySummary(ctx, yesterday); err != nil {
							log.Printf("[center-cost-cron] daily summary error: %v", err)
						}
					})
				}
			}
			if now.Day() == 1 && now.Hour() == 0 && now.Minute() >= 5 && now.Minute() < 6 {
				monthKey := now.Format("2006-01")
				if monthKey != lastMonthlyRun {
					lastMonthlyRun = monthKey
					lastMonth := now.AddDate(0, -1, 0)
					c.runWithTimeout(10*time.Minute, func(ctx context.Context) {
						if err := c.engine.GenerateMonthlySummary(ctx, lastMonth); err != nil {
							log.Printf("[center-cost-cron] monthly summary error: %v", err)
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
