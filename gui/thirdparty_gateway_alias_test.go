package main

import (
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestSetThirdPartyHardwareDeviceAliasRejectsConcurrentDuplicates(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if _, err := app.LoadConfig(); err != nil {
		t.Fatalf("load config: %v", err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for clientID, alias := range map[string]string{"esp32-a": "Desk Pet", "esp32-b": "desk pet"} {
		wg.Add(1)
		go func(clientID, alias string) {
			defer wg.Done()
			<-start
			errs <- app.SetThirdPartyHardwareDeviceAlias(clientID, alias)
		}(clientID, alias)
	}
	close(start)
	wg.Wait()
	close(errs)

	var successes, duplicateFailures int
	for err := range errs {
		if err == nil {
			successes++
		} else if strings.Contains(err.Error(), "must be unique") {
			duplicateFailures++
		} else {
			t.Fatalf("unexpected rename error: %v", err)
		}
	}
	if successes != 1 || duplicateFailures != 1 {
		t.Fatalf("successes=%d duplicateFailures=%d, want 1/1", successes, duplicateFailures)
	}

	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if len(cfg.HardwareDeviceAliases) != 1 {
		t.Fatalf("aliases=%#v, want exactly one alias", cfg.HardwareDeviceAliases)
	}
}

func TestSetThirdPartyHardwareDeviceAliasIsIdempotent(t *testing.T) {
	app := &App{testHomeDir: t.TempDir(), configCacheValid: true, configCache: corelib.AppConfig{HardwareDeviceAliases: map[string]string{"esp32-a": "Desk Pet"}}}
	if err := app.SetThirdPartyHardwareDeviceAlias("esp32-a", "Desk Pet"); err != nil {
		t.Fatalf("idempotent rename: %v", err)
	}
}
