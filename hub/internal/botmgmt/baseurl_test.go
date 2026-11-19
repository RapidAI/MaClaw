package botmgmt

import "testing"

func TestValidateBaseURLRejectsAVirtualNodeHost(t *testing.T) {
	for _, raw := range []string{
		"http://srv.abc.node.invalid",
		"https://Desktopd.ABC.Node.Invalid/v1",
		"http://node.invalid",
		"http://srv.abc.node.invalid.",
	} {
		if err := validateBaseURL(raw); err == nil {
			t.Fatalf("%s was accepted", raw)
		}
	}
	if err := validateBaseURL("https://maclawsrv.example/v1"); err != nil {
		t.Fatal(err)
	}
}
