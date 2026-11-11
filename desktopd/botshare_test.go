package desktopd

import (
	"testing"
	"time"
)

func TestSharedCPUsGrowsWithActiveBotsAndStopsAtHost(t *testing.T) {
	t.Setenv("DESKTOPD_BOT_CPUS_EACH", "")
	t.Setenv("DESKTOPD_HOST_CPU_RESERVE", "")
	t.Setenv("DESKTOPD_CPUS_FLOOR", "0")
	if got := sharedCPUs("1.5", 1, 4); got != "1.5" {
		t.Fatalf("one bot = %s", got)
	}
	if got := sharedCPUs("1.5", 3, 4); got != "3.00" {
		t.Fatalf("three bots = %s", got)
	}
	if got := sharedCPUs("1.5", 6, 4); got != "3.50" {
		t.Fatalf("capped = %s", got)
	}
	if got := sharedCPUs("1.5", 3, 1); got != "1.5" {
		t.Fatalf("small host = %s", got)
	}
	t.Setenv("DESKTOPD_BOT_CPUS_EACH", "0")
	if got := sharedCPUs("1.5", 3, 4); got != "1.5" {
		t.Fatalf("off = %s", got)
	}
}

func TestBotShareCountsRecentBotsOfOneOwner(t *testing.T) {
	var b botShare
	now := time.Now()
	b.note("o", "a", now.Add(-time.Hour))
	b.note("o", "b", now.Add(-time.Minute))
	b.note("p", "x", now)
	if n := b.note("o", "c", now); n != 2 {
		t.Fatalf("active = %d", n)
	}
	if n := b.note("o", "", now); n != 2 {
		t.Fatalf("owner open = %d", n)
	}
}

func TestSharedCPUsHasAThreeCoreFloor(t *testing.T) {
	t.Setenv("DESKTOPD_BOT_CPUS_EACH", "")
	t.Setenv("DESKTOPD_HOST_CPU_RESERVE", "")
	t.Setenv("DESKTOPD_CPUS_FLOOR", "")
	if got := sharedCPUs("1.5", 1, 4); got != "3.00" {
		t.Fatalf("one bot = %s", got)
	}
	if got := sharedCPUs("1.5", 3, 4); got != "3.50" {
		t.Fatalf("three bots on 4 cpus = %s", got)
	}
	if got := sharedCPUs("1.5", 2, 8); got != "3.75" {
		t.Fatalf("two bots on 8 cpus = %s", got)
	}
	if got := sharedCPUs("1.5", 1, 2); got != "1.50" && got != "1.5" {
		t.Fatalf("small host = %s", got)
	}
}
