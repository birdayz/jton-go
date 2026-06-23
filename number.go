package jton

import (
	"math"
	"math/big"
	"strconv"
)

// appendFloat appends f exactly as the JTON reference serializer does, with no
// intermediate string allocations.
//
// The reference uses the Rust `ryu` crate (Buffer::format) for finite floats and
// the literals NaN / Infinity / -Infinity for non-finite ones. This reproduces
// ryu's f64 "pretty" formatting byte-for-byte: shortest round-trip digits with
// ryu's fixed-vs-scientific cutoffs (kk = length + k; fixed for -5 < kk <= 16,
// scientific otherwise; positive exponents carry no '+' and no leading zeros).
func appendFloat(b []byte, f float64) []byte {
	switch {
	case math.IsNaN(f):
		return append(b, "NaN"...)
	case math.IsInf(f, 1):
		return append(b, "Infinity"...)
	case math.IsInf(f, -1):
		return append(b, "-Infinity"...)
	}

	if math.Signbit(f) {
		b = append(b, '-')
		f = math.Abs(f)
	}

	// Go's shortest scientific form yields ryu's digits and decimal exponent:
	// "d[.ddd]e±XX". strconv uses the same shortest-round-trip algorithm.
	var tmp [32]byte
	sci := strconv.AppendFloat(tmp[:0], f, 'e', -1, 64)

	ei := 0
	for ei < len(sci) && sci[ei] != 'e' {
		ei++
	}

	// Gather the significant digits contiguously (at most 17 for an f64).
	var dig [24]byte
	n := 0
	dig[n] = sci[0]
	n++
	start := 1
	if len(sci) > 1 && sci[1] == '.' {
		start = 2
	}
	for i := start; i < ei; i++ {
		dig[n] = sci[i]
		n++
	}
	digits := dig[:n]
	length := n

	// Parse the base-10 exponent without allocating.
	exp, esign, j := 0, 1, ei+1
	if j < len(sci) {
		switch sci[j] {
		case '+':
			j++
		case '-':
			esign = -1
			j++
		}
	}
	for ; j < len(sci); j++ {
		exp = exp*10 + int(sci[j]-'0')
	}
	exp *= esign

	kk := exp + 1 // 10^(kk-1) <= f < 10^kk
	k := kk - length

	switch {
	case k >= 0 && kk <= 16:
		// integer with trailing zeros: 1234e7 -> 12340000000.0
		b = append(b, digits...)
		for i := length; i < kk; i++ {
			b = append(b, '0')
		}
		b = append(b, '.', '0')
	case kk > 0 && kk <= 16:
		// decimal point inside the digits: 1234e-2 -> 12.34
		b = append(b, digits[:kk]...)
		b = append(b, '.')
		b = append(b, digits[kk:]...)
	case kk > -5 && kk <= 0:
		// leading-zero fraction: 1234e-6 -> 0.001234
		b = append(b, '0', '.')
		for i := 0; i < -kk; i++ {
			b = append(b, '0')
		}
		b = append(b, digits...)
	case length == 1:
		// single significant digit: 1e30
		b = append(b, digits[0], 'e')
		b = strconv.AppendInt(b, int64(kk-1), 10)
	default:
		// d.ddd e±XX
		b = append(b, digits[0], '.')
		b = append(b, digits[1:]...)
		b = append(b, 'e')
		b = strconv.AppendInt(b, int64(kk-1), 10)
	}
	return b
}

// formatFloat is the string form of appendFloat.
func formatFloat(f float64) string { return string(appendFloat(nil, f)) }

func formatBigInt(v *big.Int) string { return v.String() }
