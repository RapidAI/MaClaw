package main

import "strings"

type agentActivitySource string

const (
	agentActivitySourceUnknown       agentActivitySource = ""
	agentActivitySourceIM            agentActivitySource = "im"
	agentActivitySourceGUI           agentActivitySource = "gui"
	agentActivitySourceBrowserReplay agentActivitySource = "browser_replay"
	agentActivitySourceGUIReplay     agentActivitySource = "gui_replay"
)

func normalizeAgentActivitySource(source string) agentActivitySource {
	switch agentActivitySource(strings.TrimSpace(source)) {
	case agentActivitySourceIM:
		return agentActivitySourceIM
	case agentActivitySourceGUI:
		return agentActivitySourceGUI
	case agentActivitySourceBrowserReplay:
		return agentActivitySourceBrowserReplay
	case agentActivitySourceGUIReplay:
		return agentActivitySourceGUIReplay
	default:
		return agentActivitySourceUnknown
	}
}

func (source agentActivitySource) String() string {
	return string(source)
}

type agentActivityPlatform string

const (
	agentActivityPlatformDesktop agentActivityPlatform = "desktop"
)

func agentActivitySourceForPlatform(platform string) agentActivitySource {
	if agentActivityPlatform(strings.TrimSpace(platform)) == agentActivityPlatformDesktop {
		return agentActivitySourceGUI
	}
	return agentActivitySourceIM
}
