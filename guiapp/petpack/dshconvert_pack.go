package petpack

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	xdraw "golang.org/x/image/draw"
)

type dshBuilt struct {
	report   DSHConvertReport
	files    map[string][]byte
	rows     []DSHRowReport
	dropped  []DSHDroppedFrame
	kept     int
	mirrored bool
	rigPath  string
}

// errDSHKeyframeBudget asks buildDSHPack to drop another frame and try again.
var errDSHKeyframeBudget = errors.New("rig keyframe budget")

func buildDSHPack(parsed *dshParsed) (*dshBuilt, error) {
	budgetRows, images := budgetRowsFromSheet(parsed)
	budget := BudgetDSHFrames(budgetRows, parsed.columns, maxRigTextures)
	if !keptSemantic(budget, "idle") {
		partial := &dshBuilt{rows: budget.Rows, dropped: budget.DroppedFrames, kept: budget.KeptFrames, mirrored: budget.MirroredLeft}
		return partial, rejectDSH(dshRejectNoIdle)
	}
	var last *dshBuilt
	for cap := maxRigTextures; cap >= 1; cap-- {
		if cap != maxRigTextures {
			budget = BudgetDSHFrames(budgetRows, parsed.columns, cap)
		}
		if !keptSemantic(budget, "idle") {
			return &dshBuilt{rows: budget.Rows, dropped: budget.DroppedFrames, kept: budget.KeptFrames, mirrored: budget.MirroredLeft}, rejectDSH(dshRejectNoIdle)
		}
		built, err := assembleDSHPack(parsed, images, budget)
		if err == nil {
			return built, nil
		}
		if errors.Is(err, errDSHKeyframeBudget) {
			last = built
			continue
		}
		return built, err
	}
	if last != nil {
		return last, fmt.Errorf("rig exceeds %d total keyframes", maxRigTotalKeyframes)
	}
	return nil, fmt.Errorf("rig exceeds %d total keyframes", maxRigTotalKeyframes)
}

func keptSemantic(budget DSHBudgetResult, sem string) bool {
	for _, frame := range budget.Kept {
		if frame.Semantic == sem {
			return true
		}
	}
	return false
}

func budgetRowsFromSheet(parsed *dshParsed) ([]DSHBudgetRow, map[image.Point]*image.NRGBA) {
	images := map[image.Point]*image.NRGBA{}
	var rows []DSHBudgetRow
	if len(parsed.named) == 0 {
		return nil, images
	}
	for _, named := range parsed.named {
		cols := named.columns
		if len(cols) == 0 {
			cols = make([]int, parsed.columns)
			for i := range cols {
				cols[i] = i
			}
		}
		row := DSHBudgetRow{Row: named.row, Semantic: named.semantic}
		for _, col := range cols {
			if named.row < 0 || named.row >= parsed.rows || col < 0 || col >= parsed.columns {
				continue
			}
			cell := blitCell(parsed.sheet, col, named.row, parsed.cellW, parsed.cellH)
			images[image.Pt(col, named.row)] = cell
			row.Cells = append(row.Cells, DSHBudgetCell{Column: col, Opaque: imageOpaque(cell)})
		}
		rows = append(rows, row)
	}
	return rows, images
}

func blitCell(sheet *image.NRGBA, col, row, cellW, cellH int) *image.NRGBA {
	r := image.Rect(col*cellW, row*cellH, (col+1)*cellW, (row+1)*cellH)
	dst := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(dst, dst.Bounds(), sheet, r.Min, draw.Src)
	return dst
}

func imageOpaque(img *image.NRGBA) bool {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.NRGBAAt(x, y).A != 0 {
				return true
			}
		}
	}
	return false
}

func flipNRGBA(src *image.NRGBA) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			dst.SetNRGBA(x, y, src.NRGBAAt(b.Dx()-1-x, y))
		}
	}
	return dst
}

func assembleDSHPack(parsed *dshParsed, images map[image.Point]*image.NRGBA, budget DSHBudgetResult) (*dshBuilt, error) {
	type keptImg struct {
		frame DSHKeptFrame
		img   *image.NRGBA
	}
	kept := make([]keptImg, 0, len(budget.Kept))
	for _, frame := range budget.Kept {
		img := images[image.Pt(frame.Column, frame.Row)]
		if img == nil {
			continue
		}
		if frame.Mirrored {
			img = flipNRGBA(img)
		}
		kept = append(kept, keptImg{frame: frame, img: img})
	}
	if len(kept) == 0 {
		return nil, rejectDSH(dshRejectNoIdle)
	}
	imgs := make([]*image.NRGBA, len(kept))
	for i := range kept {
		imgs[i] = kept[i].img
	}
	crop := sharedCrop(imgs)
	var encoded [][]byte
	var scale float64
	var factor float64
	for _, candidate := range []float64{1, 0.75, 0.5, 0.25} {
		factor = candidate
		encoded = encoded[:0]
		ok := true
		var total int
		w := maxInt(1, int(math.Round(float64(crop.Dx())*factor)))
		h := maxInt(1, int(math.Round(float64(crop.Dy())*factor)))
		if w > maxRigTextureEdge || h > maxRigTextureEdge {
			ok = false
			continue
		}
		for _, item := range kept {
			scaled := scaleNRGBA(cropNRGBA(item.img, crop), w, h)
			raw, err := encodePNG(scaled)
			if err != nil {
				return nil, err
			}
			if len(raw) > maxRigTextureBytes {
				ok = false
				break
			}
			total += len(raw)
			encoded = append(encoded, raw)
		}
		// Stills, JSON, and YAML sit beside the textures inside the 2 MiB cap.
		if !ok || total+256*1024 > MaxUncompressedBytes {
			continue
		}
		scale = math.Min(1, math.Min(240/float64(w), 240/float64(h)))
		break
	}
	if len(encoded) != len(kept) {
		return nil, fmt.Errorf("spritesheet cells do not fit the texture byte budget")
	}
	bySem := map[string][]int{}
	for i, item := range kept {
		bySem[item.frame.Semantic] = append(bySem[item.frame.Semantic], i)
	}
	idle := bySem["idle"]
	run := bySem["run-right"]
	walk := bySem["walk-right"]
	wave := bySem["wave"]
	jump := bySem["jump"]
	failed := bySem["failed"]
	waiting := bySem["waiting"]
	review := bySem["review"]
	sing := bySem["sing"]
	locSlots := run
	locSem := "run-right"
	if len(locSlots) == 0 {
		locSlots = walk
		locSem = "walk-right"
	}
	speakSlots := sing
	speakCue := ""
	speakLoop := false
	if len(speakSlots) > 0 {
		speakCue = "sing"
		speakLoop = true
	} else {
		speakSlots = review
	}
	if len(speakSlots) == 0 {
		speakSlots = idle
	}
	listenSlots := waiting
	if len(listenSlots) == 0 {
		listenSlots = idle
	}
	alertSlots := failed
	alertCue := ""
	if len(alertSlots) > 0 {
		alertCue = "alert"
	} else {
		alertSlots = idle
	}
	rig := Rig{
		Version: 1,
		Bones:   []RigBone{{Name: "root", X: 128, Y: 136}},
		Slots:   make([]RigSlot, len(kept)),
		Clips:   map[string]RigClip{},
	}
	var textures []string
	for i := range kept {
		name := fmt.Sprintf("frame_%02d", i)
		tex := fmt.Sprintf("rig/%s.png", name)
		textures = append(textures, tex)
		slot := RigSlot{Name: name, Bone: "root", Texture: tex, Z: 0}
		if scale > 0 && scale < 1 {
			slot.ScaleX = scale
			slot.ScaleY = scale
		}
		rig.Slots[i] = slot
	}
	add := func(name string, slots []int, loop bool, cue string, travel float64, soundLoop bool) {
		if len(slots) == 0 {
			return
		}
		rig.Clips[name] = flipbookClip(len(rig.Slots), slots, loop, cue, travel, soundLoop && cue != "")
	}
	add("idle_loop", idle, true, "", 0, false)
	add("listen_loop", listenSlots, true, "", 0, false)
	add("speak_loop", speakSlots, true, speakCue, 0, speakLoop)
	add("alert_loop", alertSlots, true, alertCue, 0, false)
	if len(wave) > 0 {
		add("wave_loop", wave, true, "", 0, false)
		add("wave_greet", wave, false, "greet", 0, false)
	}
	if len(jump) > 0 {
		add("jump_in", jump, false, "jump", 0, false)
		add("jump_hold", []int{jump[len(jump)-1]}, false, "", 0, false)
		add("jump_land", []int{idle[0]}, false, "land", 0, false)
	}
	if len(locSlots) > 0 {
		cue, travel, sound := cueForSemantic(locSem)
		name := "run_loop"
		if locSem == "walk-right" {
			name = "walk_loop"
		}
		add(name, locSlots, true, cue, travel, sound)
	}
	if slots := bySem["gaze-left"]; len(slots) > 0 {
		add("look_left_loop", slots, true, "", 0, false)
	}
	if slots := bySem["gaze-right"]; len(slots) > 0 {
		add("look_right_loop", slots, true, "", 0, false)
	}
	for _, pose := range []struct {
		name string
		x, y float64
	}{
		{"gaze_center", 0, 0},
		{"gaze_left", -8, 0},
		{"gaze_right", 8, 0},
		{"gaze_up", 0, -8},
		{"expr_calm", 0, 0},
		{"expr_listen", 1, 0},
		{"expr_think", 0, -1},
		{"expr_speak", 2, 0},
		{"expr_done", 0, -2},
		{"expr_alert", -1, 0},
		{"expr_quiet", 0, 1},
	} {
		rig.Clips[pose.name] = poseClip(pose.x, pose.y)
	}
	performer := buildDSHPerformer(bySem, speakCue)
	packID := dshPackID(parsed.id, parsed.displayName)
	warnings := append([]string{}, parsed.warnings...)
	warnings = append(warnings,
		"no_thinking_row: 图集没有思考行，thinking 使用待机画面。",
		"blink_not_in_atlas: 图集没有单独的眨眼格。",
		"expression_baked_in_frame: 表情已经画在全身帧里，表情层只做很小的位移。",
	)
	if len(locSlots) == 0 {
		warnings = append(warnings, "no_locomotion: 没有可识别的跑或走，角色会以待机为主。")
	}
	if budget.MirroredLeft {
		warnings = append(warnings, "mirrored_left_row: 唯一的朝左移动行已镜像为朝右。")
	}
	for _, drop := range budget.DroppedFrames {
		if drop.Reason == "duplicate_locomotion" {
			warnings = append(warnings, "dropped_duplicate_locomotion: 只保留一条移动循环。")
			break
		}
	}
	rows := annotateRows(budget.Rows, speakCue)
	rigRaw, err := json.MarshalIndent(rig, "", "  ")
	if err != nil {
		return nil, err
	}
	perfRaw, err := json.MarshalIndent(performer, "", "  ")
	if err != nil {
		return nil, err
	}
	rigPath := "rig/" + packID + ".pet-rig.json"
	perfPath := "character/" + packID + ".performer.json"
	files := map[string][]byte{
		rigPath:  rigRaw,
		perfPath: perfRaw,
	}
	for i, raw := range encoded {
		files[fmt.Sprintf("rig/frame_%02d.png", i)] = raw
	}
	stills := map[string]int{
		"idle":      idle[0],
		"listening": listenSlots[0],
		"thinking":  idle[0],
		"speaking":  speakSlots[0],
		"done":      idle[0],
		"alert":     alertSlots[0],
		"quiet":     idle[0],
	}
	for state, slot := range stills {
		pngRaw, err := encodeStill(encoded[slot], scale)
		if err != nil {
			return nil, err
		}
		files["native/"+state+".png"] = pngRaw
	}
	files["preview.png"] = append([]byte(nil), files["native/idle.png"]...)
	name := strings.TrimSpace(parsed.displayName)
	if name == "" {
		name = packID
	}
	desc := strings.TrimSpace(parsed.description)
	if desc != "" {
		desc += " "
	}
	desc += "画面来自用户本地图集。美术授权由使用者负责，MaClaw 不授予、也不限制该授权。"
	files["pet-pack.yaml"] = []byte(renderDSHManifest(packID, name, desc, textures, rigPath, perfPath))
	built := &dshBuilt{
		files:    files,
		rows:     rows,
		dropped:  budget.DroppedFrames,
		kept:     len(kept),
		mirrored: budget.MirroredLeft,
		rigPath:  rigPath,
		report: DSHConvertReport{
			PackID:        packID,
			Warnings:      warnings,
			Rows:          rows,
			KeptFrames:    len(kept),
			DroppedFrames: budget.DroppedFrames,
			MirroredLeft:  budget.MirroredLeft,
			TextureScale:  scale,
		},
	}
	if over, reason := dshRigOverBudget(rigRaw); over {
		return built, fmt.Errorf("%w: %s", errDSHKeyframeBudget, reason)
	}
	if err := validateDSHFiles(files, textures); err != nil {
		return built, err
	}
	return built, nil
}

func sharedCrop(imgs []*image.NRGBA) image.Rectangle {
	var box image.Rectangle
	first := true
	for _, img := range imgs {
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				if img.NRGBAAt(x, y).A == 0 {
					continue
				}
				pt := image.Rect(x, y, x+1, y+1)
				if first {
					box = pt
					first = false
				} else {
					box = box.Union(pt)
				}
			}
		}
	}
	if first {
		if len(imgs) == 0 {
			return image.Rect(0, 0, 1, 1)
		}
		return imgs[0].Bounds()
	}
	box = image.Rect(box.Min.X-1, box.Min.Y-1, box.Max.X+1, box.Max.Y+1)
	limit := imgs[0].Bounds()
	return box.Intersect(limit)
}

func cropNRGBA(src *image.NRGBA, r image.Rectangle) *image.NRGBA {
	r = r.Intersect(src.Bounds())
	if r.Empty() {
		r = src.Bounds()
	}
	dst := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(dst, dst.Bounds(), src, r.Min, draw.Src)
	return dst
}

func scaleNRGBA(src *image.NRGBA, w, h int) *image.NRGBA {
	if src.Bounds().Dx() == w && src.Bounds().Dy() == h {
		return src
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeStill(pngRaw []byte, scale float64) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(pngRaw))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	dw := maxInt(1, int(math.Round(float64(b.Dx())*scaleOrOne(scale))))
	dh := maxInt(1, int(math.Round(float64(b.Dy())*scaleOrOne(scale))))
	canvas := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	x0 := 128 - dw/2
	y0 := 136 - dh/2
	scaled := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), img, b, xdraw.Over, nil)
	draw.Draw(canvas, image.Rect(x0, y0, x0+dw, y0+dh), scaled, image.Point{}, draw.Over)
	return encodePNG(canvas)
}

func scaleOrOne(scale float64) float64 {
	if scale <= 0 {
		return 1
	}
	return scale
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func flipbookClip(slotCount int, visible []int, loop bool, cue string, travel float64, soundLoop bool) RigClip {
	n := len(visible)
	if n < 1 {
		n = 1
	}
	dur := n * 120
	tracks := make(map[string][]RigKeyframe, slotCount)
	for s := 0; s < slotCount; s++ {
		alphaAt := func(frame int) float64 {
			if frame >= 0 && frame < len(visible) && visible[frame] == s {
				return 1
			}
			return 0
		}
		keys := make([]RigKeyframe, 0, 2*n+1)
		keys = append(keys, alphaKey(0, alphaAt(0)))
		for b := 1; b < n; b++ {
			keys = append(keys, alphaKey(b*120-1, alphaAt(b-1)), alphaKey(b*120, alphaAt(b)))
		}
		keys = append(keys, alphaKey(dur-1, alphaAt(n-1)))
		end := alphaAt(n - 1)
		if loop {
			end = alphaAt(0)
		}
		keys = append(keys, alphaKey(dur, end))
		tracks[fmt.Sprintf("frame_%02d", s)] = keys
	}
	return RigClip{DurationMS: dur, Loop: loop, Tracks: tracks, Cue: cue, TravelX: travel, SoundLoop: soundLoop}
}

func alphaKey(at int, alpha float64) RigKeyframe {
	a := alpha
	return RigKeyframe{AtMS: at, Alpha: &a, Ease: "linear"}
}

func poseClip(x, y float64) RigClip {
	key := func(at int) RigKeyframe {
		return RigKeyframe{AtMS: at, X: x, Y: y, Ease: "linear"}
	}
	return RigClip{DurationMS: 800, Loop: true, Tracks: map[string][]RigKeyframe{"root": {key(0), key(799), key(800)}}}
}

func annotateRows(rows []DSHRowReport, speakCue string) []DSHRowReport {
	out := append([]DSHRowReport(nil), rows...)
	for i := range out {
		cue, travel, _ := cueForSemantic(out[i].Semantic)
		if out[i].Semantic == "wave" {
			cue = ""
		}
		if out[i].Semantic == "review" {
			cue = ""
			travel = 0
		}
		if out[i].Semantic == "sing" {
			cue = speakCue
		}
		if out[i].Semantic == "waiting" || out[i].Semantic == "idle" {
			cue = ""
			travel = 0
		}
		out[i].Cue = cue
		out[i].TravelX = travel
	}
	return out
}

func buildDSHPerformer(bySem map[string][]int, speakCue string) Performer {
	idle := "idle_loop"
	listen := "listen_loop"
	speak := "speak_loop"
	alert := "alert_loop"
	wave := idle
	if len(bySem["wave"]) > 0 {
		wave = "wave_loop"
	}
	greet := idle
	if len(bySem["wave"]) > 0 {
		greet = "wave_greet"
	}
	lookL := idle
	if len(bySem["gaze-left"]) > 0 {
		lookL = "look_left_loop"
	}
	lookR := idle
	if len(bySem["gaze-right"]) > 0 {
		lookR = "look_right_loop"
	}
	jumpIn, jumpHold, jumpLand := idle, idle, idle
	if len(bySem["jump"]) > 0 {
		jumpIn, jumpHold, jumpLand = "jump_in", "jump_hold", "jump_land"
	}
	loc := idle
	switch {
	case len(bySem["run-right"]) > 0:
		loc = "run_loop"
	case len(bySem["walk-right"]) > 0:
		loc = "walk_loop"
	}
	reviewBody := speak
	if speakCue == "sing" {
		reviewBody = idle
	}
	beh := func(enter, loop, exit string, weight int, moods []string) PerformerBehavior {
		return PerformerBehavior{Enter: enter, Loop: loop, Exit: exit, Weight: weight, MinMS: 900, MaxMS: 3200, Moods: moods}
	}
	behaviors := map[string]PerformerBehavior{
		"idle_breathe":    beh(idle, idle, idle, 5, nil),
		"idle_shift":      beh(idle, idle, idle, 1, nil),
		"idle_blink":      beh(idle, idle, idle, 1, nil),
		"idle_wait":       beh(listen, listen, listen, 1, nil),
		"idle_look_left":  beh(lookL, lookL, lookL, 1, []string{"calm", "curious"}),
		"idle_look_right": beh(lookR, lookR, lookR, 1, []string{"calm", "curious"}),
		"idle_wave":       beh(wave, wave, wave, 1, []string{"calm", "pleased"}),
		"idle_run":        beh(loc, loc, loc, 1, []string{"calm", "pleased"}),
		"idle_jump":       beh(jumpIn, jumpHold, jumpLand, 1, []string{"calm", "pleased"}),
		"idle_review":     beh(reviewBody, reviewBody, reviewBody, 1, []string{"calm", "focused"}),
		"idle_rest":       beh(idle, idle, idle, 1, []string{"calm", "tired"}),
	}
	pool := []string{"idle_breathe", "idle_shift", "idle_blink", "idle_wait", "idle_look_left", "idle_look_right", "idle_wave", "idle_run", "idle_jump", "idle_review", "idle_rest"}
	state := func(enter, loop, exit, mood, expr, gaze string) PerformerState {
		return PerformerState{Enter: enter, Loop: loop, Exit: exit, Mood: mood, Expression: expr, Gaze: gaze}
	}
	return Performer{
		Version:   1,
		Moods:     []string{"calm", "curious", "focused", "pleased", "concerned", "tired"},
		Layers:    []string{"body", "expression", "gaze", "secondary"},
		Behaviors: behaviors,
		States: map[string]PerformerState{
			"idle":      {BehaviorPool: pool, Mood: "calm", Expression: "expr_calm", Gaze: "gaze_center"},
			"listening": state(listen, listen, listen, "curious", "expr_listen", "gaze_left"),
			"thinking":  state(idle, idle, idle, "focused", "expr_think", "gaze_up"),
			"speaking":  state(speak, speak, speak, "pleased", "expr_speak", "gaze_center"),
			"done":      state(idle, idle, idle, "pleased", "expr_done", "gaze_right"),
			"alert":     state(alert, alert, alert, "concerned", "expr_alert", "gaze_right"),
			"quiet":     state(idle, idle, idle, "tired", "expr_quiet", "gaze_center"),
		},
		Events: map[string]PerformerEvent{
			"click":        {Play: greet, Interrupt: "soft", Mood: "pleased", CooldownMS: 2800},
			"hover":        {Play: idle, Interrupt: "soft", Mood: "curious", CooldownMS: 4000},
			"drag_start":   {Play: idle, Interrupt: "soft", Mood: "concerned", CooldownMS: 800},
			"drag_end":     {Play: idle, Interrupt: "soft", Mood: "calm", CooldownMS: 800},
			"task_started": {Play: listen, Interrupt: "after_loop", Mood: "focused", CooldownMS: 4000},
			"task_done":    {Play: wave, Interrupt: "after_loop", Mood: "pleased", CooldownMS: 6000},
			"task_failed":  {Play: alert, Interrupt: "after_loop", Mood: "concerned", CooldownMS: 6000},
			"long_idle":    {Play: idle, Interrupt: "after_loop", Mood: "tired", CooldownMS: 20000},
		},
		Reactions: map[string]PerformerReaction{
			"click_greet":    {Event: "click", Play: greet, Interrupt: "soft", Mood: "pleased", Weight: 4, CooldownMS: 2800},
			"click_hold":     {Event: "click", Play: idle, Interrupt: "soft", Mood: "pleased", Weight: 1, CooldownMS: 2800},
			"hover_a":        {Event: "hover", Play: idle, Interrupt: "soft", Mood: "curious", Weight: 1, CooldownMS: 4000},
			"hover_b":        {Event: "hover", Play: idle, Interrupt: "soft", Mood: "curious", Weight: 1, CooldownMS: 4000},
			"drag_brace":     {Event: "drag_start", Play: idle, Interrupt: "soft", Mood: "concerned", Weight: 1, CooldownMS: 800},
			"drag_release":   {Event: "drag_end", Play: idle, Interrupt: "soft", Mood: "calm", Weight: 1, CooldownMS: 800},
			"task_focus":     {Event: "task_started", Play: listen, Interrupt: "after_loop", Mood: "focused", Weight: 1, CooldownMS: 4000},
			"task_listen":    {Event: "task_started", Play: listen, Interrupt: "after_loop", Mood: "focused", Weight: 1, CooldownMS: 4000},
			"task_celebrate": {Event: "task_done", Play: wave, Interrupt: "after_loop", Mood: "pleased", Weight: 1, CooldownMS: 6000},
			"task_settle":    {Event: "task_done", Play: idle, Interrupt: "after_loop", Mood: "pleased", Weight: 1, CooldownMS: 6000},
			"task_concern":   {Event: "task_failed", Play: alert, Interrupt: "after_loop", Mood: "concerned", Weight: 1, CooldownMS: 6000},
			"long_idle_rest": {Event: "long_idle", Play: idle, Interrupt: "after_loop", Mood: "tired", Weight: 1, CooldownMS: 20000},
		},
		Rules: PerformerRules{NoRepeatLast: 3, CrossfadeMS: 80, MaxInterruptMS: 400},
	}
}

func dshTextureList(textures []string, indent string) string {
	var tex strings.Builder
	for _, item := range textures {
		tex.WriteString(indent)
		tex.WriteString("- ")
		tex.WriteString(item)
		tex.WriteByte('\n')
	}
	return tex.String()
}

func renderDSHManifest(id, name, desc string, textures []string, rigPath, perfPath string) string {
	textureBlock := dshTextureList(textures, "      ")
	variantTextures := dshTextureList(textures, "          ")
	return "schema_version: 3\n" +
		"id: " + id + "\n" +
		"name: " + strconv.Quote(name) + "\n" +
		"version: 3.0.0\n" +
		"author: dsh-import\n" +
		"license: user-supplied\n" +
		"description: " + strconv.Quote(desc) + "\n" +
		"tier: figurative\n" +
		"min_maclaw_version: \"0.0.0\"\n" +
		"preview: preview.png\n" +
		"default_size: 88\n" +
		"tone: balanced\n" +
		"tags: [\"imported\", \"dsh-sprite\"]\n" +
		"renderer: native-character\n" +
		"face_overlay: false\n" +
		"assets:\n" +
		"  preview: preview.png\n" +
		"  native:\n" +
		"    idle: native/idle.png\n" +
		"    listening: native/listening.png\n" +
		"    thinking: native/thinking.png\n" +
		"    speaking: native/speaking.png\n" +
		"    done: native/done.png\n" +
		"    alert: native/alert.png\n" +
		"    quiet: native/quiet.png\n" +
		"  rig:\n" +
		"    definition: " + rigPath + "\n" +
		"    textures:\n" + textureBlock +
		"  character:\n" +
		"    definition: " + perfPath + "\n" +
		"fallback:\n" +
		"  renderer: native-skeleton\n" +
		"  idle: native/idle.png\n" +
		"variants:\n" +
		"  - id: default\n" +
		"    tier: figurative\n" +
		"    renderer: native-character\n" +
		"    face_overlay: false\n" +
		"    assets:\n" +
		"      native:\n" +
		"        idle: native/idle.png\n" +
		"        listening: native/listening.png\n" +
		"        thinking: native/thinking.png\n" +
		"        speaking: native/speaking.png\n" +
		"        done: native/done.png\n" +
		"        alert: native/alert.png\n" +
		"        quiet: native/quiet.png\n" +
		"      rig:\n" +
		"        definition: " + rigPath + "\n" +
		"        textures:\n" + variantTextures +
		"      character:\n" +
		"        definition: " + perfPath + "\n" +
		"  - id: classic\n" +
		"    tier: classic\n" +
		"    renderer: procedural-fallback\n" +
		"    face_overlay: true\n" +
		"motion:\n" +
		"  idle_ms: 4000\n" +
		"  listening_ms: 1200\n" +
		"  thinking_ms: 1800\n" +
		"  speaking_ms: 1280\n" +
		"  amplitude: 1\n" +
		"  sound_profile: classic\n" +
		"  pitch: 1\n" +
		"capabilities:\n" +
		"  supports_done_state: true\n" +
		"  supports_alert_state: true\n" +
		"  pet_performance_v3: true\n"
}

func validateDSHFiles(files map[string][]byte, textures []string) error {
	manifest, err := parseManifest(files["pet-pack.yaml"])
	if err != nil {
		return err
	}
	if manifest.Assets.Rig == nil || manifest.Assets.Character == nil {
		return fmt.Errorf("manifest is missing rig or character")
	}
	rigPath := manifest.Assets.Rig.Definition
	perfPath := manifest.Assets.Character.Definition
	assets := &PetPackRigAssets{Definition: rigPath, Textures: textures}
	if _, err := ValidateCharacterDefinition(files[perfPath], files[rigPath], assets); err != nil {
		return err
	}
	tex := map[string][]byte{}
	for _, item := range textures {
		tex[item] = files[item]
	}
	if err := ValidateRigTextureData(assets, tex); err != nil {
		return err
	}
	for _, state := range []string{"idle", "listening", "thinking", "speaking", "done", "alert", "quiet"} {
		if err := ValidateStaticFrameData(files["native/"+state+".png"]); err != nil {
			return err
		}
	}
	if manifest.ID == DefaultPackID || manifest.ID == LegacyClawMatePackID {
		return fmt.Errorf("refusing official pack id %s", manifest.ID)
	}
	var total int
	if len(files) > MaxZipFiles {
		return fmt.Errorf("too many files")
	}
	for name, raw := range files {
		total += len(raw)
		if len(raw) > MaxSingleFileBytes {
			return fmt.Errorf("file %s exceeds %d bytes", name, MaxSingleFileBytes)
		}
		if len(name) > MaxPathLength {
			return fmt.Errorf("path too long")
		}
	}
	if total > MaxUncompressedBytes {
		return fmt.Errorf("uncompressed pack exceeds %d bytes", MaxUncompressedBytes)
	}
	return nil
}

func dshRigOverBudget(raw []byte) (bool, string) {
	var rig Rig
	if err := json.Unmarshal(raw, &rig); err != nil {
		return true, err.Error()
	}
	total := 0
	for name, clip := range rig.Clips {
		for track, frames := range clip.Tracks {
			if len(frames) > maxRigKeyframes {
				return true, fmt.Sprintf("clip %s track %s has %d keyframes", name, track, len(frames))
			}
			total += len(frames)
		}
	}
	if total > maxRigTotalKeyframes {
		return true, fmt.Sprintf("%d keyframes", total)
	}
	return false, ""
}

func writeDSHPack(req DSHConvertRequest, built *dshBuilt) error {
	if built == nil {
		return fmt.Errorf("nothing to write")
	}
	parent := strings.TrimSpace(req.OutputDir)
	keepDir := parent != ""
	if parent == "" {
		parent = os.TempDir()
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(parent, built.report.PackID+".tmp-dshconvert")
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tmp)
		}
	}()
	for name, raw := range built.files {
		dst := filepath.Join(tmp, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, raw, 0o644); err != nil {
			return err
		}
	}
	if strings.TrimSpace(req.ZipPath) != "" {
		if err := writeDSHZip(req.ZipPath, built.report.PackID, built.files); err != nil {
			return err
		}
		built.report.ZipPath = req.ZipPath
	}
	if keepDir {
		final := filepath.Join(parent, built.report.PackID)
		_ = os.RemoveAll(final)
		if err := os.Rename(tmp, final); err != nil {
			return err
		}
		cleanup = false
		built.report.OutputDir = final
	}
	return nil
}

func writeDSHZip(dest, id string, files map[string][]byte) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil && filepath.Dir(dest) != "." {
		return err
	}
	raw, err := zipDSH(id, files, true)
	if err != nil {
		return err
	}
	var uncompressed int
	for _, file := range files {
		uncompressed += len(file)
	}
	if len(raw) > 0 && float64(uncompressed)/float64(len(raw)) > MaxCompressionRatio {
		raw, err = zipDSH(id, files, false)
		if err != nil {
			return err
		}
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Remove(dest)
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func zipDSH(id string, files map[string][]byte, deflate bool) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	method := zip.Deflate
	if !deflate {
		method = zip.Store
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		header := &zip.FileHeader{Name: path.Join(id, name), Method: method}
		w, err := zw.CreateHeader(header)
		if err != nil {
			_ = zw.Close()
			return nil, err
		}
		if _, err := w.Write(files[name]); err != nil {
			_ = zw.Close()
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
