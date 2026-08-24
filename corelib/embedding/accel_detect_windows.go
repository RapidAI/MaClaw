//go:build windows

package embedding

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

func detectNPU() (present bool, device, reason string) {
	roots := []struct {
		key  registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Enum\ACPI`},
		{registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Enum\PCI`},
		{registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Enum\SWD`},
	}
	for _, r := range roots {
		k, err := registry.OpenKey(r.key, r.path, registry.ENUMERATE_SUB_KEYS|registry.READ)
		if err != nil {
			continue
		}
		present, device = scanEnumKey(k, 0)
		k.Close()
		if present {
			return true, device, "NPU/XDNA present"
		}
	}
	return false, "", "no NPU/XDNA"
}

func scanEnumKey(k registry.Key, depth int) (bool, string) {
	if depth > 6 {
		return false, ""
	}
	if name, _, err := k.GetStringValue("FriendlyName"); err == nil {
		if isNPUName(name) {
			return true, name
		}
	}
	if name, _, err := k.GetStringValue("DeviceDesc"); err == nil {
		if isNPUName(name) {
			return true, name
		}
	}
	names, _ := k.ReadSubKeyNames(512)
	for _, n := range names {
		if isNPUName(n) && strings.Contains(strings.ToUpper(n), "AMDI") {
			sk, err := registry.OpenKey(k, n, registry.ENUMERATE_SUB_KEYS|registry.READ)
			if err == nil {
				sk.Close()
				return true, n
			}
		}
		sk, err := registry.OpenKey(k, n, registry.ENUMERATE_SUB_KEYS|registry.READ)
		if err != nil {
			continue
		}
		ok, dev := scanEnumKey(sk, depth+1)
		sk.Close()
		if ok {
			return true, dev
		}
	}
	return false, ""
}

func isNPUName(s string) bool {
	u := strings.ToUpper(s)
	if strings.Contains(u, "NPU") || strings.Contains(u, "XDNA") || strings.Contains(u, "RYZEN AI") {
		return true
	}
	if strings.Contains(u, "COMPUTE ACCELERATOR") {
		return true
	}
	if strings.Contains(u, "AMDI000A") || strings.Contains(u, "AMDI000B") {
		return true
	}
	return false
}
