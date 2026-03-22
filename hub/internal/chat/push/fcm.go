package push

import "context"

// FCMSender sends push notifications via Firebase Cloud Messaging.
type FCMSender struct {
	// TODO: configure with FCM server key or service account
}

// Send delivers a push notification to an Android device.
func (s *FCMSender) Send(ctx context.Context, deviceToken, title, body string) error {
	// TODO: implement FCM HTTP v1 API push
	return nil
}
