// Package catalog defines the official board and Release-asset allow-list.
package catalog

import (
	"errors"
	"fmt"
	"strings"

	"clawmatemaker/internal/device"
	"clawmatemaker/internal/flash"
)

// Repository is the canonical GitHub repository that owns Release assets.
const Repository = "RapidAI/MaClaw"

type BoardProfile struct {
	ID              string `json:"id"`
	FirmwareBoardID string `json:"firmwareBoardId"`
	Name            string `json:"name"`
	AssetName       string `json:"assetName"`
	Chip            string `json:"chip"`
	FlashBytes      int64  `json:"flashBytes"`
}

var officialProfiles = []BoardProfile{
	{ID: "echoear-2st", FirmwareBoardID: "echoear-2st-r8", Name: "EchoEar 2ST", AssetName: "MaClaw-ESP32S3-EchoEar-2ST-firmware.clawfw", Chip: "ESP32-S3", FlashBytes: 16 * 1024 * 1024},
	{ID: "bread-compact", FirmwareBoardID: "bread-compact-wifi-lcd-v1", Name: "Bread Compact", AssetName: "MaClaw-ESP32S3-Bread-Compact-firmware.clawfw", Chip: "ESP32-S3", FlashBytes: 16 * 1024 * 1024},
	{ID: "fangtang-4g", FirmwareBoardID: "fangtang-4g-v1", Name: "Fangtang 4G", AssetName: "MaClaw-ESP32S3-Fangtang-4G-firmware.clawfw", Chip: "ESP32-S3", FlashBytes: 16 * 1024 * 1024},
	{ID: "waveshare-amoled-1.75c", FirmwareBoardID: "waveshare-s3-touch-amoled-1.75c-v1", Name: "Waveshare S3 Touch AMOLED 1.75C", AssetName: "MaClaw-ESP32S3-Waveshare-AMOLED-1.75C-firmware.clawfw", Chip: "ESP32-S3", FlashBytes: 32 * 1024 * 1024},
}

func Profiles() []BoardProfile {
	result := make([]BoardProfile, len(officialProfiles))
	copy(result, officialProfiles)
	return result
}

func Profile(id string) (BoardProfile, error) {
	for _, profile := range officialProfiles {
		if profile.ID == id {
			return profile, nil
		}
	}
	return BoardProfile{}, fmt.Errorf("unsupported board profile: %q", id)
}

func ProfileForFirmwareBoardID(firmwareBoardID string) (BoardProfile, error) {
	for _, profile := range officialProfiles {
		if strings.EqualFold(profile.FirmwareBoardID, firmwareBoardID) {
			return profile, nil
		}
	}
	return BoardProfile{}, fmt.Errorf("unsupported firmware board target: %q", firmwareBoardID)
}

func ValidateManifestBinding(profile BoardProfile, firmwareBoardID, profileHash, chipFamily string, flashBytes int64) error {
	if profile.ID == "" || profile.FirmwareBoardID == "" {
		return fmt.Errorf("invalid board profile")
	}
	if !strings.EqualFold(firmwareBoardID, profile.FirmwareBoardID) {
		return fmt.Errorf("firmware board %q does not match selected board %q", firmwareBoardID, profile.FirmwareBoardID)
	}
	if profileHash != "catalog:"+profile.ID {
		return fmt.Errorf("firmware profile binding %q does not match selected board %q", profileHash, profile.ID)
	}
	if !strings.EqualFold(chipFamily, "esp32s3") {
		return fmt.Errorf("firmware chip family %q is not ESP32-S3", chipFamily)
	}
	if profile.FlashBytes <= 0 || flashBytes != profile.FlashBytes {
		return fmt.Errorf("firmware flash capacity %d does not match supported %d-byte profile", flashBytes, profile.FlashBytes)
	}
	return nil
}

// ValidateProfileROMBinding applies the catalog's hardware boundary to an
// independent ROM probe before an operator confirmation is minted. This is
// intentionally separate from application identity: a user may distinguish
// the three 16 MiB products by their physical label, but no confirmation may
// select a profile whose chip family or exact flash capacity contradicts ROM.
func ValidateProfileROMBinding(profile BoardProfile, chip flash.ChipInfo, observedFlash flash.FlashInfo) error {
	if profile.ID == "" || profile.FlashBytes <= 0 {
		return errors.New("invalid board profile")
	}
	if !isESP32S3ROM(chip.Chip + " " + strings.Join(chip.Features, " ")) {
		return fmt.Errorf("ROM chip %q is not ESP32-S3", chip.Chip)
	}
	if observedFlash.SizeBytes != profile.FlashBytes {
		return fmt.Errorf("ROM flash capacity %d does not match %s profile capacity %d", observedFlash.SizeBytes, profile.Name, profile.FlashBytes)
	}
	return nil
}

type Recognition struct {
	Status          string   `json:"status"`
	Reason          string   `json:"reason"`
	CandidateBoards []string `json:"candidateBoards,omitempty"`
}

func RecognizeProbe(chip flash.ChipInfo, observedFlash flash.FlashInfo) Recognition {
	observed := strings.ToUpper(chip.Chip + " " + strings.Join(chip.Features, " "))
	if !strings.Contains(observed, "ESP32-S3") && !strings.Contains(observed, "ESP32S3") {
		return Recognition{Status: "unsupported", Reason: "Only ESP32-S3 hardware is supported by the current official catalog."}
	}
	if observedFlash.SizeBytes == 32*1024*1024 {
		return Recognition{Status: "probable", Reason: "ROM data confirms ESP32-S3 with 32 MiB Flash. Waveshare S3 Touch AMOLED 1.75C is the only official 32 MiB profile; confirm the physical board label before flashing.", CandidateBoards: []string{"waveshare-amoled-1.75c"}}
	}
	// Flash capacity is a hard compatibility boundary. Never present the 32 MiB
	// Waveshare package to a 16 MiB device merely because every product uses
	// ESP32-S3. The three 16 MiB products still require protocol identity or a
	// physical label to disambiguate them.
	if observedFlash.SizeBytes == 16*1024*1024 {
		return Recognition{Status: "requires_confirmation", Reason: "ROM/USB data confirms ESP32-S3 with 16 MiB Flash but cannot distinguish EchoEar 2ST, Bread Compact, and Fangtang 4G. Confirm the board label, or use protocol:2 device identity before selecting firmware.", CandidateBoards: []string{"echoear-2st", "bread-compact", "fangtang-4g"}}
	}
	return Recognition{Status: "requires_confirmation", Reason: "ROM/USB data confirms ESP32-S3, but the observed Flash capacity does not match an official profile. Use protocol:2 device identity and confirm the physical board label before selecting firmware.", CandidateBoards: []string{"echoear-2st", "bread-compact", "fangtang-4g", "waveshare-amoled-1.75c"}}
}

func RecognizeApplicationIdentity(identity string) Recognition {
	return recognizeApplicationIdentity(identity, 2)
}

func RecognizeApplicationIdentityEvidence(identity device.AppIdentity) Recognition {
	// The desktop write preflight and post-flash boot verification both require
	// the formal protocol-v2 contract.  Do not let an older diagnostic frame
	// produce a "probable" result here: doing so would download a package and
	// present a ready-to-flash UI only for the irreversible-write boundary to
	// reject the same device later.  Protocol:1 remains parseable for logs and
	// support diagnostics, but is not an automatic matching authority.
	if identity.Protocol == 1 {
		return Recognition{Status: "requires_confirmation", Reason: "This device reports legacy protocol:1 identity. Automatic firmware matching is unavailable until the device runs protocol:2 firmware; no firmware package was selected."}
	}
	if identity.Protocol != device.ProtocolVersion {
		return Recognition{Status: "requires_confirmation", Reason: "This device did not report a supported nonce-bound protocol:2 application identity. Automatic firmware matching is blocked; no firmware package was selected."}
	}
	recognition := recognizeApplicationIdentity(identity.FirmwareTargetBoardID, identity.Protocol)
	if recognition.Status != "probable" || len(recognition.CandidateBoards) != 1 {
		return recognition
	}
	profile, err := Profile(recognition.CandidateBoards[0])
	if err != nil {
		return Recognition{Status: "requires_confirmation", Reason: "The application identity does not map to a supported board profile."}
	}
	// Protocol:2 must report chip and capacity evidence that agrees with the
	// declared product profile before it may select any firmware.
	if !isESP32S3(identity.Chip) {
		return Recognition{Status: "requires_confirmation", Reason: "The application identity reports an unsupported chip family; automatic firmware matching is blocked."}
	}
	if identity.FlashBytes != profile.FlashBytes {
		return Recognition{Status: "requires_confirmation", Reason: "The application identity flash capacity does not match its declared official board profile; automatic firmware matching is blocked."}
	}
	return recognition
}

// RecognizeApplicationIdentityWithROM accepts runtime identity only when its
// claimed product profile agrees with independent, read-only ROM evidence.
// Application identity is useful for distinguishing the 16 MiB products, but
// it is not authority to override the flash chip that esptool actually read.
func RecognizeApplicationIdentityWithROM(identity device.AppIdentity, romChip flash.ChipInfo, romFlash flash.FlashInfo) Recognition {
	recognition := RecognizeApplicationIdentityEvidence(identity)
	if recognition.Status != "probable" || len(recognition.CandidateBoards) != 1 {
		return recognition
	}
	profile, err := Profile(recognition.CandidateBoards[0])
	if err != nil {
		return Recognition{Status: "requires_confirmation", Reason: "The application identity does not map to a supported board profile."}
	}
	romObserved := romChip.Chip + " " + strings.Join(romChip.Features, " ")
	if !isESP32S3ROM(romObserved) {
		return Recognition{Status: "requires_confirmation", Reason: "ROM evidence does not confirm ESP32-S3; automatic firmware matching is blocked."}
	}
	if romFlash.SizeBytes != profile.FlashBytes {
		return Recognition{Status: "requires_confirmation", Reason: "ROM flash capacity does not match the application identity's official board profile; automatic firmware matching is blocked."}
	}
	return recognition
}

func isESP32S3(chip string) bool {
	normalized := strings.ToLower(strings.TrimSpace(chip))
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, " ", "")
	return normalized == "esp32s3"
}

func isESP32S3ROM(chip string) bool {
	normalized := strings.ToLower(chip)
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, " ", "")
	return strings.Contains(normalized, "esp32s3")
}

func recognizeApplicationIdentity(identity string, protocol int) Recognition {
	for _, profile := range officialProfiles {
		if profile.FirmwareBoardID == identity {
			return Recognition{Status: "probable", Reason: "The running nonce-bound protocol:2 application reports this board target. This is useful automatic evidence but not a physical manufacturing identity, so user confirmation is still required before flashing.", CandidateBoards: []string{profile.ID}}
		}
	}
	return Recognition{Status: "requires_confirmation", Reason: "The application identity is unknown to the official board catalog."}
}
