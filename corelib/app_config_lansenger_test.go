package corelib

import (
	"fmt"
	"testing"
)

func TestLansengerGroupIgnoredList(t *testing.T) {
	var cfg AppConfig
	if cfg.IsLansengerGroupIgnored("g1") {
		t.Fatal("empty list should not ignore")
	}
	if !cfg.SetLansengerGroupIgnored("g1", true) {
		t.Fatal("first ignore should change")
	}
	if cfg.SetLansengerGroupIgnored("g1", true) {
		t.Fatal("duplicate ignore should be no-op")
	}
	if !cfg.IsLansengerGroupIgnored("g1") {
		t.Fatal("g1 should be ignored")
	}
	if !cfg.SetLansengerGroupIgnored(" g2 ", true) {
		t.Fatal("g2 ignore should change")
	}
	if !cfg.IsLansengerGroupIgnored("g2") {
		t.Fatal("trimmed g2 should match")
	}
	if !cfg.SetLansengerGroupIgnored("g1", false) {
		t.Fatal("unignore g1 should change")
	}
	if cfg.IsLansengerGroupIgnored("g1") {
		t.Fatal("g1 should no longer be ignored")
	}
	if !cfg.IsLansengerGroupIgnored("g2") {
		t.Fatal("g2 should still be ignored")
	}
	if !cfg.SetLansengerGroupIgnored("g2", false) {
		t.Fatal("unignore g2 should change")
	}
	if len(cfg.LansengerIgnoredGroupIDs) != 0 {
		t.Fatalf("list should be empty, got %#v", cfg.LansengerIgnoredGroupIDs)
	}
}

func TestLansengerGroupIgnoredListCapsSize(t *testing.T) {
	var cfg AppConfig
	for i := 0; i < MaxLansengerIgnoredGroups+3; i++ {
		if !cfg.SetLansengerGroupIgnored(fmt.Sprintf("g-%d", i), true) && i < MaxLansengerIgnoredGroups {
			t.Fatalf("ignore g-%d should change", i)
		}
	}
	if len(cfg.LansengerIgnoredGroupIDs) != MaxLansengerIgnoredGroups {
		t.Fatalf("len=%d, want %d", len(cfg.LansengerIgnoredGroupIDs), MaxLansengerIgnoredGroups)
	}
	// Oldest should have been dropped.
	if cfg.IsLansengerGroupIgnored("g-0") {
		t.Fatal("oldest entry should have been evicted")
	}
	if !cfg.IsLansengerGroupIgnored(fmt.Sprintf("g-%d", MaxLansengerIgnoredGroups+2)) {
		t.Fatal("newest entry should remain")
	}
}
