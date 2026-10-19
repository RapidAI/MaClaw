package desktopd

import (
	"context"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// Screen asks the in-container file service for one screen.
// The CDP address, working-copy profile, gate token, and display are the
// supervisor's answer. This process does not derive a Chrome port from the
// display. The token is not written to meta.json. The ask does not refresh
// last_activity and does not recreate the container.
func (s *Service) Screen(ctx context.Context, spec Spec) (desktop.ScreenStatus, error) {
	spec, err := normalize(spec)
	if err != nil {
		return desktop.ScreenStatus{}, err
	}
	key, err := specKey(spec)
	if err != nil {
		return desktop.ScreenStatus{}, err
	}
	name := containerName(spec.TenantID, spec.UserID)
	if name == "" {
		return desktop.ScreenStatus{}, fmt.Errorf("%w: desktop container is unknown", ErrInvalid)
	}
	out, err := s.docker(ctx, "exec", name, "python3", "/desktop_supervisor.py", "screen", key)
	if err != nil {
		return desktop.ScreenStatus{}, err
	}
	status, err := desktop.ParseScreen(out)
	if err != nil {
		return desktop.ScreenStatus{}, err
	}
	owner, err := desktop.UserKey(spec.TenantID, spec.UserID)
	if err != nil || !strings.HasPrefix(status.Profile, "/desktops/"+owner+"/screens/") {
		return desktop.ScreenStatus{}, fmt.Errorf("%w: desktop data belongs to another user", ErrInvalid)
	}
	return status, nil
}
