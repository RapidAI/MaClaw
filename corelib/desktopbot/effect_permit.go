package desktopbot

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
)

// ErrEffectIdentity means a side effect is missing the owner, the account,
// the run, or the parameters that decide what it would change. The call is
// not executed and nothing is written.
type ErrEffectIdentity struct{}

func (ErrEffectIdentity) Error() string {
	return "这次外部操作缺少身份或生效参数，没有执行。"
}

// EffectPermit is one server-owned authorization of an external effect.
// A model tool-call id is not part of it. A later cap_mcp call should fill
// this permit and pass it to RunAuthorized on the service journal. It should
// not open another ledger. Fresh is set only by the server after the user
// authorizes another operation; the model cannot set it.
type EffectPermit struct {
	RunID   string
	Owner   string
	Account string
	Host    string
	Target  string
	Tool    string
	Args    string
	Fresh   bool
}

// CanonicalEffectArgs is the stable parameter text for one effect.
// Secret-bearing fields are left out. Every other field is included, so a
// recipient, URL, body, path, or MCP argument is part of the authorization.
func CanonicalEffectArgs(command string, args map[string]any) (string, error) {
	payload := map[string]string{}
	if command = strings.TrimSpace(command); command != "" {
		payload["command"] = command
	}
	for key, value := range args {
		if skipEffectArg(key) {
			continue
		}
		text := canonicalEffectValue(value)
		if strings.TrimSpace(text) == "" {
			continue
		}
		payload[key] = text
	}
	if len(payload) == 0 {
		return "", ErrEffectIdentity{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// EffectTarget is the destination a person would recognize: a recipient, a
// URL, a path, a program, or the command itself.
func EffectTarget(command string, args map[string]any) string {
	for _, key := range []string{"to", "recipient", "url", "path", "program", "target", "host", "account", "mcp_server", "mcp_tool"} {
		if text := effectArgString(args, key); text != "" {
			return text
		}
	}
	if command = strings.TrimSpace(command); command != "" {
		return command
	}
	return effectArgString(args, "packages")
}

func (p EffectPermit) validate() error {
	if strings.TrimSpace(p.RunID) == "" || strings.TrimSpace(p.Owner) == "" || strings.TrimSpace(p.Account) == "" || strings.TrimSpace(p.Tool) == "" || strings.TrimSpace(p.Args) == "" || strings.TrimSpace(p.Target) == "" {
		return ErrEffectIdentity{}
	}
	return nil
}

func (p EffectPermit) bind() string {
	return ConsentBind(p.Owner, p.Account, p.Host, p.Target, p.Args)
}

func (p EffectPermit) fingerprint(command string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(p.Owner),
		strings.TrimSpace(p.Account),
		strings.ToLower(strings.TrimSpace(p.Host)),
		strings.TrimSpace(p.Target),
		EffectName(p.Tool, command),
		p.Args,
	}, "\n")))
	return hex.EncodeToString(sum[:])
}

// RunAuthorized claims one user-authorized effect. The journal key is the
// server permit, not a model tool-call id. The same permit does not run
// twice across retries or a restarted process. An unknown claim stays
// blocked. Fresh, set only after a new user authorization, starts another
// operation even when the parameters match. A different parameter set for
// the same run and tool is stale and does not run.
func RunAuthorized(db *sql.DB, permit EffectPermit, command string, run func() (string, error)) (string, bool, error) {
	if run == nil {
		return "", false, errors.New("desktop step has no body")
	}
	if err := permit.validate(); err != nil {
		return "", false, err
	}
	if db == nil {
		return "", false, errors.New("effect journal is closed")
	}
	if err := EnsureEffectJournal(db); err != nil {
		return "", false, err
	}
	step, err := resolvePermit(db, permit, command)
	if err != nil {
		return "", false, err
	}
	return RunClaimed(db, step, command, run)
}

func resolvePermit(db *sql.DB, permit EffectPermit, command string) (Step, error) {
	tool := EffectName(permit.Tool, command)
	fp := permit.fingerprint(command)
	bind := permit.bind()
	step := Step{RunID: permit.RunID, Tool: permit.Tool, Bind: bind}
	digest := effectDigest(tool, command, bind)
	if !permit.Fresh {
		var blocked string
		err := db.QueryRow(`SELECT idempotency_key FROM desktop_step_journal WHERE done=0 AND result=? LIMIT 1`, MarkUncertainResult(digest)).Scan(&blocked)
		if err == nil && blocked != "" {
			step.Key = blocked
			return step, nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return Step{}, err
		}
	}
	prefix := effectSlotPrefix(permit.RunID, tool)
	rows, err := db.Query(`SELECT idempotency_key, done FROM desktop_step_journal WHERE idempotency_key = ? OR idempotency_key LIKE ? ESCAPE '\' ORDER BY step_no DESC, idempotency_key DESC`, prefix+"fp/"+fp, likePrefix(prefix)+"%")
	if err != nil {
		return Step{}, err
	}
	defer rows.Close()
	type slotRow struct {
		key  string
		done bool
		fp   string
	}
	var found []slotRow
	for rows.Next() {
		var row slotRow
		var done int
		if err := rows.Scan(&row.key, &done); err != nil {
			return Step{}, err
		}
		row.done = done == 1
		row.fp, _ = fingerprintInKey(row.key)
		found = append(found, row)
	}
	if err := rows.Err(); err != nil {
		return Step{}, err
	}
	if !permit.Fresh {
		for _, row := range found {
			if row.fp == fp {
				step.Key = row.key
				return step, nil
			}
		}
		if len(found) > 0 {
			return Step{}, ErrConsentStale{}
		}
		step.Key = prefix + "fp/" + fp
		return step, nil
	}
	// A new user authorization does not replay an open or finished claim.
	step.Key = prefix + "u/" + strconv.Itoa(len(found)+1) + "/fp/" + fp
	return step, nil
}

func effectSlotPrefix(runID, tool string) string {
	return "slot/" + strings.TrimSpace(runID) + "/" + strings.TrimSpace(tool) + "/"
}

func fingerprintInKey(key string) (string, bool) {
	const mark = "/fp/"
	i := strings.LastIndex(key, mark)
	if i < 0 || i+len(mark) >= len(key) {
		return "", false
	}
	return key[i+len(mark):], true
}

func likePrefix(prefix string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(prefix)
}

func skipEffectArg(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "action", "messages", "memory", "conversation", "history", "transcript", "password", "secret", "token", "api_key", "authorization", "credential":
		return true
	default:
		return false
	}
}

func effectArgString(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	for name, value := range args {
		if strings.EqualFold(strings.TrimSpace(name), key) {
			return strings.TrimSpace(canonicalEffectValue(value))
		}
	}
	return ""
}

func canonicalEffectValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []string:
		items := append([]string(nil), typed...)
		sort.Strings(items)
		raw, _ := json.Marshal(items)
		return string(raw)
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return ""
		}
		return string(raw)
	}
}
