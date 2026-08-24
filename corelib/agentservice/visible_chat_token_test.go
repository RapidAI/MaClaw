package agentservice

import "testing"

func TestWrapVisibleChatTokenCallbackDropsReasoningLaneAndTofu(t *testing.T) {
	var got []string
	cb := wrapVisibleChatTokenCallback(func(delta string) {
		got = append(got, delta)
	})
	soh := string(rune(1))
	pua := string(rune(0xEB90))
	cb(soh + "user")
	cb("I am " + pua + "Kate")
	cb("hello")
	cb(soh)
	if len(got) != 2 || got[0] != "I am Kate" || got[1] != "hello" {
		t.Fatalf("got %#v", got)
	}
}

func TestWrapVisibleChatTokenCallbackNil(t *testing.T) {
	if wrapVisibleChatTokenCallback(nil) != nil {
		t.Fatal("nil callback should stay nil")
	}
}
