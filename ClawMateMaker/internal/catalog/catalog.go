// Package catalog defines the official board and Release-asset allow-list.
package catalog

import (
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
}

var officialProfiles = []BoardProfile{
	{ID: "echoear-2st", FirmwareBoardID: "echoear-2st-r8", Name: "EchoEar 2ST", AssetName: "MaClaw-ESP32S3-EchoEar-2ST-firmware.clawfw", Chip: "ESP32-S3"},
	{ID: "bread-compact", FirmwareBoardID: "bread-compact-wifi-lcd-v1", Name: "Bread Compact", AssetName: "MaClaw-ESP32S3-Bread-Compact-firmware.clawfw", Chip: "ESP32-S3"},
	{ID: "fangtang-4g", FirmwareBoardID: "fangtang-4g-v1", Name: "Fangtang 4G", AssetName: "MaClaw-ESP32S3-Fangtang-4G-firmware.clawfw", Chip: "ESP32-S3"},
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
	if flashBytes != 16*1024*1024 {
		return fmt.Errorf("firmware flash capacity %d does not match supported 16 MiB profile", flashBytes)
	}
	return nil
}

type Recognition struct {
	Status          string   `json:"status"`
	Reason          string   `json:"reason"`
	CandidateBoards []string `json:"candidateBoards,omitempty"`
}

func RecognizeProbe(chip flash.ChipInfo, _ flash.FlashInfo) Recognition {
	observed := strings.ToUpper(chip.Chip + " " + strings.Join(chip.Features, " "))
	if !strings.Contains(observed, "ESP32-S3") && !strings.Contains(observed, "ESP32S3") {
		return Recognition{Status: "unsupported", Reason: "Only ESP32-S3 hardware is supported by the current official catalog."}
	}
	return Recognition{Status: "requires_confirmation", Reason: "ROM/USB data confirms ESP32-S3 but cannot distinguish EchoEar 2ST, Bread Compact, and Fangtang 4G. Confirm the board label, or use protocol:2 device identity before selecting firmware.", CandidateBoards: []string{"echoear-2st", "bread-compact", "fangtang-4g"}}
}

func RecognizeApplicationIdentity(identity string) Recognition {
	return recognizeApplicationIdentity(identity, 2)
}

func RecognizeApplicationIdentityEvidence(identity device.AppIdentity) Recognition {
	return recognizeApplicationIdentity(identity.FirmwareTargetBoardID, identity.Protocol)
}

func recognizeApplicationIdentity(identity string, protocol int) Recognition {
	for _, profile := range officialProfiles {
		if profile.FirmwareBoardID == identity {
			if protocol == 1 {
				return Recognition{Status: "probable", Reason: "The running nonce-bound legacy protocol:1 application reports this board target. It can automatically select the signed migration firmware, but is not physical manufacturing identity; user confirmation remains required before flashing.", CandidateBoards: []string{profile.ID}}
			}
			return Recognition{Status: "probable", Reason: "The running nonce-bound protocol:2 application reports this board target. This is useful automatic evidence but not a physical manufacturing identity, so user confirmation is still required before flashing.", CandidateBoards: []string{profile.ID}}
		}
	}
	return Recognition{Status: "requires_confirmation", Reason: "The application identity is unknown to the official board catalog."}
}
