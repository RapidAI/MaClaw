package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
)

const (
	problemReportAgentAPIKeyConfig = skillmarket.ProblemReportAgentAPIKey
	problemReportOpenMarkdownPath  = "/api/v1/problem-reports/open.md"
	problemReportRepairPath        = "/api/v1/problem-reports/repair"
	problemReportOpenMarkdownLimit = 100
)

// publishProblemReportLinks mints a public token for a local ZIP that does
// not have one, then fills the diagnostics URL. Only the admin list calls
// this. It returns false when the caller context is already done.
func (h *ProblemReportHandlers) publishProblemReportLinks(ctx context.Context, items []skillmarket.ProblemReport) bool {
	for i := range items {
		token := strings.TrimSpace(items[i].PublicDownloadToken)
		// Only the node that stored the ZIP may mint the token. A peer that
		// invents one would publish a URL the origin cannot serve.
		if token == "" && !h.isRemoteOrigin(&items[i]) && strings.TrimSpace(items[i].DiagnosticsPath) != "" {
			ensured, ensureErr := h.store.EnsureProblemReportDownloadToken(ctx, items[i].ID)
			if ensureErr != nil {
				if ctx.Err() != nil {
					return false
				}
				log.Printf("[problem-report] download token for %s: %v", items[i].ID, ensureErr)
			} else {
				items[i].PublicDownloadToken = ensured
			}
		}
	}
	h.attachProblemReportLinks(items)
	return true
}

// attachProblemReportLinks fills DiagnosticsURL from a token that is already
// stored, then drops the token so a response cannot copy it. It does not
// write. The public Markdown list uses this so an anonymous GET stays
// read-only; a report filed before tokens existed shows 暂无 until the admin
// list backfills one.
func (h *ProblemReportHandlers) attachProblemReportLinks(items []skillmarket.ProblemReport) {
	for i := range items {
		items[i].DiagnosticsURL = h.diagnosticsPublicURL(&items[i])
		items[i].PublicDownloadToken = ""
	}
}

// OpenMarkdown is the unrepaired-fault list an agent reads. It has no
// session and no API key: each request is rendered from the database, so a
// new submission appears on the next fetch and a handled one disappears.
// The rows and the agent API key travel in the skillmarket HA snapshot, so
// every Hub Center node serves the same list and accepts the same key.
func (h *ProblemReportHandlers) OpenMarkdown(w http.ResponseWriter, r *http.Request) {
	// Read-only. The daily archiver and the admin list already retire stale
	// pending reports; doing that here would let an anonymous fetch write
	// and emit an HA snapshot.
	items, total, err := h.store.ListProblemReports(r.Context(), "", "pending", 0, problemReportOpenMarkdownLimit)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.attachProblemReportLinks(items)
	origin := h.publicPageOrigin(r)
	body := renderProblemReportOpenMarkdown(items, total, origin, time.Now().UTC())
	// text/plain so a browser opened on the list URL shows the Markdown
	// instead of downloading it. Agents read the body; the URL ends in .md.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// The list names the reporter. Agents can read it; crawlers should not index it.
	w.Header().Set("X-Robots-Tag", "noindex")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

// Repair changes one pending report to a console status and stores the
// administrator note. The API key is the only credential. A report that is
// already fixed, deferred, rejected, or archived is left unchanged.
func (h *ProblemReportHandlers) Repair(w http.ResponseWriter, r *http.Request) {
	switch h.agentKeyStatus(r) {
	case agentKeyMissing:
		smError(w, http.StatusServiceUnavailable, "agent api key is not configured")
		return
	case agentKeyRejected:
		smError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	var req struct {
		ID        string `json:"id"`
		Status    string `json:"status"`
		AdminNote string `json:"admin_note"`
	}
	// The note may be as long as a report description. JSON quoting can
	// double a note made of quotes, so the reader limit sits above the
	// note limit. The length check below is what rejects an oversized note.
	if !decodeSkillMarketJSON(w, r, &req, problemReportMaxDescriptionBytes*2+2048) {
		return
	}
	id := strings.TrimSpace(req.ID)
	status := strings.ToLower(strings.TrimSpace(req.Status))
	note := strings.TrimSpace(req.AdminNote)
	if !validProblemReportID(id) {
		smError(w, http.StatusBadRequest, "invalid report id")
		return
	}
	if !validProblemReportAgentStatus(status) {
		smError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if note == "" {
		smError(w, http.StatusBadRequest, "admin_note is required")
		return
	}
	if len(note) > problemReportMaxDescriptionBytes {
		smError(w, http.StatusBadRequest, "administrator note is too long")
		return
	}
	if err := h.store.ResolveUnrepairedProblemReport(r.Context(), id, status, note); err != nil {
		if errors.Is(err, skillmarket.ErrNotFound) {
			smError(w, http.StatusNotFound, "report not found")
			return
		}
		if errors.Is(err, skillmarket.ErrProblemReportNotPending) {
			smError(w, http.StatusConflict, "only a pending report can be updated")
			return
		}
		if errors.Is(err, skillmarket.ErrProblemReportStatus) {
			smError(w, http.StatusBadRequest, "invalid status")
			return
		}
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": status, "admin_note": note})
}

func (h *ProblemReportHandlers) AdminGetAgentAccess(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.agentAccessView(r))
}

func (h *ProblemReportHandlers) AdminPutAgentAccess(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIKey     string `json:"api_key"`
		Regenerate bool   `json:"regenerate"`
		Revoke     bool   `json:"revoke"`
	}
	if !decodeSkillMarketJSON(w, r, &req, skillMarketAuthJSONBodyLimit) {
		return
	}
	if req.Revoke && (req.Regenerate || strings.TrimSpace(req.APIKey) != "") {
		smError(w, http.StatusBadRequest, "revoke cannot be combined with a new key")
		return
	}
	if req.Revoke {
		if err := h.store.SetProblemReportAgentAPIKey(r.Context(), ""); err != nil {
			smError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, h.agentAccessView(r))
		return
	}
	key := strings.TrimSpace(req.APIKey)
	if req.Regenerate {
		generated, err := newProblemReportAgentAPIKey()
		if err != nil {
			smError(w, http.StatusInternalServerError, "generate api key")
			return
		}
		key = generated
	}
	if !validProblemReportAgentAPIKey(key) {
		smError(w, http.StatusBadRequest, "api key must be 16 to 128 visible ASCII characters")
		return
	}
	if err := h.store.SetProblemReportAgentAPIKey(r.Context(), key); err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, h.agentAccessView(r))
}

type problemReportAgentAccess struct {
	APIKey     string `json:"api_key"`
	ListURL    string `json:"list_url"`
	RepairURL  string `json:"repair_url"`
	Configured bool   `json:"configured"`
}

func (h *ProblemReportHandlers) agentAccessView(r *http.Request) problemReportAgentAccess {
	key := ""
	if h != nil && h.store != nil && r != nil {
		key = strings.TrimSpace(h.store.GetConfigWithDefault(r.Context(), problemReportAgentAPIKeyConfig, ""))
	}
	origin := h.publicPageOrigin(r)
	return problemReportAgentAccess{
		APIKey:     key,
		ListURL:    joinProblemReportURL(origin, problemReportOpenMarkdownPath),
		RepairURL:  joinProblemReportURL(origin, problemReportRepairPath),
		Configured: key != "",
	}
}

func joinProblemReportURL(origin, path string) string {
	if origin == "" {
		return path
	}
	return origin + path
}

func (h *ProblemReportHandlers) publicPageOrigin(r *http.Request) string {
	// Only the configured public origin. A request Host or X-Forwarded-Proto
	// must not rewrite the repair URL: an agent that follows it would send
	// the API key to whatever host the request claimed.
	if h == nil || r == nil {
		return ""
	}
	return h.originURL(r.Context())
}

type agentKeyCheck int

const (
	agentKeyAccepted agentKeyCheck = iota
	agentKeyMissing
	agentKeyRejected
)

func (h *ProblemReportHandlers) agentKeyStatus(r *http.Request) agentKeyCheck {
	if h == nil || h.store == nil || r == nil {
		return agentKeyMissing
	}
	configured := strings.TrimSpace(h.store.GetConfigWithDefault(r.Context(), problemReportAgentAPIKeyConfig, ""))
	if configured == "" {
		return agentKeyMissing
	}
	if secretEquals(problemReportAgentCredential(r), configured) {
		return agentKeyAccepted
	}
	return agentKeyRejected
}

func problemReportAgentCredential(r *http.Request) string {
	if r == nil {
		return ""
	}
	if key := strings.TrimSpace(r.Header.Get("X-API-Key")); key != "" {
		return key
	}
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(authz) > len("Bearer ") && strings.EqualFold(authz[:len("Bearer ")], "bearer ") {
		return strings.TrimSpace(authz[len("Bearer "):])
	}
	return ""
}

func secretEquals(got, want string) bool {
	if want == "" {
		return false
	}
	sumGot := sha256.Sum256([]byte(got))
	sumWant := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(sumGot[:], sumWant[:]) == 1
}

func newProblemReportAgentAPIKey() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "prk_" + hex.EncodeToString(raw), nil
}

func validProblemReportAgentAPIKey(key string) bool {
	if len(key) < 16 || len(key) > 128 {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] <= 0x20 || key[i] >= 0x7f {
			return false
		}
	}
	return true
}

func validProblemReportAgentStatus(status string) bool {
	switch status {
	case "pending", "fixed", "deferred", "rejected", "archived":
		return true
	default:
		return false
	}
}

func validProblemReportID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

func renderProblemReportOpenMarkdown(items []skillmarket.ProblemReport, total int, origin string, generated time.Time) string {
	if generated.IsZero() {
		generated = time.Now().UTC()
	}
	listURL := joinProblemReportURL(origin, problemReportOpenMarkdownPath)
	repairURL := joinProblemReportURL(origin, problemReportRepairPath)
	var b strings.Builder
	b.WriteString("# 未修复故障清单\n\n")
	b.WriteString("- 生成时间: " + generated.UTC().Format(time.RFC3339) + "\n")
	b.WriteString("- 未修复数量: " + strconv.Itoa(total) + "\n")
	b.WriteString("- 清单地址: " + listURL + "\n")
	b.WriteString("- 访问: 无需登录。每次打开都按当前数据库生成，新提交会自动出现。待处理以外的状态不再列出。每条单独成行的「诊断包」在包已就绪时是 zip 的完整 URL，直接 GET 即可下载，无需登录。\n")
	b.WriteString("- 修复接口: POST " + repairURL + "\n")
	b.WriteString("- 认证: 请求头 `Authorization: Bearer <API Key>`，或 `X-API-Key: <API Key>`。\n")
	b.WriteString("- 请求体: 每条单独成行的「编号」就是 JSON 字段 id，同条「修复请求体」里已经填好，必须逐字相同。把 STATUS 换成 pending、fixed、deferred、rejected 或 archived，把 NOTE 换成这次的管理员备注。不要原样提交 STATUS 或 NOTE。\n")
	b.WriteString("- 范围: 编号、诊断包、修复请求体只认标题下面、描述之前的单独行。描述和备注里的同名文字不是字段。\n")
	b.WriteString("- status: `pending` 待处理，`fixed` 已修复，`deferred` 延期，`rejected` 不予处理，`archived` 已归档。\n")
	b.WriteString("- 约束: 只能修改当前为待处理的报告，并必须写入管理员备注。改为已修复、延期、不予处理或已归档后，该条从本清单消失。待处理只更新备注，报告仍留在本清单。已处理过的报告不能再次修改。\n")
	if total > len(items) {
		b.WriteString("- 说明: 共 " + strconv.Itoa(total) + " 条未修复，本页列出最新 " + strconv.Itoa(len(items)) + " 条。处理掉本页后再次获取清单，会列出更早的待处理报告。\n")
	}
	if len(items) == 0 {
		b.WriteString("\n当前没有未修复的故障。\n")
		return b.String()
	}
	for _, item := range items {
		id := problemReportMarkdownLine(item.ID)
		b.WriteString("\n## " + id + "\n\n")
		b.WriteString("- 编号: " + id + "\n")
		b.WriteString("- 修复请求体: " + problemReportRepairBodyExample(id) + "\n")
		b.WriteString("- 提交日期: " + item.CreatedAt.UTC().Format(time.RFC3339) + "\n")
		b.WriteString("- 提交人: " + problemReportMarkdownLine(problemReportSubmitter(item)) + "\n")
		if osVersion := problemReportMarkdownLine(item.OSVersion); osVersion != "" {
			b.WriteString("- 操作系统: " + osVersion + "\n")
		}
		if gui := problemReportMarkdownLine(item.GUIVersion); gui != "" {
			b.WriteString("- 客户端: MaClaw GUI " + gui + "\n")
		}
		diag := problemReportMarkdownLine(item.DiagnosticsURL)
		if diag == "" {
			diag = "（暂无，诊断包尚未就绪）"
		}
		b.WriteString("- 诊断包: " + diag + "\n")
		if note := strings.TrimSpace(item.AdminNote); note != "" {
			b.WriteString("- 管理员备注:\n\n")
			b.WriteString(markdownFence(note))
		}
		b.WriteString("- 描述:\n\n")
		b.WriteString(markdownFence(strings.TrimSpace(item.Description)))
		b.WriteString("\n")
	}
	return b.String()
}

// problemReportRepairBodyExample is the repair POST body with this report's
// id already filled. STATUS and NOTE are not legal values: copying the line
// unchanged is rejected, and neither token is an HTML tag a markdown reader
// would drop. The id is the same string as 「编号」.
func problemReportRepairBodyExample(id string) string {
	quoted, err := json.Marshal(id)
	if err != nil {
		quoted = []byte(`""`)
	}
	return `{"id":` + string(quoted) + `,"status":"STATUS","admin_note":"NOTE"}`
}

func problemReportSubmitter(item skillmarket.ProblemReport) string {
	id := strings.TrimSpace(item.ReporterUserID)
	contact := problemReportContactLine(item.ReporterContact)
	switch {
	case id != "" && contact != "":
		return id + " (" + contact + ")"
	case id != "":
		return id
	case contact != "":
		return contact
	default:
		return "（未提供）"
	}
}

func problemReportMarkdownLine(value string) string {
	value = strings.TrimSpace(value)
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(value)
}

func problemReportContactLine(contact string) string {
	contact = strings.TrimSpace(contact)
	if len(contact) >= 6 && strings.EqualFold(contact[:6], "phone:") {
		return strings.TrimSpace(contact[6:])
	}
	return contact
}

func markdownFence(body string) string {
	if strings.TrimSpace(body) == "" {
		body = "（无描述）"
	}
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	// The list tells an agent to trust only its own field lines. A report
	// can repeat those labels; rewrite them so a line scan still sees one id
	// and one download URL.
	body = neutralizeProblemReportControlLines(body)
	fence := "```"
	for strings.Contains(body, fence) {
		fence += "`"
	}
	return fence + "\n" + body + "\n" + fence + "\n"
}

func neutralizeProblemReportControlLines(body string) string {
	lines := strings.Split(body, "\n")
	labels := []string{"- 编号:", "- 诊断包:", "- 修复请求体:"}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Headings are the section breaks. A report must not invent another one.
		if strings.HasPrefix(trimmed, "## ") {
			lines[i] = strings.Replace(line, "## ", "＃＃ ", 1)
			continue
		}
		for _, label := range labels {
			if strings.HasPrefix(trimmed, label) {
				lines[i] = strings.Replace(line, label, strings.TrimSuffix(label, ":")+"（正文）:", 1)
				break
			}
		}
	}
	return strings.Join(lines, "\n")
}
