package compute

import "testing"

func validProvider() *ComputeProvider {
	return &ComputeProvider{
		ID:                   "test-1",
		Name:                 "Test Provider",
		BaseURL:              "https://api.example.com",
		Protocol:             "openai",
		InputPricePerMToken:  3.0,
		OutputPricePerMToken: 15.0,
	}
}

func TestValidateProvider_Valid(t *testing.T) {
	for _, proto := range []string{"openai", "anthropic", "gemini"} {
		p := validProvider()
		p.Protocol = proto
		if err := ValidateProvider(p); err != nil {
			t.Errorf("expected valid for protocol %q, got: %v", proto, err)
		}
	}
}

func TestValidateProvider_BaseURL(t *testing.T) {
	cases := []struct {
		url     string
		wantErr bool
	}{
		{"https://api.example.com", false},
		{"https://api.example.com/v1", false},
		{"http://api.example.com", true},
		{"ftp://api.example.com", true},
		{"api.example.com", true},
		{"", true},
	}
	for _, tc := range cases {
		p := validProvider()
		p.BaseURL = tc.url
		err := ValidateProvider(p)
		if (err != nil) != tc.wantErr {
			t.Errorf("BaseURL=%q: wantErr=%v, got %v", tc.url, tc.wantErr, err)
		}
	}
}

func TestValidateProvider_Protocol(t *testing.T) {
	for _, proto := range []string{"", "gpt4", "claude", "OPENAI"} {
		p := validProvider()
		p.Protocol = proto
		if err := ValidateProvider(p); err == nil {
			t.Errorf("expected error for protocol %q", proto)
		}
	}
}

func TestValidateProvider_PriceNonNegative(t *testing.T) {
	// Zero prices are valid.
	p := validProvider()
	p.InputPricePerMToken = 0
	p.OutputPricePerMToken = 0
	if err := ValidateProvider(p); err != nil {
		t.Errorf("zero prices should be valid: %v", err)
	}

	// Negative input price.
	p = validProvider()
	p.InputPricePerMToken = -1.0
	if err := ValidateProvider(p); err == nil {
		t.Error("expected error for negative input price")
	}

	// Negative output price.
	p = validProvider()
	p.OutputPricePerMToken = -0.01
	if err := ValidateProvider(p); err == nil {
		t.Error("expected error for negative output price")
	}
}
