//go:build !windows && !linux && !darwin

package intent

// l3CacheBytes is unknown here; the Q8P auto policy then keeps float32.
func l3CacheBytes() int64 { return 0 }
