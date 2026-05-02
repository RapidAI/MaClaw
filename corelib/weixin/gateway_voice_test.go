package weixin

import (
	"encoding/binary"
	"testing"
)

func TestInferVoiceEncodeType(t *testing.T) {
	tests := []struct {
		name string
		want int
	}{
		{name: "voice.wav", want: 1},
		{name: "voice.pcm", want: 1},
		{name: "voice.amr", want: 5},
		{name: "voice.silk", want: 6},
		{name: "voice.slk", want: 6},
		{name: "voice.mp3", want: 7},
		{name: "voice.ogg", want: 8},
		{name: "voice.opus", want: 8},
		{name: "voice", want: 8},
	}

	for _, tt := range tests {
		if got := inferVoiceEncodeType(tt.name); got != tt.want {
			t.Fatalf("inferVoiceEncodeType(%q) = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestEstimateVoicePlaytimeMS(t *testing.T) {
	wav := makeTestWAV(16000, 2, 16000*2*3)
	if got := estimateVoicePlaytimeMS(wav); got != 3000 {
		t.Fatalf("estimateVoicePlaytimeMS(wav) = %d, want 3000", got)
	}

	if got := estimateVoicePlaytimeMS([]byte("not wav")); got != 0 {
		t.Fatalf("estimateVoicePlaytimeMS(non-wav) = %d, want 0", got)
	}
}

func TestWAVVoicePayloadStripsContainer(t *testing.T) {
	wav := makeTestWAV(16000, 2, 16000*2*3)
	payload, meta, ok := wavVoicePayload(wav)
	if !ok {
		t.Fatal("wavVoicePayload() failed")
	}
	if len(payload) != 16000*2*3 {
		t.Fatalf("payload len = %d, want %d", len(payload), 16000*2*3)
	}
	if string(payload[:4]) == "RIFF" {
		t.Fatal("payload still contains WAV header")
	}
	if meta.sampleRate != 16000 || meta.bitsPerSample != 16 || meta.playtimeMS != 3000 {
		t.Fatalf("meta = sr=%d bits=%d playtime=%d", meta.sampleRate, meta.bitsPerSample, meta.playtimeMS)
	}
}

func TestWAVVoicePayloadRejectsNonPCM(t *testing.T) {
	wav := makeTestWAV(16000, 2, 16000*2)
	binary.LittleEndian.PutUint16(wav[20:22], 3)
	if _, _, ok := wavVoicePayload(wav); ok {
		t.Fatal("wavVoicePayload(non-PCM) = ok, want false")
	}
}

func TestVoiceUploadPayloadRejectsInvalidWAV(t *testing.T) {
	if _, _, err := voiceUploadPayload("voice.wav", []byte("not wav")); err == nil {
		t.Fatal("voiceUploadPayload(invalid wav) error = nil, want error")
	}
}

func TestVoiceUploadPayloadRejectsRawPCMWithoutMetadata(t *testing.T) {
	if _, _, err := voiceUploadPayload("voice.pcm", []byte{1, 2, 3, 4}); err == nil {
		t.Fatal("voiceUploadPayload(raw pcm) error = nil, want error")
	}
}

func TestVoiceUploadPayloadKeepsNonWAVAsIs(t *testing.T) {
	data := []byte("ogg data")
	payload, meta, err := voiceUploadPayload("voice.ogg", data)
	if err != nil {
		t.Fatalf("voiceUploadPayload(ogg) error = %v", err)
	}
	if string(payload) != string(data) || meta != nil {
		t.Fatalf("voiceUploadPayload(ogg) = %q meta=%v, want original nil", payload, meta)
	}
}

func TestVoiceUploadPayloadSniffsWAVWhenFileNameMissing(t *testing.T) {
	wav := makeTestWAV(16000, 2, 16000*2)
	payload, meta, err := voiceUploadPayload("", wav)
	if err != nil {
		t.Fatalf("voiceUploadPayload(empty wav) error = %v", err)
	}
	if len(payload) != 16000*2 || meta == nil || meta.sampleRate != 16000 {
		t.Fatalf("voiceUploadPayload(empty wav) payload_len=%d meta=%v", len(payload), meta)
	}
}

func TestBuildVoiceItemUsesPCMEncodeWhenMetadataPresent(t *testing.T) {
	media := &cdnMedia{EncryptQueryParam: "q", AESKey: "k", EncryptType: 1}
	_, meta, ok := wavVoicePayload(makeTestWAV(16000, 2, 16000*2))
	if !ok {
		t.Fatal("wavVoicePayload() failed")
	}
	item := buildVoiceItem(media, "", &meta)
	if item.EncodeType != 1 {
		t.Fatalf("buildVoiceItem(empty, meta).EncodeType = %d, want 1", item.EncodeType)
	}
}

func TestBuildVoiceItemOnlyAddsWAVMetadataWhenKnown(t *testing.T) {
	media := &cdnMedia{EncryptQueryParam: "q", AESKey: "k", EncryptType: 1}
	_, meta, ok := wavVoicePayload(makeTestWAV(16000, 2, 16000*2*3))
	if !ok {
		t.Fatal("wavVoicePayload() failed")
	}
	wavItem := buildVoiceItem(media, "voice.wav", &meta)
	if wavItem.EncodeType != 1 || wavItem.SampleRate != 16000 || wavItem.BitsPerSample != 16 || wavItem.Playtime != 3000 {
		t.Fatalf("buildVoiceItem(wav) = encode=%d sr=%d bits=%d playtime=%d", wavItem.EncodeType, wavItem.SampleRate, wavItem.BitsPerSample, wavItem.Playtime)
	}

	oggItem := buildVoiceItem(media, "voice.ogg", nil)
	if oggItem.EncodeType != 8 || oggItem.SampleRate != 0 || oggItem.BitsPerSample != 0 || oggItem.Playtime != 0 {
		t.Fatalf("buildVoiceItem(ogg) = encode=%d sr=%d bits=%d playtime=%d", oggItem.EncodeType, oggItem.SampleRate, oggItem.BitsPerSample, oggItem.Playtime)
	}
}

func makeTestWAV(sampleRate, bytesPerSample, dataBytes int) []byte {
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
