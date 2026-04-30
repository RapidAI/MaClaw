package tts

import (
	"encoding/json"
	"fmt"
	"os"
)

// DurationCache holds trigram/bigram/unigram duration lookup tables.
type DurationCache struct {
	Trigram map[string]int // "prev,curr,next" → duration
	Bigram  map[string]int // "curr,next" → duration
	Unigram map[string]int // "curr" → duration
}

// LoadDurationCache loads the three-level duration cache from JSON files.
func LoadDurationCache(trigramPath, bigramPath, unigramPath string) (*DurationCache, error) {
	triData, err := os.ReadFile(trigramPath)
	if err != nil {
		return nil, fmt.Errorf("trigram: %w", err)
	}
	biData, err := os.ReadFile(bigramPath)
	if err != nil {
		return nil, fmt.Errorf("bigram: %w", err)
	}
	uniData, err := os.ReadFile(unigramPath)
	if err != nil {
		return nil, fmt.Errorf("unigram: %w", err)
	}
	return LoadDurationCacheFromBytes(triData, biData, uniData)
}

// LoadDurationCacheFromBytes loads the three-level duration cache from JSON byte slices.
func LoadDurationCacheFromBytes(triData, biData, uniData []byte) (*DurationCache, error) {
	dc := &DurationCache{}
	var err error

	dc.Trigram, err = unmarshalIntMap(triData)
	if err != nil {
		return nil, fmt.Errorf("trigram: %w", err)
	}
	if biData != nil {
		dc.Bigram, err = unmarshalIntMap(biData)
		if err != nil {
			return nil, fmt.Errorf("bigram: %w", err)
		}
	}
	if uniData != nil {
		dc.Unigram, err = unmarshalIntMap(uniData)
		if err != nil {
			return nil, fmt.Errorf("unigram: %w", err)
		}
	}
	return dc, nil
}

func unmarshalIntMap(data []byte) (map[string]int, error) {
	var m map[string]int
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// Lookup returns the duration for a phoneme in context, using three-level fallback.
func (dc *DurationCache) Lookup(prev, curr, next int) int {
	// Level 1: trigram
	key := fmt.Sprintf("%d,%d,%d", prev, curr, next)
	if d, ok := dc.Trigram[key]; ok {
		return d
	}
	// Level 2: bigram (curr, next)
	key = fmt.Sprintf("%d,%d", curr, next)
	if d, ok := dc.Bigram[key]; ok {
		return d
	}
	// Level 3: unigram
	key = fmt.Sprintf("%d", curr)
	if d, ok := dc.Unigram[key]; ok {
		return d
	}
	return 5 // default
}

// PiperDurationFromCache predicts durations using the trigram cache.
func PiperDurationFromCache(phonemeIDs []int64, cache *DurationCache) (durations []int, tMel int) {
	T := len(phonemeIDs)
	durations = make([]int, T)
	for t := 0; t < T; t++ {
		prev := -1
		if t > 0 {
			prev = int(phonemeIDs[t-1])
		}
		curr := int(phonemeIDs[t])
		next := -1
		if t < T-1 {
			next = int(phonemeIDs[t+1])
		}
		d := cache.Lookup(prev, curr, next)
		if d < 1 {
			d = 1
		}
		durations[t] = d
		tMel += d
	}
	return durations, tMel
}
