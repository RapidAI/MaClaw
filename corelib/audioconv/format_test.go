package audioconv

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

func TestFormatFromPath(t *testing.T) {
	cases := map[string]string{
		`D:\a\b\c.wav`: FormatWAV,
		`/tmp/x.MP3`:   FormatMP3,
		`voice.ogg`:    FormatOGG,
		`voice.opus`:   FormatOpus,
		`msg.silk`:     FormatSilk,
		`msg.slk`:      FormatSilk,
		`song.m4a`:     FormatM4A,
		`clip.aac`:     FormatAAC,
		`clip.mp4`:     FormatM4A,
		`readme.txt`:   "",
		`noext`:        "",
	}
	for path, want := range cases {
		if got := FormatFromPath(path); got != want {
			t.Fatalf("FormatFromPath(%q)=%q want %q", path, got, want)
		}
	}
}

func TestNormalizeFormatHint(t *testing.T) {
	cases := map[string]string{
		"":             "",
		"WAV":          FormatWAV,
		"wave":         FormatWAV,
		"audio/wav":    FormatWAV,
		"audio/mpeg":   FormatMP3,
		"mp3":          FormatMP3,
		"audio/mp4":    FormatM4A,
		"x-m4a":        FormatM4A,
		"audio/x-m4a":  FormatM4A,
		"silk_v3":      FormatSilk,
		"audio/ogg":    FormatOGG,
	}
	for in, want := range cases {
		if got := NormalizeFormatHint(in); got != want {
			t.Fatalf("NormalizeFormatHint(%q)=%q want %q", in, got, want)
		}
	}
}

func TestStripFileURL(t *testing.T) {
	if got := StripFileURL(`  D:\a\b.wav  `); got != `D:\a\b.wav` {
		t.Fatalf("plain path: %q", got)
	}
	if runtime.GOOS == "windows" {
		for _, in := range []string{
			`file:///C:/Users/test/a.m4a`,
			`file://C:/Users/test/a.m4a`, // host=C: must NOT become UNC
			`file://localhost/C:/Users/test/a.m4a`,
		} {
			got := StripFileURL(in)
			lower := strings.ToLower(got)
			if strings.HasPrefix(got, `\\`) {
				t.Fatalf("drive URL became UNC: in=%q out=%q", in, got)
			}
			if !strings.Contains(lower, `users\test\a.m4a`) && !strings.Contains(lower, `users/test/a.m4a`) {
				t.Fatalf("windows file url: in=%q out=%q", in, got)
			}
			if !strings.HasPrefix(strings.ToUpper(got), "C:") {
				t.Fatalf("expected C: drive: in=%q out=%q", in, got)
			}
		}
	} else {
		got := StripFileURL(`file:///tmp/a.m4a`)
		if got != `/tmp/a.m4a` {
			t.Fatalf("unix file url: %q", got)
		}
	}
	got := StripFileURL(`file:///tmp/my%20song.wav`)
	if !strings.Contains(got, "my song.wav") {
		t.Fatalf("url unescape: %q", got)
	}
}

func TestIsDirectASRFormat(t *testing.T) {
	if !IsDirectASRFormat("wav") || !IsDirectASRFormat("audio/mpeg") {
		t.Fatal("expected direct formats")
	}
	if IsDirectASRFormat("m4a") || IsDirectASRFormat("flac") || IsDirectASRFormat("") {
		t.Fatal("m4a/flac/empty should not be direct")
	}
}

func TestASRToolDescriptionMentionsDirectFormats(t *testing.T) {
	d := ASRToolDescription()
	if !strings.Contains(d, DirectASRFormats) || !strings.Contains(d, "ffmpeg") {
		t.Fatalf("description incomplete: %s", d)
	}
}

func TestIsNativeDecodeUnsupported(t *testing.T) {
	_, err := ToWAV([]byte("not-m4a"), FormatM4A)
	if err == nil {
		t.Fatal("expected m4a decode error")
	}
	if !IsNativeDecodeUnsupported(err) {
		t.Fatalf("expected native-decode unsupported, got %v", err)
	}
	if got := NativeDecodeUnsupportedFormat(err); got != FormatM4A {
		t.Fatalf("format label = %q", got)
	}
	if IsNativeDecodeUnsupported(errors.New("audioconv: empty input data")) {
		t.Fatal("empty input should not be treated as native-decode unsupported")
	}
}

func TestAgentConvertHint(t *testing.T) {
	hint := AgentConvertHint(`D:\work\song.m4a`, FormatM4A)
	for _, needle := range []string{"m4a", DirectASRFormats, "ffmpeg", `D:\work\song.wav`, `D:\work\song.m4a`, `asr(path="`, "Whisper"} {
		if !strings.Contains(hint, needle) {
			t.Fatalf("hint missing %q: %s", needle, hint)
		}
	}
}

func TestToWAVM4AHintFallsBackToSupportedContent(t *testing.T) {
	// Minimal valid WAV bytes labeled as m4a should decode as WAV (wrong extension).
	wavIn := testWAVPCM(1, 1, 16000, 16, []byte{0, 0, 0, 0})
	got, err := ToWAV(wavIn, FormatM4A)
	if err != nil {
		t.Fatalf("ToWAV m4a-labeled wav: %v", err)
	}
	if len(got) < 44 || string(got[0:4]) != "RIFF" {
		t.Fatalf("expected WAV output, got %d bytes", len(got))
	}
}

func TestToWAVAcceptsMIMEFormatHints(t *testing.T) {
	wavIn := testWAVPCM(1, 1, 16000, 16, []byte{0, 0, 0, 0})
	got, err := ToWAV(wavIn, "audio/wav")
	if err != nil {
		t.Fatalf("audio/wav: %v", err)
	}
	if len(got) < 44 {
		t.Fatalf("short: %d", len(got))
	}
}
