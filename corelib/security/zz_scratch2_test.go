package security

import (
	"fmt"
	"testing"
)

func TestScratchDumpFlag(t *testing.T) {
	s := `apikey=[REDACTED] apisecret:compact-secret API Key = display-key API Secret: display-secret {"apikey":"[REDACTED]","apisecret":"[REDACTED]"} path=C:\data`
	for _, m := range auditSensitiveFlagRe.FindAllStringSubmatchIndex(s, -1) {
		fmt.Printf("MATCH whole=%q  g1=%q\n", s[m[0]:m[1]], s[m[2]:m[3]])
	}
}
