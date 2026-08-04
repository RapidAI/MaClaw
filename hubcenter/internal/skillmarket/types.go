package skillmarket

import "time"

// ── SkillMarket User ────────────────────────────────────────────────────

// SkillMarketUser 代表 SkillMarket 中的一个用户账户。
type SkillMarketUser struct {
	ID                string    `json:"id"`
	Email             string    `json:"email"`
	Status            string    `json:"status"`             // "unverified", "verified"
	VerifyMethod      string    `json:"verify_method"`      // "email", "phone", ""
	Credits           int64     `json:"credits"`            // 买家可用余额
	SettledCredits    int64     `json:"settled_credits"`    // 卖家已交付收益（可提现）
	PendingSettlement int64     `json:"pending_settlement"` // 卖家待交付收益（不可提现）
	Debt              int64     `json:"debt"`               // 退款负债
	VoucherCount      int       `json:"voucher_count"`      // 体验券剩余次数
	VoucherExpiresAt  time.Time `json:"voucher_expires_at"` // 体验券过期时间
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	VerifiedAt        time.Time `json:"verified_at,omitempty"`
}

// ProblemReport is an authenticated client diagnostic submission. Reporter
// contact data is deliberately only returned from admin endpoints.
type ProblemReport struct {
	ID              string   `json:"id"`
	ReporterUserID  string   `json:"reporter_user_id"`
	ReporterContact string   `json:"reporter_contact,omitempty"`
	OSVersion       string   `json:"os_version"`
	GUIVersion      string   `json:"gui_version,omitempty"`
	Description     string   `json:"description"`
	Status          string   `json:"status"`
	AdminNote       string   `json:"admin_note,omitempty"`
	DiagnosticsPath string   `json:"diagnostics_path,omitempty"`
	ScreenshotPaths []string `json:"screenshot_paths,omitempty"`
	// OriginURL is the HubCenter that owns the attachment files. HA peers sync
	// this metadata but never copy diagnostics or screenshots.
	OriginURL  string    `json:"origin_url,omitempty"`
	ArchivedAt time.Time `json:"archived_at,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ── Credits Transaction ─────────────────────────────────────────────────

// CreditsTransaction 记录一笔 Credits 交易。
type CreditsTransaction struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Type        string    `json:"type"` // purchase, earning, topup, withdraw, upgrade, refund, platform_fee
	Amount      int64     `json:"amount"`
	Balance     int64     `json:"balance"` // 交易后余额
	SkillID     string    `json:"skill_id,omitempty"`
	PurchaseID  string    `json:"purchase_id,omitempty"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreditRedeemCard is a single-use card that adds buyer Credits to an account.
// A card is never deleted: revocation, export, and redemption remain auditable.
type CreditRedeemCard struct {
	ID               string     `json:"id"`
	Code             string     `json:"code"`
	Credits          int64      `json:"credits"`
	Status           string     `json:"status"` // active, redeemed, revoked
	ExportedAt       *time.Time `json:"exported_at,omitempty"`
	ExportedBy       string     `json:"exported_by,omitempty"`
	IssuedBy         string     `json:"issued_by,omitempty"`
	IssuedAt         time.Time  `json:"issued_at"`
	RedeemedByUserID string     `json:"redeemed_by_user_id,omitempty"`
	RedeemedByEmail  string     `json:"redeemed_by_email,omitempty"`
	RedeemedAt       *time.Time `json:"redeemed_at,omitempty"`
	RevokedBy        string     `json:"revoked_by,omitempty"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
}

// ── Skill Submission ────────────────────────────────────────────────────

// SkillSubmission 记录一次 Skill 上传提交。
type SkillSubmission struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	UserID      string    `json:"user_id"`
	SkillID     string    `json:"skill_id,omitempty"`
	Fingerprint string    `json:"fingerprint,omitempty"` // uploader_email + skill_name
	Status      string    `json:"status"`                // pending, processing, success, failed
	ZipPath     string    `json:"zip_path"`
	ErrorMsg    string    `json:"error_msg,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ── Purchase Record ─────────────────────────────────────────────────────

// PurchaseRecord 记录一次 Skill 购买。
type PurchaseRecord struct {
	ID               string    `json:"id"`
	HubID            string    `json:"hub_id,omitempty"`
	TenantID         string    `json:"tenant_id,omitempty"`
	BuyerEmail       string    `json:"buyer_email"`
	BuyerID          string    `json:"buyer_id"`
	SkillID          string    `json:"skill_id"`
	PurchasedVersion int       `json:"purchased_version"`
	PurchaseType     string    `json:"purchase_type"` // purchase, upgrade
	AmountPaid       int64     `json:"amount_paid"`
	PlatformFee      int64     `json:"platform_fee"`
	SellerEarning    int64     `json:"seller_earning"`
	SellerID         string    `json:"seller_id"`
	KeyStatus        string    `json:"key_status,omitempty"` // "", delivered, pending_key, key_delivered, refunded, cancelled
	APIKeyID         string    `json:"api_key_id,omitempty"`
	Status           string    `json:"status"` // active, refunded
	CreatedAt        time.Time `json:"created_at"`
}

// ── Rating ───────────────────────────────────────────────────────────────

// Rating 记录单个用户对 Skill 的评分（以 email 去重）。
type Rating struct {
	SkillID   string    `json:"skill_id"`
	Email     string    `json:"email"`
	Score     int       `json:"score"` // -2 ~ +2
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RatingStats 是 Skill 的评分统计。
type RatingStats struct {
	SkillID      string  `json:"skill_id"`
	UniqueRaters int     `json:"unique_raters"`
	AverageScore float64 `json:"average_score"`
	TotalScore   int     `json:"total_score"`
}

// ── Admin Config ─────────────────────────────────────────────────────────

// AdminConfig 存储管理员可配置参数。
type AdminConfig struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ── Uploader Tier ────────────────────────────────────────────────────────

// UploaderTier 记录上传者的信誉等级。
type UploaderTier struct {
	UserID         string    `json:"user_id"`
	Tier           int       `json:"tier"` // 1-4
	PublishedCount int       `json:"published_count"`
	AvgRating      float64   `json:"avg_rating"`
	TotalDownloads int       `json:"total_downloads"`
	UpdatedAt      time.Time `json:"updated_at"`
}
