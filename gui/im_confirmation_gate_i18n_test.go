package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestConfirmationRiskFlagsLocalized(t *testing.T) {
	tests := []struct {
		name   string
		intent taskIntent
		lang   string
		want   []string
	}{
		{
			name:   "coding zh",
			intent: intentCoding,
			lang:   "zh-Hans",
			want:   []string{"\u672a\u786e\u8ba4\u5c31\u6267\u884c\u53ef\u80fd\u4f1a\u4fee\u6539\u9519\u8bef\u76ee\u5f55\u4e2d\u7684\u4ee3\u7801"},
		},
		{
			name:   "ssh zh",
			intent: intentSSH,
			lang:   "zh-CN",
			want:   []string{"\u672a\u786e\u8ba4\u5c31\u6267\u884c\u53ef\u80fd\u4f1a\u8fde\u63a5\u5230\u9519\u8bef\u7684\u670d\u52a1\u5668\u6216\u73af\u5883"},
		},
		{
			name:   "ambiguous en",
			intent: intentAmbiguous,
			lang:   "en-US",
			want:   []string{"The request has multiple possible execution paths and should be clarified first"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := confirmationRiskFlags(tt.intent, tt.lang); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("confirmationRiskFlags(%q, %q) = %#v, want %#v", tt.intent, tt.lang, got, tt.want)
			}
		})
	}
}

func TestBuildConfirmationResponseLocalizedPayload(t *testing.T) {
	item := &pendingConfirmation{
		ID:       "c-localized",
		Summary:  "confirm task",
		TaskType: "coding",
		Status:   confirmationStatusPending,
	}

	zh := buildConfirmationResponse(item, "zh-Hans")
	if zh == nil || zh.Confirmation == nil || zh.Confirmation.Labels == nil {
		t.Fatalf("missing zh confirmation payload: %#v", zh)
	}
	if zh.Confirmation.Labels.RiskFlags != "\u98ce\u9669\u6807\u8bb0" {
		t.Fatalf("zh risk label = %q", zh.Confirmation.Labels.RiskFlags)
	}
	if len(zh.Actions) == 0 || zh.Actions[0].Label != "\u786e\u8ba4\u5e76\u5f00\u59cb" {
		t.Fatalf("zh confirm action = %#v", zh.Actions)
	}
	if strings.Contains(zh.Text, "Please confirm") {
		t.Fatalf("zh confirmation text leaked English: %q", zh.Text)
	}

	en := buildConfirmationResponse(item, "en-US")
	if en == nil || en.Confirmation == nil || en.Confirmation.Labels == nil {
		t.Fatalf("missing en confirmation payload: %#v", en)
	}
	if en.Confirmation.Labels.RiskFlags != "Risk flags" {
		t.Fatalf("en risk label = %q", en.Confirmation.Labels.RiskFlags)
	}
	if len(en.Actions) == 0 || en.Actions[0].Label != "Confirm and start" {
		t.Fatalf("en confirm action = %#v", en.Actions)
	}
}

func TestConfirmationRevisionHintsLocalized(t *testing.T) {
	tests := []struct {
		name   string
		intent taskIntent
		lang   string
		want   []string
	}{
		{
			name:   "default zh",
			intent: intentCoding,
			lang:   "zh-Hans",
			want:   []string{"\u5982\u679c\u76ee\u5f55\u4e0d\u5bf9\uff0c\u8bf7\u56de\u590d\u6b63\u786e\u76ee\u5f55", "\u5982\u679c\u4efb\u52a1\u7406\u89e3\u4e0d\u5bf9\uff0c\u8bf7\u56de\u590d\u4fee\u6b63\u5185\u5bb9"},
		},
		{
			name:   "ambiguous zh",
			intent: intentAmbiguous,
			lang:   "zh-CN",
			want:   []string{"\u8bf7\u8bf4\u660e\u8fd9\u662f\u4ee3\u7801\u5de5\u4f5c\u8fd8\u662f SSH/\u670d\u52a1\u5668\u5de5\u4f5c", "\u8bf7\u63d0\u4f9b\u6b63\u786e\u7684\u9879\u76ee\u76ee\u5f55\u6216\u4e3b\u673a\u4fe1\u606f"},
		},
		{
			name:   "default en",
			intent: intentCoding,
			lang:   "en",
			want:   []string{"If the directory is wrong, reply with the correct directory", "If the task understanding is wrong, reply with the correction"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := confirmationRevisionHints(tt.intent, tt.lang); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("confirmationRevisionHints(%q, %q) = %#v, want %#v", tt.intent, tt.lang, got, tt.want)
			}
		})
	}
}
