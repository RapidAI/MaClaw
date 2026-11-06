//go:build windows

package guiapp

import (
	"os/exec"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func companionAppPath(name string) string {
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		key, err := registry.OpenKey(root, `Software\Microsoft\Windows\CurrentVersion\App Paths\`+name, registry.QUERY_VALUE|registry.WOW64_64KEY)
		if err != nil {
			continue
		}
		value, _, readErr := key.GetStringValue("")
		_ = key.Close()
		if readErr != nil {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		if companionPathIsFile(value) {
			return value
		}
	}
	return ""
}

func browserProcessRunningOS(image string) bool {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq "+image, "/NH").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), strings.ToLower(image))
}

func writeHKCUString(view, path, name, value string) error {
	if companionRegHook != nil {
		return companionRegHook(companionRegWrite{View: view, Path: path, Name: name, Value: value})
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE|companionRegView(view))
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue(name, value)
}

func deleteHKCUKey(view, path string) error {
	if companionRegHook != nil {
		return companionRegHook(companionRegWrite{Delete: true, View: view, Path: path})
	}
	parent := path
	child := ""
	if i := strings.LastIndex(path, `\`); i >= 0 {
		parent = path[:i]
		child = path[i+1:]
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, parent, registry.ALL_ACCESS|companionRegView(view))
	if err != nil {
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}
	defer key.Close()
	if err := registry.DeleteKey(key, child); err != nil && err != registry.ErrNotExist {
		return err
	}
	return nil
}

func companionRegView(view string) uint32 {
	if view == "32" {
		return registry.WOW64_32KEY
	}
	return registry.WOW64_64KEY
}
