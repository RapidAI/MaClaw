package guiapp

import "sync/atomic"

// outboundContextUnderreportPercent is the tolerance for provider/host prompt
// differences. Below this share of the estimate the provider count is treated
// as a partial leg (uncached/input-only), not a smaller real prompt.
const outboundContextUnderreportPercent = 80

// outboundContextMeter pairs the host's pre-request estimate with the
// provider's post-response prompt count.
//
// The estimate is computed from the exact outbound conversation and tool
// definitions, so it is a lower bound for a real prompt. Some providers report
// only the uncached/incremental input leg (Devin/Windsurf does this), which can
// be a few hundred tokens after a 40k prompt. Publishing that number as the
// context-window fill collapses the meter (BR-20261125). Keep the larger of the
// two samples for the same request; a provider count that is genuinely larger
// (for example a different tokenizer) still wins.
type outboundContextMeter struct {
	estimateTokens atomic.Int64
	estimateMax    atomic.Int64
	lastFill       atomic.Int64
	// hasSample distinguishes an explicit zero-token estimate (empty request)
	// from a meter that has not observed this request yet.
	hasSample atomic.Bool
	// lastRequestResolved distinguishes a completed request sample from a
	// pending pre-request estimate. A loop result can still carry the previous
	// request's provider prompt when the final response omitted usage; the
	// newer estimate must win instead of the stale larger number.
	lastRequestResolved atomic.Bool
}

// observeEstimate records the host-side prompt size reported immediately
// before the request leaves the process. Zero is a valid sample: it resets a
// previous floor instead of leaving the old request's tokens behind.
func (m *outboundContextMeter) observeEstimate(used, max int) {
	if m == nil {
		return
	}
	if used < 0 {
		used = 0
	}
	m.hasSample.Store(true)
	m.estimateTokens.Store(int64(used))
	// Store zero too: a later request whose window could not be resolved must
	// not inherit this request's max.
	m.estimateMax.Store(int64(max))
	m.lastFill.Store(int64(used))
	m.lastRequestResolved.Store(false)
}

// providerFill reconciles the provider's prompt count with the latest
// outbound estimate. A non-positive provider count is treated as an omitted
// leg and leaves the estimate in place.
//
// The estimate stays as the request floor until the next pre-request report.
// One host turn can make several provider calls (MoA reference fan-out then
// the aggregator, or a retried attempt). Consuming the estimate on the first
// call would let the second reference's smaller prompt collapse the meter
// again. fromEstimate reports that the returned fill came from the floor
// rather than this provider payload.
func (m *outboundContextMeter) providerFill(providerTokens int) (fill, max int, fromEstimate bool) {
	if m == nil || providerTokens <= 0 {
		// A provider that omitted the prompt leg is not a sample at all. Keep
		// the pending estimate for the next real report of this request.
		return providerTokens, 0, false
	}
	estimate := int(m.estimateTokens.Load())
	estimateMax := int(m.estimateMax.Load())
	fill = providerTokens
	max = estimateMax
	if estimate > 0 && int64(providerTokens)*100 < int64(estimate)*outboundContextUnderreportPercent {
		fill = estimate
		fromEstimate = true
	}
	if fill > 0 {
		m.lastFill.Store(int64(fill))
		m.lastRequestResolved.Store(true)
	}
	return fill, max, fromEstimate
}

// lastFillTokens returns the latest sample for this loop. Before the provider
// replies it is the outbound estimate; after reconciliation it is the request
// fill.
func (m *outboundContextMeter) lastFillTokens() int {
	if m == nil {
		return 0
	}
	return int(m.lastFill.Load())
}

// preferPrompt returns the prompt that belongs to the latest request. A
// completed provider sample may be larger than the estimate (tokenizer or
// cache accounting), while a pending estimate is newer than a result that
// still carries the previous request's provider count (for example after
// compaction when the final response omitted usage).
func (m *outboundContextMeter) preferPrompt(provider int) int {
	if m == nil || !m.hasSample.Load() {
		return provider
	}
	live := m.lastFillTokens()
	if m.lastRequestResolved.Load() && provider > live {
		return provider
	}
	return live
}
