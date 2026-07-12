package agent

import "testing"

func TestRecordLightToolDeny_Counts(t *testing.T) {
	ResetPromptProfileStatsForTest()
	if GetPromptProfileStats().LightToolDenies != 0 {
		t.Fatalf("want 0 denies, got %#v", GetPromptProfileStats())
	}
	RecordLightToolDeny("bash")
	RecordLightToolDeny("bash")
	RecordLightToolDeny("write_file")
	st := GetPromptProfileStats()
	if st.LightToolDenies != 3 {
		t.Fatalf("total=%d want 3", st.LightToolDenies)
	}
	if st.ByDeniedTool["bash"] != 2 || st.ByDeniedTool["write_file"] != 1 {
		t.Fatalf("by tool = %#v", st.ByDeniedTool)
	}
	if st.LastDeniedTool != "write_file" {
		t.Fatalf("last denied = %q", st.LastDeniedTool)
	}
	RecordLightToolDeny("  ")
	st = GetPromptProfileStats()
	if st.LightToolDenies != 4 {
		t.Fatalf("total after empty=%d", st.LightToolDenies)
	}
	if st.ByDeniedTool["(unknown)"] != 1 {
		t.Fatalf("unknown count missing: %#v", st.ByDeniedTool)
	}
}

func TestIsLightTurnToolAllowed(t *testing.T) {
	if !IsLightTurnToolAllowed("web_search") {
		t.Fatal("web_search should be allowed")
	}
	if IsLightTurnToolAllowed("bash") {
		t.Fatal("bash should not be allowed on light")
	}
	if IsLightTurnToolAllowed("") {
		t.Fatal("empty should not be allowed")
	}
}
