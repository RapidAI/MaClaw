package formula

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// WordLevel ids from FormulaNet tokenizer.json. The published decoder is
// null, so tokens are concatenated with no separator. Special ids are
// dropped and are not a space.
const (
	idBOS = 0
	idPAD = 1
	idEOS = 2
	idUNK = 3
)

func loadTokenizer(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseTokenizer(data)
}

func parseTokenizer(data []byte) ([]string, error) {
	var file struct {
		Model struct {
			Type  string         `json:"type"`
			Vocab map[string]int `json:"vocab"`
		} `json:"model"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("formula: tokenizer: %w", err)
	}
	if len(file.Model.Vocab) == 0 {
		return nil, fmt.Errorf("formula: tokenizer has an empty vocab")
	}
	maxID := 0
	for _, id := range file.Model.Vocab {
		if id < 0 {
			return nil, fmt.Errorf("formula: negative token id %d", id)
		}
		if id > maxID {
			maxID = id
		}
	}
	vocab := make([]string, maxID+1)
	for tok, id := range file.Model.Vocab {
		if vocab[id] != "" && vocab[id] != tok {
			return nil, fmt.Errorf("formula: token id %d is both %q and %q", id, vocab[id], tok)
		}
		vocab[id] = tok
	}
	return vocab, nil
}

func decodeIDs(vocab []string, ids []int64) (string, error) {
	var b strings.Builder
	for _, id := range ids {
		switch id {
		case idBOS, idPAD, idEOS, idUNK:
			continue
		}
		if id < 0 || int(id) >= len(vocab) || vocab[id] == "" {
			return "", fmt.Errorf("formula: token id %d is outside the vocab", id)
		}
		b.WriteString(vocab[id])
	}
	return b.String(), nil
}
