package main

import (
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// appendRuntimeMetricsPrometheus renders the shared Runtime metrics without
// exposing tenant ids. The same snapshot is available to GUI/TUI adapters via
// agentservice.RuntimeMetrics; this function is only the HTTP text adapter.
func appendRuntimeMetricsPrometheus(b *strings.Builder, snapshot agentruntime.RuntimeMetricsSnapshot) {
	if b == nil {
		return
	}
	metrics := []struct {
		name  string
		help  string
		kind  string
		value uint64
	}{
		{"maclaw_runtime_turns_admitted_total", "Admitted Agent turns", "counter", snapshot.TurnsAdmitted},
		{"maclaw_runtime_turns_succeeded_total", "Successfully completed Agent turns", "counter", snapshot.TurnsSucceeded},
		{"maclaw_runtime_turns_failed_total", "Failed Agent turns", "counter", snapshot.TurnsFailed},
		{"maclaw_runtime_turns_cancelled_total", "Cancelled Agent turns", "counter", snapshot.TurnsCancelled},
		{"maclaw_runtime_max_active_runs", "Maximum concurrently active Agent runs", "gauge", snapshot.MaxActiveRuns},
		{"maclaw_runtime_queue_wait_milliseconds_total", "Aggregate Agent admission wait in milliseconds", "counter", snapshot.QueueWaitMilliseconds},
		{"maclaw_runtime_queue_wait_samples_total", "Agent admission wait samples", "counter", snapshot.QueueWaitSamples},
		{"maclaw_runtime_first_token_latency_milliseconds_total", "Aggregate first-token latency in milliseconds", "counter", snapshot.FirstTokenMilliseconds},
		{"maclaw_runtime_first_token_latency_samples_total", "First-token latency samples", "counter", snapshot.FirstTokenSamples},
		{"maclaw_runtime_token_deltas_total", "Assistant token delta callbacks", "counter", snapshot.TokenDeltas},
		{"maclaw_runtime_tool_calls_total", "Agent tool calls", "counter", snapshot.ToolCalls},
		{"maclaw_runtime_tool_errors_total", "Agent tool errors", "counter", snapshot.ToolErrors},
		{"maclaw_runtime_events_emitted_total", "Runtime events accepted by the durable outbox", "counter", snapshot.EventsEmitted},
		{"maclaw_runtime_events_dropped_total", "Runtime events rejected by the durable outbox", "counter", snapshot.EventsDropped},
		{"maclaw_runtime_events_replayed_total", "Runtime events replayed from an existing idempotent row", "counter", snapshot.EventsReplayed},
		{"maclaw_runtime_quota_rejected_total", "Requests rejected by a tenant or user quota", "counter", snapshot.QuotaRejected},
		{"maclaw_runtime_rate_limited_total", "Agent turns rejected by the shared tenant token bucket", "counter", snapshot.RateLimited},
		{"maclaw_runtime_rate_limit_buckets_evicted_total", "Tenant token buckets evicted after the bounded cardinality limit was reached", "counter", snapshot.RateLimitBucketsEvicted},
		{"maclaw_runtime_tenant_series_dropped_total", "Tenant metric series dropped at the cardinality limit", "counter", snapshot.TenantSeriesDropped},
	}
	for _, metric := range metrics {
		b.WriteString("# HELP ")
		b.WriteString(metric.name)
		b.WriteByte(' ')
		b.WriteString(metric.help)
		b.WriteByte('\n')
		b.WriteString("# TYPE ")
		b.WriteString(metric.name)
		b.WriteByte(' ')
		b.WriteString(metric.kind)
		b.WriteByte('\n')
		b.WriteString(metric.name)
		b.WriteByte(' ')
		b.WriteString(strconv.FormatUint(metric.value, 10))
		b.WriteByte('\n')
	}
	// Active runs is a signed gauge because cancellation and shutdown may race
	// an observation; the collector itself clamps it at zero.
	b.WriteString("# HELP maclaw_runtime_active_runs Currently active Agent runs\n# TYPE maclaw_runtime_active_runs gauge\nmaclaw_runtime_active_runs ")
	b.WriteString(strconv.FormatInt(snapshot.ActiveRuns, 10))
	b.WriteByte('\n')
	for _, tenant := range snapshot.Tenants {
		hash := strings.TrimSpace(tenant.TenantHash)
		if hash == "" {
			continue
		}
		label := `{tenant_hash="` + hash + `"}`
		b.WriteString("maclaw_runtime_active_runs")
		b.WriteString(label)
		b.WriteByte(' ')
		b.WriteString(strconv.FormatInt(tenant.ActiveRuns, 10))
		b.WriteByte('\n')
		b.WriteString("maclaw_runtime_turns_admitted_total")
		b.WriteString(label)
		b.WriteByte(' ')
		b.WriteString(strconv.FormatUint(tenant.TurnsAdmitted, 10))
		b.WriteByte('\n')
		b.WriteString("maclaw_runtime_turns_succeeded_total")
		b.WriteString(label)
		b.WriteByte(' ')
		b.WriteString(strconv.FormatUint(tenant.TurnsSucceeded, 10))
		b.WriteByte('\n')
		b.WriteString("maclaw_runtime_turns_failed_total")
		b.WriteString(label)
		b.WriteByte(' ')
		b.WriteString(strconv.FormatUint(tenant.TurnsFailed, 10))
		b.WriteByte('\n')
		b.WriteString("maclaw_runtime_turns_cancelled_total")
		b.WriteString(label)
		b.WriteByte(' ')
		b.WriteString(strconv.FormatUint(tenant.TurnsCancelled, 10))
		b.WriteByte('\n')
		b.WriteString("maclaw_runtime_rate_limited_total")
		b.WriteString(label)
		b.WriteByte(' ')
		b.WriteString(strconv.FormatUint(tenant.RateLimited, 10))
		b.WriteByte('\n')
		b.WriteString("maclaw_runtime_tool_calls_total")
		b.WriteString(label)
		b.WriteByte(' ')
		b.WriteString(strconv.FormatUint(tenant.ToolCalls, 10))
		b.WriteByte('\n')
		b.WriteString("maclaw_runtime_tool_errors_total")
		b.WriteString(label)
		b.WriteByte(' ')
		b.WriteString(strconv.FormatUint(tenant.ToolErrors, 10))
		b.WriteByte('\n')
	}
}
