package corelib

import (
	"encoding/json"
	"testing"
)

func TestDecimalInt64StringRoundTripString(t *testing.T) {
	var d DecimalInt64String
	if err := d.SetString("9007199254740993"); err != nil { // > MAX_SAFE_INTEGER
		t.Fatal(err)
	}
	if d.Int64() != 9007199254740993 {
		t.Fatalf("Int64=%d", d.Int64())
	}
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"9007199254740993"` {
		t.Fatalf("marshal=%s, want quoted string", data)
	}
	var d2 DecimalInt64String
	if err := json.Unmarshal(data, &d2); err != nil {
		t.Fatal(err)
	}
	if d2.String() != "9007199254740993" {
		t.Fatalf("unmarshal string=%q", d2.String())
	}
}

func TestDecimalInt64StringUnmarshalNumberLegacy(t *testing.T) {
	var d DecimalInt64String
	if err := json.Unmarshal([]byte(`12345`), &d); err != nil {
		t.Fatal(err)
	}
	if d.Int64() != 12345 {
		t.Fatalf("got %d", d.Int64())
	}
}

func TestDecimalInt64StringEmpty(t *testing.T) {
	var d DecimalInt64String
	_ = d.SetString("")
	if d.Int64() != 0 || d.String() != "" {
		t.Fatalf("empty: %+v", d)
	}
	_ = d.SetString("0")
	if d.String() != "" {
		t.Fatalf("zero should clear, got %q", d.String())
	}
	if err := d.SetString("not-a-number"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDecimalInt64StringNormalizesLeadingZeros(t *testing.T) {
	var d DecimalInt64String
	if err := d.SetString("007"); err != nil {
		t.Fatal(err)
	}
	if d.String() != "7" || d.Int64() != 7 {
		t.Fatalf("got string=%q int=%d", d.String(), d.Int64())
	}
	if err := d.SetString("-01"); err != nil {
		t.Fatal(err)
	}
	if d.String() != "-1" || d.Int64() != -1 {
		t.Fatalf("got string=%q int=%d", d.String(), d.Int64())
	}
}

type sampleCfg struct {
	Chat DecimalInt64String `json:"telegram_owner_chat_id,omitempty"`
}

func TestDecimalInt64StringInStruct(t *testing.T) {
	raw := `{"telegram_owner_chat_id":"42"}`
	var c sampleCfg
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	if c.Chat.Int64() != 42 {
		t.Fatalf("got %d", c.Chat.Int64())
	}
	// Legacy number form.
	raw2 := `{"telegram_owner_chat_id":99}`
	var c2 sampleCfg
	if err := json.Unmarshal([]byte(raw2), &c2); err != nil {
		t.Fatal(err)
	}
	if c2.Chat.Int64() != 99 {
		t.Fatalf("legacy number got %d", c2.Chat.Int64())
	}
}
