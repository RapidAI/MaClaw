//go:build ignore

package main
import (
 "encoding/binary"; "fmt"; "log"; "math"; "os"; "path/filepath"
 "github.com/RapidAI/CodeClaw/corelib/tts/kokoro"
)
func main(){
 root:=filepath.Clean(`D:\workprj\aicoder\tts_eval\kokoro_go_assets`)
 data,err:=os.ReadFile(filepath.Join(root,"quality_python_conv_post_f32.bin")); if err!=nil{log.Fatal(err)}
 vals:=make([]float32,len(data)/4)
 for i:=range vals{ vals[i]=math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:])) }
 pcm:=kokoro.PCMFromConvPost(vals,12241)
 out:=filepath.Join(root,"quality_go_istft_python_post.wav")
 kokoro.WriteWAV(out,pcm,kokoro.DefaultSampleRate)
 fmt.Println(out,len(pcm),float64(len(pcm))/24000)
}
