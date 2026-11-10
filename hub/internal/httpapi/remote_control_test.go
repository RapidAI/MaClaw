package httpapi

import (
	"context"
	"testing"

	"github.com/gorilla/websocket"

	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/ws"
)

func TestRemoteControlMachineNamePrefersComputerName(t *testing.T) {
	item := device.MachineRuntimeInfo{Name: "DESKTOP-HOME", Hostname: "desktop-home", Alias: "Bravo"}
	if got := remoteControlMachineName(item); got != "DESKTOP-HOME" {
		t.Fatalf("name = %q", got)
	}
	item.Name = ""
	if got := remoteControlMachineName(item); got != "desktop-home" {
		t.Fatalf("hostname fallback = %q", got)
	}
	item.Hostname = ""
	if got := remoteControlMachineName(item); got != "Bravo" {
		t.Fatalf("alias fallback = %q", got)
	}
}

func TestRemoteControlOtherMachinesUsesLivePresence(t *testing.T) {
	runtime := device.NewRuntime()
	devices := device.NewService(nil, runtime)
	conn := &ws.ConnContext{UserID: "user-a", Conn: &websocket.Conn{}}
	devices.BindDesktop("mac-b", conn)
	if err := devices.MarkOnline(context.Background(), "mac-b", ws.MachineHelloPayload{Name: "DESKTOP-HOME", Nickname: "Bravo"}); err != nil {
		t.Fatal(err)
	}
	h := remoteControlHandler{devices: devices}
	items := h.otherMachines(context.Background(), "user-a", "mac-a")
	if len(items) != 1 || items[0].Name != "DESKTOP-HOME" || !items[0].Online {
		t.Fatalf("machines = %+v", items)
	}
	if err := devices.UnbindDesktop(context.Background(), "mac-b", conn); err != nil {
		t.Fatal(err)
	}
	if devices.IsMachineOnline("mac-b") {
		t.Fatal("unbound machine is still online")
	}
}
