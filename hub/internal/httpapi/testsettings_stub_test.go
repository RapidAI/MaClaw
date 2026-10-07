package httpapi

import (
	"context"
)

// stubSystemSettings is a minimal in-memory SystemSettingsRepository for tests.
type stubSystemSettings struct {
	data map[string]string
}

func (s *stubSystemSettings) Get(_ context.Context, key string) (string, error) {
	return s.data[key], nil
}

func (s *stubSystemSettings) Set(_ context.Context, key, value string) error {
	s.data[key] = value
	return nil
}
