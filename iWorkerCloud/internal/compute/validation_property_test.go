// Feature: compute-power-management, Property 3: Provider input validation
package compute

import (
	"testing"
	"testing/quick"
)

func TestPropertyValidationHTTPS(t *testing.T) {
	f := func(suffix string) bool {
		p := &ComputeProvider{
			BaseURL:              "https://" + suffix,
			Protocol:             ProtocolOpenAI,
			InputPricePerMToken:  0,
			OutputPricePerMToken: 0,
		}
		err := ValidateProvider(p)
		// Should pass URL check (may fail for other reasons, but URL is fine)
		if err != nil && err.Error() == `invalid base_url: must start with "https://"` {
			return false // https:// prefix should always pass URL check
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 3 (HTTPS) failed: %v", err)
	}
}

func TestPropertyValidationNonHTTPSRejects(t *testing.T) {
	prefixes := []string{"http://", "ftp://", "ws://", ""}
	f := func(seed uint8) bool {
		prefix := prefixes[int(seed)%len(prefixes)]
		p := &ComputeProvider{
			BaseURL:              prefix + "example.com",
			Protocol:             ProtocolOpenAI,
			InputPricePerMToken:  0,
			OutputPricePerMToken: 0,
		}
		err := ValidateProvider(p)
		return err != nil // non-https should always fail
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 3 (non-HTTPS reject) failed: %v", err)
	}
}

func TestPropertyValidationProtocol(t *testing.T) {
	valid := []string{ProtocolOpenAI, ProtocolAnthropic, ProtocolGemini}
	f := func(seed uint8) bool {
		proto := valid[int(seed)%len(valid)]
		p := &ComputeProvider{
			BaseURL:              "https://api.example.com",
			Protocol:             proto,
			InputPricePerMToken:  0,
			OutputPricePerMToken: 0,
		}
		return ValidateProvider(p) == nil
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 3 (valid protocol) failed: %v", err)
	}
}

func TestPropertyValidationInvalidProtocol(t *testing.T) {
	f := func(proto string) bool {
		if proto == ProtocolOpenAI || proto == ProtocolAnthropic || proto == ProtocolGemini {
			return true // skip valid protocols
		}
		p := &ComputeProvider{
			BaseURL:              "https://api.example.com",
			Protocol:             proto,
			InputPricePerMToken:  0,
			OutputPricePerMToken: 0,
		}
		return ValidateProvider(p) != nil
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 3 (invalid protocol) failed: %v", err)
	}
}

func TestPropertyValidationNonNegativePrice(t *testing.T) {
	f := func(inputSeed, outputSeed uint32) bool {
		inputPrice := float64(inputSeed) / 100.0
		outputPrice := float64(outputSeed) / 100.0
		p := &ComputeProvider{
			BaseURL:              "https://api.example.com",
			Protocol:             ProtocolOpenAI,
			InputPricePerMToken:  inputPrice,
			OutputPricePerMToken: outputPrice,
		}
		return ValidateProvider(p) == nil
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 3 (non-negative price) failed: %v", err)
	}
}

func TestPropertyValidationNegativePriceRejects(t *testing.T) {
	f := func(seed uint16) bool {
		// Ensure price is always negative (seed+1 avoids zero)
		inputPrice := -float64(int(seed)+1) * 0.01
		p := &ComputeProvider{
			BaseURL:              "https://api.example.com",
			Protocol:             ProtocolOpenAI,
			InputPricePerMToken:  inputPrice,
			OutputPricePerMToken: 0,
		}
		return ValidateProvider(p) != nil
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Property 3 (negative price reject) failed: %v", err)
	}
}
