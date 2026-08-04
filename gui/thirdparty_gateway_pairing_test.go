package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
