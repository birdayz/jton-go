package jton

// Byte scanners used on the hot paths. On amd64 these dispatch to AVX2 kernels
// (see simd_amd64.go / simd_amd64.s); everywhere else, and for short inputs,
// they use the scalar versions below. The scalar functions are also the
// reference behavior the SIMD kernels are property-tested against.

// escapeIndexScalar returns the index of the first byte in b that needs JSON
// string escaping: a control byte (< 0x20), '"', or '\\'. It returns len(b) if
// no such byte exists.
func escapeIndexScalar(b []byte) int {
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c < 0x20 || c == '"' || c == '\\' {
			return i
		}
	}
	return len(b)
}

// scanStringScalar returns the index of the first '"' or '\\' in b, or len(b).
func scanStringScalar(b []byte) int {
	for i := 0; i < len(b); i++ {
		if c := b[i]; c == '"' || c == '\\' {
			return i
		}
	}
	return len(b)
}
