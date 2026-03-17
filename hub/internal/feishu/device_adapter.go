package feishu

import (
	"context"

	"github.com/RapidAI/CodeClaw/hub/internal/device"
)

// DeviceServiceAdapter wraps device.Service to satisfy the DeviceLister interface.
type DeviceServiceAdapter struct {
	Svc *device.Service
}

func (a *DeviceServiceAdapter) ListMachines(ctx context.Context, userID string) ([]MachineInfo, error) {
	items, err := a.Svc.ListMachines(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]MachineInfo, len(items))
	for i, m := range items {
		out[i] = MachineInfo{
			MachineID:      m.MachineID,
			Name:           m.Name,
			Platform:       m.Platform,
			Hostname:       m.Hostname,
			Online:         m.Online,
			ActiveSessions: m.ActiveSessions,
			LLMConfigured:  m.LLMConfigured,
		}
	}
	return out, nil
}

func (a *DeviceServiceAdapter) IsMachineOnline(machineID string) bool {
	return a.Svc.IsMachineOnline(machineID)
}

func (a *DeviceServiceAdapter) SendToMachine(machineID string, msg any) error {
	return a.Svc.SendToMachine(machineID, msg)
}
