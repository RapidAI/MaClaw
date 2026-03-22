package push

import "context"

// APNsSender sends push notifications via Apple Push Notification service.
type APNsSender struct {
	// TODO: configure with APNs certificate/key, team ID, bundle ID
}

// Send delivers a push notification to an iOS device.
func (s *APNsSender) Send(ctx context.Context, deviceToken, title, body string) error {
	// TODO: implement APNs HTTP/2 push
	// Use golang.org/x/net/http2 or a library like github.com/sideshow/apns2
	return nil
}
