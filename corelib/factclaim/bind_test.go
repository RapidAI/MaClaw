package factclaim

import "testing"

func TestBindToolSubjectIntersection(t *testing.T) {
	cases := []struct {
		name, tool, args, evidence, want string
		wantAlias                        string
		refuse                           []string
	}{
		{
			name:     "dest not bind address",
			args:     `{"command":"ping -c 1 -I 192.168.0.2 10.1.2.3"}`,
			evidence: "PING 10.1.2.3 (10.1.2.3): 100% packet loss",
			want:     "ip:10.1.2.3",
			refuse:   []string{"ip:192.168.0.2"},
		},
		{
			name:      "hostname plus resolved ip alias",
			args:      `{"command":"ping -c 1 jump.example.com"}`,
			evidence:  "PING jump.example.com (10.1.2.3): 100% packet loss",
			want:      "host:jump.example.com",
			wantAlias: "ip:10.1.2.3",
		},
		{
			name:     "json note ip is not a subject",
			args:     `{"command":"ping -c 1 jump.example.com","note":"fallback 8.8.8.8"}`,
			evidence: "PING jump.example.com (10.1.2.3): 100% packet loss",
			want:     "host:jump.example.com",
			refuse:   []string{"ip:8.8.8.8"},
		},
		{
			name:      "gateway in evidence is not a subject",
			args:      `{"command":"ping -c 1 jump.example.com"}`,
			evidence:  "PING jump.example.com (10.1.2.3):\nFrom 192.168.1.1 icmp_seq=1 Destination Host Unreachable\n100% packet loss",
			want:      "host:jump.example.com",
			wantAlias: "ip:10.1.2.3",
			refuse:    []string{"ip:192.168.1.1"},
		},
		{
			name:     "comment hostname is not in evidence",
			args:     `{"command":"ping -c 1 10.1.2.3 # see docs.example.com"}`,
			evidence: "PING 10.1.2.3 (10.1.2.3): 100% packet loss",
			want:     "ip:10.1.2.3",
			refuse:   []string{"host:docs.example.com"},
		},
		{
			name:     "later pipeline host is not in evidence",
			args:     `{"command":"ping -c 1 10.1.2.3 && curl -I https://docs.example.com"}`,
			evidence: "PING 10.1.2.3 (10.1.2.3): 100% packet loss",
			want:     "ip:10.1.2.3",
			refuse:   []string{"host:docs.example.com"},
		},
		{
			name:     "user@host is the host identity",
			args:     `{"command":"ssh user@jump.example.com"}`,
			evidence: "ssh: connect to host jump.example.com port 22: Connection refused",
			want:     "host:jump.example.com",
		},
		{
			name:     "host:port is the host identity",
			args:     `{"command":"nc -vz jump.example.com:22"}`,
			evidence: "nc: connect to jump.example.com port 22 (tcp) failed: Connection refused",
			want:     "host:jump.example.com",
		},
		{
			name:     "structured host field names the subject",
			args:     `{"host":"jump.example.com"}`,
			evidence: "ssh: connect to host jump.example.com port 22: Connection refused",
			want:     "host:jump.example.com",
			tool:     "ssh",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool := tc.tool
			if tool == "" {
				tool = "bash"
			}
			got, aliases, ok := BindToolSubject(tool, tc.args, tc.evidence)
			if !ok {
				t.Fatal("expected a bound subject")
			}
			if got != tc.want {
				t.Fatalf("primary=%q want %q aliases=%v", got, tc.want, aliases)
			}
			if tc.wantAlias != "" {
				found := false
				for _, a := range aliases {
					if a == tc.wantAlias {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("missing alias %q in %v", tc.wantAlias, aliases)
				}
			}
			for _, bad := range tc.refuse {
				if got == bad {
					t.Fatalf("refused identity %q used as primary", bad)
				}
				for _, a := range aliases {
					if a == bad {
						t.Fatalf("refused identity %q leaked into aliases %v", bad, aliases)
					}
				}
			}
		})
	}
}

func TestBindToolSubjectRequiresEvidenceMention(t *testing.T) {
	_, _, ok := BindToolSubject(
		"bash",
		`{"command":"ping -c 1 10.1.2.3"}`,
		"ls: no such file",
	)
	if ok {
		t.Fatal("evidence that never names the probe must not bind")
	}
}

func TestBindToolSubjectProbeCommandIgnoresIncidentalServerField(t *testing.T) {
	got, aliases, ok := BindToolSubject(
		"bash",
		`{"command":"ping -c 1 10.1.2.3","server":"docs.example.com"}`,
		"PING 10.1.2.3 (10.1.2.3): 100% packet loss\nalso see docs.example.com",
	)
	if !ok || got != "ip:10.1.2.3" {
		t.Fatalf("primary=%q ok=%v aliases=%v", got, ok, aliases)
	}
	for _, a := range aliases {
		if a == "host:docs.example.com" {
			t.Fatal("incidental server field must not become a ping subject")
		}
	}
}

func TestBindToolSubjectPrefersLastMentionedWhenSeveralOverlap(t *testing.T) {
	got, _, ok := BindToolSubject(
		"bash",
		`{"command":"ssh -J jump.example.com target.example.com"}`,
		"debug1: connecting to jump.example.com\nssh: connect to host target.example.com port 22: Connection refused",
	)
	if !ok || got != "host:target.example.com" {
		t.Fatalf("primary=%q ok=%v, want the host the evidence last talks about", got, ok)
	}
}

func TestBindToolSubjectIgnoresNonProbeCommands(t *testing.T) {
	_, _, ok := BindToolSubject(
		"bash",
		`{"command":"cat /tmp/10.1.2.3.log"}`,
		"PING 10.1.2.3 (10.1.2.3): 100% packet loss",
	)
	if ok {
		t.Fatal("reading a log is not an invocation of that IP")
	}
}

func TestLooksLikeProbeInvocationIgnoresNoteMentions(t *testing.T) {
	if LooksLikeProbeInvocation("bash", `{"command":"cat /tmp/10.1.2.3.log # then ping it"}`) {
		t.Fatal("ping in a comment is not the command program")
	}
	if !LooksLikeProbeInvocation("bash", `{"command":"timeout 5 ping -c 1 10.1.2.3"}`) {
		t.Fatal("timeout wrapping ping is still a ping invocation")
	}
	if LooksLikeProbeInvocation("bash", `{"command":"cat report.txt","note":"try ping tomorrow"}`) {
		t.Fatal("the word ping in a note is not a probe invocation")
	}
	if !LooksLikeProbeInvocation("bash", `{"command":"ping -c 1 10.1.2.3"}`) {
		t.Fatal("ping command must count as a probe")
	}
	if !LooksLikeProbeInvocation("ssh", `{"host":"jump.example.com"}`) {
		t.Fatal("ssh tool plus host field must count as a probe invocation")
	}
	if LooksLikeProbeInvocation("read_file", `{"address":"docs.example.com"}`) {
		t.Fatal("a generic address field on a non-probe tool is not a probe")
	}
}

func TestBindToolSubjectWindowsResolvedAddress(t *testing.T) {
	got, aliases, ok := BindToolSubject(
		"bash",
		`{"command":"ping jump.example.com"}`,
		"Pinging jump.example.com [10.1.2.3] with 32 bytes of data:\nRequest timed out.",
	)
	if !ok || got != "host:jump.example.com" {
		t.Fatalf("primary=%q ok=%v", got, ok)
	}
	found := false
	for _, a := range aliases {
		if a == "ip:10.1.2.3" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing bracket resolved IP in %v", aliases)
	}
}

func TestBindToolSubjectIgnoresGenericAddressOnNonProbeTool(t *testing.T) {
	_, _, ok := BindToolSubject(
		"read_file",
		`{"address":"docs.example.com","path":"README.md"}`,
		"docs.example.com is unreachable in the runbook",
	)
	if ok {
		t.Fatal("read_file with an address field must not become a reachability probe")
	}
}

func TestBindTimeoutSubjectSingleInvocation(t *testing.T) {
	got, _, ok := BindTimeoutSubject("bash", `{"command":"ping -c 1 10.1.2.3"}`)
	if !ok || got != "ip:10.1.2.3" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
	got, _, ok = BindTimeoutSubject("bash", `{"command":"ping -c 1 10.1.2.3 # see docs.example.com"}`)
	if !ok || got != "ip:10.1.2.3" {
		t.Fatalf("comment is not an invocation identity: got %q ok=%v", got, ok)
	}
	_, _, ok = BindTimeoutSubject("bash", `{"command":"ping -c 1 -I 192.168.0.2 10.1.2.3"}`)
	if ok {
		t.Fatal("timeout with two invocation IPs must not guess")
	}
}
