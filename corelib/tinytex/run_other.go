//go:build !windows

package tinytex

import "os/exec"

func applyBatCommandLine(*exec.Cmd, string, []string) {}
