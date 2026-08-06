package device

import "testing"

func TestParseIdentityRequiresProtocolV2AndNonce(t *testing.T) {
	line := `CLAWMATE_EVT {"type":"IDENTITY","protocol":2,"nonce":"n","firmware_target_board_id":"bread-compact-wifi-lcd-v1","layout_id":"layout","project_name":"app","app_version":"1","chip":"esp32s3","flash_size_bytes":16777216,"psram_size_bytes":8388608}`
	identity, err := parseIdentityFrame(line, "n")
	if err != nil || identity.FirmwareTargetBoardID != "bread-compact-wifi-lcd-v1" {
		t.Fatalf("identity=%+v err=%v", identity, err)
	}
	if _, err := parseIdentityFrame(line, "other"); err == nil {
		t.Fatal("stale nonce accepted")
	}
	if _, err := parseIdentityFrame(line[:len(line)-1]+`,"protocol":1}`, "n"); err == nil {
		t.Fatal("duplicate protocol accepted")
	}
}

func TestParseIdentityAllowsLogPrefixButNotTrailingData(t *testing.T) {
	line := `I (431) esp_image: segment completeCLAWMATE_EVT {"type":"IDENTITY","protocol":2,"nonce":"n","firmware_target_board_id":"bread-compact-wifi-lcd-v1"}`
	if _, err := parseIdentityFrame(line, "n"); err != nil {
		t.Fatalf("log-prefixed event rejected: %v", err)
	}
	if _, err := parseIdentityFrame(line+` noise`, "n"); err == nil {
		t.Fatal("trailing data accepted")
	}
}

func TestParseLegacyIdentityProvidesMigrationEvidence(t *testing.T) {
	line := `CLAWMATE_EVT {"type":"IDENTITY","protocol":1,"nonce":"n","board_id":"fangtang-4g-v1","layout_id":"layout","project_name":"app","firmware_version":"legacy","chip":"esp32s3","flash_size_bytes":16777216,"psram_size_bytes":8388608}`
	identity, err := parseIdentityFrame(line, "n")
	if err != nil || identity.Protocol != 1 || identity.FirmwareTargetBoardID != "fangtang-4g-v1" || identity.AppVersion != "legacy" {
		t.Fatalf("identity=%+v err=%v", identity, err)
	}
}
