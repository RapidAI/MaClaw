package botmgmt

import "context"

// Desktop wake reasons forwarded to desktopd. Watching must not start the
// GUI; only these two may.
const (
	DesktopWakeBot   = "bot-desktop"
	DesktopWakeHuman = "human-wake"
)

type desktopWakeReasonKey struct{}

// WithDesktopWakeReason labels one Desktop.Open as bot-desktop or human-wake.
func WithDesktopWakeReason(ctx context.Context, reason string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, desktopWakeReasonKey{}, reason)
}

// DesktopWakeReasonFrom reads the reason set by WithDesktopWakeReason.
func DesktopWakeReasonFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	reason, _ := ctx.Value(desktopWakeReasonKey{}).(string)
	return reason
}
