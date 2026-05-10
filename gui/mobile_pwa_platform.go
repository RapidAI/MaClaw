package main

import "strings"

type mobilePWAShellPlatform string

const (
	mobilePWAShellPlatformUnknown mobilePWAShellPlatform = ""
	mobilePWAShellPlatformAll     mobilePWAShellPlatform = "all"
	mobilePWAShellPlatformAndroid mobilePWAShellPlatform = "android"
	mobilePWAShellPlatformIOS     mobilePWAShellPlatform = "ios"
)

func normalizeMobilePWAShellPlatform(platform string) mobilePWAShellPlatform {
	switch mobilePWAShellPlatform(strings.ToLower(strings.TrimSpace(platform))) {
	case mobilePWAShellPlatformUnknown:
		return mobilePWAShellPlatformAll
	case mobilePWAShellPlatformAll:
		return mobilePWAShellPlatformAll
	case mobilePWAShellPlatformAndroid:
		return mobilePWAShellPlatformAndroid
	case mobilePWAShellPlatformIOS:
		return mobilePWAShellPlatformIOS
	default:
		return mobilePWAShellPlatformUnknown
	}
}

func (platform mobilePWAShellPlatform) GenerateAndroid() bool {
	return platform == mobilePWAShellPlatformAll || platform == mobilePWAShellPlatformAndroid
}

func (platform mobilePWAShellPlatform) GenerateIOS() bool {
	return platform == mobilePWAShellPlatformAll || platform == mobilePWAShellPlatformIOS
}
