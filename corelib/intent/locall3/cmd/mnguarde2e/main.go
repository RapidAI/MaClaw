// Command mnguarde2e runs the real Go-Q8 Qwen3 embedder + bundled heads through
// locall3.ContextFunc for sample MN / zh / en turns, with and without the MN
// guard head, and checks that zh/en outputs are identical and that Cyrillic
// input is either accepted by the guard (non-sensitive, p ≥ τ) or sent remote.
//
//	go run ./corelib/intent/locall3/cmd/mnguarde2e -models DIR [-main-head FILE]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/intent/locall3"
)

func main() {
	models := flag.String("models", "", "models dir")
	mainHead := flag.String("main-head", "", "shipped intent head file (default <heads>/"+intent.ShippedLocalHeadName+")")
	flag.Parse()
	bundled := locall3.DefaultHeadDir(*models)
	if *mainHead == "" {
		*mainHead = filepath.Join(bundled, intent.ShippedLocalHeadName)
	}
	noMN, _ := os.MkdirTemp("", "nomn")
	heads, _ := os.MkdirTemp("", "withmn")
	defer os.RemoveAll(noMN)
	defer os.RemoveAll(heads)
	for _, d := range []string{noMN, heads} {
		os.Symlink(*mainHead, filepath.Join(d, intent.ShippedLocalHeadName))
	}
	os.Symlink(filepath.Join(bundled, intent.MNGuardHeadName), filepath.Join(heads, intent.MNGuardHeadName))
	load, err := locall3.LoadEmbedder(filepath.Join(*models, embedding.DefaultModelFilename), false)
	if err != nil {
		fmt.Println("load:", err)
		os.Exit(1)
	}
	emb := load.Embedder
	defer emb.Close()
	turns := []string{
		"Маргааш Улаанбаатарын цаг агаар ямар байх вэ?",
		"Одоо хэдэн цаг болж байна?",
		"Энэ баримт бичгийг уншаад товчлоод өгөөч",
		"Ширээний компьютер дээрх түр файлуудыг устга",
		"Серверт ssh-ээр холбогдоод дискний зай шалга",
		"Маргааш 10 цагт уулзалт товлоод сануулаарай",
		"Сайн байна уу?",
		"Хамгийн сүүлийн Go хувилбарын мэдээг интернэтээс хайгаарай",
		"帮我查一下明天北京的天气", "删除桌面上所有的临时文件", "幫我打開這個文件",
		"Search the web for the latest Go release notes", "Schedule a meeting with Alice tomorrow at 10am", "hello there",
	}
	run := func(dir string) []string {
		rt := locall3.NewWithMode(dir, intent.LocalL3On)
		rt.Logf = func(string, ...any) {}
		h, err := rt.Attach(emb)
		if err != nil {
			fmt.Println("attach:", err)
			os.Exit(1)
		}
		fmt.Printf("[l3] dir=%s head=%s\n", dir, h.ID)
		var out []string
		for _, t := range turns {
			remote := false
			fn := rt.ContextFunc(func(context.Context, context.Context, string, string) (string, error) {
				remote = true
				return `{"top":[{"skill":"REMOTE","score":1}]}`, nil
			})
			o, err := fn(context.Background(), context.Background(), "", t)
			r := fmt.Sprintf("%s err=%v remote=%v", o, err, remote)
			out = append(out, r)
		}
		return out
	}
	base, mn := run(noMN), run(heads)
	bad := 0
	for i, t := range turns {
		fmt.Printf("%-60q\n   without MN head: %s\n   with MN head:    %s\n", t, base[i], mn[i])
		if !intent.BypassLocalHeads(t) && base[i] != mn[i] {
			fmt.Println("   !! zh/en output changed")
			bad++
		}
	}
	fmt.Printf("zh/en changed: %d\n", bad)
	if bad > 0 {
		os.Exit(2)
	}
}
