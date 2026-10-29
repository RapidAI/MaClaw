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
