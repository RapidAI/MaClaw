package corelib

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// DecimalInt64String stores an int64 as a decimal string so JSON/JS round-trips
// preserve full precision (e.g. large Telegram chat ids).
//
// Unmarshal accepts either a JSON number or a string. Marshal always emits a
// string when non-empty (empty/zero → empty string, omitempty-friendly).
type DecimalInt64String string

// Int64 parses the decimal string. Invalid or empty values return 0.
func (d DecimalInt64String) Int64() int64 {
	s := strings.TrimSpace(string(d))
	if s == "" || s == "0" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// String returns the trimmed decimal form (empty when zero/unset).
func (d DecimalInt64String) String() string {
	s := strings.TrimSpace(string(d))
	if s == "" || s == "0" {
		return ""
	}
	return s
}

// SetInt64 stores n (0 clears).
func (d *DecimalInt64String) SetInt64(n int64) {
	if d == nil {
		return
	}
	if n == 0 {
		*d = ""
		return
	}
	*d = DecimalInt64String(strconv.FormatInt(n, 10))
}

// SetString validates and stores a decimal int64 string (empty/0 clears).
// Leading zeros and signs are normalized (e.g. "007" → "7", "-01" → "-1").
func (d *DecimalInt64String) SetString(s string) error {
	if d == nil {
		return fmt.Errorf("nil DecimalInt64String")
	}
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		*d = ""
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid int64 decimal %q: %w", s, err)
	}
	d.SetInt64(n)
	return nil
}

func (d *DecimalInt64String) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*d = ""
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		return d.SetString(s)
	}
	// JSON number (legacy configs written as int64).
	var n int64
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("telegram chat id: %w", err)
	}
	d.SetInt64(n)
	return nil
}

func (d DecimalInt64String) MarshalJSON() ([]byte, error) {
	s := d.String()
	if s == "" {
		return []byte(`""`), nil
	}
	return json.Marshal(s)
}
