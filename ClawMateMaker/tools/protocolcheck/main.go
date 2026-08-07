// protocolcheck verifies the firmware-to-desktop identity transport contract
// before a Release package is published. It deliberately checks source and
// generated sdkconfig headers rather than attempting to infer these guarantees
// from a merged binary after the fact.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var protocolRe = regexp.MustCompile(`(?m)^\s*#define\s+IDENTITY_PROTOCOL_VERSION\s+([0-9]+)\s*$`)

func main() {
	firmwareSource := flag.String("firmware-source", "", "path to firmware_identity.c")
	sdkconfigHeader := flag.String("sdkconfig-header", "", "path to generated sdkconfig.h")
	wantProtocol := flag.Int("protocol", 2, "required identity protocol version")
	flag.Parse()
	if *firmwareSource == "" || *sdkconfigHeader == "" || *wantProtocol <= 0 {
		fail("--firmware-source, --sdkconfig-header and a positive --protocol are required")
	}
	source, err := os.ReadFile(*firmwareSource)
	if err != nil {
		fail("read firmware source: %v", err)
	}
	match := protocolRe.FindStringSubmatch(string(source))
	if len(match) != 2 || match[1] != fmt.Sprint(*wantProtocol) {
		fail("firmware identity protocol is not %d", *wantProtocol)
	}
	for _, field := range []string{
		`"firmware_target_board_id"`,
		`"layout_id"`,
		`"release_sequence"`,
		`"firmware_version"`,
		`"project_name"`,
		`"app_version"`,
		`"app_elf_sha256"`,
		`"chip"`,
		`"flash_size_bytes"`,
		`"psram_size_bytes"`,
		`"self_test"`,
		`"ready"`,
	} {
		if !strings.Contains(string(source), field) {
			fail("firmware identity source is missing required protocol-v2 field %s", field)
		}
	}
	if !strings.Contains(string(source), `esp_app_get_elf_sha256`) {
		fail("firmware identity source does not derive app_elf_sha256 from ESP-IDF app metadata")
	}
	if !strings.Contains(string(source), `strcmp(type, "SERVICE_STATUS")`) {
		fail("firmware identity source does not keep local BOOT_STATUS readiness separate from SERVICE_STATUS")
	}
	if !strings.Contains(string(source), `"local_ready"`) || !strings.Contains(string(source), `"flash"`) || !strings.Contains(string(source), `"psram"`) {
		fail("firmware identity source is missing required local self-test evidence")
	}
	if !strings.Contains(string(source), `strcmp(type, "IDENTIFY")`) || !strings.Contains(string(source), `strcmp(type, "BOOT_STATUS")`) {
		fail("firmware identity source is missing query-bound IDENTIFY or BOOT_STATUS handling")
	}
	if !strings.Contains(string(source), `"firmware_version", CONFIG_MACLAW_RELEASE_SEQUENCE`) {
		fail("firmware_version is not bound to CONFIG_MACLAW_RELEASE_SEQUENCE")
	}
	if err := requireSDKConfig(*sdkconfigHeader, "CONFIG_ESP_CONSOLE_SECONDARY_USB_SERIAL_JTAG"); err != nil {
		fail("identity transport unavailable: %v", err)
	}
	if err := requireSDKConfig(*sdkconfigHeader, "CONFIG_ESP_CONSOLE_USB_SERIAL_JTAG_ENABLED"); err != nil {
		fail("identity transport unavailable: %v", err)
	}
	fmt.Printf("protocol contract verified: protocol=%d firmware=%s sdkconfig=%s\n", *wantProtocol, filepath.Base(*firmwareSource), filepath.Base(*sdkconfigHeader))
}

func requireSDKConfig(path, name string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	want := "#define " + name + " 1"
	s := bufio.NewScanner(f)
	for s.Scan() {
		if strings.TrimSpace(s.Text()) == want {
			return nil
		}
	}
	if err := s.Err(); err != nil {
		return err
	}
	return fmt.Errorf("%s is not enabled", name)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "protocolcheck: "+format+"\n", args...)
	os.Exit(1)
}
