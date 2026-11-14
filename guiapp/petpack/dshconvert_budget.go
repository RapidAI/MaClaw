package petpack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// DSHBudgetCell is one atlas cell after decoding. Opaque is alpha != 0.
type DSHBudgetCell struct {
	Column int
	Opaque bool
}

// DSHBudgetRow is one source animation row. Semantic is already normalized.
type DSHBudgetRow struct {
	Row      int
	Semantic string
	Cells    []DSHBudgetCell
}

// DSHKeptFrame is one cell that survives the texture cap.
type DSHKeptFrame struct {
	Row      int
	Column   int
	Semantic string
	Mirrored bool
}

// DSHBudgetResult is the deterministic frame selection for one atlas.
type DSHBudgetResult struct {
	Rows          []DSHRowReport
	DroppedFrames []DSHDroppedFrame
	Kept          []DSHKeptFrame
	KeptFrames    int
	MirroredLeft  bool
}

type dshBudgetTarget struct {
	target int
	floor  int
}

var dshBudgetTargets = map[string]dshBudgetTarget{
	"idle":       {4, 1},
	"walk-right": {4, 2},
	"run-right":  {4, 2},
	"wave":       {4, 1},
	"jump":       {4, 1},
	"failed":     {3, 1},
	"waiting":    {3, 1},
	"review":     {4, 1},
	"sing":       {4, 1},
	"gaze-left":  {2, 1},
	"gaze-right": {2, 1},
	"other":      {0, 0},
}

var dshShrinkOrder = []string{
	"other",
	"duplicate-locomotion",
	"gaze-right",
	"gaze-left",
	"review",
	"sing",
	"wave",
	"jump",
	"failed",
	"waiting",
	"run-right",
	"walk-right",
	"idle",
}

// BudgetDSHFrames selects at most textureCap opaque cells.
// columns is the atlas width in cells. Cells past that width are ignored.
func BudgetDSHFrames(rows []DSHBudgetRow, columns, textureCap int) DSHBudgetResult {
	if textureCap < 1 {
		textureCap = 1
	}
	if columns < 1 {
		columns = 1
	}
	type rowState struct {
		row      int
		semantic string
		cols     []int
		opaque   int
		mirrored bool
		dropped  int
	}
	states := make([]*rowState, 0, len(rows))
	var dropped []DSHDroppedFrame
	for _, row := range rows {
		st := &rowState{row: row.Row, semantic: row.Semantic}
		if st.semantic == "" {
			st.semantic = "other"
		}
		for _, cell := range row.Cells {
			if cell.Column < 0 || cell.Column >= columns {
				continue
			}
			if !cell.Opaque {
				dropped = append(dropped, DSHDroppedFrame{Row: row.Row, Column: cell.Column, Reason: "empty"})
				st.dropped++
				continue
			}
			st.cols = append(st.cols, cell.Column)
		}
		st.opaque = len(st.cols)
		states = append(states, st)
	}
	sort.SliceStable(states, func(i, j int) bool { return states[i].row < states[j].row })

	locomotion := func(sem string) bool {
		switch sem {
		case "run-right", "walk-right", "run-left", "walk-left":
			return true
		default:
			return false
		}
	}
	leftFacing := func(sem string) bool { return sem == "run-left" || sem == "walk-left" }
	locCount := 0
	for _, st := range states {
		if locomotion(st.semantic) && len(st.cols) > 0 {
			locCount++
		}
	}
	mirrored := false
	for _, st := range states {
		if !leftFacing(st.semantic) || len(st.cols) == 0 {
			continue
		}
		if locCount == 1 && !mirrored {
			mirrored = true
			st.mirrored = true
			if st.semantic == "walk-left" {
				st.semantic = "walk-right"
			} else {
				st.semantic = "run-right"
			}
			continue
		}
		for _, col := range st.cols {
			dropped = append(dropped, DSHDroppedFrame{Row: st.row, Column: col, Reason: "left_facing"})
		}
		st.dropped += len(st.cols)
		st.cols = nil
	}

	keptLoc := -1
	for i, st := range states {
		if locomotion(st.semantic) && len(st.cols) > 0 {
			if keptLoc < 0 {
				keptLoc = i
				continue
			}
			for _, col := range st.cols {
				dropped = append(dropped, DSHDroppedFrame{Row: st.row, Column: col, Reason: "duplicate_locomotion"})
			}
			st.dropped += len(st.cols)
			st.cols = nil
		}
	}

	for _, st := range states {
		spec := dshBudgetTargets[st.semantic]
		if _, ok := dshBudgetTargets[st.semantic]; !ok {
			spec = dshBudgetTargets["other"]
		}
		keep, extra := subsampleColumns(st.cols, spec.target)
		for _, col := range extra {
			dropped = append(dropped, DSHDroppedFrame{Row: st.row, Column: col, Reason: "subsample"})
			st.dropped++
		}
		st.cols = keep
	}

	floorOf := func(st *rowState) int {
		spec, ok := dshBudgetTargets[st.semantic]
		if !ok {
			return 0
		}
		floor := spec.floor
		if (st.semantic == "run-right" || st.semantic == "walk-right") && st.opaque < 2 {
			floor = st.opaque
		}
		if floor > len(st.cols) {
			return len(st.cols)
		}
		return floor
	}
	count := func() int {
		n := 0
		for _, st := range states {
			n += len(st.cols)
		}
		return n
	}
	dropOne := func(st *rowState, reason string) bool {
		if len(st.cols) == 0 {
			return false
		}
		last := len(st.cols) - 1
		dropped = append(dropped, DSHDroppedFrame{Row: st.row, Column: st.cols[last], Reason: reason})
		st.cols = st.cols[:last]
		st.dropped++
		return true
	}
	shrinkGroup := func(name string, toFloor bool) {
		for _, st := range states {
			match := st.semantic == name
			if name == "duplicate-locomotion" {
				match = false
			}
			if !match || len(st.cols) == 0 {
				continue
			}
			limit := 0
			if toFloor {
				limit = floorOf(st)
				if name == "sing" {
					limit = 1
					if limit > len(st.cols) {
						limit = len(st.cols)
					}
				}
				if name == "idle" && limit < 1 && st.opaque > 0 {
					limit = 1
				}
			}
			for len(st.cols) > limit && count() > textureCap {
				dropOne(st, "over_budget")
			}
		}
	}
	for _, name := range dshShrinkOrder {
		if count() <= textureCap {
			break
		}
		if name == "idle" || name == "sing" {
			shrinkGroup(name, true)
			continue
		}
		shrinkGroup(name, true)
	}
	if count() > textureCap {
		for _, name := range dshShrinkOrder {
			if count() <= textureCap {
				break
			}
			if name == "idle" || name == "sing" || name == "duplicate-locomotion" {
				continue
			}
			for _, st := range states {
				if st.semantic != name {
					continue
				}
				for len(st.cols) > 0 && count() > textureCap {
					dropOne(st, "over_budget")
				}
			}
		}
	}

	var result DSHBudgetResult
	result.DroppedFrames = dropped
	result.MirroredLeft = mirrored
	for _, st := range states {
		result.Rows = append(result.Rows, DSHRowReport{
			SourceRow: st.row,
			Semantic:  st.semantic,
			Kept:      len(st.cols),
			Dropped:   st.dropped,
		})
		for _, col := range st.cols {
			result.Kept = append(result.Kept, DSHKeptFrame{
				Row: st.row, Column: col, Semantic: st.semantic, Mirrored: st.mirrored,
			})
		}
	}
	result.KeptFrames = len(result.Kept)
	return result
}

func subsampleColumns(cols []int, k int) (keep, extra []int) {
	n := len(cols)
	if k <= 0 {
		return nil, append([]int(nil), cols...)
	}
	if n <= k {
		return append([]int(nil), cols...), nil
	}
	if k == 1 {
		return []int{cols[0]}, append([]int(nil), cols[1:]...)
	}
	chosen := make([]int, 0, k)
	seen := map[int]bool{}
	for i := 0; i < k; i++ {
		idx := int(math.Round(float64(i) * float64(n-1) / float64(k-1)))
		if idx < 0 {
			idx = 0
		}
		if idx >= n {
			idx = n - 1
		}
		if seen[idx] {
			continue
		}
		seen[idx] = true
		chosen = append(chosen, idx)
	}
	if len(chosen) < k {
		for i := 0; i < n && len(chosen) < k; i++ {
			if seen[i] {
				continue
			}
			seen[i] = true
			chosen = append(chosen, i)
		}
		sort.Ints(chosen)
	}
	keep = make([]int, 0, len(chosen))
	for _, idx := range chosen {
		keep = append(keep, cols[idx])
	}
	for i, col := range cols {
		if !seen[i] {
			extra = append(extra, col)
		}
	}
	return keep, extra
}

func normalizeDSHSemantic(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, "-", "")
	switch s {
	case "idle", "idleright", "stand":
		return "idle"
	case "runningright", "runright", "run", "running":
		return "run-right"
	case "walk", "walkright":
		return "walk-right"
	case "runningleft", "runleft", "walkleft":
		return "run-left"
	case "waving", "wave", "greet":
		return "wave"
	case "jumping", "jump":
		return "jump"
	case "failed", "fail", "error", "hit":
		return "failed"
	case "waiting", "wait", "listen", "listening":
		return "waiting"
	case "review", "reviewing", "speak", "speaking":
		return "review"
	case "sing", "singing":
		return "sing"
	case "gazeleft", "lookleft":
		return "gaze-left"
	case "gazeright", "lookright":
		return "gaze-right"
	default:
		return "other"
	}
}

var dshDefaultSemantics = []string{
	"idle", "run-right", "run-left", "wave", "jump", "failed", "waiting", "run-right", "review", "gaze-left", "gaze-right",
}

func resolveDSHRows(doc dshPetFile, rowCount, colCount int) ([]dshNamedRow, []string, error) {
	raw, warnings := pickDSHAnimation(doc)
	if len(bytesTrim(raw)) == 0 {
		var named []dshNamedRow
		for row := 0; row < rowCount; row++ {
			sem := "other"
			if row < len(dshDefaultSemantics) {
				sem = dshDefaultSemantics[row]
			}
			named = append(named, dshNamedRow{row: row, semantic: sem})
		}
		return named, warnings, nil
	}
	named, extra, err := parseDSHAnimations(raw, rowCount, colCount)
	warnings = append(warnings, extra...)
	if err != nil {
		return nil, warnings, err
	}
	if !hasSemantic(named, "idle") && rowCount > 0 && !rowClaimed(named, 0) {
		named = append(named, dshNamedRow{row: 0, semantic: "idle"})
	}
	return named, warnings, nil
}

func bytesTrim(raw []byte) []byte {
	return bytesTrimSpace(raw)
}

func bytesTrimSpace(raw []byte) []byte {
	return bytes.TrimSpace(raw)
}

func pickDSHAnimation(doc dshPetFile) (json.RawMessage, []string) {
	if len(bytes.TrimSpace(doc.Animations)) > 0 && string(bytes.TrimSpace(doc.Animations)) != "null" {
		var warnings []string
		if len(bytes.TrimSpace(doc.Tracks)) > 0 && string(bytes.TrimSpace(doc.Tracks)) != "null" {
			warnings = append(warnings, "ignored_tracks: animations 已声明，tracks 被忽略。")
		}
		if len(bytes.TrimSpace(doc.Sequences)) > 0 && string(bytes.TrimSpace(doc.Sequences)) != "null" {
			warnings = append(warnings, "ignored_sequences: animations 已声明，sequences 被忽略。")
		}
		return doc.Animations, warnings
	}
	if len(bytes.TrimSpace(doc.Tracks)) > 0 && string(bytes.TrimSpace(doc.Tracks)) != "null" {
		var warnings []string
		if len(bytes.TrimSpace(doc.Sequences)) > 0 && string(bytes.TrimSpace(doc.Sequences)) != "null" {
			warnings = append(warnings, "ignored_sequences: 已使用 tracks，sequences 被忽略。")
		}
		return doc.Tracks, warnings
	}
	if len(bytes.TrimSpace(doc.Sequences)) > 0 && string(bytes.TrimSpace(doc.Sequences)) != "null" {
		return doc.Sequences, nil
	}
	return nil, nil
}

func parseDSHAnimations(raw json.RawMessage, rowCount, colCount int) ([]dshNamedRow, []string, error) {
	var asArray []json.RawMessage
	if err := json.Unmarshal(raw, &asArray); err == nil {
		var named []dshNamedRow
		var warnings []string
		for _, item := range asArray {
			row, notes, err := parseDSHAnimItem(item, rowCount, colCount)
			warnings = append(warnings, notes...)
			if err != nil {
				return nil, warnings, err
			}
			if row != nil {
				named = append(named, *row)
			}
		}
		return named, warnings, nil
	}
	var asMap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &asMap); err != nil {
		return nil, nil, fmt.Errorf("pet.json 的 animations 无法解析")
	}
	names := make([]string, 0, len(asMap))
	for name := range asMap {
		names = append(names, name)
	}
	sort.Strings(names)
	var named []dshNamedRow
	var warnings []string
	for _, name := range names {
		row, notes, err := parseNamedAnimation(name, asMap[name], rowCount, colCount)
		warnings = append(warnings, notes...)
		if err != nil {
			return nil, warnings, err
		}
		if row != nil {
			named = append(named, *row)
		}
	}
	return named, warnings, nil
}

func parseDSHAnimItem(raw json.RawMessage, rowCount, colCount int) (*dshNamedRow, []string, error) {
	var obj struct {
		Name   string `json:"name"`
		Row    *int   `json:"row"`
		Frames []int  `json:"frames"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, nil, fmt.Errorf("pet.json 的 animations 无法解析")
	}
	row, notes := finishNamedRow(obj.Name, obj.Row, obj.Frames, rowCount, colCount)
	return row, notes, nil
}

func parseNamedAnimation(name string, body json.RawMessage, rowCount, colCount int) (*dshNamedRow, []string, error) {
	var rowNum int
	if err := json.Unmarshal(body, &rowNum); err == nil {
		row, notes := finishNamedRow(name, &rowNum, nil, rowCount, colCount)
		return row, notes, nil
	}
	var obj struct {
		Row    *int  `json:"row"`
		Frames []int `json:"frames"`
	}
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, nil, fmt.Errorf("pet.json 的 animations 无法解析")
	}
	row, notes := finishNamedRow(name, obj.Row, obj.Frames, rowCount, colCount)
	return row, notes, nil
}

func finishNamedRow(name string, row *int, frames []int, rowCount, colCount int) (*dshNamedRow, []string) {
	sem := normalizeDSHSemantic(name)
	out := &dshNamedRow{semantic: sem}
	var notes []string
	if row != nil {
		if *row < 0 || *row >= rowCount {
			return nil, []string{fmt.Sprintf("skipped_row: 行 %d 超出图集，已忽略。", *row)}
		}
		out.row = *row
	}
	if len(frames) > 0 {
		for _, col := range frames {
			if col < 0 || col >= colCount {
				notes = append(notes, fmt.Sprintf("skipped_column: 行 %d 列 %d 超出图集，已忽略。", out.row, col))
				continue
			}
			out.columns = append(out.columns, col)
		}
		if len(out.columns) == 0 {
			return nil, notes
		}
	}
	return out, notes
}

func hasSemantic(rows []dshNamedRow, sem string) bool {
	for _, row := range rows {
		if row.semantic == sem {
			return true
		}
	}
	return false
}

func rowClaimed(rows []dshNamedRow, index int) bool {
	for _, row := range rows {
		if row.row == index {
			return true
		}
	}
	return false
}

func dshPackID(sourceID, displayName string) string {
	raw := strings.TrimSpace(sourceID)
	if raw == "" {
		raw = strings.TrimSpace(displayName)
	}
	slug := slugDSH(raw)
	if slug == "" {
		slug = "pet"
	}
	if slug[0] < 'a' || slug[0] > 'z' {
		slug = "p" + slug
	}
	if !strings.HasPrefix(slug, "dsh-") {
		slug = "dsh-" + slug
	}
	slug = trimPackID(slug)
	if slug == DefaultPackID || slug == LegacyClawMatePackID || !IsValidPackID(slug) {
		return "dsh-pet"
	}
	return slug
}

func slugDSH(raw string) string {
	raw = strings.ToLower(raw)
	var b strings.Builder
	dash := false
	for _, r := range raw {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if ok {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func trimPackID(slug string) string {
	if len(slug) > 64 {
		slug = slug[:64]
	}
	slug = strings.Trim(slug, "-")
	if len(slug) < 2 {
		return "dsh-pet"
	}
	return slug
}

func cueForSemantic(sem string) (cue string, travel float64, loopSound bool) {
	switch sem {
	case "walk-right":
		return "step", 96, true
	case "run-right":
		return "run", 160, true
	case "wave":
		return "greet", 0, false
	case "jump":
		return "jump", 0, false
	case "failed":
		return "alert", 0, false
	case "sing":
		return "sing", 0, true
	default:
		return "", 0, false
	}
}
