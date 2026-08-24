package llmpool

import (
	"math"
	"testing"
	"time"
)

func TestResolveCreditMultiplierDefaultsToOne(t *testing.T) {
	got := ResolveCreditMultiplier(ProviderBillingPolicy{}, time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC))
	if got != 1 {
		t.Fatalf("got %v, want 1", got)
	}
}

func TestResolveCreditMultiplierUsesDefaultWhenNoWindowMatches(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	policy := ProviderBillingPolicy{
		Timezone:         "Asia/Shanghai",
		CreditMultiplier: 1,
		CreditMultiplierSchedule: []CreditMultiplierWindow{{
			Days:       []int{1, 2, 3, 4, 5},
			Start:      "00:30",
			End:        "08:30",
			Multiplier: 0.5,
		}},
	}
	// Sunday 12:00 Shanghai is outside the weekday off-peak window.
	started := time.Date(2026, 8, 16, 12, 0, 0, 0, loc)
	if got := ResolveCreditMultiplier(policy, started); got != 1 {
		t.Fatalf("got %v, want 1", got)
	}
}

func TestResolveCreditMultiplierMatchesWeekdayOffPeak(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	policy := ProviderBillingPolicy{
		Timezone:         "Asia/Shanghai",
		CreditMultiplier: 1,
		CreditMultiplierSchedule: []CreditMultiplierWindow{{
			Days:       []int{1, 2, 3, 4, 5},
			Start:      "00:30",
			End:        "08:30",
			Multiplier: 0.5,
		}},
	}
	// Monday 02:00 Shanghai.
	started := time.Date(2026, 8, 17, 2, 0, 0, 0, loc)
	if got := ResolveCreditMultiplier(policy, started); got != 0.5 {
		t.Fatalf("got %v, want 0.5", got)
	}
}

func TestResolveCreditMultiplierUsesRequestStartAcrossOvernightWindow(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	policy := ProviderBillingPolicy{
		CreditMultiplierSchedule: []CreditMultiplierWindow{{
			Start:      "22:00",
			End:        "08:00",
			Multiplier: 0.4,
		}},
	}
	started := time.Date(2026, 8, 16, 23, 15, 0, 0, loc)
	if got := ResolveCreditMultiplier(policy, started); got != 0.4 {
		t.Fatalf("overnight 23:15 got %v, want 0.4", got)
	}
	started = time.Date(2026, 8, 17, 7, 59, 0, 0, loc)
	if got := ResolveCreditMultiplier(policy, started); got != 0.4 {
		t.Fatalf("overnight 07:59 got %v, want 0.4", got)
	}
	started = time.Date(2026, 8, 17, 8, 0, 0, 0, loc)
	if got := ResolveCreditMultiplier(policy, started); got != 1 {
		t.Fatalf("overnight end exclusive 08:00 got %v, want 1", got)
	}
}

func TestResolveCreditMultiplierOvernightWeekdaysSpansFridayNight(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	policy := ProviderBillingPolicy{
		Timezone:         "Asia/Shanghai",
		CreditMultiplier: 1,
		CreditMultiplierSchedule: []CreditMultiplierWindow{{
			Days:       []int{1, 2, 3, 4, 5},
			Start:      "22:00",
			End:        "08:00",
			Multiplier: 0.4,
		}},
	}
	// Friday 23:00 and Saturday 01:00 (continuation of Friday night) match.
	fridayNight := time.Date(2026, 8, 21, 23, 0, 0, 0, loc)
	if got := ResolveCreditMultiplier(policy, fridayNight); got != 0.4 {
		t.Fatalf("Friday 23:00 got %v, want 0.4", got)
	}
	saturdayMorning := time.Date(2026, 8, 22, 1, 0, 0, 0, loc)
	if got := ResolveCreditMultiplier(policy, saturdayMorning); got != 0.4 {
		t.Fatalf("Saturday 01:00 from Friday night got %v, want 0.4", got)
	}
	// Monday 01:00 is a listed weekday morning.
	mondayMorning := time.Date(2026, 8, 17, 1, 0, 0, 0, loc)
	if got := ResolveCreditMultiplier(policy, mondayMorning); got != 0.4 {
		t.Fatalf("Monday 01:00 got %v, want 0.4", got)
	}
	// Sunday 01:00 is not a weekday morning and did not start Friday/Saturday night.
	sundayMorning := time.Date(2026, 8, 16, 1, 0, 0, 0, loc)
	if got := ResolveCreditMultiplier(policy, sundayMorning); got != 1 {
		t.Fatalf("Sunday 01:00 got %v, want 1", got)
	}
	saturdayEvening := time.Date(2026, 8, 22, 23, 0, 0, 0, loc)
	if got := ResolveCreditMultiplier(policy, saturdayEvening); got != 1 {
		t.Fatalf("Saturday 23:00 got %v, want 1", got)
	}
}

func TestNormalizeProviderBillingPolicyDropsWindowsWithOnlyInvalidDays(t *testing.T) {
	policy := NormalizeProviderBillingPolicy(ProviderBillingPolicy{
		CreditMultiplierSchedule: []CreditMultiplierWindow{{
			Days:       []int{99, -1},
			Start:      "00:30",
			End:        "08:30",
			Multiplier: 0.5,
		}},
	})
	if len(policy.CreditMultiplierSchedule) != 0 {
		t.Fatalf("invalid days should drop window, got %#v", policy.CreditMultiplierSchedule)
	}
}

func TestResolveCreditMultiplierFirstMatchingWindowWins(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	policy := ProviderBillingPolicy{
		CreditMultiplierSchedule: []CreditMultiplierWindow{
			{Days: []int{1}, Start: "00:00", End: "23:59", Multiplier: 2},
			{Days: []int{1, 2, 3, 4, 5}, Start: "00:00", End: "23:59", Multiplier: 3},
		},
	}
	started := time.Date(2026, 8, 17, 10, 0, 0, 0, loc) // Monday
	if got := ResolveCreditMultiplier(policy, started); got != 2 {
		t.Fatalf("got %v, want first window 2", got)
	}
}

func TestCombineCreditMultipliers(t *testing.T) {
	if got := CombineCreditMultipliers(0.5, 2); got != 1 {
		t.Fatalf("got %v, want 1", got)
	}
	if got := CombineCreditMultipliers(0, 0); got != 1 {
		t.Fatalf("zero inputs got %v, want 1", got)
	}
}

func TestNormalizeCreditMultiplierRejectsNonFinite(t *testing.T) {
	if got := NormalizeCreditMultiplier(math.NaN()); got != 1 {
		t.Fatalf("NaN got %v, want 1", got)
	}
	if got := NormalizeCreditMultiplier(math.Inf(1)); got != 1 {
		t.Fatalf("+Inf got %v, want 1", got)
	}
	if got := NormalizeCreditMultiplier(math.Inf(-1)); got != 1 {
		t.Fatalf("-Inf got %v, want 1", got)
	}
}

func TestParseAndFormatCreditMultiplierHeader(t *testing.T) {
	if got := ParseCreditMultiplierHeader("0.5"); got != 0.5 {
		t.Fatalf("parse 0.5 = %v", got)
	}
	if got := ParseCreditMultiplierHeader(""); got != 0 {
		t.Fatalf("empty parse = %v, want 0", got)
	}
	if got := ParseCreditMultiplierHeader("Inf"); got != 0 {
		t.Fatalf("parse Inf = %v, want 0", got)
	}
	if got := ParseCreditMultiplierHeader("NaN"); got != 0 {
		t.Fatalf("parse NaN = %v, want 0", got)
	}
	if got := FormatCreditMultiplierHeader(0.5); got != "0.5" {
		t.Fatalf("format 0.5 = %q", got)
	}
	if got := FormatCreditMultiplierHeader(math.Inf(1)); got != "1" {
		t.Fatalf("format Inf = %q, want 1", got)
	}
}

func TestProviderBillingPoliciesSkipsEmptyIDs(t *testing.T) {
	got := ProviderBillingPolicies([]ProviderConfig{
		{Name: "no-id", CreditMultiplier: 2},
		{ID: "deepseek", Timezone: "Asia/Shanghai", CreditMultiplier: 1, CreditMultiplierSchedule: []CreditMultiplierWindow{{
			Days: []int{1, 2, 3, 4, 5}, Start: "00:30", End: "08:30", Multiplier: 0.5,
		}}},
	})
	if len(got) != 1 || got[0].ProviderID != "deepseek" {
		t.Fatalf("policies = %#v, want deepseek only", got)
	}
	if got[0].CreditMultiplierSchedule[0].Start != "00:30" {
		t.Fatalf("normalized start = %q", got[0].CreditMultiplierSchedule[0].Start)
	}
}

func TestProviderBillingPoliciesCopiesPaused(t *testing.T) {
	got := ProviderBillingPolicies([]ProviderConfig{
		{ID: "deepseek", Paused: true, CreditMultiplier: 2},
		{ID: "yinyu", CreditMultiplier: 0.8},
	})
	if len(got) != 2 {
		t.Fatalf("policies = %#v", got)
	}
	if !got[0].Paused || got[0].ProviderID != "deepseek" {
		t.Fatalf("paused policy = %#v", got[0])
	}
	if got[1].Paused || got[1].ProviderID != "yinyu" {
		t.Fatalf("live policy = %#v", got[1])
	}
}
