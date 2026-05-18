package needleruntime

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/needledata"
)

func writeTestWeight(t testing.TB, path string) {
	t.Helper()
	buf := make([]byte, 32+4*8+3*8+3*4)
	copy(buf[0:8], []byte(WeightMagic))
	binary.LittleEndian.PutUint32(buf[8:12], WeightVersion)
	binary.LittleEndian.PutUint32(buf[12:16], 4)
	binary.LittleEndian.PutUint32(buf[16:20], 8)
	binary.LittleEndian.PutUint32(buf[20:24], 3)
	binary.LittleEndian.PutUint32(buf[24:28], 0)
	binary.LittleEndian.PutUint32(buf[28:32], 32)
	for i := 32; i < 32+4*8; i++ {
		buf[i] = byte(int8(1))
	}
	for i := 32 + 4*8; i < 32+4*8+3*8; i++ {
		buf[i] = byte(int8(1))
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatalf("write test weight: %v", err)
	}
}

func writePredictableWeight(t testing.TB, path string) {
	t.Helper()
	buf := make([]byte, 32+4*2+2*2+2*4)
	copy(buf[0:8], []byte(WeightMagic))
	binary.LittleEndian.PutUint32(buf[8:12], WeightVersion)
	binary.LittleEndian.PutUint32(buf[12:16], 4)
	binary.LittleEndian.PutUint32(buf[16:20], 2)
	binary.LittleEndian.PutUint32(buf[20:24], 2)
	binary.LittleEndian.PutUint32(buf[24:28], 0)
	binary.LittleEndian.PutUint32(buf[28:32], 32)
	copy(buf[32:40], []byte{0, 0, 8, 0, 0, 0, 1, 0})
	copy(buf[40:44], []byte{1, 0, 0, 1})
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatalf("write predictable weight: %v", err)
	}
}

func writeSparseHashWeight(t testing.TB, path string) {
	t.Helper()
	buf := make([]byte, 32+2*4+2*4)
	copy(buf[0:8], []byte(WeightMagic))
	binary.LittleEndian.PutUint32(buf[8:12], WeightVersion)
	binary.LittleEndian.PutUint32(buf[12:16], 4)
	binary.LittleEndian.PutUint32(buf[16:20], 4)
	binary.LittleEndian.PutUint32(buf[20:24], 2)
	binary.LittleEndian.PutUint32(buf[24:28], WeightFlagSparseHashHead)
	binary.LittleEndian.PutUint32(buf[28:32], 32)
	copy(buf[32:40], []byte{8, 0, 0, 0, 0, 8, 0, 0})
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatalf("write sparse hash weight: %v", err)
	}
}

func TestHashingTokenizerEncodesKnownHashBuckets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokenizer.json")
	if err := os.WriteFile(path, []byte(`{"model":{"vocab":{"__h0":0,"__h1":1,"__h2":2,"__h3":3,"__h4":4,"__h5":5,"__h6":6,"__h7":7}}}`), 0o644); err != nil {
		t.Fatalf("write tokenizer: %v", err)
	}
	tok, err := LoadSimpleTokenizer(path)
	if err != nil {
		t.Fatalf("LoadSimpleTokenizer returned error: %v", err)
	}
	ids := tok.Encode("Looks good, continue")
	if len(ids) != 3 {
		t.Fatalf("Encode produced %d ids, want 3: %#v", len(ids), ids)
	}
	for _, id := range ids {
		if id < 0 || id > 7 {
			t.Fatalf("hashed id %d outside vocab range: %#v", id, ids)
		}
	}
}

func TestHashingTokenizerEncodeRequestMatchesRenderedPrompt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokenizer.json")
	if err := os.WriteFile(path, []byte(`{"model":{"vocab":{"__h0":0,"__h1":1,"__h2":2,"__h3":3,"__h4":4,"__h5":5,"__h6":6,"__h7":7,"__h8":8,"__h9":9,"__h10":10,"__h11":11,"__h12":12,"__h13":13,"__h14":14,"__h15":15}}}`), 0o644); err != nil {
		t.Fatalf("write tokenizer: %v", err)
	}
	tok, err := LoadSimpleTokenizer(path)
	if err != nil {
		t.Fatalf("LoadSimpleTokenizer returned error: %v", err)
	}
	req := Request{Task: needledata.EventWorkflowReview, Text: "Looks good, continue", Choices: []string{"confirm", "cancel"}}
	viaPrompt := tok.Encode(RenderPrompt(req))
	viaRequest := tok.EncodeRequestInto(nil, req)
	if len(viaPrompt) != len(viaRequest) {
		t.Fatalf("EncodeRequest len = %d, want %d: prompt=%#v request=%#v", len(viaRequest), len(viaPrompt), viaPrompt, viaRequest)
	}
	for i := range viaPrompt {
		if viaPrompt[i] != viaRequest[i] {
			t.Fatalf("id[%d] = %d, want %d: prompt=%#v request=%#v", i, viaRequest[i], viaPrompt[i], viaPrompt, viaRequest)
		}
	}
}

func TestInspectTokenizerDetectsCompleteHashVocabBeyondSamples(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"model":{"vocab":{`)
	for i := 0; i < 12; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(fmt.Sprintf("%q:%d", fmt.Sprintf("__h%d", i), i))
	}
	b.WriteString(`}}}`)
	vocabSize, maxID, hashing, samples, err := inspectTokenizerJSON([]byte(b.String()))
	if err != nil {
		t.Fatalf("inspectTokenizerJSON returned error: %v", err)
	}
	if vocabSize != 12 || maxID != 11 || !hashing {
		t.Fatalf("inspectTokenizerJSON = vocab=%d maxID=%d hashing=%v samples=%#v, want complete hash vocab", vocabSize, maxID, hashing, samples)
	}
}

func TestInspectTokenizerRejectsIncompleteHashVocab(t *testing.T) {
	vocabSize, maxID, hashing, samples, err := inspectTokenizerJSON([]byte(`{"model":{"vocab":{"__h0":0,"__h2":2}}}`))
	if err != nil {
		t.Fatalf("inspectTokenizerJSON returned error: %v", err)
	}
	if vocabSize != 2 || maxID != 2 || hashing {
		t.Fatalf("inspectTokenizerJSON = vocab=%d maxID=%d hashing=%v samples=%#v, want incomplete hash vocab", vocabSize, maxID, hashing, samples)
	}
}

func TestSparseHashHeadRejectsIncompleteHashVocabAtLoad(t *testing.T) {
	weights := &Q8Weights{Header: &WeightHeader{VocabSize: 3, HiddenSize: 3, NumLabels: 1, Flags: WeightFlagSparseHashHead}, Head: []int8{1, 1, 1}, Bias: []float32{0}}
	_, err := NewQ8Predictor(&SimpleTokenizer{Vocab: map[string]int{"__h0": 0, "__h2": 2}, HashDim: 3}, []string{"confirm"}, weights)
	if err == nil {
		t.Fatal("expected incomplete hash vocab to be rejected")
	}
}

func TestRuntimeDisabledDoesNotPredict(t *testing.T) {
	r, err := New(Options{Enabled: false})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	decision, ok, err := r.Predict(context.Background(), Request{Task: needledata.EventWorkflowReview, Text: "continue"})
	if err != nil || ok || decision.Name != "" {
		t.Fatalf("Predict = %#v %v %v, want disabled no-op", decision, ok, err)
	}
	decision, ok, reason, err := r.PredictDetailed(context.Background(), Request{Task: needledata.EventWorkflowReview, Text: "continue"})
	if err != nil || ok || decision.Name != "" || reason != RejectReasonRuntimeDisabled {
		t.Fatalf("PredictDetailed = %#v %v %q %v, want disabled rejection", decision, ok, reason, err)
	}
}

func TestLoadManifest(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"format":"maclaw-needle","version":1,"runtime":"go","tasks":["workflow_review"],"weight_path":"needle.q8","weight_header":{"vocab_size":4,"hidden_size":8,"num_labels":3,"flags":0,"data_offset":32}}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	loaded, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest returned error: %v", err)
	}
	if loaded.Runtime != "go" || loaded.WeightPath != "needle.q8" || loaded.WeightHeader == nil || loaded.WeightHeader.HiddenSize != 8 {
		t.Fatalf("manifest = %#v", loaded)
	}
}

func TestInspectWarnsWhenManifestWeightHeaderMismatchesWeight(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"format":"maclaw-needle","version":1,"runtime":"go","tasks":["workflow_review"],"weight_path":"needle.q8","tokenizer":"tokenizer.json","labels":"labels.json","weight_header":{"vocab_size":4,"hidden_size":99,"num_labels":3,"flags":0,"data_offset":32}}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), []byte(`{"model":{"vocab":{"hello":0,"world":1}}}`), 0o644); err != nil {
		t.Fatalf("write tokenizer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "labels.json"), []byte(`["confirm","other","skip"]`), 0o644); err != nil {
		t.Fatalf("write labels: %v", err)
	}
	writeTestWeight(t, filepath.Join(dir, "needle.q8"))
	got := Inspect(Options{Enabled: true, ModelPath: dir})
	if got.Usable || len(got.Warnings) == 0 {
		t.Fatalf("Inspect = %#v, want manifest weight_header mismatch warning", got)
	}
	if _, err := New(Options{Enabled: true, ModelPath: dir}); err == nil {
		t.Fatal("New should reject manifest weight_header mismatch")
	}
}

func TestInspectModelPath(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"format":"maclaw-needle","version":1,"runtime":"go","tasks":["workflow_review"],"weight_path":"needle.q8","tokenizer":"tokenizer.json","labels":"labels.json"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), []byte(`{"model":{"vocab":{"hello":0,"world":1}}}`), 0o644); err != nil {
		t.Fatalf("write tokenizer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "labels.json"), []byte(`["confirm","other","skip"]`), 0o644); err != nil {
		t.Fatalf("write labels: %v", err)
	}
	writeTestWeight(t, filepath.Join(dir, "needle.q8"))
	got := Inspect(Options{Enabled: true, ModelPath: dir})
	if !got.Usable || got.Manifest == nil || got.Mode != "q8_linear" {
		t.Fatalf("Inspect = %#v", got)
	}
	if got.Weight == nil || got.Weight.Header == nil || got.Weight.Header.HiddenSize != 8 {
		t.Fatalf("Inspect weight = %#v", got.Weight)
	}
	if got.Tokenizer == nil || got.Tokenizer.VocabSize != 2 {
		t.Fatalf("Inspect tokenizer = %#v", got.Tokenizer)
	}
	if got.Labels == nil || len(got.Labels.Labels) != 3 {
		t.Fatalf("Inspect labels = %#v", got.Labels)
	}
}

func TestInspectWarnsWhenArtifactFileMissing(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"format":"maclaw-needle","version":1,"runtime":"go","tasks":["workflow_review"],"weight_path":"missing.q8"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	got := Inspect(Options{Enabled: true, ModelPath: dir})
	if got.Usable || len(got.Warnings) == 0 {
		t.Fatalf("Inspect = %#v, want missing file warning", got)
	}
}

func TestRuntimeReturnsErrorForInvalidArtifact(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"format":"maclaw-needle","version":1,"runtime":"go","tasks":["workflow_review"],"weight_path":"missing.q8","tokenizer":"tokenizer.json","labels":"labels.json"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), []byte(`{"model":{"vocab":{"<unk>":0}}}`), 0o644); err != nil {
		t.Fatalf("write tokenizer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "labels.json"), []byte(`["confirm"]`), 0o644); err != nil {
		t.Fatalf("write labels: %v", err)
	}
	if _, err := New(Options{Enabled: true, ModelPath: dir}); err == nil {
		t.Fatal("New should reject invalid explicit artifact")
	}
}

func TestInspectWarnsOnInvalidWeightHeader(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"format":"maclaw-needle","version":1,"runtime":"go","tasks":["workflow_review"],"weight_path":"needle.q8"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "needle.q8"), []byte("bad"), 0o644); err != nil {
		t.Fatalf("write bad weight: %v", err)
	}
	got := Inspect(Options{Enabled: true, ModelPath: dir})
	if got.Usable || len(got.Warnings) == 0 || got.Weight == nil {
		t.Fatalf("Inspect = %#v, want invalid header warning", got)
	}
}

func TestRuntimeRejectsWeightSHA256Mismatch(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"format":"maclaw-needle","version":1,"runtime":"go","tasks":["workflow_review"],"weight_path":"needle.q8","weight_sha256":"deadbeef","tokenizer":"tokenizer.json","labels":"labels.json"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), []byte(`{"model":{"vocab":{"hello":0,"world":1}}}`), 0o644); err != nil {
		t.Fatalf("write tokenizer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "labels.json"), []byte(`["confirm","other","skip"]`), 0o644); err != nil {
		t.Fatalf("write labels: %v", err)
	}
	writeTestWeight(t, filepath.Join(dir, "needle.q8"))
	got := Inspect(Options{Enabled: true, ModelPath: dir})
	if got.Usable || len(got.Warnings) == 0 {
		t.Fatalf("Inspect = %#v, want sha warning", got)
	}
	if _, err := New(Options{Enabled: true, ModelPath: dir}); err == nil {
		t.Fatal("New should reject weight sha mismatch")
	}
}

func TestRuntimeRejectsWeightSizeMismatch(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"format":"maclaw-needle","version":1,"runtime":"go","tasks":["workflow_review"],"weight_path":"needle.q8","tokenizer":"tokenizer.json","labels":"labels.json"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), []byte(`{"model":{"vocab":{"hello":0,"world":1}}}`), 0o644); err != nil {
		t.Fatalf("write tokenizer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "labels.json"), []byte(`["confirm","other","skip"]`), 0o644); err != nil {
		t.Fatalf("write labels: %v", err)
	}
	writeTestWeight(t, filepath.Join(dir, "needle.q8"))
	f, err := os.OpenFile(filepath.Join(dir, "needle.q8"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open weight: %v", err)
	}
	if _, err := f.Write([]byte{1}); err != nil {
		_ = f.Close()
		t.Fatalf("append weight: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close weight: %v", err)
	}
	got := Inspect(Options{Enabled: true, ModelPath: dir})
	if got.Usable || len(got.Warnings) == 0 {
		t.Fatalf("Inspect = %#v, want size warning", got)
	}
	if _, err := New(Options{Enabled: true, ModelPath: dir}); err == nil {
		t.Fatal("New should reject weight size mismatch")
	}
}

func TestInspectWarnsOnInvalidTokenizer(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"format":"maclaw-needle","version":1,"runtime":"go","tasks":["workflow_review"],"weight_path":"needle.q8","tokenizer":"tokenizer.json"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	writeTestWeight(t, filepath.Join(dir, "needle.q8"))
	if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), []byte(`{"model":{}}`), 0o644); err != nil {
		t.Fatalf("write tokenizer: %v", err)
	}
	got := Inspect(Options{Enabled: true, ModelPath: dir})
	if got.Usable || len(got.Warnings) == 0 || got.Tokenizer == nil {
		t.Fatalf("Inspect = %#v, want tokenizer warning", got)
	}
}

func TestInspectWarnsWhenTokenizerIDsExceedWeights(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"format":"maclaw-needle","version":1,"runtime":"go","tasks":["workflow_review"],"weight_path":"needle.q8","tokenizer":"tokenizer.json","labels":"labels.json"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), []byte(`{"model":{"vocab":{"<unk>":0,"too_big":9}}}`), 0o644); err != nil {
		t.Fatalf("write tokenizer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "labels.json"), []byte(`["confirm","other","skip"]`), 0o644); err != nil {
		t.Fatalf("write labels: %v", err)
	}
	writeTestWeight(t, filepath.Join(dir, "needle.q8"))
	got := Inspect(Options{Enabled: true, ModelPath: dir})
	if got.Usable || len(got.Warnings) == 0 || got.Tokenizer == nil || got.Tokenizer.MaxID != 9 {
		t.Fatalf("Inspect = %#v, want tokenizer/weight vocab warning", got)
	}
	if _, err := New(Options{Enabled: true, ModelPath: dir}); err == nil {
		t.Fatal("New should reject tokenizer ids beyond weight vocab")
	}
}

func TestInspectRejectsSparseHashHeadWithoutHashTokenizer(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"format":"maclaw-needle","version":1,"runtime":"go","tasks":["workflow_review"],"weight_path":"needle.q8","tokenizer":"tokenizer.json","labels":"labels.json"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), []byte(`{"model":{"vocab":{"hello":0,"world":1,"other":2,"more":3}}}`), 0o644); err != nil {
		t.Fatalf("write tokenizer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "labels.json"), []byte(`["confirm","cancel"]`), 0o644); err != nil {
		t.Fatalf("write labels: %v", err)
	}
	writeSparseHashWeight(t, filepath.Join(dir, "needle.q8"))
	got := Inspect(Options{Enabled: true, ModelPath: dir})
	if got.Usable || len(got.Warnings) == 0 {
		t.Fatalf("Inspect = %#v, want sparse tokenizer warning", got)
	}
	if _, err := New(Options{Enabled: true, ModelPath: dir}); err == nil {
		t.Fatal("New should reject sparse hash head without hashing tokenizer")
	}
}

func TestRuntimeSparseHashHeadPredictsFromArtifact(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"format":"maclaw-needle","version":1,"runtime":"go","tasks":["workflow_review"],"weight_path":"needle.q8","tokenizer":"tokenizer.json","labels":"labels.json"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), []byte(`{"model":{"vocab":{"__h0":0,"__h1":1,"__h2":2,"__h3":3}}}`), 0o644); err != nil {
		t.Fatalf("write tokenizer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "labels.json"), []byte(`["confirm","cancel"]`), 0o644); err != nil {
		t.Fatalf("write labels: %v", err)
	}
	writeSparseHashWeight(t, filepath.Join(dir, "needle.q8"))
	r, err := New(Options{Enabled: true, ModelPath: dir, MinConf: 0.1})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	decision, ok, err := r.Predict(context.Background(), Request{Task: needledata.EventWorkflowReview, Text: "anything", Choices: []string{"confirm", "cancel"}})
	if err != nil {
		t.Fatalf("Predict returned error: %v", err)
	}
	if !ok || decision.Source != "needle_q8" || decision.Name == "" {
		t.Fatalf("Predict = %#v ok=%v, want accepted sparse q8 decision", decision, ok)
	}
}

func TestRuntimeSkipsTasksOutsideManifest(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"format":"maclaw-needle","version":1,"runtime":"go","tasks":["workflow_review"],"weight_path":"needle.q8","tokenizer":"tokenizer.json","labels":"labels.json"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), []byte(`{"model":{"vocab":{"<unk>":0,"continue":1,"stop":2,"workflow_review":3}}}`), 0o644); err != nil {
		t.Fatalf("write tokenizer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "labels.json"), []byte(`["confirm","cancel"]`), 0o644); err != nil {
		t.Fatalf("write labels: %v", err)
	}
	writePredictableWeight(t, filepath.Join(dir, "needle.q8"))
	r, err := New(Options{Enabled: true, ModelPath: dir, MinConf: 0.1})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	decision, ok, reason, err := r.PredictDetailed(context.Background(), Request{Task: needledata.EventIntentGate, Text: "continue", Choices: []string{"confirm", "cancel"}})
	if err != nil {
		t.Fatalf("PredictDetailed returned error: %v", err)
	}
	if ok || decision.Name != "" || reason != RejectReasonUnsupportedTask {
		t.Fatalf("PredictDetailed = %#v ok=%v reason=%q, want unsupported task skipped", decision, ok, reason)
	}
}

func TestRuntimeEncode(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"format":"maclaw-needle","version":1,"runtime":"go","tasks":["workflow_review"],"weight_path":"needle.q8","tokenizer":"tokenizer.json","labels":"labels.json"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), []byte(`{"model":{"vocab":{"<unk>":0,"Task":1,"workflow_review":2,"Choices":3}}}`), 0o644); err != nil {
		t.Fatalf("write tokenizer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "labels.json"), []byte(`["confirm","other","skip"]`), 0o644); err != nil {
		t.Fatalf("write labels: %v", err)
	}
	writeTestWeight(t, filepath.Join(dir, "needle.q8"))
	r, err := New(Options{Enabled: true, ModelPath: dir})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	encoded, err := r.Encode(Request{Task: needledata.EventWorkflowReview, Text: "continue", Choices: []string{"confirm", "other"}})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if len(encoded.TokenIDs) == 0 || encoded.Prompt == "" {
		t.Fatalf("encoded = %#v", encoded)
	}
}

func TestRuntimeQ8PredictsFromArtifact(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"format":"maclaw-needle","version":1,"runtime":"go","tasks":["workflow_review"],"weight_path":"needle.q8","tokenizer":"tokenizer.json","labels":"labels.json"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), []byte(`{"model":{"vocab":{"<unk>":0,"continue":1,"stop":2,"workflow_review":3}}}`), 0o644); err != nil {
		t.Fatalf("write tokenizer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "labels.json"), []byte(`["confirm","cancel"]`), 0o644); err != nil {
		t.Fatalf("write labels: %v", err)
	}
	writePredictableWeight(t, filepath.Join(dir, "needle.q8"))
	r, err := New(Options{Enabled: true, ModelPath: dir, MinConf: 0.5})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	decision, ok, err := r.Predict(context.Background(), Request{Task: needledata.EventWorkflowReview, Text: "continue", Choices: []string{"confirm", "cancel"}})
	if err != nil {
		t.Fatalf("Predict returned error: %v", err)
	}
	if !ok || decision.Name != "confirm" || decision.Source != "needle_q8" {
		t.Fatalf("Predict = %#v ok=%v, want q8 confirm", decision, ok)
	}
}

func TestRuntimePredictsWorkflowReview(t *testing.T) {
	r, err := New(Options{Enabled: true})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	decision, ok, err := r.Predict(context.Background(), Request{Task: needledata.EventWorkflowReview, Text: "looks good, continue"})
	if err != nil {
		t.Fatalf("Predict returned error: %v", err)
	}
	if !ok || decision.Name != "confirm" || decision.Source == "" {
		t.Fatalf("Predict = %#v ok=%v, want confirm", decision, ok)
	}
}

func TestRuntimeHonorsConfidenceThreshold(t *testing.T) {
	r, err := New(Options{Enabled: true, MinConf: 0.99})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	decision, ok, err := r.Predict(context.Background(), Request{Task: needledata.EventWorkflowReview, Text: "continue"})
	if err != nil {
		t.Fatalf("Predict returned error: %v", err)
	}
	if ok || decision.Name != "confirm" {
		t.Fatalf("Predict = %#v ok=%v, want below-threshold shadow decision", decision, ok)
	}
}

func writeCollectionChildArtifact(t testing.TB, dir, task string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir child artifact: %v", err)
	}
	manifest := fmt.Sprintf(`{"format":"maclaw-needle","version":1,"runtime":"go","tasks":[%q],"weight_path":"needle.q8","tokenizer":"tokenizer.json","labels":"labels.json"}`, task)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), []byte(`{"model":{"vocab":{"<unk>":0,"continue":1,"stop":2,"workflow_review":3}}}`), 0o644); err != nil {
		t.Fatalf("write tokenizer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "labels.json"), []byte(`["confirm","cancel"]`), 0o644); err != nil {
		t.Fatalf("write labels: %v", err)
	}
	writePredictableWeight(t, filepath.Join(dir, "needle.q8"))
}

func TestRuntimeLoadsCollectionAndDelegatesByTask(t *testing.T) {
	root := t.TempDir()
	writeCollectionChildArtifact(t, filepath.Join(root, "tasks", "workflow_review"), needledata.EventWorkflowReview)
	writeCollectionChildArtifact(t, filepath.Join(root, "tasks", "intent_gate"), needledata.EventIntentGate)
	collection := `{"format":"maclaw-needle-collection","version":1,"tasks":{"workflow_review":{"path":"tasks/workflow_review","records":3,"labels":["confirm","cancel"]},"intent_gate":{"path":"tasks/intent_gate","records":2,"labels":["confirm","cancel"]}}}`
	if err := os.WriteFile(filepath.Join(root, "collection.json"), []byte(collection), 0o644); err != nil {
		t.Fatalf("write collection: %v", err)
	}
	r, err := New(Options{Enabled: true, ModelPath: root, MinConf: 0.1})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	decision, ok, err := r.Predict(context.Background(), Request{Task: needledata.EventWorkflowReview, Text: "continue", Choices: []string{"confirm", "cancel"}})
	if err != nil {
		t.Fatalf("Predict workflow returned error: %v", err)
	}
	if !ok || decision.Name != "confirm" || decision.Source != "needle_q8" {
		t.Fatalf("workflow Predict = %#v ok=%v, want q8 confirm", decision, ok)
	}
	decision, ok, err = r.Predict(context.Background(), Request{Task: needledata.EventIntentGate, Text: "continue", Choices: []string{"confirm", "cancel"}})
	if err != nil {
		t.Fatalf("Predict intent returned error: %v", err)
	}
	if !ok || decision.Name != "confirm" || decision.Source != "needle_q8" {
		t.Fatalf("intent Predict = %#v ok=%v, want q8 confirm", decision, ok)
	}
	decision, ok, err = r.Predict(context.Background(), Request{Task: "missing_task", Text: "continue"})
	if err != nil || ok || decision.Name != "" {
		t.Fatalf("missing task Predict = %#v ok=%v err=%v, want skipped", decision, ok, err)
	}
}

func TestRuntimeCollectionRejectsEscapingTaskPath(t *testing.T) {
	root := t.TempDir()
	collection := `{"format":"maclaw-needle-collection","version":1,"tasks":{"workflow_review":{"path":"../outside"}}}`
	if err := os.WriteFile(filepath.Join(root, "collection.json"), []byte(collection), 0o644); err != nil {
		t.Fatalf("write collection: %v", err)
	}
	if _, err := New(Options{Enabled: true, ModelPath: root}); err == nil {
		t.Fatal("New should reject collection task path outside root")
	}
	inspect := Inspect(Options{Enabled: true, ModelPath: root})
	if inspect.Usable || len(inspect.Warnings) == 0 {
		t.Fatalf("Inspect = %#v, want escaping path warning", inspect)
	}
}

func TestResolveCollectionTaskPathSupportsCollectionJSONPath(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "workflow")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}
	collection := `{"format":"maclaw-needle-collection","version":1,"tasks":{"workflow_review":{"path":"workflow"}}}`
	collectionPath := filepath.Join(root, "collection.json")
	if err := os.WriteFile(collectionPath, []byte(collection), 0o644); err != nil {
		t.Fatalf("write collection: %v", err)
	}
	path, ok, err := ResolveCollectionTaskPath(collectionPath, needledata.EventWorkflowReview)
	if err != nil || !ok || path != child {
		t.Fatalf("ResolveCollectionTaskPath = %q ok=%v err=%v, want %q", path, ok, err, child)
	}
}

func TestRuntimeCollectionLoadsChildLazily(t *testing.T) {
	root := t.TempDir()
	collection := `{"format":"maclaw-needle-collection","version":1,"tasks":{"workflow_review":{"path":"workflow"}}}`
	if err := os.WriteFile(filepath.Join(root, "collection.json"), []byte(collection), 0o644); err != nil {
		t.Fatalf("write collection: %v", err)
	}
	r, err := New(Options{Enabled: true, ModelPath: root, MinConf: 0.1})
	if err != nil {
		t.Fatalf("New returned error before child exists: %v", err)
	}
	if _, ok, err := r.Predict(context.Background(), Request{Task: needledata.EventWorkflowReview, Text: "continue"}); err == nil || ok {
		t.Fatalf("Predict before child exists ok=%v err=%v, want lazy load error", ok, err)
	}
	writeCollectionChildArtifact(t, filepath.Join(root, "workflow"), needledata.EventWorkflowReview)
	decision, ok, err := r.Predict(context.Background(), Request{Task: needledata.EventWorkflowReview, Text: "continue", Choices: []string{"confirm", "cancel"}})
	if err != nil {
		t.Fatalf("Predict after child exists returned error: %v", err)
	}
	if !ok || decision.Name != "confirm" {
		t.Fatalf("Predict after child exists = %#v ok=%v, want confirm", decision, ok)
	}
}

func BenchmarkRuntimeCollectionPredictCached(b *testing.B) {
	root := b.TempDir()
	child := filepath.Join(root, "workflow")
	if err := os.MkdirAll(child, 0o755); err != nil {
		b.Fatalf("mkdir child: %v", err)
	}
	manifest := `{"format":"maclaw-needle","version":1,"runtime":"go","tasks":["workflow_review"],"weight_path":"needle.q8","tokenizer":"tokenizer.json","labels":"labels.json"}`
	if err := os.WriteFile(filepath.Join(child, "manifest.json"), []byte(manifest), 0o644); err != nil {
		b.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(child, "tokenizer.json"), []byte(`{"model":{"vocab":{"__h0":0,"__h1":1,"__h2":2,"__h3":3}}}`), 0o644); err != nil {
		b.Fatalf("write tokenizer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(child, "labels.json"), []byte(`["confirm","cancel"]`), 0o644); err != nil {
		b.Fatalf("write labels: %v", err)
	}
	writeSparseHashWeight(b, filepath.Join(child, "needle.q8"))
	collection := `{"format":"maclaw-needle-collection","version":1,"tasks":{"workflow_review":{"path":"workflow"}}}`
	if err := os.WriteFile(filepath.Join(root, "collection.json"), []byte(collection), 0o644); err != nil {
		b.Fatalf("write collection: %v", err)
	}
	r, err := New(Options{Enabled: true, ModelPath: root, MinConf: 0.1})
	if err != nil {
		b.Fatalf("New returned error: %v", err)
	}
	req := Request{Task: needledata.EventWorkflowReview, Text: "continue", Choices: []string{"confirm", "cancel"}}
	if _, _, err := r.Predict(context.Background(), req); err != nil {
		b.Fatalf("warm predict: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := r.Predict(context.Background(), req); err != nil {
			b.Fatal(err)
		}
	}
}

func TestInspectCollectionIncludesTaskDetails(t *testing.T) {
	root := t.TempDir()
	writeCollectionChildArtifact(t, filepath.Join(root, "workflow"), needledata.EventWorkflowReview)
	collection := `{"format":"maclaw-needle-collection","version":1,"dim":4,"tasks":{"workflow_review":{"path":"workflow","records":7,"labels":["confirm","cancel"],"min_conf":0.42}}}`
	if err := os.WriteFile(filepath.Join(root, "collection.json"), []byte(collection), 0o644); err != nil {
		t.Fatalf("write collection: %v", err)
	}
	inspect := Inspect(Options{Enabled: true, ModelPath: root, MinConf: 0.1})
	if !inspect.Usable || inspect.Mode != "collection" || inspect.Collection == nil {
		t.Fatalf("Inspect = %#v, want usable collection details", inspect)
	}
	task := inspect.Collection.Tasks[needledata.EventWorkflowReview]
	if task.Path != "workflow" || task.Records != 7 || task.ResolvedPath == "" || task.MinConf != 0.42 || task.RuntimeInspect == nil || !task.RuntimeInspect.Usable || task.RuntimeInspect.MinConf != 0.42 {
		t.Fatalf("collection task inspect = %#v", task)
	}
	if len(task.Labels) != 2 || task.Labels[0] != "confirm" {
		t.Fatalf("collection task labels = %#v", task.Labels)
	}
}

func TestRuntimeRejectsPredictionsOutsideChoices(t *testing.T) {
	r, err := New(Options{Enabled: true, MinConf: 0.1})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	decision, ok, reason, err := r.PredictDetailed(context.Background(), Request{Task: needledata.EventWorkflowReview, Text: "looks good, continue", Choices: []string{"cancel", "skip"}})
	if err != nil {
		t.Fatalf("PredictDetailed returned error: %v", err)
	}
	if ok || decision.Name != "confirm" || reason != RejectReasonOutsideChoices {
		t.Fatalf("PredictDetailed = %#v ok=%v reason=%q, want shadow confirm rejected by choices", decision, ok, reason)
	}
}

func TestRuntimePredictDetailedRejectReasons(t *testing.T) {
	r, err := New(Options{Enabled: true, MinConf: 0.99})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	decision, ok, reason, err := r.PredictDetailed(context.Background(), Request{Task: needledata.EventWorkflowReview, Text: "looks good, continue", Choices: []string{"confirm", "cancel"}})
	if err != nil {
		t.Fatalf("PredictDetailed returned error: %v", err)
	}
	if ok || decision.Name != "confirm" || reason != RejectReasonBelowMinConf {
		t.Fatalf("PredictDetailed = %#v ok=%v reason=%q, want below confidence rejection", decision, ok, reason)
	}
}
