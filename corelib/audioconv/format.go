package audioconv

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// DirectASRFormats documents formats that ToWAV can decode without external tools.
// Agent tool descriptions and error messages should stay aligned with this list.
const DirectASRFormats = "wav, mp3, ogg/opus, silk"

// MaxNativeAudioInputBytes is the shared read/decode size cap for local ASR input.
const MaxNativeAudioInputBytes = 32 << 20

// ErrNativeDecodeUnsupported is returned when the container is recognized
// (e.g. m4a/aac) but no native decoder is wired yet.
var ErrNativeDecodeUnsupported = errors.New("audioconv: native decode is not supported")

// NativeDecodeUnsupportedError carries the format label for agent-facing hints.
type NativeDecodeUnsupportedError struct {
	Format string
}

func (e *NativeDecodeUnsupportedError) Error() string {
	if e == nil {
		return ErrNativeDecodeUnsupported.Error()
	}
	format := strings.TrimSpace(e.Format)
	if format == "" {
		return ErrNativeDecodeUnsupported.Error()
	}
	return fmt.Sprintf("%s: %s", ErrNativeDecodeUnsupported.Error(), format)
}

func (e *NativeDecodeUnsupportedError) Unwrap() error {
	return ErrNativeDecodeUnsupported
}

// NewNativeDecodeUnsupported returns a typed unsupported-decode error.
func NewNativeDecodeUnsupported(format string) error {
	format = NormalizeFormatHint(format)
	if format == "" {
		format = FormatM4A
	}
	return &NativeDecodeUnsupportedError{Format: format}
}

// NormalizeFormatHint maps MIME-ish / alias labels onto Format* constants.
// Empty input stays empty (meaning auto-detect).
func NormalizeFormatHint(format string) string {
	f := strings.ToLower(strings.TrimSpace(format))
	if f == "" {
		return ""
	}
	if i := strings.IndexByte(f, ';'); i >= 0 {
		f = strings.TrimSpace(f[:i])
	}
	f = strings.TrimPrefix(f, "audio/")
	f = strings.TrimPrefix(f, "x-")
	switch f {
	case "wav", "wave":
		return FormatWAV
	case "mp3", "mpeg", "mpga":
		return FormatMP3
	case "ogg", "oga":
		return FormatOGG
	case "opus":
		return FormatOpus
	case "silk", "slk", "silk_v3":
		return FormatSilk
	case "m4a", "mp4", "m4b", "isom":
		return FormatM4A
	case "aac", "adts":
		return FormatAAC
	default:
		return f
	}
}

// FormatFromPath returns a ToWAV format hint from a file path extension.
// Empty string means "unknown; let ToWAV auto-detect from content".
// Unknown extensions return empty (do not pass through raw "txt"/"bin"/...).
func FormatFromPath(path string) string {
	path = StripFileURL(path)
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	switch n := NormalizeFormatHint(ext); n {
	case FormatWAV, FormatMP3, FormatOGG, FormatOpus, FormatSilk, FormatM4A, FormatAAC:
		return n
	default:
		return ""
	}
}

// StripFileURL converts file:// URIs to local filesystem paths.
// Non-file inputs are returned trimmed unchanged.
//
// Handles common Windows forms:
//   - file:///C:/path  (empty host, path=/C:/path)
//   - file://C:/path   (host=C:, path=/path) — not UNC
//   - file://localhost/C:/path
//   - file://server/share/path (UNC)
func StripFileURL(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	lower := strings.ToLower(path)
	if !strings.HasPrefix(lower, "file:") {
		return path
	}
	u, err := url.Parse(path)
	if err != nil {
		// Best-effort strip for malformed but common prefixes.
		p := path
		if strings.HasPrefix(lower, "file://") {
			p = path[len("file://"):]
		} else if strings.HasPrefix(lower, "file:") {
			p = path[len("file:"):]
		}
		p = strings.TrimPrefix(p, "localhost")
		if runtime.GOOS == "windows" && strings.HasPrefix(p, "/") && len(p) >= 3 && p[2] == ':' {
			p = p[1:]
		}
		return pathUnescapeBestEffort(p)
	}
	if !strings.EqualFold(u.Scheme, "file") {
		return path
	}
	p := u.Path
	host := u.Host
	if host != "" && !strings.EqualFold(host, "localhost") {
		// file://C:/Users/... parses as Host="C:", Path="/Users/..."
		if isWindowsDriveHost(host) {
			p = host + p
			if runtime.GOOS == "windows" {
				p = strings.ReplaceAll(p, "/", `\`)
			}
			return pathUnescapeBestEffort(p)
		}
		// UNC: file://server/share → \\server\share
		p = `\\` + host + strings.ReplaceAll(p, "/", `\`)
		return pathUnescapeBestEffort(p)
	}
	if runtime.GOOS == "windows" {
		// file:///C:/path → /C:/path → C:/path
		if strings.HasPrefix(p, "/") && len(p) >= 3 && p[2] == ':' {
			p = p[1:]
		}
		p = strings.ReplaceAll(p, "/", `\`)
	}
	return pathUnescapeBestEffort(p)
}

func isWindowsDriveHost(host string) bool {
	// url.Parse("file://C:/x") → Host "C:"
	if len(host) != 2 || host[1] != ':' {
		return false
	}
	c := host[0]
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func pathUnescapeBestEffort(p string) string {
	if unesc, err := url.PathUnescape(p); err == nil {
		return unesc
	}
	return p
}

// IsDirectASRFormat reports whether format is natively decodable for ASR.
// Empty format (auto-detect) returns false — caller must rely on content.
func IsDirectASRFormat(format string) bool {
	switch NormalizeFormatHint(format) {
	case FormatWAV, FormatMP3, FormatOGG, FormatOpus, FormatSilk:
		return true
	default:
		return false
	}
}

// ASRToolDescription is the shared agent-facing description for the asr tool.
func ASRToolDescription() string {
	return "本地语音识别（ASR）。将音频文件转写为文本。直接支持: " + DirectASRFormats +
		"。推荐 16kHz mono WAV。m4a/aac/其它格式请先用 bash+ffmpeg 转为 16kHz mono 16-bit WAV 再调用；不要安装 Whisper。"
}

// DetectFormat identifies audio format from magic bytes (empty if unknown).
func DetectFormat(data []byte) string {
	return detectFormat(data)
}

// IsNativeDecodeUnsupported reports whether err is the intentional
// m4a/aac "no native decoder" failure (as opposed to corrupt data, etc.).
func IsNativeDecodeUnsupported(err error) bool {
	return errors.Is(err, ErrNativeDecodeUnsupported)
}

// NativeDecodeUnsupportedFormat extracts the format label from a native-decode
// unsupported error (e.g. "m4a"). Empty if err is not that kind.
func NativeDecodeUnsupportedFormat(err error) string {
	var typed *NativeDecodeUnsupportedError
	if errors.As(err, &typed) && typed != nil {
		return typed.Format
	}
	return ""
}

// AgentConvertHint returns a short, agent-actionable message when a format
// cannot be decoded natively. Caller should convert with ffmpeg then retry asr.
func AgentConvertHint(path, format string) string {
	format = NormalizeFormatHint(format)
	if format == "" {
		format = "unknown"
	}
	src := strings.TrimSpace(StripFileURL(path))
	if src == "" {
		src = "input." + format
	}
	// Prefer a sibling .wav path so the agent can call asr(path=...) next.
	out := src
	if ext := filepath.Ext(src); ext != "" {
		out = strings.TrimSuffix(src, ext) + ".wav"
	} else {
		out = src + ".wav"
	}
	// Chinese primary (matches other tool messages) + copy-pasteable ffmpeg.
	// Plain double quotes keep Windows backslashes unescaped for the agent.
	return fmt.Sprintf(
		"ASR 无法原生解码 %s。直接支持: %s。请先转换，例如: "+
			"ffmpeg -y -i \"%s\" -ar 16000 -ac 1 -sample_fmt s16 \"%s\"，然后调用 asr(path=\"%s\")。不要安装 Whisper。",
		format, DirectASRFormats, src, out, out,
	)
}
