package intent

import "testing"

func TestAudioSynthesizeDefinitionIsLocalPlayback(t *testing.T) {
	for _, def := range DefaultDefinitions() {
		if def.Label != LabelAudioSynthesize {
			continue
		}
		if def.MayTriggerWorkflow {
			t.Fatal("audio_synthesize must not trigger a workflow")
		}
		if len(def.ToolNames) != 0 {
			t.Fatalf("audio_synthesize must not pin tool names, got %#v", def.ToolNames)
		}
		return
	}
	t.Fatal("LabelAudioSynthesize definition missing")
}

func TestAudioSynthesizeKeywordsDoNotStealTranscribeOrRecord(t *testing.T) {
	registry := NewKeywordRegistry()
	for _, keyword := range []string{"read this aloud", "speak this text"} {
		matches := registry.Match(keyword)
		found := false
		for _, match := range matches {
			if match.Entry.Keyword == keyword && match.Entry.Label == LabelAudioSynthesize {
				found = true
			}
			if match.Entry.Keyword == keyword && (match.Entry.Label == LabelAudioTranscribe || match.Entry.Label == LabelAudioRecord) {
				t.Fatalf("%q leaked onto %s", keyword, match.Entry.Label)
			}
		}
		if !found {
			t.Fatalf("%q is not registered on audio_synthesize", keyword)
		}
	}
}
