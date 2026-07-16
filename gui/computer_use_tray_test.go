package main

import "testing"

func TestComputerUseTrayLabelsIdle(t *testing.T) {
	menu, status, pause, resume, stop, reset, pe, re, se, xe := computerUseTrayLabels(nil)
	if menu == "" || pause == "" || resume == "" || stop == "" || reset == "" {
		t.Fatalf("missing labels: %q %q %q %q %q", menu, pause, resume, stop, reset)
	}
	if pe || re || se || xe {
		t.Fatalf("idle should disable actions pe=%v re=%v se=%v xe=%v", pe, re, se, xe)
	}
	if status == "" {
		t.Fatal("empty status")
	}
}
