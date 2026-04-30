package skillmarket

import (
	"strings"
	"time"
)

// Skill is the shared catalog/search contract used by HubCenter, Maclaw GUI,
// iWorkerCenter, and iWorkerCloud. It intentionally follows the existing
// HubCenter skillmarket search shape instead of introducing another market DTO.
type Skill struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Tags           []string `json:"tags,omitempty"`
	Score          float64  `json:"score,omitempty"`
	Price          int64    `json:"price,omitempty"`
	Status         string   `json:"status,omitempty"`
	AvgRating      float64  `json:"avg_rating,omitempty"`
	DownloadCount  int      `json:"download_count,omitempty"`
	Downloads      int      `json:"downloads,omitempty"`
	Version        string   `json:"version,omitempty"`
	Author         string   `json:"author,omitempty"`
	AuthorEmail    string   `json:"author_email,omitempty"`
	SourceCenterID string   `json:"source_center_id,omitempty"`
	CreatedAt      string   `json:"created_at,omitempty"`
	UpdatedAt      string   `json:"updated_at,omitempty"`
	Category       string   `json:"category,omitempty"`
	RiskLevel      string   `json:"risk_level,omitempty"`
	PackageFormat  string   `json:"package_format,omitempty"`
	PackageSHA256  string   `json:"package_sha256,omitempty"`
	PackageSize    int64    `json:"package_size,omitempty"`
}

type SearchResponse struct {
	Results []Skill `json:"results"`
}

type CatalogResponse struct {
	Skills []Skill `json:"skills"`
}

type SkillInput struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Description          string   `json:"description"`
	Category             string   `json:"category"`
	Version              string   `json:"version"`
	Tags                 []string `json:"tags"`
	RiskLevel            string   `json:"risk_level"`
	Status               string   `json:"status"`
	Price                int64    `json:"price,omitempty"`
	Author               string   `json:"author,omitempty"`
	AuthorEmail          string   `json:"author_email,omitempty"`
	SourceCenterID       string   `json:"source_center_id,omitempty"`
	PackageFormat        string   `json:"package_format,omitempty"`
	PackageContentBase64 string   `json:"package_content_base64,omitempty"`
}

type PackageDownload struct {
	SkillID        string `json:"skill_id"`
	Version        string `json:"version"`
	Format         string `json:"format"`
	SHA256         string `json:"sha256"`
	Size           int64  `json:"size"`
	ContentBase64  string `json:"content_base64"`
	ContentType    string `json:"content_type,omitempty"`
	SourceContract string `json:"source_contract,omitempty"`
}

func NormalizeStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "active", "published", "trial":
		return "active"
	case "disabled", "draft", "rejected", "pending":
		return strings.TrimSpace(status)
	default:
		return "active"
	}
}

func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func FormatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
