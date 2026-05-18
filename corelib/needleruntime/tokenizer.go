package needleruntime

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type TokenizerInfo struct {
	Path      string   `json:"path"`
	Kind      string   `json:"kind"`
	Hashing   bool     `json:"hashing,omitempty"`
	VocabSize int      `json:"vocab_size,omitempty"`
	MaxID     int      `json:"max_id,omitempty"`
	Samples   []string `json:"samples,omitempty"`
}

type LabelInfo struct {
	Path   string   `json:"path"`
	Labels []string `json:"labels"`
}

type SimpleTokenizer struct {
	Vocab   map[string]int
	UnkID   int
	HashDim int
}

func LoadSimpleTokenizer(path string) (*SimpleTokenizer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	vocab, err := parseTokenizerVocab(data)
	if err != nil {
		return nil, err
	}
	unkID := -1
	for _, token := range []string{"<unk>", "[UNK]", "unk"} {
		if id, ok := vocab[token]; ok {
			unkID = id
			break
		}
	}
	tok := &SimpleTokenizer{Vocab: vocab, UnkID: unkID}
	if _, ok := vocab["__h0"]; ok {
		tok.HashDim = tok.MaxID() + 1
	}
	return tok, nil
}

func (t *SimpleTokenizer) Encode(text string) []int {
	return t.EncodeInto(nil, text)
}

func (t *SimpleTokenizer) EncodeInto(dst []int, text string) []int {
	if t == nil || len(t.Vocab) == 0 {
		return dst[:0]
	}
	if t.isHashingTokenizer() {
		return appendHashedText(dst[:0], text, t.HashDim)
	}
	pieces := splitPromptPieces(text)
	ids := dst[:0]
	for _, piece := range pieces {
		if id, ok := t.Vocab[piece]; ok {
			ids = append(ids, id)
			continue
		}
		lower := strings.ToLower(piece)
		if id, ok := t.Vocab[lower]; ok {
			ids = append(ids, id)
			continue
		}
		if t.UnkID >= 0 {
			ids = append(ids, t.UnkID)
		}
	}
	return ids
}

func (t *SimpleTokenizer) isHashingTokenizer() bool {
	return t != nil && t.HashDim > 0
}

func (t *SimpleTokenizer) hasCompleteHashVocab() bool {
	return t != nil && t.HashDim > 0 && isCompleteHashVocab(t.Vocab, t.HashDim-1)
}

func (t *SimpleTokenizer) encodeHashed(text string) []int {
	return t.encodeHashedInto(nil, text)
}

func (t *SimpleTokenizer) encodeHashedInto(dst []int, text string) []int {
	dim := t.HashDim
	if dim <= 0 {
		return dst[:0]
	}
	return appendHashedText(dst[:0], text, dim)
}

func (t *SimpleTokenizer) EncodeRequestInto(dst []int, req Request) []int {
	if t == nil || len(t.Vocab) == 0 {
		return dst[:0]
	}
	if !t.isHashingTokenizer() {
		return t.EncodeInto(dst, RenderPrompt(req))
	}
	ids := dst[:0]
	dim := t.HashDim
	ids = appendHashToken(ids, "Task", dim)
	ids = appendHashedText(ids, req.Task, dim)
	ids = appendHashToken(ids, "Choices", dim)
	if len(req.Choices) == 0 {
		ids = appendHashToken(ids, "none", dim)
	} else {
		for _, choice := range req.Choices {
			ids = appendHashedText(ids, choice, dim)
		}
	}
	ids = appendHashToken(ids, "User", dim)
	ids = appendHashedText(ids, req.Text, dim)
	return ids
}

func appendHashedText(ids []int, text string, dim int) []int {
	var h uint64
	inToken := false
	for i := 0; i < len(text); {
		b := text[i]
		if b < utf8.RuneSelf {
			i++
			if isASCIISep(b) {
				if inToken {
					ids = append(ids, int(h%uint64(dim)))
					inToken = false
				}
				continue
			}
			if !inToken {
				h = fnv64Offset
				inToken = true
			}
			if 'A' <= b && b <= 'Z' {
				b += 'a' - 'A'
			}
			h ^= uint64(b)
			h *= fnv64Prime
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if size <= 0 {
			break
		}
		i += size
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			if inToken {
				ids = append(ids, int(h%uint64(dim)))
				inToken = false
			}
			continue
		}
		if !inToken {
			h = fnv64Offset
			inToken = true
		}
		h = hashRuneLower(h, r)
	}
	if inToken {
		ids = append(ids, int(h%uint64(dim)))
	}
	return ids
}

func appendHashToken(ids []int, token string, dim int) []int {
	return append(ids, hashPieceID(token, dim))
}

func isASCIISep(b byte) bool {
	if b <= ' ' {
		return true
	}
	if '0' <= b && b <= '9' || 'A' <= b && b <= 'Z' || 'a' <= b && b <= 'z' || b == '_' {
		return false
	}
	return true
}

const (
	fnv64Offset uint64 = 14695981039346656037
	fnv64Prime  uint64 = 1099511628211
)

func hashPieceID(piece string, dim int) int {
	h := fnv64Offset
	for _, r := range piece {
		h = hashRuneLower(h, r)
	}
	return int(h % uint64(dim))
}

func hashRuneLower(h uint64, r rune) uint64 {
	r = unicode.ToLower(r)
	if r < utf8.RuneSelf {
		h ^= uint64(byte(r))
		return h * fnv64Prime
	}
	var buf [utf8.UTFMax]byte
	n := utf8.EncodeRune(buf[:], r)
	for i := 0; i < n; i++ {
		h ^= uint64(buf[i])
		h *= fnv64Prime
	}
	return h
}

func (t *SimpleTokenizer) MaxID() int {
	maxID := -1
	if t == nil {
		return maxID
	}
	for _, id := range t.Vocab {
		if id > maxID {
			maxID = id
		}
	}
	return maxID
}

func inspectTokenizer(modelPath string, manifest *Manifest) (*TokenizerInfo, []string) {
	if manifest == nil || strings.TrimSpace(manifest.Tokenizer) == "" {
		return nil, nil
	}
	path := artifactFilePath(modelPath, manifest.Tokenizer)
	info := &TokenizerInfo{Path: manifest.Tokenizer, Kind: tokenizerKind(manifest.Tokenizer)}
	data, err := os.ReadFile(path)
	if err != nil {
		return info, []string{fmt.Sprintf("read tokenizer failed: %v", err)}
	}
	if strings.EqualFold(info.Kind, "json") {
		vocabSize, maxID, hashing, samples, err := inspectTokenizerJSON(data)
		if err != nil {
			return info, []string{err.Error()}
		}
		info.VocabSize = vocabSize
		info.MaxID = maxID
		info.Hashing = hashing
		info.Samples = samples
	}
	return info, nil
}

func tokenizerKind(path string) string {
	if strings.HasSuffix(strings.ToLower(path), ".json") {
		return "json"
	}
	if strings.HasSuffix(strings.ToLower(path), ".model") {
		return "sentencepiece"
	}
	return "unknown"
}

func inspectTokenizerJSON(data []byte) (int, int, bool, []string, error) {
	vocab, err := parseTokenizerVocab(data)
	if err != nil {
		return 0, 0, false, nil, err
	}
	samples := make([]string, 0, len(vocab))
	maxID := -1
	for token, id := range vocab {
		samples = append(samples, token)
		if id > maxID {
			maxID = id
		}
	}
	sort.Strings(samples)
	if len(samples) > 8 {
		samples = samples[:8]
	}
	return len(vocab), maxID, isCompleteHashVocab(vocab, maxID), samples, nil
}

func isCompleteHashVocab(vocab map[string]int, maxID int) bool {
	if maxID < 0 || len(vocab) != maxID+1 {
		return false
	}
	for i := 0; i <= maxID; i++ {
		if id, ok := vocab[fmt.Sprintf("__h%d", i)]; !ok || id != i {
			return false
		}
	}
	return true
}

func parseTokenizerVocab(data []byte) (map[string]int, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse tokenizer json failed: %w", err)
	}
	vocab := map[string]any{}
	if model, _ := raw["model"].(map[string]any); model != nil {
		if v, _ := model["vocab"].(map[string]any); v != nil {
			vocab = v
		}
	}
	if len(vocab) == 0 {
		if v, _ := raw["vocab"].(map[string]any); v != nil {
			vocab = v
		}
	}
	if len(vocab) == 0 {
		return nil, fmt.Errorf("tokenizer json missing vocab")
	}
	out := make(map[string]int, len(vocab))
	for token, rawID := range vocab {
		switch v := rawID.(type) {
		case float64:
			out[token] = int(v)
		case int:
			out[token] = v
		default:
			return nil, fmt.Errorf("tokenizer vocab id for %q is not numeric", token)
		}
	}
	return out, nil
}

func inspectLabels(modelPath string, manifest *Manifest) (*LabelInfo, []string) {
	if manifest == nil || strings.TrimSpace(manifest.Labels) == "" {
		return nil, nil
	}
	path := artifactFilePath(modelPath, manifest.Labels)
	data, err := os.ReadFile(path)
	if err != nil {
		return &LabelInfo{Path: manifest.Labels}, []string{fmt.Sprintf("read labels failed: %v", err)}
	}
	var labels []string
	if err := json.Unmarshal(data, &labels); err != nil {
		return &LabelInfo{Path: manifest.Labels}, []string{fmt.Sprintf("parse labels failed: %v", err)}
	}
	if len(labels) == 0 {
		return &LabelInfo{Path: manifest.Labels}, []string{"labels file is empty"}
	}
	return &LabelInfo{Path: manifest.Labels, Labels: labels}, nil
}

func LoadLabels(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var labels []string
	if err := json.Unmarshal(data, &labels); err != nil {
		return nil, err
	}
	if len(labels) == 0 {
		return nil, fmt.Errorf("labels file is empty")
	}
	return labels, nil
}

func RenderPrompt(req Request) string {
	choices := strings.Join(req.Choices, ", ")
	if choices == "" {
		choices = "none"
	}
	return strings.TrimSpace("Task: " + req.Task + "\nChoices: " + choices + "\nUser: " + strings.TrimSpace(req.Text))
}

func splitPromptPieces(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
}
