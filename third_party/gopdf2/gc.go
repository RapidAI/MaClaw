package gopdf

// GarbageCollectLevel controls how aggressively unused objects are removed.
type GarbageCollectLevel int

const (
	// GCNone performs no garbage collection.
	GCNone GarbageCollectLevel = 0
	// GCCompact removes null/deleted objects and renumbers.
	GCCompact GarbageCollectLevel = 1
	// GCDedup additionally deduplicates identical objects.
	GCDedup GarbageCollectLevel = 2
)

// GarbageCollect removes unused and deleted objects from the document.
// This reduces file size by eliminating null placeholders left by
// DeletePage and other operations.
//
// level controls the aggressiveness:
//   - GCNone (0): no-op
//   - GCCompact (1): remove null objects, renumber references
//   - GCDedup (2): also deduplicate identical stream objects
//
// Returns the number of objects removed.
//
// Example:
//
//	pdf.DeletePage(2)
//	removed := pdf.GarbageCollect(gopdf.GCCompact)
//	fmt.Printf("Removed %d unused objects\n", removed)
func (gp *GoPdf) GarbageCollect(level GarbageCollectLevel) int {
	if level == GCNone {
		return 0
	}

	removed := 0

	// Phase 1: Mark null/deleted objects.
	liveObjs := make([]IObj, 0, len(gp.pdfObjs))
	oldToNew := make(map[int]int) // old index -> new index

	for i, obj := range gp.pdfObjs {
		if obj == nil {
			removed++
			continue
		}
		if _, isNull := obj.(nullObj); isNull {
			removed++
			continue
		}
		oldToNew[i] = len(liveObjs)
		liveObjs = append(liveObjs, obj)
	}

	if removed == 0 {
		return 0
	}

	// Phase 2: Update internal index references.
	gp.pdfObjs = liveObjs
	gp.reindexAfterCompact(oldToNew)

	// Phase 3: Deduplication (GCDedup only).
	if level >= GCDedup {
		removed += gp.deduplicateObjects()
	}

	return removed
}

// deduplicateObjects finds and merges identical ImportedObj instances.
// Returns the number of additional objects removed.
func (gp *GoPdf) deduplicateObjects() int {
	// Build a map of ImportedObj data -> first index.
	type importedKey struct {
		data string
	}
	seen := make(map[importedKey]int) // key -> first occurrence index
	mergeMap := make(map[int]int)     // duplicate index -> canonical index

	for i, obj := range gp.pdfObjs {
		imp, ok := obj.(*ImportedObj)
		if !ok {
			continue
		}
		key := importedKey{data: imp.Data}
		if firstIdx, exists := seen[key]; exists {
			mergeMap[i] = firstIdx
		} else {
			seen[key] = i
		}
	}

	if len(mergeMap) == 0 {
		return 0
	}

	// Replace duplicates with nullObj.
	for dupIdx := range mergeMap {
		gp.pdfObjs[dupIdx] = nullObj{}
	}

	// Compact again to remove the new nulls.
	liveObjs := make([]IObj, 0, len(gp.pdfObjs))
	oldToNew := make(map[int]int)
	removed := 0

	for i, obj := range gp.pdfObjs {
		if _, isNull := obj.(nullObj); isNull {
			removed++
			continue
		}
		oldToNew[i] = len(liveObjs)
		liveObjs = append(liveObjs, obj)
	}

	if removed == 0 {
		return 0
	}

	gp.pdfObjs = liveObjs
	gp.reindexAfterCompact(oldToNew)
	return removed
}

// reindexAfterCompact updates all internal index references after compaction.
func (gp *GoPdf) reindexAfterCompact(oldToNew map[int]int) {
	if newIdx, ok := oldToNew[gp.indexOfCatalogObj]; ok {
		gp.indexOfCatalogObj = newIdx
	}
	if newIdx, ok := oldToNew[gp.indexOfPagesObj]; ok {
		gp.indexOfPagesObj = newIdx
	}
	if gp.indexOfFirstPageObj >= 0 {
		if newIdx, ok := oldToNew[gp.indexOfFirstPageObj]; ok {
			gp.indexOfFirstPageObj = newIdx
		}
	}
	if gp.curr.IndexOfPageObj >= 0 {
		if newIdx, ok := oldToNew[gp.curr.IndexOfPageObj]; ok {
			gp.curr.IndexOfPageObj = newIdx
		}
	}
	if gp.indexOfContent >= 0 {
		if newIdx, ok := oldToNew[gp.indexOfContent]; ok {
			gp.indexOfContent = newIdx
		}
	}
	if newIdx, ok := oldToNew[gp.indexOfProcSet]; ok {
		gp.indexOfProcSet = newIdx
	}
	if gp.indexOfOutlinesObj >= 0 {
		if newIdx, ok := oldToNew[gp.indexOfOutlinesObj]; ok {
			gp.indexOfOutlinesObj = newIdx
		}
	}
	for i, idx := range gp.indexEncodingObjFonts {
		if newIdx, ok := oldToNew[idx]; ok {
			gp.indexEncodingObjFonts[i] = newIdx
		}
	}
	gp.numOfPagesObj = 0
	for _, obj := range gp.pdfObjs {
		if _, ok := obj.(*PageObj); ok {
			gp.numOfPagesObj++
		}
	}
}

// GetObjectCount returns the total number of PDF objects in the document.
// This includes all objects (pages, fonts, images, etc.).
func (gp *GoPdf) GetObjectCount() int {
	return len(gp.pdfObjs)
}

// GetLiveObjectCount returns the number of non-null PDF objects.
func (gp *GoPdf) GetLiveObjectCount() int {
	count := 0
	for _, obj := range gp.pdfObjs {
		if obj == nil {
			continue
		}
		if _, isNull := obj.(nullObj); isNull {
			continue
		}
		count++
	}
	return count
}
