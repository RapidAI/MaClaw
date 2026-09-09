package main

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestThirdPartyGatewayRejectsHubPairingReservationLocally(t *testing.T) {
	manager := newThirdPartyGatewayManager(&App{})
	manager.pairings["123456"] = thirdPartyDevicePairing{
		ExpiresAt: time.Now().Add(time.Minute),
		Remote:    true,
	}
	req := httptest.NewRequest(http.MethodPost, "/api/device-gateway/v1/pair", bytes.NewBufferString(`{"pairCode":"123456","clientId":"pet-a"}`))
	recorder := httptest.NewRecorder()
	manager.handleDevicePair(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("remote reservation was exchangeable locally: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if _, exists := manager.pairings["123456"]; !exists {
		t.Fatal("local rejection removed the remote collision reservation")
	}
}

func TestRemoveRemotePairingReservationKeepsLocalPairing(t *testing.T) {
	manager := newThirdPartyGatewayManager(&App{})
	manager.pairings["111111"] = thirdPartyDevicePairing{ExpiresAt: time.Now().Add(time.Minute), Remote: true}
	manager.pairings["222222"] = thirdPartyDevicePairing{Token: "token", ExpiresAt: time.Now().Add(time.Minute)}
	manager.removeRemotePairingReservation("111111")
	manager.removeRemotePairingReservation("222222")
	if _, exists := manager.pairings["111111"]; exists {
		t.Fatal("remote reservation was not removed")
	}
	if _, exists := manager.pairings["222222"]; !exists {
		t.Fatal("local pairing was removed by remote rollback")
	}
}

func TestThirdPartyPairCodeFromTranscript(t *testing.T) {
	cases := map[string]string{
		"645432":                       "645432",
		"请配对 六 四 五 四 三 二":              "645432",
		"零幺两三四五":                       "012345",
		"six four five four three two": "645432",
	}
	for input, want := range cases {
		got, ok := thirdPartyPairCodeFromTranscript(input)
		if !ok || got != want {
			t.Errorf("thirdPartyPairCodeFromTranscript(%q) = %q, %v; want %q, true", input, got, ok, want)
		}
	}
	if _, ok := thirdPartyPairCodeFromTranscript("六码 64 54 32 七"); ok {
		t.Fatal("accepted transcript containing seven digits")
	}
}

func TestThirdPartyVoicePairExchangeUsesSingleUsePairing(t *testing.T) {
	manager := newThirdPartyGatewayManager(&App{})
	manager.pairings["645432"] = thirdPartyDevicePairing{
		Token: "durable-token", ExpiresAt: time.Now().Add(time.Minute),
	}
	recorder := httptest.NewRecorder()
	manager.exchangeDevicePairing(recorder, httplessDevicePairRequest{PairCode: "645432", ClientID: "pet-a"})
	if recorder.Code != http.StatusCreated || !bytes.Contains(recorder.Body.Bytes(), []byte(`"gatewayToken":"durable-token"`)) {
		t.Fatalf("first exchange status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	manager.exchangeDevicePairing(recorder, httplessDevicePairRequest{PairCode: "645432", ClientID: "pet-a"})
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("pairing code reused: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestHardwareDoesNotConstrainIMGatewayControls(t *testing.T) {
	localMode := false
	app := &App{testHomeDir: t.TempDir(), configCacheValid: true, configCache: corelib.AppConfig{
		HardwareEnabled:            true,
		ThirdPartyGatewayEnabled:   true,
		ThirdPartyGatewayToken:     "im-token",
		ThirdPartyGatewayLocalMode: &localMode,
	}}
	app.thirdPartyGateway = newThirdPartyGatewayManager(app)
	app.thirdPartyGateway.status = gatewayConnectionStatusConnected

	if err := app.SetThirdPartyGatewayLocalMode(true); err != nil {
		t.Fatalf("changing the IM gateway mode must not be blocked by hardware: %v", err)
	}
	if !app.GetThirdPartyGatewayLocalMode() {
		t.Fatal("IM gateway mode did not change")
	}

	app.StopThirdPartyGateway()
	if status := app.GetThirdPartyGatewayStatus(); status != gatewayConnectionStatusDisconnected.String() {
		t.Fatalf("IM gateway stop was blocked by hardware: status=%q", status)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.HardwareEnabled {
		t.Fatalf("IM gateway controls modified hardware transport: %#v", cfg)
	}
}

func TestHardwareActionsRequireEnabledHardware(t *testing.T) {
	localMode := false
	app := &App{testHomeDir: t.TempDir(), configCacheValid: true, configCache: corelib.AppConfig{
		ThirdPartyGatewayEnabled:   true,
		ThirdPartyGatewayToken:     "gateway-token",
		ThirdPartyGatewayLocalMode: &localMode,
	}}

	if _, err := app.CreateThirdPartyDevicePairing(); err == nil || !strings.Contains(err.Error(), "hardware is disabled") {
		t.Fatalf("pairing while hardware is disabled = %v, want hardware guard", err)
	}
	if err := app.SendHardwareVolume(42); err == nil || !strings.Contains(err.Error(), "hardware is disabled") {
		t.Fatalf("volume while hardware is disabled = %v, want hardware guard", err)
	}
	if app.thirdPartyGateway != nil {
		t.Fatal("disabled hardware action started a gateway")
	}
}

func TestHardwareEnableRequiresConnectedHub(t *testing.T) {
	localMode := true
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	app := &App{testHomeDir: t.TempDir(), configCacheValid: true, configCache: corelib.AppConfig{
		RemoteMachineID:            "machine-a",
		ThirdPartyGatewayHost:      "127.0.0.1",
		ThirdPartyGatewayPort:      port,
		ThirdPartyGatewayLocalMode: &localMode,
	}}

	status, err := app.SetHardwareEnabled(true)
	if err == nil || !strings.Contains(err.Error(), "Hub is not connected") {
		t.Fatalf("enabling hardware without Hub = status %q, err %v; want connected-Hub guard", status, err)
	}
	if status != gatewayConnectionStatusDisconnected.String() {
		t.Fatalf("hardware must not start the IM gateway: status=%q, want %q", status, gatewayConnectionStatusDisconnected)
	}
	cfg, loadErr := app.LoadConfig()
	if loadErr != nil {
		t.Fatalf("LoadConfig after rejected enable: %v", loadErr)
	}
	if cfg.HardwareEnabled || cfg.ThirdPartyGatewayEnabled || !cfg.IsThirdPartyGatewayLocalMode() || cfg.ThirdPartyGatewayToken != "" {
		t.Fatalf("rejected hardware enable mutated IM or hardware transport settings: %#v", cfg)
	}
}

// TestThirdPartyGatewayRateLimitsPairCodeGuessing locks the brute-force guard
// on the six-digit pairing endpoint.
//
// The pairing code is the only thing between an unauthenticated LAN caller and
// the gateway bearer token, and it stays valid for 30 minutes. Before the fix
// this handler had no window at all: a probe enumerated 123,457 codes in 0.72s
// and walked away with the token. The voice endpoint was rate limited; the code
// endpoint simply was not.
func TestThirdPartyGatewayRateLimitsPairCodeGuessing(t *testing.T) {
	manager := newThirdPartyGatewayManager(&App{})
	manager.pairings["123456"] = thirdPartyDevicePairing{
		Token:     "gateway-bearer-secret",
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}

	attempt := func(code, remoteAddr string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/device-gateway/v1/pair",
			bytes.NewBufferString(`{"pairCode":"`+code+`","clientId":"pet-a"}`))
		req.RemoteAddr = remoteAddr
		recorder := httptest.NewRecorder()
		manager.handleDevicePair(recorder, req)
		return recorder.Code
	}

	// A legitimate device must still be able to pair.
	if status := attempt("123456", "10.0.0.1:5000"); status != http.StatusCreated {
		t.Fatalf("valid pairing code was rejected: status=%d", status)
	}

	// An attacker enumerating the space must be cut off long before the 10^6
	// keyspace is reachable.
	limited := 0
	for i := 0; i < 5000; i++ {
		if status := attempt(fmt.Sprintf("%06d", i), "10.0.0.99:5000"); status == http.StatusTooManyRequests {
			limited = i + 1
			break
		}
	}
	if limited == 0 {
		t.Fatal("pairing code guessing was never rate limited")
	}
	if limited > 64 {
		t.Fatalf("rate limiter allowed %d guesses before cutting off", limited)
	}

	// The window is per source IP: another device must not be collateral damage.
	if status := attempt("999999", "10.0.0.2:5000"); status == http.StatusTooManyRequests {
		t.Fatal("a different source IP was blocked by another IP's attempts")
	}
}

// Malformed requests must not consume a well-behaved caller's window.
func TestThirdPartyGatewayMalformedPairRequestsDoNotConsumeWindow(t *testing.T) {
	manager := newThirdPartyGatewayManager(&App{})
	req := httptest.NewRequest(http.MethodPost, "/api/device-gateway/v1/pair", bytes.NewBufferString(`{"pairCode":"abc"}`))
	req.RemoteAddr = "10.0.0.3:5000"
	for i := 0; i < 200; i++ {
		recorder := httptest.NewRecorder()
		manager.handleDevicePair(recorder, httptest.NewRequest(http.MethodPost, "/api/device-gateway/v1/pair", bytes.NewBufferString(`{"pairCode":"abc"}`)))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("malformed pair request status=%d", recorder.Code)
		}
	}
	valid := httptest.NewRequest(http.MethodPost, "/api/device-gateway/v1/pair", bytes.NewBufferString(`{"pairCode":"123456","clientId":"pet-a"}`))
	valid.RemoteAddr = "10.0.0.3:5000"
	recorder := httptest.NewRecorder()
	manager.handleDevicePair(recorder, valid)
	if recorder.Code == http.StatusTooManyRequests {
		t.Fatal("malformed traffic exhausted the caller's pairing window")
	}
}
