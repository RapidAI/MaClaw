//go:build windows

package guiapp

import "testing"

func TestChatMediaClipboardStagesMemoryBeforeOpen(t *testing.T) {
	staged, err := stageChatMediaClipboard(chatMediaClipboard{
		dib: []byte{1, 2, 3, 4},
		png: []byte{9, 9},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 2 || staged[0].format != chatMediaCFDIB || staged[0].mem == 0 || staged[1].format == 0 || staged[1].mem == 0 {
		t.Fatalf("staged = %+v", staged)
	}
	freeChatMediaClipboard(staged)
	for _, item := range staged {
		if item.mem != 0 {
			t.Fatal("clipboard memory was not released")
		}
	}

	empty, err := stageChatMediaClipboard(chatMediaClipboard{})
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty staged=%d err=%v", len(empty), err)
	}
}
