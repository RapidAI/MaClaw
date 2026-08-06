package verify

import (
	"strings"
	"testing"
)

func expectation() Expectation {
	return Expectation{BoardID: "bread-v1", LayoutID: "layout-v2", ReleaseSequence: 3, ProjectName: "app", AppVersion: "1.2.3", AppELFSHA256: "sha", Chip: "esp32s3", FlashBytes: 16777216, PSRAMBytes: 8388608, RequiredSelfTests: []string{"flash", "psram"}}
}
func valid(nonce string) string {
	return `CLAWMATE_EVT {"type":"BOOT_STATUS","protocol":2,"nonce":"` + nonce + `","ready":true,"firmware_target_board_id":"bread-v1","layout_id":"layout-v2","release_sequence":3,"project_name":"app","app_version":"1.2.3","app_elf_sha256":"sha","chip":"esp32s3","flash_size_bytes":16777216,"psram_size_bytes":8388608,"self_test":{"flash":"ok","psram":"ok"}}`
}
func TestParseFrameAcceptsMatchingProtocolV2(t *testing.T) {
	s, err := ParseFrame(valid("n"), "n", expectation())
	if err != nil || !s.Ready {
		t.Fatalf("status=%+v err=%v", s, err)
	}
}
func TestParseFrameRejectsLegacyNonceAndDuplicates(t *testing.T) {
	if _, err := ParseFrame(strings.Replace(valid("n"), `"protocol":2`, `"protocol":1`, 1), "n", expectation()); err == nil {
		t.Fatal("legacy protocol passed")
	}
	duplicate := strings.Replace(valid("n"), `"ready":true`, `"ready":true,"ready":true`, 1)
	if _, err := ParseFrame(duplicate, "n", expectation()); err == nil {
		t.Fatal("duplicate key passed")
	}
	if _, err := ParseFrame(valid("old"), "new", expectation()); err == nil {
		t.Fatal("stale nonce passed")
	}
}
func TestParseFrameAllowsLogPrefixButRejectsTrailingData(t *testing.T) {
	if _, err := ParseFrame("I (200) boot: init"+valid("n"), "n", expectation()); err != nil {
		t.Fatalf("log-prefixed frame rejected: %v", err)
	}
	if _, err := ParseFrame(valid("n")+" trailing", "n", expectation()); err == nil {
		t.Fatal("trailing data accepted")
	}
}
