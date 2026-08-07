// fwverify is a CI/release-gate verifier for the exact .clawfw asset that
// will be published to GitHub Release. It proves that the generated archive
// is readable by a production desktop trust root and bound to the intended
// local board catalog entry before upload; end-user applications never call
// this command.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"clawmatemaker/internal/catalog"
	"clawmatemaker/internal/firmware"
)

type evidence struct {
	Asset           string   `json:"asset"`
	BoardID         string   `json:"boardId"`
	FirmwareBoardID string   `json:"firmwareBoardId"`
	PackageID       string   `json:"packageId"`
	ReleaseVersion  string   `json:"releaseVersion"`
	Channel         string   `json:"channel"`
	ArchiveSHA256   string   `json:"archiveSha256"`
	ManifestSHA256  string   `json:"manifestSha256"`
	InstallMode     string   `json:"installMode"`
	WriteOrder      []string `json:"writeOrder,omitempty"`
	ImageCount      int      `json:"imageCount"`
	VerifiedAt      string   `json:"verifiedAt"`
}

func main() {
	var pathname, boardID, keyID, publicKey, output string
	var requireSplit bool
	flag.StringVar(&pathname, "input", "", "signed .clawfw package")
	flag.StringVar(&boardID, "board", "", "official catalog board ID")
	flag.StringVar(&keyID, "key-id", "", "trusted Ed25519 release key ID")
	flag.StringVar(&publicKey, "public-key-base64", os.Getenv("CLAWMATE_FIRMWARE_PUBLIC_KEY"), "Ed25519 public key, base64")
	flag.StringVar(&output, "evidence", "", "optional JSON evidence output")
	flag.BoolVar(&requireSplit, "require-split-full", false, "reject legacy merged full packages; required for newly built official releases")
	flag.Parse()
	result, err := verifyWithPolicy(pathname, boardID, keyID, publicKey, requireSplit)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fwverify:", err)
		os.Exit(1)
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "fwverify:", err)
		os.Exit(1)
	}
	encoded = append(encoded, '\n')
	if output != "" {
		if err := os.WriteFile(output, encoded, 0600); err != nil {
			fmt.Fprintln(os.Stderr, "fwverify:", err)
			os.Exit(1)
		}
	}
	_, _ = os.Stdout.Write(encoded)
}

func verify(pathname, boardID, keyID, publicKey string) (evidence, error) {
	return verifyWithPolicy(pathname, boardID, keyID, publicKey, false)
}

func verifyWithPolicy(pathname, boardID, keyID, publicKey string, requireSplit bool) (evidence, error) {
	if pathname == "" || boardID == "" || keyID == "" || publicKey == "" {
		return evidence{}, fmt.Errorf("input, board, key-id and public key are required")
	}
	profile, err := catalog.Profile(boardID)
	if err != nil {
		return evidence{}, err
	}
	rawKey, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil || len(rawKey) != ed25519.PublicKeySize {
		return evidence{}, fmt.Errorf("invalid Ed25519 public key")
	}
	verified, err := firmware.VerifyRelease(pathname, firmware.TrustStore{keyID: ed25519.PublicKey(rawKey)})
	if err != nil {
		return evidence{}, err
	}
	if err := catalog.ValidateManifestBinding(profile, verified.Manifest.Board.ID, verified.Manifest.Board.ProfileHash, verified.Manifest.Chip.Family, verified.Manifest.Chip.FlashBytes); err != nil {
		return evidence{}, err
	}
	plan, err := firmware.InstallPlanFor(verified.Manifest)
	if err != nil {
		return evidence{}, err
	}
	images := 0
	for _, file := range verified.Manifest.Files {
		if file.Region != "metadata" {
			images++
		}
	}
	if requireSplit && plan.Mode == firmware.ModeFull && (len(verified.Manifest.WriteOrder) == 0 || images < 3) {
		return evidence{}, fmt.Errorf("official full release must use a split signed writeOrder package")
	}
	return evidence{Asset: profile.AssetName, BoardID: profile.ID, FirmwareBoardID: verified.Manifest.Board.ID, PackageID: verified.Manifest.PackageID, ReleaseVersion: verified.Manifest.ReleaseVersion, Channel: verified.Manifest.Channel, ArchiveSHA256: verified.ArchiveSHA256, ManifestSHA256: verified.ManifestSHA256, InstallMode: plan.Mode, WriteOrder: append([]string(nil), verified.Manifest.WriteOrder...), ImageCount: images, VerifiedAt: time.Now().UTC().Format(time.RFC3339)}, nil
}
