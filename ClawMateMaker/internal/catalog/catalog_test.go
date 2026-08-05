package catalog

import (
	"clawmatemaker/internal/flash"
	"testing"
)

func TestOfficialProfileAssetNames(t *testing.T) {
	want := map[string]string{"echoear-2st": "MaClaw-ESP32S3-EchoEar-2ST-firmware.zip", "bread-compact": "MaClaw-ESP32S3-Bread-Compact-firmware.zip", "fangtang-4g": "MaClaw-ESP32S3-Fangtang-4G-firmware.zip"}
	for _, p := range Profiles() {
		if want[p.ID] != p.AssetName {
			t.Fatalf("%s asset = %q", p.ID, p.AssetName)
		}
		delete(want, p.ID)
	}
	if len(want) != 0 {
		t.Fatalf("profiles missing: %#v", want)
	}
}
func TestProbeRecognitionDoesNotGuessBoard(t *testing.T) {
	r := RecognizeProbe(flash.ChipInfo{Chip: "Chip is ESP32-S3"}, flash.FlashInfo{})
	if r.Status != "requires_confirmation" || len(r.CandidateBoards) != 3 {
		t.Fatalf("unsafe recognition: %#v", r)
	}
}
func TestGitHubReleaseURLAllowList(t *testing.T) {
	for _, raw := range []string{"https://github.com/RapidAI/CodeClaw/releases/download/v1/a.zip", "https://objects.githubusercontent.com/a"} {
		if !isGitHubReleaseURL(raw) {
			t.Fatalf("expected allowed: %s", raw)
		}
	}
	for _, raw := range []string{"http://github.com/a", "https://evil.example/a", "https://github.com.evil.example/a"} {
		if isGitHubReleaseURL(raw) {
			t.Fatalf("expected rejected: %s", raw)
		}
	}
}
