package push

import (
	"context"
	"log"
)

// Dispatcher routes push notifications to the appropriate platform sender.
type Dispatcher struct {
	apns *APNsSender
	fcm  *FCMSender
	hms  *HMSSender
	// tokenLookup resolves userID → []PushToken
	tokenLookup func(userID string) ([]TokenInfo, error)
}

// TokenInfo holds a push token and its platform.
type TokenInfo struct {
	Platform string // "apns", "fcm", "hms"
	Token    string
}

// NewDispatcher creates a push Dispatcher.
func NewDispatcher(tokenLookup func(string) ([]TokenInfo, error)) *Dispatcher {
	return &Dispatcher{
		apns:        &APNsSender{},
		fcm:         &FCMSender{},
		hms:         &HMSSender{},
		tokenLookup: tokenLookup,
	}
}

// SendPush sends a push notification to all registered devices of a user.
func (d *Dispatcher) SendPush(ctx context.Context, userID, title, body string) error {
	tokens, err := d.tokenLookup(userID)
	if err != nil {
		return err
	}
	for _, t := range tokens {
		var sendErr error
		switch t.Platform {
		case "apns":
			sendErr = d.apns.Send(ctx, t.Token, title, body)
		case "fcm":
			sendErr = d.fcm.Send(ctx, t.Token, title, body)
		case "hms":
			sendErr = d.hms.Send(ctx, t.Token, title, body)
		}
		if sendErr != nil {
			log.Printf("[push] %s send to %s failed: %v", t.Platform, userID, sendErr)
		}
	}
	return nil
}
