package device

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/ws"
)

func TestSumOnlineAdaptivePrompt(t *testing.T) {
	s := NewService(nil, NewRuntime())
	// Online machine A with stats
	s.runtime.mu.Lock()
	s.runtime.desktopsByMachine["m1"] = &ws.ConnContext{TenantID: "t1", UserID: "u1"}
	s.runtime.metadataByMachine["m1"] = MachineRuntimeInfo{
		MachineID: "m1",
		TenantID:  "t1",
		Online:    true,
		AdaptivePrompt: &corelib.AdaptivePromptStat{
			LightTurns:     2,
			FullTurns:      1,
			EstTokensSaved: 1000,
			LightUpgrades:  1,
		},
	}
	// Online machine B other tenant
	s.runtime.desktopsByMachine["m2"] = &ws.ConnContext{TenantID: "t2", UserID: "u2"}
	s.runtime.metadataByMachine["m2"] = MachineRuntimeInfo{
		MachineID: "m2",
		TenantID:  "t2",
		Online:    true,
		AdaptivePrompt: &corelib.AdaptivePromptStat{
			LightTurns:     3,
			FullTurns:      0,
			EstTokensSaved: 500,
		},
	}
	// Online machine C no stats
	s.runtime.desktopsByMachine["m3"] = &ws.ConnContext{TenantID: "t1", UserID: "u3"}
	s.runtime.metadataByMachine["m3"] = MachineRuntimeInfo{MachineID: "m3", TenantID: "t1", Online: true}
	s.runtime.mu.Unlock()

	all, with, online := s.SumOnlineAdaptivePrompt("")
	if online < 3 {
		t.Fatalf("online=%d", online)
	}
	if with != 2 {
		t.Fatalf("withStats=%d", with)
	}
	if all.LightTurns != 5 || all.FullTurns != 1 || all.EstTokensSaved != 1500 {
		t.Fatalf("totals=%+v", all)
	}

	t1, with1, _ := s.SumOnlineAdaptivePrompt("t1")
	if with1 != 1 || t1.LightTurns != 2 {
		t.Fatalf("t1 with=%d totals=%+v", with1, t1)
	}
}

func TestSumOnlineCostOps(t *testing.T) {
	s := NewService(nil, NewRuntime())
	s.runtime.mu.Lock()
	s.runtime.desktopsByMachine["m1"] = &ws.ConnContext{TenantID: "t1", UserID: "u1"}
	s.runtime.desktopsByMachine["m2"] = &ws.ConnContext{TenantID: "t1", UserID: "u2"}
	s.runtime.metadataByMachine["m1"] = MachineRuntimeInfo{
		MachineID: "m1", TenantID: "t1", Online: true,
		CostOps: &corelib.CostOpsStat{RouteDecisions: 10, DailyCostUSD: 1.5, DailyCalls: 3},
	}
	s.runtime.metadataByMachine["m2"] = MachineRuntimeInfo{
		MachineID: "m2", TenantID: "t1", Online: true,
		CostOps: &corelib.CostOpsStat{RouteDecisions: 5, DailyCostUSD: 0.5, DailyCalls: 2},
	}
	s.runtime.mu.Unlock()

	all, with, online := s.SumOnlineCostOps("")
	if online < 2 || with != 2 {
		t.Fatalf("online=%d with=%d", online, with)
	}
	if all.RouteDecisions != 15 || all.DailyCostUSD < 1.9 || all.DailyCalls != 5 {
		t.Fatalf("sum=%+v", all)
	}
}

func TestHeartbeatStoresCostOps(t *testing.T) {
	s := NewService(nil, NewRuntime())
	s.runtime.mu.Lock()
	s.runtime.desktopsByMachine["m-cost"] = &ws.ConnContext{TenantID: "t", UserID: "u"}
	s.runtime.mu.Unlock()
	stat := &corelib.CostOpsStat{RouteDecisions: 7, DailyCostUSD: 0.25, DailyCalls: 2, RouteSummary: "cost-route decisions=7"}
	if err := s.Heartbeat(nil, "m-cost", ws.MachineHeartbeatPayload{CostOps: stat}); err != nil {
		t.Fatal(err)
	}
	info, err := s.GetMachineInfo(nil, "m-cost")
	if err != nil || info == nil || info.CostOps == nil {
		t.Fatalf("info=%+v err=%v", info, err)
	}
	if info.CostOps.RouteDecisions != 7 {
		t.Fatalf("cost_ops=%+v", info.CostOps)
	}
}

func TestHeartbeatStoresAdaptivePrompt(t *testing.T) {
	s := NewService(nil, NewRuntime())
	s.runtime.mu.Lock()
	s.runtime.desktopsByMachine["m9"] = &ws.ConnContext{TenantID: "t", UserID: "u"}
	s.runtime.mu.Unlock()

	stat := &corelib.AdaptivePromptStat{LightTurns: 4, FullTurns: 1, LightPercent: 80, Summary: "adaptive-prompt: light 80%"}
	if err := s.Heartbeat(nil, "m9", ws.MachineHeartbeatPayload{AdaptivePrompt: stat}); err != nil {
		t.Fatal(err)
	}
	info, err := s.GetMachineInfo(nil, "m9")
	if err != nil || info == nil || info.AdaptivePrompt == nil {
		t.Fatalf("info=%+v err=%v", info, err)
	}
	if info.AdaptivePrompt.LightTurns != 4 {
		t.Fatalf("adaptive=%+v", info.AdaptivePrompt)
	}
}
