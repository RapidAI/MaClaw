package httpapi

import (
	"context"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/device"
)

// mobileDevicePushAdapter adapts device.Service to MobileMachinePush.
type mobileDevicePushAdapter struct {
	svc *device.Service
}

func (a mobileDevicePushAdapter) ListOnlineMachineIDsForUser(ctx context.Context, userID string) []string {
	if a.svc == nil {
		return nil
	}
	machines, err := a.svc.ListMachines(ctx, strings.TrimSpace(userID))
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(machines))
	for _, m := range machines {
		if !m.Online {
			continue
		}
		id := strings.TrimSpace(m.MachineID)
		if id == "" {
			continue
		}
		out = append(out, id)
	}
	return out
}

func (a mobileDevicePushAdapter) SendToMachine(machineID string, msg any) error {
	if a.svc == nil {
		return nil
	}
	return a.svc.SendToMachine(machineID, msg)
}
