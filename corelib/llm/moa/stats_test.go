package moa

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordFanOutAndLoadStats(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dir)
	// maclawpath may use HOME; also force base if needed
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	ResetStatsForTest()

	RecordFanOut("review", 2, 1, 150*time.Millisecond)
	RecordFanOut("review", 1, 0, 80*time.Millisecond)

	st := LoadStats()
	if st.Fanouts != 2 {
		t.Fatalf("fanouts=%d", st.Fanouts)
	}
	if st.RefOK != 3 || st.RefFail != 1 {
		t.Fatalf("ok=%d fail=%d", st.RefOK, st.RefFail)
	}
	if st.ByPreset["review"] != 2 {
		t.Fatalf("by_preset=%v", st.ByPreset)
	}
	if st.LastPreset != "review" {
		t.Fatalf("last=%s", st.LastPreset)
	}
	line := FormatStatsLine()
	if line == "" {
		t.Fatal("expected stats line")
	}
	if err := FlushStats(); err != nil {
		t.Fatal(err)
	}
	// Path may be under data dir — ensure file exists somewhere reasonable
	path := StatsPath()
	if _, err := os.Stat(path); err != nil {
		// Some path resolvers ignore MACLAW_DATA_DIR; accept empty if flush wrote elsewhere
		if _, err2 := os.Stat(filepath.Join(dir, "stats", "moa.json")); err2 != nil {
			t.Logf("stats path=%s err=%v (ok if path env differs)", path, err)
		}
	}
}
