package main

import (
	"context"
	"fmt"
	"strings"
)

func runGUI(defaultBrand brandOption, currentVersion string, checkOnly, noLaunch bool) error {
	if handled, err := guiRunInstallWizard(defaultBrand, currentVersion, checkOnly, noLaunch); handled {
		return err
	}

	brand, ok := guiChooseBrand(defaultBrand)
	if !ok {
		return nil
	}
	if !checkOnly && !guiConfirm("Ins-maclaw", fmt.Sprintf(tr("confirm.install"), brandLabel(brand))) {
		return nil
	}
	guiStatus("Ins-maclaw", fmt.Sprintf(tr("status.checking"), brandLabel(brand)))

	logs := []string{}
	result, err := runInstall(context.Background(), installOptions{
		Brand:          brand,
		CurrentVersion: currentVersion,
		CheckOnly:      checkOnly,
		NoLaunch:       noLaunch,
		Log: func(msg string) {
			logs = append(logs, msg)
		},
	})
	if err != nil {
		guiError(err.Error())
		return err
	}
	message := guiResultMessage(result, checkOnly, noLaunch)
	if message == "" {
		message = strings.Join(logs, "\n")
	}
	guiStatus("Ins-maclaw", message)
	return nil
}

func guiResultMessage(result installResult, checkOnly, noLaunch bool) string {
	latest := displayVersion(result.Release.TagName)
	if result.Skipped {
		return fmt.Sprintf(tr("result.uptodate"), latest, result.TargetFileName)
	}
	if checkOnly {
		return fmt.Sprintf(tr("result.check"), latest, result.TargetFileName, result.Release.Source)
	}
	if noLaunch {
		return fmt.Sprintf(tr("result.downloaded"), latest, result.DownloadedPath)
	}
	return fmt.Sprintf(tr("result.launched"), latest, result.DownloadedPath)
}
