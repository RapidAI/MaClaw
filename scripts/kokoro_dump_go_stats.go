//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/corelib/tts/kokoro"
)

type metaFile struct { Voice string `json:"voice"`; Phonemes string `json:"phonemes"` }
type stat struct { Shape []int `json:"shape"`; Min, Max, Mean, RMS float64 `json:"min","max","mean","rms"` }

func statOf(x []float32, shape ...int) map[string]any {
	min, max := float64(x[0]), float64(x[0]); sum, ss := 0.0, 0.0
	for _, fv := range x { v := float64(fv); if v < min { min = v }; if v > max { max = v }; sum += v; ss += v*v }
	return map[string]any{"shape": shape, "min": min, "max": max, "mean": sum/float64(len(x)), "rms": math.Sqrt(ss/float64(len(x)))}
}

func main() {
	root := filepath.Clean(`D:\workprj\aicoder\tts_eval\kokoro_go_assets`)
	data, err := os.ReadFile(filepath.Join(root,"quality_zh_short_meta.json")); if err != nil { log.Fatal(err) }
	var meta metaFile; if err := json.Unmarshal(data,&meta); err != nil { log.Fatal(err) }
	model, err := kokoro.LoadModel(kokoro.Assets{ConfigPath: filepath.Join(root,"config.json"), WeightsPath: filepath.Join(root,"kokoro-v1_0.koro")}); if err != nil { log.Fatal(err) }
	voice, err := model.LoadVoice(filepath.Join(root,"voices"), meta.Voice); if err != nil { log.Fatal(err) }
	ids, err := kokoro.TokenizePhonemes(model.Config, meta.Phonemes); if err != nil { log.Fatal(err) }
	bert, bertDim, err := model.AlbertForward(ids); if err != nil { log.Fatal(err) }
	dur, err := model.PredictDurations(meta.Phonemes, voice, 1); if err != nil { log.Fatal(err) }
	cond, err := model.BuildConditioning(meta.Phonemes, voice, 1); if err != nil { log.Fatal(err) }
	f0n, err := model.PredictF0N(cond, voice); if err != nil { log.Fatal(err) }
	feat, err := model.DecoderPreGenerator(cond, f0n, voice); if err != nil { log.Fatal(err) }
	out := map[string]any{
		"input_ids": ids,
		"pred_dur": dur.Durations,
		"bert_dur": statOf(bert, 1, len(ids), bertDim),
		"duration_encoder_d": statOf(dur.Encoded, 1, len(ids), dur.Dim),
		"F0_pred": statOf(f0n.F0, 1, len(f0n.F0)),
		"N_pred": statOf(f0n.Noise, 1, len(f0n.Noise)),
		"asr": statOf(cond.Text, 1, cond.Frames, 512),
		"decoder_pre_generator": statOf(feat.X, 1, 512, feat.Frames),
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	if err := os.WriteFile(filepath.Join(root,"quality_go_stats.json"), b, 0644); err != nil { log.Fatal(err) }
	fmt.Println(string(b))
}
