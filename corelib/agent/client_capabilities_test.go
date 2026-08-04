package agent

import (
	"strings"
	"testing"
)

func TestBuildClientCapabilityPromptIncludesFormatsAndCombinations(t *testing.T) {
	capabilities := &ClientCapabilities{
		Input: ClientInputCapabilities{Modalities: []string{"text", "audio"}},
		Output: ClientOutputCapabilities{
			Modalities:   []string{"text", "audio", "image", "file"},
			Preferred:    []string{"text", "image", "audio", "file"},
			Combinations: [][]string{{"text"}, {"text", "image"}, {"audio"}},
			Text:         &ClientTextCapabilities{MaxChars: 240, Locale: "zh-CN"},
			Audio:        &ClientAudioCapabilities{MimeTypes: []string{"audio/wav"}, SampleRates: []int{16000}, Channels: 1, Playback: true},
			Image:        &ClientImageCapabilities{MimeTypes: []string{"image/png"}, MaxWidth: 360, MaxHeight: 360},
			File:         &ClientFileCapabilities{MimeTypes: []string{"application/pdf"}, MaxBytes: 4096},
		},
	}
	prompt := BuildClientCapabilityPrompt(capabilities)
	for _, want := range []string{
		"Input modalities: text, audio",
		"Allowed output combinations: [text]; [text+image]; [audio]",
		"MIME=audio/wav, sampleRates=16000, channels=1",
		"MIME=application/pdf, maxBytes=4096",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
}

func TestNormalizeClientCapabilitiesDefaultsLegacyClientToTextOnly(t *testing.T) {
	capabilities := NormalizeClientCapabilities(nil)
	if !capabilities.SupportsOutput("text") || capabilities.SupportsOutput("image") {
		t.Fatalf("legacy defaults=%#v", capabilities)
	}
	if !capabilities.SupportsOutputCombination("text") {
		t.Fatal("legacy client must accept a text-only response")
	}
}

func TestClientCapabilitiesValidateInboundModalityAndMIME(t *testing.T) {
	capabilities := NormalizeClientCapabilities(&ClientCapabilities{Input: ClientInputCapabilities{
		Modalities: []string{"text", "audio", "image"},
		Audio:      &ClientAudioCapabilities{MimeTypes: []string{"audio/wav"}},
		Image:      &ClientImageCapabilities{MimeTypes: []string{"image/*"}},
	}})
	if !capabilities.SupportsInput("audio") || capabilities.SupportsInput("file") {
		t.Fatalf("input modalities=%#v", capabilities.Input.Modalities)
	}
	if !capabilities.SupportsInputMIME("audio", "audio/wav; rate=16000") || capabilities.SupportsInputMIME("audio", "audio/mpeg") {
		t.Fatal("audio input MIME negotiation failed")
	}
	if !capabilities.SupportsInputMIME("image", "image/jpeg") {
		t.Fatal("image wildcard input MIME should be accepted")
	}
}

func TestSelectClientOutputCombinationUsesRichestThenPreferred(t *testing.T) {
	capabilities := NormalizeClientCapabilities(&ClientCapabilities{Output: ClientOutputCapabilities{
		Modalities:   []string{"text", "image", "audio"},
		Preferred:    []string{"image", "text", "audio"},
		Combinations: [][]string{{"text"}, {"image"}, {"audio"}, {"text", "image"}},
		Image:        &ClientImageCapabilities{},
		Audio:        &ClientAudioCapabilities{Playback: true},
	}})
	selected := SelectClientOutputCombination(capabilities, "text", "image", "audio")
	if len(selected) != 2 || !containsClientString(selected, "text") || !containsClientString(selected, "image") {
		t.Fatalf("selected combination=%#v", selected)
	}
	selected = SelectClientOutputCombination(capabilities, "text", "audio")
	if len(selected) != 1 || selected[0] != "text" {
		t.Fatalf("preferred singleton=%#v", selected)
	}
}

func TestAudioDeliveryCapabilitiesAreBoundedAndURLIsExplicit(t *testing.T) {
	legacy := NormalizeClientCapabilities(&ClientCapabilities{Output: ClientOutputCapabilities{
		Modalities: []string{"audio"}, Audio: &ClientAudioCapabilities{MimeTypes: []string{"audio/wav"}, Playback: true},
	}})
	if !legacy.SupportsOutputAudioDelivery("inline", 1024) || legacy.SupportsOutputAudioDelivery("url", 1024) {
		t.Fatalf("legacy delivery=%#v", legacy.Output.Audio)
	}

	bounded := NormalizeClientCapabilities(&ClientCapabilities{Output: ClientOutputCapabilities{
		Modalities: []string{"audio"}, Audio: &ClientAudioCapabilities{
			MimeTypes: []string{"audio/wav"}, Playback: true,
			DeliveryModes:  []string{"INLINE", "url", "ftp", "url"},
			MaxInlineBytes: 8, MaxDownloadBytes: 64,
		},
	}})
	if !bounded.SupportsOutputAudioDelivery("inline", 8) || bounded.SupportsOutputAudioDelivery("inline", 9) {
		t.Fatal("inline size bound was not enforced")
	}
	if !bounded.SupportsOutputAudioDelivery("url", 64) || bounded.SupportsOutputAudioDelivery("url", 65) {
		t.Fatal("URL size bound was not enforced")
	}
	if len(bounded.Output.Audio.DeliveryModes) != 2 || bounded.Output.Audio.DeliveryModes[0] != "inline" || bounded.Output.Audio.DeliveryModes[1] != "url" {
		t.Fatalf("delivery normalization=%#v", bounded.Output.Audio.DeliveryModes)
	}

	urlWithoutLimit := NormalizeClientCapabilities(&ClientCapabilities{Output: ClientOutputCapabilities{
		Modalities: []string{"audio"}, Audio: &ClientAudioCapabilities{Playback: true, DeliveryModes: []string{"url"}},
	}})
	if urlWithoutLimit.SupportsOutputAudioDelivery("url", 1) {
		t.Fatal("URL delivery without an explicit download limit must be rejected")
	}
}
