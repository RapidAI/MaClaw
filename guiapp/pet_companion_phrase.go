package guiapp

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Stock lines are synthesized ahead of time and replayed from disk, so a wake
// acknowledgement does not wait on the speech engine.
func petStockPhrases() []string {
	return []string{
		petWakeAck,
		"没听清",
		"我看一下",
		petDocumentQuestion,
		"发到哪个聊天？",
		"好，先留在这儿。",
		"好，那先不做。",
		"删掉这个，对吗？",
		"现在发出去，对吗？",
		"在终端里跑，对吗？",
		"你说的是刚才那个，对吗？",
		"这步没做成。",
		"做好了。",
	}
}

func petIsStockPhrase(text string) bool {
	text = strings.TrimSpace(text)
	for _, line := range petStockPhrases() {
		if text == line {
			return true
		}
	}
	return false
}

var petPhraseMem = struct {
	mu    sync.Mutex
	items map[string]string
}{items: map[string]string{}}

var petPrewarmMu sync.Mutex

func petPhraseMemKey(voice, text string) string {
	return strings.TrimSpace(voice) + "\n" + strings.TrimSpace(text)
}

func rememberPetPhrase(voice, text, wav string) {
	if strings.TrimSpace(text) == "" || wav == "" {
		return
	}
	petPhraseMem.mu.Lock()
	petPhraseMem.items[petPhraseMemKey(voice, text)] = wav
	petPhraseMem.mu.Unlock()
}

func lookupPetPhrase(voice, text string) string {
	petPhraseMem.mu.Lock()
	defer petPhraseMem.mu.Unlock()
	return petPhraseMem.items[petPhraseMemKey(voice, text)]
}

func (a *App) petPhraseVoice() string {
	if a == nil {
		return "default"
	}
	voice := strings.TrimSpace(a.GetTTSVoiceID())
	if voice == "" {
		return "default"
	}
	return voice
}

func (a *App) petPhrasePath(text string) string {
	if a == nil {
		return ""
	}
	dir := strings.TrimSpace(a.GetDataDir())
	if dir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(text)))
	voice := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, a.petPhraseVoice())
	return filepath.Join(dir, "pet_phrase_cache", voice, hex.EncodeToString(sum[:8])+".b64")
}

func (a *App) cachedPetPhrase(text string) string {
	text = strings.TrimSpace(text)
	if a == nil || text == "" {
		return ""
	}
	voice := a.petPhraseVoice()
	if wav := lookupPetPhrase(voice, text); wav != "" {
		return wav
	}
	path := a.petPhrasePath(text)
	if path == "" {
		return ""
	}
	body, err := os.ReadFile(path)
	if err != nil || len(body) == 0 {
		return ""
	}
	wav := string(body)
	rememberPetPhrase(voice, text, wav)
	return wav
}

func (a *App) storePetPhrase(text, wav string) {
	text = strings.TrimSpace(text)
	if a == nil || text == "" || wav == "" {
		return
	}
	rememberPetPhrase(a.petPhraseVoice(), text, wav)
	path := a.petPhrasePath(text)
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		petLogf("phrase cache dir: %v", err)
		return
	}
	if err := os.WriteFile(path, []byte(wav), 0o644); err != nil {
		petLogf("phrase cache write: %v", err)
	}
}

func (a *App) prewarmPetPhrases() {
	if a == nil || !petPrewarmMu.TryLock() {
		return
	}
	defer petPrewarmMu.Unlock()
	missing := 0
	for _, line := range petStockPhrases() {
		if a.cachedPetPhrase(line) != "" {
			continue
		}
		missing++
		if a.synthesizePetLine(line) == "" {
			petLogf("phrase cache missed %s", line)
		}
	}
	petLogf("phrase cache ready missing=%d", missing)
}
