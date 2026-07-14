package moa

// Conservative daily-budget precheck for one MoA wave (PR3).
//
// We do not know exact model prices at fan-out time for every provider, so
// estimates use a mid-tier floor sized for typical advisor short answers
// (reference_max_tokens ~600) plus one aggregator round.

// Estimate tokens per reference call (input context slice + short answer).
const (
	estRefInputTokens  = 4000
	estRefOutputTokens = 600
	estAggInputTokens  = 6000
	estAggOutputTokens = 1500
	// Mid-tier USD per 1M tokens (between mini and flagship).
	estInputPerM  = 1.0
	estOutputPerM = 5.0
)

// EstimateWaveMinUSD returns a conservative USD floor for one fan-out wave:
// nRefs advisor calls + 1 aggregator call. nRefs <= 0 yields aggregator-only.
func EstimateWaveMinUSD(nRefs int) float64 {
	if nRefs < 0 {
		nRefs = 0
	}
	ref := costUSD(estRefInputTokens, estRefOutputTokens) * float64(nRefs)
	agg := costUSD(estAggInputTokens, estAggOutputTokens)
	return ref + agg
}

func costUSD(inTok, outTok int) float64 {
	return float64(inTok)/1_000_000*estInputPerM + float64(outTok)/1_000_000*estOutputPerM
}
