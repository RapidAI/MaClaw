package desktoppool

import "testing"

func TestIntranetDesktopForcesDirectEgress(t *testing.T) {
	rec := record{
		Servers: []Server{
			{ID: "own", Intranet: true},
			{ID: "cloud"},
		},
		Assignments: []Assignment{{Scope: ScopeUser, TargetID: "u1", ServerID: "own"}},
	}
	if !intranetDesktop(rec, "u1") || !serverIntranet(rec.Servers[0]) {
		t.Fatal("a GUI-installed desktopd is an intranet desktop")
	}
	view := applyIntranetProxy(rec, "u1", ProxyModeView{Mode: ProxyModeSmart})
	if view.Mode != ProxyModeDirect || !view.Locked || !view.Intranet {
		t.Fatalf("intranet egress = %+v", view)
	}

	owned := record{
		Servers:     []Server{{ID: "old", OwnerUserID: "u1"}},
		Assignments: []Assignment{{Scope: ScopeUser, TargetID: "u1", ServerID: "old"}},
	}
	view = applyIntranetProxy(owned, "u1", ProxyModeView{Mode: ProxyModeSmart})
	if view.Mode != ProxyModeDirect || !view.Intranet {
		t.Fatalf("owned desktop without the new flag = %+v", view)
	}

	rec.Assignments[0].ServerID = "cloud"
	if intranetDesktop(rec, "u1") {
		t.Fatal("a tenant cloud desktop stays on the cloud proxy policy")
	}
	view = applyIntranetProxy(rec, "u1", ProxyModeView{Mode: ProxyModeSmart})
	if view.Mode != ProxyModeSmart || view.Locked || view.Intranet {
		t.Fatalf("cloud egress = %+v", view)
	}
}
