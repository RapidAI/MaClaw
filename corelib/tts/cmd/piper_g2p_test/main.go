package main

import (
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib/tts"
)

func main() {
	texts := []string{
		"你好世界",
		"今天天气不错",
		"我们一起来写代码吧",
		"欢迎使用智能助手",
		"人工智能正在改变世界",
	}
	for _, text := range texts {
		g2p := tts.PiperTextToPhonemes(text)
		fmt.Printf("%s: %v\n", text, g2p.PhonemeIDs)
	}
}
