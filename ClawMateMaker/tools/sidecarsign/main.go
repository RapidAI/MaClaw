// sidecarsign creates the signed helper manifest required by official
// ClawMate Maker builds. It runs only in the protected release workflow.
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type record struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Version string `json:"version"`
}

type signature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyId"`
	Signature string `json:"signature"`
}

type manifest struct {
	SchemaVersion int       `json:"schemaVersion"`
	Tools         []record  `json:"tools"`
	Signature     signature `json:"signature"`
}

type unsignedManifest struct {
	SchemaVersion int      `json:"schemaVersion"`
	Tools         []record `json:"tools"`
}

func main() {
	dir := flag.String("dir", "", "directory containing the sidecar")
	binary := flag.String("binary", "", "sidecar filename")
	version := flag.String("version", "", "sidecar version")
	keyID := flag.String("key-id", "", "Ed25519 release signing key ID")
	privateKey := flag.String("private-key-base64", os.Getenv("CLAWMATE_FIRMWARE_SIGNING_KEY"), "Ed25519 private key")
	flag.Parse()
	if err := run(*dir, *binary, *version, *keyID, *privateKey); err != nil {
		fmt.Fprintln(os.Stderr, "sidecarsign:", err)
		os.Exit(1)
	}
}

func run(dir, binary, version, keyID, privateKey string) error {
	if dir == "" || binary == "" || version == "" || keyID == "" || privateKey == "" {
		return errors.New("dir, binary, version, key-id and private key are required")
	}
	if filepath.Base(binary) != binary || strings.ContainsAny(binary, `\\/`) {
		return errors.New("sidecar binary must be a direct filename")
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	path := filepath.Join(root, binary)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return errors.New("sidecar binary is unavailable")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(contents)
	unsigned := unsignedManifest{SchemaVersion: 1, Tools: []record{{Name: "esptool", Path: binary, SHA256: "sha256:" + hex.EncodeToString(sum[:]), Version: version}}}
	payload, err := json.Marshal(unsigned)
	if err != nil {
		return err
	}
	rawKey, err := base64.StdEncoding.DecodeString(privateKey)
	if err != nil || len(rawKey) != ed25519.PrivateKeySize {
		return errors.New("invalid Ed25519 private key")
	}
	signed := manifest{SchemaVersion: unsigned.SchemaVersion, Tools: unsigned.Tools, Signature: signature{Algorithm: "ed25519", KeyID: keyID, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.PrivateKey(rawKey), payload))}}
	raw, err := json.Marshal(signed)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "sidecar-manifest.json"), append(raw, '\n'), 0600)
}
