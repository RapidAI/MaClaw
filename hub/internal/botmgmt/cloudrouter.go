package botmgmt

import "context"

// CloudInstance is the bot-side view of one cloud_agents row. It carries only
// what the router needs to decide reuse — the full cloudagent.Instance stays
// inside cloudagent so botmgmt never imports that package (cloudagent already
// imports botmgmt for SettingsKey; importing back would cycle).
type CloudInstance struct {
	ID     string
	Kind   string
	Status string
}

// CloudRouter is the bridge to cloudagent the hub wires in. botmgmt talks to
// it through these four calls; httpapi adapts the real cloudagent.Service to
// the interface so this package stays dependency-free. A nil CloudRouter
// leaves a no-desktop bot on its original srv /messages path.
type CloudRouter interface {
	// FindReusable returns the newest live instance owned by (tenant, owner,
	// bot) of this kind, or found=false when none is reusable. Stopped
	// instances come back so the caller can Resume them; deleted and
	// provision_failed rows are not reusable.
	FindReusable(ctx context.Context, tenantID, userID, botID, kind string) (inst CloudInstance, found bool, err error)
	// Create provisions a fresh instance of this kind bound to the bot. The
	// name is display-only.
	Create(ctx context.Context, tenantID, userID, botID, name, kind string) (CloudInstance, error)
	// Resume wakes a stopped instance back to idle. Only meaningful on a
	// CloudInstance whose Status is the stopped state.
	Resume(ctx context.Context, tenantID, userID, instanceID string) (CloudInstance, error)
	// Delete removes an instance the router can no longer use — e.g. one
	// whose Resume failed — freeing its quota slot for a fresh Create.
	Delete(ctx context.Context, tenantID, userID, instanceID string) error
	// SendMessage posts one user turn to the instance. clientMessageID is the
	// per-turn idempotency key; a repeated key replays the run it started.
	SendMessage(ctx context.Context, tenantID, userID, instanceID, clientMessageID, text string) error
}
