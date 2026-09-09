package guiapp

import "testing"

func TestLanguageFromLANGID(t *testing.T) {
	cases := map[uint16]string{
		0x0000: "en",
		0x0409: "en",      // en-US
		0x0804: "zh-Hans", // zh-CN
		0x1004: "zh-Hans", // zh-SG
		0x0404: "zh-Hant", // zh-TW
		0x0C04: "zh-Hant", // zh-HK
		0x1404: "zh-Hant", // zh-MO
	}
	for id, want := range cases {
		if got := languageFromLANGID(id); got != want {
			t.Fatalf("languageFromLANGID(0x%04X) = %q, want %q", id, got, want)
		}
	}
}

func TestGetCurrentLanguageFallsBackToOSWhenUnset(t *testing.T) {
	app := &App{}
	got := app.GetCurrentLanguage()
	if got != "en" && got != "zh-Hans" && got != "zh-Hant" {
		t.Fatalf("GetCurrentLanguage() = %q, want en or zh-*", got)
	}
	app.CurrentLanguage = "zh-CN"
	if got := app.GetCurrentLanguage(); got != "zh-Hans" {
		t.Fatalf("GetCurrentLanguage() with CurrentLanguage zh-CN = %q, want zh-Hans", got)
	}
}
