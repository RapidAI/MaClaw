package ocr

import (
	_ "embed"
	"strings"
	"sync"
)

// dictText is the PP-OCRv6 character dictionary (one character per line),
// extracted verbatim from the official rec_inference.yml `character_dict`.
// CTC id = line index + 1; id 0 is the CTC blank. PaddleOCR's CTCLabelDecode
// (use_space_char=true) additionally appends a space as the last id, so the
// full vocabulary is ["", <dict...>, " "] with len(dict)+2 == 18710.
//
//go:embed dict_ppocrv6.txt
var dictText string

var (
	dictOnce sync.Once
	dictVal  []string
)

// Dict returns the id→character mapping. Id 0 is the blank token (""), ids
// 1..N-2 come from dict_ppocrv6.txt, and the last id is the space character
// appended by CTCLabelDecode.
func Dict() []string {
	dictOnce.Do(func() {
		text := strings.ReplaceAll(dictText, "\r\n", "\n")
		lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
		dictVal = make([]string, 0, len(lines)+2)
		dictVal = append(dictVal, "")
		dictVal = append(dictVal, lines...)
		dictVal = append(dictVal, " ")
	})
	return dictVal
}

// VocabSize returns the number of CTC classes (blank + dict + space).
func VocabSize() int { return len(Dict()) }
