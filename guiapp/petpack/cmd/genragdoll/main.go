// Generate the official Ragdoll cat-girl pet pack.
//
// Run from guiapp/petpack:
//
//	go run ./cmd/genragdoll
//
// Layers are drawn in code so each texture's joint sits on the geometric
// center, which is the pivot the skeleton renderer uses. DeepSeek Harness
// pets are not inputs: this pack is MaClaw YAML, rig JSON, and PNG only.
package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/guiapp/petpack"
)

func main() {
	if _, err := os.Stat("bundled"); err != nil {
		panic("run from guiapp/petpack so bundled/ is the output root")
	}
	dir := filepath.Join("bundled", "ragdoll")
	for _, sub := range []string{"native", "rig", "character"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			panic(err)
		}
	}
	textures := drawTextures(filepath.Join(dir, "rig"))
	rig := buildRig()
	performer := buildPerformer()
	rigRaw, err := json.MarshalIndent(rig, "", "  ")
	if err != nil {
		panic(err)
	}
	performerRaw, err := json.MarshalIndent(performer, "", "  ")
	if err != nil {
		panic(err)
	}
	assets := &petpack.PetPackRigAssets{
		Definition: "rig/ragdoll.pet-rig.json",
		Textures:   textures,
	}
	if err := os.WriteFile(filepath.Join(dir, "rig", "ragdoll.pet-rig.json"), rigRaw, 0o644); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "character", "ragdoll.performer.json"), performerRaw, 0o644); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pet-pack.yaml"), []byte(manifestYAML), 0o644); err != nil {
		panic(err)
	}
	if _, err := petpack.ValidateCharacterDefinition(performerRaw, rigRaw, assets); err != nil {
		panic(err)
	}
	texBytes := map[string][]byte{}
	for _, rel := range textures {
		raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			panic(err)
		}
		texBytes[rel] = raw
	}
	if err := petpack.ValidateRigTextureData(assets, texBytes); err != nil {
		panic(err)
	}

	resolved := &petpack.ResolvedPack{
		Manifest:  &petpack.PetPackManifest{ID: "ragdoll"},
		Renderer:  petpack.RendererCharacter,
		AssetFS:   os.DirFS(dir),
		Rig:       assets,
		Character: &petpack.PetPackCharacterAssets{Definition: "character/ragdoll.performer.json"},
	}
	renderer, err := petpack.NewRigRenderer(resolved)
	if err != nil || renderer == nil {
		panic(fmt.Sprintf("renderer: %v", err))
	}
	stills := []struct {
		name  string
		clips []string
		at    int
	}{
		{"idle", []string{"idle_breathe_loop", "expr_calm", "gaze_center", "secondary_idle"}, 900},
		{"listening", []string{"listen_loop", "expr_curious", "gaze_left", "secondary_listen"}, 400},
		{"thinking", []string{"think_loop", "expr_focus", "gaze_up", "secondary_think"}, 500},
		{"speaking", []string{"speak_loop", "expr_speak", "gaze_center", "secondary_speak"}, 480},
		{"done", []string{"done_loop", "expr_pleased", "gaze_center", "secondary_done"}, 240},
		{"alert", []string{"alert_loop", "expr_concerned", "gaze_right", "secondary_alert"}, 200},
		{"quiet", []string{"quiet_loop", "expr_tired", "gaze_center", "secondary_quiet"}, 600},
	}
	var preview *image.NRGBA
	for _, still := range stills {
		frame := renderer.RenderClips(still.clips, still.at, 256)
		if frame == nil {
			panic("empty still " + still.name)
		}
		path := filepath.Join(dir, "native", still.name+".png")
		if err := writePNG(path, frame); err != nil {
			panic(err)
		}
		if still.name == "idle" {
			preview = frame
		}
		fmt.Println("wrote", path)
	}
	if err := writePNG(filepath.Join(dir, "preview.png"), preview); err != nil {
		panic(err)
	}
	front := filepath.Join("..", "frontend", "src", "assets", "images", "maclaw-ragdoll.png")
	if err := writePNG(front, preview); err != nil {
		panic(err)
	}
	qa := filepath.Join(os.TempDir(), "maclaw-ragdoll-qa")
	_ = os.MkdirAll(qa, 0o755)
	for _, sample := range []struct {
		name  string
		clips []string
		at    int
	}{
		{"jump", []string{"jump_loop", "expr_pleased", "gaze_center", "secondary_idle"}, 200},
		{"bend", []string{"bend_loop", "expr_calm", "gaze_center", "secondary_idle"}, 400},
		{"walk", []string{"walk_loop", "expr_calm", "gaze_center", "secondary_idle"}, 160},
		{"run", []string{"run_loop", "expr_pleased", "gaze_center", "secondary_idle"}, 120},
		{"sing", []string{"sing_loop", "expr_speak", "gaze_center", "secondary_speak"}, 480},
	} {
		frame := renderer.RenderClips(sample.clips, sample.at, 256)
		path := filepath.Join(qa, sample.name+".png")
		if err := writePNG(path, frame); err != nil {
			panic(err)
		}
		fmt.Println("qa", path)
	}
	idleTop := opaqueTop(preview)
	jumpTop := opaqueTop(renderer.RenderClips([]string{"jump_loop"}, 200, 256))
	if jumpTop >= idleTop-12 {
		panic(fmt.Sprintf("jump does not clear the idle pose: jumpTop=%d idleTop=%d", jumpTop, idleTop))
	}
	idleX := centroidX(renderer.RenderClips([]string{"idle_breathe_loop"}, 0, 256))
	bendX := centroidX(renderer.RenderClips([]string{"bend_loop"}, 400, 256))
	if bendX < idleX+6 {
		panic(fmt.Sprintf("bend does not shift right: bend=%0.1f idle=%0.1f", bendX, idleX))
	}
	fmt.Println("ragdoll pack ok", dir)
}

const manifestYAML = `schema_version: 3
id: ragdoll
name: Ragdoll
version: 3.0.0
author: MaClaw Official
license: proprietary
description: "A ragdoll cat-girl desktop companion who walks, runs, jumps, bows, and sings"
tier: figurative
min_maclaw_version: "0.0.0"
label:
  en: Ragdoll
  zh-Hans: 布偶猫
  zh-Hant: 布偶貓
description_i18n:
  en: "A ragdoll cat-girl who walks the desktop, runs, jumps, bows, and sings"
  zh-Hans: "会在桌面上走动、奔跑、跳跃、弯腰和唱歌的布偶猫女娘"
  zh-Hant: "會在桌面上走動、奔跑、跳躍、彎腰和唱歌的布偶貓女娘"
preview: preview.png
default_size: 224
tone: balanced
tags: ["official", "default"]
renderer: native-character
face_overlay: false
assets:
  preview: preview.png
  native:
    idle: native/idle.png
    listening: native/listening.png
    thinking: native/thinking.png
    speaking: native/speaking.png
    done: native/done.png
    alert: native/alert.png
    quiet: native/quiet.png
  rig:
    definition: rig/ragdoll.pet-rig.json
    textures:
      - rig/tail.png
      - rig/leg-l.png
      - rig/leg-r.png
      - rig/torso.png
      - rig/arm-l.png
      - rig/arm-r.png
      - rig/head.png
      - rig/mouth.png
      - rig/eyes.png
  character:
    definition: character/ragdoll.performer.json
fallback:
  renderer: native-skeleton
  idle: native/idle.png
variants:
  - id: default
    tier: figurative
    renderer: native-character
    face_overlay: false
    assets:
      native:
        idle: native/idle.png
        listening: native/listening.png
        thinking: native/thinking.png
        speaking: native/speaking.png
        done: native/done.png
        alert: native/alert.png
        quiet: native/quiet.png
      rig:
        definition: rig/ragdoll.pet-rig.json
        textures:
          - rig/tail.png
          - rig/leg-l.png
          - rig/leg-r.png
          - rig/torso.png
          - rig/arm-l.png
          - rig/arm-r.png
          - rig/head.png
          - rig/mouth.png
          - rig/eyes.png
      character:
        definition: character/ragdoll.performer.json
  - id: classic
    tier: classic
    renderer: procedural-fallback
    face_overlay: true
motion:
  idle_ms: 4000
  listening_ms: 1200
  thinking_ms: 1800
  speaking_ms: 1280
  amplitude: 1
  sound_profile: classic
  pitch: 1
capabilities:
  supports_done_state: true
  supports_alert_state: true
  pet_performance_v3: true
`

func buildRig() *petpack.Rig {
	clips := map[string]petpack.RigClip{}
	add := func(name string, clip petpack.RigClip) {
		clips[name] = clip
	}
	// Idle: breathe, look, blink, then the large actions.
	add("idle_breathe_in", clip(420, false, 0, "", false, tr().
		k("root", kf(0, 0, 2, 0, ""), kf(420, 0, 0, 0, "ease-in-out")).
		k("torso", kfs(0, 0.98, ""), kfs(420, 1.02, "ease-in-out"))))
	add("idle_breathe_loop", clip(3200, true, 80, "", false, tr().
		k("root", kf(0, 0, 0, 0, ""), kf(800, 0, -4, 0, "ease-in-out"), kf(1800, 0, -1, 0, "ease-in-out"), kf(3200, 0, 0, 0, "ease-in-out")).
		k("torso", kfs(0, 1, ""), kfs(800, 1.04, "ease-in-out"), kfs(1800, 1.015, "ease-in-out"), kfs(3200, 1, "ease-in-out"))))
	add("idle_breathe_out", clip(360, false, 0, "", false, tr().
		k("root", kf(0, 0, -2, 0, ""), kf(360, 0, 0, 0, "ease-in-out")).
		k("torso", kfs(0, 1.02, ""), kfs(360, 1, "ease-in-out"))))

	add("idle_look_left_in", clip(420, false, 0, "", false, tr().
		k("head", kf(0, 0, 0, 4, ""), kf(120, 0, 0, 8, "ease-out"), kf(420, -2, 0, -18, "ease-in-out"))))
	add("idle_look_left_loop", clip(2600, true, 0, "", false, tr().
		k("head", kf(0, 0, 0, -18, ""), kf(900, -4, 0, -26, "ease-in-out"), kf(1800, -2, 0, -16, "ease-in-out"), kf(2600, 0, 0, -18, "ease-in-out"))))
	add("idle_look_left_out", clip(380, false, 0, "", false, tr().
		k("head", kf(0, -2, 0, -16, ""), kf(380, 0, 0, 0, "ease-in-out"))))

	add("idle_look_right_in", clip(420, false, 0, "", false, tr().
		k("head", kf(0, 0, 0, -4, ""), kf(120, 0, 0, -8, "ease-out"), kf(420, 2, 0, 18, "ease-in-out"))))
	add("idle_look_right_loop", clip(2600, true, 0, "", false, tr().
		k("head", kf(0, 0, 0, 18, ""), kf(900, 4, 0, 26, "ease-in-out"), kf(1800, 2, 0, 16, "ease-in-out"), kf(2600, 0, 0, 18, "ease-in-out"))))
	add("idle_look_right_out", clip(380, false, 0, "", false, tr().
		k("head", kf(0, 2, 0, 16, ""), kf(380, 0, 0, 0, "ease-in-out"))))

	add("idle_blink_in", clip(160, false, 0, "blink", false, tr().
		k("eyes", kfs(0, 1, ""), kfs(160, 0.16, "ease-out"))))
	add("idle_blink_loop", clip(1100, true, 0, "", false, tr().
		k("eyes", kfs(0, 0.16, ""), kfs(140, 1, "ease-out"), kfs(700, 1, ""), kfs(820, 0.2, "ease-out"), kfs(980, 1, "ease-out"), kfs(1100, 0.16, "ease-in-out"))))
	add("idle_blink_out", clip(180, false, 0, "", false, tr().
		k("eyes", kfs(0, 0.2, ""), kfs(180, 1, "ease-out"))))

	add("walk_in", clip(360, false, 0, "", false, tr().
		k("root", kf(0, 0, -2, 0, ""), kf(160, 0, 6, 0, "ease-out"), kf(360, 0, 0, 0, "ease-in-out")).
		k("leg_l", kf(0, 0, 0, 0, ""), kf(360, 0, -8, 8, "ease-in-out")).
		k("arm_r", kf(0, 0, 0, 0, ""), kf(360, 0, 0, -18, "ease-in-out"))))
	add("walk_loop", clip(640, true, 110, "step", true, march(640, 26, 36, 8)))
	add("walk_out", clip(340, false, 0, "", false, tr().
		k("root", kf(0, 0, -4, 0, ""), kf(340, 0, 0, 0, "ease-in-out")).
		k("leg_l", kf(0, 0, -8, 6, ""), kf(340, 0, 0, 0, "ease-in-out")).
		k("leg_r", kf(0, 0, 2, -4, ""), kf(340, 0, 0, 0, "ease-in-out")).
		k("arm_l", kf(0, 0, 0, 16, ""), kf(340, 0, 0, 0, "ease-in-out")).
		k("arm_r", kf(0, 0, 0, -16, ""), kf(340, 0, 0, 0, "ease-in-out"))))

	add("run_in", clip(280, false, 0, "", false, tr().
		k("root", kf(0, 0, 0, 0, ""), kf(120, 0, 8, 0, "ease-out"), kf(280, 0, -6, 0, "ease-in-out")).
		k("torso", kf(0, 0, 0, 0, ""), kf(280, 0, 0, -8, "ease-out"))))
	add("run_loop", clip(420, true, 200, "run", true, march(420, 40, 58, 16)))
	add("run_out", clip(300, false, 0, "land", false, tr().
		k("root", kf(0, 0, -10, 0, ""), kf(140, 0, 8, 0, "ease-out"), kf(300, 0, 0, 0, "ease-in-out")).
		k("leg_l", kf(0, 0, -12, 10, ""), kf(300, 0, 0, 0, "ease-in-out")).
		k("leg_r", kf(0, 0, 4, -8, ""), kf(300, 0, 0, 0, "ease-in-out"))))

	add("jump_in", clip(340, false, 0, "jump", false, tr().
		k("root", kf(0, 0, 0, 0, ""), kf(140, 0, 12, 0, "ease-out"), kf(340, 0, -28, 0, "ease-out")).
		k("leg_l", kf(0, 0, 0, 0, ""), kf(140, 0, 4, 22, "ease-out"), kf(340, 0, -6, 18, "ease-in-out")).
		k("leg_r", kf(0, 0, 0, 0, ""), kf(140, 0, 4, -22, "ease-out"), kf(340, 0, -6, -18, "ease-in-out")).
		k("arm_l", kf(0, 0, 0, 0, ""), kf(340, 0, 0, 70, "ease-out")).
		k("arm_r", kf(0, 0, 0, 0, ""), kf(340, 0, 0, -70, "ease-out"))))
	add("jump_loop", clip(520, true, 0, "", false, tr().
		k("root", kf(0, 0, -56, 0, ""), kf(260, 0, -48, 0, "ease-in-out"), kf(520, 0, -56, 0, "ease-in-out")).
		k("leg_l", kf(0, 0, -8, 28, ""), kf(520, 0, -8, 28, "")).
		k("leg_r", kf(0, 0, -8, -28, ""), kf(520, 0, -8, -28, "")).
		k("arm_l", kf(0, 0, 0, 130, ""), kf(260, 0, -4, 146, "ease-in-out"), kf(520, 0, 0, 130, "ease-in-out")).
		k("arm_r", kf(0, 0, 0, -130, ""), kf(260, 0, -4, -146, "ease-in-out"), kf(520, 0, 0, -130, "ease-in-out")).
		k("torso", kf(0, 0, 0, -6, ""), kf(520, 0, 0, -6, ""))))
	add("jump_out", clip(360, false, 0, "land", false, tr().
		k("root", kf(0, 0, -20, 0, ""), kf(160, 0, 10, 0, "ease-out"), kf(360, 0, 0, 0, "ease-in-out")).
		k("leg_l", kf(0, 0, -4, 16, ""), kf(160, 0, 6, 20, "ease-out"), kf(360, 0, 0, 0, "ease-in-out")).
		k("leg_r", kf(0, 0, -4, -16, ""), kf(160, 0, 6, -20, "ease-out"), kf(360, 0, 0, 0, "ease-in-out")).
		k("arm_l", kf(0, 0, 0, 40, ""), kf(360, 0, 0, 0, "ease-in-out")).
		k("arm_r", kf(0, 0, 0, -40, ""), kf(360, 0, 0, 0, "ease-in-out"))))

	add("bend_in", clip(560, false, 0, "bend", false, tr().
		k("root", kf(0, 0, 0, 0, ""), kf(160, 0, -4, 0, "ease-out"), kf(560, 4, 10, 0, "ease-in-out")).
		k("torso", kf(0, 0, 0, -8, ""), kf(560, 0, 0, 62, "ease-in-out")).
		k("head", kf(0, 0, 0, 4, ""), kf(560, 0, 0, -40, "ease-in-out")).
		k("arm_l", kf(0, 0, 0, 0, ""), kf(560, 0, 6, 24, "ease-in-out")).
		k("arm_r", kf(0, 0, 0, 0, ""), kf(560, 0, 6, 18, "ease-in-out"))))
	add("bend_loop", clip(1400, true, 0, "", false, tr().
		k("root", kf(0, 4, 10, 0, ""), kf(700, 6, 12, 0, "ease-in-out"), kf(1400, 4, 10, 0, "ease-in-out")).
		k("torso", kf(0, 0, 0, 62, ""), kf(700, 0, 0, 68, "ease-in-out"), kf(1400, 0, 0, 62, "ease-in-out")).
		k("head", kf(0, 0, 0, -40, ""), kf(700, 0, 0, -46, "ease-in-out"), kf(1400, 0, 0, -40, "ease-in-out")).
		k("arm_l", kf(0, 0, 6, 24, ""), kf(1400, 0, 6, 24, "")).
		k("arm_r", kf(0, 0, 6, 18, ""), kf(1400, 0, 6, 18, ""))))
	add("bend_out", clip(480, false, 0, "", false, tr().
		k("torso", kf(0, 0, 0, 62, ""), kf(480, 0, 0, 0, "ease-in-out")).
		k("head", kf(0, 0, 0, -40, ""), kf(480, 0, 0, 0, "ease-in-out")).
		k("root", kf(0, 4, 10, 0, ""), kf(480, 0, 0, 0, "ease-in-out")).
		k("arm_l", kf(0, 0, 4, 20, ""), kf(480, 0, 0, 0, "ease-in-out")).
		k("arm_r", kf(0, 0, 4, 14, ""), kf(480, 0, 0, 0, "ease-in-out"))))

	add("stretch_in", clip(520, false, 0, "stretch", false, tr().
		k("root", kf(0, 0, 2, 0, ""), kf(180, 0, 6, 0, "ease-out"), kf(520, 0, -14, 0, "ease-in-out")).
		k("arm_l", kf(0, 0, 0, 10, ""), kf(520, 0, 0, 150, "ease-out")).
		k("arm_r", kf(0, 0, 0, -10, ""), kf(520, 0, 0, -150, "ease-out")).
		k("torso", kfs(0, 1, ""), kfs(520, 1.08, "ease-out"))))
	add("stretch_loop", clip(1600, true, 0, "", false, tr().
		k("root", kf(0, 0, -14, 0, ""), kf(600, 0, -20, 0, "ease-in-out"), kf(1600, 0, -14, 0, "ease-in-out")).
		k("arm_l", kf(0, 0, 0, 150, ""), kf(700, 0, -4, 162, "ease-in-out"), kf(1600, 0, 0, 150, "ease-in-out")).
		k("arm_r", kf(0, 0, 0, -150, ""), kf(800, 0, -4, -158, "ease-in-out"), kf(1600, 0, 0, -150, "ease-in-out"))))
	add("stretch_out", clip(440, false, 0, "", false, tr().
		k("root", kf(0, 0, -12, 0, ""), kf(440, 0, 0, 0, "ease-in-out")).
		k("arm_l", kf(0, 0, 0, 140, ""), kf(440, 0, 0, 0, "ease-in-out")).
		k("arm_r", kf(0, 0, 0, -140, ""), kf(440, 0, 0, 0, "ease-in-out")).
		k("torso", kfs(0, 1.06, ""), kfs(440, 1, "ease-in-out"))))

	add("sing_in", singIn())
	add("sing_loop", singLoop("sing"))
	add("sing_out", singOut())
	add("speak_in", singIn())
	add("speak_loop", singLoop("sing"))
	add("speak_out", singOut())

	add("rest_in", clip(520, false, 0, "rest", false, tr().
		k("root", kf(0, 0, 0, 0, ""), kf(520, 0, 14, 0, "ease-in-out")).
		k("head", kf(0, 0, 0, 0, ""), kf(520, 0, 4, 10, "ease-in-out")).
		k("eyes", kfs(0, 1, ""), kfs(520, 0.55, "ease-in-out")).
		k("arm_l", kf(0, 0, 0, 0, ""), kf(520, 0, 4, 12, "ease-in-out")).
		k("arm_r", kf(0, 0, 0, 0, ""), kf(520, 0, 4, -12, "ease-in-out"))))
	add("rest_loop", clip(3600, true, 0, "", false, tr().
		k("root", kf(0, 0, 14, 0, ""), kf(1600, 0, 16, 0, "ease-in-out"), kf(3600, 0, 14, 0, "ease-in-out")).
		k("eyes", kfs(0, 0.55, ""), kfs(1800, 0.42, "ease-in-out"), kfs(3600, 0.55, "ease-in-out"))))
	add("rest_out", clip(460, false, 0, "", false, tr().
		k("root", kf(0, 0, 14, 0, ""), kf(460, 0, 0, 0, "ease-in-out")).
		k("head", kf(0, 0, 4, 10, ""), kf(460, 0, 0, 0, "ease-in-out")).
		k("eyes", kfs(0, 0.55, ""), kfs(460, 1, "ease-out"))))

	add("listen_in", clip(400, false, 0, "", false, tr().
		k("head", kf(0, 0, 0, 2, ""), kf(400, -2, 0, -14, "ease-in-out")).
		k("torso", kf(0, 0, 0, 0, ""), kf(400, 0, 0, -6, "ease-in-out"))))
	add("listen_loop", clip(1500, true, 0, "", false, tr().
		k("head", kf(0, 0, 0, -14, ""), kf(700, -3, 0, -18, "ease-in-out"), kf(1500, 0, 0, -14, "ease-in-out"))))
	add("listen_out", clip(340, false, 0, "", false, tr().
		k("head", kf(0, -2, 0, -12, ""), kf(340, 0, 0, 0, "ease-in-out")).
		k("torso", kf(0, 0, 0, -4, ""), kf(340, 0, 0, 0, "ease-in-out"))))

	add("think_in", clip(420, false, 0, "", false, tr().
		k("root", kf(0, 0, 0, 0, ""), kf(420, 0, 2, 0, "ease-in-out")).
		k("head", kf(0, 0, 0, 0, ""), kf(420, 2, -2, 8, "ease-in-out")).
		k("arm_l", kf(0, 0, 0, 0, ""), kf(420, 6, -10, 48, "ease-out"))))
	add("think_loop", clip(1800, true, 72, "step", true, march(1800, 14, 22, 4)))
	add("think_out", clip(360, false, 0, "", false, tr().
		k("head", kf(0, 2, -2, 6, ""), kf(360, 0, 0, 0, "ease-in-out")).
		k("arm_l", kf(0, 4, -6, 30, ""), kf(360, 0, 0, 0, "ease-in-out"))))

	add("done_in", clip(420, false, 0, "celebrate", false, tr().
		k("root", kf(0, 0, 4, 0, ""), kf(160, 0, 10, 0, "ease-out"), kf(420, 0, -36, 0, "ease-out")).
		k("arm_l", kf(0, 0, 0, 0, ""), kf(420, 0, 0, 140, "ease-out")).
		k("arm_r", kf(0, 0, 0, 0, ""), kf(420, 0, 0, -140, "ease-out"))))
	add("done_loop", clip(900, true, 0, "", false, tr().
		k("root", kf(0, 0, -28, 0, ""), kf(360, 0, -16, 0, "ease-in-out"), kf(900, 0, -28, 0, "ease-in-out")).
		k("arm_l", kf(0, 0, 0, 130, ""), kf(900, 0, 0, 120, "ease-in-out")).
		k("arm_r", kf(0, 0, 0, -130, ""), kf(900, 0, 0, -120, "ease-in-out"))))
	add("done_out", clip(400, false, 0, "land", false, tr().
		k("root", kf(0, 0, -12, 0, ""), kf(160, 0, 8, 0, "ease-out"), kf(400, 0, 0, 0, "ease-in-out")).
		k("arm_l", kf(0, 0, 0, 80, ""), kf(400, 0, 0, 0, "ease-in-out")).
		k("arm_r", kf(0, 0, 0, -80, ""), kf(400, 0, 0, 0, "ease-in-out"))))

	add("alert_in", clip(280, false, 0, "alert", false, tr().
		k("root", kf(0, 0, 2, 0, ""), kf(100, 0, 6, 0, "ease-out"), kf(280, 0, -16, 0, "ease-out")).
		k("arm_l", kf(0, 0, 0, 0, ""), kf(280, 0, 0, 36, "ease-out")).
		k("arm_r", kf(0, 0, 0, 0, ""), kf(280, 0, 0, -36, "ease-out"))))
	add("alert_loop", clip(1100, true, 0, "", false, tr().
		k("root", kf(0, 0, -12, 0, ""), kf(400, 0, -8, 0, "ease-in-out"), kf(1100, 0, -12, 0, "ease-in-out"))))
	add("alert_out", clip(340, false, 0, "", false, tr().
		k("root", kf(0, 0, -8, 0, ""), kf(340, 0, 0, 0, "ease-in-out")).
		k("arm_l", kf(0, 0, 0, 20, ""), kf(340, 0, 0, 0, "ease-in-out")).
		k("arm_r", kf(0, 0, 0, -20, ""), kf(340, 0, 0, 0, "ease-in-out"))))

	add("quiet_in", clip(480, false, 0, "rest", false, tr().
		k("root", kf(0, 0, 0, 0, ""), kf(480, 0, 12, 0, "ease-in-out")).
		k("eyes", kfs(0, 1, ""), kfs(480, 0.5, "ease-in-out"))))
	add("quiet_loop", clip(4000, true, 0, "", false, tr().
		k("root", kf(0, 0, 12, 0, ""), kf(2000, 0, 14, 0, "ease-in-out"), kf(4000, 0, 12, 0, "ease-in-out")).
		k("eyes", kfs(0, 0.5, ""), kfs(4000, 0.46, "ease-in-out"))))
	add("quiet_out", clip(420, false, 0, "", false, tr().
		k("root", kf(0, 0, 12, 0, ""), kf(420, 0, 0, 0, "ease-in-out")).
		k("eyes", kfs(0, 0.5, ""), kfs(420, 1, "ease-out"))))

	add("expr_calm", clip(1400, true, 0, "", false, tr().k("eyes", kfs(0, 1, ""), kfs(700, 0.96, "ease-in-out"), kfs(1400, 1, "ease-in-out"))))
	add("expr_curious", clip(1000, true, 0, "", false, tr().k("eyes", sc(0, 1.12, 1.12, ""), sc(500, 1.18, 1.16, "ease-in-out"), sc(1000, 1.12, 1.12, "ease-in-out"))))
	add("expr_focus", clip(1200, true, 0, "", false, tr().k("eyes", sc(0, 0.9, 1.05, ""), sc(600, 0.86, 1.08, "ease-in-out"), sc(1200, 0.9, 1.05, "ease-in-out"))))
	add("expr_speak", clip(720, true, 0, "", false, tr().k("eyes", kfs(0, 1, ""), kfs(260, 0.9, "ease-in-out"), kfs(720, 1, "ease-in-out"))))
	add("expr_pleased", clip(1000, true, 0, "", false, tr().k("eyes", sc(0, 0.92, 0.72, ""), sc(500, 0.96, 0.78, "ease-in-out"), sc(1000, 0.92, 0.72, "ease-in-out"))))
	add("expr_concerned", clip(900, true, 0, "", false, tr().k("eyes", sc(0, 1.16, 1.08, ""), sc(450, 1.22, 1.12, "ease-in-out"), sc(900, 1.16, 1.08, "ease-in-out"))))
	add("expr_tired", clip(1800, true, 0, "", false, tr().k("eyes", kfs(0, 0.58, ""), kfs(900, 0.5, "ease-in-out"), kfs(1800, 0.58, "ease-in-out"))))

	add("gaze_wander", clip(2600, true, 0, "", false, tr().k("eyes", kf(0, 0, 0, 0, ""), kf(700, -3, 0, 0, "ease-in-out"), kf(1600, 3, -1, 0, "ease-in-out"), kf(2600, 0, 0, 0, "ease-in-out"))))
	add("gaze_left", clip(800, true, 0, "", false, tr().k("eyes", kf(0, -3, 0, 0, ""), kf(800, -4, 0, 0, "ease-in-out"))))
	add("gaze_right", clip(800, true, 0, "", false, tr().k("eyes", kf(0, 3, 0, 0, ""), kf(800, 4, 0, 0, "ease-in-out"))))
	add("gaze_up", clip(900, true, 0, "", false, tr().k("eyes", kf(0, 0, -3, 0, ""), kf(900, 1, -4, 0, "ease-in-out"))))
	add("gaze_center", clip(800, true, 0, "", false, tr().k("eyes", kf(0, 0, 0, 0, ""), kf(800, 1, 0, 0, "ease-in-out"))))

	add("secondary_idle", clip(2400, true, 0, "", false, tailSway(2400, 10)))
	add("secondary_listen", clip(1500, true, 0, "", false, tailSway(1500, 16)))
	add("secondary_think", clip(1800, true, 0, "", false, tailSway(1800, 8)))
	add("secondary_speak", clip(1100, true, 0, "", false, tailSway(1100, 22)))
	add("secondary_done", clip(900, true, 0, "", false, tailSway(900, 26)))
	add("secondary_alert", clip(800, true, 0, "", false, tailSway(800, 6)))
	add("secondary_quiet", clip(3200, true, 0, "", false, tailSway(3200, 4)))

	add("react_click_greet", clip(980, false, 0, "greet", false, tr().
		k("root", kf(0, 0, 2, 0, ""), kf(180, 0, 8, 0, "ease-out"), kf(460, 0, -18, 0, "ease-out"), kf(980, 0, 0, 0, "ease-in-out")).
		k("arm_r", kf(0, 0, 0, 0, ""), kf(460, 0, 0, -150, "ease-out"), kf(720, 0, 0, -110, "ease-in-out"), kf(980, 0, 0, 0, "ease-in-out")).
		k("head", kf(0, 0, 0, 0, ""), kf(460, 0, 0, 8, "ease-out"), kf(980, 0, 0, 0, "ease-in-out"))))
	add("react_click_notice", clip(760, false, 0, "notice", false, tr().
		k("root", kf(0, 0, 0, 0, ""), kf(200, 0, -8, 0, "ease-out"), kf(760, 0, 0, 0, "ease-in-out")).
		k("head", kf(0, 0, 0, 0, ""), kf(220, 0, 0, -12, "ease-out"), kf(760, 0, 0, 0, "ease-in-out"))))
	add("react_hover_peek", clip(720, false, 0, "notice", false, tr().
		k("head", kf(0, 0, 0, 0, ""), kf(180, 3, 0, 16, "ease-out"), kf(480, -2, 0, -8, "ease-in-out"), kf(720, 0, 0, 0, "ease-in-out"))))
	add("react_hover_blink", clip(520, false, 0, "blink", false, tr().
		k("eyes", kfs(0, 1, ""), kfs(90, 0.16, "ease-out"), kfs(220, 1, "ease-out"), kfs(520, 1, ""))))
	add("react_drag_brace", clip(520, false, 0, "brace", false, tr().
		k("root", kf(0, 0, 0, 0, ""), kf(160, 0, 8, 0, "ease-out"), kf(520, 0, 4, 0, "ease-in-out")).
		k("arm_l", kf(0, 0, 0, 0, ""), kf(240, 0, 0, 28, "ease-out"), kf(520, 0, 0, 16, "ease-in-out")).
		k("arm_r", kf(0, 0, 0, 0, ""), kf(240, 0, 0, -28, "ease-out"), kf(520, 0, 0, -16, "ease-in-out"))))
	add("react_drag_release", clip(640, false, 0, "release", false, tr().
		k("root", kf(0, 0, 4, 0, ""), kf(180, 0, -10, 0, "ease-out"), kf(640, 0, 0, 0, "ease-in-out")).
		k("arm_l", kf(0, 0, 0, 16, ""), kf(640, 0, 0, 0, "ease-in-out")).
		k("arm_r", kf(0, 0, 0, -16, ""), kf(640, 0, 0, 0, "ease-in-out"))))
	add("react_task_focus", clip(860, false, 0, "focus", false, tr().
		k("head", kf(0, 0, 0, 0, ""), kf(300, 2, -4, 6, "ease-out"), kf(860, 0, 0, 0, "ease-in-out")).
		k("arm_l", kf(0, 0, 0, 0, ""), kf(400, 4, -8, 40, "ease-out"), kf(860, 0, 0, 0, "ease-in-out"))))
	add("react_task_listen", clip(780, false, 0, "notice", false, tr().
		k("head", kf(0, 0, 0, 0, ""), kf(240, -4, 0, -16, "ease-out"), kf(780, 0, 0, 0, "ease-in-out"))))
	add("react_task_celebrate", clip(1100, false, 0, "celebrate", false, tr().
		k("root", kf(0, 0, 2, 0, ""), kf(180, 0, 10, 0, "ease-out"), kf(480, 0, -40, 0, "ease-out"), kf(780, 0, -8, 0, "ease-in-out"), kf(1100, 0, 0, 0, "ease-in-out")).
		k("arm_l", kf(0, 0, 0, 0, ""), kf(480, 0, 0, 150, "ease-out"), kf(1100, 0, 0, 0, "ease-in-out")).
		k("arm_r", kf(0, 0, 0, 0, ""), kf(480, 0, 0, -150, "ease-out"), kf(1100, 0, 0, 0, "ease-in-out"))))
	add("react_task_settle", clip(820, false, 0, "land", false, tr().
		k("root", kf(0, 0, -6, 0, ""), kf(280, 0, 6, 0, "ease-in-out"), kf(820, 0, 0, 0, "ease-in-out"))))
	add("react_task_concern", clip(900, false, 0, "concern", false, tr().
		k("torso", kf(0, 0, 0, 0, ""), kf(360, 0, 0, 28, "ease-in-out"), kf(900, 0, 0, 0, "ease-in-out")).
		k("head", kf(0, 0, 0, 0, ""), kf(360, 0, 2, 12, "ease-in-out"), kf(900, 0, 0, 0, "ease-in-out"))))
	add("react_long_idle_rest", clip(1200, false, 0, "rest", false, tr().
		k("root", kf(0, 0, 0, 0, ""), kf(400, 0, 12, 0, "ease-in-out"), kf(900, 0, 8, 0, "ease-in-out"), kf(1200, 0, 0, 0, "ease-in-out")).
		k("eyes", kfs(0, 1, ""), kfs(500, 0.4, "ease-in-out"), kfs(1000, 0.7, "ease-out"), kfs(1200, 1, "ease-in-out"))))

	return &petpack.Rig{
		Version: 1,
		Bones: []petpack.RigBone{
			{Name: "root", X: 128, Y: 150},
			{Name: "hip", Parent: "root", Y: 34},
			{Name: "torso", Parent: "hip", Y: -34},
			{Name: "head", Parent: "torso", Y: -50},
			{Name: "eyes", Parent: "head", Y: -6},
			{Name: "mouth", Parent: "head", Y: 14},
			{Name: "arm_l", Parent: "torso", X: -28, Y: -22},
			{Name: "arm_r", Parent: "torso", X: 28, Y: -22},
			{Name: "leg_l", Parent: "hip", X: -8, Y: 2},
			{Name: "leg_r", Parent: "hip", X: 8, Y: 2},
			{Name: "tail", Parent: "hip", X: 4, Y: -8, Rotate: -20},
		},
		Slots: []petpack.RigSlot{
			{Name: "tail", Bone: "tail", Texture: "rig/tail.png", Z: 0},
			{Name: "leg_l", Bone: "leg_l", Texture: "rig/leg-l.png", Z: 1},
			{Name: "leg_r", Bone: "leg_r", Texture: "rig/leg-r.png", Z: 1},
			{Name: "torso", Bone: "torso", Texture: "rig/torso.png", Z: 2},
			{Name: "arm_l", Bone: "arm_l", Texture: "rig/arm-l.png", Z: 3},
			{Name: "arm_r", Bone: "arm_r", Texture: "rig/arm-r.png", Z: 3},
			{Name: "head", Bone: "head", Texture: "rig/head.png", Z: 4},
			{Name: "mouth", Bone: "mouth", Texture: "rig/mouth.png", Z: 5},
			{Name: "eyes", Bone: "eyes", Texture: "rig/eyes.png", Z: 6},
		},
		Clips: clips,
	}
}

func singIn() petpack.RigClip {
	return clip(400, false, 0, "", false, tr().
		k("root", kf(0, 0, 2, 0, ""), kf(400, 0, -8, 0, "ease-out")).
		k("arm_l", kf(0, 0, 0, 0, ""), kf(400, 0, 0, 120, "ease-out")).
		k("arm_r", kf(0, 0, 0, 0, ""), kf(400, 0, 0, -110, "ease-out")).
		k("mouth", kfs(0, 0.7, ""), kfs(400, 1.35, "ease-out")).
		k("torso", kf(0, 0, 0, 0, ""), kf(400, 0, 0, 8, "ease-in-out")))
}

func singLoop(cue string) petpack.RigClip {
	return clip(1280, true, 0, cue, true, tr().
		k("root", kf(0, 0, -8, 0, ""), kf(320, 0, -18, 0, "ease-in-out"), kf(640, 0, -6, 0, "ease-in-out"), kf(960, 0, -16, 0, "ease-in-out"), kf(1280, 0, -8, 0, "ease-in-out")).
		k("torso", kf(0, 0, 0, 8, ""), kf(320, 0, 0, -14, "ease-in-out"), kf(640, 0, 0, 16, "ease-in-out"), kf(960, 0, 0, -10, "ease-in-out"), kf(1280, 0, 0, 8, "ease-in-out")).
		k("head", kf(0, 0, 0, -4, ""), kf(320, 0, 0, 10, "ease-in-out"), kf(640, 0, 0, -8, "ease-in-out"), kf(1280, 0, 0, -4, "ease-in-out")).
		k("arm_l", kf(0, 0, 0, 120, ""), kf(400, 0, -6, 156, "ease-in-out"), kf(800, 0, 0, 100, "ease-in-out"), kf(1280, 0, 0, 120, "ease-in-out")).
		k("arm_r", kf(0, 0, 0, -110, ""), kf(480, 0, -4, -150, "ease-in-out"), kf(900, 0, 0, -90, "ease-in-out"), kf(1280, 0, 0, -110, "ease-in-out")).
		k("mouth", kfs(0, 1.2, ""), kfs(180, 1.7, "ease-out"), kfs(360, 0.65, "ease-in-out"), kfs(560, 1.55, "ease-out"), kfs(860, 0.7, "ease-in-out"), kfs(1280, 1.2, "ease-in-out")))
}

func singOut() petpack.RigClip {
	return clip(420, false, 0, "", false, tr().
		k("root", kf(0, 0, -8, 0, ""), kf(420, 0, 0, 0, "ease-in-out")).
		k("arm_l", kf(0, 0, 0, 100, ""), kf(420, 0, 0, 0, "ease-in-out")).
		k("arm_r", kf(0, 0, 0, -90, ""), kf(420, 0, 0, 0, "ease-in-out")).
		k("mouth", kfs(0, 1.2, ""), kfs(420, 0.85, "ease-in-out")).
		k("torso", kf(0, 0, 0, 6, ""), kf(420, 0, 0, 0, "ease-in-out")).
		k("head", kf(0, 0, 0, 4, ""), kf(420, 0, 0, 0, "ease-in-out")))
}

// march is a front-view step cycle: legs lift, arms swing, the window travels.
func march(ms int, lift, arm, bob float64) tracks {
	half := ms / 2
	quarter := ms / 4
	return tr().
		k("root", kf(0, 0, 0, 0, ""), kf(quarter, 0, -bob, 0, "ease-in-out"), kf(half, 0, 0, 0, "ease-in-out"), kf(half+quarter, 0, -bob*0.8, 0, "ease-in-out"), kf(ms, 0, 0, 0, "ease-in-out")).
		k("leg_l", kf(0, 0, 0, 0, ""), kf(quarter, 0, -lift, 12, "ease-out"), kf(half, 0, 2, 0, "ease-in-out"), kf(ms, 0, 0, 0, "ease-in-out")).
		k("leg_r", kf(0, 0, 2, 0, ""), kf(half, 0, 0, 0, "ease-in-out"), kf(half+quarter, 0, -lift, -12, "ease-out"), kf(ms, 0, 2, 0, "ease-in-out")).
		k("arm_l", kf(0, 0, 0, arm*0.4, ""), kf(half, 0, 0, -arm, "ease-in-out"), kf(ms, 0, 0, arm*0.4, "ease-in-out")).
		k("arm_r", kf(0, 0, 0, -arm*0.4, ""), kf(half, 0, 0, arm, "ease-in-out"), kf(ms, 0, 0, -arm*0.4, "ease-in-out")).
		k("torso", kf(0, 0, 0, -4, ""), kf(half, 0, 0, 5, "ease-in-out"), kf(ms, 0, 0, -4, "ease-in-out"))
}

func tailSway(ms int, amp float64) tracks {
	return tr().k("tail", kf(0, 0, 0, 0, ""), kf(ms/3, 0, 0, amp, "ease-in-out"), kf(ms*2/3, 0, 0, -amp*0.7, "ease-in-out"), kf(ms, 0, 0, 0, "ease-in-out"))
}

func buildPerformer() *petpack.Performer {
	b := func(enter, loop, exit string, weight, minMS, maxMS, cool int, moods ...string) petpack.PerformerBehavior {
		return petpack.PerformerBehavior{Enter: enter, Loop: loop, Exit: exit, Weight: weight, MinMS: minMS, MaxMS: maxMS, CooldownMS: cool, Moods: moods}
	}
	ev := func(play, interrupt, mood string, cool int) petpack.PerformerEvent {
		return petpack.PerformerEvent{Play: play, Interrupt: interrupt, Mood: mood, CooldownMS: cool}
	}
	rc := func(event, play, interrupt, mood string, cool, weight int) petpack.PerformerReaction {
		return petpack.PerformerReaction{Event: event, Play: play, Interrupt: interrupt, Mood: mood, CooldownMS: cool, Weight: weight}
	}
	return &petpack.Performer{
		Version: 1,
		Moods:   []string{"calm", "curious", "focused", "pleased", "concerned", "tired"},
		Layers:  []string{"body", "expression", "gaze", "secondary"},
		Behaviors: map[string]petpack.PerformerBehavior{
			"idle_breathe":    b("idle_breathe_in", "idle_breathe_loop", "idle_breathe_out", 5, 3200, 6200, 4000, "calm"),
			"idle_look_left":  b("idle_look_left_in", "idle_look_left_loop", "idle_look_left_out", 3, 2400, 4200, 9000, "calm", "curious"),
			"idle_look_right": b("idle_look_right_in", "idle_look_right_loop", "idle_look_right_out", 3, 2400, 4200, 9000, "calm", "curious"),
			"idle_blink":      b("idle_blink_in", "idle_blink_loop", "idle_blink_out", 4, 900, 1800, 5000, "calm", "focused"),
			"idle_walk":       b("walk_in", "walk_loop", "walk_out", 3, 2200, 4800, 8000, "calm", "curious"),
			"idle_run":        b("run_in", "run_loop", "run_out", 1, 1600, 2800, 22000, "pleased", "calm"),
			"idle_jump":       b("jump_in", "jump_loop", "jump_out", 1, 1400, 2400, 18000, "pleased", "calm"),
			"idle_bend":       b("bend_in", "bend_loop", "bend_out", 1, 1800, 3200, 16000, "curious", "calm"),
			"idle_stretch":    b("stretch_in", "stretch_loop", "stretch_out", 2, 2000, 3600, 14000, "tired", "calm"),
			"idle_sing":       b("sing_in", "sing_loop", "sing_out", 2, 2200, 4600, 12000, "pleased", "calm"),
			"idle_rest":       b("rest_in", "rest_loop", "rest_out", 1, 3000, 5600, 20000, "tired", "calm"),
		},
		States: map[string]petpack.PerformerState{
			"idle": {
				BehaviorPool: []string{"idle_breathe", "idle_look_left", "idle_look_right", "idle_blink", "idle_walk", "idle_run", "idle_jump", "idle_bend", "idle_stretch", "idle_sing", "idle_rest"},
				Mood:         "calm", Expression: "expr_calm", Gaze: "gaze_wander", Secondary: "secondary_idle",
			},
			"listening": {Enter: "listen_in", Loop: "listen_loop", Exit: "listen_out", Mood: "curious", Expression: "expr_curious", Gaze: "gaze_left", Secondary: "secondary_listen"},
			"thinking":  {Enter: "think_in", Loop: "think_loop", Exit: "think_out", Mood: "focused", Expression: "expr_focus", Gaze: "gaze_up", Secondary: "secondary_think"},
			"speaking":  {Enter: "speak_in", Loop: "speak_loop", Exit: "speak_out", Mood: "pleased", Expression: "expr_speak", Gaze: "gaze_center", Secondary: "secondary_speak"},
			"done":      {Enter: "done_in", Loop: "done_loop", Exit: "done_out", Mood: "pleased", Expression: "expr_pleased", Gaze: "gaze_center", Secondary: "secondary_done"},
			"alert":     {Enter: "alert_in", Loop: "alert_loop", Exit: "alert_out", Mood: "concerned", Expression: "expr_concerned", Gaze: "gaze_right", Secondary: "secondary_alert"},
			"quiet":     {Enter: "quiet_in", Loop: "quiet_loop", Exit: "quiet_out", Mood: "tired", Expression: "expr_tired", Gaze: "gaze_center", Secondary: "secondary_quiet"},
		},
		Events: map[string]petpack.PerformerEvent{
			"click":        ev("react_click_greet", "soft", "pleased", 2800),
			"hover":        ev("react_hover_peek", "soft", "curious", 4000),
			"drag_start":   ev("react_drag_brace", "soft", "concerned", 800),
			"drag_end":     ev("react_drag_release", "soft", "calm", 800),
			"task_started": ev("react_task_focus", "after_loop", "focused", 4000),
			"task_done":    ev("react_task_celebrate", "after_loop", "pleased", 6000),
			"task_failed":  ev("react_task_concern", "after_loop", "concerned", 6000),
			"long_idle":    ev("react_long_idle_rest", "after_loop", "tired", 20000),
		},
		Reactions: map[string]petpack.PerformerReaction{
			"click_greet":    rc("click", "react_click_greet", "soft", "pleased", 2800, 4),
			"click_notice":   rc("click", "react_click_notice", "soft", "curious", 2800, 2),
			"hover_peek":     rc("hover", "react_hover_peek", "soft", "curious", 4000, 3),
			"hover_blink":    rc("hover", "react_hover_blink", "soft", "calm", 4500, 1),
			"drag_brace":     rc("drag_start", "react_drag_brace", "soft", "concerned", 800, 3),
			"drag_release":   rc("drag_end", "react_drag_release", "soft", "calm", 800, 3),
			"task_focus":     rc("task_started", "react_task_focus", "after_loop", "focused", 4000, 3),
			"task_listen":    rc("task_started", "react_task_listen", "after_loop", "curious", 4500, 1),
			"task_celebrate": rc("task_done", "react_task_celebrate", "after_loop", "pleased", 6000, 3),
			"task_settle":    rc("task_done", "react_task_settle", "after_loop", "calm", 7000, 1),
			"task_concern":   rc("task_failed", "react_task_concern", "after_loop", "concerned", 6000, 3),
			"long_idle_rest": rc("long_idle", "react_long_idle_rest", "after_loop", "tired", 20000, 3),
		},
		Rules: petpack.PerformerRules{NoRepeatLast: 3, CrossfadeMS: 160, MaxInterruptMS: 800},
	}
}

type tracks map[string][]petpack.RigKeyframe

func tr() tracks { return tracks{} }

func (t tracks) k(bone string, frames ...petpack.RigKeyframe) tracks {
	t[bone] = frames
	return t
}

func clip(ms int, loop bool, travel float64, cue string, soundLoop bool, t tracks) petpack.RigClip {
	return petpack.RigClip{DurationMS: ms, Loop: loop, TravelX: travel, Cue: cue, SoundLoop: soundLoop, Tracks: t}
}

func kf(at int, x, y, rot float64, ease string) petpack.RigKeyframe {
	return petpack.RigKeyframe{AtMS: at, X: x, Y: y, Rotate: rot, Ease: ease}
}

func kfs(at int, scaleY float64, ease string) petpack.RigKeyframe {
	return petpack.RigKeyframe{AtMS: at, ScaleY: scaleY, Ease: ease}
}

func sc(at int, sx, sy float64, ease string) petpack.RigKeyframe {
	return petpack.RigKeyframe{AtMS: at, ScaleX: sx, ScaleY: sy, Ease: ease}
}

func drawTextures(dir string) []string {
	parts := []struct {
		name string
		img  *image.NRGBA
	}{
		{"tail.png", drawTail()},
		{"leg-l.png", drawLeg(false)},
		{"leg-r.png", drawLeg(true)},
		{"torso.png", drawTorso()},
		{"arm-l.png", drawArm(false)},
		{"arm-r.png", drawArm(true)},
		{"head.png", drawHead()},
		{"mouth.png", drawMouth()},
		{"eyes.png", drawEyes()},
	}
	rels := make([]string, 0, len(parts))
	for _, part := range parts {
		rel := "rig/" + part.name
		if err := writePNG(filepath.Join(dir, part.name), part.img); err != nil {
			panic(err)
		}
		rels = append(rels, rel)
	}
	return rels
}

func drawHead() *image.NRGBA {
	// Face center is the bitmap center, so the head turns around the face.
	// Hair and ears extend above; side locks fall past the jaw.
	const w, h = 96, 112
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	cx := float64(w) / 2
	cy := float64(h) / 2
	fillEllipse(img, cx, cy+6, 34, 42, hair)
	fillCapsule(img, cx-18, cy-6, cx-24, cy+46, 9, hair)
	fillCapsule(img, cx+18, cy-4, cx+26, cy+48, 9, hair)
	fillEllipse(img, cx-26, cy-30, 11, 18, hair)
	fillEllipse(img, cx+26, cy-30, 11, 18, hair)
	fillEllipse(img, cx-26, cy-26, 5, 10, earPink)
	fillEllipse(img, cx+26, cy-26, 5, 10, earPink)
	fillEllipse(img, cx, cy-8, 30, 24, hair)
	fillEllipse(img, cx, cy+2, 20, 24, skin)
	fillEllipse(img, cx-16, cy-10, 10, 8, hair)
	fillEllipse(img, cx+14, cy-12, 11, 8, hair)
	fillEllipse(img, cx, cy-16, 16, 6, hair)
	fillEllipse(img, cx-8, cy-22, 7, 3, hairLite)
	fillEllipse(img, cx-10, cy-6, 6, 1.3, brow)
	fillEllipse(img, cx+10, cy-6, 6, 1.3, brow)
	fillEllipse(img, cx, cy+10, 2.2, 2.6, nose)
	fillEllipse(img, cx-12, cy+12, 5, 2.4, blush)
	fillEllipse(img, cx+12, cy+12, 5, 2.4, blush)
	fillEllipse(img, cx+30, cy-24, 7, 4, bow)
	fillEllipse(img, cx+36, cy-20, 5, 3, bow)
	fillEllipse(img, cx+32, cy-22, 2.2, 2.2, bowDeep)
	return img
}

func drawEyes() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 56, 20))
	for _, x := range []float64{15, 41} {
		fillEllipse(img, x, 11, 9, 5.5, white)
		fillEllipse(img, x, 8.5, 9, 2.2, lash)
		fillEllipse(img, x, 11.5, 4.2, 4.6, iris)
		fillEllipse(img, x, 12, 2, 2.4, pupil)
		fillEllipse(img, x-1.6, 9.6, 1.4, 1.4, white)
	}
	return img
}

func drawMouth() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 28, 16))
	fillEllipse(img, 14, 7, 6.5, 2.1, lip)
	fillEllipse(img, 14, 9.2, 5.2, 2.2, lipDeep)
	return img
}

func drawTorso() *image.NRGBA {
	// Waist is the bitmap center. Neck and shoulders sit above it, the skirt below.
	const w, h = 100, 124
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	cx := float64(w) / 2
	waist := float64(h) / 2
	fillCapsule(img, cx, waist-42, cx, waist-26, 6.5, skin)
	fillEllipse(img, cx, waist-22, 26, 8, skin)
	fillEllipse(img, cx, waist-10, 16, 14, dress)
	fillEllipse(img, cx, waist-14, 10, 6, skinShade)
	fillEllipse(img, cx, waist+2, 12, 6, dressDeep)
	fillCapsule(img, cx-10, waist+6, cx-24, waist+44, 7, dress)
	fillCapsule(img, cx+10, waist+6, cx+24, waist+44, 7, dress)
	fillEllipse(img, cx, waist+26, 22, 16, dress)
	fillEllipse(img, cx, waist+40, 26, 7, dressLite)
	fillEllipse(img, cx, waist+44, 24, 4, dressDeep)
	fillEllipse(img, cx, waist-20, 8, 2.4, white)
	fillEllipse(img, cx, waist+4, 12, 2.2, ribbon)
	fillEllipse(img, cx-5, waist+4, 3.2, 2.4, ribbon)
	fillEllipse(img, cx+5, waist+4, 3.2, 2.4, ribbon)
	return img
}

func drawArm(right bool) *image.NRGBA {
	// Shoulder joint is the bitmap center. The limb hangs in the lower half.
	const w, h = 28, 168
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	cx := float64(w) / 2
	joint := float64(h) / 2
	fillEllipse(img, cx, joint, 7, 6, skin)
	fillCapsule(img, cx, joint+2, cx, joint+34, 4.6, skin)
	fillEllipse(img, cx, joint+34, 4, 3.6, skinShade)
	fillCapsule(img, cx, joint+34, cx+1, joint+64, 3.8, skin)
	fillEllipse(img, cx+1, joint+70, 5, 6, skin)
	fillEllipse(img, cx-4, joint+66, 2.2, 3, skinShade)
	if right {
		return mirrorNRGBA(img)
	}
	return img
}

func drawLeg(right bool) *image.NRGBA {
	const w, h = 28, 136
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	cx := float64(w) / 2
	joint := float64(h) / 2
	fillCapsule(img, cx, joint+4, cx, joint+28, 6, skin)
	fillEllipse(img, cx, joint+28, 5, 4.5, skinShade)
	fillCapsule(img, cx, joint+28, cx, joint+50, 4.4, skin)
	fillEllipse(img, cx+1, joint+54, 8, 3.6, shoe)
	fillEllipse(img, cx+3, joint+52, 3, 2.2, shoeLite)
	if right {
		return mirrorNRGBA(img)
	}
	return img
}

func drawTail() *image.NRGBA {
	const w, h = 128, 48
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	cx, cy := float64(w)/2, float64(h)/2
	fillCapsule(img, cx, cy, cx+18, cy+4, 5, hair)
	fillEllipse(img, cx+28, cy+2, 10, 8, hair)
	fillEllipse(img, cx+42, cy-2, 11, 9, hair)
	fillEllipse(img, cx+52, cy-6, 8, 8, hairLite)
	return img
}

var (
	skin      = color.NRGBA{255, 224, 206, 255}
	skinShade = color.NRGBA{236, 196, 176, 255}
	hair      = color.NRGBA{92, 58, 46, 255}
	hairLite  = color.NRGBA{148, 104, 78, 255}
	earPink   = color.NRGBA{236, 170, 176, 255}
	nose      = color.NRGBA{214, 140, 140, 255}
	brow      = color.NRGBA{72, 48, 40, 255}
	blush     = color.NRGBA{240, 150, 150, 90}
	bow       = color.NRGBA{214, 84, 122, 255}
	bowDeep   = color.NRGBA{168, 52, 88, 255}
	dress     = color.NRGBA{214, 96, 132, 255}
	dressLite = color.NRGBA{236, 150, 176, 255}
	dressDeep = color.NRGBA{168, 64, 102, 255}
	ribbon    = color.NRGBA{120, 48, 72, 255}
	white     = color.NRGBA{255, 250, 246, 255}
	iris      = color.NRGBA{62, 128, 196, 255}
	pupil     = color.NRGBA{24, 36, 58, 255}
	lash      = color.NRGBA{48, 32, 28, 255}
	lip       = color.NRGBA{196, 96, 112, 255}
	lipDeep   = color.NRGBA{160, 64, 84, 255}
	shoe      = color.NRGBA{62, 44, 40, 255}
	shoeLite  = color.NRGBA{92, 68, 60, 255}
)

func fillCapsule(img *image.NRGBA, x1, y1, x2, y2, r float64, c color.NRGBA) {
	if r < 0.5 || c.A == 0 {
		return
	}
	minX := int(math.Min(x1, x2) - r - 1)
	maxX := int(math.Max(x1, x2) + r + 1)
	minY := int(math.Min(y1, y2) - r - 1)
	maxY := int(math.Max(y1, y2) + r + 1)
	b := img.Bounds()
	if minX < b.Min.X {
		minX = b.Min.X
	}
	if minY < b.Min.Y {
		minY = b.Min.Y
	}
	if maxX > b.Max.X-1 {
		maxX = b.Max.X - 1
	}
	if maxY > b.Max.Y-1 {
		maxY = b.Max.Y - 1
	}
	dx, dy := x2-x1, y2-y1
	l2 := dx*dx + dy*dy
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			px := float64(x) + 0.5
			py := float64(y) + 0.5
			t := 0.0
			if l2 > 0 {
				t = ((px-x1)*dx + (py-y1)*dy) / l2
				if t < 0 {
					t = 0
				} else if t > 1 {
					t = 1
				}
			}
			qx, qy := x1+t*dx, y1+t*dy
			dist := math.Hypot(px-qx, py-qy)
			if dist >= r+0.8 {
				continue
			}
			cover := 1.0
			if dist > r-0.8 {
				cover = (r + 0.8 - dist) / 1.6
			}
			src := c
			src.A = uint8(float64(c.A) * cover)
			blend(img, x, y, src)
		}
	}
}

func fillEllipse(img *image.NRGBA, cx, cy, rx, ry float64, c color.NRGBA) {
	if rx < 0.5 || ry < 0.5 || c.A == 0 {
		return
	}
	minX := int(cx - rx - 1)
	maxX := int(cx + rx + 1)
	minY := int(cy - ry - 1)
	maxY := int(cy + ry + 1)
	b := img.Bounds()
	if minX < b.Min.X {
		minX = b.Min.X
	}
	if minY < b.Min.Y {
		minY = b.Min.Y
	}
	if maxX > b.Max.X-1 {
		maxX = b.Max.X - 1
	}
	if maxY > b.Max.Y-1 {
		maxY = b.Max.Y - 1
	}
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			dx := (float64(x) + 0.5 - cx) / rx
			dy := (float64(y) + 0.5 - cy) / ry
			d := dx*dx + dy*dy
			if d >= 1.08 {
				continue
			}
			cover := 1.0
			if d > 0.86 {
				cover = (1.08 - d) / 0.22
			}
			src := c
			src.A = uint8(float64(c.A) * cover)
			blend(img, x, y, src)
		}
	}
}

func blend(img *image.NRGBA, x, y int, src color.NRGBA) {
	if src.A == 0 {
		return
	}
	i := img.PixOffset(x, y)
	dstA := uint32(img.Pix[i+3])
	srcA := uint32(src.A)
	outA := srcA + dstA*(255-srcA)/255
	if outA == 0 {
		return
	}
	img.Pix[i] = uint8((uint32(src.R)*srcA + uint32(img.Pix[i])*dstA*(255-srcA)/255) / outA)
	img.Pix[i+1] = uint8((uint32(src.G)*srcA + uint32(img.Pix[i+1])*dstA*(255-srcA)/255) / outA)
	img.Pix[i+2] = uint8((uint32(src.B)*srcA + uint32(img.Pix[i+2])*dstA*(255-srcA)/255) / outA)
	img.Pix[i+3] = uint8(outA)
}

func mirrorNRGBA(src *image.NRGBA) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(b)
	w := b.Dx()
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < w; x++ {
			si := src.PixOffset(b.Min.X+x, b.Min.Y+y)
			di := dst.PixOffset(b.Min.X+w-1-x, b.Min.Y+y)
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst
}

func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func opaqueTop(img *image.NRGBA) int {
	if img == nil {
		return 1 << 30
	}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.Pix[img.PixOffset(x, y)+3] > 24 {
				return y
			}
		}
	}
	return b.Max.Y
}

func centroidX(img *image.NRGBA) float64 {
	if img == nil {
		return 0
	}
	b := img.Bounds()
	var sum float64
	var n int
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.Pix[img.PixOffset(x, y)+3] > 24 {
				sum += float64(x)
				n++
			}
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}
