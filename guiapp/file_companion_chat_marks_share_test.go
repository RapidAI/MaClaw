package guiapp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileCompanionSplitSharePayloadReadsHighlightsAndChatMarks(t *testing.T) {
	highlights := `[{"id":"m1","page":1,"color":"yellow","rects":[{"x":0.1,"y":0.2,"w":0.3,"h":0.04}],"text":"重点"}]`
	chatMarks := `[{"message":2,"color":"green","start":3,"end":5,"text":"回答"}]`
	annotations := `[{"id":"a1","kind":"annotation","text":"原文","translation":"批注","parts":[{"page":1,"rects":[{"x":0.1,"y":0.2,"w":0.3,"h":0.04}]}]}]`
	drawings := `[{"id":"d1","page":1,"color":"yellow","tool":"line","points":[{"x":0.1,"y":0.2},{"x":0.3,"y":0.4}]}]`
	gotHighlights, gotChat, gotAnnotations, gotDrawings, gotBookmarks, err := fileCompanionSplitSharePayload(`{"highlights":` + highlights + `,"chatMarks":` + chatMarks + `,"annotations":` + annotations + `,"drawings":` + drawings + `}`)
	if err != nil {
		t.Fatal(err)
	}
	var pageMarks []map[string]any
	if err := json.Unmarshal(gotHighlights, &pageMarks); err != nil || len(pageMarks) != 1 || pageMarks[0]["id"] != "m1" {
		t.Fatalf("highlights = %s (%v)", gotHighlights, err)
	}
	var answerMarks []map[string]any
	if err := json.Unmarshal(gotChat, &answerMarks); err != nil || len(answerMarks) != 1 || answerMarks[0]["color"] != "green" {
		t.Fatalf("chat marks = %s (%v)", gotChat, err)
	}
	var notes []map[string]any
	if err := json.Unmarshal(gotAnnotations, &notes); err != nil || len(notes) != 1 || notes[0]["kind"] != "annotation" {
		t.Fatalf("annotations = %s (%v)", gotAnnotations, err)
	}
	var strokes []map[string]any
	if err := json.Unmarshal(gotDrawings, &strokes); err != nil || len(strokes) != 1 || strokes[0]["tool"] != "line" {
		t.Fatalf("drawings = %s (%v)", gotDrawings, err)
	}
	if gotBookmarks != nil {
		t.Fatalf("omitted bookmarks = %s", gotBookmarks)
	}

	// A legacy caller sends the highlights array alone.
	gotHighlights, gotChat, gotAnnotations, gotDrawings, gotBookmarks, err = fileCompanionSplitSharePayload(highlights)
	if err != nil {
		t.Fatal(err)
	}
	if gotBookmarks != nil {
		t.Fatalf("payload without bookmarks grew bookmarks: %s", gotBookmarks)
	}
	if gotChat != nil || gotAnnotations != nil || gotDrawings != nil || gotBookmarks != nil {
		t.Fatalf("legacy payload grew chat marks, annotations, drawings, or bookmarks: %s %s %s %s", gotChat, gotAnnotations, gotDrawings, gotBookmarks)
	}
	if err := json.Unmarshal(gotHighlights, &pageMarks); err != nil || len(pageMarks) != 1 {
		t.Fatalf("legacy highlights = %s (%v)", gotHighlights, err)
	}
}

func TestWriteFileCompanionProjectKeepsChatMarks(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "paper.pdf")
	if err := os.WriteFile(source, []byte("%PDF-"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "project.zip")
	chatMarks := json.RawMessage(`[{"message":0,"color":"blue","start":0,"end":2,"text":"答"}]`)
	if err := writeFileCompanionProject(dest, source, json.RawMessage("[]"), chatMarks, nil, json.RawMessage(`[{"id":"d1","page":1,"color":"yellow","tool":"line","points":[{"x":0.1,"y":0.2},{"x":0.3,"y":0.4}]}]`), nil, []FileCompanionMessage{{Role: "assistant", Text: "答"}}, "", 0); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	manifest, _, _, project := readFileCompanionShareProject(raw)
	if !project {
		t.Fatal("the project was not recognized")
	}
	var marks []map[string]any
	if err := json.Unmarshal(manifest.ChatMarks, &marks); err != nil || len(marks) != 1 || marks[0]["color"] != "blue" {
		t.Fatalf("manifest chat marks = %s (%v)", manifest.ChatMarks, err)
	}
	var strokes []map[string]any
	if err := json.Unmarshal(manifest.Drawings, &strokes); err != nil || len(strokes) != 1 || strokes[0]["tool"] != "line" {
		t.Fatalf("manifest drawings = %s (%v)", manifest.Drawings, err)
	}

	// A project written without chat marks stays a legacy project: the field is
	// absent, so a reader keeps its own answer highlights.
	legacy := filepath.Join(dir, "legacy.zip")
	if err := writeFileCompanionProject(legacy, source, json.RawMessage("[]"), nil, nil, nil, nil, nil, "", 0); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(legacy)
	if err != nil {
		t.Fatal(err)
	}
	manifest, _, _, _ = readFileCompanionShareProject(raw)
	if len(manifest.ChatMarks) != 0 || len(manifest.Drawings) != 0 {
		t.Fatalf("legacy manifest carries chat marks or drawings: %s %s", manifest.ChatMarks, manifest.Drawings)
	}
}

func TestFileCompanionCleanAnnotationsDropsUnusableNotes(t *testing.T) {
	raw := json.RawMessage(`[
		{"id":"a1","kind":"annotation","text":"原文","translation":"批注","parts":[{"page":1,"rects":[{"x":0.1,"y":0.2,"w":0.3,"h":0.04}]}],"place":{"x":0.2,"y":-0.1},"size":{"w":0.4,"h":0.3}},
		{"id":"t1","text":"本地译文","translation":"不该分享","parts":[{"page":1,"rects":[{"x":0.1,"y":0.2,"w":0.3,"h":0.04}]}]},
		{"id":"a2","kind":"annotation","text":"没有锚点","translation":"批注","parts":[]},
		{"id":"a3","kind":"annotation","text":"坐标越界","translation":"批注","parts":[{"page":1,"rects":[{"x":2,"y":0,"w":0.1,"h":0.1}]}]}
	]`)
	got, err := fileCompanionCleanAnnotations(raw)
	if err != nil {
		t.Fatal(err)
	}
	var notes []fileCompanionAnnotation
	if err := json.Unmarshal(got, &notes); err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 {
		t.Fatalf("kept %d notes: %s", len(notes), got)
	}
	note := notes[0]
	if note.ID != "a1" || note.Kind != "annotation" || note.Text != "原文" || note.Translation != "批注" {
		t.Fatalf("note = %+v", note)
	}
	if note.Size == nil || note.Size.W != 0.4 || note.Size.H != 0.3 {
		t.Fatalf("size = %+v", note.Size)
	}
	if note.Place == nil || note.Place.X != 0.2 || note.Place.Y != -0.1 {
		t.Fatalf("place = %+v", note.Place)
	}
	if len(note.Parts) != 1 || note.Parts[0].Page != 1 || len(note.Parts[0].Rects) != 1 {
		t.Fatalf("parts = %+v", note.Parts)
	}
}

func TestFileCompanionCleanDrawingsValidatesStrokes(t *testing.T) {
	raw := json.RawMessage(`[
		{"id":"d1","page":1,"color":"yellow","tool":"line","points":[{"x":0.1,"y":0.2},{"x":0.3,"y":0.4}]},
		{"id":"d2","page":2,"color":"blue","tool":"box","points":[{"x":0.1,"y":0.1},{"x":0.4,"y":0.3},{"x":0.6,"y":0.5}]},
		{"id":"bad-color","page":1,"color":"red","tool":"line","points":[{"x":0.1,"y":0.2},{"x":0.3,"y":0.4}]},
		{"id":"bad-tool","page":1,"color":"yellow","tool":"circle","points":[{"x":0.1,"y":0.2},{"x":0.3,"y":0.4}]},
		{"id":"off-page","page":1,"color":"yellow","tool":"line","points":[{"x":-0.1,"y":0.2},{"x":0.3,"y":0.4}]},
		{"id":"bad-point","page":1,"color":"yellow","tool":"line","points":[{"x":"0.1","y":0.2},{"x":0.3,"y":0.4}]},
		{"id":"tiny-box","page":1,"color":"yellow","tool":"box","points":[{"x":0.1,"y":0.2},{"x":0.1005,"y":0.2005}]}
	]`)
	got, err := fileCompanionCleanDrawings(raw)
	if err != nil {
		t.Fatal(err)
	}
	var drawings []fileCompanionDrawing
	if err := json.Unmarshal(got, &drawings); err != nil {
		t.Fatal(err)
	}
	if len(drawings) != 2 {
		t.Fatalf("kept %d drawings: %s", len(drawings), got)
	}
	if drawings[0].ID != "d1" || drawings[0].Tool != "line" || len(drawings[0].Points) != 2 {
		t.Fatalf("line = %+v", drawings[0])
	}
	if drawings[1].ID != "d2" || drawings[1].Tool != "box" || len(drawings[1].Points) != 2 {
		t.Fatalf("box = %+v", drawings[1])
	}
	if drawings[1].Points[0].X != 0.1 || drawings[1].Points[1].X != 0.6 {
		t.Fatalf("box corners = %+v", drawings[1].Points)
	}
}

func TestFileCompanionCleanBookmarksKeepsAddedOrderAndTheNewest(t *testing.T) {
	raw := `[
		{"id":"b2","page":7,"x":0.4,"y":0.8,"color":"orange","name":"  后   加  "},
		{"id":"` + strings.Repeat("x", 81) + `","page":1,"x":0.2,"y":0.2,"color":"teal","name":"太长"},
		{"id":"bad","page":1,"x":0.2,"y":0.2,"color":"navy","name":"丢掉"},
		{"id":"long-name","page":3,"x":0,"y":1,"color":"purple","name":"` + strings.Repeat("名", 41) + `"},
		{"id":"b1","page":2,"x":0.2,"y":0.1,"color":"teal","name":"先加"}
	]`
	got, err := fileCompanionCleanBookmarks(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	var kept []fileCompanionProjectBookmark
	if err := json.Unmarshal(got, &kept); err != nil {
		t.Fatal(err)
	}
	if len(kept) != 3 || kept[0].Name != "后 加" || kept[0].Color != "orange" || kept[2].Name != "先加" || kept[2].Color != "teal" {
		t.Fatalf("order = %+v", kept)
	}
	if kept[1].ID != "long-name" || kept[1].Name != strings.Repeat("名", 40) {
		t.Fatalf("trimmed name = %+v", kept[1])
	}
	dup, err := fileCompanionCleanBookmarks(json.RawMessage(`[
		{"id":"b1","page":2,"x":0.2,"y":0.1,"color":"teal","name":"先加"},
		{"id":"b1","page":4,"x":0.3,"y":0.6,"color":"blue","name":"改过"}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	var one []fileCompanionProjectBookmark
	if err := json.Unmarshal(dup, &one); err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 || one[0].Page != 4 || one[0].Name != "改过" || one[0].Color != "blue" || one[0].Y != 0.6 {
		t.Fatalf("duplicate id = %+v", one)
	}
	if _, err := fileCompanionCleanBookmarks(json.RawMessage(`{"not":"an array"}`)); err == nil {
		t.Fatal("expected an object payload to fail")
	}

	many := make([]fileCompanionProjectBookmark, 0, 205)
	for i := 0; i < 205; i++ {
		many = append(many, fileCompanionProjectBookmark{
			ID: fmt.Sprintf("n%03d", i), Page: 1, X: 0.2, Y: 0.3, Color: "green", Name: fmt.Sprintf("第%d", i),
		})
	}
	body, err := json.Marshal(many)
	if err != nil {
		t.Fatal(err)
	}
	capped, err := fileCompanionCleanBookmarks(body)
	if err != nil {
		t.Fatal(err)
	}
	var newest []fileCompanionProjectBookmark
	if err := json.Unmarshal(capped, &newest); err != nil {
		t.Fatal(err)
	}
	if len(newest) != 200 || newest[0].ID != "n005" || newest[len(newest)-1].ID != "n204" {
		t.Fatalf("cap kept %d from %s to %s", len(newest), newest[0].ID, newest[len(newest)-1].ID)
	}

	// More valid rows than the old walk was willing to keep. The newest stay.
	huge := make([]fileCompanionProjectBookmark, 0, 2001)
	for i := 0; i < 2001; i++ {
		huge = append(huge, fileCompanionProjectBookmark{
			ID: fmt.Sprintf("n%04d", i), Page: 1, X: 0.2, Y: 0.3, Color: "green", Name: fmt.Sprintf("第%d", i),
		})
	}
	hugeBody, err := json.Marshal(huge)
	if err != nil {
		t.Fatal(err)
	}
	hugeKept, err := fileCompanionCleanBookmarks(hugeBody)
	if err != nil {
		t.Fatal(err)
	}
	var tail []fileCompanionProjectBookmark
	if err := json.Unmarshal(hugeKept, &tail); err != nil {
		t.Fatal(err)
	}
	if len(tail) != 200 || tail[0].ID != "n1801" || tail[len(tail)-1].ID != "n2000" {
		t.Fatalf("long cap kept %d from %s to %s", len(tail), tail[0].ID, tail[len(tail)-1].ID)
	}

	// Rejected rows after a real bookmark must not hide it.
	mixed := make([]fileCompanionProjectBookmark, 0, 2001)
	mixed = append(mixed, fileCompanionProjectBookmark{ID: "keep", Page: 2, X: 0.1, Y: 0.2, Color: "teal", Name: "留下"})
	for i := 0; i < 2000; i++ {
		mixed = append(mixed, fileCompanionProjectBookmark{
			ID: fmt.Sprintf("bad%04d", i), Page: 1, X: 0.2, Y: 0.3, Color: "navy", Name: "丢掉",
		})
	}
	mixedBody, err := json.Marshal(mixed)
	if err != nil {
		t.Fatal(err)
	}
	mixedKept, err := fileCompanionCleanBookmarks(mixedBody)
	if err != nil {
		t.Fatal(err)
	}
	var buried []fileCompanionProjectBookmark
	if err := json.Unmarshal(mixedKept, &buried); err != nil {
		t.Fatal(err)
	}
	if len(buried) != 1 || buried[0].ID != "keep" || buried[0].Name != "留下" {
		t.Fatalf("buried = %+v", buried)
	}
}
