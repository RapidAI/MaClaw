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
