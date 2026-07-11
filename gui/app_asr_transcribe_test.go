package main

import (
	"strings"
	"testing"
)

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

func TestShouldDropASRTextPunctuationOnly(t *testing.T) {
	cases := []string{"。", ".", "…", "。。", "...", "！？", " !?.,;: ", "——"}
	for _, text := range cases {
		drop, reason := shouldDropASRText(text, 16000)
		if !drop {
			t.Fatalf("expected punctuation-only %q to be dropped, reason=%q", text, reason)
		}
		if reason != "punctuation-only" {
			t.Fatalf("expected reason punctuation-only for %q, got %q", text, reason)
		}
	}
}

func TestShouldDropASRTextKeepsContentWithPunctuation(t *testing.T) {
	cases := []string{"查询北京天气。", "hello.", "嗯。", "OK", "123"}
	for _, text := range cases {
		drop, reason := shouldDropASRText(text, 16000)
		if drop {
			t.Fatalf("expected content %q to be kept, reason=%q", text, reason)
		}
	}
}

func TestShouldDropASRTextDropsReplacementGarbage(t *testing.T) {
	drop, reason := shouldDropASRText("银行银行银行银行"+strings.Repeat(string(rune(0xFFFD)), 16), 16000)
	if !drop {
		t.Fatalf("expected replacement garbage to be dropped, reason=%q", reason)
	}
}
