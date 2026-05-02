package main

import "testing"

func TestShouldDropASRTextPhraseLoop(t *testing.T) {
	drop, reason := shouldDropASRText("对你好啊对你好啊对你好啊对你好啊对你好啊", 16000)
	if !drop {
		t.Fatalf("expected phrase loop to be dropped, reason=%q", reason)
	}
}

func TestShouldDropASRTextDropsTokenPhraseLoop(t *testing.T) {
	drop, reason := shouldDropASRText("Zither Harp Zither Harp Zither Harp Zither Harp Zither Harp", 16000)
	if !drop {
		t.Fatalf("expected token phrase loop to be dropped, reason=%q", reason)
	}
}

func TestShouldDropASRTextDropsShortRuneLoop(t *testing.T) {
	drop, reason := shouldDropASRText("拍脸瓜拍脸瓜拍脸瓜拍脸瓜", 16000)
	if !drop {
		t.Fatalf("expected short rune loop to be dropped, reason=%q", reason)
	}
}

func TestShouldDropASRTextKeepsShortGreeting(t *testing.T) {
	drop, reason := shouldDropASRText("你好", 16000)
	if drop {
		t.Fatalf("expected short greeting to be kept, reason=%q", reason)
	}
}

func TestShouldDropASRTextDropsReplacementGarbage(t *testing.T) {
	drop, reason := shouldDropASRText("银行银行银行银行����������������", 16000)
	if !drop {
		t.Fatalf("expected replacement garbage to be dropped, reason=%q", reason)
	}
}
