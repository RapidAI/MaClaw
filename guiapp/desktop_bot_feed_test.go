package guiapp

import "testing"

func TestDesktopBotRunOutputIsDoneOnlyWhenTheRunEnded(t *testing.T) {
	if o := desktopBotRunOutputOf("running", "", "", ""); o.Done {
		t.Fatal("running run shown as finished")
	}
	o := desktopBotRunOutputOf("succeeded", "raw", "已处理队友的消息", "")
	if !o.Done || o.Text != "已处理队友的消息" {
		t.Fatalf("%+v", o)
	}
	if o := desktopBotRunOutputOf("cancelled", "", "", "run cancelled"); !o.Done || o.Error == "" {
		t.Fatalf("%+v", o)
	}
}
