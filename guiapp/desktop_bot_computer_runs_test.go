package guiapp

import "testing"

func TestDesktopRunsOnThisComputerCountsOnlyThatComputer(t *testing.T) {
	runs := map[string]*ActiveDesktopRun{
		"cloud":   {BotID: "bot-cloud", WaitingForUser: false},
		"local":   {BotID: "bot-local"},
		"other":   {BotID: "bot-other"},
		"waiting": {BotID: "bot-cloud", WaitingForUser: true},
		"gone":    {BotID: "bot-missing"},
		"empty":   nil,
	}
	bots := map[string]string{
		"bot-cloud": "",
		"bot-local": "node-a",
		"bot-other": "node-b",
	}
	if got := desktopRunsOnThisComputer(runs, bots, ""); got != 1 {
		t.Fatalf("cloud computer runs = %d, want 1", got)
	}
	if got := desktopRunsOnThisComputer(runs, bots, "node-a"); got != 1 {
		t.Fatalf("local computer runs = %d, want 1", got)
	}
	if got := desktopRunsOnThisComputer(runs, bots, "node-b"); got != 1 {
		t.Fatalf("other computer runs = %d, want 1", got)
	}
}
