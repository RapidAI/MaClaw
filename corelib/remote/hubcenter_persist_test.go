package remote

import "testing"

func TestIsLoopbackURLCoversLocalAddressForms(t *testing.T) {
	tests := map[string]bool{
		"http://127.0.0.1:65140": true,
		"http://127.2.3.4:65140": true,
		"localhost:9388":         true,
		"http://[::1]:9388":      true,
		"::1":                    true,
		"http://0.0.0.0:9388":    true,
		"https://hubs.example":   false,
	}
	for value, want := range tests {
		if got := IsLoopbackURL(value); got != want {
			t.Fatalf("IsLoopbackURL(%q) = %v, want %v", value, got, want)
		}
	}
}
