package main

import (
	"testing"
)

func TestSdkMessageToText_ImageBlock(t *testing.T) {
	tests := []struct {
		name string
		msg  SDKMessage
		want string
	}{
		{
			name: "image block with source and media_type",
			msg: SDKMessage{
				Type: "assistant",
				Message: &SDKAssistantPayload{
					Role: "assistant",
					Content: []SDKContentBlock{
						{
							Type: "image",
							Source: &SDKImageSource{
								Type:      "base64",
								MediaType: "image/png",
								Data:      "abc123",
							},
						},
					},
				},
			},
			want: "🖼 Image (image/png)",
		},
		{
			name: "image block with nil source",
			msg: SDKMessage{
				Type: "assistant",
				Message: &SDKAssistantPayload{
					Role: "assistant",
					Content: []SDKContentBlock{
						{Type: "image"},
					},
				},
			},
			want: "🖼 Image",
		},
		{
			name: "image block with empty media_type",
			msg: SDKMessage{
				Type: "assistant",
				Message: &SDKAssistantPayload{
					Role: "assistant",
					Content: []SDKContentBlock{
						{
							Type:   "image",
							Source: &SDKImageSource{Type: "base64", MediaType: "", Data: "abc"},
						},
					},
				},
			},
			want: "🖼 Image",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sdkMessageToText(tt.msg)
			if got != tt.want {
				t.Errorf("sdkMessageToText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractStreamEventText_ImageBlock(t *testing.T) {
	tests := []struct {
		name  string
		event map[string]interface{}
		want  string
	}{
		{
			name: "content_block_start with image type and source",
			event: map[string]interface{}{
				"type": "content_block_start",
				"content_block": map[string]interface{}{
					"type": "image",
					"source": map[string]interface{}{
						"type":       "base64",
						"media_type": "image/jpeg",
						"data":       "abc123",
					},
				},
			},
			want: "\n🖼 Image (image/jpeg)",
		},
		{
			name: "content_block_start with image type no source",
			event: map[string]interface{}{
				"type": "content_block_start",
				"content_block": map[string]interface{}{
					"type": "image",
				},
			},
			want: "\n🖼 Image",
		},
		{
			name: "content_block_start with image type empty media_type",
			event: map[string]interface{}{
				"type": "content_block_start",
				"content_block": map[string]interface{}{
					"type": "image",
					"source": map[string]interface{}{
						"type":       "base64",
						"media_type": "",
					},
				},
			},
			want: "\n🖼 Image",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractStreamEventText(tt.event)
			if got != tt.want {
				t.Errorf("extractStreamEventText() = %q, want %q", got, tt.want)
			}
		})
	}
}
