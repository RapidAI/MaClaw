//go:build windows

package embedding

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExpandInfString covers the device-name forms the Enum keys actually store.
// The indirect "@inf,key;fallback" form is what an INF-installed driver writes,
// and showing it raw is what put "@oem21.inf,%ipumcdmdriver%;NPU Compute
// Accelerator Device" in the About badge.
func TestExpandInfString(t *testing.T) {
	dir := t.TempDir()
	inf := filepath.Join(dir, "oem99.inf")
	body := "; test INF\r\n[Version]\r\nSignature=\"$WINDOWS NT$\"\r\n\r\n" +
		"[Strings]\r\nipumcdmdriver = \"AMD IPU Device\"\r\nquoted = \"  padded  \"\r\n\r\n" +
		"[OtherSection]\r\nipumcdmdriver = \"wrong section\"\r\n"
	if err := os.WriteFile(inf, []byte(body), 0o644); err != nil {
		t.Fatalf("write INF: %v", err)
	}

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain name passes through", "AMD XDNA NPU", "AMD XDNA NPU"},
		{"empty stays empty", "", ""},
		{"key resolves from Strings", "@" + inf + ",ipumcdmdriver;NPU Compute Accelerator Device", "AMD IPU Device"},
		{"missing key falls back to description", "@" + inf + ",nosuchkey;NPU Compute Accelerator Device", "NPU Compute Accelerator Device"},
		{"missing file falls back to description", "@" + filepath.Join(dir, "absent.inf") + ",k;Fallback Text", "Fallback Text"},
		{"key match is case-insensitive", "@" + inf + ",IPUMCDMDRIVER;fb", "AMD IPU Device"},
		{"other sections are ignored", "@" + inf + ",ipumcdmdriver;fb", "AMD IPU Device"},
		{"quotes are stripped", "@" + inf + ",quoted;fb", "padded"},
		{"surrounding space is trimmed", "  AMD XDNA NPU  ", "AMD XDNA NPU"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := expandInfString(c.in); got != c.want {
				t.Fatalf("expandInfString(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestExpandInfStringNoFallbackKeepsRaw: with neither a resolvable key nor a
// description there is nothing better to show than the raw value, and returning
// an empty string would blank the device name in the diagnostics.
func TestExpandInfStringNoFallbackKeepsRaw(t *testing.T) {
	raw := "@absent.inf,%missing%"
	if got := expandInfString(raw); got != raw {
		t.Fatalf("got %q, want the raw value %q", got, raw)
	}
}

// TestDecodeInfText covers the encodings an INF can arrive in.
func TestDecodeInfText(t *testing.T) {
	ascii := []byte("[Strings]\r\nk = \"v\"\r\n")
	if got := decodeInfText(ascii); got != string(ascii) {
		t.Fatalf("ascii: got %q", got)
	}

	// UTF-16 LE with a BOM, carrying a non-Latin string.
	text := "[Strings]\r\nk = \"加速器\"\r\n"
	raw := []byte{0xFF, 0xFE}
	for _, r := range text {
		if r < 0x10000 {
			raw = append(raw, byte(r), byte(r>>8))
		} else {
			r -= 0x10000
			hi, lo := 0xD800+(r>>10), 0xDC00+(r&0x3FF)
			raw = append(raw, byte(hi), byte(hi>>8), byte(lo), byte(lo>>8))
		}
	}
	if got := decodeInfText(raw); got != text {
		t.Fatalf("utf16: got %q want %q", got, text)
	}

	// UTF-8 with a BOM.
	withBOM := append([]byte{0xEF, 0xBB, 0xBF}, []byte("x")...)
	if got := decodeInfText(withBOM); got != "x" {
		t.Fatalf("utf8 bom: got %q", got)
	}
}

// TestLookupInfStringUTF16 INF pins the end-to-end read of a UTF-16 INF, the
// form localized drivers ship.
func TestLookupInfStringUTF16INF(t *testing.T) {
	dir := t.TempDir()
	inf := filepath.Join(dir, "oem98.inf")
	text := "[Strings]\r\nnpu = \"NPU 计算加速器\"\r\n"
	raw := []byte{0xFF, 0xFE}
	for _, r := range text {
		raw = append(raw, byte(r), byte(r>>8))
	}
	if err := os.WriteFile(inf, raw, 0o644); err != nil {
		t.Fatalf("write INF: %v", err)
	}
	if got := lookupInfString(inf, "npu"); got != "NPU 计算加速器" {
		t.Fatalf("got %q", got)
	}
}
