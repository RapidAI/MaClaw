//go:build !windows

package guiapp

func presentComputerUseWindowHalo() {}

func hideComputerUseWindowHaloPlatform() {}

func computerUseHaloHWND() uint64 { return 0 }

func computerUseHaloIsOverlay(uint64) bool { return false }
