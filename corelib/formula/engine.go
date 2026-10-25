// Package formula recognizes a printed formula image as LaTeX.
//
// The graphs are the published FormulaNet ONNX export (a PP-FormulaNet-S
// distillation). Weights are Sicheng Mao's, license AGPL-3.0. They are
// downloaded at runtime from the GitHub release Model_Release, then from
// the hub mirror when GitHub cannot be reached. They are not embedded.
// This package is original Go against that ONNX input/output contract.
package formula

import (
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/onnxrt"
)

const (
	defaultMaxTokens = 256
	tokenCap         = 1024
)

// Engine runs FormulaNet: one encoder pass, then greedy decoding with the
// cached-key decoder. Recognize is safe for concurrent callers.
type Engine struct {
	mu        sync.Mutex
	enc       *onnxrt.Graph
	dec       *onnxrt.Graph
	past      *onnxrt.Graph
	vocab     []string
	maxTokens int

	encIn, encOut                  string
	decIDs, decHidden, decLogits   string
	decPDK, decPDV, decPEK, decPEV []string
	pastIDs, pastLogits            string
	pastDK, pastDV, pastEK, pastEV []string
	pastPDK, pastPDV               []string
	layers                         int
}

// Open loads the FormulaNet graphs and tokenizer from dir. It does not
// download. VerifyLoaded checks the pin first, so a same-size corrupt file
// is refused before the graph runs. EnsureModels fetches the release names.
// A directory that still has the older Hugging Face names is loaded when
// those bytes match the pin.
func Open(dir string) (*Engine, error) {
	if err := VerifyLoaded(dir); err != nil {
		return nil, err
	}
	encPath, err := pinnedAsset(dir, fileEncoder)
	if err != nil {
		return nil, err
	}
	decPath, err := pinnedAsset(dir, fileDecoder)
	if err != nil {
		return nil, err
	}
	pastPath, err := pinnedAsset(dir, filePast)
	if err != nil {
		return nil, err
	}
	tokPath, err := pinnedAsset(dir, fileTokenizer)
	if err != nil {
		return nil, err
	}
	enc, err := onnxrt.LoadGraph(encPath)
	if err != nil {
		return nil, fmt.Errorf("formula: encoder: %w", err)
	}
	dec, err := onnxrt.LoadGraph(decPath)
	if err != nil {
		return nil, fmt.Errorf("formula: decoder: %w", err)
	}
	past, err := onnxrt.LoadGraph(pastPath)
	if err != nil {
		return nil, fmt.Errorf("formula: decoder_with_past: %w", err)
	}
	vocab, err := loadTokenizer(tokPath)
	if err != nil {
		return nil, err
	}
	e := &Engine{
		enc:       enc,
		dec:       dec,
		past:      past,
		vocab:     vocab,
		maxTokens: defaultMaxTokens,
	}
	if err := e.bind(); err != nil {
		return nil, err
	}
	return e, nil
}

func pinnedAsset(dir, name string) (string, error) {
	a, ok := assetNamed(name)
	if !ok {
		return "", fmt.Errorf("formula: unknown asset %s", name)
	}
	return pinnedFile(dir, a), nil
}

// EnsureAndOpen downloads any missing FormulaNet file (GitHub release, then
// the hub mirror) and loads it. dir and hubURL follow EnsureModels.
func EnsureAndOpen(ctx context.Context, dir, hubURL string) (*Engine, error) {
	if dir == "" {
		var err error
		dir, err = DefaultModelDir()
		if err != nil {
			return nil, err
		}
	}
	if err := EnsureModels(ctx, dir, hubURL); err != nil {
		return nil, err
	}
	return Open(dir)
}

// SetMaxTokens caps the number of LaTeX tokens produced after the BOS token.
// The default is 256. Values above 1024 are clamped.
func (e *Engine) SetMaxTokens(n int) {
	if n < 1 {
		n = 1
	}
	if n > tokenCap {
		n = tokenCap
	}
	e.mu.Lock()
	e.maxTokens = n
	e.mu.Unlock()
}

// Recognize preprocesses img and returns the greedy LaTeX string.
func (e *Engine) Recognize(img image.Image) (string, error) {
	if e == nil {
		return "", fmt.Errorf("formula: nil engine")
	}
	pix, err := preprocess(img)
	if err != nil {
		return "", err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.decode(pix)
}

// RecognizeReader decodes a PNG, JPEG, or GIF and recognizes it.
func (e *Engine) RecognizeReader(r io.Reader) (string, error) {
	img, err := DecodeImage(r)
	if err != nil {
		return "", err
	}
	return e.Recognize(img)
}

func (e *Engine) decode(pix *onnxrt.Tensor) (string, error) {
	encOut, err := e.enc.Run(map[string]*onnxrt.Tensor{e.encIn: pix})
	if err != nil {
		return "", fmt.Errorf("formula: encoder: %w", err)
	}
	hidden := encOut[e.encOut]
	if hidden == nil {
		return "", fmt.Errorf("formula: encoder did not produce %s", e.encOut)
	}
	step, err := e.dec.Run(map[string]*onnxrt.Tensor{
		e.decIDs:    onnxrt.IntFrom([]int64{idBOS}, 1, 1),
		e.decHidden: hidden,
	})
	if err != nil {
		return "", fmt.Errorf("formula: decoder: %w", err)
	}
	tok, err := argmaxLast(step[e.decLogits])
	if err != nil {
		return "", err
	}
	decK := make([]*onnxrt.Tensor, e.layers)
	decV := make([]*onnxrt.Tensor, e.layers)
	encK := make([]*onnxrt.Tensor, e.layers)
	encV := make([]*onnxrt.Tensor, e.layers)
	for i := 0; i < e.layers; i++ {
		decK[i] = step[e.decPDK[i]]
		decV[i] = step[e.decPDV[i]]
		encK[i] = step[e.decPEK[i]]
		encV[i] = step[e.decPEV[i]]
		if decK[i] == nil || decV[i] == nil || encK[i] == nil || encV[i] == nil {
			return "", fmt.Errorf("formula: decoder step 0 missing kv for layer %d", i)
		}
	}
	if tok == idEOS {
		return "", nil
	}
	gen := []int64{tok}
	limit := e.maxTokens
	if limit <= 0 {
		limit = defaultMaxTokens
	}
	for len(gen) < limit {
		in := map[string]*onnxrt.Tensor{
			e.pastIDs: onnxrt.IntFrom([]int64{gen[len(gen)-1]}, 1, 1),
		}
		for i := 0; i < e.layers; i++ {
			in[e.pastDK[i]] = decK[i]
			in[e.pastDV[i]] = decV[i]
			in[e.pastEK[i]] = encK[i]
			in[e.pastEV[i]] = encV[i]
		}
		step, err = e.past.Run(in)
		if err != nil {
			return "", fmt.Errorf("formula: decoder step %d: %w", len(gen), err)
		}
		tok, err = argmaxLast(step[e.pastLogits])
		if err != nil {
			return "", err
		}
		for i := 0; i < e.layers; i++ {
			dk := step[e.pastPDK[i]]
			dv := step[e.pastPDV[i]]
			if dk == nil || dv == nil {
				return "", fmt.Errorf("formula: decoder step %d missing present kv", len(gen))
			}
			decK[i], decV[i] = dk, dv
		}
		if tok == idEOS {
			break
		}
		gen = append(gen, tok)
	}
	return decodeIDs(e.vocab, gen)
}

func argmaxLast(t *onnxrt.Tensor) (int64, error) {
	if t == nil || t.DType != onnxrt.DFloat32 || t.Rank() < 1 || t.Shape[0] != 1 {
		if t == nil {
			return 0, fmt.Errorf("formula: missing logits")
		}
		return 0, fmt.Errorf("formula: bad logits shape %v dtype %v", t.Shape, t.DType)
	}
	vocab := t.Shape[t.Rank()-1]
	if vocab <= 0 || len(t.F32) < vocab {
		return 0, fmt.Errorf("formula: empty logits")
	}
	start := len(t.F32) - vocab
	// Batch is 1, so the tail of the buffer is the last position.
	if t.NumElements() != len(t.F32) {
		return 0, fmt.Errorf("formula: logits length %d != shape %v", len(t.F32), t.Shape)
	}
	best := 0
	bestV := float32(math.Inf(-1))
	for i, v := range t.F32[start : start+vocab] {
		if math.IsNaN(float64(v)) {
			return 0, fmt.Errorf("formula: NaN logits")
		}
		if v > bestV {
			bestV = v
			best = i
		}
	}
	return int64(best), nil
}

func (e *Engine) bind() error {
	ins := e.enc.InputNames()
	outs := e.enc.OutputNames()
	if len(ins) != 1 || len(outs) != 1 {
		return fmt.Errorf("formula: encoder inputs %v outputs %v", ins, outs)
	}
	e.encIn, e.encOut = ins[0], outs[0]

	var err error
	e.decIDs, err = pickIntInput(e.dec)
	if err != nil {
		return fmt.Errorf("formula: decoder: %w", err)
	}
	e.decHidden, err = pickHidden(e.dec)
	if err != nil {
		return fmt.Errorf("formula: decoder: %w", err)
	}
	e.decLogits, err = pickLogits(e.dec)
	if err != nil {
		return fmt.Errorf("formula: decoder: %w", err)
	}
	e.decPDK, e.decPDV, e.decPEK, e.decPEV, err = collectKV(e.dec.OutputNames(), "present", true)
	if err != nil {
		return fmt.Errorf("formula: decoder outputs: %w", err)
	}
	e.pastIDs, err = pickIntInput(e.past)
	if err != nil {
		return fmt.Errorf("formula: decoder_with_past: %w", err)
	}
	e.pastLogits, err = pickLogits(e.past)
	if err != nil {
		return fmt.Errorf("formula: decoder_with_past: %w", err)
	}
	e.pastDK, e.pastDV, e.pastEK, e.pastEV, err = collectKV(e.past.InputNames(), "past_key_values", true)
	if err != nil {
		return fmt.Errorf("formula: decoder_with_past inputs: %w", err)
	}
	e.pastPDK, e.pastPDV, _, _, err = collectKV(e.past.OutputNames(), "present", false)
	if err != nil {
		return fmt.Errorf("formula: decoder_with_past outputs: %w", err)
	}
	e.layers = len(e.decPDK)
	if e.layers == 0 || e.layers != len(e.pastDK) || e.layers != len(e.pastPDK) {
		return fmt.Errorf("formula: decoder layers present=%d past-in=%d past-out=%d",
			len(e.decPDK), len(e.pastDK), len(e.pastPDK))
	}
	return nil
}

func pickIntInput(g *onnxrt.Graph) (string, error) {
	var ints []string
	for _, name := range g.InputNames() {
		switch g.InputType(name) {
		case onnxrt.TypeInt32, onnxrt.TypeInt64:
			ints = append(ints, name)
		}
	}
	if len(ints) == 1 {
		return ints[0], nil
	}
	for _, name := range g.InputNames() {
		if name == "input_ids" || strings.HasSuffix(name, "input_ids") {
			return name, nil
		}
	}
	return "", fmt.Errorf("no input_ids among %v", g.InputNames())
}

func pickHidden(g *onnxrt.Graph) (string, error) {
	for _, name := range g.InputNames() {
		if strings.Contains(name, "encoder_hidden") {
			return name, nil
		}
	}
	var floats []string
	for _, name := range g.InputNames() {
		if strings.Contains(name, "key") || strings.Contains(name, "value") || strings.Contains(name, "input_ids") {
			continue
		}
		switch g.InputType(name) {
		case onnxrt.TypeFloat, onnxrt.TypeFloat16, onnxrt.TypeDouble, onnxrt.TypeUndefined:
			floats = append(floats, name)
		}
	}
	if len(floats) == 1 {
		return floats[0], nil
	}
	return "", fmt.Errorf("no encoder hidden state among %v", g.InputNames())
}

func pickLogits(g *onnxrt.Graph) (string, error) {
	for _, name := range g.OutputNames() {
		if name == "logits" || strings.HasSuffix(name, "logits") {
			return name, nil
		}
	}
	return "", fmt.Errorf("no logits among %v", g.OutputNames())
}

// collectKV groups present.* or past_key_values.* names by layer.
// needEnc requires encoder key and value as well as the decoder pair.
func collectKV(names []string, head string, needEnc bool) (decK, decV, encK, encV []string, err error) {
	type slot struct{ decK, decV, encK, encV string }
	var slots []slot
	for _, name := range names {
		parts := strings.Split(name, ".")
		if len(parts) != 4 || parts[0] != head {
			continue
		}
		layer, convErr := strconv.Atoi(parts[1])
		if convErr != nil || layer < 0 || layer > 64 {
			continue
		}
		for len(slots) <= layer {
			slots = append(slots, slot{})
		}
		switch parts[2] + "." + parts[3] {
		case "decoder.key":
			slots[layer].decK = name
		case "decoder.value":
			slots[layer].decV = name
		case "encoder.key":
			slots[layer].encK = name
		case "encoder.value":
			slots[layer].encV = name
		}
	}
	if len(slots) == 0 {
		return nil, nil, nil, nil, fmt.Errorf("no %s kv in %v", head, names)
	}
	decK = make([]string, len(slots))
	decV = make([]string, len(slots))
	encK = make([]string, len(slots))
	encV = make([]string, len(slots))
	for i, s := range slots {
		if s.decK == "" || s.decV == "" || (needEnc && (s.encK == "" || s.encV == "")) {
			return nil, nil, nil, nil, fmt.Errorf("%s layer %d is incomplete (%+v)", head, i, s)
		}
		decK[i], decV[i], encK[i], encV[i] = s.decK, s.decV, s.encK, s.encV
	}
	return decK, decV, encK, encV, nil
}
