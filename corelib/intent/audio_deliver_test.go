package intent

import "testing"

func TestAudioDeliverDefinitionIsChannelVoiceSend(t *testing.T) {
	for _, def := range DefaultDefinitions() {
		if def.Label != LabelAudioDeliver {
			continue
		}
		if def.MayTriggerWorkflow {
			t.Fatal("audio_deliver must not trigger a workflow")
		}
		if len(def.ToolNames) != 0 {
			t.Fatalf("audio_deliver must not pin tool names, got %#v", def.ToolNames)
		}
		return
	}
	t.Fatal("LabelAudioDeliver definition missing")
}

func TestAudioDeliverKeywordsDoNotStealLocalPlayback(t *testing.T) {
	registry := NewKeywordRegistry()
	for _, keyword := range []string{"send this as a voice message", "deliver this as speech"} {
		matches := registry.Match(keyword)
		found := false
		for _, match := range matches {
			if match.Entry.Keyword == keyword && match.Entry.Label == LabelAudioDeliver {
				found = true
			}
			if match.Entry.Keyword == keyword && match.Entry.Label == LabelAudioSynthesize {
				t.Fatalf("%q leaked onto audio_synthesize", keyword)
			}
		}
		if !found {
			t.Fatalf("%q is not registered on audio_deliver", keyword)
		}
	}
	for _, keyword := range []string{"read this aloud", "念给我听"} {
		for _, match := range registry.Match(keyword) {
			if match.Entry.Keyword == keyword && match.Entry.Label == LabelAudioDeliver {
				t.Fatalf("%q leaked onto audio_deliver", keyword)
			}
		}
	}
}
