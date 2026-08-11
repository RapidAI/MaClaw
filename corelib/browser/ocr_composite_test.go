package browser

import "testing"

type compositeOCRStub struct {
	closed int
}

func (*compositeOCRStub) Recognize(string) ([]OCRResult, error) { return nil, nil }
func (*compositeOCRStub) IsAvailable() bool                     { return true }
func (s *compositeOCRStub) Close()                              { s.closed++ }

func TestCompositeOCRProviderCloseDoesNotCloseBorrowedProviders(t *testing.T) {
	provider := &compositeOCRStub{}
	NewCompositeOCRProvider(provider).Close()
	if provider.closed != 0 {
		t.Fatalf("composite closed a borrowed OCR provider %d times", provider.closed)
	}
}
