package kokoro

import "fmt"

func TokenizePhonemes(cfg *Config, phonemes string) ([]int, error) {
	ids := make([]int, 0, len([]rune(phonemes))+2)
	ids = append(ids, 0)
	for _, r := range phonemes {
		id, ok := cfg.Vocab[string(r)]
		if !ok {
			continue
		}
		ids = append(ids, id)
	}
	ids = append(ids, 0)
	if max := cfg.PLBert.MaxPositionEmbeddings; max > 0 && len(ids) > max {
		return nil, fmt.Errorf("kokoro: token sequence length %d exceeds context %d", len(ids), max)
	}
	return ids, nil
}
