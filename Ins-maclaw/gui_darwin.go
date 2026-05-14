//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

func guiChooseBrand(defaultBrand brandOption) (brandOption, bool) {
	defaultButton := brandLabel(brandOptions[0])
	if defaultBrand.ID == brandOptions[1].ID {
		defaultButton = brandLabel(brandOptions[1])
	}
	script := fmt.Sprintf(`display dialog %q buttons {"%s", "%s", "%s"} default button "%s" with title %q`, tr("choose.brand"), tr("cancel"), brandLabel(brandOptions[1]), brandLabel(brandOptions[0]), defaultButton, tr("app.title"))
	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	if err != nil {
		return brandOption{}, false
	}
	if strings.Contains(string(out), "TigerClaw") {
		return brandOptions[1], true
	}
	return brandOptions[0], true
}

func guiConfirm(title, message string) bool {
	script := fmt.Sprintf(`display dialog %q buttons {"%s", "OK"} default button "OK" with title %q`, message, tr("cancel"), title)
	return exec.Command("osascript", "-e", script).Run() == nil
}

func guiStatus(title, message string) {
	script := fmt.Sprintf(`display dialog %q buttons {"OK"} default button "OK" with title %q`, message, title)
	_ = exec.Command("osascript", "-e", script).Run()
}

func guiError(message string) {
	script := fmt.Sprintf(`display dialog %q buttons {"OK"} default button "OK" with icon stop with title %q`, message, tr("app.title"))
	_ = exec.Command("osascript", "-e", script).Run()
}
