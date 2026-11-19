package desktopbot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// planModeWords are the labels a specifying turn must not show. The turn
// itself stays an ordinary question.
var planModeWords = []string{
	"进入规划模式，", "进入规划模式,", "进入规划模式",
	"進入規劃模式，", "進入規劃模式,", "進入規劃模式",
	"进入隐藏规划", "進入隱藏規劃",
	"规划模式", "規劃模式", "隐藏规划", "隱藏規劃",
	"plan mode", "Plan Mode", "PLAN MODE",
}

// HidePlanMode removes a planning-mode label from a sentence the person
// sees. The remaining words stay. An empty result means the sentence was
// only that label.
func HidePlanMode(s string) string {
	out := strings.Join(strings.Fields(StripPlanModeLabels(s)), " ")
	return strings.Trim(out, " ，,。.")
}

// StripPlanModeLabels removes a planning-mode label and keeps the line
// breaks of a stored spec. The sentence the person sees uses HidePlanMode.
func StripPlanModeLabels(s string) string {
	out := s
	for _, word := range planModeWords {
		out = strings.ReplaceAll(out, word, "")
	}
	return strings.TrimSpace(out)
}

// specRec is one bot's unfinished spec. gen moves on every replace from a
// new message, so a background write from an older turn cannot land later.
type specRec struct {
	text string
	gen  uint64
}

var (
	specMu   sync.Mutex
	specRecs = map[string]specRec{}
	specFile string
)

type specDiskItem struct {
	Tenant string `json:"tenant"`
	User   string `json:"user"`
	Bot    string `json:"bot"`
	Gen    uint64 `json:"gen"`
	Text   string `json:"text"`
}

// BindSpecFile loads the open specs and keeps later edits in that file, so a
// restart can continue a specification that was not settled. An unreadable
// file is left in place and not used.
func BindSpecFile(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return os.ErrInvalid
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	recs := map[string]specRec{}
	if len(strings.TrimSpace(string(data))) > 0 {
		var disk struct {
			Items []specDiskItem `json:"items"`
		}
		if err := json.Unmarshal(data, &disk); err != nil {
			return err
		}
		for _, item := range disk.Items {
			key := specKey(item.Tenant, item.User, item.Bot)
			text := strings.TrimSpace(item.Text)
			if key == "" || text == "" {
				continue
			}
			gen := item.Gen
			if gen == 0 {
				gen = 1
			}
			recs[key] = specRec{text: text, gen: gen}
		}
	}
	specMu.Lock()
	specRecs = recs
	specFile = path
	specMu.Unlock()
	return nil
}

func detachSpecFile() {
	specMu.Lock()
	specFile = ""
	specMu.Unlock()
}

func persistSpecsLocked() {
	if specFile == "" {
		return
	}
	items := make([]specDiskItem, 0, len(specRecs))
	for key, rec := range specRecs {
		text := strings.TrimSpace(rec.text)
		if text == "" {
			continue
		}
		parts := strings.Split(key, "\x00")
		if len(parts) != 3 {
			continue
		}
		items = append(items, specDiskItem{Tenant: parts[0], User: parts[1], Bot: parts[2], Gen: rec.gen, Text: text})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Tenant != items[j].Tenant {
			return items[i].Tenant < items[j].Tenant
		}
		if items[i].User != items[j].User {
			return items[i].User < items[j].User
		}
		return items[i].Bot < items[j].Bot
	})
	body, err := json.Marshal(struct {
		Items []specDiskItem `json:"items"`
	}{Items: items})
	if err != nil {
		return
	}
	tmp := specFile + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, specFile); err != nil {
		if werr := os.WriteFile(specFile, body, 0o600); werr != nil {
			_ = os.Remove(tmp)
			return
		}
		_ = os.Remove(tmp)
	}
}

func specKey(tenant, user, bot string) string {
	bot = strings.TrimSpace(bot)
	if bot == "" {
		return ""
	}
	return strings.TrimSpace(tenant) + "\x00" + strings.TrimSpace(user) + "\x00" + bot
}

// NoteSpec stores the spec written so far and returns its generation.
// An empty instruction is ignored and returns 0.
func NoteSpec(tenant, user, bot, instruction string) uint64 {
	key := specKey(tenant, user, bot)
	instruction = strings.TrimSpace(instruction)
	if key == "" || instruction == "" {
		return 0
	}
	specMu.Lock()
	defer specMu.Unlock()
	rec := specRecs[key]
	rec.gen++
	rec.text = instruction
	specRecs[key] = rec
	persistSpecsLocked()
	return rec.gen
}

// SpecOf is the open spec, or empty when this bot is not specifying.
func SpecOf(tenant, user, bot string) string {
	key := specKey(tenant, user, bot)
	if key == "" {
		return ""
	}
	specMu.Lock()
	defer specMu.Unlock()
	return strings.TrimSpace(specRecs[key].text)
}

// SpecGen is the generation of the open spec. Zero means there is none.
func SpecGen(tenant, user, bot string) uint64 {
	key := specKey(tenant, user, bot)
	if key == "" {
		return 0
	}
	specMu.Lock()
	defer specMu.Unlock()
	rec, ok := specRecs[key]
	if !ok || strings.TrimSpace(rec.text) == "" {
		return 0
	}
	return rec.gen
}

// ReplaceSpec writes text only when gen is still the open spec's generation.
// A cleared spec or a newer message makes it return false.
func ReplaceSpec(tenant, user, bot string, gen uint64, text string) bool {
	key := specKey(tenant, user, bot)
	text = strings.TrimSpace(text)
	if key == "" || gen == 0 || text == "" {
		return false
	}
	specMu.Lock()
	defer specMu.Unlock()
	rec, ok := specRecs[key]
	if !ok || rec.gen != gen || strings.TrimSpace(rec.text) == "" {
		return false
	}
	rec.text = text
	specRecs[key] = rec
	persistSpecsLocked()
	return true
}

// ClearSpec drops the open spec. A settled job and a different job call it.
func ClearSpec(tenant, user, bot string) {
	key := specKey(tenant, user, bot)
	if key == "" {
		return
	}
	specMu.Lock()
	defer specMu.Unlock()
	if _, ok := specRecs[key]; !ok {
		return
	}
	delete(specRecs, key)
	persistSpecsLocked()
}

// ResetSpecMemory drops every open spec. Tests call it.
func ResetSpecMemory() {
	specMu.Lock()
	defer specMu.Unlock()
	specRecs = map[string]specRec{}
}
