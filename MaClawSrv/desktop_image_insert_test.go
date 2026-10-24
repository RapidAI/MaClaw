package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestManualImageInsertIsRedirectedToPublish(t *testing.T) {
	raw, err := os.ReadFile("testdata/manual_image/steps.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name   string         `json:"name"`
		Refuse bool           `json:"refuse"`
		Action string         `json:"action"`
		Args   map[string]any `json:"args"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	task := "找当天点赞最多的论文，在知乎发论文解读，配论文图片"
	for _, c := range cases {
		err := desktopManualImageCheck(c.Action, c.Args, task, false)
		if (err != nil) != c.Refuse {
			t.Errorf("%s: refuse=%v err=%v", c.Name, c.Refuse, err)
		}
		if err != nil && !strings.Contains(err.Error(), "action=publish") {
			t.Errorf("%s: refusal must point at action=publish: %v", c.Name, err)
		}
		if desktopManualImageCheck(c.Action, c.Args, "把这张图复制到剪贴板", false) != nil {
			t.Errorf("%s: refused without a publish task", c.Name)
		}
		if desktopManualImageCheck(c.Action, c.Args, task, true) != nil {
			t.Errorf("%s: refused after publish", c.Name)
		}
	}
}
