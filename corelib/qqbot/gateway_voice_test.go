package qqbot

import (
	"encoding/base64"
	"encoding/binary"
	"testing"
)

func TestPrepareQQVoiceDataEncodesWAVToSilk(t *testing.T) {
	wav := makeQQTestWAV(16000, 2, 16000*2)
	got, err := prepareQQVoiceData("voice.wav", base64.StdEncoding.EncodeToString(wav))
	if err != nil {
		t.Fatalf("prepareQQVoiceData(wav) error = %v", err)
	}
	data, err := base64.StdEncoding.DecodeString(got)
	if err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if !isTencentSilk(data) {
		t.Fatalf("prepared voice is not SILK: %q", data[:min(len(data), 10)])
	}
	if string(data[:4]) == "RIFF" {
		t.Fatal("prepared voice still contains WAV header")
	}
}

func TestPrepareQQVoiceDataKeepsValidSilk(t *testing.T) {
	silkData := []byte("\x02#!SILK_V3\x01\x00x")
	got, err := prepareQQVoiceData("voice.silk", base64.StdEncoding.EncodeToString(silkData))
	if err != nil {
		t.Fatalf("prepareQQVoiceData(silk) error = %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(got)
	if err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if string(decoded) != string(silkData) {
		t.Fatalf("prepared silk = %q, want original", decoded)
	}
}

func TestPrepareQQVoiceDataRejectsOGG(t *testing.T) {
	if _, err := prepareQQVoiceData("voice.ogg", base64.StdEncoding.EncodeToString([]byte("ogg data"))); err == nil {
		t.Fatal("prepareQQVoiceData(ogg) error = nil, want error")
	}
}

func TestPrepareQQVoiceDataRejectsInvalidSilkExtension(t *testing.T) {
	if _, err := prepareQQVoiceData("voice.silk", base64.StdEncoding.EncodeToString([]byte("not silk"))); err == nil {
		t.Fatal("prepareQQVoiceData(invalid silk) error = nil, want error")
	}
}

func makeQQTestWAV(sampleRate, bytesPerSample, dataBytes int) []byte {
	byteRate := sampleRate * bytesPerSample
	out := make([]byte, 44+dataBytes)
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(36+dataBytes))
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16)
	binary.LittleEndian.PutUint16(out[20:22], 1)
	binary.LittleEndian.PutUint16(out[22:24], 1)
	binary.LittleEndian.PutUint32(out[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(out[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(out[32:34], uint16(bytesPerSample))
	binary.LittleEndian.PutUint16(out[34:36], uint16(bytesPerSample*8))
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(dataBytes))
	return out
}
