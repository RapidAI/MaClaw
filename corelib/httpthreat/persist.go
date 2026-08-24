package httpthreat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

type TrainGold struct {
	ID         string `json:"id"`
	GoldClass  string `json:"gold_class"`
	GoldSource string `json:"gold_source"`
}

// TrainRun is the frozen snapshot of one successful fit. It has no previews.
type TrainRun struct {
	Version   int         `json:"version"`
	TrainedAt string      `json:"trained_at"`
	SampleIDs []string    `json:"sample_ids"`
	Golds     []TrainGold `json:"golds"`
}

type persistedArtifact struct {
	Pipeline     string            `json:"pipeline"`
	Serving      *Head             `json:"serving,omitempty"`
	Previous     *Head             `json:"previous,omitempty"`
	Candidate    *Head             `json:"candidate,omitempty"`
	TrainIDs     []string          `json:"train_ids,omitempty"`
	ACKs         map[string]string `json:"acks,omitempty"`
	TargetNodes  []string          `json:"target_nodes,omitempty"`
	OverrideNote string            `json:"override_note,omitempty"`
	ExportOK     bool              `json:"export_ok,omitempty"`
	CorpusCap    int               `json:"corpus_cap,omitempty"`
	RuleMap      map[string]string `json:"rule_map,omitempty"`
	SafetyValve  bool              `json:"safety_valve,omitempty"`
	IntelHosts   map[string]string `json:"intel_hosts,omitempty"`
	Sites        []string          `json:"sites,omitempty"`
}

func tenantFileName(tenant string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(tenant) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "_empty"
	}
	return b.String()
}

func writeAtomicJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

func (e *Engine) corpusPath(tenant string) string {
	return filepath.Join(e.dir, "corpus", tenantFileName(tenant)+".json")
}

func (e *Engine) artifactPath(tenant string) string {
	return filepath.Join(e.dir, "artifacts", tenantFileName(tenant)+".json")
}

func (e *Engine) runsPath(tenant string) string {
	return filepath.Join(e.dir, "runs", tenantFileName(tenant)+".json")
}

func (e *Engine) scheduleFlush(tenant string) {
	if e == nil || strings.TrimSpace(e.dir) == "" || strings.TrimSpace(tenant) == "" {
		return
	}
	e.mu.Lock()
	if e.flushDirty == nil {
		e.flushDirty = map[string]bool{}
	}
	if e.flushSched == nil {
		e.flushSched = map[string]bool{}
	}
	e.flushDirty[tenant] = true
	if e.flushSched[tenant] {
		e.mu.Unlock()
		return
	}
	e.flushSched[tenant] = true
	e.mu.Unlock()
	go func() {
		time.Sleep(200 * time.Millisecond)
		for {
			e.flushTenant(tenant)
			e.mu.Lock()
			if !e.flushDirty[tenant] {
				e.flushSched[tenant] = false
				e.mu.Unlock()
				return
			}
			e.mu.Unlock()
			time.Sleep(200 * time.Millisecond)
		}
	}()
}

func (e *Engine) flushTenant(tenant string) {
	if e == nil || strings.TrimSpace(e.dir) == "" || strings.TrimSpace(tenant) == "" {
		return
	}
	e.flushMu.Lock()
	defer e.flushMu.Unlock()
	e.mu.Lock()
	if e.flushDirty != nil {
		e.flushDirty[tenant] = false
	}
	e.mu.Unlock()
	samples := e.sealSamples(e.corpus.ListTenant(tenant))
	e.mu.Lock()
	a := e.art(tenant)
	art := persistedArtifact{
		Pipeline: a.Pipeline, Serving: a.Serving, Previous: a.Previous, Candidate: a.Candidate,
		TrainIDs: append([]string(nil), a.TrainIDs...), ACKs: map[string]string{},
		TargetNodes: append([]string(nil), a.TargetNodes...), OverrideNote: a.OverrideNote,
		ExportOK: a.ExportOK, CorpusCap: a.CorpusCap, RuleMap: map[string]string{},
		SafetyValve: a.SafetyValve, IntelHosts: map[string]string{},
		Sites: append([]string(nil), a.Sites...),
	}
	for k, v := range a.RuleMap {
		art.RuleMap[k] = v
	}
	for k, v := range a.IntelHosts {
		art.IntelHosts[k] = v
	}
	for k, v := range a.ACKs {
		art.ACKs[k] = v
	}
	runs := append([]TrainRun(nil), e.runs[tenant]...)
	e.mu.Unlock()
	err1 := writeAtomicJSON(e.corpusPath(tenant), samples)
	err2 := writeAtomicJSON(e.artifactPath(tenant), art)
	err3 := writeAtomicJSON(e.runsPath(tenant), runs)
	if err1 != nil || err2 != nil || err3 != nil {
		e.mu.Lock()
		if e.flushDirty == nil {
			e.flushDirty = map[string]bool{}
		}
		e.flushDirty[tenant] = true
		e.mu.Unlock()
	}
}

func (e *Engine) loadAll() {
	if e == nil || strings.TrimSpace(e.dir) == "" {
		return
	}
	tenants := map[string]struct{}{}
	for _, kind := range []string{"corpus", "artifacts", "runs"} {
		entries, err := os.ReadDir(filepath.Join(e.dir, kind))
		if err != nil {
			continue
		}
		for _, ent := range entries {
			if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
				continue
			}
			tenants[strings.TrimSuffix(ent.Name(), ".json")] = struct{}{}
		}
	}
	for name := range tenants {
		var samples []Sample
		if raw, err := os.ReadFile(filepath.Join(e.dir, "corpus", name+".json")); err == nil {
			samples = e.openSamples(raw)
		}
		if len(samples) > 0 {
			e.corpus.Load(samples)
		}
		tenant := name
		if len(samples) > 0 && strings.TrimSpace(samples[0].TenantID) != "" {
			tenant = samples[0].TenantID
		}
		var art persistedArtifact
		if err := readJSON(e.artifactPath(tenant), &art); err == nil || readJSON(filepath.Join(e.dir, "artifacts", name+".json"), &art) == nil {
			e.mu.Lock()
			a := e.art(tenant)
			if art.Pipeline != "" {
				a.Pipeline = NormalizePipeline(art.Pipeline)
			}
			a.Serving, a.Previous, a.Candidate = art.Serving, art.Previous, art.Candidate
			a.TrainIDs = append([]string(nil), art.TrainIDs...)
			a.TargetNodes = append([]string(nil), art.TargetNodes...)
			a.OverrideNote = art.OverrideNote
			a.ExportOK = art.ExportOK
			a.SafetyValve = art.SafetyValve
			a.CorpusCap = art.CorpusCap
			a.RuleMap = map[string]string{}
			for k, v := range art.RuleMap {
				a.RuleMap[k] = v
			}
			a.IntelHosts = map[string]string{}
			for k, v := range art.IntelHosts {
				a.IntelHosts[k] = v
			}
			a.Sites = append([]string(nil), art.Sites...)
			for _, site := range a.Sites {
				e.siteTenant[site] = tenant
			}
			cap := DefaultCorpusCap
			if a.CorpusCap > 0 {
				cap = a.CorpusCap
			}
			a.ACKs = map[string]string{}
			for k, v := range art.ACKs {
				a.ACKs[k] = v
			}
			e.mu.Unlock()
			e.corpus.TrimTenant(tenant, cap)
		} else if len(samples) > 0 {
			e.corpus.TrimTenant(tenant, DefaultCorpusCap)
		}
		var runs []TrainRun
		if err := readJSON(e.runsPath(tenant), &runs); err == nil || readJSON(filepath.Join(e.dir, "runs", name+".json"), &runs) == nil {
			e.mu.Lock()
			e.runs[tenant] = runs
			e.mu.Unlock()
		}
	}
}

func (e *Engine) writable() bool {
	if e == nil || strings.TrimSpace(e.dir) == "" {
		return true
	}
	if err := os.MkdirAll(e.dir, 0o755); err != nil {
		return false
	}
	f, err := os.CreateTemp(e.dir, ".httpthreat-w-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}
