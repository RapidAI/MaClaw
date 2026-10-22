package main

import (
	"testing"
	"time"
)

func fakeResidency(idle time.Duration, one bool) (*modelResidency, map[string]bool, *[]string) {
	loaded := map[string]bool{}
	var unloaded []string
	r := &modelResidency{idle: idle, oneModel: one, lastUsed: map[string]time.Time{}}
	r.loaded = func(m string) bool { return loaded[m] }
	r.unload = func(m string) { loaded[m] = false; unloaded = append(unloaded, m) }
	return r, loaded, &unloaded
}

func TestModelResidencyOneModelUnloadsOthers(t *testing.T) {
	r, loaded, unloaded := fakeResidency(time.Minute, true)
	loaded[srvAIModelOCR], loaded[srvAIModelASR] = true, true
	r.before(srvAIModelOmniParser)
	if loaded[srvAIModelOCR] || loaded[srvAIModelASR] || len(*unloaded) != 2 {
		t.Fatalf("others should be unloaded: %v", *unloaded)
	}
	loaded[srvAIModelOmniParser] = true
	r.before(srvAIModelOmniParser)
	if !loaded[srvAIModelOmniParser] {
		t.Fatal("the requested model must stay loaded")
	}
}

func TestModelResidencyKeepsOthersWhenOneModelOff(t *testing.T) {
	r, loaded, unloaded := fakeResidency(time.Minute, false)
	loaded[srvAIModelOCR] = true
	r.before(srvAIModelOmniParser)
	if !loaded[srvAIModelOCR] || len(*unloaded) != 0 {
		t.Fatal("one-model off must not unload")
	}
}

func TestModelResidencySweepUnloadsOnlyIdle(t *testing.T) {
	r, loaded, _ := fakeResidency(5*time.Minute, false)
	now := time.Now()
	loaded[srvAIModelOCR], loaded[srvAIModelTTS] = true, true
	r.lastUsed[srvAIModelOCR] = now.Add(-6 * time.Minute)
	r.lastUsed[srvAIModelTTS] = now.Add(-time.Minute)
	out := r.sweep(now)
	if len(out) != 1 || out[0] != srvAIModelOCR || loaded[srvAIModelOCR] || !loaded[srvAIModelTTS] {
		t.Fatalf("sweep=%v loaded=%v", out, loaded)
	}
	// a model loaded outside our view gets a clock, not an instant unload
	loaded[srvAIModelASR] = true
	if out := r.sweep(now); len(out) != 0 {
		t.Fatalf("unexpected unload %v", out)
	}
	if out := r.sweep(now.Add(6 * time.Minute)); len(out) != 2 {
		t.Fatalf("expected ASR+TTS unloaded, got %v", out)
	}
}

func TestModelResidencyIdleZeroNeverUnloads(t *testing.T) {
	r, loaded, _ := fakeResidency(0, false)
	loaded[srvAIModelOCR] = true
	r.lastUsed[srvAIModelOCR] = time.Now().Add(-time.Hour)
	if out := r.sweep(time.Now()); len(out) != 0 {
		t.Fatal("idle=0 must disable unloading")
	}
}

func TestModelResidencyEnv(t *testing.T) {
	t.Setenv("MACLAW_MODEL_IDLE_UNLOAD_SEC", "")
	t.Setenv("MACLAW_MODEL_SERVICE_ONE_MODEL", "")
	r := newModelResidencyFromEnv(newSrvAIModelManager(t.TempDir()))
	if r.idle != 300*time.Second || !r.oneModel {
		t.Fatalf("defaults: %v %v", r.idle, r.oneModel)
	}
	t.Setenv("MACLAW_MODEL_IDLE_UNLOAD_SEC", "60")
	t.Setenv("MACLAW_MODEL_SERVICE_ONE_MODEL", "0")
	r = newModelResidencyFromEnv(newSrvAIModelManager(t.TempDir()))
	if r.idle != time.Minute || r.oneModel {
		t.Fatalf("env: %v %v", r.idle, r.oneModel)
	}
}
