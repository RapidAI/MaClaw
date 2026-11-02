package mailbox

import "testing"

func TestExternalMutationKnown(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{name: "empty", text: "", want: false},
		{name: "blank", text: "  ", want: false},
		{name: "unrecognized", text: "天气晴", want: false},
		{name: "not configured wrapped", text: "[system rejected] " + MsgNotConfigured, want: true},
		{name: "not configured bare", text: MsgNotConfigured, want: true},
		{name: "uncertain bare", text: MsgUncertain, want: false},
		{name: "uncertain wins over rejection prefix", text: "[system rejected] " + MsgUncertain, want: false},
		{name: "system unknown", text: "[system unknown] " + MsgUncertain, want: false},
		{name: "delete uncertain", text: MsgDeleteUncertain, want: false},
		{name: "external review", text: MsgExternalReview, want: false},
		{name: "delete external review wrapped", text: "[system rejected] " + MsgDeleteExternalReview, want: false},
		{name: "sent", text: "邮件已发送", want: true},
		{name: "deleted", text: MsgDeletePurged, want: true},
		{name: "moved and purged", text: MsgDeleteMovedAndPurged, want: true},
		{name: "connect", text: MsgConnectLong, want: true},
		{name: "rejected", text: MsgRejected, want: true},
		{name: "argument", text: "不能在参数里提供确认标记", want: true},
		{name: "pending draft", text: MsgPending, want: true},
		{name: "confirmation card", text: `__ASK_USER__{"question":"发送这封邮件？"}`, want: false},
		{name: "awaiting receipt", text: "[system rejected] dynamic_effect_awaiting_receipt", want: false},
		{name: "awaiting confirmation code", text: "[system rejected] mailbox_send_awaiting_confirmation", want: false},
		{name: "known code", text: "[system rejected] mailbox_send_not_sent", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExternalMutationKnown(tc.text); got != tc.want {
				t.Fatalf("ExternalMutationKnown(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestExternalMutationUncertain(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{name: "empty", text: "", want: true},
		{name: "blank", text: "  ", want: true},
		{name: "uncertain bare", text: MsgUncertain, want: true},
		{name: "uncertain wrapped", text: "[system unknown] " + MsgUncertain, want: true},
		{name: "system unknown only", text: "[system unknown]", want: true},
		{name: "delete uncertain", text: MsgDeleteUncertain, want: true},
		{name: "uncertain wins over rejection prefix", text: "[system rejected] " + MsgUncertain, want: true},
		{name: "confirmation card", text: `__ASK_USER__{"question":"发送这封邮件？"}`, want: false},
		{name: "awaiting receipt", text: "[system rejected] dynamic_effect_awaiting_receipt", want: false},
		{name: "awaiting confirmation code", text: "[system rejected] mailbox_send_awaiting_confirmation", want: false},
		{name: "delete awaiting confirmation", text: "mailbox_delete_awaiting_confirmation", want: false},
		{name: "not configured", text: "[system rejected] " + MsgNotConfigured, want: false},
		{name: "sent", text: "邮件已发送", want: false},
		{name: "external review", text: MsgExternalReview, want: false},
		{name: "delete external review", text: MsgDeleteExternalReview, want: false},
		{name: "unrecognized", text: "天气晴", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExternalMutationUncertain(tc.text); got != tc.want {
				t.Fatalf("ExternalMutationUncertain(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}
