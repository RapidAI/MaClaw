package kokoro

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestReadTensorFileV2MixedQ8(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(tensorMagic[:])
	mustWrite := func(v any) {
		t.Helper()
		if err := binary.Write(&buf, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	mustTensorHeader := func(name string, dims ...uint32) {
		t.Helper()
		mustWrite(uint16(len(name)))
		buf.WriteString(name)
		mustWrite(uint8(len(dims)))
		for _, dim := range dims {
			mustWrite(dim)
		}
	}

	mustWrite(tensorFormatVersionV2)
	mustWrite(uint32(2))
	mustTensorHeader("f32", 2, 2)
	mustWrite(uint8(TensorFloat32))
	mustWrite([]float32{1, 2, 3, 4})
	mustTensorHeader("q8", 2, 3)
	mustWrite(uint8(TensorQ8Rowwise))
	mustWrite(uint32(2))
	mustWrite(uint32(3))
	mustWrite([]float32{0.5, 2})
	buf.Write([]byte{2, 252, 6, 1, 0, 254})

	tf, err := ReadTensorFile(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	f32, ok := tf.Get("f32")
	if !ok {
		t.Fatal("missing f32 tensor")
	}
	f32Data, err := f32.Float32()
	if err != nil {
		t.Fatal(err)
	}
	assertFloat32s(t, f32Data, []float32{1, 2, 3, 4})
	q8, ok := tf.Get("q8")
	if !ok {
		t.Fatal("missing q8 tensor")
	}
	if q8.Data != nil {
		t.Fatalf("q8 tensor should dequantize lazily")
	}
	q8Data, err := q8.Float32()
	if err != nil {
		t.Fatal(err)
	}
	assertFloat32s(t, q8Data, []float32{1, -2, 3, 2, 0, -4})
}

func assertFloat32s(t *testing.T, got, want []float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len=%d want=%d got=%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("[%d]=%v want=%v all=%v", i, got[i], want[i], got)
		}
	}
}

func TestNormalizeForMandarinTTS(t *testing.T) {
	got := NormalizeForMandarinTTS("AI Coder tests TTS with Kokoro-82M in 2026")
	wantParts := []string{"智能代码助手", "语音合成", "科科罗八千二百万参数", "二零二六"}
	for _, part := range wantParts {
		if !contains(got, part) {
			t.Fatalf("NormalizeForMandarinTTS() = %q, missing %q", got, part)
		}
	}
}

func TestTokenizePhonemes(t *testing.T) {
	cfg := &Config{PLBert: PLBertConfig{MaxPositionEmbeddings: 8}, Vocab: map[string]int{"a": 43, " ": 16}}
	ids, err := TokenizePhonemes(cfg, "a ?")
	if err != nil {
		t.Fatal(err)
	}
	want := []int{0, 43, 16, 0}
	if len(ids) != len(want) {
		t.Fatalf("ids=%v want=%v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids=%v want=%v", ids, want)
		}
	}
}

func TestLoadExportedVoiceWhenAvailable(t *testing.T) {
	root := os.Getenv("KOKORO_GO_ASSETS")
	if root == "" {
		t.Skip("set KOKORO_GO_ASSETS to run asset smoke test")
	}
	cfg, err := LoadConfig(filepath.Join(root, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HiddenDim != 512 || cfg.StyleDim != 128 {
		t.Fatalf("unexpected config dims: hidden=%d style=%d", cfg.HiddenDim, cfg.StyleDim)
	}
	voice, err := LoadTensorFile(filepath.Join(root, "voices", "zm_yunxi.koro"))
	if err != nil {
		t.Fatal(err)
	}
	pack, ok := voice.Get("pack")
	if !ok {
		t.Fatalf("voice tensor pack not found")
	}
	if len(pack.Dims) != 3 || pack.Dims[0] != 510 || pack.Dims[1] != 1 || pack.Dims[2] != 256 {
		t.Fatalf("unexpected voice pack dims: %v", pack.Dims)
	}
	model, err := LoadModel(Assets{ConfigPath: filepath.Join(root, "config.json"), WeightsPath: filepath.Join(root, "kokoro-v1_0.koro")})
	if err != nil {
		t.Fatal(err)
	}
	pcm, err := model.SynthesizePhonemes("a", voice, 1)
	if err != nil {
		t.Fatalf("SynthesizePhonemes err=%v", err)
	}
	if len(pcm) == 0 {
		t.Fatalf("SynthesizePhonemes returned empty audio")
	}
	for i, v := range pcm {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			t.Fatalf("SynthesizePhonemes sample %d is not finite: %v", i, v)
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && index(s, sub) >= 0)
}

func index(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestAlbertForwardWhenAssetsAvailable(t *testing.T) {
	root := os.Getenv("KOKORO_GO_ASSETS")
	if root == "" {
		t.Skip("set KOKORO_GO_ASSETS to run ALBERT smoke test")
	}
	model, err := LoadModel(Assets{ConfigPath: filepath.Join(root, "config.json"), WeightsPath: filepath.Join(root, "kokoro-v1_0.koro")})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := TokenizePhonemes(model.Config, "a")
	if err != nil {
		t.Fatal(err)
	}
	out, dim, err := model.AlbertForward(ids)
	if err != nil {
		t.Fatal(err)
	}
	if dim != 768 || len(out) != len(ids)*dim {
		t.Fatalf("AlbertForward shape len=%d dim=%d ids=%d", len(out), dim, len(ids))
	}
	for i, v := range out {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			t.Fatalf("AlbertForward non-finite at %d: %v", i, v)
		}
	}
}

func TestPredictDurationsWhenAssetsAvailable(t *testing.T) {
	root := os.Getenv("KOKORO_GO_ASSETS")
	if root == "" {
		t.Skip("set KOKORO_GO_ASSETS to run duration predictor smoke test")
	}
	model, err := LoadModel(Assets{ConfigPath: filepath.Join(root, "config.json"), WeightsPath: filepath.Join(root, "kokoro-v1_0.koro")})
	if err != nil {
		t.Fatal(err)
	}
	voice, err := model.LoadVoice(filepath.Join(root, "voices"), "zm_yunxi")
	if err != nil {
		t.Fatal(err)
	}
	res, err := model.PredictDurations("a", voice, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Durations) != len(res.InputIDs) || res.Dim != 640 || len(res.Encoded) != len(res.InputIDs)*res.Dim {
		t.Fatalf("bad duration result: ids=%d durations=%d dim=%d encoded=%d", len(res.InputIDs), len(res.Durations), res.Dim, len(res.Encoded))
	}
	for i, d := range res.Durations {
		if d < 1 || d > 200 {
			t.Fatalf("duration[%d]=%d out of range; all=%v", i, d, res.Durations)
		}
	}
}

func TestBuildConditioningWhenAssetsAvailable(t *testing.T) {
	root := os.Getenv("KOKORO_GO_ASSETS")
	if root == "" {
		t.Skip("set KOKORO_GO_ASSETS to run conditioning smoke test")
	}
	model, err := LoadModel(Assets{ConfigPath: filepath.Join(root, "config.json"), WeightsPath: filepath.Join(root, "kokoro-v1_0.koro")})
	if err != nil {
		t.Fatal(err)
	}
	voice, err := model.LoadVoice(filepath.Join(root, "voices"), "zm_yunxi")
	if err != nil {
		t.Fatal(err)
	}
	cond, err := model.BuildConditioning("a", voice, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cond.Frames <= 0 || len(cond.Prosody) != cond.Frames*640 || len(cond.Text) != cond.Frames*512 {
		t.Fatalf("bad conditioning shape: frames=%d prosody=%d text=%d", cond.Frames, len(cond.Prosody), len(cond.Text))
	}
	for i, v := range cond.Text {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			t.Fatalf("conditioning text non-finite at %d: %v", i, v)
		}
	}
}

func TestPredictF0NWhenAssetsAvailable(t *testing.T) {
	root := os.Getenv("KOKORO_GO_ASSETS")
	if root == "" {
		t.Skip("set KOKORO_GO_ASSETS to run F0/N predictor smoke test")
	}
	model, err := LoadModel(Assets{ConfigPath: filepath.Join(root, "config.json"), WeightsPath: filepath.Join(root, "kokoro-v1_0.koro")})
	if err != nil {
		t.Fatal(err)
	}
	voice, err := model.LoadVoice(filepath.Join(root, "voices"), "zm_yunxi")
	if err != nil {
		t.Fatal(err)
	}
	cond, err := model.BuildConditioning("a", voice, 1)
	if err != nil {
		t.Fatal(err)
	}
	f0n, err := model.PredictF0N(cond, voice)
	if err != nil {
		t.Fatal(err)
	}
	if f0n.Frames <= cond.Frames || len(f0n.F0) != f0n.Frames || len(f0n.Noise) != f0n.Frames {
		t.Fatalf("bad F0/N shape: cond=%d frames=%d f0=%d noise=%d", cond.Frames, f0n.Frames, len(f0n.F0), len(f0n.Noise))
	}
	for i, v := range f0n.F0 {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			t.Fatalf("F0 non-finite at %d: %v", i, v)
		}
	}
}

func TestDecoderPreGeneratorWhenAssetsAvailable(t *testing.T) {
	root := os.Getenv("KOKORO_GO_ASSETS")
	if root == "" {
		t.Skip("set KOKORO_GO_ASSETS to run decoder smoke test")
	}
	model, err := LoadModel(Assets{ConfigPath: filepath.Join(root, "config.json"), WeightsPath: filepath.Join(root, "kokoro-v1_0.koro")})
	if err != nil {
		t.Fatal(err)
	}
	voice, err := model.LoadVoice(filepath.Join(root, "voices"), "zm_yunxi")
	if err != nil {
		t.Fatal(err)
	}
	cond, err := model.BuildConditioning("a", voice, 1)
	if err != nil {
		t.Fatal(err)
	}
	f0n, err := model.PredictF0N(cond, voice)
	if err != nil {
		t.Fatal(err)
	}
	feat, err := model.DecoderPreGenerator(cond, f0n, voice)
	if err != nil {
		t.Fatal(err)
	}
	if feat.Frames != cond.Frames*2 || len(feat.X) != 512*feat.Frames {
		t.Fatalf("bad decoder feature shape: cond=%d frames=%d len=%d", cond.Frames, feat.Frames, len(feat.X))
	}
	for i, v := range feat.X {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			t.Fatalf("decoder feature non-finite at %d: %v", i, v)
		}
	}
}

func TestSynthesizePhonemesWhenAssetsAvailable(t *testing.T) {
	root := os.Getenv("KOKORO_GO_ASSETS")
	if root == "" {
		t.Skip("set KOKORO_GO_ASSETS to run synthesis smoke test")
	}
	model, err := LoadModel(Assets{ConfigPath: filepath.Join(root, "config.json"), WeightsPath: filepath.Join(root, "kokoro-v1_0.koro")})
	if err != nil {
		t.Fatal(err)
	}
	voice, err := model.LoadVoice(filepath.Join(root, "voices"), "zm_yunxi")
	if err != nil {
		t.Fatal(err)
	}
	pcm, err := model.SynthesizePhonemes("a", voice, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(pcm) == 0 {
		t.Fatal("empty pcm")
	}
	for i, v := range pcm {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			t.Fatalf("pcm non-finite at %d: %v", i, v)
		}
	}
}
