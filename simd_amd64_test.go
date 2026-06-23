//go:build amd64

package jton

import (
	"bytes"
	"testing"
)

func cleanScanBuf() []byte {
	return bytes.Repeat([]byte("the quick brown fox jumps over the lazy dog "), 2048) // ~90 KB, no escapes
}

func BenchmarkEscapeScanAVX2(b *testing.B) {
	if !useAVX2 {
		b.Skip("no AVX2")
	}
	buf := cleanScanBuf()
	b.SetBytes(int64(len(buf)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = escapeIndexAVX2(&buf[0], len(buf))
	}
}

func BenchmarkEscapeScanScalar(b *testing.B) {
	buf := cleanScanBuf()
	b.SetBytes(int64(len(buf)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = escapeIndexScalar(buf)
	}
}
