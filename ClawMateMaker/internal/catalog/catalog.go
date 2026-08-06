// Package catalog defines the official board and Release-asset allow-list.
// It deliberately does not decide that an ESP32-S3 USB port identifies a
// particular product: that would make a wrong-board flash look safe.
package catalog

import (
	"clawmatemaker/internal/flash"
	"fmt"
	"strings"
)

const Repository = "RapidAI/CodeClaw"

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

// Recognition is intentionally evidence-based and fail-closed. ROM exposes
// chip/flash information, but not the product SKU for these three boards.
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
