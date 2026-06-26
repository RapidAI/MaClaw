//go:build linux

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

func guiChooseBrand(defaultBrand brandOption) (brandOption, bool) {
	if _, err := exec.LookPath("zenity"); err == nil {
		args := []string{
			"--list", "--radiolist",
			"--title=" + tr("app.title"), "--text=" + tr("choose.brand"),
			"--column=", "--column=Brand",
		}
		for _, brand := range brandOptions {
			selected := "FALSE"
			if brand.ID == defaultBrand.ID {
				selected = "TRUE"
			}
			args = append(args, selected, brandLabel(brand))
		}
		out, err := exec.Command("zenity", args...).Output()
		if err != nil {
			return brandOption{}, false
		}
		selected := strings.TrimSpace(string(out))
		for _, brand := range brandOptions {
			if selected == brandLabel(brand) || strings.Contains(selected, brand.ProductName) {
				return brand, true
			}
		}
		return brandOptions[0], true
	}
	return runTerminalBrandFallback(defaultBrand)
}

func guiConfirm(title, message string) bool {
	if _, err := exec.LookPath("zenity"); err == nil {
		return exec.Command("zenity", "--question", "--title="+title, "--text="+message).Run() == nil
	}
	fmt.Println(message)
	return true
}

func guiStatus(title, message string) {
	if _, err := exec.LookPath("zenity"); err == nil {
		_ = exec.Command("zenity", "--info", "--title="+title, "--text="+message).Run()
		return
	}
	fmt.Println(message)
}

func guiError(message string) {
	if _, err := exec.LookPath("zenity"); err == nil {
		_ = exec.Command("zenity", "--error", "--title="+tr("app.title"), "--text="+message).Run()
		return
	}
	fmt.Println(message)
}

func runTerminalBrandFallback(defaultBrand brandOption) (brandOption, bool) {
	fmt.Printf("%s %s\n", tr("language"), brandLabel(defaultBrand))
	return defaultBrand, true
}
