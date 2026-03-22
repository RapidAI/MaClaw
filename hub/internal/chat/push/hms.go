package push

import "context"

// HMSSender sends push notifications via Huawei Mobile Services Push Kit.
type HMSSender struct {
	// TODO: configure with HMS app ID, app secret
}

// Send delivers a push notification to a HarmonyOS/Huawei device.
func (s *HMSSender) Send(ctx context.Context, deviceToken, title, body string) error {
	// TODO: implement HMS Push Kit API
	return nil
}
