// Command dshconvert turns one local DeepSeek Harness sprite folder into a
// MaClaw user pet pack. It does not install the pack and it does not open a window.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/RapidAI/CodeClaw/guiapp/petpack"
)

func main() {
	in := flag.String("in", "", "宠物目录，里面要有 pet.json 和 spritesheet")
	out := flag.String("out", "", "输出父目录，包会写到 <父目录>/<packID>/")
	zipPath := flag.String("zip", "", "输出 zip 路径")
	flag.Parse()
	if *in == "" || (*out == "" && *zipPath == "") {
		fmt.Fprintln(os.Stderr, "用法：dshconvert -in <宠物目录> [-out <父目录>] [-zip <文件.zip>]")
		os.Exit(1)
	}
	report, err := petpack.ConvertDSHPet(petpack.DSHConvertRequest{
		InputDir:  *in,
		OutputDir: *out,
		ZipPath:   *zipPath,
	})
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if encErr := enc.Encode(report); encErr != nil {
		fmt.Fprintln(os.Stderr, encErr.Error())
		os.Exit(1)
	}
	if err == nil {
		fmt.Fprintf(os.Stderr, "已转换 %s，留下 %d 帧，丢掉 %d 帧。\n", report.PackID, report.KeptFrames, len(report.DroppedFrames))
		os.Exit(0)
	}
	if report.RejectedReason != "" {
		fmt.Fprintln(os.Stderr, report.RejectedReason)
		os.Exit(2)
	}
	if report.ValidatorError != "" {
		fmt.Fprintln(os.Stderr, report.ValidatorError)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, err.Error())
	os.Exit(1)
}
