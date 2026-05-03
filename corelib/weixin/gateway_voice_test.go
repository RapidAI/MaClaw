package weixin

import (
	"encoding/binary"
	"strconv"
	"testing"
)

func TestEstimateVoicePlaytimeMS(t *testing.T) {
	wav := makeTestWAV(16000, 2, 16000*2*3)
	if got := estimateVoicePlaytimeMS(wav); got != 3000 {
		t.Fatalf("estimateVoicePlaytimeMS(wav) = %d, want 3000", got)
	}

	if got := estimateVoicePlaytimeMS([]byte("not wav")); got != 0 {
		t.Fatalf("estimateVoicePlaytimeMS(non-wav) = %d, want 0", got)
	}
}

func TestVoiceUploadPayloadEncodesWAVToSilk(t *testing.T) {
	wav := makeTestWAV(16000, 2, 16000*2*3)
	payload, meta, err := voiceUploadPayload("voice.wav", wav)
	if err != nil {
		t.Fatalf("voiceUploadPayload(wav) error = %v", err)
	}
	if !isSilkVoicePayload(payload) {
		t.Fatalf("payload is not SILK: %q", payload[:min(len(payload), 10)])
	}
	if string(payload[:4]) == "RIFF" {
		t.Fatal("payload still contains WAV header")
	}
	if meta == nil || meta.sampleRate != weixinVoiceSampleRate || meta.playtimeMS != 3000 {
		t.Fatalf("meta = %#v, want sample_rate=%d playtime=3000", meta, weixinVoiceSampleRate)
	}
	if meta.payloadSize != len(payload) || meta.payloadMD5 == "" {
		t.Fatalf("meta payload fields = size %d md5 %q, want size %d and md5", meta.payloadSize, meta.payloadMD5, len(payload))
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

func TestVoiceUploadPayloadSniffsWAVWhenFileNameMissing(t *testing.T) {
	wav := makeTestWAV(16000, 2, 16000*2)
	payload, meta, err := voiceUploadPayload("", wav)
	if err != nil {
		t.Fatalf("voiceUploadPayload(empty wav) error = %v", err)
	}
	if !isSilkVoicePayload(payload) || meta == nil || meta.sampleRate != weixinVoiceSampleRate {
		t.Fatalf("voiceUploadPayload(empty wav) payload_len=%d meta=%v", len(payload), meta)
	}
}

func TestBuildVoiceItemUsesSilkEncode(t *testing.T) {
	media := &cdnMedia{EncryptQueryParam: "q", AESKey: "k", EncryptType: 1}
	_, meta, ok := wavVoicePayload(makeTestWAV(16000, 2, 16000*2))
	if !ok {
		t.Fatal("wavVoicePayload() failed")
	}
	item := buildVoiceItem(media, &meta)
	if item.EncodeType != 6 {
		t.Fatalf("buildVoiceItem(empty, meta).EncodeType = %d, want 6", item.EncodeType)
	}
}

func TestBuildVoiceItemOnlyAddsWAVMetadataWhenKnown(t *testing.T) {
	media := &cdnMedia{EncryptQueryParam: "q", AESKey: "k", EncryptType: 1}
	_, meta, ok := wavVoicePayload(makeTestWAV(16000, 2, 16000*2*3))
	if !ok {
		t.Fatal("wavVoicePayload() failed")
	}
	wavItem := buildVoiceItem(media, &meta)
	if wavItem.EncodeType != 6 || wavItem.SampleRate != 16000 || wavItem.BitsPerSample != 0 || wavItem.Playtime != 3000 {
		t.Fatalf("buildVoiceItem(wav) = encode=%d sr=%d bits=%d playtime=%d", wavItem.EncodeType, wavItem.SampleRate, wavItem.BitsPerSample, wavItem.Playtime)
	}

	unknownItem := buildVoiceItem(media, nil)
	if unknownItem.EncodeType != 6 || unknownItem.SampleRate != 0 || unknownItem.BitsPerSample != 0 || unknownItem.Playtime != 0 {
		t.Fatalf("buildVoiceItem(nil) = encode=%d sr=%d bits=%d playtime=%d", unknownItem.EncodeType, unknownItem.SampleRate, unknownItem.BitsPerSample, unknownItem.Playtime)
	}
}

func TestBuildVoiceItemIncludesPayloadIntegrityMetadata(t *testing.T) {
	wav := makeTestWAV(16000, 2, 16000*2)
	payload, meta, err := voiceUploadPayload("voice.wav", wav)
	if err != nil {
		t.Fatalf("voiceUploadPayload(wav) error = %v", err)
	}
	media := &cdnMedia{EncryptQueryParam: "q", AESKey: "k", EncryptType: 1}
	item := buildVoiceItem(media, meta)
	if item.Len != strconv.Itoa(len(payload)) {
		t.Fatalf("voice item len = %q, want %d", item.Len, len(payload))
	}
	if item.VoiceMD5 == "" {
		t.Fatal("voice item md5 is empty")
	}
}

func TestVoiceUploadPayloadKeepsShortWAV(t *testing.T) {
	wav := makeTestWAV(16000, 2, 16000*2/10)
	payload, meta, err := voiceUploadPayload("voice.wav", wav)
	if err != nil {
		t.Fatalf("voiceUploadPayload(short wav) error = %v", err)
	}
	if !isSilkVoicePayload(payload) || estimateSilkPlaytimeMS(payload) == 0 {
		t.Fatalf("short WAV produced invalid SILK payload_len=%d meta=%v", len(payload), meta)
	}
}

func TestVoiceUploadPayloadKeepsValidSilk(t *testing.T) {
	data := []byte("\x02#!SILK_V3\x01\x00x")
	payload, meta, err := voiceUploadPayload("voice.silk", data)
	if err != nil {
		t.Fatalf("voiceUploadPayload(silk) error = %v", err)
	}
	if string(payload) != string(data) || meta == nil || meta.sampleRate != weixinVoiceSampleRate {
		t.Fatalf("voiceUploadPayload(silk) payload=%q meta=%v", payload, meta)
	}
}

func TestVoiceUploadPayloadRejectsNonWAVVoice(t *testing.T) {
	if _, _, err := voiceUploadPayload("voice.ogg", []byte("ogg data")); err == nil {
		t.Fatal("voiceUploadPayload(ogg) error = nil, want error")
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

func TestValidateAPIStatusRejectsSendMessageError(t *testing.T) {
	if err := validateAPIStatus("sendmessage", []byte(`{"ret":0,"errcode":0}`)); err != nil {
		t.Fatalf("validateAPIStatus(success) error = %v", err)
	}
	if err := validateAPIStatus("sendmessage", []byte(`{"ret":0,"errcode":-1,"errmsg":"bad voice"}`)); err == nil {
		t.Fatal("validateAPIStatus(error) = nil, want error")
	}
}
