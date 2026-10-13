//go:build windows

package buildverify

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// defaultLogonPathDirs reads the machine and user Path values a new logon
// shell would search. A desktop process keeps the PATH it inherited at
// launch, so a toolchain installed afterwards is invisible to LookPath
// until the process is restarted. The reviewed program is still the one
// named in the command table; this only finds that program.
func defaultLogonPathDirs() []string {
	var dirs []string
	seen := map[string]bool{}
	add := func(block string) {
		block = os.ExpandEnv(block)
		for _, dir := range strings.Split(block, ";") {
			dir = strings.Trim(strings.TrimSpace(dir), `"`)
			if dir == "" {
				continue
			}
			dir = filepath.Clean(dir)
			key := strings.ToLower(dir)
			if seen[key] {
				continue
			}
			seen[key] = true
			dirs = append(dirs, dir)
		}
	}
	if machine, ok := registryPathValue(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`); ok {
		add(machine)
	}
	if user, ok := registryPathValue(registry.CURRENT_USER, `Environment`); ok {
		add(user)
	}
	return dirs
}

func registryPathValue(root registry.Key, path string) (string, bool) {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer key.Close()
	value, _, err := key.GetStringValue("Path")
	if err != nil {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	return value, true
}
