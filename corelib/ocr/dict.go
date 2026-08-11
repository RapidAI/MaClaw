package ocr

import (
	_ "embed"
	"fmt"
	"strings"
	"sync"
)

// dictText is the PP-OCRv6 character dictionary (one character per line),
// extracted verbatim from the official rec_inference.yml `character_dict`.
// CTC id = line index + 1; id 0 is the CTC blank. PaddleOCR's CTCLabelDecode
// (use_space_char=true) additionally appends a space as the last id, so the
// full vocabulary is ["", <dict...>, " "] with len(dict)+2 == 18710.
//
// The small and medium rec models share this dictionary (verified identical
// in their rec_inference.yml files); the tiny rec model uses a smaller
// 6904-character dictionary (dict_ppocrv6_tiny.txt, vocab 6906).
//
//go:embed dict_ppocrv6.txt
var dictText string

//go:embed dict_ppocrv6_tiny.txt
var dictTinyText string

var (
	dictOnce     sync.Once
	dictVal      []string
	dictTinyOnce sync.Once
	dictTinyVal  []string
)

// parseDict builds the id→character mapping from an embedded dictionary:
// id 0 is the blank token (""), ids 1..N-2 come from the file lines, and the
// last id is the space character appended by CTCLabelDecode.
func parseDict(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	d := make([]string, 0, len(lines)+2)
	d = append(d, "")
	d = append(d, lines...)
	d = append(d, " ")
	return d
}

// Dict returns the id→character mapping of the default (small/medium) rec
// models.
func Dict() []string {
	dictOnce.Do(func() { dictVal = parseDict(dictText) })
	return dictVal
}

// DictTiny returns the id→character mapping of the tiny rec model.
func DictTiny() []string {
	dictTinyOnce.Do(func() { dictTinyVal = parseDict(dictTinyText) })
	return dictTinyVal
}

// VocabSize returns the number of CTC classes (blank + dict + space) of the
// default (small/medium) rec models.
func VocabSize() int { return len(Dict()) }

// DictForVocab returns the dictionary matching a rec model's CTC vocab size
// (the last dimension of its output tensor), so tier switching works without
// the caller threading the tier through: small/medium share one dict, tiny
// has its own. An unknown vocab size is an error — decoding with a
// mismatched dict would produce garbage text.
func DictForVocab(vocab int) ([]string, error) {
	if d := Dict(); vocab == len(d) {
		return d, nil
	}
	if d := DictTiny(); vocab == len(d) {
		return d, nil
	}
	return nil, fmt.Errorf("ocr: rec vocab %d matches no known dict (small/medium %d, tiny %d)",
		vocab, VocabSize(), len(DictTiny()))
}
