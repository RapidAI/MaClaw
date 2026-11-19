package agentservice

import "testing"

// A plain chat turn has no runtime attempt, so runtimeRequest is empty;
// the instance still marks a Hub bot and keeps the container web tools.
func TestHubBotInstanceFallsBackToInstance(t *testing.T) {
	c := &coreAgentCallbacks{instance: Instance{Metadata: map[string]string{"hub_bot": "1"}}}
	if !c.hubBotInstance() || !c.botUsesContainer() {
		t.Fatal("hub bot chat turn not recognised")
	}
	if (&coreAgentCallbacks{}).hubBotInstance() {
		t.Fatal("empty callbacks marked as hub bot")
	}
}

func TestPromptPrefixMetaFollowsTheDesktopMode(t *testing.T) {
	none := &coreAgentCallbacks{
		botID:           "bot_none",
		messageMetadata: map[string]string{"bot_desktop": "none"},
		runtimeRequest:  ExecuteRequest{Instance: Instance{Metadata: map[string]string{"hub_bot": "1", "hub_bot_desktop": "none"}}},
	}
	if none.promptPrefixMeta().DesktopMode != "none" {
		t.Fatalf("none mode = %q", none.promptPrefixMeta().DesktopMode)
	}
	desk := &coreAgentCallbacks{instance: Instance{Metadata: map[string]string{"hub_bot": "1"}}}
	if desk.promptPrefixMeta().DesktopMode != "desktop" {
		t.Fatalf("desktop mode = %q", desk.promptPrefixMeta().DesktopMode)
	}
	if (&coreAgentCallbacks{}).promptPrefixMeta().DesktopMode != "" {
		t.Fatal("a digital employee was given a desktop mode")
	}
}

func TestWakeTurnSkipsDesktopOutcomeCheck(t *testing.T) {
	c := &coreAgentCallbacks{messageMetadata: map[string]string{"bot_phase": "execute", "bot_wake": "1"}}
	d := c.ReviewDesktopOutcome("mailbox", "一句话", nil, nil)
	if d.Continue || d.Replace != "" {
		t.Fatalf("wake turn was checked: %+v", d)
	}
}

func TestHubBotSurfaceDropsInProcessDelegate(t *testing.T) {
	c := &coreAgentCallbacks{instance: Instance{Metadata: map[string]string{"hub_bot": "1"}}}
	out := c.finishToolSurface([]map[string]interface{}{{"name": "delegate_task"}, {"name": "read_file"}})
	for _, tool := range out {
		if tool["name"] == "delegate_task" {
			t.Fatal("hub bot still offers delegate_task")
		}
	}
}
