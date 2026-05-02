//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/corelib/tts/kokoro"
)

type metaFile struct { Voice string `json:"voice"`; Phonemes string `json:"phonemes"` }

func main() {
	root := filepath.Clean(`D:\workprj\aicoder\tts_eval\kokoro_go_assets`)
	data, err := os.ReadFile(filepath.Join(root, "quality_zh_short_meta.json")); if err != nil { log.Fatal(err) }
	var meta metaFile
	if err := json.Unmarshal(data, &meta); err != nil { log.Fatal(err) }
	model, err := kokoro.LoadModel(kokoro.Assets{ConfigPath: filepath.Join(root,"config.json"), WeightsPath: filepath.Join(root,"kokoro-v1_0.koro")}); if err != nil { log.Fatal(err) }
	voice, err := model.LoadVoice(filepath.Join(root,"voices"), meta.Voice); if err != nil { log.Fatal(err) }
	res, err := model.PredictDurations(meta.Phonemes, voice, 1); if err != nil { log.Fatal(err) }
	sum := 0; for _, d := range res.Durations { sum += d }
	out := map[string]any{"phonemes": meta.Phonemes, "input_ids": res.InputIDs, "pred_dur": res.Durations, "sum_dur": sum}
	b, _ := json.MarshalIndent(out, "", "  ")
	if err := os.WriteFile(filepath.Join(root,"quality_go_duration.json"), b, 0644); err != nil { log.Fatal(err) }
	fmt.Println(string(b))
}
