package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/ws"
)

func TestDebugListMachinesHandlerReturnsMachinesForUser(t *testing.T) {
	identity, deviceSvc, _ := newHTTPAPITestServices(t)
	_, enroll := issueViewerToken(t, identity, "debug-machines@example.com")

	deviceSvc.BindDesktop(enroll.MachineID, &ws.ConnContext{UserID: enroll.UserID, Role: "machine"})
	if err := deviceSvc.MarkOnline(context.Background(), enroll.MachineID, ws.MachineHelloPayload{
		Name:                 "office-pc",
		Platform:             "windows",
		Hostname:             "office-host",
		Arch:                 "amd64",
		AppVersion:           "1.0.0",
		HeartbeatIntervalSec: 60,
	}); err != nil {
		t.Fatalf("MarkOnline: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/debug/machines?user_id="+url.QueryEscape(enroll.UserID), nil)
	rr := httptest.NewRecorder()

	DebugListMachinesHandler(deviceSvc, identity.UsersRepo()).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	if !strings.Contains(body, enroll.MachineID) || !strings.Contains(body, "office-pc") {
		t.Fatalf("unexpected body=%s", body)
	}
	if !strings.Contains(body, `"user_email":"debug-machines@example.com"`) {
		t.Fatalf("expected bound email in body=%s", body)
	}
	if !strings.Contains(body, `"hostname":"office-host"`) || !strings.Contains(body, `"arch":"amd64"`) {
		t.Fatalf("expected machine metadata in body=%s", body)
	}
	if !strings.Contains(body, `"status":"online"`) {
		t.Fatalf("expected online status in body=%s", body)
	}
}

func TestDebugListMachinesHandlerFallsBackToOnlineRuntimeSnapshot(t *testing.T) {
	deviceSvc := deviceOnlyTestService()
	deviceSvc.BindDesktop("machine_runtime_1", &ws.ConnContext{UserID: "user_runtime_1", Role: "machine"})

	req := httptest.NewRequest(http.MethodGet, "/api/debug/machines", nil)
	rr := httptest.NewRecorder()

	DebugListMachinesHandler(deviceSvc, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	if !strings.Contains(body, "machine_runtime_1") || !strings.Contains(body, `"online":true`) {
		t.Fatalf("unexpected body=%s", body)
	}
}

func TestDebugListMachineEventsHandlerReturnsRecentEvents(t *testing.T) {
	deviceSvc := deviceOnlyTestService()
	deviceSvc.BindDesktop("machine_events_1", &ws.ConnContext{UserID: "user_events_1", Role: "machine"})

	req := httptest.NewRequest(http.MethodGet, "/api/debug/machine-events", nil)
	rr := httptest.NewRecorder()

	DebugListMachineEventsHandler(deviceSvc).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	if !strings.Contains(body, "machine_events_1") || !strings.Contains(body, `"type":"bind"`) {
		t.Fatalf("unexpected body=%s", body)
	}
}

func deviceOnlyTestService() *device.Service {
	return device.NewService(nil, device.NewRuntime())
}
