package httpapi

import (
	"os"
	"strconv"
	"strings"
	"sync"
)

// Mobile commercial plan caps (single source for bootstrap limits + entitlement flags).
// free: emergency only; official: entitled agent; service_card/paid: larger storage + shared employees.
// desktop_delegate: storage like free, agent path via desktop QR (not Hub official agent).
//
// Ops overrides (optional, MiB / count):
//
//	MACLAW_MOBILE_CAP_DOC_FREE_MIB
//	MACLAW_MOBILE_CAP_DOC_PAID_MIB
//	MACLAW_MOBILE_CAP_EXPORT_FREE
//	MACLAW_MOBILE_CAP_EXPORT_PAID
//	MACLAW_MOBILE_CAP_HUB_FILE_DOWNLOAD_MIB  (absolute hub_exec download cap; supports chunked pull)
//
// Runtime overrides (optional, via PUT /api/mobile/entitlements/caps with admin token):
// take precedence over env until process restart.

const (
	mobileCapDocFreeDefault         int64 = 100 * 1024 * 1024
	mobileCapDocPaidDefault         int64 = 500 * 1024 * 1024
	mobileCapExportFreeDefault            = 3
	mobileCapExportPaidDefault            = 10
	// Phase E: hub_exec can pull larger files via chunked base64 (default 32MiB absolute).
	mobileCapHubFileDownloadDefault int64 = 32 * 1024 * 1024
)

// Effective constants used by tests and callers (resolved at call time for env/runtime).
var (
	// Deprecated aliases for tests that compare against package-level names.
	mobileCapDocFree         = mobileCapDocFreeDefault
	mobileCapDocPaid         = mobileCapDocPaidDefault
	mobileCapExportFree      = mobileCapExportFreeDefault
	mobileCapExportPaid      = mobileCapExportPaidDefault
	mobileCapHubFileDownload = mobileCapHubFileDownloadDefault
)

// mobileCapsRuntime holds process-local ops overrides (admin PUT).
var mobileCapsRuntime = struct {
	sync.RWMutex
	DocFreeMib         int64
	DocPaidMib         int64
	ExportFree         int64
	ExportPaid         int64
	HubFileDownloadMib int64
}{}

func mobileEnvPositiveInt(name string) (int64, bool) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func mobileCapDocFreeBytes() int64 {
	mobileCapsRuntime.RLock()
	mib := mobileCapsRuntime.DocFreeMib
	mobileCapsRuntime.RUnlock()
	if mib > 0 {
		return mib * 1024 * 1024
	}
	if mib, ok := mobileEnvPositiveInt("MACLAW_MOBILE_CAP_DOC_FREE_MIB"); ok {
		return mib * 1024 * 1024
	}
	return mobileCapDocFreeDefault
}

func mobileCapDocPaidBytes() int64 {
	mobileCapsRuntime.RLock()
	mib := mobileCapsRuntime.DocPaidMib
	mobileCapsRuntime.RUnlock()
	if mib > 0 {
		return mib * 1024 * 1024
	}
	if mib, ok := mobileEnvPositiveInt("MACLAW_MOBILE_CAP_DOC_PAID_MIB"); ok {
		return mib * 1024 * 1024
	}
	return mobileCapDocPaidDefault
}

func mobileCapExportFreeN() int {
	mobileCapsRuntime.RLock()
	n := mobileCapsRuntime.ExportFree
	mobileCapsRuntime.RUnlock()
	if n > 0 && n <= 100 {
		return int(n)
	}
	if n, ok := mobileEnvPositiveInt("MACLAW_MOBILE_CAP_EXPORT_FREE"); ok && n <= 100 {
		return int(n)
	}
	return mobileCapExportFreeDefault
}

func mobileCapExportPaidN() int {
	mobileCapsRuntime.RLock()
	n := mobileCapsRuntime.ExportPaid
	mobileCapsRuntime.RUnlock()
	if n > 0 && n <= 100 {
		return int(n)
	}
	if n, ok := mobileEnvPositiveInt("MACLAW_MOBILE_CAP_EXPORT_PAID"); ok && n <= 100 {
		return int(n)
	}
	return mobileCapExportPaidDefault
}

func mobileCapHubFileDownloadBytes() int64 {
	mobileCapsRuntime.RLock()
	mib := mobileCapsRuntime.HubFileDownloadMib
	mobileCapsRuntime.RUnlock()
	if mib > 0 {
		return mib * 1024 * 1024
	}
	if mib, ok := mobileEnvPositiveInt("MACLAW_MOBILE_CAP_HUB_FILE_DOWNLOAD_MIB"); ok {
		return mib * 1024 * 1024
	}
	return mobileCapHubFileDownloadDefault
}

// mobileCapsRuntimeSnapshot returns active process overrides (0 = unset).
func mobileCapsRuntimeSnapshot() map[string]any {
	mobileCapsRuntime.RLock()
	defer mobileCapsRuntime.RUnlock()
	return map[string]any{
		"doc_free_mib":          mobileCapsRuntime.DocFreeMib,
		"doc_paid_mib":          mobileCapsRuntime.DocPaidMib,
		"export_free":           mobileCapsRuntime.ExportFree,
		"export_paid":           mobileCapsRuntime.ExportPaid,
		"hub_file_download_mib": mobileCapsRuntime.HubFileDownloadMib,
	}
}

// mobileCapsRuntimeApply applies partial overrides; zero values leave existing fields.
// Negative clears a field back to env/default chain.
func mobileCapsRuntimeApply(docFree, docPaid, exportFree, exportPaid, hubDL int64) {
	mobileCapsRuntime.Lock()
	defer mobileCapsRuntime.Unlock()
	if docFree > 0 {
		mobileCapsRuntime.DocFreeMib = docFree
	} else if docFree < 0 {
		mobileCapsRuntime.DocFreeMib = 0
	}
	if docPaid > 0 {
		mobileCapsRuntime.DocPaidMib = docPaid
	} else if docPaid < 0 {
		mobileCapsRuntime.DocPaidMib = 0
	}
	if exportFree > 0 {
		mobileCapsRuntime.ExportFree = exportFree
	} else if exportFree < 0 {
		mobileCapsRuntime.ExportFree = 0
	}
	if exportPaid > 0 {
		mobileCapsRuntime.ExportPaid = exportPaid
	} else if exportPaid < 0 {
		mobileCapsRuntime.ExportPaid = 0
	}
	if hubDL > 0 {
		mobileCapsRuntime.HubFileDownloadMib = hubDL
	} else if hubDL < 0 {
		mobileCapsRuntime.HubFileDownloadMib = 0
	}
}

// mobilePlanCaps is the effective product matrix for a viewer plan.
type mobilePlanCaps struct {
	Plan                   string
	DocumentQuotaBytes     int64
	MaxUploadBytes         int64
	MaxExportJobs          int
	SharedEmployees        bool
	HubSSHExec             bool
	MobileAgent            bool
	DocumentAI             bool
	HubFileDownloadMaxBytes int64
}

// mobilePlanCapsFor builds caps from plan label + grant + official entitlement.
func mobilePlanCapsFor(plan string, grant mobileServiceGrantSnapshot, officialEntitled bool) mobilePlanCaps {
	p := strings.ToLower(strings.TrimSpace(plan))
	if p == "" {
		p = "free"
	}
	// Paid boost: service card / spendable credits (same as historical quota rule).
	paidStorage := grant.Active && (grant.HasCardGrant || grant.CreditsAvailable > 0)
	if p == "service_card" || p == "paid" {
		paidStorage = true
	}

	docFree := mobileCapDocFreeBytes()
	docPaid := mobileCapDocPaidBytes()
	exportFree := mobileCapExportFreeN()
	exportPaid := mobileCapExportPaidN()
	// Keep package vars in sync for older tests that read them after env is set.
	mobileCapDocFree = docFree
	mobileCapDocPaid = docPaid
	mobileCapExportFree = exportFree
	mobileCapExportPaid = exportPaid
	mobileCapHubFileDownload = mobileCapHubFileDownloadBytes()

	caps := mobilePlanCaps{
		Plan:                    p,
		DocumentQuotaBytes:      docFree,
		MaxUploadBytes:          docFree,
		MaxExportJobs:           exportFree,
		SharedEmployees:         false,
		HubSSHExec:              true,
		MobileAgent:             officialEntitled || grant.Active,
		DocumentAI:              officialEntitled || grant.Active,
		HubFileDownloadMaxBytes: mobileCapHubFileDownload,
	}

	switch p {
	case "service_card", "paid":
		caps.DocumentQuotaBytes = docPaid
		caps.MaxUploadBytes = docPaid
		caps.MaxExportJobs = exportPaid
		caps.SharedEmployees = true
		caps.MobileAgent = true
		caps.DocumentAI = true
	case "official":
		// Official LLM path: agent on; storage upgrades only with credits/card.
		caps.MobileAgent = true
		caps.DocumentAI = true
		if paidStorage {
			caps.DocumentQuotaBytes = docPaid
			caps.MaxUploadBytes = docPaid
			caps.MaxExportJobs = exportPaid
			caps.SharedEmployees = true
		}
	case "desktop_delegate":
		// Third-party desktop QR: no Hub agent entitlement; own employees only.
		caps.MobileAgent = false
		caps.DocumentAI = false
		caps.SharedEmployees = false
		if paidStorage {
			caps.DocumentQuotaBytes = docPaid
			caps.MaxUploadBytes = docPaid
			caps.MaxExportJobs = exportPaid
		}
	default: // free
		if paidStorage {
			// Active grant without plan label still gets paid storage (card redeem race).
			caps.DocumentQuotaBytes = docPaid
			caps.MaxUploadBytes = docPaid
			caps.MaxExportJobs = exportPaid
			caps.SharedEmployees = mobileSharedEmployeesFromGrant(grant, p)
			caps.MobileAgent = grant.Active
			caps.DocumentAI = grant.Active
		}
	}

	// Shared employees: keep matrix + grant helper in sync.
	if mobileSharedEmployeesFromGrant(grant, p) {
		caps.SharedEmployees = true
	}
	return caps
}

func (c mobilePlanCaps) toEntitlementMap(grant mobileServiceGrantSnapshot, entitled bool) map[string]any {
	return map[string]any{
		"mobile_official":            entitled,
		"mobile_agent":               c.MobileAgent,
		"document_ai":                c.DocumentAI,
		"shared_employees":           c.SharedEmployees,
		"hub_ssh_exec":               c.HubSSHExec,
		"plan":                       c.Plan,
		"service_active":             grant.Active,
		"credits_available":          grant.CreditsAvailable,
		"credits_remaining":          grant.CreditsRemaining,
		"service_group_count":        grant.ServiceGroupCount,
		"has_service_card_grant":     grant.HasCardGrant,
		"document_quota_bytes":       c.DocumentQuotaBytes,
		"max_upload_bytes":           c.MaxUploadBytes,
		"max_export_jobs":            c.MaxExportJobs,
		"hub_file_download_max_bytes": c.HubFileDownloadMaxBytes,
	}
}
