package guiapp

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// The bot's first line ("明白：……我先……") has to arrive in a couple of
// seconds. The person's chosen model is picked for quality, not speed: on
// papers 2026-11-12 qwen4-max-preview took 11-40 s per call, so the first
// reply missed every budget and the person saw a stock receipt. The first
// reply therefore races the chosen model against the fastest model this
// account can reach (desktopBotFastAckConfig) and keeps whichever usable line
// comes back first. Cost is not a concern here (Daniel 11-12): a one-line
// call is a few hundred tokens.

// desktopBotAckMaxOut caps a one-line reply. About forty Chinese characters
// is under a hundred tokens; the cap keeps a rambling or thinking model from
// spending seconds on text that is cut anyway.
const desktopBotAckMaxOut = 160

// desktopBotFastAckLatency remembers how long each model took to write a
// first line (exponentially weighted), so the fast pick follows what is
// actually fast on this machine and network, not only what a name suggests.
var desktopBotFastAckLatency = struct {
	sync.Mutex
	ms map[string]float64
}{ms: map[string]float64{}}

func desktopBotFastAckKey(cfg corelib.MaclawLLMConfig) string {
	return strings.TrimRight(strings.TrimSpace(cfg.URL), "/") + "|" + strings.ToLower(strings.TrimSpace(cfg.Model))
}

// noteDesktopBotAckLatency records one answered first-line call.
func noteDesktopBotAckLatency(cfg corelib.MaclawLLMConfig, took time.Duration) {
	if strings.TrimSpace(cfg.Model) == "" || took <= 0 {
		return
	}
	key := desktopBotFastAckKey(cfg)
	ms := float64(took.Milliseconds())
	desktopBotFastAckLatency.Lock()
	defer desktopBotFastAckLatency.Unlock()
	if old, ok := desktopBotFastAckLatency.ms[key]; ok {
		ms = 0.6*old + 0.4*ms
	}
	desktopBotFastAckLatency.ms[key] = ms
}

func desktopBotKnownAckLatency(cfg corelib.MaclawLLMConfig) (float64, bool) {
	desktopBotFastAckLatency.Lock()
	defer desktopBotFastAckLatency.Unlock()
	ms, ok := desktopBotFastAckLatency.ms[desktopBotFastAckKey(cfg)]
	return ms, ok
}

// desktopBotModelSpeedScore guesses from a model id how quickly it answers a
// short prompt: positive for small or low-latency tiers, negative for large,
// reasoning or top tiers. It is only a prior; measured latency wins.
func desktopBotModelSpeedScore(model string) int {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return -100
	}
	score := 0
	for _, w := range []string{"flash", "turbo", "lite", "mini", "fast", "haiku", "instant", "nano", "air", "small", "official-low", "-low", "8b", "7b"} {
		if strings.Contains(m, w) {
			score += 3
			break
		}
	}
	for _, w := range []string{"max", "pro", "reasoner", "think", "-r1", "opus", "official-high", "-high", "o1", "o3", "72b", "235b", "405b"} {
		if strings.Contains(m, w) {
			score -= 3
			break
		}
	}
	if m == "auto" || m == "default" {
		score -= 1
	}
	return score
}

type desktopBotFastAckCandidate struct {
	cfg   corelib.MaclawLLMConfig
	score int
	ms    float64
	known bool
}

// desktopBotFastAckConfig is the fastest model this account can reach for a
// one-line reply, or nil when nothing differs from the chosen model. It looks
// at the MaClaw official provider (its catalog plus the low tier) and every
// custom provider that has a key, and skips the model the first-reply race
// already calls (skip, the chosen foreground model).
func (a *App) desktopBotFastAckConfig(skip corelib.MaclawLLMConfig) *corelib.MaclawLLMConfig {
	if a == nil {
		return nil
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil
	}
	state := a.maclawLLMProvidersFromConfig(&cfg)
	return pickDesktopBotFastAck(state.Providers, skip, func(p corelib.MaclawLLMProvider, model string) corelib.MaclawLLMConfig {
		return a.materializeResolvedRole(maclawLLMRoleForeground, p, model)
	})
}

// pickDesktopBotFastAck ranks every reachable model: measured latency first
// (under the first-reply budget), then the speed prior from its name, and
// returns the best one that is not skip.
func pickDesktopBotFastAck(providers []corelib.MaclawLLMProvider, skip corelib.MaclawLLMConfig, materialize func(corelib.MaclawLLMProvider, string) corelib.MaclawLLMConfig) *corelib.MaclawLLMConfig {
	skipKey := desktopBotFastAckKey(skip)
	seen := map[string]bool{}
	var out []desktopBotFastAckCandidate
	for _, p := range providers {
		hub := providerIsHubService(p)
		if !hub && (strings.TrimSpace(p.URL) == "" || (strings.TrimSpace(p.Key) == "" && strings.TrimSpace(p.OAuthAccessToken) == "")) {
			continue
		}
		models := append([]string{p.Model}, p.Models...)
		if hub {
			models = append(models, "official-low")
		}
		for _, model := range models {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			c := materialize(p, model)
			if strings.TrimSpace(c.URL) == "" || strings.TrimSpace(c.Model) == "" {
				continue
			}
			key := desktopBotFastAckKey(c)
			if key == skipKey || seen[key] {
				continue
			}
			seen[key] = true
			ms, known := desktopBotKnownAckLatency(c)
			out = append(out, desktopBotFastAckCandidate{cfg: c, score: desktopBotModelSpeedScore(model), ms: ms, known: known})
		}
	}
	if len(out) == 0 {
		return nil
	}
	budgetMs := float64(desktopBotFirstReplyBudget.Milliseconds())
	sort.SliceStable(out, func(i, j int) bool {
		fi := out[i].known && out[i].ms <= budgetMs
		fj := out[j].known && out[j].ms <= budgetMs
		if fi != fj {
			return fi
		}
		if fi && fj && out[i].ms != out[j].ms {
			return out[i].ms < out[j].ms
		}
		// A model measured slower than the budget loses to an untried one
		// with the same prior.
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		si := out[i].known && out[i].ms > budgetMs
		sj := out[j].known && out[j].ms > budgetMs
		return !si && sj
	})
	best := out[0]
	if best.score < 0 && !(best.known && best.ms <= budgetMs) {
		// Only large or reasoning models are reachable: racing another one
		// adds load without making the first line faster.
		return nil
	}
	cfg := best.cfg
	return &cfg
}
