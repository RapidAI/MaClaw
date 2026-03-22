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
	if len(e.Tags) > 0 {
		text += " " + strings.Join(e.Tags, " ")
	}
	return bm25.Doc{ID: e.ID, Text: text}
}
