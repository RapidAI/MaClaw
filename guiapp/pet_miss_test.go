package guiapp

import (
	"testing"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/guiapp/petpack"
)

func TestPetMissGrowsWithAbsenceAndBond(t *testing.T) {
	stages := []petpack.PetPackRearingStage{
		{ID: "familiar", AfterMS: 7_200_000},
		{ID: "grown", AfterMS: 86_400_000},
		{ID: "settled", AfterMS: 259_200_000},
	}
	if petMissIntensity(4*time.Minute, 2) != 0 || petMissLine(0, 2) != "" || petMissClip(0) != "" {
		t.Fatal("a short absence is not a complaint")
	}
	if expr, gaze, mood := petMissFeel(0); expr != "" || gaze != "" || mood != "" {
		t.Fatalf("a short absence still has a face %s %s %s", expr, gaze, mood)
	}
	if petMissBondLevel(0, stages) != 0 || petMissBondLevel(7_200_000, stages) != 1 || petMissBondLevel(86_400_000, stages) != 2 || petMissBondLevel(259_200_000, stages) != 2 {
		t.Fatalf("bond levels 0=%d familiar=%d grown=%d settled=%d", petMissBondLevel(0, stages), petMissBondLevel(7_200_000, stages), petMissBondLevel(86_400_000, stages), petMissBondLevel(259_200_000, stages))
	}
	cases := []struct {
		away time.Duration
		bond int
		want int
		line string
		clip string
	}{
		{5 * time.Minute, 0, 1, "好久没理我了", "react_long_idle_rest"},
		{5 * time.Minute, 1, 2, "你都不来看我", "react_long_idle_sulk"},
		{5 * time.Minute, 2, 2, "一起这么久，你倒忘了我", "react_long_idle_sulk"},
		{30 * time.Minute, 0, 2, "你是不是把我忘了", "react_long_idle_sulk"},
		{30 * time.Minute, 2, 3, "你把我晾了这么久", "react_long_idle_pout"},
		{2 * time.Hour, 0, 3, "我在这儿等了好久", "react_long_idle_pout"},
		{3 * time.Hour, 2, 3, "你把我晾了这么久", "react_long_idle_pout"},
	}
	for _, tc := range cases {
		got := petMissIntensity(tc.away, tc.bond)
		line := petMissLine(got, tc.bond)
		clip := petMissClip(got)
		expr, gaze, mood := petMissFeel(got)
		wantExpr, wantGaze, wantMood := "", "", ""
		switch tc.want {
		case 1:
			wantExpr, wantGaze, wantMood = "expr_wait", "gaze_center", "tired"
		case 2:
			wantExpr, wantGaze, wantMood = "expr_sulk", "gaze_center", "tired"
		case 3:
			wantExpr, wantGaze, wantMood = "expr_pout", "gaze_center", "tired"
		}
		if got != tc.want || line != tc.line || clip != tc.clip || expr != wantExpr || gaze != wantGaze || mood != wantMood {
			t.Fatalf("away=%s bond=%d got=%d %q %s %s/%s/%s want=%d %q %s %s/%s/%s", tc.away, tc.bond, got, line, clip, expr, gaze, mood, tc.want, tc.line, tc.clip, wantExpr, wantGaze, wantMood)
		}
		if utf8.RuneCountInString(line) > petPresenceRunes {
			t.Fatalf("line %q is longer than %d runes", line, petPresenceRunes)
		}
	}
	if petShownPresence("正在做", "好久没理我了") != "正在做" || petShownPresence("", "好久没理我了") != "好久没理我了" {
		t.Fatal("work sentence must stay ahead of the complaint")
	}
	if !petMissOccupied("还没开始", "idle") || !petMissOccupied("", "thinking") || !petMissOccupied("", "listening") || !petMissOccupied("", "speaking") || !petMissOccupied("", "done") || !petMissOccupied("", "alert") {
		t.Fatal("a task sentence or a companion pose is not time she was left")
	}
	if petMissOccupied("", "idle") || petMissOccupied("", "quiet") || petMissOccupied("  ", "idle") {
		t.Fatal("idle, quiet, and a blank sentence still count as waiting")
	}
}
