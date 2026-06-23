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
	if f == 0 {
		return append(b, "0.0"...)
	}

	// Ryū shortest digits in one pass: value == output * 10^pexp.
	fbits := math.Float64bits(f)
	mant := fbits & ((uint64(1) << doubleMantissaBits) - 1)
	exp := uint32(fbits>>doubleMantissaBits) & ((1 << 11) - 1)
	output, pexp := d2d(mant, exp)

	var dig [24]byte
	d := strconv.AppendUint(dig[:0], output, 10)
	return appendPretty(b, d, int(pexp)+len(d))
}

// appendPretty formats shortest decimal digits dig at decimal-point position kk
// (10^(kk-1) <= value < 10^kk) in ryu's pretty style, byte-identical to the
// reference: fixed for -5 < kk <= 16, scientific otherwise; positive exponents
// carry no '+' and no leading zeros.
func appendPretty(b, dig []byte, kk int) []byte {
	length := len(dig)
	switch k := kk - length; {
	case k >= 0 && kk <= 16:
		// integer with trailing zeros: 1234e7 -> 12340000000.0
		b = append(b, dig...)
		for i := 0; i < k; i++ {
			b = append(b, '0')
		}
		b = append(b, '.', '0')
	case kk > 0 && kk <= 16:
		// decimal point inside the digits: 1234e-2 -> 12.34
		b = append(b, dig[:kk]...)
		b = append(b, '.')
		b = append(b, dig[kk:]...)
	case kk > -5 && kk <= 0:
		// leading-zero fraction: 1234e-6 -> 0.001234
		b = append(b, '0', '.')
		for i := 0; i < -kk; i++ {
			b = append(b, '0')
		}
		b = append(b, dig...)
	case length == 1:
		// single significant digit: 1e30
		b = append(b, dig[0], 'e')
		b = strconv.AppendInt(b, int64(kk-1), 10)
	default:
		// d.ddd e±XX
		b = append(b, dig[0], '.')
		b = append(b, dig[1:]...)
		b = append(b, 'e')
		b = strconv.AppendInt(b, int64(kk-1), 10)
	}
	return b
}

// formatFloat is the string form of appendFloat.
func formatFloat(f float64) string { return string(appendFloat(nil, f)) }

func formatBigInt(v *big.Int) string { return v.String() }
