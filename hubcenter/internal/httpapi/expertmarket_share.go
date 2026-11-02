package httpapi

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
)

const (
	expertMarketMaxShareTargets = 32
	// Raw tokens can repeat the same person. Stop parsing well before a
	// hostile body can turn into one lookup per token.
	expertMarketMaxShareTargetTokens = expertMarketMaxShareTargets * 4
	expertMarketMaxSharePhoneDigits  = 20
)

var (
	expertShareEmailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	expertShareChinaMobile  = regexp.MustCompile(`^1[3-9]\d{9}$`)
)

// expertShareRejected is an input problem the caller can correct. Database
// failures stay ordinary errors so the handler does not report them as 400.
type expertShareRejected struct{ msg string }

func (e expertShareRejected) Error() string { return e.msg }

// expertShareTarget is one phone or email the owner named. Contact is the
// canonical sm_users email when that account already exists (including the
// phone: prefix), otherwise the normalized contact itself. UserID stays empty
// until an account with that contact exists. Matching never consults a Hub or
// tenant: the identifier is global.
type expertShareTarget struct {
	Contact string
	UserID  string
}

func (h *SkillMarketHandlers) ensureExpertShareSchema(db interface {
	Exec(string, ...any) (sql.Result, error)
}) {
	if db == nil {
		return
	}
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS sm_expert_market_share_targets (
		listing_id TEXT NOT NULL,
		contact TEXT NOT NULL,
		user_id TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		PRIMARY KEY (listing_id, contact)
	)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sm_expert_market_share_user ON sm_expert_market_share_targets(user_id, listing_id)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sm_expert_market_share_contact ON sm_expert_market_share_targets(contact, listing_id)`)
}

func splitExpertShareTargetField(values []string) []string {
	var out []string
	for _, value := range values {
		parts := strings.FieldsFunc(value, func(r rune) bool {
			switch r {
			case ',', ';', '\n', '\r', '，', '；', '、':
				return true
			default:
				return false
			}
		})
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func normalizeExpertShareContact(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len([]rune(raw)) > 254 {
		return "", false
	}
	if strings.Contains(raw, "@") {
		email := strings.ToLower(raw)
		if !expertShareEmailPattern.MatchString(email) {
			return "", false
		}
		return email, true
	}
	phone := normalizeEntryResolvePhoneIdentity(raw)
	if phone == "" {
		return "", false
	}
	digits := strings.TrimPrefix(phone, "phone:")
	if len(digits) > expertMarketMaxSharePhoneDigits {
		return "", false
	}
	if national, ok := expertShareNationalDigits(digits); ok {
		return "phone:" + national, true
	}
	return phone, true
}

// expertShareNationalDigits reports the 11-digit mainland mobile inside a
// stored or typed number. A single leading 86 or 0086 is the same number.
// Any other prefix is left alone so it is not treated as a country code.
func expertShareNationalDigits(digits string) (string, bool) {
	national := digits
	switch {
	case strings.HasPrefix(digits, "0086") && expertShareChinaMobile.MatchString(strings.TrimPrefix(digits, "0086")):
		national = strings.TrimPrefix(digits, "0086")
	case strings.HasPrefix(digits, "86") && expertShareChinaMobile.MatchString(strings.TrimPrefix(digits, "86")):
		national = strings.TrimPrefix(digits, "86")
	}
	if !expertShareChinaMobile.MatchString(national) {
		return "", false
	}
	return national, true
}

// expertShareContactKeys expands a stored or signed-in identity so a Chinese
// mobile still meets the same number written with 86 or 0086.
func expertShareContactKeys(identity string) []string {
	identity = strings.ToLower(strings.TrimSpace(identity))
	if identity == "" {
		return nil
	}
	keys := []string{identity}
	if digits, ok := strings.CutPrefix(identity, "phone:"); ok {
		if national, ok := expertShareNationalDigits(digits); ok {
			keys = append(keys, "phone:"+national, "phone:86"+national, "phone:0086"+national)
		}
	}
	return uniqueStrings(keys)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		unique = append(unique, value)
	}
	return unique
}

func expertShareInputSample(raw string) string {
	raw = strings.TrimSpace(raw)
	runes := []rune(raw)
	if len(runes) <= 80 {
		return raw
	}
	return string(runes[:80]) + "…"
}

func expertShareContactDisplay(contact string) string {
	contact = strings.TrimSpace(contact)
	digits := contact
	fromPhone := false
	if value, ok := strings.CutPrefix(strings.ToLower(contact), "phone:"); ok {
		digits = value
		fromPhone = true
	}
	if national, ok := expertShareNationalDigits(digits); ok {
		return national
	}
	if fromPhone {
		return digits
	}
	return contact
}

func expertShareTargetDisplays(targets []expertShareTarget) []string {
	out := make([]string, 0, len(targets))
	for _, target := range targets {
		if display := expertShareContactDisplay(target.Contact); display != "" {
			out = append(out, display)
		}
	}
	return out
}

type expertShareIdentity struct{ id, email string }

func (h *SkillMarketHandlers) lookupExpertShareUsers(ctx context.Context, keys []string) (map[string]expertShareIdentity, error) {
	found := map[string]expertShareIdentity{}
	keys = uniqueStrings(keys)
	if h == nil || h.store == nil || h.store.DB() == nil || len(keys) == 0 {
		return found, nil
	}
	placeholders := make([]string, len(keys))
	args := make([]any, len(keys))
	for i, key := range keys {
		placeholders[i] = "?"
		args[i] = key
	}
	rows, err := h.store.DB().QueryContext(ctx, `SELECT id, email FROM sm_users WHERE email COLLATE NOCASE IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, email string
		if err := rows.Scan(&id, &email); err != nil {
			return nil, err
		}
		found[strings.ToLower(strings.TrimSpace(email))] = expertShareIdentity{id: id, email: email}
	}
	return found, rows.Err()
}

func expertShareIdentityFor(found map[string]expertShareIdentity, keys []string) expertShareIdentity {
	for _, key := range keys {
		if item, ok := found[strings.ToLower(key)]; ok {
			return item
		}
	}
	return expertShareIdentity{}
}

func (h *SkillMarketHandlers) resolveExpertShareTargets(ctx context.Context, ownerID, ownerEmail string, rawFields []string) ([]expertShareTarget, error) {
	parts := splitExpertShareTargetField(rawFields)
	if len(parts) == 0 {
		return nil, expertShareRejected{msg: "enter at least one phone number or email"}
	}
	if len(parts) > expertMarketMaxShareTargetTokens {
		return nil, expertShareRejected{msg: "too many share targets"}
	}
	ownerKeys := map[string]bool{}
	for _, key := range expertShareContactKeys(ownerEmail) {
		ownerKeys[key] = true
	}
	type preparedShare struct {
		contact string
		keys    []string
	}
	prepared := make([]preparedShare, 0, len(parts))
	seenKey := map[string]bool{}
	lookupKeys := make([]string, 0, len(parts)*2)
	for _, part := range parts {
		contact, ok := normalizeExpertShareContact(part)
		if !ok {
			return nil, expertShareRejected{msg: "invalid phone number or email: " + expertShareInputSample(part)}
		}
		keys := expertShareContactKeys(contact)
		owned := false
		duplicate := false
		for _, key := range keys {
			if ownerKeys[key] {
				owned = true
			}
			if seenKey[key] {
				duplicate = true
			}
		}
		if owned {
			return nil, expertShareRejected{msg: "cannot share an expert with yourself"}
		}
		if duplicate {
			continue
		}
		for _, key := range keys {
			seenKey[key] = true
		}
		prepared = append(prepared, preparedShare{contact: contact, keys: keys})
		lookupKeys = append(lookupKeys, keys...)
		if len(prepared) > expertMarketMaxShareTargets {
			return nil, expertShareRejected{msg: "too many share targets"}
		}
	}
	if len(prepared) == 0 {
		return nil, expertShareRejected{msg: "enter at least one phone number or email"}
	}
	found, err := h.lookupExpertShareUsers(ctx, lookupKeys)
	if err != nil {
		return nil, err
	}
	targets := make([]expertShareTarget, 0, len(prepared))
	seenStored := map[string]bool{}
	for _, item := range prepared {
		account := expertShareIdentityFor(found, item.keys)
		if account.id != "" && account.id == ownerID {
			return nil, expertShareRejected{msg: "cannot share an expert with yourself"}
		}
		stored := item.contact
		if strings.TrimSpace(account.email) != "" {
			stored = strings.ToLower(strings.TrimSpace(account.email))
		}
		if seenStored[stored] {
			continue
		}
		seenStored[stored] = true
		targets = append(targets, expertShareTarget{Contact: stored, UserID: account.id})
	}
	if len(targets) == 0 {
		return nil, expertShareRejected{msg: "enter at least one phone number or email"}
	}
	return targets, nil
}

func (h *SkillMarketHandlers) expertMarketSharedWith(ctx context.Context, listingID, userID, email string) (bool, error) {
	keys := expertShareContactKeys(email)
	args := []any{listingID}
	clauses := make([]string, 0, 2)
	// An empty user id must not match every not-yet-linked target. Those rows
	// store user_id='' until the recipient signs in.
	if id := strings.TrimSpace(userID); id != "" {
		clauses = append(clauses, "user_id=?")
		args = append(args, id)
	}
	if len(keys) > 0 {
		placeholders := make([]string, len(keys))
		for i, key := range keys {
			placeholders[i] = "?"
			args = append(args, key)
		}
		clauses = append(clauses, "contact COLLATE NOCASE IN ("+strings.Join(placeholders, ",")+")")
	}
	if len(clauses) == 0 {
		return false, nil
	}
	var n int
	err := h.store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM sm_expert_market_share_targets WHERE listing_id=? AND (`+strings.Join(clauses, " OR ")+`)`, args...).Scan(&n)
	return n > 0, err
}

func (h *SkillMarketHandlers) expertMarketShareTargetDisplays(ctx context.Context, ids []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(ids) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := h.store.DB().QueryContext(ctx, `SELECT listing_id, contact FROM sm_expert_market_share_targets WHERE listing_id IN (`+strings.Join(placeholders, ",")+`) ORDER BY rowid ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var listingID, contact string
		if err := rows.Scan(&listingID, &contact); err != nil {
			return nil, err
		}
		if display := expertShareContactDisplay(contact); display != "" {
			out[listingID] = append(out[listingID], display)
		}
	}
	return out, rows.Err()
}

func (h *SkillMarketHandlers) linkExpertShareTargets(ctx context.Context, userID, email string) {
	keys := expertShareContactKeys(email)
	if strings.TrimSpace(userID) == "" || len(keys) == 0 || h == nil || h.store == nil || h.store.DB() == nil {
		return
	}
	placeholders := make([]string, len(keys))
	args := make([]any, len(keys))
	for i, key := range keys {
		placeholders[i] = "?"
		args[i] = key
	}
	inClause := strings.Join(placeholders, ",")
	var pending int
	if err := h.store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM sm_expert_market_share_targets WHERE user_id='' AND contact COLLATE NOCASE IN (`+inClause+`)`, args...).Scan(&pending); err != nil || pending == 0 {
		return
	}
	updateArgs := make([]any, 0, len(args)+1)
	updateArgs = append(updateArgs, userID)
	updateArgs = append(updateArgs, args...)
	_, _ = h.store.DB().ExecContext(ctx, `UPDATE sm_expert_market_share_targets SET user_id=? WHERE user_id='' AND contact COLLATE NOCASE IN (`+inClause+`)`, updateArgs...)
}
