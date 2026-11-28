package tensor

import "strings"

// parseARMCPUInfoFeatures reads the Features line of /proc/cpuinfo.
// asimddp is FEAT_DotProd (SDOT); i8mm is FEAT_I8MM (SMMLA). Both show up
// as whole tokens on Android and Linux, including when the Go runtime could
// not see the auxiliary vector (c-shared libraries on Android).
func parseARMCPUInfoFeatures(text string) (dotprod, i8mm bool) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 8 {
			continue
		}
		head := line
		if len(head) > 8 {
			head = head[:8]
		}
		if !strings.EqualFold(head, "features") {
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		for _, tok := range strings.Fields(line[colon+1:]) {
			switch tok {
			case "asimddp":
				dotprod = true
			case "i8mm":
				i8mm = true
			}
		}
		if dotprod && i8mm {
			return
		}
	}
	return
}
