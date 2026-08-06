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
	Asset           string `json:"asset"`
	BoardID         string `json:"boardId"`
	FirmwareBoardID string `json:"firmwareBoardId"`
	PackageID       string `json:"packageId"`
	ReleaseVersion  string `json:"releaseVersion"`
	ArchiveSHA256   string `json:"archiveSha256"`
	ManifestSHA256  string `json:"manifestSha256"`
	VerifiedAt      string `json:"verifiedAt"`
}

func main() {
	var pathname, boardID, keyID, publicKey, output string
	flag.StringVar(&pathname, "input", "", "signed .clawfw package")
	flag.StringVar(&boardID, "board", "", "official catalog board ID")
	flag.StringVar(&keyID, "key-id", "", "trusted Ed25519 release key ID")
	flag.StringVar(&publicKey, "public-key-base64", os.Getenv("CLAWMATE_FIRMWARE_PUBLIC_KEY"), "Ed25519 public key, base64")
	flag.StringVar(&output, "evidence", "", "optional JSON evidence output")
	flag.Parse()
	result, err := verify(pathname, boardID, keyID, publicKey)
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
	return evidence{Asset: profile.AssetName, BoardID: profile.ID, FirmwareBoardID: verified.Manifest.Board.ID, PackageID: verified.Manifest.PackageID, ReleaseVersion: verified.Manifest.ReleaseVersion, ArchiveSHA256: verified.ArchiveSHA256, ManifestSHA256: verified.ManifestSHA256, VerifiedAt: time.Now().UTC().Format(time.RFC3339)}, nil
}
