package jton

import (
	"math/rand"
	"testing"
)

// TestScannersMatchScalar fuzzes the dispatched scanners (AVX2 on amd64 for
// inputs >= 32 bytes) against the scalar reference across lengths that straddle
// the 32-byte vector boundary and byte classes that exercise every escape rule.
func TestScannersMatchScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for iter := 0; iter < 50000; iter++ {
		b := make([]byte, rng.Intn(160))
		for i := range b {
			switch rng.Intn(6) {
			case 0:
				b[i] = '"'
			case 1:
				b[i] = '\\'
			case 2:
				b[i] = byte(rng.Intn(0x20)) // control byte
			default:
				b[i] = byte(0x20 + rng.Intn(0xE0))
			}
		}
		if got, want := escapeIndex(b), escapeIndexScalar(b); got != want {
			t.Fatalf("escapeIndex len=%d got=%d want=%d\n%q", len(b), got, want, b)
		}
		if got, want := scanStringBody(b), scanStringScalar(b); got != want {
			t.Fatalf("scanStringBody len=%d got=%d want=%d\n%q", len(b), got, want, b)
		}
	}
	// Clean buffers of every length 0..128 (no escapes): both must return len.
	for n := 0; n <= 128; n++ {
		b := make([]byte, n)
		for i := range b {
			b[i] = 'a'
		}
		if got := escapeIndex(b); got != n {
			t.Fatalf("escapeIndex clean len=%d got=%d", n, got)
		}
		if got := scanStringBody(b); got != n {
			t.Fatalf("scanStringBody clean len=%d got=%d", n, got)
		}
	}
}
