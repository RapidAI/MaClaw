package botmgmt

import (
	"context"
	"strings"
	"time"
)

// desktopControlTTL is how long one machine keeps the keyboard after its last
// claim. The GUI claims again while that screen is open on that machine.
const desktopControlTTL = 20 * time.Second

// desktopControlHold is the machine currently allowed to type on one screen.
// The key is the desktop user (the owner for a shared screen, the bot's own
// user for an isolated display), so two clients cannot both type on the same
// pixels through two shared bots.
type desktopControlHold struct {
	MachineID string
	BotID     string
	Until     time.Time
}

// ClaimDesktopControl gives this machine the keyboard for the bot's screen.
// A different machine that still holds it is refused. An empty machine id
// cannot be told apart from another end, so it never gets the keyboard.
func (s *Service) ClaimDesktopControl(ctx context.Context, tenantID, userID, botID, machineID string) (bool, error) {
	machineID = strings.TrimSpace(machineID)
	if s == nil || machineID == "" {
		return false, nil
	}
	key, err := s.desktopControlKey(ctx, tenantID, userID, botID)
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.dropExpiredDesktopControlLocked(now)
	if hold, ok := s.desktopControl[key]; ok && hold.MachineID != machineID {
		return false, nil
	}
	if s.desktopControl == nil {
		s.desktopControl = map[string]desktopControlHold{}
	}
	s.desktopControl[key] = desktopControlHold{MachineID: machineID, BotID: strings.TrimSpace(botID), Until: now.Add(desktopControlTTL)}
	return true, nil
}

// ReleaseDesktopControl drops the hold when this machine is the one typing.
func (s *Service) ReleaseDesktopControl(ctx context.Context, tenantID, userID, botID, machineID string) error {
	machineID = strings.TrimSpace(machineID)
	if s == nil || machineID == "" {
		return nil
	}
	key, err := s.desktopControlKey(ctx, tenantID, userID, botID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	hold, ok := s.desktopControl[key]
	// A later claim for another bot on this same screen updates BotID. A release
	// that was already in flight for the previous bot must not drop that hold.
	if ok && hold.MachineID == machineID && (hold.BotID == "" || hold.BotID == strings.TrimSpace(botID)) {
		delete(s.desktopControl, key)
	}
	return nil
}

// DesktopInputGranted reports whether this machine may type on the bot's screen.
// Looking does not extend the hold. Only ClaimDesktopControl does, so a watch
// poll after the person let go cannot take the keyboard back.
func (s *Service) DesktopInputGranted(ctx context.Context, tenantID, userID, botID, machineID string) bool {
	machineID = strings.TrimSpace(machineID)
	if s == nil || machineID == "" {
		return false
	}
	key, err := s.desktopControlKey(ctx, tenantID, userID, botID)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropExpiredDesktopControlLocked(s.now())
	hold, ok := s.desktopControl[key]
	return ok && hold.MachineID == machineID
}

// releaseDesktopControlScreen drops whoever holds this screen. Handing the
// desktop back to the bot ends the person's keyboard on every machine.
func (s *Service) releaseDesktopControlScreen(tenantID, desktopUser string) {
	if s == nil {
		return
	}
	key := desktopViewKey(tenantID, desktopUser)
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.desktopControl, key)
}

func (s *Service) desktopControlKey(ctx context.Context, tenantID, userID, botID string) (string, error) {
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return "", err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != strings.TrimSpace(userID) {
		return "", ErrNotFound
	}
	return desktopViewKey(tenantID, desktopUserForBot(userID, rec.Bots[index])), nil
}

func (s *Service) dropExpiredDesktopControlLocked(now time.Time) {
	for key, hold := range s.desktopControl {
		if !now.Before(hold.Until) {
			delete(s.desktopControl, key)
		}
	}
}
