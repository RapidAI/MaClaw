//go:build amd64

package tensor

//go:noescape
func gemmaMaddwdRowM8N24AVX2Orig(out *float32, aQ *int16, aS *float32, bData *byte, bS *float32, N, ns, ne int)
