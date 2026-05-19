package chat

// PresenceService manages user online/offline status.
type PresenceService struct {
	store    *Store
	notifier *Notifier
}

// NewPresenceService creates a PresenceService.
func NewPresenceService(store *Store, notifier *Notifier) *PresenceService {
	return &PresenceService{store: store, notifier: notifier}
}

// IsOnline checks if a user is currently connected via WS.
func (s *PresenceService) IsOnline(userID string) bool {
	return s.notifier.IsOnline(userID)
}

func (s *PresenceService) IsOnlineForTenant(tenantID, userID string) bool {
	return s.notifier.IsOnlineForTenant(tenantID, userID)
}

// SetOnline marks a user as online (called when WS connects).
func (s *PresenceService) SetOnline(userID string) error {
	return s.store.SetPresence(userID, true)
}

func (s *PresenceService) SetOnlineForTenant(tenantID, userID string) error {
	return s.store.SetPresenceForTenant(tenantID, userID, true)
}

// SetOffline marks a user as offline (called when WS disconnects).
func (s *PresenceService) SetOffline(userID string) error {
	return s.store.SetPresence(userID, false)
}

func (s *PresenceService) SetOfflineForTenant(tenantID, userID string) error {
	return s.store.SetPresenceForTenant(tenantID, userID, false)
}
