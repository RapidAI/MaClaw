package llmpool

import (
	"strings"
	"testing"
	"time"
)

func TestTokenBankShareWindowDefaultsToAlways(t *testing.T) {
	if !TokenBankShareWindowAllows(TokenBankShareWindow{}, time.Now()) {
		t.Fatal("an empty window stays available")
	}
	full := &TokenBankShareWindow{Days: []int{0, 1, 2, 3, 4, 5, 6}, Start: "00:00", End: "24:00"}
	stored, err := MarshalTokenBankShareWindow(full)
	if err != nil || stored != "" {
		t.Fatalf("every day 00:00-24:00 stores as always, got %q err=%v", stored, err)
	}
	if !ParseTokenBankShareWindow("").Always() {
		t.Fatal("blank storage is always available")
	}
}

func TestTokenBankShareWindowUsesBeijingCheapHours(t *testing.T) {
	loc := loadCreditMultiplierLocation(DefaultCreditMultiplierTimezone)
	fridayNight := time.Date(2026, 10, 2, 23, 0, 0, 0, loc)
	saturdayMorning := time.Date(2026, 10, 3, 3, 0, 0, 0, loc)
	saturdayNoon := time.Date(2026, 10, 3, 12, 0, 0, 0, loc)
	mondayMorning := time.Date(2026, 10, 5, 3, 0, 0, 0, loc)
	mondayNight := time.Date(2026, 10, 5, 23, 0, 0, 0, loc)
	if fridayNight.Weekday() != time.Friday || mondayMorning.Weekday() != time.Monday {
		t.Fatalf("fixture weekdays friday=%s monday=%s", fridayNight.Weekday(), mondayMorning.Weekday())
	}
	window := TokenBankShareWindow{Days: []int{1, 2, 3, 4, 5}, Start: "22:00", End: "08:00"}
	if !TokenBankShareWindowAllows(window, fridayNight) {
		t.Fatal("Friday 23:00 Beijing is inside a weekday night window")
	}
	if !TokenBankShareWindowAllows(window, saturdayMorning) {
		t.Fatal("Saturday 03:00 Beijing still belongs to Friday night")
	}
	if TokenBankShareWindowAllows(window, saturdayNoon) {
		t.Fatal("Saturday noon is outside the cheap window")
	}
	if TokenBankShareWindowAllows(window, mondayMorning) {
		t.Fatal("Monday 03:00 belongs to Sunday night, which was not checked")
	}
	if !TokenBankShareWindowAllows(window, mondayNight) {
		t.Fatal("Monday 23:00 Beijing is inside the window")
	}

	stored, err := MarshalTokenBankShareWindow(&window)
	if err != nil || !strings.Contains(stored, `"start":"22:00"`) || !strings.Contains(stored, `"end":"08:00"`) {
		t.Fatalf("stored = %q err=%v", stored, err)
	}
	parsed := ParseTokenBankShareWindow(stored)
	if !TokenBankShareWindowAllows(parsed, fridayNight) || TokenBankShareWindowAllows(parsed, saturdayNoon) {
		t.Fatal("a stored window must match the same Beijing hours")
	}
}

func TestTokenBankShareWindowRejectsAClockThatCannotBeRead(t *testing.T) {
	if _, err := MarshalTokenBankShareWindow(&TokenBankShareWindow{Start: "24:00", End: "08:00"}); err == nil {
		t.Fatal("24:00 cannot start a window")
	}
	if _, err := MarshalTokenBankShareWindow(&TokenBankShareWindow{Start: "08:00", End: "08:00"}); err == nil {
		t.Fatal("a zero-length window is rejected")
	}
	if TokenBankShareWindowAllows(ParseTokenBankShareWindow("{"), time.Now()) {
		t.Fatal("a corrupt window is not treated as always available")
	}
	day := TokenBankShareWindow{Days: []int{5}, Start: "0:00", End: "8:00"}
	normalized, ok := NormalizeTokenBankShareWindow(&day)
	if !ok || normalized.Start != "00:00" || normalized.End != "08:00" || len(normalized.Days) != 1 || normalized.Days[0] != 5 {
		t.Fatalf("normalized = %+v ok=%v", normalized, ok)
	}
}
