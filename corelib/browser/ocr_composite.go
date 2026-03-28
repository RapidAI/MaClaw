package browser

// CompositeOCRProvider tries multiple OCR providers in order, returning
// the first successful result. Fallback chain: RapidOCR → LLM Vision → empty.
type CompositeOCRProvider struct {
	providers []OCRProvider
}

// NewCompositeOCRProvider creates a composite provider from the given providers.
// Providers are tried in order; the first available and successful one wins.
func NewCompositeOCRProvider(providers ...OCRProvider) *CompositeOCRProvider {
	return &CompositeOCRProvider{providers: providers}
}

// Recognize implements OCRProvider.
func (c *CompositeOCRProvider) Recognize(pngBase64 string) ([]OCRResult, error) {
	for _, p := range c.providers {
		if p == nil || !p.IsAvailable() {
			continue
		}
		results, err := p.Recognize(pngBase64)
		if err == nil {
			return results, nil
		}
		// Try next provider on error
	}
	// All failed — return empty (not an error, let caller fallback to DOM text)
	return nil, nil
}

// IsAvailable implements OCRProvider.
func (c *CompositeOCRProvider) IsAvailable() bool {
	for _, p := range c.providers {
		if p != nil && p.IsAvailable() {
			return true
		}
	}
	return false
}

// Close implements OCRProvider.
func (c *CompositeOCRProvider) Close() {
	for _, p := range c.providers {
		if p != nil {
			p.Close()
		}
	}
}
