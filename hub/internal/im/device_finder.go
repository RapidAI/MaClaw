package im

import (
	"context"

	"github.com/RapidAI/CodeClaw/hub/internal/device"
)

// defaultNicknames is the pool of friendly Chinese nicknames assigned to
// machines that have no Alias. Once exhausted, "助手N" is used.
var defaultNicknames = []string{"张三", "李四", "王五", "赵六", "孙七"}

// DeviceServiceFinder adapts device.Service to the DeviceFinder interface
// required by MessageRouter.
type DeviceServiceFinder struct {
	Svc *device.Service
}

// FindOnlineMachineForUser returns the first online machine belonging to the
// given user. If multiple machines are online, the first one found is returned.
func (f *DeviceServiceFinder) FindOnlineMachineForUser(ctx context.Context, userID string) (machineID string, llmConfigured bool, found bool) {
	machines, err := f.Svc.ListMachines(ctx, userID)
	if err != nil {
		return "", false, false
	}
	for _, m := range machines {
		if m.Online {
			return m.MachineID, m.LLMConfigured, true
		}
	}
	return "", false, false
}

// displayName returns the best display name for a machine: Alias > Name.
func displayName(m device.MachineRuntimeInfo) string {
	if m.Alias != "" {
		return m.Alias
	}
	return m.Name
}

// FindAllOnlineMachinesForUser returns all online machines for the user.
// Machines without an Alias get a lazy-assigned friendly nickname.
func (f *DeviceServiceFinder) FindAllOnlineMachinesForUser(ctx context.Context, userID string) []OnlineMachineInfo {
	machines, err := f.Svc.ListMachines(ctx, userID)
	if err != nil {
		return nil
	}
	var online []device.MachineRuntimeInfo
	for _, m := range machines {
		if m.Online {
			online = append(online, m)
		}
	}
	// Lazy-assign nicknames to machines without Alias (only when >1 online).
	// This is a fallback for edge cases where MarkOnline didn't assign one
	// (e.g. hub restart before client reconnects). When assigning, persist
	// the nickname to runtime so it sticks and notify the client.
	if len(online) > 1 {
		usedNames := make(map[string]bool)
		for _, m := range online {
			if m.Alias != "" {
				usedNames[m.Alias] = true
			}
		}
		nextIdx := 0
		for i := range online {
			if online[i].Alias == "" {
				nick := pickNextNickname(&nextIdx, usedNames)
				online[i].Alias = nick
				usedNames[nick] = true
				// Persist to runtime and notify client so it remembers.
				f.Svc.SetAlias(ctx, online[i].MachineID, nick)
				_ = f.Svc.SendToMachine(online[i].MachineID, map[string]any{
					"type": "machine.nickname_assigned",
					"payload": map[string]any{
						"nickname": nick,
					},
				})
			}
		}
	}
	// Single device without Alias: leave it alone — the agent will
	// proactively report its nickname via set_nickname on first turn.
	out := make([]OnlineMachineInfo, 0, len(online))
	for _, m := range online {
		out = append(out, OnlineMachineInfo{
			MachineID:     m.MachineID,
			Name:          displayName(m),
			LLMConfigured: m.LLMConfigured,
		})
	}
	return out
}

// pickNextNickname returns the next available friendly nickname.
func pickNextNickname(idx *int, used map[string]bool) string {
	for *idx < len(defaultNicknames) {
		n := defaultNicknames[*idx]
		*idx++
		if !used[n] {
			return n
		}
	}
	// Fallback: 助手N
	for i := *idx + 1; ; i++ {
		n := "助手" + itoa(i)
		if !used[n] {
			*idx = i
			return n
		}
	}
}

// itoa is a minimal int-to-string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}

// FindOnlineMachineByName returns the machine ID matching the given name
// (case-insensitive) for the user.
func (f *DeviceServiceFinder) FindOnlineMachineByName(ctx context.Context, userID, name string) (machineID string, found bool) {
	return f.Svc.FindOnlineMachineByName(userID, name)
}

// SendToMachine sends a JSON-serialisable message to the machine via WebSocket.
func (f *DeviceServiceFinder) SendToMachine(machineID string, msg any) error {
	return f.Svc.SendToMachine(machineID, msg)
}
