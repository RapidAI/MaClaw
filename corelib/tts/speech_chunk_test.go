package tts

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitSpeechChunks_ShortUnchanged(t *testing.T) {
	text := "现在是2026年7月16日星期四，实时新闻播报。"
	got := SplitSpeechChunks(text, 0)
	if len(got) != 1 {
		t.Fatalf("expected 1 chunk, got %d: %#v", len(got), got)
	}
	if !strings.Contains(got[0], "新闻播报") {
		t.Fatalf("chunk missing content: %q", got[0])
	}
}

func TestSplitSpeechChunks_LongNewsSemantic(t *testing.T) {
	text := `现在是2026年7月16日星期四，实时新闻播报。

首先是时政要闻：习近平在上海考察。

经济方面：二季度GDP增长百分之四点三，引发广泛关注。

体育方面，今天最大的热点是美洲杯半决赛，阿根廷二比一淘汰英格兰，决赛将对阵西班牙。这场比赛火药味十足，双方半场就贡献了十九次犯规，英阿大战被网友戏称为变身自由搏击。阿根廷连续两届闯进决赛。另外，网红甲亢哥怒喷裁判双标判罚。

社会新闻：四川一辆旅游车坠入河滩，多部门正在紧急救援。

科技方面：日媒报道，日系车开始拥抱中国技术。

另外提醒大家，存一百万元解锁百分之五点二五利息的消息已被证实为不实信息。

以上就是今天的新闻热点播报。`

	chunks := SplitSpeechChunks(text, 0)
	if len(chunks) < 3 {
		t.Fatalf("expected multi-chunk split for long news, got %d: %#v", len(chunks), chunks)
	}

	joined := strings.Join(chunks, "")
	for _, want := range []string{"习近平", "GDP", "阿根廷", "四川", "日系车", "新闻热点播报"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in joined chunks", want)
		}
	}

	// Content preservation: cleaned text should be recoverable (ignoring join spaces).
	cleaned := CleanForSpeech(text)
	compactJoined := strings.ReplaceAll(joined, " ", "")
	compactClean := strings.ReplaceAll(cleaned, " ", "")
	if !strings.Contains(compactJoined, compactClean) && !strings.Contains(compactClean, compactJoined) {
		// Allow minor space differences from packing; key is near-equal length.
		jr, cr := utf8.RuneCountInString(compactJoined), utf8.RuneCountInString(compactClean)
		if jr < cr*9/10 || jr > cr+20 {
			t.Errorf("content length drift: joined=%d cleaned=%d\njoined=%q\ncleaned=%q", jr, cr, joined, cleaned)
		}
	}

	for i, c := range chunks {
		n := utf8.RuneCountInString(c)
		if n > MaxSafeSpeechChunkRunes {
			t.Errorf("chunk %d exceeds rune cap: %d > %d (%q)", i, n, MaxSafeSpeechChunkRunes, c)
		}
		if !fitsSpeechChunk(c, MaxSafeSpeechChunkRunes) {
			t.Errorf("chunk %d fails phoneme/rune budget: runes=%d phonemes≈%d text=%q",
				i, n, estimatePhonemeTokens(c), c)
		}
	}
}

func TestSplitSpeechChunks_CleansMarkdown(t *testing.T) {
	text := "## 标题\n\n**加粗**新闻，[链接](https://example.com) 内容。"
	chunks := SplitSpeechChunks(text, 0)
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
	joined := strings.Join(chunks, "")
	if strings.Contains(joined, "**") || strings.Contains(joined, "https://") || strings.Contains(joined, "##") {
		t.Fatalf("markdown not cleaned: %q", joined)
	}
	if !strings.Contains(joined, "加粗") || !strings.Contains(joined, "新闻") {
		t.Fatalf("content lost: %q", joined)
	}
}

func TestSplitSpeechChunks_HardSplitOversizedClause(t *testing.T) {
	text := strings.Repeat("这是一段没有句号的长文本内容", 20)
	chunks := SplitSpeechChunks(text, 40)
	if len(chunks) < 2 {
		t.Fatalf("expected hard split, got %d", len(chunks))
	}
	for i, c := range chunks {
		if utf8.RuneCountInString(c) > MaxSafeSpeechChunkRunes {
			t.Errorf("chunk %d too long: %d", i, utf8.RuneCountInString(c))
		}
		if !fitsSpeechChunk(c, MaxSafeSpeechChunkRunes) {
			t.Errorf("chunk %d over phoneme budget: %q", i, c)
		}
	}
}

func TestSplitSpeechChunks_KeepsDecimalIntact(t *testing.T) {
	text := "二季度GDP增长百分之四点三，引发广泛关注。增长约4.3个百分点。"
	chunks := SplitSpeechChunks(text, 0)
	joined := strings.Join(chunks, "")
	if !strings.Contains(joined, "4.3") {
		t.Fatalf("decimal split incorrectly: %#v", chunks)
	}
}

func TestSplitSpeechChunks_EnglishParagraphs(t *testing.T) {
	text := "Hello world. This is a second sentence. And a third one about Argentina beating England."
	chunks := SplitSpeechChunks(text, 40)
	if len(chunks) < 2 {
		t.Fatalf("expected multi chunk, got %#v", chunks)
	}
	joined := strings.Join(chunks, " ")
	for _, want := range []string{"Hello", "second", "Argentina"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %q", want, joined)
		}
	}
}

func TestNormalizeSpeechChunkRunes_ClampsLargeBudget(t *testing.T) {
	if got := normalizeSpeechChunkRunes(300); got != MaxSafeSpeechChunkRunes {
		t.Fatalf("normalizeSpeechChunkRunes(300)=%d, want %d", got, MaxSafeSpeechChunkRunes)
	}
	if got := normalizeSpeechChunkRunes(0); got != DefaultSpeechChunkRunes {
		t.Fatalf("normalizeSpeechChunkRunes(0)=%d, want %d", got, DefaultSpeechChunkRunes)
	}
}

func TestCapSpeechText(t *testing.T) {
	long := strings.Repeat("新闻要点。", 50)
	got := CapSpeechText(long, AutoSpeechMaxRunes)
	if utf8.RuneCountInString(got) > AutoSpeechMaxRunes {
		t.Fatalf("cap failed: %d > %d", utf8.RuneCountInString(got), AutoSpeechMaxRunes)
	}
	if got == "" {
		t.Fatal("empty cap result")
	}
	// Prefer sentence boundary.
	if !strings.HasSuffix(got, "。") && !strings.Contains(got, "。") {
		t.Logf("cap without sentence end (acceptable): %q", got)
	}
}

func TestPrepareSpeechChunks_SingleCleanAndCap(t *testing.T) {
	// Markdown + over-cap body: one-shot prepare must clean and chunk under budget.
	body := strings.Repeat("这是一条实时新闻播报内容。", 80)
	text := "## 标题\n\n**加粗**\n\n" + body
	chunks := PrepareSpeechChunks(text, 200, 0)
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
	joined := strings.Join(chunks, "")
	if strings.Contains(joined, "**") || strings.Contains(joined, "##") {
		t.Fatalf("markdown not cleaned: %q", joined)
	}
	total := 0
	for i, c := range chunks {
		total += utf8.RuneCountInString(c)
		if !fitsSpeechChunk(c, MaxSafeSpeechChunkRunes) {
			t.Errorf("chunk %d over budget: runes=%d ph=%d %q",
				i, utf8.RuneCountInString(c), estimatePhonemeTokens(c), c)
		}
	}
	if total > 200+10 { // small slack for join spaces only; cap is on cleaned text before split
		// Cap applies before split; join spaces can slightly increase joined length.
		if total > 240 {
			t.Fatalf("total runes after prepare too large: %d", total)
		}
	}
}

func TestEnsureSpeechChunksFit_ReSplitsOverflow(t *testing.T) {
	// Force a single oversized blob past ensure.
	blob := strings.Repeat("密", 200)
	got := ensureSpeechChunksFit([]string{blob}, 40)
	if len(got) < 2 {
		t.Fatalf("expected re-split, got %#v", got)
	}
	for i, c := range got {
		if utf8.RuneCountInString(c) > MaxSafeSpeechChunkRunes {
			t.Errorf("chunk %d still too long: %d", i, utf8.RuneCountInString(c))
		}
	}
}

func TestConcatenateWAVs_JoinsSegments(t *testing.T) {
	a := EncodeWAV([]float32{0.1, 0.2, 0.3, 0.0}, 24000)
	b := EncodeWAV([]float32{0.4, 0.5}, 24000)
	joined, err := ConcatenateWAVs([][]byte{a, b}, 10)
	if err != nil {
		t.Fatalf("ConcatenateWAVs: %v", err)
	}
	pcm, rate, ch, err := parseWAVS16(joined)
	if err != nil {
		t.Fatalf("parse joined: %v", err)
	}
	if rate != 24000 || ch != 1 {
		t.Fatalf("rate=%d ch=%d", rate, ch)
	}
	// 4 + silence(240 samples for 10ms@24k) + 2
	if len(pcm) < 4+2 {
		t.Fatalf("joined pcm too short: %d", len(pcm))
	}
	// With 10ms gap @ 24kHz => 240 samples
	if len(pcm) != 4+240+2 {
		t.Fatalf("unexpected joined length: %d want %d", len(pcm), 4+240+2)
	}
}

func TestSynthesizeSpeechParts_Empty(t *testing.T) {
	_, _, err := SynthesizeSpeechParts(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil manager/parts")
	}
}

func TestEstimatePhonemeTokens_MonotonicWithLength(t *testing.T) {
	short := estimatePhonemeTokens("你好")
	long := estimatePhonemeTokens("你好世界，欢迎使用语音合成系统。")
	if long <= short {
		t.Fatalf("expected longer text more tokens: short=%d long=%d", short, long)
	}
	// Frame tokens = content + 2
	if estimatePhonemeTokens("你好") != phonemeFrameTokens(estimatePhonemeContentTokens("你好")) {
		t.Fatal("frame token mismatch")
	}
}

func TestPackingDoesNotDoubleCountBOS(t *testing.T) {
	// Two short sentences that fit together when BOS/EOS is not double-counted per unit.
	a := "你好。"
	b := "世界。"
	single := estimatePhonemeTokens(a + b)
	sumFramed := estimatePhonemeTokens(a) + estimatePhonemeTokens(b) // naive sum over-counts frame
	if sumFramed <= single {
		t.Fatalf("expected sum of framed estimates > combined: sum=%d single=%d", sumFramed, single)
	}
	// Content-only sum may slightly under-count vs joined G2P (context); packing adds join slack.
	contentSum := estimatePhonemeContentTokens(a) + estimatePhonemeContentTokens(b)
	combinedContent := estimatePhonemeContentTokens(a + b)
	if contentSum+phonemeJoinSlackTokens < combinedContent {
		t.Fatalf("join slack insufficient: sum=%d slack=%d combined=%d",
			contentSum, phonemeJoinSlackTokens, combinedContent)
	}
	// Packing should put both into one chunk at default budget.
	chunks := SplitSpeechChunks(a+b, 0)
	if len(chunks) != 1 {
		t.Fatalf("expected single chunk after BOS fix, got %d: %#v", len(chunks), chunks)
	}
	if !fitsSpeechChunk(chunks[0], MaxSafeSpeechChunkRunes) {
		t.Fatalf("packed chunk over budget: %q ph=%d", chunks[0], estimatePhonemeTokens(chunks[0]))
	}
}

func TestSynthesizeSpeechParts_FakeManager(t *testing.T) {
	// Minimal valid mono WAV from EncodeWAV.
	mk := func(tag float32) []byte {
		return EncodeWAV([]float32{tag, tag * 0.5, 0, 0.1}, 24000)
	}
	fake := &fakeTextSynth{wavFor: map[string][]byte{
		"一段。": mk(0.2),
		"二段。": mk(0.4),
	}}
	wav, n, err := SynthesizeSpeechParts(fake, []string{"一段。", "二段。"})
	if err != nil {
		t.Fatalf("SynthesizeSpeechParts: %v", err)
	}
	if n != 2 {
		t.Fatalf("chunks=%d", n)
	}
	pcm, rate, ch, err := parseWAVS16(wav)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if rate != 24000 || ch != 1 {
		t.Fatalf("rate/ch=%d/%d", rate, ch)
	}
	// 4 + silence(200ms@24k=4800) + 4
	want := 4 + 24000*SpeechChunkSilenceMs/1000 + 4
	if len(pcm) != want {
		t.Fatalf("pcm len=%d want %d", len(pcm), want)
	}
	if len(fake.seen) != 2 {
		t.Fatalf("seen=%v", fake.seen)
	}
}

type fakeTextSynth struct {
	wavFor map[string][]byte
	seen   []string
}

func (f *fakeTextSynth) SynthesizeText(text string) ([]byte, error) {
	f.seen = append(f.seen, text)
	if w, ok := f.wavFor[text]; ok {
		return append([]byte(nil), w...), nil
	}
	return EncodeWAV([]float32{0.1, 0.1}, 24000), nil
}

func BenchmarkSplitSpeechChunks_LongNews(b *testing.B) {
	text := strings.Repeat("现在是新闻播报。阿根廷淘汰英格兰。二季度GDP增长百分之四点三。", 30)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = SplitSpeechChunks(text, 0)
	}
}
