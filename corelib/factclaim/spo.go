package factclaim

import (
	"regexp"
	"strings"
)

var spoPatterns = []struct {
	predicate string
	re        *regexp.Regexp
}{
	{"端口", regexp.MustCompile(`(?i)^(.{2,40}?)(?:端口|port)\s*(?:是|为|:|：)?\s*(\d{2,5})$`)},
	{"地址", regexp.MustCompile(`(?i)^(.{2,40}?)(?:地址|url|endpoint)\s*(?:是|为|:|：)?\s*(\S{4,120})$`)},
	{"uses", regexp.MustCompile(`(?i)^(.{2,80}?)\s+(?:uses|use|adopts|depends on|requires)\s+(.{2,120})$`)},
	{"使用", regexp.MustCompile(`^(.{2,40}?)(?:使用|采用|支持|依赖)([^，。；;,.!?！？]{2,80})$`)},
	{"是", regexp.MustCompile(`^(.{2,40}?)(?:是|为)([^，。；;,.!?！？]{2,80})$`)},
	{"属于", regexp.MustCompile(`^(.{2,40}?)(?:属于|归属)([^，。；;,.!?！？]{2,80})$`)},
}

func parseSPO(sent string) (subject, predicate, object string, ok bool) {
	sent = strings.TrimSpace(sent)
	sent = strings.Trim(sent, "。.;；")
	if sent == "" {
		return "", "", "", false
	}
	for _, p := range spoPatterns {
		m := p.re.FindStringSubmatch(sent)
		if len(m) < 3 {
			continue
		}
		sub := strings.TrimSpace(m[1])
		obj := strings.TrimSpace(m[2])
		if sub == "" || obj == "" || strings.EqualFold(sub, obj) {
			continue
		}
		if DetectPolarity(sent) != PolarityUnknown {
			continue
		}
		return sub, p.predicate, obj, true
	}
	return "", "", "", false
}
