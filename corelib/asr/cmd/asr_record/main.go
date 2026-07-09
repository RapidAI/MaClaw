// asr_record — 真人录音 + ASR 测试
//
// 模拟用户按下录音键：使用 Windows winmm.dll 录制麦克风音频，
// 录完后直接送入 Moonshine ASR 转写。
//
// 用法:
//   go run .              — 默认录 5 秒（自动选设备）
//   go run . 3            — 录 3 秒
//   go run . 5 output.wav — 录 5 秒并保存 WAV 文件
//   go run . --list       — 列出音频输入设备
//   go run . --dev 1 5    — 使用设备 1 录 5 秒
package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/asr"
)

var (
	winmm                     = syscall.NewLazyDLL("winmm.dll")
	waveInOpen                = winmm.NewProc("waveInOpen")
	waveInPrepareHeader       = winmm.NewProc("waveInPrepareHeader")
	waveInAddBuffer           = winmm.NewProc("waveInAddBuffer")
	waveInStart               = winmm.NewProc("waveInStart")
	waveInStop                = winmm.NewProc("waveInStop")
	waveInReset               = winmm.NewProc("waveInReset")
	waveInClose               = winmm.NewProc("waveInClose")
	waveInUnprepareHeader     = winmm.NewProc("waveInUnprepareHeader")
	waveInGetNumDevs          = winmm.NewProc("waveInGetNumDevs")
	waveInGetDevCapsW         = winmm.NewProc("waveInGetDevCapsW")
)

const (
	WAVE_FORMAT_PCM = 1
	CALLBACK_NULL   = 0
	WAVE_MAPPER     = 0xFFFFFFFF
	WHDR_DONE       = 0x00000001
)

type waveFormatEx struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	CbSize         uint16
}

type waveHdr struct {
	Data          uintptr
	BufferLength  uint32
	BytesRecorded uint32
	User          uintptr
	Flags         uint32
	Loops         uint32
	Next          uintptr
	Reserved      uintptr
}

// WAVEINCAPSW
type waveInCapsW struct {
	ManufacturerID uint16
	ProductID      uint16
	DriverVersion  uint32
	ProductName    [32]uint16
	Formats        uint32
	Channels       uint16
	Reserved       uint16
}

func getDeviceName(caps *waveInCapsW) string {
	name := syscall.UTF16ToString(caps.ProductName[:])
	return name
}

func listDevices() int {
	numDevs, _, _ := waveInGetNumDevs.Call()
	n := int(numDevs)
	fmt.Printf("Audio input devices: %d\n\n", n)
	for i := 0; i < n; i++ {
		var caps waveInCapsW
		ret, _, _ := waveInGetDevCapsW.Call(uintptr(i), uintptr(unsafe.Pointer(&caps)), unsafe.Sizeof(caps))
		if ret == 0 {
			fmt.Printf("  [%d] %s (channels=%d)\n", i, getDeviceName(&caps), caps.Channels)
		} else {
			fmt.Printf("  [%d] (error getting caps: %d)\n", i, ret)
		}
	}
	return n
}

func main() {
	seconds := 5
	savePath := ""
	deviceID := WAVE_MAPPER // default: let Windows choose
	listOnly := false

	// Parse args
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--list":
			listOnly = true
		case "--dev":
			if i+1 < len(args) {
				i++
				if d, err := strconv.Atoi(args[i]); err == nil {
					deviceID = d
				}
			}
		default:
			if n, err := strconv.Atoi(args[i]); err == nil && n > 0 && n < 60 {
				seconds = n
			} else if args[i] != "" {
				savePath = args[i]
			}
		}
	}

	numDevs := listDevices()
	if listOnly || numDevs == 0 {
		if numDevs == 0 {
			fmt.Println("\nERROR: No audio input device found!")
			os.Exit(1)
		}
		return
	}

	// Find model
	modelPath := findModel()
	if modelPath == "" {
		fmt.Println("\nERROR: ASR model not found")
		os.Exit(1)
	}

	devName := "WAVE_MAPPER (auto)"
	if deviceID != WAVE_MAPPER {
		var caps waveInCapsW
		if ret, _, _ := waveInGetDevCapsW.Call(uintptr(deviceID), uintptr(unsafe.Pointer(&caps)), unsafe.Sizeof(caps)); ret == 0 {
			devName = fmt.Sprintf("[%d] %s", deviceID, getDeviceName(&caps))
		}
	}

	fmt.Printf("\nUsing device: %s\n", devName)
	fmt.Printf("Duration: %d seconds\n", seconds)
	fmt.Printf("Model: %s\n", filepath.Base(modelPath))
	fmt.Println("\n--- RECORDING START (speak now!) ---")

	pcmData, err := recordAudio(seconds, deviceID)
	if err != nil {
		fmt.Printf("\nRecording error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("--- RECORDING END ---")

	// Convert & analyze
	samples := int16ToFloat32(pcmData)
	dur := float64(len(samples)) / 16000.0
	rmsVal := calcRMS(samples)
	peakVal := calcPeak(samples)

	fmt.Printf("\nAudio stats: %.2fs, %d samples, RMS=%.5f, Peak=%.4f\n",
		dur, len(samples), rmsVal, peakVal)

	if rmsVal < 0.002 {
		fmt.Printf("  WARNING: Very low energy - possibly no speech captured\n")
		fmt.Printf("  Try: --dev <N> to select a different device\n")
	}

	// Save WAV
	if savePath == "" {
		savePath = filepath.Join(os.TempDir(), "maclaw_asr_record.wav")
	}
	writeWAV(savePath, pcmData)
	fmt.Printf("  Saved WAV: %s\n", savePath)

	// Transcribe
	fmt.Print("\nTranscribing... ")
	model, err := asr.NewMoonshine(modelPath)
	if err != nil {
		fmt.Printf("model load error: %v\n", err)
		os.Exit(1)
	}
	defer model.Close()

	start := time.Now()
	text, err := model.Transcribe(samples)
	elapsed := time.Since(start)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("done (%dms)\n", elapsed.Milliseconds())
	fmt.Println()
	fmt.Println("========================================")
	if text == "" {
		fmt.Println("  (empty - no speech detected)")
	} else {
		fmt.Printf("  %s\n", text)
	}
	fmt.Println("========================================")

	// Write result file
	home, _ := os.UserHomeDir()
	resultFile := filepath.Join(home, ".maclaw", "asr_last_result.txt")
	os.WriteFile(resultFile, []byte(fmt.Sprintf(
		"time=%s\ndevice=%s\nduration=%.2fs\nrms=%.5f\npeak=%.4f\ninference_ms=%d\nresult=%s\nwav=%s\n",
		time.Now().Format("2006-01-02 15:04:05"),
		devName, dur, rmsVal, peakVal,
		elapsed.Milliseconds(), text, savePath,
	)), 0644)
}

func recordAudio(seconds int, deviceID int) ([]byte, error) {
	sampleRate := uint32(16000)
	channels := uint16(1)
	bitsPerSample := uint16(16)
	blockAlign := channels * (bitsPerSample / 8)
	bufSize := uint32(sampleRate) * uint32(blockAlign) * uint32(seconds)

	wfx := waveFormatEx{
		FormatTag:      WAVE_FORMAT_PCM,
		Channels:       channels,
		SamplesPerSec:  sampleRate,
		AvgBytesPerSec: sampleRate * uint32(blockAlign),
		BlockAlign:     blockAlign,
		BitsPerSample:  bitsPerSample,
		CbSize:         0,
	}

	var hWaveIn uintptr
	ret, _, _ := waveInOpen.Call(
		uintptr(unsafe.Pointer(&hWaveIn)),
		uintptr(uint32(deviceID)),
		uintptr(unsafe.Pointer(&wfx)),
		0, 0,
		CALLBACK_NULL,
	)
	if ret != 0 {
		return nil, fmt.Errorf("waveInOpen failed (code=%d) - device may not support 16kHz mono", ret)
	}
	defer waveInClose.Call(hWaveIn)

	buf := make([]byte, bufSize)
	hdr := waveHdr{
		Data:         uintptr(unsafe.Pointer(&buf[0])),
		BufferLength: bufSize,
	}

	ret, _, _ = waveInPrepareHeader.Call(hWaveIn, uintptr(unsafe.Pointer(&hdr)), unsafe.Sizeof(hdr))
	if ret != 0 {
		return nil, fmt.Errorf("waveInPrepareHeader failed: %d", ret)
	}
	defer waveInUnprepareHeader.Call(hWaveIn, uintptr(unsafe.Pointer(&hdr)), unsafe.Sizeof(hdr))

	ret, _, _ = waveInAddBuffer.Call(hWaveIn, uintptr(unsafe.Pointer(&hdr)), unsafe.Sizeof(hdr))
	if ret != 0 {
		return nil, fmt.Errorf("waveInAddBuffer failed: %d", ret)
	}

	ret, _, _ = waveInStart.Call(hWaveIn)
	if ret != 0 {
		return nil, fmt.Errorf("waveInStart failed: %d", ret)
	}

	// Wait
	deadline := time.Now().Add(time.Duration(seconds)*time.Second + 500*time.Millisecond)
	for time.Now().Before(deadline) {
		if hdr.Flags&WHDR_DONE != 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	waveInStop.Call(hWaveIn)
	waveInReset.Call(hWaveIn)

	if hdr.BytesRecorded == 0 {
		return nil, fmt.Errorf("no audio recorded")
	}
	return buf[:hdr.BytesRecorded], nil
}

func int16ToFloat32(data []byte) []float32 {
	n := len(data) / 2
	samples := make([]float32, n)
	for i := 0; i < n; i++ {
		s := int16(binary.LittleEndian.Uint16(data[i*2:]))
		samples[i] = float32(s) / 32768.0
	}
	return samples
}

func writeWAV(path string, pcmData []byte) {
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	dataSize := uint32(len(pcmData))
	sampleRate := uint32(16000)
	f.Write([]byte("RIFF"))
	binary.Write(f, binary.LittleEndian, uint32(36+dataSize))
	f.Write([]byte("WAVE"))
	f.Write([]byte("fmt "))
	binary.Write(f, binary.LittleEndian, uint32(16))
	binary.Write(f, binary.LittleEndian, uint16(1))
	binary.Write(f, binary.LittleEndian, uint16(1))
	binary.Write(f, binary.LittleEndian, sampleRate)
	binary.Write(f, binary.LittleEndian, sampleRate*2)
	binary.Write(f, binary.LittleEndian, uint16(2))
	binary.Write(f, binary.LittleEndian, uint16(16))
	f.Write([]byte("data"))
	binary.Write(f, binary.LittleEndian, dataSize)
	f.Write(pcmData)
}

func findModel() string {
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, ".maclaw", "models", "moonshine-base-zh.gguf")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

func calcRMS(pcm []float32) float64 {
	var sum float64
	for _, s := range pcm {
		sum += float64(s) * float64(s)
	}
	return math.Sqrt(sum / float64(len(pcm)))
}

func calcPeak(pcm []float32) float64 {
	var peak float64
	for _, s := range pcm {
		if v := math.Abs(float64(s)); v > peak {
			peak = v
		}
	}
	return peak
}
