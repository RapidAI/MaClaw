package petpack

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func opaqueBudgetRow(row int, semantic string, columns int) DSHBudgetRow {
	cells := make([]DSHBudgetCell, columns)
	for i := range cells {
		cells[i] = DSHBudgetCell{Column: i, Opaque: true}
	}
	return DSHBudgetRow{Row: row, Semantic: semantic, Cells: cells}
}

func standardBudgetRows() []DSHBudgetRow {
	specs := []string{"idle", "run-right", "wave", "jump", "failed", "waiting", "review", "sing", "gaze-left", "gaze-right"}
	rows := make([]DSHBudgetRow, len(specs))
	for i, sem := range specs {
		rows[i] = opaqueBudgetRow(i, sem, 8)
	}
	return rows
}

func rowReport(rows []DSHRowReport, semantic string) DSHRowReport {
	for _, row := range rows {
		if row.Semantic == semantic {
			return row
		}
	}
	return DSHRowReport{Semantic: semantic, Kept: -1}
}

func TestBudgetDSHFramesCapsAndOrder(t *testing.T) {
	rows := standardBudgetRows()
	first := BudgetDSHFrames(rows, 8, 32)
	second := BudgetDSHFrames(rows, 8, 32)
	if !reflect.DeepEqual(first.DroppedFrames, second.DroppedFrames) {
		t.Fatal("same input and cap produced different dropped frames")
	}
	if first.KeptFrames != 32 {
		t.Fatalf("cap 32 kept %d", first.KeptFrames)
	}
	if rowReport(first.Rows, "gaze-right").Kept != 1 || rowReport(first.Rows, "gaze-left").Kept != 1 {
		t.Fatalf("gaze rows = %+v %+v", rowReport(first.Rows, "gaze-right"), rowReport(first.Rows, "gaze-left"))
	}
	if rowReport(first.Rows, "review").Kept != 4 || rowReport(first.Rows, "sing").Kept != 4 {
		t.Fatalf("review/sing = %+v %+v", rowReport(first.Rows, "review"), rowReport(first.Rows, "sing"))
	}
	over := map[string]int{}
	for _, drop := range first.DroppedFrames {
		if drop.Reason != "over_budget" {
			continue
		}
		for _, row := range first.Rows {
			if row.SourceRow == drop.Row {
				over[row.Semantic]++
			}
		}
	}
	if over["gaze-right"] != 1 || over["gaze-left"] != 1 || over["review"] != 0 || over["sing"] != 0 {
		t.Fatalf("cap 32 over_budget = %+v", over)
	}
	idleKept := keptColumns(first, "idle")
	if !reflect.DeepEqual(idleKept, []int{0, 2, 5, 7}) {
		t.Fatalf("idle subsample = %v", idleKept)
	}

	tight := BudgetDSHFrames(rows, 8, 28)
	if rowReport(tight.Rows, "review").Kept != 1 || rowReport(tight.Rows, "sing").Kept != 3 {
		t.Fatalf("cap 28 review/sing = %+v %+v", rowReport(tight.Rows, "review"), rowReport(tight.Rows, "sing"))
	}
	reviewOverBeforeSing := 0
	seenSing := false
	for _, drop := range tight.DroppedFrames {
		if drop.Reason != "over_budget" {
			continue
		}
		sem := ""
		for _, row := range tight.Rows {
			if row.SourceRow == drop.Row {
				sem = row.Semantic
			}
		}
		if sem == "sing" {
			seenSing = true
			break
		}
		if sem == "review" {
			reviewOverBeforeSing++
		}
	}
	if !seenSing || reviewOverBeforeSing != 3 {
		t.Fatalf("sing over_budget before review reached floor: reviewDrops=%d seenSing=%v", reviewOverBeforeSing, seenSing)
	}
}

func keptColumns(result DSHBudgetResult, semantic string) []int {
	var cols []int
	for _, frame := range result.Kept {
		if frame.Semantic == semantic {
			cols = append(cols, frame.Column)
		}
	}
	return cols
}

func TestBudgetDSHFramesDropsEmptyLeftAndDuplicate(t *testing.T) {
	rows := []DSHBudgetRow{
		opaqueBudgetRow(0, "idle", 2),
		opaqueBudgetRow(1, "run-right", 2),
		{
			Row: 2, Semantic: "wave",
			Cells: []DSHBudgetCell{{Column: 0, Opaque: true}, {Column: 1, Opaque: false}},
		},
		opaqueBudgetRow(3, "run-left", 2),
		opaqueBudgetRow(7, "run-right", 2),
	}
	result := BudgetDSHFrames(rows, 8, 32)
	reasons := map[string]int{}
	for _, drop := range result.DroppedFrames {
		reasons[drop.Reason]++
	}
	if reasons["empty"] != 1 || reasons["left_facing"] != 2 || reasons["duplicate_locomotion"] != 2 {
		t.Fatalf("reasons = %+v", reasons)
	}
	if result.MirroredLeft {
		t.Fatal("left row was mirrored even though a right row exists")
	}
	if rowReport(result.Rows, "run-left").Kept != 0 {
		t.Fatal("left-facing pixels were kept")
	}
}

func TestBudgetDSHFramesMirrorsUniqueLeftRow(t *testing.T) {
	rows := []DSHBudgetRow{
		opaqueBudgetRow(0, "idle", 1),
		opaqueBudgetRow(2, "run-left", 4),
	}
	result := BudgetDSHFrames(rows, 8, 32)
	if !result.MirroredLeft {
		t.Fatal("unique left locomotion was not mirrored")
	}
	if !reflect.DeepEqual(keptColumns(result, "run-right"), []int{0, 1, 2, 3}) {
		t.Fatalf("mirrored columns were rewritten: %v", keptColumns(result, "run-right"))
	}
	for _, frame := range result.Kept {
		if frame.Semantic == "run-right" && !frame.Mirrored {
			t.Fatal("mirrored frame lost its flag")
		}
	}
	both := BudgetDSHFrames([]DSHBudgetRow{
		opaqueBudgetRow(0, "idle", 1),
		opaqueBudgetRow(1, "run-left", 2),
		opaqueBudgetRow(2, "walk-left", 2),
	}, 8, 32)
	if both.MirroredLeft || rowReport(both.Rows, "run-left").Kept != 0 || rowReport(both.Rows, "walk-left").Kept != 0 {
		t.Fatalf("two left rows were not both dropped: %+v mirrored=%v", both.Rows, both.MirroredLeft)
	}
}

func TestBudgetDSHFramesNeverDeletesIdle(t *testing.T) {
	rows := []DSHBudgetRow{opaqueBudgetRow(0, "idle", 8), opaqueBudgetRow(1, "sing", 8)}
	result := BudgetDSHFrames(rows, 8, 1)
	if rowReport(result.Rows, "idle").Kept < 1 || rowReport(result.Rows, "sing").Kept < 1 {
		t.Fatalf("idle or sing deleted: %+v", result.Rows)
	}
}

func TestDSHPackIDRules(t *testing.T) {
	cases := map[string]string{
		"dsh-whale": "dsh-whale",
		"DSH-Whale": "dsh-whale",
		"ragdoll":   "dsh-ragdoll",
		"clawmate":  "dsh-clawmate",
		"":          "dsh-pet",
		"123cat":    "dsh-p123cat",
	}
	for in, want := range cases {
		if got := dshPackID(in, ""); got != want {
			t.Fatalf("dshPackID(%q) = %q, want %q", in, got, want)
		}
	}
	if got := normalizeDSHSemantic("Walk-Left"); got != "run-left" {
		t.Fatalf("walk-left normalized to %s", got)
	}
	if got := normalizeDSHSemantic("running"); got != "run-right" {
		t.Fatalf("running normalized to %s", got)
	}
}

func TestConvertDSHPetRejectsFamilies(t *testing.T) {
	parent := t.TempDir()
	assertReject := func(dir, want string) {
		t.Helper()
		report, err := ConvertDSHPet(DSHConvertRequest{
			InputDir:  dir,
			OutputDir: parent,
			ZipPath:   filepath.Join(parent, "out.zip"),
		})
		if err == nil {
			t.Fatal("expected an error")
		}
		if report.RejectedReason != want || report.ValidatorError != "" || report.OK {
			t.Fatalf("report = %+v err=%v", report, err)
		}
		if report.OutputDir != "" || report.ZipPath != "" {
			t.Fatalf("reject wrote paths %+v", report)
		}
	}
	empty := t.TempDir()
	assertReject(empty, dshRejectNoJSON)

	live := writeSpriteDir(t, map[string]any{
		"id": "whale", "renderer": "live2d", "spritesheetPath": "spritesheet.png",
		"frame": map[string]int{"width": 8, "height": 8, "columns": 1, "rows": 1},
	}, 1, 1, nil)
	assertReject(live, dshRejectLive2D)

	both := writeSpriteDir(t, map[string]any{
		"id": "whale", "spritesheetPath": "spritesheet.png",
		"live2d": map[string]string{"model": "a.model3.json"},
		"frame":  map[string]int{"width": 8, "height": 8, "columns": 1, "rows": 1},
	}, 1, 1, nil)
	assertReject(both, dshRejectLive2D)

	video := t.TempDir()
	writePetJSON(t, video, map[string]any{"id": "clip", "spritesheetPath": "clip.mp4"})
	assertReject(video, dshRejectVideo)

	script := t.TempDir()
	writePetJSON(t, script, map[string]any{"id": "widget", "entry": "pet.js"})
	assertReject(script, dshRejectScript)

	escape := t.TempDir()
	writePetJSON(t, escape, map[string]any{"id": "whale", "spritesheetPath": "../spritesheet.png"})
	assertReject(escape, dshRejectPath)

	absolute := t.TempDir()
	writePetJSON(t, absolute, map[string]any{"id": "whale", "spritesheetPath": "C:/spritesheet.png"})
	assertReject(absolute, dshRejectPath)

	mismatch := writeSpriteDir(t, map[string]any{
		"id": "whale", "spritesheetPath": "spritesheet.png",
		"frame": map[string]int{"width": 4, "height": 8, "columns": 2, "rows": 1},
	}, 1, 1, nil)
	assertReject(mismatch, dshRejectGrid)

	noFrame := writeSpriteDir(t, map[string]any{
		"id": "whale", "spritesheetPath": "spritesheet.png",
	}, 1, 1, nil)
	assertReject(noFrame, dshRejectNoFrame)

	noIdle := writeSpriteDir(t, map[string]any{
		"id": "whale", "renderer": "sprite", "spritesheetPath": "spritesheet.png",
		"frame":      map[string]int{"width": 8, "height": 8, "columns": 1, "rows": 1},
		"animations": []any{map[string]any{"name": "wave", "row": 0}},
	}, 1, 1, func(img *image.NRGBA, _, _ int) {
		fillCell(img, 0, 0, 8, 8, color.NRGBA{R: 10, A: 255})
	})
	assertReject(noIdle, dshRejectNoIdle)

	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("reject left files: %v", entries)
	}
}

func TestConvertDSHPetUnknownRendererIsNotAReject(t *testing.T) {
	dir := writeSpriteDir(t, map[string]any{
		"id": "whale", "renderer": "widget", "spritesheetPath": "spritesheet.png",
		"frame": map[string]int{"width": 8, "height": 8, "columns": 1, "rows": 1},
	}, 1, 1, nil)
	report, err := ConvertDSHPet(DSHConvertRequest{InputDir: dir})
	if err == nil || report.RejectedReason != "" || report.ValidatorError != "" {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}

func TestConvertDSHPetSuccess(t *testing.T) {
	const cols, rows, cell = 4, 8, 8
	dir := writeSpriteDir(t, map[string]any{
		"id": "dsh-whale", "displayName": "Whale", "description": "tiny",
		"renderer": "spritesheet", "spritesheetPath": "spritesheet.png",
		"frame": map[string]int{"width": cell, "height": cell, "columns": cols, "rows": rows},
		"animations": []any{
			map[string]any{"name": "idle", "row": 0},
			map[string]any{"name": "running-right", "row": 1},
			map[string]any{"name": "waiting", "row": 2},
			map[string]any{"name": "review", "row": 3},
			map[string]any{"name": "sing", "row": 4},
			map[string]any{"name": "jump", "row": 5},
			map[string]any{"name": "wave", "row": 6},
			map[string]any{"name": "failed", "row": 7, "frames": []int{0, 1, 2, 99}},
		},
		"tracks": []any{map[string]any{"name": "run", "row": 1}},
	}, cols, rows, func(img *image.NRGBA, col, row int) {
		if row == 0 && col == 3 {
			return
		}
		fillCell(img, col, row, cell, cell, color.NRGBA{R: uint8(20 + row*20), G: uint8(30 + col*20), B: 40, A: 255})
	})
	parent := t.TempDir()
	zipPath := filepath.Join(parent, "whale.zip")
	report, err := ConvertDSHPet(DSHConvertRequest{InputDir: dir, OutputDir: parent, ZipPath: zipPath})
	if err != nil {
		t.Fatalf("convert: %v validator=%s rejected=%s", err, report.ValidatorError, report.RejectedReason)
	}
	if !report.OK || report.RejectedReason != "" || report.ValidatorError != "" {
		t.Fatalf("report = %+v", report)
	}
	if report.PackID != "dsh-whale" || report.KeptFrames > 32 || report.KeptFrames < 1 {
		t.Fatalf("pack %s kept %d", report.PackID, report.KeptFrames)
	}
	if !warningHas(report.Warnings, "ignored_tracks") || !warningHas(report.Warnings, "no_thinking_row") || !warningHas(report.Warnings, "blink_not_in_atlas") || !warningHas(report.Warnings, "expression_baked_in_frame") {
		t.Fatalf("warnings = %+v", report.Warnings)
	}
	if !warningHas(report.Warnings, "skipped_column") {
		t.Fatalf("out-of-range column was not reported: %+v", report.Warnings)
	}
	for _, row := range report.Rows {
		switch row.Semantic {
		case "waiting", "review", "wave", "idle":
			if row.Cue != "" || row.TravelX != 0 {
				t.Fatalf("row %s cue=%q travel=%v", row.Semantic, row.Cue, row.TravelX)
			}
		case "sing":
			if row.Cue != "sing" {
				t.Fatalf("sing row cue = %q", row.Cue)
			}
		}
	}
	packDir := filepath.Join(parent, "dsh-whale")
	raw, err := os.ReadFile(filepath.Join(packDir, "rig", "dsh-whale.pet-rig.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rig Rig
	if err := json.Unmarshal(raw, &rig); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateCharacterDefinition(mustRead(t, filepath.Join(packDir, "character", "dsh-whale.performer.json")), raw, &PetPackRigAssets{Definition: "rig/dsh-whale.pet-rig.json", Textures: rigTextureNames(rig)}); err != nil {
		t.Fatal(err)
	}
	assertFlipbook(t, rig.Clips["idle_loop"], true)
	assertFlipbook(t, rig.Clips["run_loop"], true)
	assertFlipbook(t, rig.Clips["listen_loop"], true)
	if rig.Clips["listen_loop"].Cue != "" || rig.Clips["listen_loop"].TravelX != 0 {
		t.Fatalf("listening clip is audible: %+v", rig.Clips["listen_loop"].Cue)
	}
	speak := rig.Clips["speak_loop"]
	if speak.Cue != "sing" || !speak.SoundLoop || !speak.Loop {
		t.Fatalf("speak = cue %q loop %v soundLoop %v", speak.Cue, speak.Loop, speak.SoundLoop)
	}
	for name, clip := range rig.Clips {
		if clip.Cue == "sing" && name != "speak_loop" {
			t.Fatalf("cue sing on %s", name)
		}
		if name == "speak_loop" && (clip.Cue == "step" || clip.Cue == "run") {
			t.Fatal("speaking uses footsteps")
		}
	}
	run := rig.Clips["run_loop"]
	if run.Cue != "run" || run.TravelX != 160 || !run.SoundLoop {
		t.Fatalf("run = %+v", run.Cue)
	}
	if rig.Clips["wave_loop"].Cue != "" || rig.Clips["wave_greet"].Cue != "greet" || rig.Clips["wave_greet"].Loop {
		t.Fatal("greet leaked onto the wave loop")
	}
	jump := rig.Clips["jump_in"]
	if jump.Loop || jump.Cue != "jump" {
		t.Fatalf("jump_in loop=%v cue=%s", jump.Loop, jump.Cue)
	}
	n := jump.DurationMS / 120
	last, lastN := visibleSlot(jump, (n-1)*120+60)
	end, endN := visibleSlot(jump, jump.DurationMS)
	first, _ := visibleSlot(jump, 60)
	if lastN != 1 || endN != 1 || last != end || (n > 1 && last == first) {
		t.Fatalf("one-shot endpoint last=%d end=%d first=%d n=%d", last, end, first, n)
	}
	alert := rig.Clips["alert_loop"]
	if !alert.Loop || alert.SoundLoop || alert.Cue != "alert" {
		t.Fatalf("alert loop sound = cue %q soundLoop %v loop %v", alert.Cue, alert.SoundLoop, alert.Loop)
	}
	manifest, err := parseManifest(mustRead(t, filepath.Join(packDir, "pet-pack.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ID != "dsh-whale" || manifest.License != "user-supplied" || manifest.DefaultSize != 88 || !manifest.Capabilities.PetPerformanceV3 {
		t.Fatalf("manifest id=%s license=%s size=%d", manifest.ID, manifest.License, manifest.DefaultSize)
	}
	if !strings.Contains(manifest.Description, "画面来自用户本地图集") {
		t.Fatalf("description = %s", manifest.Description)
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	sawManifest := false
	for _, file := range zr.File {
		if strings.Contains(file.Name, ".maclaw-pet-source") {
			t.Fatal("zip contains a source marker")
		}
		if file.Name == "dsh-whale/pet-pack.yaml" {
			sawManifest = true
		}
	}
	if !sawManifest {
		t.Fatal("zip is missing the pack directory")
	}
	user := t.TempDir()
	reg := NewRegistry(user, nil)
	id, err := reg.InstallZip(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetPackSource(id, SourceImported); err != nil {
		t.Fatal(err)
	}
	if packSourceForDir(filepath.Join(user, id)) != SourceImported {
		t.Fatal("installed pack was not marked imported")
	}
	installed, ok := reg.Get(id)
	if !ok || installed.Status != StatusOK {
		errText := ""
		if installed != nil {
			errText = installed.Error
		}
		t.Fatalf("installed pack status invalid: %s", errText)
	}
	if _, err := os.Stat(filepath.Join(parent, "dsh-whale.tmp-dshconvert")); !os.IsNotExist(err) {
		t.Fatal("temp pack directory was left behind")
	}
}

func TestConvertDSHPetAcceptsEmptyRenderer(t *testing.T) {
	dir := writeSpriteDir(t, map[string]any{
		"id": "whale", "displayName": "Whale", "spritesheetPath": "spritesheet.png",
		"frame": map[string]int{"width": 8, "height": 8, "columns": 1, "rows": 1},
	}, 1, 1, func(img *image.NRGBA, col, row int) {
		fillCell(img, col, row, 8, 8, color.NRGBA{R: 20, G: 30, B: 40, A: 255})
	})
	report, err := ConvertDSHPet(DSHConvertRequest{InputDir: dir, OutputDir: t.TempDir()})
	if err != nil || report.PackID != "dsh-whale" {
		t.Fatalf("empty renderer: %+v err=%v", report, err)
	}
}

func TestConvertDSHPetReviewWithoutSingIsSilent(t *testing.T) {
	dir := writeSpriteDir(t, map[string]any{
		"id": "review-pet", "spritesheetPath": "spritesheet.png", "renderer": "atlas",
		"frame": map[string]int{"width": 8, "height": 8, "columns": 2, "rows": 2},
		"animations": []any{
			map[string]any{"name": "idle", "row": 0},
			map[string]any{"name": "speaking", "row": 1},
		},
	}, 2, 2, func(img *image.NRGBA, col, row int) {
		fillCell(img, col, row, 8, 8, color.NRGBA{G: 200, A: 255})
	})
	report, err := ConvertDSHPet(DSHConvertRequest{InputDir: dir, OutputDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if report.PackID != "dsh-review-pet" {
		t.Fatalf("id = %s", report.PackID)
	}
	raw := mustRead(t, filepath.Join(report.OutputDir, "rig", report.PackID+".pet-rig.json"))
	var rig Rig
	if err := json.Unmarshal(raw, &rig); err != nil {
		t.Fatal(err)
	}
	speak := rig.Clips["speak_loop"]
	if speak.Cue != "" || speak.SoundLoop || speak.Cue == "step" || speak.Cue == "run" {
		t.Fatalf("review speak cue = %q", speak.Cue)
	}
	if _, ok := rig.Clips["run_loop"]; ok {
		t.Fatal("review row became locomotion")
	}
	if !warningHas(report.Warnings, "no_locomotion") {
		t.Fatalf("warnings = %+v", report.Warnings)
	}
}

func TestConvertDSHPetMirrorsLeftPixels(t *testing.T) {
	const cell = 8
	dir := writeSpriteDir(t, map[string]any{
		"id": "lefty", "spritesheetPath": "spritesheet.png",
		"frame": map[string]int{"width": cell, "height": cell, "columns": 1, "rows": 2},
		"animations": []any{
			map[string]any{"name": "idle", "row": 0},
			map[string]any{"name": "running-left", "row": 1},
		},
	}, 1, 2, func(img *image.NRGBA, col, row int) {
		if row == 0 {
			fillCell(img, col, row, cell, cell, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
			return
		}
		img.SetNRGBA(0, row*cell+3, color.NRGBA{R: 255, A: 255})
	})
	report, err := ConvertDSHPet(DSHConvertRequest{InputDir: dir, OutputDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !report.MirroredLeft || !warningHas(report.Warnings, "mirrored_left_row") {
		t.Fatalf("mirror report = %+v", report)
	}
	pngRaw := mustRead(t, filepath.Join(report.OutputDir, "rig", "frame_01.png"))
	img, err := png.Decode(bytes.NewReader(pngRaw))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	right := color.NRGBAModel.Convert(img.At(b.Max.X-1, 3)).(color.NRGBA)
	left := color.NRGBAModel.Convert(img.At(b.Min.X, 3)).(color.NRGBA)
	if right.R < 200 || right.A < 200 {
		t.Fatalf("flipped pixel missing on the right: %+v", right)
	}
	if left.R > 200 && left.G < 20 && left.B < 20 {
		t.Fatalf("left-facing pixel was stored: %+v", left)
	}
}

func TestConvertDSHPetWriteErrorIsNotAReject(t *testing.T) {
	dir := writeSpriteDir(t, map[string]any{
		"id": "whale", "spritesheetPath": "spritesheet.png",
		"frame":      map[string]int{"width": 8, "height": 8, "columns": 1, "rows": 1},
		"animations": []any{map[string]any{"name": "idle", "row": 0}},
	}, 1, 1, func(img *image.NRGBA, col, row int) {
		fillCell(img, col, row, 8, 8, color.NRGBA{B: 255, A: 255})
	})
	blocked := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := ConvertDSHPet(DSHConvertRequest{InputDir: dir, OutputDir: blocked})
	if err == nil || report.RejectedReason != "" || report.ValidatorError != "" || report.OutputDir != "" {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}

func TestDSHValidatorErrorIsNotAReject(t *testing.T) {
	err := validateDSHFiles(map[string][]byte{"pet-pack.yaml": []byte("id: \"bad id\"\n")}, nil)
	if err == nil {
		t.Fatal("expected validator error")
	}
	rejected, validator := classifyDSHError(err)
	if rejected != "" || validator == "" {
		t.Fatalf("classified reject=%q validator=%q", rejected, validator)
	}
	rejected, validator = classifyDSHError(rejectDSH(dshRejectNoIdle))
	if rejected != dshRejectNoIdle || validator != "" {
		t.Fatalf("reject classified as validator: %q %q", rejected, validator)
	}
}

func TestWriteDSHZipStoresOverCompressedBytes(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "packed.zip")
	files := map[string][]byte{"note.txt": bytes.Repeat([]byte{0x61}, 80_000)}
	if err := writeDSHZip(dest, "dsh-pet", files); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if len(zr.File) != 1 || zr.File[0].Method != zip.Store {
		t.Fatalf("method = %v", zr.File[0].Method)
	}
}

func assertFlipbook(t *testing.T, clip RigClip, loop bool) {
	t.Helper()
	if clip.DurationMS < 120 || clip.Loop != loop {
		t.Fatalf("clip duration %d loop %v", clip.DurationMS, clip.Loop)
	}
	n := clip.DurationMS / 120
	for k := 0; k < n; k++ {
		_, count := visibleSlot(clip, k*120+60)
		if count != 1 {
			t.Fatalf("t=%d shows %d slots", k*120+60, count)
		}
	}
	last, lastN := visibleSlot(clip, clip.DurationMS-2)
	if lastN != 1 {
		t.Fatalf("hold frame shows %d slots", lastN)
	}
	held, _ := visibleSlot(clip, (n-1)*120+60)
	if last != held {
		t.Fatalf("duration-2 shows slot %d, last frame is %d", last, held)
	}
	if loop {
		a, _ := visibleSlot(clip, 0)
		b, _ := visibleSlot(clip, clip.DurationMS)
		if a != b {
			t.Fatalf("loop endpoint %d != start %d", b, a)
		}
	}
}

func visibleSlot(clip RigClip, at int) (slot, count int) {
	slot = -1
	for name, frames := range clip.Tracks {
		if !strings.HasPrefix(name, "frame_") {
			continue
		}
		if trackAlpha(frames, at) < 0.02 {
			continue
		}
		count++
		n, _ := strconv.Atoi(strings.TrimPrefix(name, "frame_"))
		slot = n
	}
	return slot, count
}

func rigTextureNames(rig Rig) []string {
	names := make([]string, 0, len(rig.Slots))
	for _, slot := range rig.Slots {
		names = append(names, slot.Texture)
	}
	return names
}

func warningHas(warnings []string, token string) bool {
	for _, warning := range warnings {
		if strings.HasPrefix(warning, token) {
			return true
		}
	}
	return false
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func writePetJSON(t *testing.T, dir string, doc map[string]any) {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pet.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeSpriteDir(t *testing.T, doc map[string]any, cols, rows int, paint func(*image.NRGBA, int, int)) string {
	t.Helper()
	dir := t.TempDir()
	cellW, cellH := 8, 8
	if frame, ok := doc["frame"].(map[string]int); ok {
		if frame["width"] > 0 {
			cellW = frame["width"]
		}
		if frame["height"] > 0 {
			cellH = frame["height"]
		}
	}
	img := image.NewNRGBA(image.Rect(0, 0, cols*cellW, rows*cellH))
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			if paint != nil {
				paint(img, col, row)
				continue
			}
			fillCell(img, col, row, cellW, cellH, color.NRGBA{R: 80, G: 90, B: 100, A: 255})
		}
	}
	f, err := os.Create(filepath.Join(dir, "spritesheet.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	writePetJSON(t, dir, doc)
	return dir
}

func fillCell(img *image.NRGBA, col, row, cellW, cellH int, c color.NRGBA) {
	x0 := col * cellW
	y0 := row * cellH
	for y := 0; y < cellH; y++ {
		for x := 0; x < cellW; x++ {
			img.SetNRGBA(x0+x, y0+y, c)
		}
	}
}
