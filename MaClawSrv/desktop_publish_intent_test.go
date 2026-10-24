package main

import "testing"

func TestPublishIntentShortVerb(t *testing.T) {
	for _, s := range []string{"在知乎发论文解读：原理方法", "在小红书上发一条笔记", "发到知乎"} {
		if !desktopPublishIntentRe.MatchString(s) {
			t.Errorf("want intent: %s", s)
		}
	}
	for _, s := range []string{"在知乎发现一篇好文章", "看看知乎上的论文"} {
		if desktopPublishIntentRe.MatchString(s) {
			t.Errorf("no intent: %s", s)
		}
	}
}
