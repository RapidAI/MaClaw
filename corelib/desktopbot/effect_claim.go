package desktopbot

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
)

const effectUncertainPrefix = "effect-uncertain\t"

// ErrEffectUncertain means an external send, post, payment, transfer, delete,
// or other side effect was claimed and then interrupted or left ambiguous.
// The claim stays. A later call with the same key must not perform it again.
type ErrEffectUncertain struct{}

func (ErrEffectUncertain) Error() string {
	return "这次外部操作的结果还不能确定，不会自动重做。请先核对外部结果，再明确批准一次新的操作。"
}

func (ErrEffectUncertain) ExternalEffectHeld() string { return "uncertain" }

// ErrConsentStale means the stored claim was for a different owner, account,
// host, target, or content. The old claim is not permission to run the new
// one, and the old effect is not replayed.
type ErrConsentStale struct{}

func (ErrConsentStale) Error() string {
	return "原先的批准已失效：所有者、帐号、主机、目标或内容已变化。不会重放那次操作。"
}

func (ErrConsentStale) ExternalEffectHeld() string { return "stale" }

// ConsentBind is the identity of one approved effect. Content is hashed.
// The returned string is hashed again before it is stored, so the journal
// row does not keep the owner, the target, or the content.
func ConsentBind(owner, account, host, target, content string) string {
	sum := sha256.Sum256([]byte(content))
	return strings.Join([]string{
		strings.TrimSpace(owner),
		strings.TrimSpace(account),
		strings.ToLower(strings.TrimSpace(host)),
		strings.TrimSpace(target),
		hex.EncodeToString(sum[:]),
	}, "\n")
}

// EffectPending reports a claimed side effect that has no recorded outcome.
func EffectPending(step Step) bool {
	return !step.Done && strings.HasPrefix(step.Result, effectUncertainPrefix)
}

// MarkUncertainResult is the journal result of a claim that has not settled.
func MarkUncertainResult(digest string) string {
	return effectUncertainPrefix + strings.TrimSpace(digest)
}

// EnsureEffectJournal creates the step table used by RunClaimed. The columns
// stay the original six: an unfinished claim is done=0, not a new column.
func EnsureEffectJournal(db *sql.DB) error {
	if db == nil {
		return errors.New("effect journal is closed")
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS desktop_step_journal (
  idempotency_key TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  step_no INTEGER NOT NULL,
  tool TEXT NOT NULL,
  result TEXT NOT NULL,
  done INTEGER NOT NULL
)`); err != nil {
		return err
	}
	_, err := db.Exec(`CREATE INDEX IF NOT EXISTS desktop_step_journal_run ON desktop_step_journal(run_id, step_no)`)
	return err
}

// RunClaimed writes the side effect before it runs and lets exactly one
// caller own that key. A crash after the claim, an uncertain error, or a
// changed consent bind does not run the effect again. A definite error
// before any ambiguous outcome releases the claim so the same key can be
// tried again. A recorded success returns that result and does not run.
func RunClaimed(db *sql.DB, step Step, command string, run func() (string, error)) (string, bool, error) {
	if run == nil {
		return "", false, errors.New("desktop step has no body")
	}
	step.Key = strings.TrimSpace(step.Key)
	if step.Key == "" {
		text, err := run()
		return text, false, err
	}
	if db == nil {
		return "", false, errors.New("effect journal is closed")
	}
	step.RunID = strings.TrimSpace(step.RunID)
	if step.RunID == "" {
		step.RunID = step.Key
	}
	step.Tool = EffectName(step.Tool, command)
	digest := effectDigest(step.Tool, command, step.Bind)
	switch decision, text, err := claimEffect(db, step, digest); {
	case err != nil:
		return "", false, err
	case decision == claimDone:
		return text, true, nil
	case decision == claimUncertain:
		return "", false, ErrEffectUncertain{}
	case decision == claimStale:
		return "", false, ErrConsentStale{}
	}
	text, err := run()
	if err != nil {
		if keepEffectClaim(err) {
			return "", false, err
		}
		if relErr := releaseEffectClaim(db, step.Key); relErr != nil {
			return "", false, ErrEffectUncertain{}
		}
		return text, false, err
	}
	if err := completeEffectClaim(db, step, text); err != nil {
		return "", false, ErrEffectUncertain{}
	}
	return text, false, nil
}

const (
	claimReady int = iota
	claimDone
	claimUncertain
	claimStale
)

func effectDigest(tool, command, bind string) string {
	sum := sha256.Sum256([]byte(tool + "\n" + strings.TrimSpace(command) + "\n" + bind))
	return hex.EncodeToString(sum[:])
}

func keepEffectClaim(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var held interface{ ExternalEffectHeld() string }
	if errors.As(err, &held) {
		return true
	}
	var marked interface{ UncertainExternalEffect() }
	return errors.As(err, &marked)
}

func claimEffect(db *sql.DB, step Step, digest string) (int, string, error) {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return 0, "", err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return 0, "", err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	var result string
	var done int
	err = conn.QueryRowContext(ctx, `SELECT result, done FROM desktop_step_journal WHERE idempotency_key=?`, step.Key).Scan(&result, &done)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		var max sql.NullInt64
		if err := conn.QueryRowContext(ctx, `SELECT MAX(step_no) FROM desktop_step_journal WHERE run_id=?`, step.RunID).Scan(&max); err != nil {
			return 0, "", err
		}
		number := 1
		if step.Number > 0 {
			number = step.Number
		} else if max.Valid {
			number = int(max.Int64) + 1
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO desktop_step_journal(idempotency_key, run_id, step_no, tool, result, done) VALUES(?,?,?,?,?,0)`,
			step.Key, step.RunID, number, step.Tool, MarkUncertainResult(digest)); err != nil {
			return 0, "", err
		}
	case err != nil:
		return 0, "", err
	case done == 1:
		if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
			return 0, "", err
		}
		committed = true
		return claimDone, result, nil
	default:
		stored, ok := strings.CutPrefix(result, effectUncertainPrefix)
		if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
			return 0, "", err
		}
		committed = true
		if ok && stored != "" && stored != digest {
			return claimStale, "", nil
		}
		return claimUncertain, "", nil
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return 0, "", err
	}
	committed = true
	return claimReady, "", nil
}

func releaseEffectClaim(db *sql.DB, key string) error {
	_, err := db.Exec(`DELETE FROM desktop_step_journal WHERE idempotency_key=? AND done=0`, strings.TrimSpace(key))
	return err
}

func completeEffectClaim(db *sql.DB, step Step, result string) error {
	res, err := db.Exec(`UPDATE desktop_step_journal SET result=?, done=1, tool=? WHERE idempotency_key=? AND done=0`,
		result, step.Tool, step.Key)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	var done int
	err = db.QueryRow(`SELECT done FROM desktop_step_journal WHERE idempotency_key=?`, step.Key).Scan(&done)
	if errors.Is(err, sql.ErrNoRows) {
		number := step.Number
		if number <= 0 {
			number = 1
		}
		_, err = db.Exec(`INSERT OR IGNORE INTO desktop_step_journal(idempotency_key, run_id, step_no, tool, result, done) VALUES(?,?,?,?,?,1)`,
			step.Key, step.RunID, number, step.Tool, result)
		return err
	}
	if err != nil {
		return err
	}
	if done == 1 {
		return nil
	}
	return ErrEffectUncertain{}
}
