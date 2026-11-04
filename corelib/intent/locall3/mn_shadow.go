package locall3

// mn_shadow.go — shadow log for the Mongolian guard head.
//
// In MN shadow mode (intent.MNLocalEnv unset or "shadow", the default) every
// Cyrillic turn is decided by the remote L3 as before; the guard head runs in
// the background and one JSONL record per turn is appended with the local
// verdict next to the remote one, so real-traffic agreement / accuracy can be
// measured before MN local decisions are switched on. In mode "on" the record
// is written too (decided_by=local|remote).
//
//	MACLAW_INTENT_MN_SHADOW_LOG=<file>   default <intent-shadow dir>/mn_guard_shadow.jsonl
//	MACLAW_INTENT_MN_SHADOW_TEXT=hash|full|none   default hash (salted HMAC-SHA256)
//
// Local file only (0600, dir 0700), rotated at 32 MB (one backup); never sent anywhere.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

const (
	MNShadowLogEnv  = "MACLAW_INTENT_MN_SHADOW_LOG"
	MNShadowTextEnv = "MACLAW_INTENT_MN_SHADOW_TEXT"
	mnShadowMaxSize = 32 << 20
)

type mnShadowRecord struct {
	TS          string  `json:"ts"`
	Mode        string  `json:"mode"`
	Head        string  `json:"head"`
	Tau         float64 `json:"tau"`
	LocalLabel  string  `json:"local_label,omitempty"`
	LocalProb   float64 `json:"local_p"`
	Sensitive   bool    `json:"local_sensitive"`
	WouldAccept bool    `json:"would_accept"`
	LocalErr    string  `json:"local_err,omitempty"`
	DecidedBy   string  `json:"decided_by"` // local | remote | none
	RemoteTop   string  `json:"remote_top,omitempty"`
	RemoteErr   string  `json:"remote_err,omitempty"`
	Agree       *bool   `json:"agree,omitempty"` // local top-1 == remote top-1
	TextHash    string  `json:"text_hash,omitempty"`
	Text        string  `json:"text,omitempty"`
	Chars       int     `json:"chars"`
}

type mnShadowLog struct {
	mu   sync.Mutex
	path string
	text string
	salt []byte
}

func newMNShadowLog() *mnShadowLog {
	p := strings.TrimSpace(os.Getenv(MNShadowLogEnv))
	if p == "" {
		p = filepath.Join(intent.DefaultShadowLogDir(), "mn_guard_shadow.jsonl")
	}
	t := strings.ToLower(strings.TrimSpace(os.Getenv(MNShadowTextEnv)))
	if t != "full" && t != "none" {
		t = "hash"
	}
	return &mnShadowLog{path: p, text: t}
}

func (l *mnShadowLog) saltLocked() []byte {
	if l.salt != nil {
		return l.salt
	}
	sp := l.path + ".salt"
	if b, err := os.ReadFile(sp); err == nil && len(b) >= 16 {
		l.salt = b
		return b
	}
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	_ = os.WriteFile(sp, b, 0o600)
	l.salt = b
	return b
}

func (l *mnShadowLog) write(rec mnShadowRecord, text string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return
	}
	rec.Chars = len([]rune(text))
	switch l.text {
	case "full":
		rec.Text = text
	case "hash":
		m := hmac.New(sha256.New, l.saltLocked())
		m.Write([]byte(text))
		rec.TextHash = hex.EncodeToString(m.Sum(nil))[:32]
	}
	if fi, err := os.Stat(l.path); err == nil && fi.Size() > mnShadowMaxSize {
		_ = os.Rename(l.path, l.path+".1")
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(rec)
	_, _ = f.Write(append(b, '\n'))
}

// remoteTop extracts the top skill from a tree-LLM JSON answer.
func remoteTop(out string) string {
	var v struct {
		Top []struct {
			Skill string `json:"skill"`
		} `json:"top"`
	}
	if json.Unmarshal([]byte(out), &v) == nil && len(v.Top) > 0 {
		return v.Top[0].Skill
	}
	return ""
}

type mnVerdict struct {
	label  string
	prob   float64
	accept bool
	err    error
}

func (r *Runtime) logMN(s *state, mode, text string, v mnVerdict, decidedBy, rout string, rerr error) {
	if r.mnLog == nil {
		return
	}
	rec := mnShadowRecord{TS: time.Now().Format(time.RFC3339Nano), Mode: mode, Head: s.mnHead.ID, Tau: s.mnHead.Tau,
		LocalLabel: v.label, LocalProb: v.prob, Sensitive: s.mnHead.IsSensitive(v.label), WouldAccept: v.accept, DecidedBy: decidedBy}
	if v.err != nil {
		rec.LocalErr = v.err.Error()
	}
	if rerr != nil {
		rec.RemoteErr = rerr.Error()
	} else if rout != "" {
		rec.RemoteTop = remoteTop(rout)
		if rec.RemoteTop != "" && v.err == nil {
			lt := v.label
			if lt == string(intent.LabelUnknown) {
				lt = string(intent.LabelAmbiguous)
			}
			a := lt == rec.RemoteTop
			rec.Agree = &a
		}
	}
	r.mnLog.write(rec, text)
}
