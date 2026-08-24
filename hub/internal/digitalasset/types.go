package digitalasset

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// Feature flag key in hub config / system settings.
const FeatureEnabledKey = "digital_assets.enabled"

// Default tenant digital_assets settings keys (JSON under tenants.settings_json or system).
const SettingsKey = "digital_assets"

// ACLMode values.
const (
	ACLModeAllMembers = "all_members"
	ACLModeRestricted = "restricted"
	MaxACLDepartments = 512
)

// LibraryKind values.
const (
	LibraryKindBusiness  = "business"
	LibraryKindTechnical = "technical"

	SubmissionKindBusiness = "business_knowledge"
	SubmissionKindCoding   = "coding_experience"

	MaxSubmissionItems        = 200
	MaxSubmissionPackageBytes = 20 << 20
)

// PackageFormat for sync packages.
const PackageFormatJSONL = "knowledge_snapshot_jsonl_v1"

// TenantSettings holds tenant-level digital asset configuration.
type TenantSettings struct {
	Enabled                  bool     `json:"enabled"`
	SyncEnabled              bool     `json:"sync_enabled"`
	RevokePolicy             string   `json:"revoke_policy"` // keep_local | purge_local
	AllowAdminImportPrivate  bool     `json:"allow_admin_import_private_shares"`
	LocalDirAllowlist        []string `json:"local_dir_allowlist"`
	MaxLibraryBytes          int64    `json:"max_library_bytes"`
	MaxLibraries             int      `json:"max_libraries"`
	MaxArchiveUploadBytes    int64    `json:"max_archive_upload_bytes"`
	MaxArchiveExtractedBytes int64    `json:"max_archive_extracted_bytes"`
	MaxArchiveFileCount      int      `json:"max_archive_file_count"`
	MaxBrowserDirFiles       int      `json:"max_browser_dir_files"`
	MaxBrowserDirBytes       int64    `json:"max_browser_dir_bytes"`
	MaxOpenLibraries         int      `json:"max_open_libraries"`
	ChangelogKeepRevs        int      `json:"changelog_keep_revs"`
	ChangelogKeepDays        int      `json:"changelog_keep_days"`
	PerTenantConcurrentPulls int      `json:"per_tenant_concurrent_pulls"`
	PerUserPullRPM           int      `json:"per_user_pull_rpm"`
	ArchiveIncludeExtensions []string `json:"archive_include_extensions"`
	ArchiveDenyExtensions    []string `json:"archive_deny_extensions"`
}

// DefaultTenantSettings returns product defaults from the design doc.
func DefaultTenantSettings() TenantSettings {
	return TenantSettings{
		Enabled:                  false,
		SyncEnabled:              true,
		RevokePolicy:             "keep_local",
		AllowAdminImportPrivate:  true,
		LocalDirAllowlist:        nil,
		MaxLibraryBytes:          5 * 1024 * 1024 * 1024, // 5GB
		MaxLibraries:             50,
		MaxArchiveUploadBytes:    200 * 1024 * 1024,
		MaxArchiveExtractedBytes: 1024 * 1024 * 1024,
		MaxArchiveFileCount:      5000,
		MaxBrowserDirFiles:       2000,
		MaxBrowserDirBytes:       500 * 1024 * 1024,
		MaxOpenLibraries:         16,
		ChangelogKeepRevs:        50,
		ChangelogKeepDays:        30,
		PerTenantConcurrentPulls: 8,
		PerUserPullRPM:           30,
		ArchiveIncludeExtensions: []string{".pdf", ".docx", ".doc", ".md", ".txt", ".xlsx", ".xls", ".pptx", ".ppt", ".html", ".htm", ".csv"},
		ArchiveDenyExtensions:    []string{".exe", ".dll", ".so", ".bat", ".cmd", ".ps1", ".sh", ".msi", ".dmg"},
	}
}

// ACL is the per-library access control structure.
type ACL struct {
	Mode        string   `json:"mode"`
	Departments []string `json:"departments"`
}

// ParseACL builds ACL from library JSON fields. The third parameter remains for
// compatibility with the persisted legacy acl_users_json column, which is ignored.
func ParseACL(mode, deptsJSON, _ string) ACL {
	acl := ACL{Mode: strings.TrimSpace(mode)}
	if acl.Mode == "" {
		acl.Mode = ACLModeAllMembers
	}
	_ = json.Unmarshal([]byte(strings.TrimSpace(deptsJSON)), &acl.Departments)
	acl.Departments = normalizeACLDepartments(acl.Departments)
	if acl.Mode == ACLModeAllMembers {
		acl.Departments = []string{}
	}
	return acl
}

func normalizeACLDepartments(departments []string) []string {
	seen := make(map[string]struct{}, len(departments))
	out := make([]string, 0, len(departments))
	for _, department := range departments {
		department = strings.TrimSpace(department)
		if department == "" {
			continue
		}
		if _, ok := seen[department]; ok {
			continue
		}
		seen[department] = struct{}{}
		out = append(out, department)
	}
	sort.Strings(out)
	return out
}

// Fingerprint returns sha256 of canonical ACL JSON (mode + sorted departments).
func (a ACL) Fingerprint() string {
	mode := strings.TrimSpace(a.Mode)
	if mode == "" {
		mode = ACLModeAllMembers
	}
	depts := normalizeACLDepartments(a.Departments)
	if mode == ACLModeAllMembers {
		depts = []string{}
	}
	// Compact fixed key order.
	type canon struct {
		Mode        string   `json:"mode"`
		Departments []string `json:"departments"`
	}
	b, _ := json.Marshal(canon{Mode: mode, Departments: depts})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// EncodeACLJSON returns departments JSON and clears obsolete user grants.
func EncodeACLJSON(acl ACL) (deptsJSON, usersJSON string) {
	if acl.Departments == nil {
		acl.Departments = []string{}
	}
	db, _ := json.Marshal(acl.Departments)
	return string(db), "[]"
}

// LibraryView is the admin/user API shape for a library.
type LibraryView struct {
	ID             string   `json:"id"`
	TenantID       string   `json:"tenant_id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Status         string   `json:"status"`
	SyncEnabled    bool     `json:"sync_enabled"`
	ACLMode        string   `json:"acl_mode"`
	Departments    []string `json:"departments"`
	ACLFingerprint string   `json:"acl_fingerprint"`
	ContentRev     int64    `json:"content_rev"`
	ContentHash    string   `json:"content_hash"`
	SourceCount    int64    `json:"source_count"`
	CardCount      int64    `json:"card_count"`
	ByteSize       int64    `json:"byte_size"`
	LibraryKind        string   `json:"library_kind"`
	AcceptsSubmissions bool     `json:"accepts_submissions"`
	CreatedBy          string   `json:"created_by"`
	UpdatedBy          string   `json:"updated_by"`
	CreatedAt          string   `json:"created_at"`
	UpdatedAt          string   `json:"updated_at"`
}

// SubmissionView is the admin/user API shape for a contribution.
type SubmissionView struct {
	ID              string `json:"id"`
	TenantID        string `json:"tenant_id"`
	LibraryID       string `json:"library_id"`
	SubmitterUserID string `json:"submitter_user_id"`
	SubmitterEmail  string `json:"submitter_email"`
	Kind            string `json:"kind"`
	Status          string `json:"status"`
	Title           string `json:"title"`
	Summary         string `json:"summary"`
	ItemCount       int    `json:"item_count"`
	PackageBytes    int64  `json:"package_bytes"`
	ReviewNote      string `json:"review_note,omitempty"`
	ReviewedAt      string `json:"reviewed_at,omitempty"`
	ImportJobID     string `json:"import_job_id,omitempty"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
	PreviewTitles   []string `json:"preview_titles,omitempty"`
}

// FormatTime formats a time for API JSON.
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
