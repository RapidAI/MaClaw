package httpapi

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
)

// SkillMarketHandlers 处理 SkillMarket 相关的 HTTP 请求。
type SkillMarketHandlers struct {
	store          *skillmarket.Store
	skillStore     *skill.SkillStore
	userSvc        *skillmarket.UserService
	creditsSvc     *skillmarket.CreditsService
	processor      *skillmarket.Processor
	ratingSvc      *skillmarket.RatingService
	trialMgr       *skillmarket.TrialManager
	searchSvc      *skillmarket.SearchService
	leaderboardSvc *skillmarket.LeaderboardService
	apiKeySvc      *skillmarket.APIKeyPoolService
	refundSvc      *skillmarket.RefundService
	rateLimiter    *skillmarket.RateLimiter
	rsaPrivKey     *rsa.PrivateKey
	pendingDir     string
	dataDir        string
}

// SkillMarketConfig 是创建 SkillMarketHandlers 所需的配置。
type SkillMarketConfig struct {
	Store          *skillmarket.Store
	SkillStore     *skill.SkillStore
	UserSvc        *skillmarket.UserService
	CreditsSvc     *skillmarket.CreditsService
	Processor      *skillmarket.Processor
	RatingSvc      *skillmarket.RatingService
	TrialMgr       *skillmarket.TrialManager
	SearchSvc      *skillmarket.SearchService
	LeaderboardSvc *skillmarket.LeaderboardService
	APIKeySvc      *skillmarket.APIKeyPoolService
	RefundSvc      *skillmarket.RefundService
	RateLimiter    *skillmarket.RateLimiter
	RSAPrivKey     *rsa.PrivateKey
	PendingDir     string
	DataDir        string
}

// NewSkillMarketHandlers 创建 SkillMarket HTTP handlers。
func NewSkillMarketHandlers(cfg SkillMarketConfig) *SkillMarketHandlers {
	return &SkillMarketHandlers{
		store:          cfg.Store,
		skillStore:     cfg.SkillStore,
		userSvc:        cfg.UserSvc,
		creditsSvc:     cfg.CreditsSvc,
		processor:      cfg.Processor,
		ratingSvc:      cfg.RatingSvc,
		trialMgr:       cfg.TrialMgr,
		searchSvc:      cfg.SearchSvc,
		leaderboardSvc: cfg.LeaderboardSvc,
		apiKeySvc:      cfg.APIKeySvc,
		refundSvc:      cfg.RefundSvc,
		rateLimiter:    cfg.RateLimiter,
		rsaPrivKey:     cfg.RSAPrivKey,
		pendingDir:     cfg.PendingDir,
		dataDir:        cfg.DataDir,
	}
}

// ── Upload Submit ───────────────────────────────────────────────────────

// SubmitSkill handles POST /api/v1/skills/submit (multipart/form-data).
func (h *SkillMarketHandlers) SubmitSkill(w http.ResponseWriter, r *http.Request) {
	// 限制上传大小 100MB
	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)
	if err := r.ParseMultipartForm(100 << 20); err != nil {
		smError(w, http.StatusBadRequest, "invalid multipart form: "+err.Error())
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" {
		smError(w, http.StatusBadRequest, "email is required")
		return
	}

	file, header, err := r.FormFile("zip")
	if err != nil {
		smError(w, http.StatusBadRequest, "zip file is required")
		return
	}
	defer file.Close()

	if !strings.HasSuffix(strings.ToLower(header.Filename), ".zip") {
		smError(w, http.StatusBadRequest, "file must be a .zip")
		return
	}

	// 确保用户账户存在
	user, err := h.userSvc.EnsureAccount(r.Context(), email)
	if err != nil {
		smError(w, http.StatusInternalServerError, "ensure account: "+err.Error())
		return
	}

	// 频率限制检查
	if h.rateLimiter != nil {
		if err := h.rateLimiter.CheckRateLimit(r.Context(), email, user.ID); err != nil {
			smError(w, http.StatusTooManyRequests, err.Error())
			return
		}
		// 大小限制检查
		if err := h.rateLimiter.CheckSizeLimit(r.Context(), user.ID, header.Size); err != nil {
			smError(w, http.StatusRequestEntityTooLarge, err.Error())
			return
		}
	}

	// 保存 zip 到 pending 目录
	subID := fmt.Sprintf("sub-%d", uniqueCounter())
	_ = os.MkdirAll(h.pendingDir, 0o755)
	zipPath := filepath.Join(h.pendingDir, subID+".zip")
	out, err := os.Create(zipPath)
	if err != nil {
		smError(w, http.StatusInternalServerError, "save zip: "+err.Error())
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		smError(w, http.StatusInternalServerError, "save zip: "+err.Error())
		return
	}
	out.Close()

	// 创建 submission 记录
	sub := &skillmarket.SkillSubmission{
		ID:     subID,
		Email:  email,
		UserID: user.ID,
		Status: "pending",
		ZipPath: zipPath,
	}
	now := time.Now()
	sub.CreatedAt = now
	sub.UpdatedAt = now
	if err := h.store.CreateSubmission(r.Context(), sub); err != nil {
		os.Remove(zipPath) // 清理已保存的 zip 文件
		smError(w, http.StatusInternalServerError, "create submission: "+err.Error())
		return
	}

	// 入队异步处理
	h.processor.Enqueue(subID)

	writeJSON(w, http.StatusOK, map[string]any{
		"submission_id": subID,
		"status":        "pending",
	})
}

// GetSubmissionStatus handles GET /api/v1/skills/submissions/{id}.
func (h *SkillMarketHandlers) GetSubmissionStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		smError(w, http.StatusBadRequest, "submission id is required")
		return
	}
	sub, err := h.store.GetSubmissionByID(r.Context(), id)
	if err != nil {
		smError(w, http.StatusNotFound, "submission not found")
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

// ── Account ─────────────────────────────────────────────────────────────

// EnsureAccount handles POST /api/v1/account/ensure.
func (h *SkillMarketHandlers) EnsureAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		smError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if strings.TrimSpace(req.Email) == "" {
		smError(w, http.StatusBadRequest, "email is required")
		return
	}
	user, err := h.userSvc.EnsureAccount(r.Context(), strings.TrimSpace(req.Email))
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, user)
}

// GetAccount handles GET /api/v1/account/{email}.
func (h *SkillMarketHandlers) GetAccount(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	if email == "" {
		smError(w, http.StatusBadRequest, "email is required")
		return
	}
	user, err := h.userSvc.GetAccount(r.Context(), email)
	if err != nil {
		smError(w, http.StatusNotFound, "account not found")
		return
	}
	writeJSON(w, http.StatusOK, user)
}

// VerifyAccount handles POST /api/v1/account/verify.
func (h *SkillMarketHandlers) VerifyAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email  string `json:"email"`
		Method string `json:"method"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		smError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Email == "" {
		smError(w, http.StatusBadRequest, "email is required")
		return
	}
	user, err := h.userSvc.VerifyAccount(r.Context(), req.Email, req.Method)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, user)
}

// ── Credits ─────────────────────────────────────────────────────────────

// GetCreditsBalance handles GET /api/v1/credits/balance.
func (h *SkillMarketHandlers) GetCreditsBalance(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		smError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	balance, err := h.creditsSvc.GetBalance(r.Context(), userID)
	if err != nil {
		smError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"balance": balance})
}

// GetCreditsTransactions handles GET /api/v1/credits/transactions.
func (h *SkillMarketHandlers) GetCreditsTransactions(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		smError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	txs, total, err := h.store.ListTransactionsByUser(r.Context(), userID, offset, limit)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"transactions": txs, "total": total})
}

// TopUpCredits handles POST /api/v1/credits/topup.
func (h *SkillMarketHandlers) TopUpCredits(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id"`
		Amount int64  `json:"amount"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		smError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.creditsSvc.TopUp(r.Context(), req.UserID, req.Amount); err != nil {
		status := http.StatusInternalServerError
		if err == skillmarket.ErrUnverifiedAccount {
			status = http.StatusForbidden
		}
		smError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// WithdrawCredits handles POST /api/v1/credits/withdraw.
func (h *SkillMarketHandlers) WithdrawCredits(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id"`
		Amount int64  `json:"amount"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		smError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.creditsSvc.Withdraw(r.Context(), req.UserID, req.Amount); err != nil {
		status := http.StatusInternalServerError
		if err == skillmarket.ErrUnverifiedAccount {
			status = http.StatusForbidden
		} else if err == skillmarket.ErrInsufficientCredits {
			status = http.StatusPaymentRequired
		}
		smError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── Crypto ──────────────────────────────────────────────────────────────

// GetPublicKey handles GET /api/v1/crypto/pubkey.
func (h *SkillMarketHandlers) GetPublicKey(w http.ResponseWriter, r *http.Request) {
	pemData, err := skillmarket.LoadPublicKeyPEM(h.dataDir)
	if err != nil {
		smError(w, http.StatusInternalServerError, "public key not available")
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.WriteHeader(http.StatusOK)
	w.Write(pemData)
}

// ── Download (经济系统集成) ───────────────────────────────────────────────

// DownloadSkillMarket handles GET /api/v1/skillmarket/{id}/download.
// 集成：trial 免费、体验券、版本升级折扣、平台抽成 30%、API Key 分配。
func (h *SkillMarketHandlers) DownloadSkillMarket(w http.ResponseWriter, r *http.Request) {
	skillID := r.PathValue("id")
	if skillID == "" {
		smError(w, http.StatusBadRequest, "skill id is required")
		return
	}
	buyerEmail := r.URL.Query().Get("email")
	if buyerEmail == "" {
		smError(w, http.StatusBadRequest, "email is required")
		return
	}

	ctx := r.Context()

	// 获取 Skill
	sk, err := h.skillStore.Get(skillID)
	if err != nil {
		smError(w, http.StatusNotFound, "skill not found")
		return
	}
	if !sk.Visible {
		smError(w, http.StatusForbidden, "skill not available")
		return
	}

	// 确保买家账户
	buyer, err := h.userSvc.EnsureAccount(ctx, buyerEmail)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 获取 Skill 价格（从 sm_skill_index 或默认 0）
	price := h.getSkillPrice(ctx, skillID)

	var purchaseID string
	var amountPaid int64

	// trial 状态免费（Req 27）
	isTrial := false // TODO: 当 HubSkillMeta 有 Status 字段后检查
	_ = isTrial

	if price > 0 {
		// 检查体验券
		voucherUsed := false
		if buyer.VoucherCount > 0 && time.Now().Before(buyer.VoucherExpiresAt) {
			// 体验券不适用于声明了 required_env 的 Skill
			hasRequiredEnv := h.skillHasRequiredEnv(ctx, skillID)
			if !hasRequiredEnv {
				if err := h.useVoucher(ctx, buyer.ID); err == nil {
					voucherUsed = true
				}
			}
		}

		if !voucherUsed {
			// 检查版本升级折扣
			isUpgrade, _ := h.checkUpgradePurchase(ctx, buyer.ID, skillID)
			actualPrice := price
			if isUpgrade {
				actualPrice = price / 2 // 50% 折扣
			}

			// 扣款
			purchaseID = fmt.Sprintf("pur-%d", uniqueCounter())
			if err := h.creditsSvc.Debit(ctx, buyer.ID, actualPrice, skillID, purchaseID, "purchase skill"); err != nil {
				if err == skillmarket.ErrInsufficientCredits {
					smError(w, http.StatusPaymentRequired, fmt.Sprintf("insufficient credits: need %d", actualPrice))
					return
				}
				smError(w, http.StatusInternalServerError, err.Error())
				return
			}
			amountPaid = actualPrice

			// 平台抽成 30%
			platformFee := actualPrice * 30 / 100
			sellerEarning := actualPrice - platformFee

			// 给上传者入账（默认 settled，有 required_env 则 pending）
			settled := !h.skillHasRequiredEnv(ctx, skillID)
			uploaderID := h.getSkillUploaderID(ctx, skillID)
			if uploaderID != "" {
				purchaseType := "purchase"
				if isUpgrade {
					purchaseType = "upgrade"
				}
				_ = h.creditsSvc.Credit(ctx, uploaderID, sellerEarning, settled, skillID, purchaseID, "skill sold")
				_ = h.creditsSvc.RecordPlatformFee(ctx, platformFee, skillID, purchaseID, "platform fee 30%")

				// 创建 Purchase Record
				h.createPurchaseRecord(ctx, purchaseID, buyer, skillID, purchaseType, amountPaid, platformFee, sellerEarning, uploaderID)
			}
		}
	}

	// 加密下载
	encPkg, err := skillmarket.EncryptForDownload([]byte(sk.AgentSkillMD), buyer.ID, h.rsaPrivKey)
	if err != nil {
		smError(w, http.StatusInternalServerError, "encrypt failed: "+err.Error())
		return
	}

	// 下载成功后原子递增 download_count
	_ = h.skillStore.IncrementDownloadCount(skillID)

	writeJSON(w, http.StatusOK, map[string]any{
		"encrypted_data": encPkg,
		"skill_id":       skillID,
		"amount_paid":    amountPaid,
	})
}

func (h *SkillMarketHandlers) getSkillPrice(ctx context.Context, skillID string) int64 {
	var price int64
	_ = h.store.ReadDB().QueryRowContext(ctx, `SELECT price FROM sm_skill_index WHERE skill_id = ?`, skillID).Scan(&price)
	return price
}

func (h *SkillMarketHandlers) getSkillUploaderID(_ context.Context, skillID string) string {
	sk, err := h.skillStore.Get(skillID)
	if err != nil {
		return ""
	}
	return sk.UploaderID
}

func (h *SkillMarketHandlers) skillHasRequiredEnv(_ context.Context, skillID string) bool {
	sk, err := h.skillStore.Get(skillID)
	if err != nil {
		return false
	}
	return len(sk.RequiredEnv) > 0
}

func (h *SkillMarketHandlers) useVoucher(ctx context.Context, userID string) error {
	res, err := h.store.DB().ExecContext(ctx,
		`UPDATE sm_users SET voucher_count = voucher_count - 1, updated_at = ? WHERE id = ? AND voucher_count > 0`,
		time.Now().Format(time.RFC3339), userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("no voucher available")
	}
	return nil
}

func (h *SkillMarketHandlers) checkUpgradePurchase(ctx context.Context, buyerID, skillID string) (bool, error) {
	var count int
	err := h.store.ReadDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sm_purchase_records WHERE buyer_id = ? AND skill_id = ? AND status = 'active'`,
		buyerID, skillID).Scan(&count)
	return count > 0, err
}

func (h *SkillMarketHandlers) createPurchaseRecord(ctx context.Context, id string, buyer *skillmarket.SkillMarketUser, skillID, purchaseType string, amountPaid, platformFee, sellerEarning int64, sellerID string) {
	_, _ = h.store.DB().ExecContext(ctx, `
		INSERT INTO sm_purchase_records (id, buyer_email, buyer_id, skill_id, purchased_version, purchase_type, amount_paid, platform_fee, seller_earning, seller_id, status, created_at)
		VALUES (?, ?, ?, ?, 1, ?, ?, ?, ?, ?, 'active', ?)`,
		id, buyer.Email, buyer.ID, skillID, purchaseType, amountPaid, platformFee, sellerEarning, sellerID, time.Now().Format(time.RFC3339))
}

// ── Leaderboard ──────────────────────────────────────────────────────────

// GetLeaderboard handles GET /api/v1/skillmarket/top.
func (h *SkillMarketHandlers) GetLeaderboard(w http.ResponseWriter, r *http.Request) {
	sortBy := r.URL.Query().Get("sort")
	if sortBy == "" {
		sortBy = "rating"
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 10
	}
	entries := h.leaderboardSvc.GetTop(sortBy, limit)
	if entries == nil {
		entries = []skillmarket.LeaderboardEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "total": len(entries)})
}

// ── Search ───────────────────────────────────────────────────────────────

// SearchSkillMarket handles GET /api/v1/skillmarket/search.
func (h *SkillMarketHandlers) SearchSkillMarket(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	tagsParam := r.URL.Query().Get("tags")
	topN, _ := strconv.Atoi(r.URL.Query().Get("top_n"))
	if topN <= 0 {
		topN = 20
	}
	var tags []string
	if tagsParam != "" {
		for _, t := range strings.Split(tagsParam, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				tags = append(tags, t)
			}
		}
	}
	results, err := h.searchSvc.Search(r.Context(), query, tags, topN)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if results == nil {
		results = []skillmarket.SearchResult{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "total": len(results)})
}

// ── Rating ───────────────────────────────────────────────────────────────

// RateSkill handles POST /api/v1/skillmarket/{id}/rate.
func (h *SkillMarketHandlers) RateSkill(w http.ResponseWriter, r *http.Request) {
	skillID := r.PathValue("id")
	if skillID == "" {
		smError(w, http.StatusBadRequest, "skill id is required")
		return
	}
	var req struct {
		Email         string `json:"email"`
		Score         int    `json:"score"`
		UploaderEmail string `json:"uploader_email"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		smError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Email == "" {
		smError(w, http.StatusBadRequest, "email is required")
		return
	}
	takedown, err := h.ratingSvc.SubmitRating(r.Context(), skillID, req.Email, req.Score, req.UploaderEmail)
	if err != nil {
		smError(w, http.StatusBadRequest, err.Error())
		return
	}
	// -2 评分触发紧急下架
	if takedown {
		_ = h.trialMgr.EmergencyTakedown(r.Context(), skillID)
	}
	// 检查是否满足自动上架条件
	autoPublish, _ := h.trialMgr.CheckAutoPublish(r.Context(), skillID)
	if autoPublish {
		_ = h.trialMgr.AdminApprove(r.Context(), skillID)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"emergency_takedown": takedown,
		"auto_published":     autoPublish,
	})
}

// GetRatingStats handles GET /api/v1/skillmarket/{id}/ratings.
func (h *SkillMarketHandlers) GetRatingStats(w http.ResponseWriter, r *http.Request) {
	skillID := r.PathValue("id")
	if skillID == "" {
		smError(w, http.StatusBadRequest, "skill id is required")
		return
	}
	stats, err := h.ratingSvc.GetStats(r.Context(), skillID)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ratings, err := h.ratingSvc.GetRatings(r.Context(), skillID)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"stats":   stats,
		"ratings": ratings,
	})
}

// ── Admin Review ────────────────────────────────────────────────────────

// AdminApproveSkill handles POST /api/v1/admin/skillmarket/{id}/approve.
func (h *SkillMarketHandlers) AdminApproveSkill(w http.ResponseWriter, r *http.Request) {
	skillID := r.PathValue("id")
	if skillID == "" {
		smError(w, http.StatusBadRequest, "skill id is required")
		return
	}
	if err := h.trialMgr.AdminApprove(r.Context(), skillID); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

// AdminRejectSkill handles POST /api/v1/admin/skillmarket/{id}/reject.
func (h *SkillMarketHandlers) AdminRejectSkill(w http.ResponseWriter, r *http.Request) {
	skillID := r.PathValue("id")
	if skillID == "" {
		smError(w, http.StatusBadRequest, "skill id is required")
		return
	}
	if err := h.trialMgr.AdminReject(r.Context(), skillID); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

// UpdateTrialConfig handles PUT /api/v1/admin/config/trial.
func (h *SkillMarketHandlers) UpdateTrialConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TrialDuration       *int `json:"trial_duration_days,omitempty"`
		AutoPublishThreshold *int `json:"auto_publish_threshold,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		smError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	ctx := r.Context()
	if req.TrialDuration != nil {
		if err := h.store.SetConfig(ctx, "trial_duration_days", strconv.Itoa(*req.TrialDuration)); err != nil {
			smError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.AutoPublishThreshold != nil {
		if err := h.store.SetConfig(ctx, "auto_publish_threshold", strconv.Itoa(*req.AutoPublishThreshold)); err != nil {
			smError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── Withdraw (上传者主动下架) ─────────────────────────────────────────────

// WithdrawSkill handles POST /api/v1/skillmarket/{id}/withdraw.
func (h *SkillMarketHandlers) WithdrawSkill(w http.ResponseWriter, r *http.Request) {
	skillID := r.PathValue("id")
	if skillID == "" {
		smError(w, http.StatusBadRequest, "skill id is required")
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		smError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Email == "" {
		smError(w, http.StatusBadRequest, "email is required")
		return
	}

	// 权限检查：请求 email 必须匹配 Skill 的 author
	sk, err := h.skillStore.Get(skillID)
	if err != nil {
		smError(w, http.StatusNotFound, "skill not found")
		return
	}
	if sk.Author != req.Email {
		smError(w, http.StatusForbidden, "only the uploader can withdraw this skill")
		return
	}

	// 下架（设为不可见）
	if err := h.skillStore.SetVisibility(skillID, false); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "withdrawn"})
}

// ── Tier ─────────────────────────────────────────────────────────────────

// GetAccountTier handles GET /api/v1/account/{email}/tier.
func (h *SkillMarketHandlers) GetAccountTier(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	if email == "" {
		smError(w, http.StatusBadRequest, "email is required")
		return
	}
	user, err := h.userSvc.GetAccount(r.Context(), email)
	if err != nil {
		smError(w, http.StatusNotFound, "account not found")
		return
	}
	tier, err := h.rateLimiter.TierSvc().GetTier(r.Context(), user.ID)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	limits := h.rateLimiter.TierSvc().GetLimits(tier.Tier)
	writeJSON(w, http.StatusOK, map[string]any{
		"tier":   tier,
		"limits": limits,
	})
}

// ── API Key Pool ─────────────────────────────────────────────────────────

// UploadAPIKeys handles POST /api/v1/skillmarket/{id}/apikeys/upload.
func (h *SkillMarketHandlers) UploadAPIKeys(w http.ResponseWriter, r *http.Request) {
	skillID := r.PathValue("id")
	if skillID == "" {
		smError(w, http.StatusBadRequest, "skill id is required")
		return
	}
	var req struct {
		EnvName string   `json:"env_name"`
		Keys    []string `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		smError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if len(req.Keys) == 0 {
		smError(w, http.StatusBadRequest, "keys is required")
		return
	}
	count, err := h.apiKeySvc.UploadKeys(r.Context(), skillID, req.EnvName, req.Keys)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 补货后自动分配 pending 订单
	fulfilled, _ := h.apiKeySvc.FulfillPendingOrders(r.Context(), skillID)
	writeJSON(w, http.StatusOK, map[string]any{"uploaded": count, "fulfilled_pending": fulfilled})
}

// GetAPIKeyStatus handles GET /api/v1/skillmarket/{id}/apikeys/status.
func (h *SkillMarketHandlers) GetAPIKeyStatus(w http.ResponseWriter, r *http.Request) {
	skillID := r.PathValue("id")
	if skillID == "" {
		smError(w, http.StatusBadRequest, "skill id is required")
		return
	}
	stock := h.apiKeySvc.GetStockStatus(r.Context(), skillID)
	pending := h.apiKeySvc.GetPendingOrderCount(r.Context(), skillID)
	writeJSON(w, http.StatusOK, map[string]any{"stock_status": stock, "pending_orders": pending})
}

// ── Refund ───────────────────────────────────────────────────────────────

// AdminRefund handles POST /api/v1/admin/refund.
func (h *SkillMarketHandlers) AdminRefund(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PurchaseRecordID string `json:"purchase_record_id"`
		AdminEmail       string `json:"admin_email"`
		Reason           string `json:"reason"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		smError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.PurchaseRecordID == "" {
		smError(w, http.StatusBadRequest, "purchase_record_id is required")
		return
	}
	if err := h.refundSvc.ProcessRefund(r.Context(), req.PurchaseRecordID, req.AdminEmail, req.Reason); err != nil {
		status := http.StatusInternalServerError
		if err == skillmarket.ErrAlreadyRefunded {
			status = http.StatusConflict
		}
		smError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "refunded"})
}

// AdminListPurchases handles GET /api/v1/admin/purchases.
func (h *SkillMarketHandlers) AdminListPurchases(w http.ResponseWriter, r *http.Request) {
	buyerEmail := r.URL.Query().Get("buyer_email")
	skillID := r.URL.Query().Get("skill_id")
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	records, total, err := h.refundSvc.ListPurchases(r.Context(), buyerEmail, skillID, offset, limit)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": records, "total": total})
}

// ── helpers ─────────────────────────────────────────────────────────────

func smError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

var _counter uint64

func uniqueCounter() uint64 {
	return atomic.AddUint64(&_counter, 1)
}
