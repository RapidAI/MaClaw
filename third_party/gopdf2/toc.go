package gopdf

// TOCItem represents a single entry in the table of contents.
type TOCItem struct {
	// Level is the hierarchy level (1 = top level).
	Level int
	// Title is the bookmark title text.
	Title string
	// PageNo is the 1-based target page number.
	PageNo int
	// Y is the vertical position on the target page (in points from top).
	Y float64
}

// GetTOC returns the table of contents (outline/bookmark tree) as a flat list.
// Each item includes its hierarchy level, title, target page, and Y position.
//
// This reads the outlines that were added via AddOutline/AddOutlineWithPosition.
// For imported PDFs, outlines from the source are not preserved by gofpdi,
// so this returns only outlines added programmatically.
//
// Example:
//
//	toc := pdf.GetTOC()
//	for _, item := range toc {
//	    fmt.Printf("L%d: %s -> page %d\n", item.Level, item.Title, item.PageNo)
//	}
func (gp *GoPdf) GetTOC() []TOCItem {
	if gp.outlines == nil || gp.outlines.Count() == 0 {
		return nil
	}

	var items []TOCItem

	// Walk through pdfObjs to find OutlineObj instances and reconstruct the tree.
	// Since the current outline system is a flat linked list under OutlinesObj,
	// we traverse the linked list via first/next pointers.
	firstObjID := gp.outlines.first
	if firstObjID <= 0 {
		return nil
	}

	// Build a map of objID -> OutlineObj for traversal.
	outlineMap := make(map[int]*OutlineObj)
	for i, obj := range gp.pdfObjs {
		if o, ok := obj.(*OutlineObj); ok {
			outlineMap[i+1] = o
		}
	}

	// Map dest (page obj ID) -> page number.
	pageObjIDs := make(map[int]int) // objID -> 1-based page number
	pageCount := 0
	for i, obj := range gp.pdfObjs {
		if _, ok := obj.(*PageObj); ok {
			pageCount++
			pageObjIDs[i+1] = pageCount
		}
	}

	// Traverse the linked list starting from first.
	visited := make(map[int]bool)
	gp.collectTOCItems(outlineMap, pageObjIDs, firstObjID, 1, &items, visited)

	return items
}

// collectTOCItems recursively collects TOC items from the outline tree.
func (gp *GoPdf) collectTOCItems(
	outlineMap map[int]*OutlineObj,
	pageObjIDs map[int]int,
	objID int,
	level int,
	items *[]TOCItem,
	visited map[int]bool,
) {
	if objID <= 0 || visited[objID] {
		return
	}
	visited[objID] = true

	o, ok := outlineMap[objID]
	if !ok {
		return
	}

	pageNo := pageObjIDs[o.dest]
	if pageNo == 0 {
		pageNo = -1
	}

	*items = append(*items, TOCItem{
		Level:  level,
		Title:  o.title,
		PageNo: pageNo,
		Y:      o.height,
	})

	// Recurse into children.
	if o.first > 0 {
		gp.collectTOCItems(outlineMap, pageObjIDs, o.first, level+1, items, visited)
	}

	// Continue to next sibling.
	if o.next > 0 {
		gp.collectTOCItems(outlineMap, pageObjIDs, o.next, level, items, visited)
	}
}

// SetTOC replaces the entire outline tree with the provided TOC items.
// Each item's Level must be >= 1. The first item must have Level 1.
// Levels may increase by at most 1 from one item to the next.
//
// Example:
//
//	pdf.SetTOC([]gopdf.TOCItem{
//	    {Level: 1, Title: "Chapter 1", PageNo: 1},
//	    {Level: 2, Title: "Section 1.1", PageNo: 1, Y: 200},
//	    {Level: 2, Title: "Section 1.2", PageNo: 2},
//	    {Level: 1, Title: "Chapter 2", PageNo: 3},
//	})
func (gp *GoPdf) SetTOC(items []TOCItem) error {
	if len(items) == 0 {
		// Clear outlines.
		gp.outlines = &OutlinesObj{}
		gp.outlines.init(func() *GoPdf { return gp })
		gp.outlines.SetIndexObjOutlines(gp.indexOfOutlinesObj + 1)
		return nil
	}

	// Validate levels.
	if items[0].Level != 1 {
		return ErrInvalidTOCLevel
	}
	for i := 1; i < len(items); i++ {
		if items[i].Level < 1 {
			return ErrInvalidTOCLevel
		}
		if items[i].Level > items[i-1].Level+1 {
			return ErrInvalidTOCLevel
		}
	}

	// Build page number -> page obj ID map.
	pageObjIDByNo := make(map[int]int) // 1-based page number -> obj ID
	pageCount := 0
	for i, obj := range gp.pdfObjs {
		if _, ok := obj.(*PageObj); ok {
			pageCount++
			pageObjIDByNo[pageCount] = i + 1
		}
	}

	// Reset outlines.
	gp.outlines = &OutlinesObj{}
	gp.outlines.init(func() *GoPdf { return gp })
	gp.outlines.SetIndexObjOutlines(gp.indexOfOutlinesObj + 1)

	// For a flat (level-1 only) TOC, use the simple AddOutline approach.
	// For hierarchical TOC, we need to build the tree structure.
	allFlat := true
	for _, item := range items {
		if item.Level > 1 {
			allFlat = false
			break
		}
	}

	if allFlat {
		for _, item := range items {
			dest := pageObjIDByNo[item.PageNo]
			if dest == 0 {
				continue
			}
			if item.Y > 0 {
				gp.outlines.AddOutlinesWithPosition(dest, item.Title, item.Y)
			} else {
				gp.outlines.AddOutline(dest, item.Title)
			}
		}
		return nil
	}

	// Build hierarchical outline tree using OutlineNodes.
	type nodeInfo struct {
		node  *OutlineNode
		level int
	}

	var roots []*OutlineNode
	var stack []nodeInfo

	for _, item := range items {
		dest := pageObjIDByNo[item.PageNo]
		if dest == 0 {
			continue
		}

		oo := gp.outlines.AddOutlinesWithPosition(dest, item.Title, item.Y)
		node := &OutlineNode{Obj: oo}

		// Pop stack until we find the parent level.
		for len(stack) > 0 && stack[len(stack)-1].level >= item.Level {
			stack = stack[:len(stack)-1]
		}

		if len(stack) == 0 {
			roots = append(roots, node)
		} else {
			parent := stack[len(stack)-1].node
			parent.Children = append(parent.Children, node)
		}

		stack = append(stack, nodeInfo{node: node, level: item.Level})
	}

	// Parse the tree to set prev/next/first/last/parent pointers.
	OutlineNodes(roots).Parse()

	// Update outlines first/last from roots.
	if len(roots) > 0 {
		gp.outlines.first = roots[0].Obj.GetIndex()
		gp.outlines.last = roots[len(roots)-1].Obj.GetIndex()
	}

	return nil
}

// ErrInvalidTOCLevel is returned when TOC items have invalid hierarchy levels.
var ErrInvalidTOCLevel = errorf("invalid TOC level: first item must be level 1, and levels may increase by at most 1")

func errorf(msg string) error {
	return &tocError{msg: msg}
}

type tocError struct {
	msg string
}

func (e *tocError) Error() string {
	return e.msg
}
