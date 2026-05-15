package kokoro

func TokenizePhonemes(cfg *Config, phonemes string) ([]int, error) {
	ids := make([]int, 0, len([]rune(phonemes))+2)
	ids = append(ids, 0) // BOS
	for _, r := range phonemes {
		id, ok := cfg.Vocab[string(r)]
		if !ok {
			continue
		}
		ids = append(ids, id)
	}
	ids = append(ids, 0) // EOS

	// Truncate to fit PLBert's positional embedding context window.
	// This is the standard behavior for transformer models — input exceeding
	// max_position_embeddings is truncated, not rejected. The model produces
	// valid output from the truncated prefix (synthesizes less text).
	if max := cfg.PLBert.MaxPositionEmbeddings; max > 0 && len(ids) > max {
		if max < 2 {
			// Cannot fit even BOS + EOS — return minimal valid sequence.
			return []int{0, 0}, nil
		}
		// Keep BOS at [0], truncate content, replace last token with EOS.
		ids = ids[:max]
		ids[max-1] = 0 // EOS
	}
	return ids, nil
}
