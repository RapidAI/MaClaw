//go:build linux

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

func guiChooseBrand(defaultBrand brandOption) (brandOption, bool) {
	if _, err := exec.LookPath("zenity"); err == nil {
		maclawDefault, tigerDefault := "FALSE", "FALSE"
		if defaultBrand.ID == brandOptions[1].ID {
			tigerDefault = "TRUE"
		} else {
			maclawDefault = "TRUE"
		}
		out, err := exec.Command(
			"zenity", "--list", "--radiolist",
			"--title="+tr("app.title"), "--text="+tr("choose.brand"),
			"--column=", "--column=Brand",
			maclawDefault, brandLabel(brandOptions[0]),
			tigerDefault, brandLabel(brandOptions[1]),
		).Output()
		if err != nil {
			return brandOption{}, false
		}
		if strings.Contains(string(out), "TigerClaw") {
			return brandOptions[1], true
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
