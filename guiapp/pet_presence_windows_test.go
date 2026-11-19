//go:build windows

package guiapp

import (
	"testing"
	"unsafe"
)

func TestPetPresenceWin32Layouts(t *testing.T) {
	if unsafe.Sizeof(toolInfoW{}) != 72 {
		t.Fatalf("TOOLINFOW size = %d", unsafe.Sizeof(toolInfoW{}))
	}
	if unsafe.Sizeof(trackMouseEventW{}) != 24 {
		t.Fatalf("TRACKMOUSEEVENT size = %d", unsafe.Sizeof(trackMouseEventW{}))
	}
}

func TestPetPresenceLeaveDuringCaptureIsNotAnExit(t *testing.T) {
	if petPresenceOnLeave(true) {
		t.Fatal("a leave posted for the capture cancellation ended the visit")
	}
	if !petPresenceOnLeave(false) {
		t.Fatal("a leave without capture kept the visit")
	}
}

func TestPetPresenceTrackSkipsTheSamePoint(t *testing.T) {
	if petPresenceTrackNeeded(true, "正在做", "正在做", true, 40, 12, 40, 12) {
		t.Fatal("the same sentence at the same point was sent again")
	}
	if !petPresenceTrackNeeded(true, "正在做", "在改定价页", true, 40, 12, 40, 12) {
		t.Fatal("a new sentence did not move the note")
	}
	if !petPresenceTrackNeeded(true, "正在做", "正在做", true, 40, 12, 48, 12) {
		t.Fatal("a moved pet left the note behind")
	}
	if !petPresenceTrackNeeded(false, "正在做", "正在做", true, 40, 12, 40, 12) {
		t.Fatal("a hidden note was not shown")
	}
}

func TestPetPresencePointInRectUsesTheWindowEdges(t *testing.T) {
	if !petPointInRect(10, 20, 10, 20, 30, 40) {
		t.Fatal("the top-left of the pet was outside")
	}
	if petPointInRect(30, 20, 10, 20, 30, 40) || petPointInRect(10, 40, 10, 20, 30, 40) {
		t.Fatal("the exclusive edge counted as still on the pet")
	}
	if petPointInRect(9, 20, 10, 20, 30, 40) {
		t.Fatal("a point just outside counted as on the pet")
	}
}
