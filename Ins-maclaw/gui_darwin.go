//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

func guiChooseBrand(defaultBrand brandOption) (brandOption, bool) {
	labels := make([]string, 0, len(brandOptions))
	defaultLabel := brandLabel(brandOptions[0])
	for _, brand := range brandOptions {
		label := brandLabel(brand)
		labels = append(labels, fmt.Sprintf("%q", label))
		if brand.ID == defaultBrand.ID {
			defaultLabel = label
		}
	}
	script := fmt.Sprintf(
		`choose from list {%s} with prompt %q default items {%q} with title %q`,
		strings.Join(labels, ", "), tr("choose.brand"), defaultLabel, tr("app.title"),
	)
	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	if err != nil {
		return brandOption{}, false
	}
	selected := strings.TrimSpace(string(out))
	if selected == "" || selected == "false" {
		return brandOption{}, false
	}
	for _, brand := range brandOptions {
		if selected == brandLabel(brand) || strings.Contains(selected, brand.ProductName) {
			return brand, true
		}
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
