package weixin

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
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
	if meta.bitsPerSample != 0 || meta.channels != 1 {
		t.Fatalf("converted SILK meta bits=%d channels=%d, want compressed mono metadata", meta.bitsPerSample, meta.channels)
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
	if item.EncodeType != 4 {
		t.Fatalf("buildVoiceItem(empty, meta).EncodeType = %d, want 4", item.EncodeType)
	}
	if item.Format != "" || item.MimeType != "" {
		t.Fatalf("buildVoiceItem format = %q mime = %q, want omitted", item.Format, item.MimeType)
	}
}

func TestBuildVoiceItemOnlyAddsWAVMetadataWhenKnown(t *testing.T) {
	media := &cdnMedia{EncryptQueryParam: "q", AESKey: "k", EncryptType: 1}
	_, meta, ok := wavVoicePayload(makeTestWAV(16000, 2, 16000*2*3))
	if !ok {
		t.Fatal("wavVoicePayload() failed")
	}
	wavItem := buildVoiceItem(media, &meta)
	if wavItem.EncodeType != 4 || wavItem.SampleRate != 16000 || wavItem.BitsPerSample != 0 || wavItem.Playtime != 3000 {
		t.Fatalf("buildVoiceItem(wav) = encode=%d sr=%d bits=%d playtime=%d", wavItem.EncodeType, wavItem.SampleRate, wavItem.BitsPerSample, wavItem.Playtime)
	}

	unknownItem := buildVoiceItem(media, nil)
	if unknownItem.EncodeType != 4 || unknownItem.SampleRate != 0 || unknownItem.BitsPerSample != 0 || unknownItem.Playtime != 0 {
		t.Fatalf("buildVoiceItem(nil) = encode=%d sr=%d bits=%d playtime=%d", unknownItem.EncodeType, unknownItem.SampleRate, unknownItem.BitsPerSample, unknownItem.Playtime)
	}
}

func TestBuildVoiceItemMatchesInboundShapeWithoutPayloadIntegrityFields(t *testing.T) {
	wav := makeTestWAV(16000, 2, 16000*2)
	_, meta, err := voiceUploadPayload("voice.wav", wav)
	if err != nil {
		t.Fatalf("voiceUploadPayload(wav) error = %v", err)
	}
	media := &cdnMedia{EncryptQueryParam: "q", AESKey: "k", EncryptType: 1}
	item := buildVoiceItem(media, meta)
	if item.Len != "" || item.Size != "" || item.VoiceMD5 != "" || item.MD5 != "" {
		t.Fatalf("voice item integrity fields len=%q size=%q voice_md5=%q md5=%q, want omitted", item.Len, item.Size, item.VoiceMD5, item.MD5)
	}
}

func TestVoiceItemsDebugSummaryRedactsMediaSecrets(t *testing.T) {
	items := []messageItem{{
		Type: ItemTypeVoice,
		VoiceItem: &voiceItem{
			Media:      &cdnMedia{EncryptQueryParam: "secret-query", AESKey: "secret-key", EncryptType: 1},
			EncodeType: 6,
			Format:     "silk",
			MimeType:   "audio/silk",
			SampleRate: 16000,
			Playtime:   1000,
			Len:        "510",
			VoiceMD5:   "0123456789abcdef0123456789abcdef",
		},
	}}
	got := voiceItemsDebugSummary(items)
	if got == "" {
		t.Fatal("voiceItemsDebugSummary() is empty")
	}
	if strings.Contains(got, "secret-query") || strings.Contains(got, "secret-key") {
		t.Fatalf("voiceItemsDebugSummary leaked media secret: %s", got)
	}
	if !strings.Contains(got, `"format":"silk"`) || !strings.Contains(got, `"sample_rate":16000`) {
		t.Fatalf("voiceItemsDebugSummary missing voice fields: %s", got)
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

func TestVoiceUploadPayloadRecordsSilkDiagnostics(t *testing.T) {
	wav := makeTestWAV(16000, 2, 16000*2)
	payload, meta, err := voiceUploadPayload("voice.wav", wav)
	if err != nil {
		t.Fatalf("voiceUploadPayload(wav) error = %v", err)
	}
	if !isSilkVoicePayload(payload) {
		t.Fatal("payload is not SILK")
	}
	if meta == nil || meta.packetCount == 0 || meta.packetBytes == 0 || meta.packetSizeMin == 0 || meta.packetSizeMax == 0 {
		t.Fatalf("missing packet diagnostics: %#v", meta)
	}
	if meta.decodeError != "" || meta.decodedPCM == 0 || meta.decodedMS == 0 {
		t.Fatalf("decode diagnostics failed: %#v", meta)
	}
}

func TestSaveDebugWeixinVoicePayloadWritesSilkAndMetadata(t *testing.T) {
	dir := t.TempDir()
	old := weixinVoiceDebugDirForTest
	weixinVoiceDebugDirForTest = dir
	t.Cleanup(func() { weixinVoiceDebugDirForTest = old })

	wav := makeTestWAV(16000, 2, 16000*2)
	data, voiceMeta, err := voiceUploadPayload("voice.wav", wav)
	if err != nil {
		t.Fatalf("voiceUploadPayload(wav) error = %v", err)
	}
	path, err := saveDebugWeixinVoicePayload(data, voiceMeta)
	if err != nil {
		t.Fatalf("saveDebugWeixinVoicePayload() error = %v", err)
	}
	if filepath.Dir(path) != dir || filepath.Ext(path) != ".silk" {
		t.Fatalf("debug path = %q, want .silk in %q", path, dir)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read debug silk: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("debug silk payload = %q, want %q", got, data)
	}
	var meta map[string]any
	metaBytes, err := os.ReadFile(path + ".json")
	if err != nil {
		t.Fatalf("read debug metadata: %v", err)
	}
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("decode debug metadata: %v", err)
	}
	if meta["is_silk"] != true || int(meta["sample_rate"].(float64)) != weixinVoiceSampleRate || int(meta["packet_count"].(float64)) == 0 || int(meta["decoded_pcm_bytes"].(float64)) == 0 {
		t.Fatalf("debug metadata = %#v", meta)
	}
}

func TestSaveDebugWeixinVoicePayloadKeepsRecentFiles(t *testing.T) {
	dir := t.TempDir()
	old := weixinVoiceDebugDirForTest
	weixinVoiceDebugDirForTest = dir
	t.Cleanup(func() { weixinVoiceDebugDirForTest = old })

	for i := 0; i < weixinVoiceDebugKeep+3; i++ {
		path := filepath.Join(dir, "old_"+strconv.Itoa(i)+".silk")
		if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
			t.Fatalf("write old silk: %v", err)
		}
		if err := os.WriteFile(path+".json", []byte("{}"), 0o600); err != nil {
			t.Fatalf("write old metadata: %v", err)
		}
		mtime := time.Now().Add(-time.Duration(weixinVoiceDebugKeep+3-i) * time.Minute)
		_ = os.Chtimes(path, mtime, mtime)
	}

	if _, err := saveDebugWeixinVoicePayload([]byte("\x02#!SILK_V3\x01\x00x"), nil); err != nil {
		t.Fatalf("saveDebugWeixinVoicePayload() error = %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read debug dir: %v", err)
	}
	silkCount := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".silk" {
			silkCount++
		}
	}
	if silkCount != weixinVoiceDebugKeep {
		t.Fatalf("silk file count = %d, want %d", silkCount, weixinVoiceDebugKeep)
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
	if err := validateAPIStatus("sendmessage", []byte(`{"code":200,"message":"ok"}`)); err != nil {
		t.Fatalf("validateAPIStatus(code 200 success) error = %v", err)
	}
	if err := validateAPIStatus("sendmessage", []byte(`{"ret":0,"errcode":-1,"errmsg":"bad voice"}`)); err == nil {
		t.Fatal("validateAPIStatus(error) = nil, want error")
	}
	if err := validateAPIStatus("sendmessage", []byte(`{"code":500,"message":"bad voice"}`)); err == nil {
		t.Fatal("validateAPIStatus(code error) = nil, want error")
	}
}

func TestCompactAPIResponseLog(t *testing.T) {
	if got := compactAPIResponseLog([]byte("   \n")); got != "<empty>" {
		t.Fatalf("compactAPIResponseLog(empty) = %q", got)
	}
	long := []byte(`{"ret":0,"data":"` + string(make([]byte, 600)) + `"}`)
	if got := compactAPIResponseLog(long); len(got) != 512 {
		t.Fatalf("compactAPIResponseLog(long) length = %d, want 512", len(got))
	}
}
