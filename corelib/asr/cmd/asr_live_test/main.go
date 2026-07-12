// asr_live — 简单的 ASR 在线测试工具
//
// 使用方式:
//   go run . <wav_file>          — 转写指定的 WAV 文件
//   go run . --record <seconds>  — 录音指定秒数后转写（需要 ffmpeg）
//   go run . --dir <dir>         — 转写目录下所有 WAV 文件
//   go run . --batch             — 转写项目根目录下所有已知测试 WAV
//
// WAV 文件要求: 16kHz mono 16-bit PCM（非此格式会自动重采样）
package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/asr"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	modelPath := findModelPath()
	if modelPath == "" {
		log.Fatal("找不到 ASR 模型文件 moonshine-base-zh.gguf\n" +
			"搜索路径: ~/.maclaw/models/, ./models/, ../../RapidSpeech.cpp/models/gguf/")
	}
	fmt.Printf("模型: %s\n", modelPath)

	// 加载模型
	fmt.Print("加载模型...")
	start := time.Now()
	model, err := asr.NewMoonshine(modelPath)
	if err != nil {
		log.Fatalf("加载模型失败: %v", err)
	}
	defer model.Close()
	fmt.Printf(" 完成 (%.1fs)\n\n", time.Since(start).Seconds())

	switch os.Args[1] {
	case "--record":
		seconds := 5
		if len(os.Args) > 2 {
			if n, err := strconv.Atoi(os.Args[2]); err == nil && n > 0 {
				seconds = n
			}
		}
		recordAndTranscribe(model, seconds)

	case "--dir":
		if len(os.Args) < 3 {
			log.Fatal("用法: go run . --dir <目录路径>")
		}
		transcribeDir(model, os.Args[2])

	default:
		// 当作 WAV 文件路径
		for _, wavPath := range os.Args[1:] {
			transcribeFile(model, wavPath)
		}
	}
}

func printUsage() {
	fmt.Println(`ASR 在线测试工具

用法:
  go run . <wav文件>          转写指定 WAV 文件
  go run . file1.wav file2.wav  批量转写
  go run . --record [秒数]    录音后转写（默认5秒，需要 ffmpeg）
  go run . --dir <目录>       转写目录下所有 .wav 文件`)
}

func findModelPath() string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".maclaw", "models", "moonshine-base-zh.gguf"),
		filepath.Join(".", "models", "moonshine-base-zh.gguf"),
		filepath.Join("..", "..", "..", "..", "RapidSpeech.cpp", "models", "gguf", "moonshine-base-zh.gguf"),
		filepath.Join("..", "..", "..", "..", "RapidSpeech.cpp", "build", "moonshine-base-zh.gguf"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			abs, _ := filepath.Abs(p)
			return abs
		}
	}
	return ""
}

func transcribeFile(model *asr.MoonshineModel, wavPath string) {
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("文件: %s\n", wavPath)

	pcm, err := asr.LoadWAV(wavPath)
	if err != nil {
		fmt.Printf("  错误: %v\n\n", err)
		return
	}

	durationSec := float64(len(pcm)) / 16000.0
	rmsVal := pcmRMS(pcm)
	peakVal := pcmPeak(pcm)
	fmt.Printf("  时长: %.2fs | 采样: %d | RMS: %.5f | Peak: %.4f\n",
		durationSec, len(pcm), rmsVal, peakVal)

	if rmsVal < 0.001 {
		fmt.Printf("   音频能量极低 (RMS=%.5f)，可能是静音\n", rmsVal)
	}

	start := time.Now()
	text, err := model.Transcribe(pcm)
	elapsed := time.Since(start)
	if err != nil {
		fmt.Printf("  转写错误: %v\n\n", err)
		return
	}

	rtf := elapsed.Seconds() / durationSec
	if text == "" {
		fmt.Printf("  结果: (空)\n")
	} else {
		fmt.Printf("  结果: 「%s」\n", text)
	}
	fmt.Printf("  耗时: %dms | RTF: %.2f\n\n", elapsed.Milliseconds(), rtf)
}

func transcribeDir(model *asr.MoonshineModel, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Fatalf("读取目录失败: %v", err)
	}

	count := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".wav") {
			continue
		}
		transcribeFile(model, filepath.Join(dir, e.Name()))
		count++
	}
	if count == 0 {
		fmt.Println("目录下没有找到 .wav 文件")
	} else {
		fmt.Printf("共转写 %d 个文件\n", count)
	}
}

func recordAndTranscribe(model *asr.MoonshineModel, seconds int) {
	// 使用 ffmpeg 从默认音频设备录音
	tmpFile := filepath.Join(os.TempDir(), "maclaw_asr_test.wav")
	defer os.Remove(tmpFile)

	fmt.Printf("准备录音 %d 秒...\n", seconds)
	fmt.Println("   (确保麦克风正常，准备好后按 Enter 开始)")
	fmt.Scanln()

	fmt.Printf("录音中... (%d秒)\n", seconds)

	// 尝试 ffmpeg (Windows dshow)
	cmd := exec.Command("ffmpeg",
		"-f", "dshow",
		"-i", "audio=@device_cm_{33D9A762-90C8-11D0-BD43-00A0C911CE86}\\wave_{00000000-0000-0000-0000-000000000000}",
		"-t", strconv.Itoa(seconds),
		"-ar", "16000",
		"-ac", "1",
		"-y",
		tmpFile,
	)

	// 如果默认设备不行，用列表模式获取设备名
	err := cmd.Run()
	if err != nil {
		// 回退: 用 ffmpeg 自动选择默认
		fmt.Println("  ffmpeg dshow 默认设备失败，尝试用设备列表...")
		cmd = exec.Command("ffmpeg",
			"-f", "dshow",
			"-list_devices", "true",
			"-i", "dummy")
		output, _ := cmd.CombinedOutput()
		fmt.Printf("  可用设备:\n%s\n", string(output))

		// 再试一次用简单方式
		fmt.Println("  尝试 PowerShell 录音方案...")
		err = recordWithPowerShell(tmpFile, seconds)
		if err != nil {
			log.Fatalf("录音失败: %v\n请手动录制 WAV 文件后用: go run . <wav文件>", err)
		}
	}

	fmt.Println("录音结束")
	fmt.Println()

	// 检查录音文件
	fi, err := os.Stat(tmpFile)
	if err != nil || fi.Size() < 100 {
		log.Fatal("录音文件不存在或太小")
	}
	fmt.Printf("录音文件: %s (%d bytes)\n", tmpFile, fi.Size())

	transcribeFile(model, tmpFile)
}

func recordWithPowerShell(outPath string, seconds int) error {
	// 使用 PowerShell 的 .NET 音频录制
	script := fmt.Sprintf(`
Add-Type -AssemblyName System.Speech
Add-Type -TypeDefinition @"
using System;
using System.IO;
using System.Runtime.InteropServices;
public class WavRecorder {
    [DllImport("winmm.dll")]
    public static extern int waveInGetNumDevs();
}
"@

$tempWav = "%s"
$durationMs = %d * 1000

# 使用 SoundRecorder 替代方案: ffmpeg with virtual audio
Write-Host "PowerShell 录音不可用，请使用以下替代方案:"
Write-Host "1. 手动录制 WAV: 使用 Windows 录音机录制，保存为 WAV"
Write-Host "2. 安装 ffmpeg: winget install ffmpeg"
Write-Host "3. 使用 sox: sox -d -r 16000 -c 1 -b 16 output.wav trim 0 $seconds"
exit 1
`, strings.ReplaceAll(outPath, `\`, `\\`), seconds)

	cmd := exec.Command("powershell", "-Command", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func pcmRMS(pcm []float32) float64 {
	if len(pcm) == 0 {
		return 0
	}
	var sum float64
	for _, s := range pcm {
		sum += float64(s) * float64(s)
	}
	return math.Sqrt(sum / float64(len(pcm)))
}

func pcmPeak(pcm []float32) float64 {
	var peak float64
	for _, s := range pcm {
		abs := math.Abs(float64(s))
		if abs > peak {
			peak = abs
		}
	}
	return peak
}
