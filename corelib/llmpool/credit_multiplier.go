package llmpool

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "time/tzdata" // keep Asia/Shanghai and other IANA zones available on Windows
)

// BillingPolicy returns the vendor time-of-use policy for this provider.
func (p ProviderConfig) BillingPolicy() ProviderBillingPolicy {
	return ProviderBillingPolicy{
		ProviderID:               strings.TrimSpace(p.ID),
		Timezone:                 p.Timezone,
		CreditMultiplier:         p.CreditMultiplier,
		CreditMultiplierSchedule: append([]CreditMultiplierWindow(nil), p.CreditMultiplierSchedule...),
		Paused:                   p.Paused,
	}
}

// ResolveCreditMultiplier returns the vendor multiplier in effect at startedAt.
// Unmatched windows fall back to CreditMultiplier, which defaults to 1.
// The first matching window in list order wins.
func ResolveCreditMultiplier(policy ProviderBillingPolicy, startedAt time.Time) float64 {
	policy = NormalizeProviderBillingPolicy(policy)
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	local := startedAt.In(loadCreditMultiplierLocation(policy.Timezone))
	weekday := int(local.Weekday())
	minutes := local.Hour()*60 + local.Minute()
	for _, window := range policy.CreditMultiplierSchedule {
		if creditMultiplierWindowMatches(window, weekday, minutes) {
			return NormalizeCreditMultiplier(window.Multiplier)
		}
	}
	return NormalizeCreditMultiplier(policy.CreditMultiplier)
}

// CombineCreditMultipliers multiplies vendor time-of-use rate by a route markup.
func CombineCreditMultipliers(vendor, route float64) float64 {
	return NormalizeCreditMultiplier(NormalizeCreditMultiplier(vendor) * NormalizeCreditMultiplier(route))
}

// NormalizeCreditMultiplier treats non-positive, NaN, and Inf values as 1.
func NormalizeCreditMultiplier(v float64) float64 {
	if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 1
	}
	return v
}

// NormalizeProviderBillingPolicy fills defaults and drops invalid windows.
func NormalizeProviderBillingPolicy(policy ProviderBillingPolicy) ProviderBillingPolicy {
	policy.ProviderID = strings.TrimSpace(policy.ProviderID)
	policy.Timezone = strings.TrimSpace(policy.Timezone)
	if policy.Timezone == "" {
		policy.Timezone = DefaultCreditMultiplierTimezone
	}
	policy.CreditMultiplier = NormalizeCreditMultiplier(policy.CreditMultiplier)
	if len(policy.CreditMultiplierSchedule) == 0 {
		return policy
	}
	windows := make([]CreditMultiplierWindow, 0, len(policy.CreditMultiplierSchedule))
	for _, window := range policy.CreditMultiplierSchedule {
		normalized, ok := normalizeCreditMultiplierWindow(window)
		if !ok {
			continue
		}
		windows = append(windows, normalized)
	}
	policy.CreditMultiplierSchedule = windows
	return policy
}

func (p *ProviderConfig) NormalizeBilling() {
	if p == nil {
		return
	}
	policy := NormalizeProviderBillingPolicy(p.BillingPolicy())
	p.Timezone = policy.Timezone
	p.CreditMultiplier = policy.CreditMultiplier
	p.CreditMultiplierSchedule = policy.CreditMultiplierSchedule
}

var creditMultiplierLocations sync.Map // string -> *time.Location

func loadCreditMultiplierLocation(name string) *time.Location {
	name = strings.TrimSpace(name)
	if name == "" {
		name = DefaultCreditMultiplierTimezone
	}
	if cached, ok := creditMultiplierLocations.Load(name); ok {
		if loc, _ := cached.(*time.Location); loc != nil {
			return loc
		}
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		if name != DefaultCreditMultiplierTimezone {
			loc = loadCreditMultiplierLocation(DefaultCreditMultiplierTimezone)
		} else {
			loc = time.UTC
		}
	}
	creditMultiplierLocations.Store(name, loc)
	return loc
}

func normalizeCreditMultiplierWindow(window CreditMultiplierWindow) (CreditMultiplierWindow, bool) {
	start, ok := parseClockMinutes(window.Start)
	if !ok {
		return CreditMultiplierWindow{}, false
	}
	end, ok := parseClockMinutes(window.End)
	if !ok {
		return CreditMultiplierWindow{}, false
	}
	if start == end {
		return CreditMultiplierWindow{}, false
	}
	hadDays := len(window.Days) > 0
	days := uniqueWeekdays(window.Days)
	if hadDays && len(days) == 0 {
		return CreditMultiplierWindow{}, false
	}
	return CreditMultiplierWindow{
		Days:       days,
		Start:      formatClockMinutes(start),
		End:        formatClockMinutes(end),
		Multiplier: NormalizeCreditMultiplier(window.Multiplier),
	}, true
}

func creditMultiplierWindowMatches(window CreditMultiplierWindow, weekday, minutes int) bool {
	start, ok := parseClockMinutes(window.Start)
	if !ok {
		return false
	}
	end, ok := parseClockMinutes(window.End)
	if !ok || start == end {
		return false
	}
	if start < end {
		return weekdayMatches(window.Days, weekday) && minutes >= start && minutes < end
	}
	// Overnight windows: evening of a listed day, plus early hours that still
	// belong to that night (previous listed day) or to a listed calendar day.
	if minutes >= start {
		return weekdayMatches(window.Days, weekday)
	}
	if minutes < end {
		prev := (weekday + 6) % 7
		return weekdayMatches(window.Days, weekday) || weekdayMatches(window.Days, prev)
	}
	return false
}

func weekdayMatches(days []int, weekday int) bool {
	if len(days) == 0 {
		return true
	}
	for _, day := range days {
		if day == weekday {
			return true
		}
	}
	return false
}

func uniqueWeekdays(days []int) []int {
	if len(days) == 0 {
		return nil
	}
	seen := map[int]struct{}{}
	out := make([]int, 0, len(days))
	for _, day := range days {
		if day < 0 || day > 6 {
			continue
		}
		if _, ok := seen[day]; ok {
			continue
		}
		seen[day] = struct{}{}
		out = append(out, day)
	}
	return out
}

func parseClockMinutes(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	parts := strings.Split(raw, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	hour, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || hour < 0 || hour > 23 {
		return 0, false
	}
	minute, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || minute < 0 || minute > 59 {
		return 0, false
	}
	return hour*60 + minute, true
}

func formatClockMinutes(minutes int) string {
	if minutes < 0 {
		minutes = 0
	}
	if minutes > 23*60+59 {
		minutes = 23*60 + 59
	}
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

// ParseCreditMultiplierHeader reads X-MaClaw-Credit-Multiplier.
func ParseCreditMultiplierHeader(raw string) float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}

func FormatCreditMultiplierHeader(value float64) string {
	value = NormalizeCreditMultiplier(value)
	formatted := strconv.FormatFloat(value, 'f', 6, 64)
	formatted = strings.TrimRight(strings.TrimRight(formatted, "0"), ".")
	if formatted == "" {
		return "1"
	}
	return formatted
}

// ProviderBillingPolicies copies billing rules from configured providers.
func ProviderBillingPolicies(providers []ProviderConfig) []ProviderBillingPolicy {
	if len(providers) == 0 {
		return nil
	}
	out := make([]ProviderBillingPolicy, 0, len(providers))
	for _, provider := range providers {
		policy := NormalizeProviderBillingPolicy(provider.BillingPolicy())
		if strings.TrimSpace(policy.ProviderID) == "" {
			continue
		}
		out = append(out, policy)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// FindProviderBillingPolicy returns the policy for providerID, or empty.
func FindProviderBillingPolicy(policies []ProviderBillingPolicy, providerID string) (ProviderBillingPolicy, bool) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return ProviderBillingPolicy{}, false
	}
	for _, policy := range policies {
		if strings.EqualFold(strings.TrimSpace(policy.ProviderID), providerID) {
			return NormalizeProviderBillingPolicy(policy), true
		}
	}
	return ProviderBillingPolicy{}, false
}
