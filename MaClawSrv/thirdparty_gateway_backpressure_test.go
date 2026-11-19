package main

import (
	"testing"
)

func TestThirdPartyIncomingBackpressureDoesNotMarkRejectedEvent(t *testing.T) {
	manager := newSrvThirdPartyGatewayManager(nil)
	manager.incomingSlots = make(chan struct{}, 1)
	manager.incomingSlots <- struct{}{}
	req := srvThirdPartyIncomingRequest{ClientID: "client-a", EventID: "event-retry"}
	principal := srvThirdPartyPrincipal{}

	duplicate, _, admitted := manager.scheduleIncoming(principal.Principal, "client-key", req, "mc-1")
	if admitted || duplicate {
		t.Fatalf("saturated schedule duplicate=%t admitted=%t", duplicate, admitted)
	}
	manager.mu.Lock()
	_, seen := manager.clients["client-key"]
	manager.mu.Unlock()
	if seen {
		t.Fatal("rejected event created dedupe state and would be dropped on retry")
	}
}

func TestThirdPartyDuplicateIncomingReleasesAdmissionSlot(t *testing.T) {
	manager := newSrvThirdPartyGatewayManager(nil)
	manager.incomingSlots = make(chan struct{}, 1)
	manager.markIncoming("client-key", "event-duplicate", "mc-existing")
	req := srvThirdPartyIncomingRequest{ClientID: "client-a", EventID: "event-duplicate"}
	principal := srvThirdPartyPrincipal{}

	duplicate, storedID, admitted := manager.scheduleIncoming(principal.Principal, "client-key", req, "mc-new")
	if !admitted || !duplicate || storedID != "mc-existing" {
		t.Fatalf("duplicate schedule storedID=%q duplicate=%t admitted=%t", storedID, duplicate, admitted)
	}
	select {
	case manager.incomingSlots <- struct{}{}:
	default:
		t.Fatal("duplicate event retained its admission slot")
	}
}
