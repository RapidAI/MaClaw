package corelib

import (
	"encoding/json"
	"testing"
)

func TestIsShowAppEntryEnabled_DefaultOn(t *testing.T) {
	// Empty config: UnmarshalJSON seeds AppConfigDefaults; nil pointer means on.
	var cfg AppConfig
	if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
		t.Fatalf("Unmarshal empty: %v", err)
	}
	if !cfg.IsShowAppEntryEnabled() {
		t.Fatal("empty JSON should enable MaClaw app entry by default")
	}

	// Explicit false must survive (user turned the switch off).
	var off AppConfig
	if err := json.Unmarshal([]byte(`{"show_app_entry":false}`), &off); err != nil {
		t.Fatalf("Unmarshal false: %v", err)
	}
	if off.IsShowAppEntryEnabled() {
		t.Fatal("explicit show_app_entry:false must keep the entry disabled")
	}

	// Explicit true stays on.
	var on AppConfig
	if err := json.Unmarshal([]byte(`{"show_app_entry":true}`), &on); err != nil {
		t.Fatalf("Unmarshal true: %v", err)
	}
	if !on.IsShowAppEntryEnabled() {
		t.Fatal("explicit show_app_entry:true must keep the entry enabled")
	}
}

func TestShowUtilitiesEntry_DefaultOnAndConfigurable(t *testing.T) {
	var cfg AppConfig
	if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
		t.Fatalf("Unmarshal empty config: %v", err)
	}
	if cfg.ShowUtilitiesEntry != nil {
		t.Fatal("empty config should keep utilities visibility unset for default-on behavior")
	}

	var off AppConfig
	if err := json.Unmarshal([]byte(`{"show_utilities_entry":false}`), &off); err != nil {
		t.Fatalf("Unmarshal utilities disabled config: %v", err)
	}
	if off.ShowUtilitiesEntry == nil || *off.ShowUtilitiesEntry {
		t.Fatal("explicit show_utilities_entry:false must keep the entry disabled")
	}
}
