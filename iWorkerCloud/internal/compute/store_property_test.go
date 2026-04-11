// Feature: compute-power-management, Property 2: Provider CRUD round-trip
package compute

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"io"
	"testing"
	"testing/quick"

	_ "modernc.org/sqlite"
)

func TestPropertyStoreCRUDRoundTrip(t *testing.T) {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	key := make([]byte, AES256KeyLen)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		t.Fatalf("generate key: %v", err)
	}

	store := NewProviderStore(db, key)
	ctx := context.Background()
	if err := store.CreateTable(ctx); err != nil {
		t.Fatalf("create table: %v", err)
	}

	protocols := []string{ProtocolOpenAI, ProtocolAnthropic, ProtocolGemini}
	counter := 0

	f := func(name string, apiKey string, priority uint8, enabled bool) bool {
		if name == "" {
			name = "provider"
		}
		counter++
		proto := protocols[counter%len(protocols)]

		p := &ComputeProvider{
			Name:                 fmt.Sprintf("%s_%d", name, counter),
			BaseURL:              "https://api.example.com",
			APIKey:               apiKey,
			Protocol:             proto,
			UserAgent:            "test-agent",
			ComputeType:          "general",
			Model:                "gpt-4",
			Enabled:              enabled,
			Priority:             int(priority),
			Description:          "test provider",
			InputPricePerMToken:  1.5,
			OutputPricePerMToken: 2.0,
		}

		if err := store.CreateProvider(ctx, p); err != nil {
			t.Logf("create error: %v", err)
			return false
		}

		got, err := store.GetProvider(ctx, p.ID)
		if err != nil {
			t.Logf("get error: %v", err)
			return false
		}
		if got == nil {
			t.Log("provider not found after create")
			return false
		}

		// Compare all fields except id and timestamps
		if got.Name != p.Name {
			t.Logf("name mismatch: %q vs %q", got.Name, p.Name)
			return false
		}
		if got.BaseURL != p.BaseURL {
			return false
		}
		if got.APIKey != apiKey {
			t.Logf("api_key mismatch: %q vs %q", got.APIKey, apiKey)
			return false
		}
		if got.Protocol != p.Protocol {
			return false
		}
		if got.Enabled != p.Enabled {
			return false
		}
		if got.Priority != p.Priority {
			return false
		}
		if got.InputPricePerMToken != p.InputPricePerMToken {
			return false
		}
		if got.OutputPricePerMToken != p.OutputPricePerMToken {
			return false
		}
		if got.Model != p.Model {
			return false
		}
		if got.ComputeType != p.ComputeType {
			return false
		}
		if got.UserAgent != p.UserAgent {
			return false
		}
		if got.Description != p.Description {
			return false
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 2 failed: %v", err)
	}
}
