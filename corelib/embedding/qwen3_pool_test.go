package embedding

import (
	"math"
	"testing"
)

func cosF32(a, b []float32) float64 {
	var d, na, nb float64
	for i := range a {
		d += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	return d / math.Sqrt(na*nb)
}

// Pooled last-token == EmbedWithRole (up to unpruned-row float noise); mean
// with the prefix cache == mean without it; mean is not the last token.
func TestQwen3PooledEmbed(t *testing.T) {
	e := loadQwen3(t)
	for _, txt := range qwen3GoldenTexts {
		ref, err := e.EmbedWithRole(txt, RoleClassification)
		if err != nil {
			t.Fatal(err)
		}
		last, mean, err := e.EmbedPooledWithRole(txt, RoleClassification)
		if err != nil {
			t.Fatal(err)
		}
		if c := cosF32(ref, last); c < 0.9999 {
			t.Fatalf("%q pooled last vs EmbedWithRole cos %.6f", txt, c)
		}
		qwen3PrefixKVEnabled = false
		_, mean2, err := e.EmbedPooledWithRole(txt, RoleClassification)
		qwen3PrefixKVEnabled = true
		if err != nil {
			t.Fatal(err)
		}
		if c := cosF32(mean, mean2); c < 0.9999 {
			t.Fatalf("%q mean cached-prefix vs full cos %.6f", txt, c)
		}
		if c := cosF32(mean, last); c > 0.99 {
			t.Fatalf("%q mean ~ last (cos %.4f): pooling not applied", txt, c)
		}
		t.Logf("%q last~ref %.6f mean cache~full %.6f mean~last %.3f", txt, cosF32(ref, last), cosF32(mean, mean2), cosF32(mean, last))
	}
}
