package factclaim

import "testing"

func TestExtractReachabilityAndAddressAreDifferentPredicates(t *testing.T) {
	got := Extract("跳板机 10.9.8.7 当前可达。API 地址是 https://old.example.com")
	var hasReach, hasAddr bool
	for _, c := range got {
		switch c.Predicate {
		case PredicateReachability:
			hasReach = true
			if c.Value != "reachable" {
				t.Fatalf("reachability value=%q", c.Value)
			}
		case "地址":
			hasAddr = true
			if c.Value != "https://old.example.com" {
				t.Fatalf("address value=%q", c.Value)
			}
		}
	}
	if !hasReach || !hasAddr {
		t.Fatalf("expected both predicates, got %#v", got)
	}
}

func TestContradictSamePredicateDifferentValue(t *testing.T) {
	old := Extract("API 地址是 https://old.example.com")
	newer := Extract("API 地址是 https://new.example.com")
	if len(old) == 0 || len(newer) == 0 {
		t.Fatalf("extract failed old=%#v new=%#v", old, newer)
	}
	if !Contradict(newer[0], old[0]) {
		t.Fatal("same predicate different value must contradict")
	}
}

func TestIsolatedRejectsMixedPredicates(t *testing.T) {
	stored := Extract("跳板机 10.9.8.7 当前可达。API 地址是 https://old.example.com")
	newer := Extract("10.9.8.7 当前不可达")
	if Isolated(newer, stored) {
		t.Fatal("a note that also asserts an address must not be rewritten as a whole")
	}
}

func TestContradictDifferentPredicateDoesNotClobber(t *testing.T) {
	reach := Claim{Subject: "ip:10.9.8.7", Predicate: PredicateReachability, Value: "unreachable", Aliases: []string{"ip:10.9.8.7"}}
	addr := Claim{Subject: "ip:10.9.8.7", Predicate: "地址", Value: "https://api.example.com", Aliases: []string{"ip:10.9.8.7"}}
	if Contradict(reach, addr) || Contradict(addr, reach) {
		t.Fatal("reachability must not clobber an address fact on the same host")
	}
}

func TestSubjectsOverlapIgnoresIncidentalEntitiesInProse(t *testing.T) {
	down := Extract("跳板机 10.9.8.7 当前不可达，备用 DNS 8.8.8.8")
	dns := Extract("8.8.8.8 当前可达")
	if len(down) == 0 || len(dns) == 0 {
		t.Fatal("extract failed")
	}
	if SubjectsOverlap(down[0], dns[0]) {
		t.Fatal("a reachability claim must not cover every IP mentioned in the same sentence")
	}
}

func TestContradictReachabilityPolarity(t *testing.T) {
	up := Extract("服务器 10.9.8.7 当前可达")
	down := Extract("服务器 10.9.8.7 当前不可达")
	if len(up) == 0 || len(down) == 0 {
		t.Fatal("extract failed")
	}
	if !Contradict(down[0], up[0]) {
		t.Fatal("reachable vs unreachable must contradict")
	}
}
