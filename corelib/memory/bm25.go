package memory

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
)

// bm25Index wraps the shared bm25.Index for memory entries.
type bm25Index struct {
	idx *bm25.Index
}

func newBM25Index() *bm25Index {
	return &bm25Index{idx: bm25.New()}
}

func (b *bm25Index) rebuild(entries []Entry) {
	docs := make([]bm25.Doc, len(entries))
	for i, e := range entries {
		docs[i] = entryToDoc(e)
	}
	b.idx.Rebuild(docs)
}

func (b *bm25Index) addEntry(e Entry) {
	b.idx.Add(entryToDoc(e))
}

func (b *bm25Index) removeEntry(id string) {
	b.idx.Remove(id)
}

func (b *bm25Index) updateEntry(e Entry) {
	b.idx.Update(entryToDoc(e))
}

func (b *bm25Index) score(query string) map[string]float64 {
	return b.idx.Score(query)
}

func entryToDoc(e Entry) bm25.Doc {
	text := e.Content
	// Include CompactForm in the index — it may contain refined keywords
	// that the LLM compressor extracted from the original content.
	if e.CompactForm != "" && e.CompactForm != e.Content {
		text += " " + e.CompactForm
	}
	if len(e.Tags) > 0 {
		tagStr := strings.Join(e.Tags, " ")
		// Repeat tags to boost their BM25 term frequency weight.
		// Tags are curated labels with high signal-to-noise ratio; giving them
		// higher TF helps distinguish entries that share similar content but
		// differ in tags (e.g. "api-server" vs "gpu-server").
		text += " " + tagStr + " " + tagStr
	}
	// Include entity names in the index for entity-centric recall.
	// Entity tags (e.g. "entity:Alice", "relation:lives_in") are stripped
	// of their prefix and added to the searchable text.
	if len(e.Entities) > 0 {
		for _, ent := range e.Entities {
			if strings.HasPrefix(ent, "entity:") {
				name := strings.TrimPrefix(ent, "entity:")
				if name != "" {
					text += " " + name
				}
			}
		}
	}
	return bm25.Doc{ID: e.ID, Text: text}
}
