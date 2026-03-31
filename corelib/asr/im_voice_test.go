package asr

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/audioconv"
)

func TestIMVoiceSilkASR(t *testing.T) {
	modelPath := findModel(t)
	home, _ := os.UserHomeDir()
	silkPath := filepath.Join(home, ".maclaw", "im_files",
		"1774869494928_59abdb9e694d88f1a68467cbd6a1cb3d.amr")
	data, err := os.ReadFile(silkPath)
	if err != nil {
		t.Skipf("silk test file not found: %v", err)
	}
	t.Logf("silk/amr file: %d bytes, magic: %x %x %x",
		len(data), data[0], data[1], data[2])

	// Step 1: audioconv.ToWAV
	wavData, err := audioconv.ToWAV(data, "silk")
	if err != nil {
		t.Fatalf("audioconv.ToWAV(silk): %v", err)
	}
	t.Logf("WAV output: %d bytes", len(wavData))

	// Step 2: WAVToFloat32
	pcm, err := WAVToFloat32(wavData)
	if err != nil {
		t.Fatalf("WAVToFloat32: %v", err)
	}
	t.Logf("PCM: %d samples (%.2fs)", len(pcm), float64(len(pcm))/16000)

	// Step 3: Transcribe
	m, err := NewMoonshine(modelPath)
	if err != nil {
		t.Fatalf("load model: %v", err)
	}
	text, err := m.Transcribe(pcm)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	fmt.Printf("QQ silk ASR: %q\n", text)
	t.Logf("result: %q", text)
	if text == "" {
		t.Error("empty transcription from silk")
	}
}

func TestIMVoiceOpusASR(t *testing.T) {
	modelPath := findModel(t)
	home, _ := os.UserHomeDir()
	opusPath := filepath.Join(home, ".maclaw", "im_files",
		"1774870607129_file_v3_00109_3d9a5940-d80a-424b-ab80-b276fe1832dg")
	data, err := os.ReadFile(opusPath)
	if err != nil {
		t.Skipf("opus test file not found: %v", err)
	}
	t.Logf("feishu opus file: %d bytes, magic: %s",
		len(data), string(data[:4]))

	// Step 1: audioconv.ToWAV (auto-detect format)
	wavData, err := audioconv.ToWAV(data, "")
	if err != nil {
		t.Fatalf("audioconv.ToWAV(opus): %v", err)
	}
	t.Logf("WAV output: %d bytes", len(wavData))

	// Step 2: WAVToFloat32
	pcm, err := WAVToFloat32(wavData)
	if err != nil {
		t.Fatalf("WAVToFloat32: %v", err)
	}
	t.Logf("PCM: %d samples (%.2fs)", len(pcm), float64(len(pcm))/16000)

	// Step 3: Transcribe
	m, err := NewMoonshine(modelPath)
	if err != nil {
		t.Fatalf("load model: %v", err)
	}
	text, err := m.Transcribe(pcm)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	fmt.Printf("Feishu opus ASR: %q\n", text)
	t.Logf("result: %q", text)
	if text == "" {
		t.Error("empty transcription from opus")
	}
}
