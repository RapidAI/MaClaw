// Package embed provides gzipped TTS data files embedded in the binary.
// These are small enough (~1.5MB total) to avoid runtime file dependencies.
// The GGUF model file (60MB) is NOT embedded — it's downloaded on demand.
package embed

import _ "embed"

// LexiconGz is the xiao_ya Chinese lexicon (20901 chars → pinyin), gzipped.
//
//go:embed lexicon.txt.gz
var LexiconGz []byte

// CMUDictGz is the CMU Pronouncing Dictionary (126K English words → ARPAbet), gzipped.
//
//go:embed cmudict.dict.gz
var CMUDictGz []byte

// DurationTrigramCacheGz is the trigram duration cache, gzipped.
//
//go:embed duration_trigram_cache.json.gz
var DurationTrigramCacheGz []byte

// DurationBigramCacheGz is the bigram duration cache, gzipped.
//
//go:embed duration_bigram_cache.json.gz
var DurationBigramCacheGz []byte

// DurationUnigramCacheGz is the unigram duration cache, gzipped.
//
//go:embed duration_unigram_cache.json.gz
var DurationUnigramCacheGz []byte

// DurationMLPGz is the duration MLP weights, gzipped.
//
//go:embed duration_mlp.bin.gz
var DurationMLPGz []byte
