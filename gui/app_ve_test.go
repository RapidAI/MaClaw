package main

import (
	"encoding/json"
	"testing"
)

func TestResolveVEInviteMachineIDPrefersMachineID(t *testing.T) {
	employees := []VirtualEmployeeEntry{
		{ID: "ve_machine-a", MachineID: "machine-a", Name: "Legal Researcher"},
		{ID: "ve_machine-b", Name: "Fallback Analyst"},
	}
	if got := resolveVEInviteMachineID(employees, "ve_machine-a"); got != "machine-a" {
		t.Fatalf("resolved id = %q, want machine-a", got)
	}
	if got := resolveVEInviteMachineID(employees, "machine-a"); got != "machine-a" {
		t.Fatalf("resolved machine id = %q, want machine-a", got)
	}
	if got := resolveVEInviteMachineID(employees, "ve_machine-b"); got != "ve_machine-b" {
		t.Fatalf("fallback id = %q, want ve_machine-b", got)
	}
	if got := resolveVEInviteMachineID(employees, "unknown"); got != "unknown" {
		t.Fatalf("unknown id = %q, want unknown", got)
	}
}

func TestVirtualEmployeeEntryDecodesAccessLists(t *testing.T) {
	var resp VEStatusResponse
	raw := []byte(`{"registered":true,"employee":{"id":"ve-1","name":"Legal Researcher","skill_description":"contracts","access_policy":"whitelist","status":"active","whitelist":["user-a"],"blacklist":["user-b"]}}`)
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal VE status: %v", err)
	}
	if resp.Employee == nil || len(resp.Employee.Whitelist) != 1 || resp.Employee.Whitelist[0] != "user-a" {
		t.Fatalf("whitelist not decoded: %+v", resp.Employee)
	}
	if len(resp.Employee.Blacklist) != 1 || resp.Employee.Blacklist[0] != "user-b" {
		t.Fatalf("blacklist not decoded: %+v", resp.Employee)
	}
}

func TestVEGroupKeyNormalizesParticipantSet(t *testing.T) {
	left := veGroupKey([]string{" ve-b ", "ve-a", "ve-b", ""})
	right := veGroupKey([]string{"ve-a", "ve-b"})
	if left != right || left != "ve-a|ve-b" {
		t.Fatalf("group key = %q, want %q", left, "ve-a|ve-b")
	}
}

func TestCacheVESessionIgnoresBlankValues(t *testing.T) {
	app := &App{}
	app.cacheVESession(" ", "session-1")
	app.cacheVESession("ve-a", " ")
	if _, ok := app.veSessionCache.Load("ve-a"); ok {
		t.Fatal("blank session id should not be cached")
	}
	app.cacheVESession(" ve-a ", " session-1 ")
	if got, ok := app.veSessionCache.Load("ve-a"); !ok || got != "session-1" {
		t.Fatalf("cached session = %#v ok=%v", got, ok)
	}
}

func TestIsVEConsultationActiveJSONReadsNestedStatus(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "top-level open", raw: `{"status":"open"}`, want: true},
		{name: "discussion open", raw: `{"discussion":{"status":"open"}}`, want: true},
		{name: "session open", raw: `{"session":{"status":"open"}}`, want: true},
		{name: "closed", raw: `{"discussion":{"status":"closed"},"session":{"status":"open"}}`, want: false},
		{name: "unknown", raw: `{"discussion":{"status":""}}`, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isVEConsultationActiveJSON([]byte(tc.raw)); got != tc.want {
				t.Fatalf("active = %v, want %v", got, tc.want)
			}
		})
	}
}
