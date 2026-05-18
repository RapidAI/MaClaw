package needledata

import "strings"

func cleanJSONLLine(line string) string {
	return strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
}
