package apiroutes

import "strings"

var adminAPIPrefixes = []string{
	"/admin/roles",
	"/admin/colleagues",
	"/admin/memories",
	"/admin/capabilities",
	"/admin/capabilities-import",
	"/admin/collaborations",
	"/admin/workflows",
	"/admin/workflow-instances",
	"/admin/workflow-design",
	"/admin/audit",
	"/admin/a2a",
	"/admin/config-bundles",
	"/admin/security",
	"/admin/model-endpoints",
	"/admin/model-routing-policies",
	"/admin/im-config",
	"/admin/iworker",
	"/admin/cloud",
	"/admin/diworker-auth",
	"/admin/compute",
	"/admin/recommend",
	"/admin/bootstrap",
	"/admin/goalwatch",
	"/admin/executive",
	"/admin/profile",
	"/admin/password",
	"/admin/system",
}

func IsAdminAPIPath(path string) bool {
	for _, prefix := range adminAPIPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
