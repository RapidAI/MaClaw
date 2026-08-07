// sidecarverify is a CI-only proof that a packaged production executable and
// its adjacent esptool helper share the embedded release trust root.  It never
// opens a serial port or invokes the helper.
package main

import (
	"bytes"
	"fmt"
	"os"

	"clawmatemaker/internal/flash"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: sidecarverify <desktop-executable>")
		os.Exit(2)
	}
	keyID := os.Getenv("CLAWMATE_FIRMWARE_SIGNING_KEY_ID")
	publicKey := os.Getenv("CLAWMATE_FIRMWARE_PUBLIC_KEY")
	if keyID == "" || publicKey == "" {
		fmt.Fprintln(os.Stderr, "sidecarverify: release public key and key ID are required")
		os.Exit(2)
	}
	if err := verifyEmbeddedTrustMetadata(os.Args[1], keyID, publicKey); err != nil {
		fmt.Fprintln(os.Stderr, "sidecarverify:", err)
		os.Exit(1)
	}
	flash.ConfigureSidecar(os.Args[1], true)
	flash.ConfigureSidecarTrust(keyID, publicKey)
	tool, err := flash.FindTool()
	if err != nil {
		fmt.Fprintln(os.Stderr, "sidecarverify:", err)
		os.Exit(1)
	}
	fmt.Printf("managed sidecar verified: %s (%s)\n", tool.Path, tool.Version)
}

// verifyEmbeddedTrustMetadata makes the CI proof cover both ends of the
// release contract. A valid sidecar signed by the environment's key is not
// enough if the already-built desktop binary was linked with a different key
// (or as a developer build). Go's -X linker values are retained in the binary
// string table, so checking their exact byte sequence is a portable, offline
// release gate; normal runtime signature validation remains authoritative.
func verifyEmbeddedTrustMetadata(executable, keyID, publicKey string) error {
	contents, err := os.ReadFile(executable)
	if err != nil {
		return fmt.Errorf("read desktop executable: %w", err)
	}
	if len(contents) == 0 {
		return fmt.Errorf("desktop executable is empty")
	}
	if !bytes.Contains(contents, []byte(keyID)) {
		return fmt.Errorf("desktop executable does not embed expected release key ID")
	}
	if !bytes.Contains(contents, []byte(publicKey)) {
		return fmt.Errorf("desktop executable does not embed expected release public key")
	}
	return nil
}
