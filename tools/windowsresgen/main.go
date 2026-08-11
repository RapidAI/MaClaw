// windowsresgen generates the single Windows resource object used by MaClaw.
//
// Wails loads group-icon resource 3, while Windows Explorer selects the first
// group icon and the Win32 application class can load IDI_APPLICATION (32512).
// Keeping all three in one COFF object prevents the linker from resolving
// duplicate groups from separately generated resource files unpredictably.
package main

import (
	"bytes"
	"crypto/sha256"
	"flag"
	"fmt"
	"os"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

const (
	wailsAppIconID = 3
	idiApplication = 32512
)

func main() {
	var (
		arch      = flag.String("arch", "", "target architecture: amd64 or arm64")
		icoPath   = flag.String("ico", "", "source .ico file")
		manifest  = flag.String("manifest", "", "application manifest XML")
		versionJS = flag.String("versioninfo", "", "version-info JSON")
		output    = flag.String("out", "", "generated COFF .syso output")
		verifyEXE = flag.String("verify", "", "verify icons in this built executable")
	)
	flag.Parse()

	if *verifyEXE != "" {
		must(verify(*verifyEXE, *icoPath))
		return
	}
	must(generate(*arch, *icoPath, *manifest, *versionJS, *output))
}

func generate(archName, icoPath, manifestPath, versionPath, output string) error {
	if icoPath == "" || manifestPath == "" || versionPath == "" || output == "" {
		return fmt.Errorf("-ico, -manifest, -versioninfo and -out are required")
	}
	arch, err := targetArch(archName)
	if err != nil {
		return err
	}

	icoFile, err := os.Open(icoPath)
	if err != nil {
		return fmt.Errorf("open icon: %w", err)
	}
	defer icoFile.Close()
	ico, err := winres.LoadICO(icoFile)
	if err != nil {
		return fmt.Errorf("load icon: %w", err)
	}

	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	appManifest, err := winres.AppManifestFromXML(manifestData)
	if err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	versionData, err := os.ReadFile(versionPath)
	if err != nil {
		return fmt.Errorf("read version info: %w", err)
	}
	var versionInfo version.Info
	if err := versionInfo.UnmarshalJSON(versionData); err != nil {
		return fmt.Errorf("parse version info: %w", err)
	}

	resources := winres.ResourceSet{}
	for _, resourceID := range []uint16{1, wailsAppIconID, idiApplication} {
		if err := resources.SetIcon(winres.ID(resourceID), ico); err != nil {
			return fmt.Errorf("set icon %d: %w", resourceID, err)
		}
	}
	resources.SetManifest(appManifest)
	resources.SetVersionInfo(versionInfo)

	out, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer out.Close()
	if err := resources.WriteObject(out, arch); err != nil {
		return fmt.Errorf("write resources: %w", err)
	}
	return nil
}

func verify(exePath, icoPath string) error {
	if icoPath == "" {
		return fmt.Errorf("-ico is required with -verify")
	}
	expectedFile, err := os.Open(icoPath)
	if err != nil {
		return fmt.Errorf("open expected icon: %w", err)
	}
	defer expectedFile.Close()
	expected, err := winres.LoadICO(expectedFile)
	if err != nil {
		return fmt.Errorf("load expected icon: %w", err)
	}
	expectedHash, err := iconHash(expected)
	if err != nil {
		return err
	}

	exe, err := os.Open(exePath)
	if err != nil {
		return fmt.Errorf("open executable: %w", err)
	}
	defer exe.Close()
	resources, err := winres.LoadFromEXE(exe)
	if err != nil {
		return fmt.Errorf("read executable resources: %w", err)
	}
	for _, resourceID := range []uint16{1, wailsAppIconID, idiApplication} {
		actual, err := resources.GetIcon(winres.ID(resourceID))
		if err != nil {
			return fmt.Errorf("missing icon group %d: %w", resourceID, err)
		}
		actualHash, err := iconHash(actual)
		if err != nil {
			return err
		}
		if actualHash != expectedHash {
			return fmt.Errorf("icon group %d does not match %s", resourceID, icoPath)
		}
	}
	fmt.Printf("verified Explorer, Wails (ID %d), and IDI_APPLICATION icon groups in %s\n", wailsAppIconID, exePath)
	return nil
}

func iconHash(icon *winres.Icon) ([sha256.Size]byte, error) {
	var saved bytes.Buffer
	if err := icon.SaveICO(&saved); err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("serialise icon: %w", err)
	}
	return sha256.Sum256(saved.Bytes()), nil
}

func targetArch(name string) (winres.Arch, error) {
	switch name {
	case "amd64":
		return winres.ArchAMD64, nil
	case "arm64":
		return winres.ArchARM64, nil
	default:
		return "", fmt.Errorf("unsupported -arch %q (expected amd64 or arm64)", name)
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "windowsresgen:", err)
		os.Exit(1)
	}
}
