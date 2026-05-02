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
func main(){
 root:=filepath.Clean(`D:\workprj\aicoder\tts_eval\kokoro_go_assets`)
 data,err:=os.ReadFile(filepath.Join(root,"quality_zh_short_meta.json")); if err!=nil{log.Fatal(err)}
 var meta metaFile; if err:=json.Unmarshal(data,&meta); err!=nil{log.Fatal(err)}
 model,err:=kokoro.LoadModel(kokoro.Assets{ConfigPath:filepath.Join(root,"config.json"),WeightsPath:filepath.Join(root,"kokoro-v1_0.koro")}); if err!=nil{log.Fatal(err)}
 voice,err:=model.LoadVoice(filepath.Join(root,"voices"),meta.Voice); if err!=nil{log.Fatal(err)}
 cond,err:=model.BuildConditioning(meta.Phonemes,voice,1); if err!=nil{log.Fatal(err)}
 f0n,err:=model.PredictF0N(cond,voice); if err!=nil{log.Fatal(err)}
 feat,err:=model.DecoderPreGenerator(cond,f0n,voice); if err!=nil{log.Fatal(err)}
 stats,err:=model.GeneratorDebugStats(feat,f0n,voice); if err!=nil{log.Fatal(err)}
 b,_:=json.MarshalIndent(stats,"","  ")
 os.WriteFile(filepath.Join(root,"quality_go_generator_stats.json"),b,0644)
 fmt.Println(string(b))
}
