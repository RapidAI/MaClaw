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

func TestForwardChatTokenLanesSendsReasoningWithoutAnswer(t *testing.T) {
	var tokens, thoughts []string
	cb := forwardChatTokenLanes(func(delta string) {
		tokens = append(tokens, delta)
	}, func(delta string) {
		thoughts = append(thoughts, delta)
	})
	soh := string(rune(1))
	cb(soh + "user")
	cb("hello")
	cb(soh)
	cb("\x02kept")
	if len(thoughts) != 1 || thoughts[0] != "user" {
		t.Fatalf("thoughts %#v", thoughts)
	}
	if len(tokens) != 2 || tokens[0] != "hello" || tokens[1] != "kept" {
		t.Fatalf("tokens %#v", tokens)
	}
}
